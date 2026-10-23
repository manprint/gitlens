package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/command"
)

type signalConn struct {
	backendType string
	appName     string
	username    string
	ownRole     bool
	ownBackend  bool
	noPID       bool
	signalled   bool
	// checkedStart is the backend start the identity check reads, and
	// currentStart is the start of the session that holds the PID when the
	// signal runs. They differ when the PID was reused in between.
	checkedStart time.Time
	currentStart time.Time
	queries      []string
}

func (c *signalConn) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (c *signalConn) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	c.queries = append(c.queries, sql)
	if strings.Contains(sql, "backend_start = $2") {
		// The signal reaches only the session that started when it was checked.
		started, _ := args[1].(*time.Time)
		if started == nil || !started.Equal(c.currentStart) {
			return signalRow{err: pgx.ErrNoRows}
		}
		return signalRow{values: []any{c.signalled}}
	}
	if strings.Contains(sql, "backend_type") {
		if c.noPID {
			return signalRow{err: pgx.ErrNoRows}
		}
		checked := c.checkedStart
		return signalRow{values: []any{c.backendType, c.appName, c.username, c.ownRole, c.ownBackend, &checked}}
	}
	return signalRow{values: []any{c.signalled}}
}
func (c *signalConn) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (c *signalConn) Release() {}

type signalRow struct {
	values []any
	err    error
}

func (r signalRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, value := range r.values {
		switch dst := dest[i].(type) {
		case *string:
			*dst = value.(string)
		case *bool:
			*dst = value.(bool)
		case **time.Time:
			*dst = value.(*time.Time)
		default:
			panic("unsupported signal scan destination")
		}
	}
	return nil
}

func TestSignal_RejectsUnknownPID(t *testing.T) {
	conn := &signalConn{noPID: true}
	_, err := NewCancelExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, command.Args{PID: intPtr(404)})
	var rejected *RejectedError
	require.True(t, errors.As(err, &rejected))
	require.Contains(t, rejected.Error(), "not present")
}

func TestSignal_RejectsNonClientBackend(t *testing.T) {
	conn := &signalConn{backendType: "autovacuum worker"}
	_, err := NewCancelExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, command.Args{PID: intPtr(42)})
	var rejected *RejectedError
	require.True(t, errors.As(err, &rejected))
	require.Contains(t, rejected.Error(), "client backend")
	require.Len(t, conn.queries, 1)
}

// The agent names its sessions pglens/shared, pglens/ash and
// pglens/<datname>, and the monitoring role defaults to "pglens". The old
// self-protection looked for "pglens-agent/" — a name nothing ever used — so
// this fixture now uses the names the agent really sets.
func TestSignal_RefusesOwnBackends(t *testing.T) {
	for _, appName := range []string{"pglens/shared", "pglens/ash", "pglens/app", "pglens", "pglens-agent/shared"} {
		conn := &signalConn{backendType: "client backend", appName: appName}
		_, err := NewTerminateExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, command.Args{PID: intPtr(42)})
		var rejected *RejectedError
		require.True(t, errors.As(err, &rejected), appName)
		require.Contains(t, rejected.Error(), "pglens backend", appName)
		require.Len(t, conn.queries, 1, appName)
	}
}

// A PID that the operating system hands to a new session between the identity
// check and the signal must not be signalled: the signal names the start time
// the check read, and the new session started later.
func TestSignal_LeavesAPIDReusedAfterTheCheckAlone(t *testing.T) {
	checked := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	conn := &signalConn{backendType: "client backend", appName: "psql", username: "alice", checkedStart: checked, currentStart: checked.Add(time.Minute), signalled: true}
	_, err := NewTerminateExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, command.Args{PID: intPtr(42)})
	var rejected *RejectedError
	require.True(t, errors.As(err, &rejected))
	require.Contains(t, rejected.Error(), "no longer the session that was checked")
	require.Len(t, conn.queries, 2)
}

func TestSignal_RefusesMonitoringRoleAndOwnPID(t *testing.T) {
	for name, conn := range map[string]*signalConn{
		"same role":   {backendType: "client backend", appName: "psql", username: "pglens", ownRole: true},
		"own backend": {backendType: "client backend", appName: "psql", ownBackend: true},
	} {
		_, err := NewTerminateExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, command.Args{PID: intPtr(42)})
		var rejected *RejectedError
		require.True(t, errors.As(err, &rejected), name)
		require.Len(t, conn.queries, 1, name)
	}
}

func TestSignal_IdentityQueryToleratesNulls(t *testing.T) {
	conn := &signalConn{backendType: "client backend", appName: "application-x", signalled: true}
	_, err := NewCancelExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, command.Args{PID: intPtr(42)})
	require.NoError(t, err)
	require.Contains(t, conn.queries[0], "COALESCE(usename, '')")
	require.Contains(t, conn.queries[0], "COALESCE(backend_type, '')")
}

func TestSignal_ResultCarriesTargetIdentity(t *testing.T) {
	conn := &signalConn{backendType: "client backend", appName: "application-x", username: "alice", signalled: true}
	result, err := NewCancelExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, command.Args{PID: intPtr(42)})
	require.NoError(t, err)
	var decoded struct {
		PID             int    `json:"pid"`
		Signalled       bool   `json:"signalled"`
		ApplicationName string `json:"application_name"`
		Username        string `json:"usename"`
	}
	require.NoError(t, json.Unmarshal(result, &decoded))
	require.Equal(t, 42, decoded.PID)
	require.True(t, decoded.Signalled)
	require.Equal(t, "application-x", decoded.ApplicationName)
	require.Equal(t, "alice", decoded.Username)
	require.Contains(t, conn.queries[1], "pg_cancel_backend")
}
