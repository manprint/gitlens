package check

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
)

func metricsByKey(r Result) map[string]float64 {
	out := map[string]float64{}
	for _, m := range r.Metrics {
		out[m.Name+"|"+pgtype.CanonicalLabels(m.Labels)] = m.Value
	}
	return out
}

func TestInternalsChecks_Requirements(t *testing.T) {
	for _, tc := range []struct {
		c        Check
		name     string
		interval time.Duration
	}{
		{&slruCheck{}, "slru", time.Minute},
		{&walDirectoryCheck{}, "wal_directory", time.Minute},
		{&recoveryCheck{}, "recovery", 30 * time.Second},
		{&databaseSizeCheck{}, "database_size", 15 * time.Minute},
	} {
		require.Equal(t, tc.name, tc.c.Name())
		require.Equal(t, ScopeInstance, tc.c.Requires().Scope)
		require.Equal(t, pgtype.TierReadOnly, tc.c.Requires().PermTier)
		// No role gate: a rule reading these checks is never left without
		// its input on one role.
		require.Empty(t, tc.c.Requires().Roles)
		require.Equal(t, tc.interval, tc.c.DefaultInterval())
	}
}

// PostgreSQL 17 renamed the caches; both spellings give one series.
func TestSLRU_NamesAreTheSameAcrossVersions(t *testing.T) {
	scrape := func(names ...string) Result {
		var rows [][]any
		for i, name := range names {
			rows = append(rows, []any{name, int64(i), int64(100 + i), int64(10), int64(4), int64(0), int64(2), int64(1)})
		}
		conn := &mockConn{queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			require.Equal(t, slruQuery, sql)
			return &mockRows{rows: rows}, nil
		}}
		r, err := (&slruCheck{}).Scrape(context.Background(), maintenanceTarget(conn))
		require.NoError(t, err)
		require.Equal(t, 1, conn.releaseCalls)
		return r
	}
	old := scrape("CommitTs", "MultiXactMember", "MultiXactOffset", "Notify", "Serial", "Subtrans", "Xact", "other")
	current := scrape("commit_timestamp", "multixact_member", "multixact_offset", "notify", "serializable", "subtransaction", "transaction", "other")
	require.Len(t, old.Metrics, 8*len(slruCounters))
	require.Equal(t, metricsByKey(current), metricsByKey(old))
	m := metricsByKey(current)
	require.Equal(t, 102.0, m["pg_slru_blks_hit_total|slru=multixact_offset"])
	require.Equal(t, 10.0, m["pg_slru_blks_read_total|slru=transaction"])
	for _, metric := range old.Metrics {
		require.Equal(t, pgtype.KindCounter, metric.Kind)
	}
}

func TestWALDirectory_Sizes(t *testing.T) {
	conn := &mockConn{queryRowFunc: func(_ context.Context, sql string, _ ...any) pgx.Row {
		require.Equal(t, walDirectoryQuery, sql)
		return &mockRow{vals: []any{float64(12), float64(12 << 24)}}
	}}
	r, err := (&walDirectoryCheck{}).Scrape(context.Background(), maintenanceTarget(conn))
	require.NoError(t, err)
	require.Equal(t, map[string]float64{"pg_wal_dir_bytes|": 12 << 24, "pg_wal_dir_files|": 12}, metricsByKey(r))
}

// Without EXECUTE on pg_ls_waldir the metrics are left out; any other error
// fails the scrape.
func TestWALDirectory_PermissionDeniedIsNotAFailure(t *testing.T) {
	for code, fails := range map[string]bool{pgInsufficientPrivilege: false, pgUndefinedFunction: false, "57014": true} {
		conn := &mockConn{queryRowFunc: func(context.Context, string, ...any) pgx.Row {
			return &mockRow{err: &pgconn.PgError{Code: code}}
		}}
		r, err := (&walDirectoryCheck{}).Scrape(context.Background(), maintenanceTarget(conn))
		if fails {
			require.Error(t, err, code)
			continue
		}
		require.NoError(t, err, code)
		require.Empty(t, r.Metrics, code)
	}
}

