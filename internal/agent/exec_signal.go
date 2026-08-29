package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/manprint/pglens/internal/check"
	"github.com/manprint/pglens/internal/command"
)

// SignalExecutor sends a cancel or terminate request to a verified client
// backend. The command carries only a PID; the backend identity is read from
// pg_stat_activity immediately before the signal is sent.
type SignalExecutor struct{ kind command.Kind }

func NewCancelExecutor() *SignalExecutor {
	return &SignalExecutor{kind: command.KindCancel}
}

func NewTerminateExecutor() *SignalExecutor {
	return &SignalExecutor{kind: command.KindTerminate}
}

func (e *SignalExecutor) Kind() command.Kind { return e.kind }

func (e *SignalExecutor) Execute(ctx context.Context, target check.Target, args command.Args) (json.RawMessage, error) {
	if args.PID == nil {
		return nil, rejectedf("%s requires pid", e.kind)
	}
	conn, err := target.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	var backendType, applicationName, username string
	err = conn.QueryRow(ctx, `
		SELECT backend_type, application_name, usename
		FROM pg_stat_activity
		WHERE pid = $1`, *args.PID).Scan(&backendType, &applicationName, &username)
	if err != nil {
		if errorsIsNoRows(err) {
			return nil, rejectedf("pid %d is not present on this instance", *args.PID)
		}
		return nil, err
	}
	if backendType != "client backend" {
		return nil, rejectedf("pid %d is not a client backend", *args.PID)
	}
	if strings.HasPrefix(applicationName, "pglens-agent/") {
		return nil, rejectedf("refusing to signal pglens-agent backend pid %d", *args.PID)
	}

	function := "pg_cancel_backend"
	if e.kind == command.KindTerminate {
		function = "pg_terminate_backend"
	}
	var signalled bool
	if err := conn.QueryRow(ctx, fmt.Sprintf("SELECT %s($1)", function), *args.PID).Scan(&signalled); err != nil {
		return nil, err
	}
	result, err := json.Marshal(struct {
		PID             int    `json:"pid"`
		Signalled       bool   `json:"signalled"`
		ApplicationName string `json:"application_name"`
		Username        string `json:"usename"`
	}{PID: *args.PID, Signalled: signalled, ApplicationName: applicationName, Username: username})
	if err != nil {
		return nil, err
	}
	return result, nil
}
