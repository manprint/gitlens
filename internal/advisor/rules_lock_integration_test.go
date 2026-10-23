//go:build integration

package advisor

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

// TestINTADV012_LongWaitIsReadFromTheStoredTree: the loader's query runs
// against the lock_snapshots table of migration 0008, and the rule reads the
// wait the locks check stored with the tree.
func TestINTADV012_LongWaitIsReadFromTheStoredTree(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		pool := pg.Pool(t, "app", pgtest.RoleSuperuser)
		_, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS lock_snapshots (
			tenant_id text NOT NULL DEFAULT 'default',
			instance_id uuid NOT NULL,
			cluster_id bigint NOT NULL,
			ts timestamptz NOT NULL,
			tree jsonb NOT NULL,
			PRIMARY KEY (tenant_id, instance_id))`)
		require.NoError(t, err)
		instanceID := uuid.New()
		now := time.Now().UTC().Truncate(time.Microsecond)
		_, err = pool.Exec(ctx, `INSERT INTO lock_snapshots (tenant_id, instance_id, cluster_id, ts, tree) VALUES ('default', $1, 1, $2, $3::jsonb)`,
			instanceID, now, `{"nodes":[{"pid":10,"query":"UPDATE t SET x = 1","blocked_by":[20],"wait_duration":412.5},{"pid":20,"query":"SELECT 2"}]}`)
		require.NoError(t, err)

		s := &Snapshot{Now: now, InstanceID: instanceID, SkippedChecks: map[string]string{}}
		require.NoError(t, loadLockTree(ctx, pool, s))
		require.True(t, now.Equal(s.LockTree.SampledAt))
		require.Len(t, s.LockTree.Nodes, 2)
		got := ruleByID(t, "lock.long_wait").Evaluate(s)
		require.Len(t, got, 1)
		require.Equal(t, map[string]any{"waiting_pid": int32(10), "root_blocker_pid": int32(20), "wait_seconds": 412.5}, got[0].Evidence)
	})
}
