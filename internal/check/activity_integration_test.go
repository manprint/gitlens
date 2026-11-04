//go:build integration

package check

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/sessionorigin"

	"github.com/google/uuid"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

func TestINTACT001_ActivityDatabaseAndRatio(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		conn, err := pool.Acquire(context.Background())
		require.NoError(t, err)
		target := &registryTestTarget{conn: conn, version: pg.Version, database: "app", instanceID: uuid.New(), clusterID: 1, role: pgtype.RolePrimary, permTier: pgtype.TierReadOnly}
		result, err := (&activityCheck{}).Scrape(context.Background(), target)
		require.NoError(t, err)
		require.NotEmpty(t, result.Metrics)
		foundRatio := false
		for _, metric := range result.Metrics {
			if metric.Name == "pg_connections_used_ratio" {
				foundRatio = true
				require.GreaterOrEqual(t, metric.Value, 0.0)
				require.LessOrEqual(t, metric.Value, 1.0)
			}
		}
		require.True(t, foundRatio)
	})
}

func TestINTACT002_PreparedTransactionGaugesAreAlwaysPresent(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		conn, err := pool.Acquire(context.Background())
		require.NoError(t, err)
		target := &registryTestTarget{conn: conn, version: pg.Version, database: "app", instanceID: uuid.New(), clusterID: 1, role: pgtype.RolePrimary, permTier: pgtype.TierReadOnly}
		result, err := (&activityCheck{}).Scrape(context.Background(), target)
		require.NoError(t, err)
		require.NotEqual(t, -1.0, metricValue(result, "pg_prepared_xacts", nil))
		require.NotEqual(t, -1.0, metricValue(result, "pg_oldest_prepared_xact_seconds", nil))
	})
}

func TestINTACT004_ActivityMetricNamesAcrossVersions(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		conn, err := pool.Acquire(context.Background())
		require.NoError(t, err)
		target := &registryTestTarget{conn: conn, version: pg.Version, database: "app", instanceID: uuid.New(), clusterID: 1, role: pgtype.RolePrimary, permTier: pgtype.TierReadOnly}
		result, err := (&activityCheck{}).Scrape(context.Background(), target)
		require.NoError(t, err)
		for _, name := range []string{"pg_max_xact_age_seconds", "pg_max_idle_in_transaction_seconds", "pg_prepared_xacts", "pg_oldest_prepared_xact_seconds", "pg_max_datfrozenxid_age"} {
			found := false
			for _, m := range result.Metrics {
				if m.Name == name {
					found = true
				}
			}
			require.True(t, found, name)
		}
	})
}

// TestINTACT005_IdleInTransactionCountsAbortedSessions: a session whose
// transaction failed and that was left idle keeps the transaction open, so it
// is reported as 'idle in transaction (aborted)'. It is an idle-in-transaction
// session for the maximum that the advisor and the UI read.
func TestINTACT005_IdleInTransactionCountsAbortedSessions(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		aborted, err := pool.Acquire(ctx)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = aborted.Exec(context.Background(), "ROLLBACK")
			aborted.Release()
		})
		_, err = aborted.Exec(ctx, "BEGIN")
		require.NoError(t, err)
		_, err = aborted.Exec(ctx, "SELECT 1/0")
		require.Error(t, err)
		time.Sleep(1500 * time.Millisecond)

		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer conn.Release()
		target := &registryTestTarget{conn: conn, version: pg.Version, database: "app", instanceID: uuid.New(), clusterID: 1, role: pgtype.RolePrimary, permTier: pgtype.TierReadOnly}
		result, err := (&activityCheck{}).Scrape(ctx, target)
		require.NoError(t, err)
		require.GreaterOrEqual(t, metricValue(result, "pg_max_idle_in_transaction_seconds", nil), 1.0)
	})
}

