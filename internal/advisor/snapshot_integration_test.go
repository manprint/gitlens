//go:build integration

package advisor

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

type queryCounter struct{ count atomic.Int32 }

func (q *queryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	q.count.Add(1)
	return ctx
}
func (*queryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestINTADV002_LoadSnapshotUsesBoundedQueries(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		cfg, err := pgxpool.ParseConfig(pg.DSN("app", pgtest.RoleSuperuser))
		require.NoError(t, err)
		tracer := &queryCounter{}
		cfg.ConnConfig.Tracer = tracer
		pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
		require.NoError(t, err)
		t.Cleanup(pool.Close)
		_, err = pool.Exec(context.Background(), `CREATE TABLE instances (instance_id uuid PRIMARY KEY, cluster_id bigint NOT NULL, role text NOT NULL)`)
		require.NoError(t, err)
		instanceID := uuid.New()
		_, err = pool.Exec(context.Background(), `INSERT INTO instances(instance_id,cluster_id,role) VALUES ($1,$2,'primary')`, instanceID, int64(9915))
		require.NoError(t, err)
		tracer.count.Store(0)
		_, err = LoadSnapshot(context.Background(), pool, instanceID, time.Now())
		require.NoError(t, err)
		// Two identity queries and twelve section loaders, whatever the
		// number of relations, statements or siblings.
		require.LessOrEqual(t, tracer.count.Load(), int32(14))
	})
}

func TestINTADV003_NoHistoryLeavesBaselineNil(t *testing.T) {
	s := &Snapshot{Baseline: nil, Metrics: map[string]map[string]float64{}, Facts: map[string]map[string]Fact{}}
	require.Nil(t, s.Baseline)
	require.NotPanics(t, func() { _, _ = s.Metric("missing", nil) })
}

func TestINTADV009_LoadTablesKeepsTheOldestRelations(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		pool := pg.Pool(t, "app", pgtest.RoleSuperuser)
		_, err := pool.Exec(ctx, `CREATE TABLE metrics_tables (
			ts timestamptz NOT NULL, instance_id uuid NOT NULL, datname text NOT NULL,
			schemaname text NOT NULL, relname text NOT NULL,
			n_live_tup bigint, n_dead_tup bigint, n_mod_since_analyze bigint,
			last_autovacuum timestamptz, last_vacuum timestamptz,
			relfrozenxid_age bigint, seq_scan bigint, idx_scan bigint)`)
		require.NoError(t, err)
		instanceID := uuid.New()
		now := time.Now()
		// 600 churning relations outrank the append-only one on dead tuples,
		// and the append-only relation has the oldest relfrozenxid of all.
		_, err = pool.Exec(ctx, `INSERT INTO metrics_tables (ts, instance_id, datname, schemaname, relname,
				n_live_tup, n_dead_tup, n_mod_since_analyze, relfrozenxid_age, seq_scan, idx_scan)
			SELECT $1, $2, 'app', 'public', 'churn_' || i, 1000, 1000 + i, 0, i, 0, 0
			FROM generate_series(0, 599) AS i`, now, instanceID)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `INSERT INTO metrics_tables (ts, instance_id, datname, schemaname, relname,
				n_live_tup, n_dead_tup, n_mod_since_analyze, relfrozenxid_age, seq_scan, idx_scan)
			VALUES ($1, $2, 'app', 'public', 'ledger', 1000, 0, 0, 1800000000, 0, 0)`, now, instanceID)
		require.NoError(t, err)
		s := &Snapshot{InstanceID: instanceID}
		require.NoError(t, loadTables(ctx, pool, s, now))
		names := make(map[string]bool, len(s.Tables))
		for _, table := range s.Tables {
			names[table.Name] = true
		}
		require.True(t, names["public.ledger"], "the append-only relation with the oldest relfrozenxid must be kept")
		require.True(t, names["public.churn_599"], "the relation with the most dead tuples must be kept")
		require.False(t, names["public.churn_0"], "a relation outside both rankings is still dropped")
	})
}

