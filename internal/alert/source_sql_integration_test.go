//go:build integration

package alert

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

func TestINTALERT001_MetricSourceReadsTypedTable(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		pool := pg.Pool(t, "app", pgtest.RoleSuperuser)
		ctx := context.Background()
		_, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS metrics_replication (ts timestamptz NOT NULL, cluster_id bigint NOT NULL, instance_id uuid NOT NULL, replay_lag_sec double precision)`)
		require.NoError(t, err)
		iid := uuid.New()
		now := time.Now().UTC()
		_, err = pool.Exec(ctx, `INSERT INTO metrics_replication (ts, cluster_id, instance_id, replay_lag_sec) VALUES ($1, 1, $2, 42)`, now, iid)
		require.NoError(t, err)
		got, err := (&metricSource{db: pool, interval: time.Minute}).Samples(ctx, Rule{ID: "replica.lag_high", Metric: "pg_replication_lag_seconds"}, now)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, iid, *got[0].InstanceID)
		require.Equal(t, 42.0, got[0].Value)
	})
}

func TestINTALERT002_EventSourceLookback(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		pool := pg.Pool(t, "app", pgtest.RoleSuperuser)
		ctx := context.Background()
		_, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS events (event_id bigserial, ts timestamptz NOT NULL, type text NOT NULL, cluster_id bigint, instance_id uuid)`)
		require.NoError(t, err)
		now := time.Now().UTC()
		iid := uuid.New()
		_, err = pool.Exec(ctx, `INSERT INTO events (ts, type, cluster_id, instance_id) VALUES ($1, 'failover_detected', 1, $2)`, now, iid)
		require.NoError(t, err)
		s := &eventSource{db: pool, interval: time.Minute}
		got, err := s.Samples(ctx, Rule{ID: "failover_detected", EventType: "failover_detected"}, now)
		require.NoError(t, err)
		require.Len(t, got, 1)
		got, err = s.Samples(ctx, Rule{ID: "failover_detected", EventType: "failover_detected"}, now.Add(3*time.Minute))
		require.NoError(t, err)
		require.Empty(t, got)
	})
}

// TestINTALERT003_ClusterRulesAggregateAcrossStandbys — both cluster-scoped
// replication rules used to fall through to the per-instance query, so
// "every standby in the cluster is lagging" fired as soon as any one standby
// lagged, and each standby opened its own alert despite the cluster scope.
func TestINTALERT003_ClusterRulesAggregateAcrossStandbys(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		pool := pg.Pool(t, "app", pgtest.RoleSuperuser)
		ctx := context.Background()
		_, err := pool.Exec(ctx, `DROP TABLE IF EXISTS metrics_replication`)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `CREATE TABLE metrics_replication (
			ts timestamptz NOT NULL, cluster_id bigint NOT NULL, instance_id uuid NOT NULL,
			slot_name text NOT NULL DEFAULT '', sync_state text, replay_lag_sec double precision)`)
		require.NoError(t, err)

		primary := uuid.New()
		now := time.Now().UTC()
		// Two standbys of cluster 1: one lagging badly, one healthy.
		_, err = pool.Exec(ctx, `INSERT INTO metrics_replication (ts, cluster_id, instance_id, slot_name, sync_state, replay_lag_sec) VALUES
			($1, 1, $2, 'standby_a', 'async', 120),
			($1, 1, $2, 'standby_b', 'sync', 1)`, now, primary)
		require.NoError(t, err)

		src := &metricSource{db: pool, interval: time.Minute}

		lagging, err := src.Samples(ctx, Rule{ID: "replica.all_standbys_lagging", Scope: ScopeCluster, Metric: "pg_replication_lag_seconds", Comparator: GT, Threshold: 30}, now)
		require.NoError(t, err)
		require.Len(t, lagging, 1, "one sample per cluster, not one per standby")
		require.Nil(t, lagging[0].InstanceID, "a cluster-scoped sample must not carry an instance_id")
		require.Equal(t, int64(1), *lagging[0].ClusterID)
		require.Equal(t, 1.0, lagging[0].Value, "the minimum lag across standbys: not every standby is lagging")

		sync, err := src.Samples(ctx, Rule{ID: "replica.no_sync_standby", Scope: ScopeCluster, Metric: "pg_sync_standby_count", Comparator: LT, Threshold: 1}, now)
		require.NoError(t, err)
		require.Len(t, sync, 1)
		require.Nil(t, sync[0].InstanceID)
		require.Equal(t, 1.0, sync[0].Value, "one synchronous standby is present")

		// Now every standby lags and none is synchronous.
		later := now.Add(time.Second)
		_, err = pool.Exec(ctx, `INSERT INTO metrics_replication (ts, cluster_id, instance_id, slot_name, sync_state, replay_lag_sec) VALUES
			($1, 1, $2, 'standby_a', 'async', 150),
			($1, 1, $2, 'standby_b', 'async', 90)`, later, primary)
		require.NoError(t, err)

		lagging, err = src.Samples(ctx, Rule{ID: "replica.all_standbys_lagging", Scope: ScopeCluster, Metric: "pg_replication_lag_seconds", Comparator: GT, Threshold: 30}, later)
		require.NoError(t, err)
		require.Len(t, lagging, 1)
		require.Equal(t, 90.0, lagging[0].Value, "the newest reading of each standby is what counts")

		sync, err = src.Samples(ctx, Rule{ID: "replica.no_sync_standby", Scope: ScopeCluster, Metric: "pg_sync_standby_count", Comparator: LT, Threshold: 1}, later)
		require.NoError(t, err)
		require.Len(t, sync, 1)
		require.Equal(t, 0.0, sync[0].Value, "no synchronous standby left")
	})
}
