//go:build integration

package advisor

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/test/pgtest"
)

// WINDOW-INT-001: an index PostgreSQL 16 has not seen scanned since a
// statistics reset eight days ago is an open index.unused finding after the
// first evaluation, although pglens has a single sample of it. A real
// PostgreSQL cannot be given an old stats_reset (pg_stat_reset sets it to now),
// so the store is seeded with the labels the agent reports, and the facts,
// indexes and siblings are read by the snapshot's own loaders.
func TestWINDOWINT001_OldStatsResetJudgesAtOnce(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		pool := pg.Pool(t, "app", pgtest.RoleSuperuser)
		_, err := pool.Exec(ctx, advisorIntegrationSchema)
		require.NoError(t, err)
		for _, stmt := range []string{
			`DROP TABLE IF EXISTS object_facts, metrics_indexes`,
			`CREATE TABLE object_facts (tenant_id text NOT NULL DEFAULT 'default', cluster_id bigint NOT NULL,
				instance_id uuid NOT NULL, datname text NOT NULL DEFAULT '', kind text NOT NULL, key text NOT NULL,
				labels jsonb NOT NULL DEFAULT '{}'::jsonb, value_text text, value_json jsonb,
				first_seen timestamptz NOT NULL, last_seen timestamptz NOT NULL, changed_at timestamptz NOT NULL,
				PRIMARY KEY (tenant_id, instance_id, datname, kind, key))`,
			`CREATE TABLE metrics_indexes (ts timestamptz NOT NULL, tenant_id text NOT NULL DEFAULT 'default',
				cluster_id bigint NOT NULL, instance_id uuid NOT NULL, datname text NOT NULL, schemaname text NOT NULL,
				relname text NOT NULL, indexrelname text NOT NULL, idx_scan double precision,
				index_bytes bigint, is_primary boolean, is_unique boolean, is_valid boolean, def_hash text)`,
		} {
			_, err := pool.Exec(ctx, stmt)
			require.NoError(t, err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS object_facts, metrics_indexes`) })

		now := time.Now().UTC()
		id := setupAdvisorInstance(t, pool, now)
		standby := uuid.New()
		_, err = pool.Exec(ctx, `INSERT INTO instances(instance_id,cluster_id,addr,port,pg_version,role,perm_tier,last_seen) VALUES ($1,$2,'advisor-standby',5432,180000,'standby','T0',$3)`, standby, int64(7001), now)
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM instances WHERE instance_id=$1`, standby) })

		reset := now.Add(-8 * 24 * time.Hour)
		labels, err := json.Marshal(map[string]string{"def_hash": "h1", "last_idx_scan": "", "stats_reset": reset.Format(time.RFC3339)})
		require.NoError(t, err)
		for _, inst := range []uuid.UUID{id, standby} {
			_, err = pool.Exec(ctx, `INSERT INTO object_facts (cluster_id, instance_id, datname, kind, key, labels, value_text, first_seen, last_seen, changed_at)
				VALUES ($1, $2, 'app', 'index_def', 'public.orders_note_idx', $3::jsonb, 'CREATE INDEX orders_note_idx ON public.orders (note)', $4, $4, $4)`,
				int64(7001), inst, labels, now)
			require.NoError(t, err)
		}
		_, err = pool.Exec(ctx, `INSERT INTO metrics_indexes (ts, cluster_id, instance_id, datname, schemaname, relname, indexrelname, idx_scan, index_bytes, is_primary, is_unique, is_valid, def_hash)
			VALUES ($1, $2, $3, 'app', 'public', 'orders', 'orders_note_idx', 0, $4, false, false, true, 'h1')`,
			now, int64(7001), id, int64(64*1024*1024))
		require.NoError(t, err)

		e := NewEngine(pool, nil, time.Minute)
		defer e.Stop()
		e.load = func(ctx context.Context, instanceID uuid.UUID, at time.Time) (*Snapshot, error) {
			s := advisorIntegrationSnapshot(instanceID, at, 0)
			if err := loadFacts(ctx, pool, s); err != nil {
				return nil, err
			}
			if err := loadSiblings(ctx, pool, s, at); err != nil {
				return nil, err
			}
			if err := loadIndexes(ctx, pool, s, at); err != nil {
				return nil, err
			}
			attachIndexDefs(s)
			return s, nil
		}
		require.NoError(t, e.Run(ctx))

		var state, source string
		var observed, window int64
		require.NoError(t, pool.QueryRow(ctx, `SELECT state, evidence->>'observed_source',
				(evidence->>'observed_for_seconds')::bigint, (evidence->>'window_seconds')::bigint
			FROM findings WHERE instance_id=$1 AND rule_id='index.unused' AND object_name='public.orders_note_idx'`, id).
			Scan(&state, &source, &observed, &window))
		require.Equal(t, string(StateOpen), state)
		require.Equal(t, ObservedByPostgresStats, source)
		require.InDelta(t, float64(8*24*3600), float64(observed), 120)
		require.Equal(t, int64(7*24*3600), window)
	})
}
