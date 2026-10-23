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
		"pg_max_xact_age_seconds", "pg_max_idle_in_transaction_seconds", "pg_oldest_prepared_xact_seconds",
		"pg_xact_rollback_ratio", "pg_max_datfrozenxid_age", "pg_connections_used_ratio",
		"pg_checkpoints_requested_ratio", "pg_connections_idle_share", "pg_connections_used",
		"pg_archiver_failed_ratio", "pg_archiver_last_archived_age_seconds",
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
			"track_io_timing": "on", "wal_keep_size_bytes": "0",
			"fsync": "on", "full_page_writes": "on",
			"autovacuum": "on", "archive_mode": "on", "archive_timeout_seconds": "0",
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

// TestINTADV006_SameDatabaseFindingOnTwoInstancesStaysSeparate — two replicas
// of one database both report a high rollback ratio. Their findings used to
// share one ID, so the second write replaced the first row and the second
// instance's healthy pass resolved the first instance's open finding.
func TestINTADV006_SameDatabaseFindingOnTwoInstancesStaysSeparate(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		pool := pg.Pool(t, "app", pgtest.RoleSuperuser)
		_, err := pool.Exec(ctx, advisorIntegrationSchema)
		require.NoError(t, err)
		now := time.Now().UTC()
		first := setupAdvisorInstance(t, pool, now)
		second := uuid.New()
		_, err = pool.Exec(ctx, `INSERT INTO instances(instance_id,cluster_id,addr,port,pg_version,role,perm_tier,last_seen) VALUES ($1,$2,'advisor-test-2',5432,180000,'standby','T0',$3)`, second, int64(7001), now)
		require.NoError(t, err)
		secondRollingBack := true
		e := NewEngine(pool, nil, time.Minute)
		e.load = func(_ context.Context, id uuid.UUID, _ time.Time) (*Snapshot, error) {
			s := advisorIntegrationSnapshot(id, now, 0)
			ratio := 0.5
			if id == second && !secondRollingBack {
				ratio = 0.01
			}
			s.Metrics["pg_xact_rollback_ratio"] = map[string]float64{"database=app": ratio}
			return s, nil
		}
		require.NoError(t, e.Run(ctx))
		var open int
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM findings WHERE rule_id='txn.high_rollback_ratio' AND state='open'`).Scan(&open))
		require.Equal(t, 2, open, "each instance keeps its own finding")

		secondRollingBack = false
		require.NoError(t, e.Run(ctx))
		var firstOpen, secondResolved int
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM findings WHERE instance_id=$1 AND rule_id='txn.high_rollback_ratio' AND state='open'`, first).Scan(&firstOpen))
		require.Equal(t, 1, firstOpen, "a healthy replica must not resolve the other replica's finding")
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM findings WHERE instance_id=$1 AND rule_id='txn.high_rollback_ratio' AND state='resolved'`, second).Scan(&secondResolved))
		require.Equal(t, 1, secondResolved)
		e.Stop()
	})
}

// TestINTADV010_MutedAndSilentFindingsResolve — a muted finding stayed muted
// after its condition cleared, and the findings of an instance that went quiet
// stayed open for ever. A muted finding whose condition still holds stays muted,
// and an active instance is never swept.
func TestINTADV010_MutedAndSilentFindingsResolve(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		pool := pg.Pool(t, "app", pgtest.RoleSuperuser)
		_, err := pool.Exec(ctx, advisorIntegrationSchema)
		require.NoError(t, err)
		now := time.Now().UTC()
		id := setupAdvisorInstance(t, pool, now)
		quiet := uuid.New()
		_, err = pool.Exec(ctx, `INSERT INTO instances(instance_id,cluster_id,addr,port,pg_version,role,perm_tier,last_seen) VALUES ($1,$2,'advisor-gone',5432,180000,'primary','T0',$3)`, quiet, int64(7001), now.Add(-48*time.Hour))
		require.NoError(t, err)
		st := NewPgStore(pool)
		finding := func(instance uuid.UUID, object string) Finding {
			return Finding{RuleID: "txn.long_running", Severity: SeverityWarning, State: StateOpen, Scope: ScopeInstance, InstanceID: &instance, ObjectName: object, Title: "t", Detail: "d"}
		}
		upsertMuted := func(f Finding) {
			require.NoError(t, st.UpsertFinding(ctx, f, now))
			_, err := pool.Exec(ctx, `UPDATE findings SET state='muted', muted_until=$2 WHERE finding_id=$1`, f.ID(), now.Add(time.Hour))
			require.NoError(t, err)
		}
		state := func(f Finding) string {
			var s string
			require.NoError(t, pool.QueryRow(ctx, `SELECT state FROM findings WHERE finding_id=$1`, f.ID()).Scan(&s))
			return s
		}

		cleared, held := finding(id, "cleared"), finding(id, "held")
		upsertMuted(cleared)
		upsertMuted(held)
		require.NoError(t, st.ResolveAbsent(ctx, id, []string{"txn.long_running"}, []string{held.ID()}, now))
		require.Equal(t, "resolved", state(cleared), "a muted finding whose condition cleared is resolved")
		require.Equal(t, "muted", state(held), "a muted finding whose condition still holds stays muted")

		goneMuted, goneOpen, active := finding(quiet, "muted-gone"), finding(quiet, "open-gone"), finding(id, "still-open")
		upsertMuted(goneMuted)
		require.NoError(t, st.UpsertFinding(ctx, goneOpen, now))
		require.NoError(t, st.UpsertFinding(ctx, active, now))
		require.NoError(t, st.ResolveUnseen(ctx, now.Add(-unseenInstanceGrace), now))
		require.Equal(t, "resolved", state(goneMuted), "the muted finding of an instance gone quiet is resolved")
		require.Equal(t, "resolved", state(goneOpen), "the open finding of an instance gone quiet is resolved")
		require.Equal(t, "open", state(active), "an instance seen within the grace period is not swept")
	})
}
