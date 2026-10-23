package harness

import (
	"fmt"
	"regexp"
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
	Logf(format string, args ...any)
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
	// AllowedCheckErrors names checks that may fail every scrape of a
	// scenario — a permission tier deliberately reduced below what the check
	// needs, an extension deliberately absent. It is not needed for a check
	// whose target is only briefly disturbed; see the rule below.
	//
	// The rule the budget enforces is not "no check ever errored". A scenario
	// that fails over a primary, partitions a link or restarts a container
	// makes every check against that instance report one error, and that is
	// the product behaving correctly. The rule is that a check which errored
	// must also have succeeded at least once: a check whose every scrape
	// failed produced no data at all, which is exactly how stat_statements
	// failed invisibly for months — wire.Result.Error was carried from the
	// agent and never read by anything.
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

	// The loops below judge only the samples they find, so an exposition
	// without the family would pass them all. That is what an empty body, or
	// an HTML page answered with 200, looks like; it is not evidence that the
	// budget holds. The family's TYPE line is the evidence: metrics_handler.go
	// writes it whether or not any instance reports yet. Requiring a sample
	// instead would fail every replica that has just restarted or has never
	// pushed, which is a true state of the system, not a broken budget.
	if !hasMetricFamily(exposition, "pglens_series_total", "gauge") {
		t.Errorf("I-8: /metrics has no pglens_series_total family, so the cardinality budget was not evaluated")
	} else if len(parseLabelledMetric(exposition, "pglens_series_total")) == 0 {
		// Passing here is true of an instance that has just restarted, but it
		// judged nothing. Say so, so a green run does not read as a measured one.
		t.Logf("I-8: the exposition declares pglens_series_total but reports no series yet; the budget held on no samples")
	}
	for _, sample := range parseLabelledMetric(exposition, "pglens_series_total") {
		if budget.MaxSeriesPerInstance > 0 && sample.value > budget.MaxSeriesPerInstance {
			t.Errorf("I-8: cardinality budget exceeded: instance %s reports %.0f series, budget is %.0f",
				sample.label, sample.value, budget.MaxSeriesPerInstance)
		}
	}

	// The counters are kept per instance: a check that works on one instance and
	// fails on every scrape of another is broken there, whatever the first does.
	ok := make(map[checkSeries]float64)
	for _, sample := range parseCheckMetric(exposition, "pglens_check_ok_total") {
		ok[sample.series()] += sample.value
	}
	for _, sample := range parseCheckMetric(exposition, "pglens_check_error_total") {
		if sample.value == 0 || budget.AllowedCheckErrors[sample.check] {
			continue
		}
		if ok[sample.series()] > 0 {
			// Errored and recovered: a fault window, not a broken check.
			continue
		}
		if sample.value < minErrorsToJudgeACheck {
			// One failed scrape and nothing else is not evidence. A check
			// whose only scrape of the scenario landed inside a deliberate
			// fault window looks exactly like a check that has never worked,
			// and the counters cannot tell them apart. Seen on SYS-UI-001:
			// "io" errored once, never scraped again, and the same scenario
			// had passed on the run before. A check that is genuinely broken
			// fails repeatedly — that is the case this invariant is for.
			t.Logf("check %q on instance %s errored once and never succeeded; too little evidence to judge, not failing the scenario", sample.check, sample.instance)
			continue
		}
		t.Errorf("check %q on instance %s failed every one of its %.0f scrape(s) and never once succeeded; a check that cannot scrape produces no data at all and must fail the scenario that relied on it",
			sample.check, sample.instance, sample.value)
	}
}

// minErrorsToJudgeACheck is how many failed scrapes, with no successful one,
// it takes before a check is called broken. See the comment at its use.
const minErrorsToJudgeACheck = 2

// hasMetricFamily reports whether the exposition declares the family with the
// given TYPE line, e.g. `# TYPE pglens_series_total gauge`.
func hasMetricFamily(exposition, name, kind string) bool {
	want := "# TYPE " + name + " " + kind
	for _, line := range strings.Split(exposition, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

// labelledSample is one `name{label="value"} number` line.
type labelledSample struct {
	label string
	value float64
}

// checkSeries is the (instance, check) pair a check counter is kept for.
type checkSeries struct {
	instance string
	check    string
}

// checkSample is one `name{instance_id="...",check="..."} number` line.
type checkSample struct {
	instance string
	check    string
	value    float64
}

func (s checkSample) series() checkSeries {
	return checkSeries{instance: s.instance, check: s.check}
}

// parseCheckMetric pulls every sample of one per-instance check counter family
// out of a Prometheus text exposition. Like parseLabelledMetric it is minimal:
// the exposition has this one fixed shape, and a line that does not match it is
// skipped rather than guessed at.
func parseCheckMetric(exposition, name string) []checkSample {
	line := regexp.MustCompile(`^` + regexp.QuoteMeta(name) + `\{instance_id="([^"]*)",check="([^"]*)"\} (\S+)$`)
	var out []checkSample
	for _, text := range strings.Split(exposition, "\n") {
		m := line.FindStringSubmatch(strings.TrimSpace(text))
		if m == nil {
			continue
		}
		value, err := strconv.ParseFloat(m[3], 64)
		if err != nil {
			continue
		}
		out = append(out, checkSample{instance: m[1], check: m[2], value: value})
	}
	return out
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
