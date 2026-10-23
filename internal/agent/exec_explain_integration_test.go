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

// TestINTCMD036_ExplainWithholdsCredentials: a plan shows the constants of its
// statement and of the views the planner expanded. A statement whose text has a
// sign of a credential is not explained, and a plan that has one is not
// shipped, even when the statement text itself shows none.
func TestINTCMD036_ExplainWithholdsCredentials(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		_, err := admin.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_stat_statements")
		require.NoError(t, err)

		view := fmt.Sprintf("pglens_remote_%d", pg.Major)
		_, err = admin.Exec(ctx, "CREATE OR REPLACE VIEW "+view+" AS SELECT 'postgresql://app:hunter2@db/app'::text AS dsn")
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP VIEW IF EXISTS "+view) })
		_, err = admin.Exec(ctx, "GRANT SELECT ON "+view+" TO pglens_t1")
		require.NoError(t, err)

		viewRead := "SELECT dsn FROM " + view
		_, err = admin.Exec(ctx, viewRead)
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "SELECT relname AS secret_name FROM pg_class WHERE relpages > 1000000")
		require.NoError(t, err)
		var viewQueryID, namedQueryID int64
		require.NoError(t, admin.QueryRow(ctx, "SELECT queryid FROM pg_stat_statements WHERE query = $1 LIMIT 1", viewRead).Scan(&viewQueryID))
		require.NoError(t, admin.QueryRow(ctx, "SELECT queryid FROM pg_stat_statements WHERE query LIKE 'SELECT relname AS secret_name FROM pg_class%' LIMIT 1").Scan(&namedQueryID))

		mgr, err := NewManager(ctx, "explain-credential-target", pg.DSN("postgres", pgtest.RoleT1), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()

		var rejected *RejectedError
		_, err = NewExplainExecutor().Execute(ctx, mgr, command.Args{QueryID: &viewQueryID, Datname: "postgres"})
		require.ErrorAs(t, err, &rejected, "the plan of a view holding a connection URI must not be shipped")
		require.Contains(t, err.Error(), "the plan for queryid")
		require.NotContains(t, err.Error(), "hunter2")

		_, err = NewExplainExecutor().Execute(ctx, mgr, command.Args{QueryID: &namedQueryID, Datname: "postgres"})
		require.ErrorAs(t, err, &rejected, "a statement whose text would be withheld must not be explained")
		require.Contains(t, err.Error(), "may hold a credential; pglens does not explain it")
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

// TestINTCMD014_ExplainAnalyzePlanHashIsStable: plan history keys each plan by
// its hash, so two ANALYZE runs of one statement must hash alike. The real
// output carries timings and buffer counts that differ on every run.
func TestINTCMD014_ExplainAnalyzePlanHashIsStable(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		_, err := admin.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_stat_statements")
		require.NoError(t, err)

		const statement = "SELECT count(*) FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace"
		ids := registerStatements(t, pg, statement)

		hashOf := func() string {
			result, err := explainAs(t, pg, pgtest.RoleT1, ids[statement], true)
			require.NoError(t, err)
			require.Contains(t, string(result), "Shared Hit Blocks")
			var decoded struct {
				PlanHash string `json:"plan_hash"`
			}
			require.NoError(t, json.Unmarshal(result, &decoded))
			require.NotEmpty(t, decoded.PlanHash)
			return decoded.PlanHash
		}
		first := hashOf()
		second := hashOf()
		require.Equal(t, first, second)
	})
}

// TestINTCMD026_ExplainEndsItsSession proves that an EXPLAIN does not return
// its session to the pool. Session state outlives ROLLBACK, so the next caller
// of the pool must get a new backend, not the one the command used.
func TestINTCMD026_ExplainEndsItsSession(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		_, err := admin.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_stat_statements")
		require.NoError(t, err)
		const statement = "SELECT current_database()"
		queryID := registerStatements(t, pg, statement)[statement]

		mgr, err := NewManager(ctx, "explain-session", pg.DSN("postgres", pgtest.RoleT1), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()
		backend := func() int {
			conn, err := mgr.ForDatabase(ctx, "postgres")
			require.NoError(t, err)
			defer conn.Release()
			var pid int
			require.NoError(t, conn.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid))
			return pid
		}
		before := backend()

		_, err = NewExplainExecutor().Execute(ctx, mgr, command.Args{QueryID: &queryID, Datname: "postgres"})
		require.NoError(t, err)
		require.NotEqual(t, before, backend(), "the session the EXPLAIN used went back to the pool")
	})
}
