//go:build integration

package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func writeASHTx(t *testing.T, ctx context.Context, pool *pgxpool.Pool, rows []ASHRow) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, WriteASH(ctx, tx, rows))
	require.NoError(t, tx.Commit(ctx))
}

func ashDimRow(inst uuid.UUID, ts time.Time, user string) ASHRow {
	return ASHRow{TS: ts, TenantID: "default", ClusterID: 1, InstanceID: inst, Datname: "app",
		WaitEventType: "CPU", WaitEvent: "CPU", State: "active", Samples: 3, WindowSeconds: 10, WindowTicks: 10,
		Usename: user, ClientAddr: "10.0.0.7", ApplicationName: "shop"}
}

// INT-ASH-D-002: the three dimensions are part of the row identity, a repeated
// row is dropped, and both writer paths (batch and copy) store the values.
func TestINTASHD002_DimensionsStoredAndDeduplicated(t *testing.T) {
	ctx, _, pool := newMigrationTestPool(t)
	require.NoError(t, Migrate(ctx, pool))
	inst := uuid.New()
	ts := time.Now().UTC().Truncate(time.Second)

	writeASHTx(t, ctx, pool, []ASHRow{ashDimRow(inst, ts, "a"), ashDimRow(inst, ts, "b")})
	writeASHTx(t, ctx, pool, []ASHRow{ashDimRow(inst, ts, "a")})
	var n int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM metrics_ash WHERE instance_id = $1`, inst).Scan(&n))
	require.Equal(t, 2, n)

	var user, addr, app string
	require.NoError(t, pool.QueryRow(ctx, `SELECT usename, client_addr, application_name FROM metrics_ash WHERE instance_id = $1 AND usename = 'a'`, inst).Scan(&user, &addr, &app))
	require.Equal(t, []string{"a", "10.0.0.7", "shop"}, []string{user, addr, app})

	copyInst := uuid.New()
	large := make([]ASHRow, 0, 600)
	for i := 0; i < 600; i++ {
		large = append(large, ashDimRow(copyInst, ts.Add(-time.Duration(i)*10*time.Second), "c"))
	}
	writeASHTx(t, ctx, pool, large)
	writeASHTx(t, ctx, pool, large[:1])
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM metrics_ash WHERE instance_id = $1 AND usename = 'c' AND client_addr = '10.0.0.7' AND application_name = 'shop'`, copyInst).Scan(&n))
	require.Equal(t, 600, n)
}

// INT-ASH-D-003: rows stored before 0033, in a compressed chunk, survive the
// migration with '' in the three new columns.
func TestINTASHD003_MigrationKeepsOldRows(t *testing.T) {
	ctx, _, pool := newMigrationTestPool(t)
	require.NoError(t, Migrate(ctx, pool))
	// Rewind metrics_ash to its shape before 0033.
	for _, sql := range []string{
		`DROP INDEX metrics_ash_dedup_idx`,
		`ALTER TABLE metrics_ash DROP COLUMN usename, DROP COLUMN client_addr, DROP COLUMN application_name`,
		`CREATE UNIQUE INDEX metrics_ash_dedup_idx ON metrics_ash (instance_id, datname, wait_event_type, wait_event, state, COALESCE(queryid, 0), ts)`,
		`DELETE FROM schema_migrations WHERE version = 33`,
	} {
		_, err := pool.Exec(ctx, sql)
		require.NoError(t, err, sql)
	}
	inst := uuid.New()
	old := time.Now().UTC().Add(-72 * time.Hour).Truncate(time.Second)
	for i := 0; i < 2; i++ {
		_, err := pool.Exec(ctx, `INSERT INTO metrics_ash (ts, tenant_id, cluster_id, instance_id, datname, wait_event_type, wait_event, state, samples, window_seconds, window_ticks)
VALUES ($1, 'default', 1, $2, 'app', 'CPU', 'CPU', 'active', 2, 10, 10)`, old.Add(time.Duration(i)*10*time.Second), inst)
		require.NoError(t, err)
	}
	_, err := pool.Exec(ctx, `SELECT compress_chunk(c) FROM show_chunks('metrics_ash') c`)
	require.NoError(t, err)

	require.NoError(t, Migrate(ctx, pool))
	var n, blank int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE usename = '' AND client_addr = '' AND application_name = '') FROM metrics_ash WHERE instance_id = $1`, inst).Scan(&n, &blank))
	require.Equal(t, 2, n)
	require.Equal(t, 2, blank)

	writeASHTx(t, ctx, pool, []ASHRow{ashDimRow(inst, old, "late")})
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM metrics_ash WHERE instance_id = $1`, inst).Scan(&n))
	require.Equal(t, 3, n, "a row that differs by usename lands beside the compressed one")
}
