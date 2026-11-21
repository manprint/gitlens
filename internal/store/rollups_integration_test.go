//go:build integration

package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

var rollupViews = []string{"metrics_1h", "metrics_ash_1h", "metrics_statements_1h"}

// INT-RLP-001: the three rollups exist with one refresh and one retention
// policy each, and a second Migrate changes nothing.
func TestINTRLP001_RollupsAndPolicies(t *testing.T) {
	ctx, _, pool := newMigrationTestPool(t)
	require.NoError(t, Migrate(ctx, pool))
	require.NoError(t, Migrate(ctx, pool))

	rows, err := pool.Query(ctx, `SELECT view_name::text FROM timescaledb_information.continuous_aggregates ORDER BY view_name`)
	require.NoError(t, err)
	var views []string
	for rows.Next() {
		var v string
		require.NoError(t, rows.Scan(&v))
		views = append(views, v)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, rollupViews, views)

	for _, view := range rollupViews {
		for _, proc := range []string{"policy_refresh_continuous_aggregate", "policy_retention"} {
			var n int
			require.NoError(t, pool.QueryRow(ctx, `
SELECT count(*) FROM timescaledb_information.jobs j
JOIN timescaledb_information.continuous_aggregates c
  ON (j.hypertable_schema, j.hypertable_name) IN
     ((c.view_schema, c.view_name), (c.materialization_hypertable_schema, c.materialization_hypertable_name))
WHERE c.view_name = $1 AND j.proc_name = $2`, view, proc).Scan(&n))
			require.Equal(t, 1, n, "%s %s", view, proc)
		}
	}
}

// refreshRollup refreshes a rollup, waiting out the refresh job its policy
// starts right after the migration (SQLSTATE 55P03).
func refreshRollup(t *testing.T, pool *pgxpool.Pool, view string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		_, err := pool.Exec(context.Background(), `CALL refresh_continuous_aggregate($1::regclass, NULL, now())`, view)
		var pgErr *pgconn.PgError
		if err == nil || !errors.As(err, &pgErr) || pgErr.Code != "55P03" || time.Now().After(deadline) {
			require.NoError(t, err)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func rollupHours() (time.Time, []time.Time) {
	start := time.Now().UTC().Truncate(time.Hour).Add(-5 * time.Hour)
	var ts []time.Time
	for at := start; at.Before(start.Add(3 * time.Hour)); at = at.Add(time.Minute) {
		ts = append(ts, at)
	}
	return start, ts
}

// INT-RLP-002: three hours of a constant rate 6 give hourly buckets of 6.
func TestINTRLP002_StatementsRollupAverages(t *testing.T) {
	ctx, _, pool := newMigrationTestPool(t)
	require.NoError(t, Migrate(ctx, pool))
	inst := uuid.New()
	start, stamps := rollupHours()
	for _, ts := range stamps {
		_, err := pool.Exec(ctx, `INSERT INTO metrics_statements (ts, cluster_id, instance_id, datname, queryid, calls_rate, sample_seconds)
VALUES ($1, 1, $2, 'app', 42, 6, 60)`, ts, inst)
		require.NoError(t, err)
	}
	refreshRollup(t, pool, "metrics_statements_1h")

	rows, err := pool.Query(ctx, `SELECT bucket, calls_rate, sample_seconds, samples FROM metrics_statements_1h WHERE instance_id = $1 ORDER BY bucket`, inst)
	require.NoError(t, err)
	var buckets []time.Time
	for rows.Next() {
		var bucket time.Time
		var rate, seconds float64
		var samples int64
		require.NoError(t, rows.Scan(&bucket, &rate, &seconds, &samples))
		require.InDelta(t, 6, rate, 1e-9)
		require.InDelta(t, 3600, seconds, 1e-9)
		require.EqualValues(t, 60, samples)
		buckets = append(buckets, bucket.UTC())
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []time.Time{start, start.Add(time.Hour), start.Add(2 * time.Hour)}, buckets)
}

// INT-RLP-003: the ASH rollup keeps the raw sum of samples.
func TestINTRLP003_ASHRollupSums(t *testing.T) {
	ctx, _, pool := newMigrationTestPool(t)
	require.NoError(t, Migrate(ctx, pool))
	inst := uuid.New()
	_, stamps := rollupHours()
	var rows []ASHRow
	for i, ts := range stamps {
		row := ashDimRow(inst, ts, []string{"a", "b"}[i%2])
		row.Samples = 1 + i%4
		rows = append(rows, row)
	}
	writeASHTx(t, ctx, pool, rows)
	refreshRollup(t, pool, "metrics_ash_1h")

	var raw, rolled, rawTicks, rolledTicks int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT sum(samples), sum(window_ticks) FROM metrics_ash WHERE instance_id = $1`, inst).Scan(&raw, &rawTicks))
	require.NoError(t, pool.QueryRow(ctx, `SELECT sum(samples), sum(window_ticks) FROM metrics_ash_1h WHERE instance_id = $1`, inst).Scan(&rolled, &rolledTicks))
	require.Equal(t, raw, rolled)
	require.Equal(t, rawTicks, rolledTicks)
	require.Positive(t, rolled)
}
