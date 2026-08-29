package alert

import (
	"testing"
	"time"

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