func scrapeRecoveryForTest(t *testing.T, inRecovery bool, pause any, conflicts, prefetch [][]any) Result {
	t.Helper()
	conn := &mockConn{
		queryRowFunc: func(_ context.Context, sql string, _ ...any) pgx.Row {
			require.Equal(t, recoveryStateQuery, sql)
			return &mockRow{vals: []any{inRecovery, pause}}
		},
		queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			require.True(t, inRecovery, "a primary is asked nothing more")
			switch sql {
			case recoveryConflictsQuery:
				return &mockRows{rows: conflicts}, nil
			case recoveryPrefetchQuery:
				return &mockRows{rows: prefetch}, nil
			}
			t.Fatalf("unexpected query %q", sql)
			return nil, nil
		},
	}
	r, err := (&recoveryCheck{}).Scrape(context.Background(), maintenanceTarget(conn))
	require.NoError(t, err)
	require.Equal(t, 1, conn.releaseCalls)
	return r
}

func TestRecovery_PrimaryReportsNothing(t *testing.T) {
	require.Empty(t, scrapeRecoveryForTest(t, false, nil, nil, nil).Metrics)
}

// 15 has no confl_active_logicalslot; 16 and later do. Both rows give one
// counter per reason they carry.
func TestRecovery_StandbyConflictsPauseAndPrefetch(t *testing.T) {
	r := scrapeRecoveryForTest(t, true, "paused", [][]any{
		{"app", `{"datid": 5, "datname": "app", "confl_tablespace": 0, "confl_lock": 3, "confl_snapshot": 7, "confl_bufferpin": 0, "confl_deadlock": 0, "confl_active_logicalslot": 1, "stats_reset": null}`},
		{"old", `{"datid": 6, "datname": "old", "confl_tablespace": 0, "confl_lock": 0, "confl_snapshot": 2, "confl_bufferpin": 0, "confl_deadlock": 0}`},
	}, [][]any{
		{`{"stats_reset": "2026-10-01T00:00:00Z", "prefetch": 40, "hit": 900, "skip_init": 5, "skip_new": 1, "skip_fpw": 50, "skip_rep": 4, "wal_distance": 8192, "block_distance": 3, "io_depth": 2}`},
	})
	m := metricsByKey(r)
	require.Equal(t, 1.0, m["pg_recovery_replay_paused|"])
	conflict := func(db, reason string) float64 {
		v, ok := m["pg_recovery_conflicts_total|"+pgtype.CanonicalLabels(map[string]string{"database": db, "reason": reason})]
		require.True(t, ok, "%s %s", db, reason)
		return v
	}
	require.Equal(t, 3.0, conflict("app", "lock"))
	require.Equal(t, 7.0, conflict("app", "snapshot"))
	require.Equal(t, 1.0, conflict("app", "active_logicalslot"))
	require.Equal(t, 2.0, conflict("old", "snapshot"))
	_, ok := m["pg_recovery_conflicts_total|"+pgtype.CanonicalLabels(map[string]string{"database": "old", "reason": "active_logicalslot"})]
	require.False(t, ok)
	require.Equal(t, 900.0, m["pg_recovery_prefetch_blocks_total|outcome=hit"])
	require.Equal(t, 50.0, m["pg_recovery_prefetch_blocks_total|outcome=skip_fpw"])
	require.Equal(t, 8192.0, m["pg_recovery_prefetch_wal_distance_bytes|"])
	require.Equal(t, 2.0, m["pg_recovery_prefetch_io_depth|"])
}

func TestRecovery_ReplayRunning(t *testing.T) {
	for state, want := range map[string]float64{"not paused": 0, "pause requested": 1, "paused": 1} {
		m := metricsByKey(scrapeRecoveryForTest(t, true, state, nil, nil))
		require.Equal(t, want, m["pg_recovery_replay_paused|"], state)
	}
}

func TestDatabaseSize_OneGaugePerDatabase(t *testing.T) {
	conn := &mockConn{queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
		require.Equal(t, databaseSizeQuery, sql)
		return &mockRows{rows: [][]any{{"app", int64(8 << 20)}, {"postgres", int64(7 << 20)}}}, nil
	}}
	r, err := (&databaseSizeCheck{}).Scrape(context.Background(), maintenanceTarget(conn))
	require.NoError(t, err)
	require.Equal(t, 1, conn.releaseCalls)
	require.Equal(t, map[string]float64{
		"pg_database_size_bytes|database=app":      8 << 20,
		"pg_database_size_bytes|database=postgres": 7 << 20,
	}, metricsByKey(r))
	for _, m := range r.Metrics {
		require.Equal(t, pgtype.KindGauge, m.Kind)
	}
}
