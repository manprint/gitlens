//go:build integration

package alert

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

// createAlertTables creates the columns of the server schema the metric
// source reads (the test container has no TimescaleDB and no migrations).
func createAlertTables(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS metrics_replication, instances, agents, object_facts`,
		`CREATE TABLE metrics_replication (
			ts timestamptz NOT NULL, cluster_id bigint NOT NULL, instance_id uuid NOT NULL,
			upstream_id uuid, slot_name text NOT NULL DEFAULT '', source text, sync_state text,
			replay_lag_sec double precision, write_lag_sec double precision,
			slot_active boolean, slot_retained_bytes bigint)`,
		`CREATE TABLE agents (agent_id uuid PRIMARY KEY, revoked_at timestamptz)`,
		`CREATE TABLE instances (instance_id uuid PRIMARY KEY, cluster_id bigint NOT NULL, agent_id uuid NOT NULL REFERENCES agents,
			role text NOT NULL, last_seen timestamptz NOT NULL)`,
		`CREATE TABLE object_facts (instance_id uuid NOT NULL, datname text NOT NULL DEFAULT '', kind text NOT NULL, key text NOT NULL, value_text text)`,
	} {
		_, err := pool.Exec(context.Background(), stmt)
		require.NoError(t, err)
	}
}

func TestINTALERT001_MetricSourceReadsTypedTable(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		pool := pg.Pool(t, "app", pgtest.RoleSuperuser)
		ctx := context.Background()
		createAlertTables(t, pool)
		iid := uuid.New()
		now := time.Now().UTC()
		_, err := pool.Exec(ctx, `INSERT INTO metrics_replication (ts, cluster_id, instance_id, source, replay_lag_sec) VALUES ($1, 1, $2, 'receiver', 42)`, now, iid)
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
		createAlertTables(t, pool)

		primary := uuid.New()
		agent := uuid.New()
		now := time.Now().UTC()
		_, err := pool.Exec(ctx, `INSERT INTO agents (agent_id) VALUES ($1)`, agent)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `INSERT INTO instances VALUES ($1, 1, $2, 'primary', $3)`, primary, agent, now)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `INSERT INTO object_facts (instance_id, kind, key, value_text) VALUES ($1, 'setting', 'synchronous_standby_names', 'ANY 1 (standby_a, standby_b)')`, primary)
		require.NoError(t, err)
		// Two standbys of cluster 1: one lagging badly, one healthy.
		_, err = pool.Exec(ctx, `INSERT INTO metrics_replication (ts, cluster_id, instance_id, slot_name, source, sync_state, replay_lag_sec) VALUES
			($1, 1, $2, 'standby_a', 'sender', 'async', 120),
			($1, 1, $2, 'standby_b', 'sender', 'sync', 1)`, now, primary)
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
		_, err = pool.Exec(ctx, `INSERT INTO metrics_replication (ts, cluster_id, instance_id, slot_name, source, sync_state, replay_lag_sec) VALUES
			($1, 1, $2, 'standby_a', 'sender', 'async', 150),
			($1, 1, $2, 'standby_b', 'sender', 'async', 90)`, later, primary)
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

// TestINTALERT004_ReplicationSamplesPerStandby: a slot row newest on its
// instance, a primary with two standbys and a standby that also reports its
// own receiver. The per-instance query used to keep one row per instance: the
// slot row's NULL lag failed the scan of the rule, and only one of the
// primary's standbys was ever judged.
func TestINTALERT004_ReplicationSamplesPerStandby(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		pool := pg.Pool(t, "app", pgtest.RoleSuperuser)
		ctx := context.Background()
		createAlertTables(t, pool)
		primary, standbyA, orphan := uuid.New(), uuid.New(), uuid.New()
		now := time.Now().UTC()
		_, err := pool.Exec(ctx, `INSERT INTO metrics_replication (ts, cluster_id, instance_id, upstream_id, slot_name, source, replay_lag_sec, slot_active, slot_retained_bytes) VALUES
			($1, 1, $2, NULL, 'standby_a', 'sender', 45, NULL, NULL),
			($1, 1, $2, NULL, 'standby_b', 'sender', 0.2, NULL, NULL),
			($1 + interval '1 second', 1, $2, NULL, 'standby_a', 'slot', NULL, true, 1024),
			($1, 1, $3, $2, 'standby_a', 'receiver', 44, NULL, NULL),
			($1, 1, $4, $5, '', 'receiver', 7, NULL, NULL)`, now, primary, standbyA, orphan, uuid.New())
		require.NoError(t, err)

		src := &metricSource{db: pool, interval: time.Minute}
		got, err := src.Samples(ctx, Rule{ID: "replica.lag_high", Scope: ScopeInstance, Metric: "pg_replication_lag_seconds", Comparator: GT, Threshold: 30}, now.Add(time.Second))
		require.NoError(t, err)
		byKey := map[string]float64{}
		for _, s := range got {
			byKey[Key(Rule{ID: "replica.lag_high"}, s)] = s.Value
		}
		require.Equal(t, map[string]float64{
			"replica.lag_high/" + primary.String() + "/standby=standby_a": 45,
			"replica.lag_high/" + primary.String() + "/standby=standby_b": 0.2,
			// standby_a's own receiver is dropped: its upstream reports it.
			// The orphan's upstream is not monitored, so its own view counts.
			"replica.lag_high/" + orphan.String() + "/": 7,
		}, byKey)

		slots, err := src.Samples(ctx, Rule{ID: "custom.slot", Scope: ScopeInstance, Metric: "pg_replication_slot_retained_bytes", Comparator: GT, Threshold: 0}, now.Add(time.Second))
		require.NoError(t, err)
		require.Len(t, slots, 1)
		require.Equal(t, map[string]string{"slot_name": "standby_a"}, slots[0].Labels)
		require.Equal(t, 1024.0, slots[0].Value)
	})
}

// TestINTALERT005_NoSyncStandbyOnlyWhereRequired: an asynchronous cluster is
// inapplicable, a synchronous primary that lost its every standby counts 0.
// The rule used to fire for every asynchronous cluster and never for a
// synchronous primary with no pg_stat_replication row left.
func TestINTALERT005_NoSyncStandbyOnlyWhereRequired(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		pool := pg.Pool(t, "app", pgtest.RoleSuperuser)
		ctx := context.Background()
		createAlertTables(t, pool)
		agent := uuid.New()
		asyncPrimary, syncPrimary := uuid.New(), uuid.New()
		now := time.Now().UTC()
		for _, stmt := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO agents (agent_id) VALUES ($1)`, []any{agent}},
			{`INSERT INTO instances VALUES ($1, 1, $3, 'primary', $4), ($2, 2, $3, 'primary', $4)`, []any{asyncPrimary, syncPrimary, agent, now}},
			{`INSERT INTO object_facts (instance_id, kind, key, value_text) VALUES ($1, 'setting', 'synchronous_standby_names', ''), ($2, 'setting', 'synchronous_standby_names', 'standby_x')`, []any{asyncPrimary, syncPrimary}},
			{`INSERT INTO metrics_replication (ts, cluster_id, instance_id, slot_name, source, sync_state, replay_lag_sec) VALUES ($1, 1, $2, 'standby_a', 'sender', 'async', 0.1)`, []any{now, asyncPrimary}},
		} {
			_, err := pool.Exec(ctx, stmt.sql, stmt.args...)
			require.NoError(t, err)
		}

		got, err := (&metricSource{db: pool, interval: time.Minute}).Samples(ctx, Rule{ID: "replica.no_sync_standby", Scope: ScopeCluster, Metric: "pg_sync_standby_count", Comparator: LT, Threshold: 1}, now)
		require.NoError(t, err)
		require.Len(t, got, 2)
		byCluster := map[int64]Sample{}
		for _, s := range got {
			byCluster[*s.ClusterID] = s
		}
		require.True(t, byCluster[1].Inapplicable, "an asynchronous cluster is not missing a synchronous standby")
		require.False(t, byCluster[2].Inapplicable)
		require.Equal(t, 0.0, byCluster[2].Value, "the synchronous primary has no standby left")
	})
}

