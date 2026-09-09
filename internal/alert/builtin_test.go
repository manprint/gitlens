package alert

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuiltin_AllValidate(t *testing.T) {
	for _, r := range Builtin() {
		require.NoError(t, r.Validate())
	}
}
func TestBuiltin_IDsAreUnique(t *testing.T) {
	m := map[string]bool{}
	for _, r := range Builtin() {
		require.False(t, m[r.ID])
		m[r.ID] = true
	}
}
func TestBuiltin_AllAreTier0(t *testing.T) {
	for _, r := range Builtin() {
		require.Equal(t, Tier0, r.Tier)
		require.True(t, r.Enabled)
	}
}
func TestBuiltin_OrderIsDeterministic(t *testing.T) {
	a, b := Builtin(), Builtin()
	require.Equal(t, a, b)
}
func TestBuiltin_ReturnsFreshSlice(t *testing.T) {
	a := Builtin()
	a[0].ID = "x"
	require.NotEqual(t, "x", Builtin()[0].ID)
}
func TestBuiltin_CoversDocumentedCatalogue(t *testing.T) {
	want := []string{"agent_down", "instance_unreachable", "check_failing", "no_primary_in_cluster", "agent_buffer_full", "clock_skew", "cardinality_budget_exceeded", "failover_detected", "split_brain_detected", "slot_inactive"}
	got := []string{}
	for _, r := range Builtin() {
		got = append(got, r.ID)
	}
	require.Equal(t, want, got)
}

// builtinMetricProducers names, for every metric a Tier 0 rule compares, the
// file that actually produces it.
//
// Four of these rules used to reference metrics nothing in the system ever
// wrote: no check emits a pglens_* metric, so check_failing,
// agent_buffer_full, clock_skew and cardinality_budget_exceeded were
// structurally unable to fire whatever happened in the field, while looking
// perfectly configured in the API and the docs. The map is the contract; the
// test below is what keeps it true.
var builtinMetricProducers = map[string]string{
	"up":                                "internal/alert/source_sql.go",
	"pglens_check_error_rate":           "internal/server/pipeline.go",
	"pglens_cardinality_truncated_rate": "internal/server/pipeline.go",
	"pglens_agent_clock_skew_seconds":   "internal/server/pipeline.go",
	"pglens_samples_dropped_rate":       "cmd/pglens-agent/run.go",
}

func TestBuiltin_EveryRuleMetricHasAProducer(t *testing.T) {
	for _, r := range Builtin() {
		if r.Metric == "" {
			continue // event-driven rule
		}
		producer, ok := builtinMetricProducers[r.Metric]
		require.True(t, ok, "rule %s compares metric %s, which no known producer writes", r.ID, r.Metric)

		data, err := os.ReadFile(filepath.Join("..", "..", producer))
		require.NoError(t, err, "producer file for %s", r.Metric)
		require.Contains(t, string(data), r.Metric,
			"%s no longer produces %s (rule %s would silently stop firing)", producer, r.Metric, r.ID)
	}
}

func TestBuiltin_EveryEventRuleHasAnEmitter(t *testing.T) {
	emitters := map[string]string{
		"instance_unreachable":  "internal/server/staleness.go",
		"no_primary_in_cluster": "internal/server/staleness.go",
		"slot_inactive":         "internal/server/staleness.go",
		"failover_detected":     "internal/topology/engine.go",
		"split_brain_detected":  "internal/topology/engine.go",
	}
	for _, r := range Builtin() {
		if r.EventType == "" {
			continue
		}
		emitter, ok := emitters[r.EventType]
		require.True(t, ok, "rule %s waits for event %s, which nothing is known to emit", r.ID, r.EventType)
		data, err := os.ReadFile(filepath.Join("..", "..", emitter))
		require.NoError(t, err)
		require.Contains(t, string(data), r.EventType,
			"%s no longer emits %s (rule %s would silently stop firing)", emitter, r.EventType, r.ID)
	}
}
