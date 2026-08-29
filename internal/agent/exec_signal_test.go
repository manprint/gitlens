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
		return signalRow{values: []any{c.backendType, c.appName, c.username}}
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

func TestSignal_RefusesOwnBackends(t *testing.T) {
	conn := &signalConn{backendType: "client backend", appName: "pglens-agent/shared"}
	_, err := NewTerminateExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, command.Args{PID: intPtr(42)})
	var rejected *RejectedError
	require.True(t, errors.As(err, &rejected))
	require.Contains(t, rejected.Error(), "pglens-agent")
	require.Len(t, conn.queries, 1)
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
