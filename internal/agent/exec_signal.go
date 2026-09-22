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
	var ownRole, ownBackend bool
	err = conn.QueryRow(ctx, `
		SELECT COALESCE(backend_type, ''), COALESCE(application_name, ''), COALESCE(usename, ''),
		       COALESCE(usename = current_user, false), pid = pg_backend_pid()
		FROM pg_stat_activity
		WHERE pid = $1`, *args.PID).Scan(&backendType, &applicationName, &username, &ownRole, &ownBackend)
	if err != nil {
		if errorsIsNoRows(err) {
			return nil, rejectedf("pid %d is not present on this instance", *args.PID)
		}
		return nil, err
	}
	if backendType != "client backend" {
		return nil, rejectedf("pid %d is not a client backend", *args.PID)
	}
	// pglens never signals its own sessions. The check used to look for an
	// application_name prefix ("pglens-agent/") that the agent never sets —
	// its sessions are named pglens/shared, pglens/ash and pglens/<datname>,
	// and the role default is plain "pglens" — so it matched nothing and a
	// terminate could kill the agent's own ASH or check connections. The
	// session's role is the robust signal; the name is a second one.
	if ownBackend || ownRole || isPglensApplication(applicationName) {
		return nil, rejectedf("refusing to signal pglens backend pid %d", *args.PID)
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

// isPglensApplication reports whether an application_name belongs to a
// pglens session: "pglens" (the monitoring role's default) or "pglens/...".
func isPglensApplication(name string) bool {
	return name == "pglens" || strings.HasPrefix(name, "pglens/") || strings.HasPrefix(name, "pglens-agent/")
}