// connectNamed opens a session as role with the given application_name.
func connectNamed(t *testing.T, pg *pgtest.PG, role pgtest.Role, application string) {
	t.Helper()
	cfg, err := pgx.ParseConfig(pg.DSN("app", role))
	require.NoError(t, err)
	cfg.RuntimeParams["application_name"] = application
	conn, err := pgx.ConnectConfig(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
}

// ACTCFG-INT-001 and SORIGIN-003 (PostgreSQL half): the origin metrics equal
// what pg_stat_activity holds for a controlled set of sessions, and the
// rendered predicate answers like IsPglensApplication for every name.
func TestACTCFGINT001_OriginMetricsMatchPgStatActivity(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		// The admin session exists before the first sample, so every sample sees it.
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		_, err := admin.Exec(ctx, "SELECT 1")
		require.NoError(t, err)
		// scrape runs the check on a pooled session, which the check releases;
		// pid is that session's, the one the check leaves out of its counts.
		scrape := func() (res Result, pid int32) {
			conn, err := pool.Acquire(ctx)
			require.NoError(t, err)
			pid = int32(conn.Conn().PgConn().PID())
			target := &registryTestTarget{conn: conn, version: pg.Version, database: "app", instanceID: uuid.New(), clusterID: 1, role: pgtype.RolePrimary, permTier: pgtype.TierReadOnly}
			res, err = newActivityCheck().Scrape(ctx, target)
			require.NoError(t, err)
			return res, pid
		}

		// The check's own role is "pglens": a session under it is a pglens
		// session whatever its application_name; the others only by name.
		for _, app := range []string{"api", "PGLENS", "pglens_x", ""} {
			connectNamed(t, pg, pgtest.RoleSuperuser, app)
		}
		connectNamed(t, pg, pgtest.RoleT1, "psql")
		for _, app := range []string{"pglens", "pglens/ash", "pglens-agent/x"} {
			connectNamed(t, pg, pgtest.RoleSuperuser, app)
		}
		connectNamed(t, pg, pgtest.RoleT0, "worker")

		// Sessions the harness leaves behind are not ours to count: the unique
		// application names and users of the controlled set are exact, the rest
		// is compared with pg_stat_activity below.
		after, _ := scrape()
		applications := metricsNamed(after, "pg_connections_by_application")
		for _, app := range []string{"api", "PGLENS", "pglens_x", "psql", "worker", "pglens", "pglens/ash", "pglens-agent/x"} {
			require.Equal(t, 1.0, applications[app], app)
		}
		users := metricsNamed(after, "pg_connections_by_user")
		require.Equal(t, 1.0, users["pglens_t1"])
		require.Equal(t, 1.0, users["pglens"])
		require.GreaterOrEqual(t, users["postgres"], 7.0)

		// Ground truth: pg_stat_activity classified by the Go function with the
		// role the check runs as, the sampling backend excluded.
		var truth map[string]float64
		require.Eventually(t, func() bool {
			res, pid := scrape()
			var currentUser string
			require.NoError(t, admin.QueryRow(ctx, `SELECT usename FROM pg_stat_activity WHERE pid = $1`, pid).Scan(&currentUser))
			rows, err := admin.Query(ctx, `SELECT COALESCE(application_name, ''), COALESCE(usename, '') FROM pg_stat_activity WHERE backend_type = 'client backend' AND pid <> $1`, pid)
			require.NoError(t, err)
			truth = map[string]float64{"application": 0, "pglens": 0}
			for rows.Next() {
				var app, user string
				require.NoError(t, rows.Scan(&app, &user))
				truth[sessionorigin.Origin(app, user, currentUser)]++
			}
			rows.Close()
			require.NoError(t, rows.Err())
			return fmt.Sprint(truth) == fmt.Sprint(metricsNamed(res, "pg_connections_by_origin"))
		}, 10*time.Second, 200*time.Millisecond, "the origin metrics equal pg_stat_activity classified by sessionorigin.Origin")
		require.Equal(t, 4.0, truth["pglens"], "the four pglens-named or pglens-role controlled sessions")
		require.GreaterOrEqual(t, truth["application"], 5.0)

		for _, name := range []string{"pglens", "pglens/check", "pglens/ash", "pglens/shared", "pglens/odoo", "pglens-agent/x", "pglens_x", "PGLENS", "", "psql", "pglens-agent"} {
			var inSQL, byUser bool
			require.NoError(t, admin.QueryRow(ctx, "SELECT "+sessionorigin.PredicateSQL("$1::text", "NULL::text")+", "+sessionorigin.PredicateSQL("$1::text", "current_user::text"), name).Scan(&inSQL, &byUser))
			require.Equal(t, sessionorigin.IsPglensApplication(name), inSQL, "application name %q", name)
			require.True(t, byUser, "the current user is a pglens session (%q)", name)
		}
	})
}
