//go:build integration

package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/command"
	"github.com/manprint/pglens/test/pgtest"
)

const workbookLookup = "SELECT v FROM wbk_items WHERE id = $1"

// workbookFixture creates an indexed table and runs a lookup by key, so
// pg_stat_statements holds workbookLookup. It returns the statement's queryid.
func workbookFixture(t *testing.T, pg *pgtest.PG) int64 {
	t.Helper()
	ctx := context.Background()
	admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
	// Earlier tests leave entries of the same text: on PG < 18 the queryid
	// hashes the table OID, so this database's entries start empty.
	_, err := admin.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
		SELECT public.pg_stat_statements_reset(0, (SELECT oid FROM pg_database WHERE datname = current_database()), 0);
		DROP TABLE IF EXISTS wbk_items;
		CREATE TABLE wbk_items (id int PRIMARY KEY, v int);
		INSERT INTO wbk_items SELECT g, g % 100 FROM generate_series(1, 50000) g;
		ANALYZE wbk_items`)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP TABLE IF EXISTS wbk_items") })
	app, err := pgx.Connect(ctx, pg.DSN("app", pgtest.RoleSuperuser))
	require.NoError(t, err)
	_, err = app.Exec(ctx, workbookLookup, 42)
	require.NoError(t, err)
	require.NoError(t, app.Close(ctx))
	var queryID int64
	require.NoError(t, admin.QueryRow(ctx, "SELECT queryid FROM public.pg_stat_statements WHERE query = $1 AND dbid = (SELECT oid FROM pg_database WHERE datname = current_database()) LIMIT 1", workbookLookup).Scan(&queryID))
	return queryID
}

func workbookManager(t *testing.T, pg *pgtest.PG) *Manager {
	t.Helper()
	mgr, err := NewManager(context.Background(), "wbk-target", pg.DSN("app", pgtest.RoleT1), DefaultConnOptions(), clock.System())
	require.NoError(t, err)
	t.Cleanup(mgr.Close)
	return mgr
}

func rootNodeType(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var decoded struct {
		Plan []struct {
			Plan map[string]any `json:"Plan"`
		} `json:"plan"`
	}
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.NotEmpty(t, decoded.Plan)
	return decoded.Plan[0].Plan["Node Type"].(string)
}

var indexScansOff = map[string]string{"enable_indexscan": "off", "enable_bitmapscan": "off", "enable_indexonlyscan": "off"}

// TestINTWBK001: the variant's settings change the plan: with the index scans
// off, the lookup by primary key becomes a Seq Scan.
func TestINTWBK001_SettingsChangeThePlan(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		queryID := workbookFixture(t, pg)
		mgr := workbookManager(t, pg)
		params := []*string{strPtr("42")}

		base, err := NewExplainExecutor().Execute(ctx, mgr, command.Args{QueryID: &queryID, Datname: "app", Params: params})
		require.NoError(t, err)
		require.Equal(t, "Index Scan", rootNodeType(t, base))

		variant, err := NewExplainExecutor().Execute(ctx, mgr, command.Args{QueryID: &queryID, Datname: "app", Params: params, Settings: indexScansOff})
		require.NoError(t, err)
		require.Equal(t, "Seq Scan", rootNodeType(t, variant))
		var echo struct {
			Settings map[string]string `json:"settings"`
		}
		require.NoError(t, json.Unmarshal(variant, &echo))
		require.Equal(t, indexScansOff, echo.Settings)

		// work_mem goes through set_config with its kB unit.
		_, err = NewExplainExecutor().Execute(ctx, mgr, command.Args{QueryID: &queryID, Datname: "app", Params: params, Settings: map[string]string{"work_mem": "65536"}})
		require.NoError(t, err)
	})
}

// TestINTWBK002: nothing outlives the command; a later statement on an agent
// connection sees the server's own setting.
func TestINTWBK002_SettingsDoNotOutliveTheCommand(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		queryID := workbookFixture(t, pg)
		mgr := workbookManager(t, pg)
		for i := 0; i < 3; i++ {
			_, err := NewExplainExecutor().Execute(ctx, mgr, command.Args{QueryID: &queryID, Datname: "app", Params: []*string{strPtr("7")}, Settings: indexScansOff})
			require.NoError(t, err)
		}
		for _, db := range []string{"app", "postgres"} {
			conn, err := mgr.ForDatabase(ctx, db)
			require.NoError(t, err)
			var value string
			require.NoError(t, conn.QueryRow(ctx, "SHOW enable_indexscan").Scan(&value))
			conn.Release()
			require.Equal(t, "on", value, db)
		}
	})
}

// TestINTWBK003: hints reach pg_hint_plan on both explain paths where the
// server preloads it, and are refused where it does not.
func TestINTWBK003_HintsNeedPreloadedHintPlan(t *testing.T) {
	t.Run("pgext", func(t *testing.T) {
		pg := pgtest.PGExt(t)
		pg.Lock(t)
		ctx := context.Background()
		queryID := workbookFixture(t, pg)
		mgr := workbookManager(t, pg)
		hints := "SeqScan(wbk_items)"

		bound, err := NewExplainExecutor().Execute(ctx, mgr, command.Args{QueryID: &queryID, Datname: "app", Params: []*string{strPtr("42")}})
		require.NoError(t, err)
		require.Equal(t, "Index Scan", rootNodeType(t, bound))

		bound, err = NewExplainExecutor().Execute(ctx, mgr, command.Args{QueryID: &queryID, Datname: "app", Params: []*string{strPtr("42")}, Hints: hints})
		require.NoError(t, err)
		require.Equal(t, "Seq Scan", rootNodeType(t, bound), "bound path")

		// Without values, PostgreSQL 18 plans the statement verbatim with
		// GENERIC_PLAN over the simple protocol.
		verbatim, err := NewExplainExecutor().Execute(ctx, mgr, command.Args{QueryID: &queryID, Datname: "app", Hints: hints})
		require.NoError(t, err)
		require.Equal(t, "Seq Scan", rootNodeType(t, verbatim), "verbatim path")
		var echo struct {
			Hints      string `json:"hints"`
			ValuesFrom string `json:"values_from"`
		}
		require.NoError(t, json.Unmarshal(verbatim, &echo))
		require.Equal(t, hints, echo.Hints)
		require.Equal(t, "generic", echo.ValuesFrom)
	})
	// The stock images do not preload pg_hint_plan (postgres:18.6-alpine and
	// every other version under test).
	t.Run("stock", func(t *testing.T) {
		pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
			pg.Lock(t)
			queryID := workbookFixture(t, pg)
			mgr := workbookManager(t, pg)
			_, err := NewExplainExecutor().Execute(context.Background(), mgr, command.Args{QueryID: &queryID, Datname: "app", Params: []*string{strPtr("42")}, Hints: "SeqScan(wbk_items)"})
			require.ErrorContains(t, err, "hint_plan_inactive")
		})
	})
}
