//go:build integration

package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/command"
	"github.com/manprint/pglens/test/pgtest"
)

// indexCheckFixture creates a table of 100 000 rows without an index on
// customer_id and runs a parameterised lookup on it, so pg_stat_statements
// holds `... WHERE customer_id = $1`. It returns that statement's queryid.
func indexCheckFixture(t *testing.T, pg *pgtest.PG) int64 {
	t.Helper()
	ctx := context.Background()
	admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
	_, err := admin.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
CREATE EXTENSION IF NOT EXISTS hypopg;
DROP TABLE IF EXISTS idxv_orders;
CREATE TABLE idxv_orders (id bigint, customer_id int, total numeric);
INSERT INTO idxv_orders SELECT g, g % 10000, g FROM generate_series(1, 100000) g;
ANALYZE idxv_orders`)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP TABLE IF EXISTS idxv_orders") })

	app, err := pgx.Connect(ctx, pg.DSN("app", pgtest.RoleSuperuser))
	require.NoError(t, err)
	_, err = app.Exec(ctx, "SELECT id, total FROM idxv_orders WHERE customer_id = $1", 42)
	require.NoError(t, err)
	require.NoError(t, app.Close(ctx))
	var queryID int64
	require.NoError(t, admin.QueryRow(ctx, "SELECT queryid FROM public.pg_stat_statements WHERE query = 'SELECT id, total FROM idxv_orders WHERE customer_id = $1' LIMIT 1").Scan(&queryID))
	return queryID
}

func indexCheckArgsFor(queryID int64) command.Args {
	return command.Args{QueryID: &queryID, Datname: "app", IndexSchema: "public", IndexRelation: "idxv_orders", IndexColumns: []string{"customer_id"}, IndexInclude: []string{"total"}}
}

// TestINTIDXV001: the planner would use an index on the filtered column.
func TestINTIDXV001(t *testing.T) {
	pg := pgtest.PGExt(t)
	pg.Lock(t)
	ctx := context.Background()
	queryID := indexCheckFixture(t, pg)
	mgr, err := NewManager(ctx, "idxv-target", pg.DSN("app", pgtest.RoleT1), DefaultConnOptions(), clock.System())
	require.NoError(t, err)
	defer mgr.Close()

	raw, err := NewIndexCheckExecutor().Execute(ctx, mgr, indexCheckArgsFor(queryID))
	require.NoError(t, err)
	var got indexCheckResult
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Equal(t, "app", got.Datname)
	require.NotEmpty(t, got.IndexName)
	require.True(t, got.IndexUsed, "plan after: %s", got.PlanAfter)
	require.Less(t, got.CostAfter, got.CostBefore)
	require.Positive(t, got.IndexSizeBytes)

	// A column the table lacks is refused before any index is made.
	args := indexCheckArgsFor(queryID)
	args.IndexColumns = []string{"nope"}
	_, err = NewIndexCheckExecutor().Execute(ctx, mgr, args)
	require.ErrorContains(t, err, "column_not_found: ")
	args = indexCheckArgsFor(queryID)
	args.IndexRelation = "idxv_missing"
	_, err = NewIndexCheckExecutor().Execute(ctx, mgr, args)
	require.ErrorContains(t, err, "relation_not_found: ")
}

// TestINTIDXV002: nothing outlives the command, neither a hypothetical index
// in any session of the agent nor a real index.
func TestINTIDXV002(t *testing.T) {
	pg := pgtest.PGExt(t)
	pg.Lock(t)
	ctx := context.Background()
	queryID := indexCheckFixture(t, pg)
	admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
	indexes := func() int {
		var n int
		require.NoError(t, admin.QueryRow(ctx, "SELECT count(*) FROM pg_indexes WHERE tablename = 'idxv_orders'").Scan(&n))
		return n
	}
	before := indexes()
	mgr, err := NewManager(ctx, "idxv-cleanup", pg.DSN("app", pgtest.RoleT1), DefaultConnOptions(), clock.System())
	require.NoError(t, err)
	defer mgr.Close()

	for i := 0; i < 3; i++ {
		_, err = NewIndexCheckExecutor().Execute(ctx, mgr, indexCheckArgsFor(queryID))
		require.NoError(t, err)
	}
	// Every pooled connection the agent hands out sees no hypothetical index:
	// hold connections until the database's pool has no more to give.
	conns := make([]interface{ Release() }, 0, 4)
	for {
		acquire, cancel := context.WithTimeout(ctx, 2*time.Second)
		conn, err := mgr.ForDatabase(acquire, "app")
		cancel()
		if err != nil {
			break
		}
		var n int
		require.NoError(t, conn.QueryRow(ctx, "SELECT count(*) FROM public.hypopg()").Scan(&n))
		require.Zero(t, n)
		conns = append(conns, conn)
	}
	require.NotEmpty(t, conns)
	for _, conn := range conns {
		conn.Release()
	}
	fresh, err := pgx.Connect(ctx, pg.DSN("app", pgtest.RoleT1))
	require.NoError(t, err)
	defer func() { _ = fresh.Close(ctx) }()
	var n int
	require.NoError(t, fresh.QueryRow(ctx, "SELECT count(*) FROM public.hypopg()").Scan(&n))
	require.Zero(t, n)
	require.Equal(t, before, indexes())
}

// TestINTIDXV003: without hypopg the command is refused with hypopg_missing.
func TestINTIDXV003(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		_, err := admin.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_stat_statements")
		require.NoError(t, err)
		mgr, err := NewManager(ctx, "idxv-missing", pg.DSN("app", pgtest.RoleT1), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()
		queryID := int64(1)
		_, err = NewIndexCheckExecutor().Execute(ctx, mgr, indexCheckArgsFor(queryID))
		var rejected *RejectedError
		require.ErrorAs(t, err, &rejected)
		require.ErrorContains(t, err, `hypopg_missing: the hypopg extension is not installed in database "app"`)
	})
}
