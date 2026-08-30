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
		require.LessOrEqual(t, tracer.count.Load(), int32(12))
	})
}

func TestINTADV003_NoHistoryLeavesBaselineNil(t *testing.T) {
	s := &Snapshot{Baseline: nil, Metrics: map[string]map[string]float64{}, Facts: map[string]map[string]Fact{}}
	require.Nil(t, s.Baseline)
	require.NotPanics(t, func() { _, _ = s.Metric("missing", nil) })
}
