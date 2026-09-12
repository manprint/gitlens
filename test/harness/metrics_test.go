package harness

import (
	"strings"
	"testing"
)

// spyT records failures instead of failing, so a test can prove the invariant
// both fires and stays quiet at the right times.
type spyT struct {
	errors []string
	fatals []string
}

func (s *spyT) Helper() {}
func (s *spyT) Errorf(format string, args ...any) {
	s.errors = append(s.errors, sprintfLike(format, args...))
}
func (s *spyT) Fatalf(format string, args ...any) {
	s.fatals = append(s.fatals, sprintfLike(format, args...))
}

const healthyExposition = `# HELP pglens_series_total distinct metric series currently tracked for the instance (I-8 cardinality budget)
# TYPE pglens_series_total gauge
pglens_series_total{instance_id="11111111-1111-1111-1111-111111111111"} 412
pglens_series_total{instance_id="22222222-2222-2222-2222-222222222222"} 88
# HELP pglens_check_error_total check scrapes that reported an error, by check name
# TYPE pglens_check_error_total counter
pglens_check_error_total{check="stat_statements"} 0
# HELP pglens_check_ok_total check scrapes that completed without an error, by check name
# TYPE pglens_check_ok_total counter
pglens_check_ok_total{check="stat_statements"} 14
# HELP pglens_agent_clock_skew_seconds most recently observed agent/server clock skew
# TYPE pglens_agent_clock_skew_seconds gauge
pglens_agent_clock_skew_seconds 0
`

func TestAssertMetricInvariants_PassesOnAHealthyExposition(t *testing.T) {
	spy := &spyT{}
	AssertMetricInvariants(spy, healthyExposition, DefaultMetricBudget())
	if len(spy.errors) != 0 {
		t.Errorf("a healthy exposition failed: %s", strings.Join(spy.errors, "; "))
	}
}

// I-8 is the invariant the suite documented as unreachable for the whole life
// of the project, because the counter behind it did not exist. It exists now.
func TestAssertMetricInvariants_FailsOnCardinalityBlowup(t *testing.T) {
	spy := &spyT{}
	budget := DefaultMetricBudget()
	budget.MaxSeriesPerInstance = 100
	AssertMetricInvariants(spy, healthyExposition, budget)
	if len(spy.errors) != 1 {
		t.Fatalf("expected exactly the one over-budget instance to fail, got %d: %v", len(spy.errors), spy.errors)
	}
	if !strings.Contains(spy.errors[0], "I-8") || !strings.Contains(spy.errors[0], "412") {
		t.Errorf("failure does not name the invariant and the offending value: %s", spy.errors[0])
	}
}

// wire.Result.Error was carried from the agent and read by nothing for
// months, which is how stat_statements failed every single scrape invisibly.
// The shape that proves it is "errored, and never once succeeded".
func TestAssertMetricInvariants_FailsOnACheckThatNeverSucceeds(t *testing.T) {
	exposition := strings.NewReplacer(
		`pglens_check_error_total{check="stat_statements"} 0`,
		`pglens_check_error_total{check="stat_statements"} 37`,
		`pglens_check_ok_total{check="stat_statements"} 14`,
		`pglens_check_ok_total{check="settings"} 14`,
	).Replace(healthyExposition)

	spy := &spyT{}
	AssertMetricInvariants(spy, exposition, DefaultMetricBudget())
	if len(spy.errors) != 1 {
		t.Fatalf("a check erroring on every scrape did not fail the scenario: %v", spy.errors)
	}
	if !strings.Contains(spy.errors[0], "stat_statements") || !strings.Contains(spy.errors[0], "37") {
		t.Errorf("failure does not name the check and the count: %s", spy.errors[0])
	}
}

// The counterpart, and the reason the success counter had to exist at all: a
// scenario that fails over a primary, partitions a link or restarts a
// container makes every check against that instance report one error. Holding
// the suite to "no check ever errored" fails every fault-injection scenario in
// it — which is exactly what happened on the first run after this invariant
// was wired up, on SYS-UI-001, with eleven checks each reporting one error and
// the browser assertions all green.
func TestAssertMetricInvariants_ToleratesACheckThatErroredAndRecovered(t *testing.T) {
	exposition := strings.Replace(healthyExposition,
		`pglens_check_error_total{check="stat_statements"} 0`,
		`pglens_check_error_total{check="stat_statements"} 1`, 1)

	spy := &spyT{}
	AssertMetricInvariants(spy, exposition, DefaultMetricBudget())
	if len(spy.errors) != 0 {
		t.Errorf("a check that errored during a fault window and recovered failed the scenario: %v", spy.errors)
	}
}

// A scenario that deliberately breaks a check for its whole duration — a
// permission tier below what the check needs, an absent extension — must be
// able to say so, because there is no success to recover to.
func TestAssertMetricInvariants_HonoursAllowedCheckErrors(t *testing.T) {
	exposition := strings.NewReplacer(
		`pglens_check_error_total{check="stat_statements"} 0`,
		`pglens_check_error_total{check="stat_statements"} 5`,
		`pglens_check_ok_total{check="stat_statements"} 14`,
		`pglens_check_ok_total{check="settings"} 14`,
	).Replace(healthyExposition)

	budget := DefaultMetricBudget()
	budget.AllowedCheckErrors = map[string]bool{"stat_statements": true}
	spy := &spyT{}
	AssertMetricInvariants(spy, exposition, budget)
	if len(spy.errors) != 0 {
		t.Errorf("an explicitly allowed check error failed the scenario: %v", spy.errors)
	}
}

func TestParseLabelledMetric(t *testing.T) {
	got := parseLabelledMetric(healthyExposition, "pglens_series_total")
	if len(got) != 2 {
		t.Fatalf("parsed %d samples, want 2: %+v", len(got), got)
	}
	if got[0].label != "11111111-1111-1111-1111-111111111111" || got[0].value != 412 {
		t.Errorf("first sample = %+v", got[0])
	}
	if n := len(parseLabelledMetric(healthyExposition, "pglens_missing_total")); n != 0 {
		t.Errorf("an absent family parsed %d samples, want 0", n)
	}
	// HELP/TYPE lines and unlabelled families must never be mistaken for samples.
	if n := len(parseLabelledMetric(healthyExposition, "pglens_agent_clock_skew_seconds")); n != 0 {
		t.Errorf("an unlabelled family parsed %d samples, want 0", n)
	}
	// A malformed value is skipped rather than parsed as zero, which would
	// silently turn a broken exposition into a passing budget check.
	if n := len(parseLabelledMetric(`pglens_series_total{instance_id="x"} not-a-number`, "pglens_series_total")); n != 0 {
		t.Errorf("a malformed sample parsed %d samples, want 0", n)
	}
}
