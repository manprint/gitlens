package harness

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// TestingT is the subset of *testing.T the invariant checks need. Accepting
// this instead of the concrete type lets a test prove "this check would have
// failed" via a substitute that records the call instead of aborting the
// test that is verifying detection works (see test/e2e/invariants_test.go).
type TestingT interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
}

var _ TestingT = (*testing.T)(nil)

// MetricBudget is what AssertServerMetricInvariants holds the server to.
type MetricBudget struct {
	// MaxSeriesPerInstance is the I-8 cardinality ceiling: how many distinct
	// metric series one instance may be reporting at the end of a scenario.
	// The agent's own per-check budgets (cardinality.Options.MaxKeys, 200 per
	// check) bound each check; this bounds their sum, which is the number
	// that actually decides whether the server's memory and the metrics table
	// stay finite under a churning workload.
	MaxSeriesPerInstance float64
	// AllowedCheckErrors names checks that are legitimately allowed to report
	// errors during a scenario (a target deliberately killed, a permission
	// tier deliberately reduced). Every other check reporting an error is a
	// regression: wire.Result.Error was carried from the agent and never read
	// by anything for the whole life of the project, which is how
	// stat_statements failed every single scrape, invisibly, for months.
	AllowedCheckErrors map[string]bool
}

// DefaultMetricBudget is the budget a healthy scenario must satisfy.
func DefaultMetricBudget() MetricBudget {
	return MetricBudget{
		MaxSeriesPerInstance: 2000,
		AllowedCheckErrors:   map[string]bool{},
	}
}

// AssertMetricInvariants is the pure half of AssertServerMetricInvariants,
// split out so it can be unit tested against a fixture exposition instead of
// a running compose stack.
func AssertMetricInvariants(t TestingT, exposition string, budget MetricBudget) {
	t.Helper()

	for _, sample := range parseLabelledMetric(exposition, "pglens_series_total") {
		if budget.MaxSeriesPerInstance > 0 && sample.value > budget.MaxSeriesPerInstance {
			t.Errorf("I-8: cardinality budget exceeded: instance %s reports %.0f series, budget is %.0f",
				sample.label, sample.value, budget.MaxSeriesPerInstance)
		}
	}

	for _, sample := range parseLabelledMetric(exposition, "pglens_check_error_total") {
		if sample.value == 0 || budget.AllowedCheckErrors[sample.label] {
			continue
		}
		t.Errorf("check %q reported %.0f scrape error(s); a check that cannot scrape produces no data at all and must fail the scenario that relied on it",
			sample.label, sample.value)
	}
}

// labelledSample is one `name{label="value"} number` line.
type labelledSample struct {
	label string
	value float64
}

// parseLabelledMetric pulls every sample of one single-label metric family out
// of a Prometheus text exposition. Deliberately minimal: the exposition is
// written by metrics_handler.go in this same repository, in one fixed shape,
// so a full parser (and the dependency it would need) buys nothing.
func parseLabelledMetric(exposition, name string) []labelledSample {
	var out []labelledSample
	prefix := name + "{"
	for _, line := range strings.Split(exposition, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		closeIdx := strings.Index(line, "}")
		if closeIdx < 0 {
			continue
		}
		labels := line[len(prefix):closeIdx]
		value, err := strconv.ParseFloat(strings.TrimSpace(line[closeIdx+1:]), 64)
		if err != nil {
			continue
		}
		label := labels
		if eq := strings.Index(labels, "="); eq >= 0 {
			label = strings.Trim(strings.TrimSpace(labels[eq+1:]), `"`)
		}
		out = append(out, labelledSample{label: label, value: value})
	}
	return out
}

// sprintfLike is fmt.Sprintf, kept behind a name so the assertion helpers
// above stay readable in stack traces.
func sprintfLike(format string, args ...any) string { return fmt.Sprintf(format, args...) }
