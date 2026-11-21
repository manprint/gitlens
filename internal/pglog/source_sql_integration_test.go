//go:build integration

package pglog_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/pglog"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

type connQuerier struct{ c *pgx.Conn }

func (q connQuerier) QueryRow(ctx context.Context, sql string, args ...any) pglog.Row {
	return q.c.QueryRow(ctx, sql, args...)
}

func connect(t *testing.T, dsn string) *pgx.Conn {
	t.Helper()
	c, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

// INT-PGL-001: on PostgreSQL 18 with jsonlog the SQL source of a superuser
// reads what the server wrote; a pg_monitor role without the grant on
// pg_read_binary_file gets ErrLogReadDenied.
func TestINTPGL001_SQLSource(t *testing.T) {
	pg := pgtest.StartLogging(t, 18, pglog.FormatJSON)
	ctx := t.Context()
	super := connect(t, pg.DSN("app", pgtest.RoleSuperuser))
	src := pglog.NewSQLSource(connQuerier{super}, pglog.FormatJSON)

	_, next, _, err := pglog.Advance(ctx, src, pglog.Position{}, true, 1<<20)
	require.NoError(t, err)
	_, err = super.Exec(ctx, "SELECT * FROM int_pgl_001_missing")
	require.Error(t, err)

	var found pglog.Entry
	require.Eventually(t, func() bool {
		chunks, n, _, err := pglog.Advance(ctx, src, next, false, 1<<20)
		if err != nil {
			return false
		}
		for _, c := range chunks {
			recs, consumed := pglog.Split(pglog.FormatJSON, c.Data, pglog.MaxLineBytes)
			next = pglog.Settle(n, c, consumed)
			for _, line := range recs.JSON {
				e, err := pglog.ParseJSONLine(line, time.UTC)
				if err == nil && e.SQLState == "42P01" {
					found = e
					return true
				}
			}
		}
		if len(chunks) == 0 {
			next = n
		}
		return false
	}, 10*time.Second, 200*time.Millisecond)
	require.Equal(t, "ERROR", found.Severity)
	require.Contains(t, found.Message, "int_pgl_001_missing")

	monitor := connect(t, pg.DSN("app", pgtest.RoleT0))
	msrc := pglog.NewSQLSource(connQuerier{monitor}, pglog.FormatJSON)
	name, size, err := msrc.Current(ctx)
	require.NoError(t, err, "pg_monitor lists the log directory")
	require.Positive(t, size)
	_, err = msrc.ReadAt(ctx, name, 0, 16)
	require.ErrorIs(t, err, pglog.ErrLogReadDenied)

	var user string
	require.NoError(t, monitor.QueryRow(ctx, "SELECT current_user").Scan(&user))
	_, err = super.Exec(ctx, "GRANT EXECUTE ON FUNCTION pg_read_binary_file(text,bigint,bigint,boolean) TO "+pgx.Identifier{user}.Sanitize())
	require.NoError(t, err)
	data, err := msrc.ReadAt(ctx, name, 0, 16)
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix(data, []byte("{")))
}
