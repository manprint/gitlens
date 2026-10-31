//go:build integration

package advisor

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/test/pgtest"
)

// windowStore is a store seeded for the index.unused window tests: a primary,
// a standby of the same cluster that reports every seeded index unscanned, and
// the facts and index metrics tables the snapshot's own loaders read.
type windowStore struct {
	pool             *pgxpool.Pool
	now              time.Time
	primary, standby uuid.UUID
}

func newWindowStore(t *testing.T, pg *pgtest.PG) windowStore {
	t.Helper()
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
	st := windowStore{pool: pool, now: now, primary: setupAdvisorInstance(t, pool, now), standby: uuid.New()}
	_, err = pool.Exec(ctx, `INSERT INTO instances(instance_id,cluster_id,addr,port,pg_version,role,perm_tier,last_seen) VALUES ($1,$2,'advisor-standby',5432,180000,'standby','T0',$3)`, st.standby, int64(7001), now)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM instances WHERE instance_id=$1`, st.standby) })
	return st
}

// seedIndex stores a 64 MiB index of public.orders: its index_def fact with
// the given labels on both members, and one metrics sample of the primary per
// age, with the scan rate given for it.
func (st windowStore) seedIndex(t *testing.T, name string, labels map[string]string, scansByAge map[time.Duration]float64) {
	t.Helper()
	ctx := context.Background()
	labels["def_hash"] = "h-" + name
	raw, err := json.Marshal(labels)
	require.NoError(t, err)
	for _, inst := range []uuid.UUID{st.primary, st.standby} {
		_, err = st.pool.Exec(ctx, `INSERT INTO object_facts (cluster_id, instance_id, datname, kind, key, labels, value_text, first_seen, last_seen, changed_at)
			VALUES ($1, $2, 'app', 'index_def', $3, $4::jsonb, $5, $6, $6, $6)`,
			int64(7001), inst, "public."+name, raw, "CREATE INDEX "+name+" ON public.orders (note)", st.now)
		require.NoError(t, err)
	}
	for age, scans := range scansByAge {
		_, err = st.pool.Exec(ctx, `INSERT INTO metrics_indexes (ts, cluster_id, instance_id, datname, schemaname, relname, indexrelname, idx_scan, index_bytes, is_primary, is_unique, is_valid, def_hash)
			VALUES ($1, $2, $3, 'app', 'public', 'orders', $4, $5, $6, false, false, true, $7)`,
			st.now.Add(-age), int64(7001), st.primary, name, scans, int64(64*1024*1024), "h-"+name)
		require.NoError(t, err)
	}
}

// run evaluates the primary once with the windows the source answers. The
// facts, indexes and siblings are read by the snapshot's own loaders.
func (st windowStore) run(t *testing.T, windows Windows) {
	t.Helper()
	e := NewEngine(st.pool, nil, time.Minute)
	defer e.Stop()
	e.SetWindowsSource(func(context.Context) Windows { return windows })
	e.load = func(ctx context.Context, instanceID uuid.UUID, at time.Time, windows Windows) (*Snapshot, error) {
		s := advisorIntegrationSnapshot(instanceID, at, 0)
		s.Windows = windows
		if err := loadFacts(ctx, st.pool, s); err != nil {
			return nil, err
		}
		if err := loadSiblings(ctx, st.pool, s, at); err != nil {
			return nil, err
		}
		if err := loadIndexes(ctx, st.pool, s, at); err != nil {
			return nil, err
		}
		attachIndexDefs(s)
		return s, nil
	}
	require.NoError(t, e.Run(context.Background()))
}

// unusedFinding is the stored index.unused finding of an index of the
// primary; found is false when there is none.
type unusedRow struct {
	found            bool
	state            string
	source           sql.NullString
	observed, window sql.NullInt64
}

func (st windowStore) unusedFinding(t *testing.T, name string) unusedRow {
	t.Helper()
	var row unusedRow
	err := st.pool.QueryRow(context.Background(), `SELECT state, evidence->>'observed_source',
			(evidence->>'observed_for_seconds')::bigint, (evidence->>'window_seconds')::bigint
		FROM findings WHERE instance_id=$1 AND rule_id='index.unused' AND object_name=$2`, st.primary, "public."+name).
		Scan(&row.state, &row.source, &row.observed, &row.window)
	if err != nil {
		require.ErrorContains(t, err, "no rows")
		return unusedRow{}
	}
	row.found = true
	return row
}

// WINDOW-INT-001: an index PostgreSQL 16 has not seen scanned since a
// statistics reset eight days ago is an open index.unused finding after the
// first evaluation, although pglens has a single sample of it. A real
// PostgreSQL cannot be given an old stats_reset (pg_stat_reset sets it to now),
// so the store is seeded with the labels the agent reports, and the facts,
// indexes and siblings are read by the snapshot's own loaders.
func TestWINDOWINT001_OldStatsResetJudgesAtOnce(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		st := newWindowStore(t, pg)
		reset := st.now.Add(-8 * 24 * time.Hour)
		st.seedIndex(t, "orders_note_idx", map[string]string{"last_idx_scan": "", "stats_reset": reset.Format(time.RFC3339)}, map[time.Duration]float64{0: 0})
		st.run(t, DefaultWindows())

		got := st.unusedFinding(t, "orders_note_idx")
		require.True(t, got.found)
		require.Equal(t, string(StateOpen), got.state)
		require.Equal(t, ObservedByPostgresStats, got.source.String)
		require.InDelta(t, float64(8*24*3600), float64(got.observed.Int64), 120)
		require.Equal(t, int64(7*24*3600), got.window.Int64)
	})
}

// WINDOW-INT-002 (R-P13 #2): the stored history spans 20 days for both
// indexes, PostgreSQL 15 shape (no last_idx_scan). Under a 7-day window the
// index last scanned 20 days ago, on the primary and on the standby, is an
// open candidate; the one scanned 2 days ago is in use and has no finding.
// The scans used to count over the whole history, so neither was ever a
// candidate.
func TestWINDOWINT002_ScanBeforeTheWindowDoesNotCount(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		st := newWindowStore(t, pg)
		day := 24 * time.Hour
		st.seedIndex(t, "orders_old_scan_idx", map[string]string{}, map[time.Duration]float64{20 * day: 5, 10 * day: 0, 0: 0})
		st.seedIndex(t, "orders_recent_scan_idx", map[string]string{}, map[time.Duration]float64{20 * day: 0, 2 * day: 3, 0: 0})
		// The standby scanned the first index 20 days ago too: before the
		// window, so it does not keep the index in use either.
		_, err := st.pool.Exec(context.Background(), `INSERT INTO metrics_indexes (ts, cluster_id, instance_id, datname, schemaname, relname, indexrelname, idx_scan, index_bytes, is_primary, is_unique, is_valid, def_hash)
			VALUES ($1, $2, $3, 'app', 'public', 'orders', 'orders_old_scan_idx', 2, $4, false, false, true, 'h-orders_old_scan_idx')`,
			st.now.Add(-20*day), int64(7001), st.standby, int64(64*1024*1024))
		require.NoError(t, err)
		st.run(t, Windows{UnusedIndex: 7 * day})

		old := st.unusedFinding(t, "orders_old_scan_idx")
		require.True(t, old.found)
		require.Equal(t, string(StateOpen), old.state)
		require.Equal(t, ObservedByPglens, old.source.String)
		require.InDelta(t, float64(20*24*3600), float64(old.observed.Int64), 120)
		require.Equal(t, int64(7*24*3600), old.window.Int64)
		require.False(t, st.unusedFinding(t, "orders_recent_scan_idx").found)
	})
}

// WINDOW-INT-003 (R-P13 #5, D22b): pglens has an hour of history of both
// indexes and saw no scan. On PostgreSQL 16, last_idx_scan ten days ago (on
// the primary and on the standby) proves the 7-day window at once: an open
// finding resting on the statistics. The PostgreSQL 15 shape has no
// last_idx_scan, so the same index stays degraded.
func TestWINDOWINT003_LastScanBeforeTheWindowJudgesAtOnce(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		st := newWindowStore(t, pg)
		lastScan := st.now.Add(-10 * 24 * time.Hour)
		reset := st.now.Add(-30 * 24 * time.Hour)
		hour := map[time.Duration]float64{time.Hour: 0, 0: 0}
		st.seedIndex(t, "orders_pg16_idx", map[string]string{"last_idx_scan": lastScan.Format(time.RFC3339), "stats_reset": reset.Format(time.RFC3339)}, hour)
		st.seedIndex(t, "orders_pg15_idx", map[string]string{}, hour)
		st.run(t, Windows{UnusedIndex: 7 * 24 * time.Hour})

		pg16 := st.unusedFinding(t, "orders_pg16_idx")
		require.True(t, pg16.found)
		require.Equal(t, string(StateOpen), pg16.state)
		require.Equal(t, ObservedByPostgresStats, pg16.source.String)
		require.InDelta(t, float64(10*24*3600), float64(pg16.observed.Int64), 120)
		require.Equal(t, int64(7*24*3600), pg16.window.Int64)

		pg15 := st.unusedFinding(t, "orders_pg15_idx")
		require.True(t, pg15.found)
		require.Equal(t, string(StateDegraded), pg15.state)
	})
}

// WINDOW-INT-004 (R-P13b #1): a configured 5-day window reaches the index
// loader through the engine: an index last scanned 6 days ago, which the
// default 7-day window would keep in use, is an open candidate.
func TestWINDOWINT004_ConfiguredWindowReachesTheLoader(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		st := newWindowStore(t, pg)
		day := 24 * time.Hour
		st.seedIndex(t, "orders_six_days_idx", map[string]string{}, map[time.Duration]float64{20 * day: 0, 6 * day: 2, 0: 0})
		st.run(t, Windows{UnusedIndex: 5 * day})

		got := st.unusedFinding(t, "orders_six_days_idx")
		require.True(t, got.found)
		require.Equal(t, string(StateOpen), got.state)
		require.Equal(t, int64(5*24*3600), got.window.Int64)
	})
}

// WINDOW-INT-005 (R-P13b #2): the engine reads the retention policy of
// metrics_indexes once per pass. A test PostgreSQL has no TimescaleDB, so the
// catalogue view the engine reads is stood in by a table of the same name.
// With 30 days kept and a 60-day window, the degraded reason names the kept
// history; without the catalogue the reason is the usual one.
func TestWINDOWINT005_ReasonNamesTheRetentionInForce(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		st := newWindowStore(t, pg)
		ctx := context.Background()
		day := 24 * time.Hour
		st.seedIndex(t, "orders_kept_idx", map[string]string{}, map[time.Duration]float64{20 * day: 0, 0: 0})
		reason := func() string {
			var got string
			require.NoError(t, st.pool.QueryRow(ctx, `SELECT degraded_reason FROM findings
				WHERE instance_id=$1 AND rule_id='index.unused' AND object_name='public.orders_kept_idx'`, st.primary).Scan(&got))
			return got
		}

		st.run(t, Windows{UnusedIndex: 60 * day})
		require.Equal(t, "index usage history is shorter than 60 days", reason())

		for _, stmt := range []string{
			`CREATE SCHEMA timescaledb_information`,
			`CREATE TABLE timescaledb_information.jobs (proc_name name, hypertable_schema name, hypertable_name name, config jsonb)`,
			`INSERT INTO timescaledb_information.jobs VALUES
				('policy_retention', 'public', 'metrics_indexes', '{"drop_after": "30 days"}'),
				('policy_retention', 'public', 'metrics_tables', '{"drop_after": "3 days"}'),
				('policy_compression', 'public', 'metrics_indexes', '{"compress_after": "2 days"}')`,
		} {
			_, err := st.pool.Exec(ctx, stmt)
			require.NoError(t, err)
		}
		t.Cleanup(func() { _, _ = st.pool.Exec(ctx, `DROP SCHEMA IF EXISTS timescaledb_information CASCADE`) })
		st.run(t, Windows{UnusedIndex: 60 * day})
		require.Equal(t, "the unused-index window (60 days) is longer than the kept index metrics history (30 days)", reason())
	})
}