// TestINTALERT006_RevokedAgentIsNotDown: a revoked agent's instance stops
// pushing by design. It used to page as agent_down indefinitely.
func TestINTALERT006_RevokedAgentIsNotDown(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		pool := pg.Pool(t, "app", pgtest.RoleSuperuser)
		ctx := context.Background()
		createAlertTables(t, pool)
		live, revoked := uuid.New(), uuid.New()
		liveInstance, revokedInstance := uuid.New(), uuid.New()
		old := time.Now().UTC().Add(-time.Hour)
		_, err := pool.Exec(ctx, `INSERT INTO agents VALUES ($1, NULL), ($2, now())`, live, revoked)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `INSERT INTO instances VALUES ($1, 1, $2, 'primary', $5), ($3, 1, $4, 'standby', $5)`, liveInstance, live, revokedInstance, revoked, old)
		require.NoError(t, err)

		got, err := (&metricSource{db: pool, interval: time.Minute}).Samples(ctx, Rule{ID: "agent_down", Scope: ScopeInstance, Metric: "up", Comparator: LT, Threshold: 1}, time.Now().UTC())
		require.NoError(t, err)
		require.Len(t, got, 2)
		for _, s := range got {
			require.Equal(t, 0.0, s.Value)
			require.Equal(t, *s.InstanceID == revokedInstance, s.Inapplicable)
		}
	})
}
