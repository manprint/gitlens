package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

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
	queries     []string
}

func (c *signalConn) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (c *signalConn) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	c.queries = append(c.queries, sql)
	if strings.Contains(sql, "backend_type") {
		if c.noPID {
			return signalRow{err: pgx.ErrNoRows}
		}
		return signalRow{values: []any{c.backendType, c.appName, c.username, c.ownRole, c.ownBackend}}
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
