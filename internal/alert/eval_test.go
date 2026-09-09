package alert

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func evalRule() Rule {
	return Rule{ID: "r", Severity: SeverityWarning, Scope: ScopeInstance, Metric: "m", Comparator: GT, Threshold: 1, For: time.Minute}
}
func evalSample(t time.Time) Sample {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	return Sample{InstanceID: &id, Value: 2, TS: t}
}
func TestCompare_AllOperators(t *testing.T) {
	for _, x := range []struct {
		c    Comparator
		v, w float64
		ok   bool
	}{{GT, 2, 1, true}, {GE, 1, 1, true}, {LT, 0, 1, true}, {LE, 1, 1, true}, {EQ, 1, 1, true}, {NE, 2, 1, true}} {
		require.Equal(t, x.ok, Compare(x.v, x.c, x.w))
	}
}
func TestCompare_UnknownReturnsFalse(t *testing.T) { require.False(t, Compare(2, "unknown", 1)) }
func TestStep_FalseWithoutPriorIsNone(t *testing.T) {
	e := NewEvaluator()
	s := evalSample(time.Unix(1, 0))
	s.Value = 0
	a, tr := e.Step(evalRule(), s, s.TS)
	require.Nil(t, a)
	require.Equal(t, TransitionNone, tr)
}
func TestForget_KeepsRecentKeys(t *testing.T) {
	e := NewEvaluator()
	n := time.Unix(1, 0)
	e.Step(evalRule(), evalSample(n), n)
	require.Equal(t, 0, e.Forget(n))
}
func TestStep_ForZeroFiresImmediately(t *testing.T) {
	r := evalRule()
	r.For = 0
	e := NewEvaluator()
	a, tr := e.Step(r, evalSample(time.Unix(1, 0)), time.Unix(2, 0))
	require.Equal(t, StateFiring, a.State)
	require.Equal(t, TransitionFired, tr)
}
func TestStep_PendingUntilForElapses(t *testing.T) {
	e := NewEvaluator()
	n := time.Unix(100, 0)
	a, tr := e.Step(evalRule(), evalSample(n), n)
	require.Equal(t, StatePending, a.State)
	require.Equal(t, TransitionOpened, tr)
	a, tr = e.Step(evalRule(), evalSample(n), n.Add(time.Minute))
	require.Equal(t, StateFiring, a.State)
	require.Equal(t, TransitionFired, tr)
}
func TestStep_FiresExactlyAtBoundary(t *testing.T) {
	e := NewEvaluator()
	n := time.Unix(100, 0)
	e.Step(evalRule(), evalSample(n), n)
	a, _ := e.Step(evalRule(), evalSample(n), n.Add(time.Minute))
	require.Equal(t, StateFiring, a.State)
}
func TestStep_StartedAtIsFirstTrueSample(t *testing.T) {
	e := NewEvaluator()
	n := time.Unix(100, 0)
	a, _ := e.Step(evalRule(), evalSample(n), n.Add(time.Second))
	require.Equal(t, n, a.StartedAt)
}
func TestStep_FalseCancelsPending(t *testing.T) {
	e := NewEvaluator()
	r := evalRule()
	n := time.Unix(1, 0)
	e.Step(r, evalSample(n), n)
	s := evalSample(n)
	s.Value = 0
	a, tr := e.Step(r, s, n.Add(time.Second))
	require.Nil(t, a)
	require.Equal(t, TransitionCancelled, tr)
}
func TestStep_FalseResolvesFiring(t *testing.T) {
	e := NewEvaluator()
	r := evalRule()
	r.For = 0
	n := time.Unix(1, 0)
	e.Step(r, evalSample(n), n)
	s := evalSample(n)
	s.Value = 0
	a, tr := e.Step(r, s, n.Add(time.Second))
	require.Equal(t, StateResolved, a.State)
	require.Equal(t, TransitionResolved, tr)
}
func TestStep_RefireGetsNewStartedAt(t *testing.T) {
	e := NewEvaluator()
	r := evalRule()
	r.For = 0
	n := time.Unix(1, 0)
	e.Step(r, evalSample(n), n)
	s := evalSample(n)
	s.Value = 0
	e.Step(r, s, n.Add(time.Second))
	a, _ := e.Step(r, evalSample(n.Add(2*time.Second)), n.Add(2*time.Second))
	require.NotEqual(t, a.StartedAt, n)
}
func TestStep_FlapDoesNotCarryFirstTrue(t *testing.T) {
	e := NewEvaluator()
	r := evalRule()
	n := time.Unix(1, 0)
	e.Step(r, evalSample(n), n)
	s := evalSample(n)
	s.Value = 0
	e.Step(r, s, n.Add(time.Second))
	a, _ := e.Step(r, evalSample(n.Add(2*time.Second)), n.Add(2*time.Second))
	require.Equal(t, n.Add(2*time.Second), a.StartedAt)
}
func TestForget_DropsStaleKeys(t *testing.T) {
	e := NewEvaluator()
	n := time.Unix(1, 0)
	e.Step(evalRule(), evalSample(n), n)
	require.Equal(t, 1, e.Forget(n.Add(time.Second)))
	require.Equal(t, 0, e.Forget(n.Add(time.Second)))
}

// TestForget_NeverDropsFiringState — the evaluator state machine is the only
// record that an episode is currently firing. Forgetting it made every alert
// that outlived the cutoff re-fire (and re-notify) as a new episode with a
// reset StartedAt, and never emit its resolved transition.
func TestForget_NeverDropsFiringState(t *testing.T) {
	e := NewEvaluator()
	r := evalRule()
	start := time.Unix(1, 0)

	e.Step(r, evalSample(start), start)
	fired := start.Add(2 * time.Minute)
	a, tr := e.Step(r, evalSample(fired), fired)
	require.Equal(t, TransitionFired, tr)
	require.Equal(t, start, a.StartedAt)

	// Hours later, the condition is still true.
	require.Equal(t, 0, e.Forget(fired.Add(24*time.Hour)), "a firing episode must never be forgotten")

	later := fired.Add(25 * time.Hour)
	a, tr = e.Step(r, evalSample(later), later)
	require.Equal(t, TransitionNone, tr, "the episode must continue, not re-fire")
	require.Equal(t, start, a.StartedAt, "StartedAt must keep the original episode start")

	// And it still resolves.
	clear := evalSample(later)
	clear.Value = 0
	a, tr = e.Step(r, clear, later)
	require.Equal(t, TransitionResolved, tr)
	require.Equal(t, StateResolved, a.State)
}
