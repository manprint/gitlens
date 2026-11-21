//go:build integration

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/manprint/pglens/internal/store"
)

// TestINTQTAG002: migration 0041 applies over compressed metrics_ash chunks,
// the rows stored before it read ” for service and endpoint, and rows written
// after it group by service on /api/v1/ash.
func TestINTQTAG002_ServiceMigrationAndGroupBy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()
	c, err := postgres.Run(ctx, "timescale/timescaledb:2.31.0-pg17",
		postgres.WithDatabase("test"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })
	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, store.Migrate(ctx, pool))

	// Rewind metrics_ash to its shape before 0041.
	for _, sql := range []string{
		`DROP INDEX metrics_ash_dedup_idx`,
		`ALTER TABLE metrics_ash DROP COLUMN service, DROP COLUMN endpoint`,
		`CREATE UNIQUE INDEX metrics_ash_dedup_idx ON metrics_ash (instance_id, datname, wait_event_type, wait_event, state, COALESCE(queryid, 0), usename, client_addr, application_name, ts)`,
		`DELETE FROM schema_migrations WHERE version = 41`,
	} {
		_, err := pool.Exec(ctx, sql)
		require.NoError(t, err, sql)
	}
	inst := uuid.New()
	base := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	for i := 0; i < 2; i++ {
		_, err := pool.Exec(ctx, `INSERT INTO metrics_ash (ts, tenant_id, cluster_id, instance_id, datname, wait_event_type, wait_event, state, samples, window_seconds, window_ticks)
VALUES ($1, 'default', 1, $2, 'app', 'CPU', 'CPU', 'active', 2, 10, 10)`, base.Add(time.Duration(i)*10*time.Second), inst)
		require.NoError(t, err)
	}
	var compressed int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(compress_chunk(c)) FROM show_chunks('metrics_ash') c`).Scan(&compressed))
	require.Positive(t, compressed)

	require.NoError(t, store.Migrate(ctx, pool))
	var n, blank int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE service = '' AND endpoint = '') FROM metrics_ash WHERE instance_id = $1`, inst).Scan(&n, &blank))
	require.Equal(t, 2, n)
	require.Equal(t, 2, blank)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, store.WriteASH(ctx, tx, []store.ASHRow{{
		TS: base, TenantID: "default", ClusterID: 1, InstanceID: inst, Datname: "app",
		WaitEventType: "CPU", WaitEvent: "CPU", State: "active", Samples: 5, WindowSeconds: 10, WindowTicks: 10,
		Service: "shop", Endpoint: "/cart",
	}}))
	require.NoError(t, tx.Commit(ctx))

	r := chi.NewRouter()
	NewAshAPI(pool).RegisterRoutes(r)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/ash?instance_id="+inst.String()+"&from="+base.Format(time.RFC3339)+"&to="+base.Add(time.Hour).Format(time.RFC3339)+"&group_by=service", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp ashResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	byService := map[string]int{}
	for _, b := range resp.Buckets {
		require.NotNil(t, b.Service)
		byService[*b.Service] += b.Samples
	}
	require.Equal(t, map[string]int{"": 4, "shop": 5}, byService)
}
