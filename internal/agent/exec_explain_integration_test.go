//go:build integration

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/command"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

func TestINTCMD006_ExplainKnownQueryIDReturnsPlan(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		_, err := admin.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_stat_statements")
		require.NoError(t, err)
		mgr, err := NewManager(ctx, "explain-target", pg.DSN("postgres", pgtest.RoleT1), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()

		conn, err := mgr.ForDatabase(ctx, "postgres")
		require.NoError(t, err)
		defer conn.Release()
		_, err = conn.Exec(ctx, "SELECT current_database()")
		require.NoError(t, err)
		var queryID int64
		require.NoError(t, conn.QueryRow(ctx, "SELECT queryid FROM public.pg_stat_statements WHERE query = 'SELECT current_database()' ORDER BY calls DESC LIMIT 1").Scan(&queryID))

		result, err := NewExplainExecutor().Execute(ctx, mgr, command.Args{QueryID: &queryID, Datname: "postgres"})
		require.NoError(t, err)
		var decoded struct {
			Plan []struct {
				Plan map[string]any `json:"Plan"`
			} `json:"plan"`
			PlanHash string `json:"plan_hash"`
		}
		require.NoError(t, json.Unmarshal(result, &decoded))
		require.NotEmpty(t, decoded.PlanHash)
		require.NotEmpty(t, decoded.Plan)
		require.Equal(t, "Result", decoded.Plan[0].Plan["Node Type"])
	})
}

// TestINTCMD007_ExplainAnalyzeNeverWrites proves the monitored database is
// never modified through EXPLAIN: ANALYZE of a writing statement is refused
// before anything runs — even when the monitoring role holds the privilege —
// while plan-only EXPLAIN of the same statement still returns a plan.
func TestINTCMD007_ExplainAnalyzeNeverWrites(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		_, err := admin.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_stat_statements")
		require.NoError(t, err)

		table := fmt.Sprintf("pglens_explain_%d", pg.Major)
		_, err = admin.Exec(ctx, "DROP TABLE IF EXISTS "+table)
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "CREATE TABLE "+table+" (value int)")
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP TABLE IF EXISTS "+table) })
		_, err = admin.Exec(ctx, "INSERT INTO "+table+" VALUES (1)")
		require.NoError(t, err)
		// The privilege is granted on purpose: the refusal must come from the
		// read-only guarantee, not from a missing grant.
		_, err = admin.Exec(ctx, "GRANT UPDATE ON "+table+" TO pglens_t1")
		require.NoError(t, err)
		update := "UPDATE " + table + " SET value = value + value"
		_, err = admin.Exec(ctx, update)
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "UPDATE "+table+" SET value = 1")
		require.NoError(t, err)
		var queryID int64
		require.NoError(t, admin.QueryRow(ctx, "SELECT queryid FROM pg_stat_statements WHERE query = $1 LIMIT 1", update).Scan(&queryID))

		mgr, err := NewManager(ctx, "explain-update-target", pg.DSN("postgres", pgtest.RoleT1), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()

		_, err = NewExplainExecutor().Execute(ctx, mgr, command.Args{QueryID: &queryID, Datname: "postgres", Analyze: true})
		var rejected *RejectedError
		require.ErrorAs(t, err, &rejected)

		result, err := NewExplainExecutor().Execute(ctx, mgr, command.Args{QueryID: &queryID, Datname: "postgres"})
		require.NoError(t, err)
		require.NotEmpty(t, result)

		var value int
		require.NoError(t, admin.QueryRow(ctx, "SELECT value FROM "+table).Scan(&value))
		require.Equal(t, 1, value)
	})
}

// TestINTCMD008_ExplainParameterisedStatement covers the normal case: almost
// every pg_stat_statements entry has $n placeholders, which only
// EXPLAIN (GENERIC_PLAN) can plan (PostgreSQL 16+).
func TestINTCMD008_ExplainParameterisedStatement(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		_, err := admin.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_stat_statements")
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "SELECT relname FROM pg_class WHERE relpages > 7 AND relname <> 'pglens'")
		require.NoError(t, err)
		var queryID int64
		require.NoError(t, admin.QueryRow(ctx, `SELECT queryid FROM pg_stat_statements
			WHERE query LIKE 'SELECT relname FROM pg_class WHERE relpages > $1%' LIMIT 1`).Scan(&queryID))

		mgr, err := NewManager(ctx, "explain-generic-target", pg.DSN("postgres", pgtest.RoleT1), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()

		result, err := NewExplainExecutor().Execute(ctx, mgr, command.Args{QueryID: &queryID, Datname: "postgres"})
		if pg.Major < 16 {
			var rejected *RejectedError
			require.ErrorAs(t, err, &rejected)
			return
		}
		require.NoError(t, err)
		require.NotEmpty(t, result)
	})
}
