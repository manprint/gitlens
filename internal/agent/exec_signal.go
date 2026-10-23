package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/manprint/pglens/internal/check"
	"github.com/manprint/pglens/internal/command"
)

// SignalExecutor sends a cancel or terminate request to a verified client
// backend. The command carries a PID and, when the caller supplies it, the
// start time of the session the caller saw at that PID. The backend identity
// is read from pg_stat_activity immediately before the signal is sent.
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
	var backendStart *time.Time
	err = conn.QueryRow(ctx, `
		SELECT COALESCE(backend_type, ''), COALESCE(application_name, ''), COALESCE(usename, ''),
		       COALESCE(usename = current_user, false), pid = pg_backend_pid(), backend_start
		FROM pg_stat_activity
		WHERE pid = $1`, *args.PID).Scan(&backendType, &applicationName, &username, &ownRole, &ownBackend, &backendStart)
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
	// A caller that names the session it saw must find that session at the
	// PID. A PID the operating system gave to a new session since then is
	// refused here, before any signal is sent. Times are compared at
	// microsecond precision, the precision PostgreSQL reports.
	if args.BackendStart != nil && (backendStart == nil || backendStart.UnixMicro() != args.BackendStart.UnixMicro()) {
		return nil, rejectedf("pid %d is a different session from the one requested (backend_start differs); nothing was signalled", *args.PID)
	}

	function := "pg_cancel_backend"
	if e.kind == command.KindTerminate {
		function = "pg_terminate_backend"
	}
	signalled, err := signalVerifiedBackend(ctx, conn, function, *args.PID, backendStart)
	if errorsIsNoRows(err) {
		return nil, rejectedf("pid %d is no longer the session that was checked; nothing was signalled", *args.PID)
	}
	if err != nil {
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

// signalVerifiedBackend signals the backend the identity check read, and no
// other. The check and the signal are separate round trips, so the PID can be
// handed to a new session in between. The statement repeats the guards and
// requires the start time the check saw, so a new session with the same PID
// matches no row and is left running. A reuse inside this one statement is
// still possible, because PostgreSQL names a backend only by its PID.
func signalVerifiedBackend(ctx context.Context, conn check.Conn, function string, pid int, started *time.Time) (bool, error) {
	var signalled bool
	err := conn.QueryRow(ctx, fmt.Sprintf(`
		SELECT %s(pid) FROM pg_stat_activity
		WHERE pid = $1
		  AND backend_start = $2
		  AND backend_type = 'client backend'
		  AND usename IS DISTINCT FROM current_user
		  AND pid <> pg_backend_pid()
		  AND NOT (COALESCE(application_name, '') = 'pglens'
		           OR starts_with(COALESCE(application_name, ''), 'pglens/')
		           OR starts_with(COALESCE(application_name, ''), 'pglens-agent/'))`, function), pid, started).Scan(&signalled)
	return signalled, err
}

// isPglensApplication reports whether an application_name belongs to a
// pglens session: "pglens" (the monitoring role's default) or "pglens/...".
func isPglensApplication(name string) bool {
	return name == "pglens" || strings.HasPrefix(name, "pglens/") || strings.HasPrefix(name, "pglens-agent/")
}
