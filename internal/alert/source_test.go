package alert

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestMetricSource_RoutesToTypedTable(t *testing.T) {
	table, column, err := metricTable("pg_replication_lag_seconds")
	require.NoError(t, err)
	require.Equal(t, "metrics_replication", table)
	require.Equal(t, "replay_lag_sec", column)
}

func TestMetricSource_ClusterRuleWithoutAggregationErrors(t *testing.T) {
	s := &metricSource{interval: time.Minute}
	_, err := s.Samples(t.Context(), Rule{ID: "unknown", Scope: ScopeCluster, Metric: "x"}, time.Now())
	require.EqualError(t, err, "unsupported cluster alert rule aggregation: unknown")
}

func TestLookbackWindow(t *testing.T) {
	require.Equal(t, 2*time.Minute, lookback(30*time.Second))
	require.Equal(t, 4*time.Minute, lookback(2*time.Minute))
}

func TestSourcesRejectWrongRuleKind(t *testing.T) {
	_, err := (&metricSource{}).Samples(t.Context(), Rule{ID: "event", EventType: "x"}, time.Now())
	require.Error(t, err)
	_, err = (&eventSource{}).Samples(t.Context(), Rule{ID: "metric", Metric: "x"}, time.Now())
	require.Error(t, err)
}

func TestSourcesConstructorsAndNilDatabase(t *testing.T) {
	metric := NewMetricSource(nil, time.Minute)
	event := NewEventSource(nil, time.Minute)
	require.Equal(t, "metric", metric.Kind())
	require.Equal(t, "event", event.Kind())
}

// TestMetricTable_MapsEachReplicationMetricToItsOwnColumn — the router used to
// answer replay_lag_sec for every pg_replication_* name, so a rule on the
// write or flush lag silently compared the replay lag instead.
func TestMetricTable_MapsEachReplicationMetricToItsOwnColumn(t *testing.T) {
	for metric, want := range map[string]string{
		"pg_replication_lag_seconds":         "replay_lag_sec",
		"pg_replication_replay_lag_seconds":  "replay_lag_sec",
		"pg_replication_write_lag_seconds":   "write_lag_sec",
		"pg_replication_flush_lag_seconds":   "flush_lag_sec",
		"pg_replication_write_lag_bytes":     "write_lag_bytes",
		"pg_replication_flush_lag_bytes":     "flush_lag_bytes",
		"pg_replication_replay_lag_bytes":    "replay_lag_bytes",
		"pg_replication_slot_retained_bytes": "slot_retained_bytes",
	} {
		table, column, err := metricTable(metric)
		require.NoError(t, err, metric)
		require.Equal(t, "metrics_replication", table, metric)
		require.Equal(t, want, column, metric)
	}
}

// TestMetricTable_UnknownReplicationMetricIsAnError — better a reported error
// than a number that is not the one the rule names.
func TestMetricTable_UnknownReplicationMetricIsAnError(t *testing.T) {
	_, _, err := metricTable("pg_replication_made_up_seconds")
	require.ErrorContains(t, err, "unknown replication metric")
}

func TestMetricTable_PlainMetricUsesTheGenericTable(t *testing.T) {
	table, column, err := metricTable("pg_connections_used_ratio")
	require.NoError(t, err)
	require.Equal(t, "metrics", table)
	require.Equal(t, "value", column)
}

func TestMetricSource_PerInstanceSamplesCarryIdentity(t *testing.T) {
	instance := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	ts := time.Unix(1000, 0).UTC()
	db := &fakeQueryer{results: []*fakeRows{{rows: [][]any{
		{int64(4), instance, "app", 0.93, ts},
	}}}}
	s := &metricSource{db: db, interval: time.Minute}

	got, err := s.Samples(t.Context(), Rule{ID: "conn.near_max", Scope: ScopeInstance, Metric: "pg_connections_used_ratio", Comparator: GT, Threshold: 0.8}, ts)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, int64(4), *got[0].ClusterID)
	require.Equal(t, instance, *got[0].InstanceID)
	require.Equal(t, "app", got[0].Datname)
	require.Equal(t, 0.93, got[0].Value)

	require.Len(t, db.calls, 1)
	require.Contains(t, db.calls[0].sql, "FROM metrics")
	require.Equal(t, "pg_connections_used_ratio", db.calls[0].args[0])
}

// The lag rules read a typed column of metrics_replication, not `metrics`.
func TestMetricSource_ReplicationRuleReadsItsOwnColumn(t *testing.T) {
	instance := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	ts := time.Unix(1000, 0).UTC()
	db := &fakeQueryer{results: []*fakeRows{{rows: [][]any{{int64(1), instance, "", 12.5, ts}}}}}
	s := &metricSource{db: db, interval: time.Minute}

	got, err := s.Samples(t.Context(), Rule{ID: "replica.lag_high", Scope: ScopeInstance, Metric: "pg_replication_write_lag_seconds", Comparator: GT, Threshold: 30}, ts)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, 12.5, got[0].Value)
	require.Contains(t, db.calls[0].sql, "write_lag_sec")
	require.Contains(t, db.calls[0].sql, "FROM metrics_replication")
	require.NotContains(t, db.calls[0].sql, "replay_lag_sec")
}

