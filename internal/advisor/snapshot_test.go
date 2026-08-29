package advisor

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSnapshot_MetricHelperUsesCanonicalLabels(t *testing.T) {
	s := &Snapshot{Metrics: map[string]map[string]float64{
		"host_load1": {"a=1\x1fb=2": 3.5},
	}}
	v, ok := s.Metric("host_load1", map[string]string{"b": "2", "a": "1"})
	require.True(t, ok)
	require.Equal(t, 3.5, v)
}

func TestSnapshot_MetricMissingReturnsFalse(t *testing.T) {
	s := &Snapshot{Metrics: map[string]map[string]float64{}}
	_, ok := s.Metric("missing", nil)
	require.False(t, ok)
}
