//go:build integration

package advisor

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

func advisorIntegrationSnapshot(id uuid.UUID, now time.Time, age float64) *Snapshot {
	metrics := map[string]map[string]float64{}
	for _, name := range []string{
		"pg_max_xact_age_seconds", "pg_max_idle_in_txn_seconds", "pg_oldest_prepared_xact_seconds",
		"pg_xact_rollback_ratio", "pg_max_datfrozenxid_age", "pg_connections_used_ratio",
		"pg_checkpoints_requested_ratio", "pg_replication_lag_bytes", "pg_connections_idle_share",
		"pg_archiver_failed_ratio", "pg_archiver_last_archived_age_seconds", "pg_basebackup_age_days",
	} {
		metrics[name] = map[string]float64{"": 0}
	}
	metrics["pg_max_xact_age_seconds"][""] = age
	return &Snapshot{
		Now: now, ClusterID: 7001, InstanceID: id, Role: pgtype.RolePrimary,
		PermTier: pgtype.TierReadOnly, Metrics: metrics, Facts: map[string]map[string]Fact{},
		Tables: []TableStat{}, Indexes: []IndexStat{}, Bloat: []BloatStat{}, Statements: []StatementStat{},
		Host: HostInfo{Available: true, TotalBytes: 16 * 1024 * 1024 * 1024}, Siblings: []SiblingInfo{},
		SkippedChecks: map[string]string{}, Baseline: &Baseline{MeanExecTimeByQueryID: map[int64]float64{}},
		Settings: map[string]string{
			"work_mem_bytes": "4194304", "max_connections": "100", "shared_buffers_bytes": "268435456",
			"effective_cache_size_bytes": "8589934592", "maintenance_work_mem_bytes": "268435456",
			"pooler_observed": "true", "track_io_timing": "on", "wal_keep_size_bytes": "0",
			"replication_slot_protects": "true", "fsync": "on", "full_page_writes": "on",
			"autovacuum": "on", "archive_mode": "on", "archive_timeout_seconds": "0", "basebackup_seen": "true",
		},
	}
}

func setupAdvisorInstance(t *testing.T, pool *pgxpool.Pool, now time.Time) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	instanceID := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO clusters(cluster_id) VALUES ($1)`, int64(7001))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO instances(instance_id,cluster_id,addr,port,pg_version,role,perm_tier,last_seen) VALUES ($1,$2,'advisor-test',5432,180000,'primary','T0',$3)`, instanceID, int64(7001), now)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM findings WHERE instance_id=$1`, instanceID)
		_, _ = pool.Exec(ctx, `DELETE FROM instances WHERE instance_id=$1`, instanceID)
		_, _ = pool.Exec(ctx, `DELETE FROM clusters WHERE cluster_id=$1`, int64(7001))
	})
	return instanceID
}

const advisorIntegrationSchema = `
DROP TABLE IF EXISTS findings, instances, clusters, agents CASCADE;
CREATE TABLE agents (agent_id uuid PRIMARY KEY);
CREATE TABLE clusters (cluster_id bigint PRIMARY KEY);
CREATE TABLE instances (instance_id uuid PRIMARY KEY, tenant_id text NOT NULL DEFAULT 'default', cluster_id bigint NOT NULL, addr text NOT NULL, port int NOT NULL, pg_version int NOT NULL, role text NOT NULL, perm_tier text NOT NULL, last_seen timestamptz NOT NULL);
CREATE TABLE findings (finding_id text NOT NULL, tenant_id text NOT NULL DEFAULT 'default', rule_id text NOT NULL, severity text NOT NULL, state text NOT NULL, scope text NOT NULL, cluster_id bigint, instance_id uuid, datname text NOT NULL DEFAULT '', object_name text NOT NULL DEFAULT '', title text NOT NULL, detail text NOT NULL, remediation text NOT NULL DEFAULT '', evidence jsonb NOT NULL DEFAULT '{}'::jsonb, degraded_reason text, first_seen timestamptz NOT NULL, last_seen timestamptz NOT NULL, resolved_at timestamptz, muted_until timestamptz, mute_reason text, PRIMARY KEY (tenant_id, finding_id));`

func TestINTADV004_EngineResolvesFixedFindings(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		pool := pg.Pool(t, "app", pgtest.RoleSuperuser)
		_, err := pool.Exec(ctx, advisorIntegrationSchema)
		require.NoError(t, err)
		now := time.Now().UTC()
		id := setupAdvisorInstance(t, pool, now)
		problem := true
		e := NewEngine(pool, nil, time.Minute)
		e.load = func(context.Context, uuid.UUID, time.Time) (*Snapshot, error) {
			age := float64(0)
			if problem {
				age = 1000
			}
			return advisorIntegrationSnapshot(id, now, age), nil
		}
		require.NoError(t, e.Run(ctx))
		var open int
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM findings WHERE instance_id=$1 AND rule_id='txn.long_running' AND state='open'`, id).Scan(&open))
		require.Equal(t, 1, open)
		problem = false
		require.NoError(t, e.Run(ctx))
		var resolved int
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM findings WHERE instance_id=$1 AND rule_id='txn.long_running' AND state='resolved'`, id).Scan(&resolved))
		require.Equal(t, 1, resolved)
		e.Stop()
	})
}

func TestINTADV005_TwoEnginesProduceOneFindingSet(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		pool := pg.Pool(t, "app", pgtest.RoleSuperuser)
		_, err := pool.Exec(ctx, advisorIntegrationSchema)
		require.NoError(t, err)
		now := time.Now().UTC()
		id := setupAdvisorInstance(t, pool, now)
		loader := func(context.Context, uuid.UUID, time.Time) (*Snapshot, error) {
			return advisorIntegrationSnapshot(id, now, 1000), nil
		}
		e1, e2 := NewEngine(pool, nil, time.Minute), NewEngine(pool, nil, time.Minute)
		e1.load, e2.load = loader, loader
		require.NoError(t, e1.Run(ctx))
		require.NoError(t, e2.Run(ctx))
		var count int
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM findings WHERE instance_id=$1 AND finding_id='txn.long_running/' || $1::text`, id).Scan(&count))
		require.Equal(t, 1, count)
		e1.Stop()
		e2.Stop()
	})
}