// TestMetricSource_UnknownReplicationMetricIsReported — the engine reports the
// rule as broken instead of silently comparing the wrong column.
func TestMetricSource_UnknownReplicationMetricIsReported(t *testing.T) {
	db := &fakeQueryer{}
	s := &metricSource{db: db, interval: time.Minute}
	_, err := s.Samples(t.Context(), Rule{ID: "custom", Scope: ScopeInstance, Metric: "pg_replication_nonsense"}, time.Unix(1, 0))
	require.ErrorContains(t, err, "unknown replication metric")
	require.Empty(t, db.calls, "no query must be issued for a metric that cannot be resolved")
}

// TestMetricSource_UpReadsInstanceLastSeen — `up` is not an ingest metric; it
// comes from instances.last_seen so every replica evaluates the same signal.
func TestMetricSource_UpReadsInstanceLastSeen(t *testing.T) {
	instance := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	lastSeen := time.Unix(500, 0).UTC()
	now := time.Unix(1000, 0).UTC()
	db := &fakeQueryer{results: []*fakeRows{{rows: [][]any{{int64(2), instance, "", 0.0, lastSeen}}}}}
	s := &metricSource{db: db, interval: time.Minute}

	got, err := s.Samples(t.Context(), Rule{ID: "agent_down", Scope: ScopeInstance, Metric: "up", Comparator: LT, Threshold: 1, For: 90 * time.Second}, now)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Contains(t, db.calls[0].sql, "FROM instances")
	// A stale instance is seeded at the rule boundary so agent_down does not
	// wait out its 90s a second time on top of the staleness threshold.
	require.Equal(t, now.Add(-90*time.Second), got[0].TS)
}

func TestMetricSource_ClusterRulesAggregatePerCluster(t *testing.T) {
	ts := time.Unix(1000, 0).UTC()
	lagging := &fakeQueryer{results: []*fakeRows{{rows: [][]any{
		{int64(1), 90.0, ts},
		{int64(2), 2.0, ts},
	}}}}
	got, err := (&metricSource{db: lagging, interval: time.Minute}).Samples(t.Context(),
		Rule{ID: "replica.all_standbys_lagging", Scope: ScopeCluster, Metric: "pg_replication_lag_seconds", Comparator: GT, Threshold: 30}, ts)
	require.NoError(t, err)
	require.Len(t, got, 2)
	for _, s := range got {
		require.Nil(t, s.InstanceID, "a cluster-scoped sample must not be keyed per instance")
		require.NotNil(t, s.ClusterID)
	}
	require.Contains(t, lagging.calls[0].sql, "min(replay_lag_sec)")

	sync := &fakeQueryer{results: []*fakeRows{{rows: [][]any{{int64(1), 0.0, ts}}}}}
	got, err = (&metricSource{db: sync, interval: time.Minute}).Samples(t.Context(),
		Rule{ID: "replica.no_sync_standby", Scope: ScopeCluster, Metric: "pg_sync_standby_count", Comparator: LT, Threshold: 1}, ts)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, 0.0, got[0].Value)
	require.Contains(t, sync.calls[0].sql, "sync_state IN ('sync', 'quorum')")
}

func TestMetricSource_QueryErrorsAreWrapped(t *testing.T) {
	boom := errors.New("statement timeout")
	for name, tc := range map[string]struct {
		rule Rule
		want string
	}{
		"per instance": {Rule{ID: "conn.near_max", Scope: ScopeInstance, Metric: "pg_connections_used_ratio"}, "query metric"},
		"staleness":    {Rule{ID: "agent_down", Scope: ScopeInstance, Metric: "up"}, "query staleness"},
		"cluster":      {Rule{ID: "replica.no_sync_standby", Scope: ScopeCluster, Metric: "pg_sync_standby_count"}, "query cluster metric"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := (&metricSource{db: &fakeQueryer{errs: []error{boom}}, interval: time.Minute}).Samples(t.Context(), tc.rule, time.Unix(1, 0))
			require.ErrorContains(t, err, tc.want)
			require.ErrorIs(t, err, boom)
		})
	}
}

func TestEventSource_QueriesEventsByType(t *testing.T) {
	ts := time.Unix(1000, 0).UTC()
	db := &fakeQueryer{results: []*fakeRows{{rows: [][]any{}}}}
	got, err := (&eventSource{db: db, interval: time.Minute}).Samples(t.Context(), Rule{ID: "failover_detected", EventType: "failover_detected"}, ts)
	require.NoError(t, err)
	require.Empty(t, got)
	require.Contains(t, db.calls[0].sql, "FROM events")
	require.Equal(t, "failover_detected", db.calls[0].args[0])
}

func TestEventSource_QueryErrorIsWrapped(t *testing.T) {
	boom := errors.New("relation does not exist")
	_, err := (&eventSource{db: &fakeQueryer{errs: []error{boom}}, interval: time.Minute}).Samples(t.Context(), Rule{ID: "x", EventType: "y"}, time.Unix(1, 0))
	require.ErrorContains(t, err, "query event y")
	require.ErrorIs(t, err, boom)
}