// TestINTADV012_SiblingIndexScansComeFromLabelsAndMetrics: a standby's index
// scans are read from its last_idx_scan label and from its metrics history.
// An inactive sibling, and an index a sibling never scanned, count for nothing.
func TestINTADV012_SiblingIndexScansComeFromLabelsAndMetrics(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		pool := pg.Pool(t, "app", pgtest.RoleSuperuser)
		for _, stmt := range []string{
			`CREATE TABLE instances (instance_id uuid PRIMARY KEY, cluster_id bigint NOT NULL, role text NOT NULL, last_seen timestamptz NOT NULL)`,
			`CREATE TABLE object_facts (tenant_id text NOT NULL DEFAULT 'default', cluster_id bigint NOT NULL,
				instance_id uuid NOT NULL, datname text NOT NULL DEFAULT '', kind text NOT NULL, key text NOT NULL,
				labels jsonb NOT NULL DEFAULT '{}'::jsonb, value_text text, value_json jsonb,
				first_seen timestamptz NOT NULL, last_seen timestamptz NOT NULL, changed_at timestamptz NOT NULL,
				PRIMARY KEY (tenant_id, instance_id, datname, kind, key))`,
			`CREATE TABLE metrics_indexes (ts timestamptz NOT NULL, tenant_id text NOT NULL DEFAULT 'default',
				cluster_id bigint NOT NULL, instance_id uuid NOT NULL, datname text NOT NULL, schemaname text NOT NULL,
				relname text NOT NULL, indexrelname text NOT NULL, idx_scan double precision)`,
		} {
			_, err := pool.Exec(ctx, stmt)
			require.NoError(t, err)
		}
		now := time.Now().UTC()
		self, metricsSibling, labelSibling, stale := uuid.New(), uuid.New(), uuid.New(), uuid.New()
		const cluster = int64(7712)
		for _, row := range []struct {
			id   uuid.UUID
			seen time.Time
		}{{self, now}, {metricsSibling, now}, {labelSibling, now}, {stale, now.Add(-2 * time.Hour)}} {
			_, err := pool.Exec(ctx, `INSERT INTO instances (instance_id, cluster_id, role, last_seen) VALUES ($1, $2, 'standby', $3)`, row.id, cluster, row.seen)
			require.NoError(t, err)
		}
		fact := func(id uuid.UUID, key string, labels string) {
			_, err := pool.Exec(ctx, `INSERT INTO object_facts (cluster_id, instance_id, datname, kind, key, labels, value_text, first_seen, last_seen, changed_at)
				VALUES ($1, $2, 'app', 'index_def', $3, $4::jsonb, 'CREATE INDEX ...', $5, $5, $5)`, cluster, id, key, labels, now)
			require.NoError(t, err)
		}
		fact(metricsSibling, "public.orders_metrics_idx", `{"def_hash":"h1"}`)
		fact(metricsSibling, "public.orders_idle_idx", `{"def_hash":"h2","last_idx_scan":""}`)
		fact(labelSibling, "public.orders_label_idx", `{"def_hash":"h3","last_idx_scan":"2026-10-20T06:30:00Z"}`)
		fact(labelSibling, "public.orders_idle_idx", `{"def_hash":"h2","last_idx_scan":""}`)
		metric := func(id uuid.UUID, index string, scans float64) {
			_, err := pool.Exec(ctx, `INSERT INTO metrics_indexes (ts, cluster_id, instance_id, datname, schemaname, relname, indexrelname, idx_scan)
				VALUES ($1, $2, $3, 'app', 'public', 'orders', $4, $5)`, now, cluster, id, index, scans)
			require.NoError(t, err)
		}
		metric(metricsSibling, "orders_metrics_idx", 0.5)
		metric(metricsSibling, "orders_idle_idx", 0)
		metric(stale, "orders_stale_idx", 3)

		s := &Snapshot{ClusterID: cluster, InstanceID: self}
		require.NoError(t, loadSiblings(ctx, pool, s, now))
		byID := map[uuid.UUID]SiblingInfo{}
		for _, sib := range s.Siblings {
			byID[sib.InstanceID] = sib
		}
		require.Len(t, byID, 2, "the inactive sibling is not a sibling")
		metricsKey := indexHashKey("app", "public.orders_metrics_idx")
		labelKey := indexHashKey("app", "public.orders_label_idx")
		idleKey := indexHashKey("app", "public.orders_idle_idx")
		require.True(t, byID[metricsSibling].IndexScanned[metricsKey], "a scan in the metrics history counts")
		require.False(t, byID[metricsSibling].IndexScanned[idleKey])
		require.True(t, byID[labelSibling].IndexScanned[labelKey], "a last scan since the reset counts")
		require.Equal(t, "h2", byID[labelSibling].IndexHashes[idleKey], "the idle index is reported")
		require.False(t, byID[labelSibling].IndexScanned[idleKey], "an empty last scan is no scan")
		for _, sib := range s.Siblings {
			require.NotContains(t, sib.IndexScanned, indexHashKey("app", "public.orders_stale_idx"))
		}
	})
}
