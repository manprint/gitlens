//go:build integration

package check

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/manprint/pglens/internal/cardinality"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

// INT-FUN-001: at T0 function_stats reads the call counters of tracked
// functions, overloads apart, and reports the track_functions value its
// own session runs with; function_security lists the SECURITY DEFINER
// functions with the search_path they pin and whether PUBLIC may run them.
// Neither waits for a function another session is altering.
func TestINTFUN001_Functions(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		schema := fmt.Sprintf("pglens_fun_%d", testSuffix())
		t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE") })
		_, err := admin.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %[1]s;
CREATE FUNCTION %[1]s.f(x integer) RETURNS integer LANGUAGE plpgsql AS 'BEGIN RETURN x; END';
CREATE FUNCTION %[1]s.f(x text) RETURNS text LANGUAGE plpgsql AS 'BEGIN RETURN x; END';
CREATE FUNCTION %[1]s.exposed() RETURNS integer LANGUAGE plpgsql SECURITY DEFINER AS 'BEGIN RETURN 1; END';
CREATE FUNCTION %[1]s.pinned() RETURNS integer LANGUAGE sql SECURITY DEFINER SET search_path = %[1]s, pg_temp AS 'SELECT 1';
REVOKE EXECUTE ON FUNCTION %[1]s.pinned() FROM PUBLIC;
CREATE PROCEDURE %[1]s.job() LANGUAGE sql SECURITY DEFINER SET search_path = %[1]s AS 'SELECT 1'`, schema))
		require.NoError(t, err)

		// Tracking is a superuser setting: one admin session turns it on
		// for itself only, calls the functions and flushes its counters.
		caller, err := admin.Acquire(ctx)
		require.NoError(t, err)
		_, err = caller.Exec(ctx, fmt.Sprintf(`SET track_functions = 'pl';
SELECT %[1]s.f(g) FROM generate_series(1, 3) g;
SELECT %[1]s.f(g::text) FROM generate_series(1, 2) g;
SELECT pg_stat_force_next_flush()`, schema))
		require.NoError(t, err)
		_, err = caller.Exec(ctx, "SELECT 1")
		require.NoError(t, err)
		caller.Release()

		// Another session alters a reported function and keeps its
		// transaction open.
		holder, err := admin.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = holder.Rollback(context.Background()) }()
		_, err = holder.Exec(ctx, fmt.Sprintf("ALTER FUNCTION %s.exposed() SET search_path = pg_catalog", schema))
		require.NoError(t, err)

		stats := &functionStatsCheck{selectors: newScopedSelectors(cardinality.Options{TopN: 50})}
		var calls map[string]float64
		require.Eventually(t, func() bool {
			conn, err := pool.Acquire(ctx)
			require.NoError(t, err)
			result, err := stats.Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
			require.NoError(t, err)
			require.Len(t, result.Facts, 1)
			// The monitoring role's own session does not track.
			require.Equal(t, "none", result.Facts[0].ValueText)
			calls = map[string]float64{}
			for _, m := range result.Metrics {
				if m.Name == "pg_function_calls_total" && m.Labels["schemaname"] == schema {
					require.Equal(t, "plpgsql", m.Labels["language"])
					calls[m.Labels["function"]+"("+m.Labels["arguments"]+")"] = m.Value
				}
			}
			return len(calls) == 2
		}, 10*time.Second, 200*time.Millisecond)
		require.Equal(t, map[string]float64{"f(x integer)": 3, "f(x text)": 2}, calls)

		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		started := time.Now()
		result, err := (&functionSecurityCheck{}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, err)
		require.Less(t, time.Since(started), 900*time.Millisecond, "the check waited for a lock")
		require.False(t, result.Truncated)
		facts := map[string]Fact{}
		for _, f := range result.Facts {
			if f.Labels["schemaname"] == schema {
				facts[f.Labels["name"]] = f
			}
		}
		require.ElementsMatch(t, []string{"exposed", "pinned", "job"}, keysOf(facts))

		// The uncommitted ALTER is not seen.
		exposed := facts["exposed"]
		require.Equal(t, "unpinned", exposed.ValueText)
		require.Equal(t, schema+".exposed()", exposed.Key)
		require.Equal(t, "true", exposed.Labels["owner_superuser"])
		require.Equal(t, "true", exposed.Labels["public_execute"])
		require.Empty(t, exposed.Labels["search_path"])

		pinned := facts["pinned"]
		require.Equal(t, "pinned", pinned.ValueText)
		require.Equal(t, schema+", pg_temp", pinned.Labels["search_path"])
		require.Equal(t, "true", pinned.Labels["pg_temp_last"])
		require.Equal(t, "false", pinned.Labels["public_execute"])

		job := facts["job"]
		require.Equal(t, "procedure", job.Labels["kind"])
		require.Equal(t, "false", job.Labels["pg_temp_last"])
	})
}
