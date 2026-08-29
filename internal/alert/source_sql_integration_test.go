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
