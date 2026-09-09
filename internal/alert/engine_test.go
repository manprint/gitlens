package alert

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/clock"
)

type engineSource struct {
	kind    string
	samples []Sample
}

func (s engineSource) Kind() string { return s.kind }
func (s engineSource) Samples(context.Context, Rule, time.Time) ([]Sample, error) {
	return s.samples, nil
}

type engineStore struct {
	rules    []Rule
	silences []Silence
	alerts   []Alert
}

func (s *engineStore) Rules(context.Context) ([]Rule, error)                  { return s.rules, nil }
func (s *engineStore) Silences(context.Context, time.Time) ([]Silence, error) { return s.silences, nil }
func (s *engineStore) Upsert(_ context.Context, a Alert) error {
	s.alerts = append(s.alerts, a)
	return nil
}
func (s *engineStore) ClaimNotification(context.Context, string, string, string, Alert) (bool, error) {
	return true, nil
}
func (s *engineStore) MarkNotification(context.Context, string, string, string, bool, int, error) error {
	return nil
}
func (s *engineStore) Active(context.Context, Filter) ([]Alert, error) { return s.alerts, nil }

type engineNotifier struct{ alerts []Alert }

func (n *engineNotifier) Notify(_ context.Context, a Alert) error {
	n.alerts = append(n.alerts, a)
	return nil
}

func testEngine(rule Rule, source Source, store *engineStore, notifier *engineNotifier, now time.Time) *Engine {
	e := NewEngine(nil, clock.NewFake(now), store, notifier, []Source{source})
	store.rules = []Rule{rule}
	return e
}

func TestTick_NotifiesOnFire(t *testing.T) {
	now := time.Unix(100, 0)
	rule := Rule{ID: "custom", Enabled: true, Severity: SeverityWarning, Scope: ScopeInstance, Metric: "x", Comparator: GT, Threshold: 1, Summary: "x"}
	s := engineSource{kind: "metric", samples: []Sample{{Value: 2, TS: now}}}
	store, notifier := &engineStore{}, &engineNotifier{}
	require.NoError(t, testEngine(rule, s, store, notifier, now).Tick(context.Background()))
	require.Len(t, store.alerts, 1)
	require.Len(t, notifier.alerts, 1)
}

func TestTick_RestoresPersistedFiringAlert(t *testing.T) {
	now := time.Unix(100, 0)
	id := uuid.New()
	rule := Rule{ID: "custom", Enabled: true, Severity: SeverityWarning, Scope: ScopeInstance, Metric: "x", Comparator: GT, Threshold: 1, Summary: "x"}
	stored := Alert{Key: "custom/" + id.String() + "/", RuleID: rule.ID, Severity: rule.Severity, State: StateFiring, InstanceID: &id, StartedAt: now.Add(-time.Minute), LastEvalAt: now.Add(-time.Second), Summary: rule.Summary}
	store := &engineStore{alerts: []Alert{stored}}
	notifier := &engineNotifier{}
	e := testEngine(rule, engineSource{kind: "metric", samples: []Sample{{InstanceID: &id, Value: 2, TS: now}}}, store, notifier, now)

	require.NoError(t, e.Tick(context.Background()))
	require.Empty(t, notifier.alerts, "a newly elected engine must not re-fire a persisted episode")
}

func TestTick_NotifiesOnResolve(t *testing.T) {
	now := time.Unix(100, 0)
	rule := Rule{ID: "custom", Enabled: true, Severity: SeverityWarning, Scope: ScopeInstance, Metric: "x", Comparator: GT, Threshold: 1, Summary: "x"}
	store, notifier := &engineStore{}, &engineNotifier{}
	e := testEngine(rule, engineSource{kind: "metric", samples: []Sample{{Value: 2, TS: now}}}, store, notifier, now)
	require.NoError(t, e.Tick(context.Background()))
	e.sources = []Source{engineSource{kind: "metric", samples: []Sample{{Value: 0, TS: now.Add(time.Second)}}}}
	require.NoError(t, e.Tick(context.Background()))
	require.Len(t, notifier.alerts, 2)
	require.Equal(t, StateResolved, notifier.alerts[1].State)
}

func TestTick_SilencedAlertIsPersistedButNotNotified(t *testing.T) {
	now := time.Unix(100, 0)
	rule := Rule{ID: "custom", Enabled: true, Severity: SeverityWarning, Scope: ScopeInstance, Metric: "x", Comparator: GT, Threshold: 1, Summary: "x"}
	store, notifier := &engineStore{silences: []Silence{{Matchers: []Matcher{{Name: "rule_id", Value: "custom"}}, StartsAt: now.Add(-time.Second), EndsAt: now.Add(time.Second)}}}, &engineNotifier{}
	require.NoError(t, testEngine(rule, engineSource{kind: "metric", samples: []Sample{{Value: 2, TS: now}}}, store, notifier, now).Tick(context.Background()))
	require.Len(t, store.alerts, 1)
	require.True(t, store.alerts[0].Suppressed)
	require.Empty(t, notifier.alerts)
}

func TestTick_BuiltinWinsOverStoredRuleWithSameID(t *testing.T) {
	now := time.Unix(100, 0)
	stored := Rule{ID: "agent_down", Enabled: true, Severity: SeverityInfo, Scope: ScopeInstance, Metric: "up", Comparator: GT, Threshold: -100, Summary: "wrong"}
	store, notifier := &engineStore{rules: []Rule{stored}}, &engineNotifier{}
	e := NewEngine(nil, clock.NewFake(now), store, notifier, []Source{engineSource{kind: "metric", samples: []Sample{{Value: 0, TS: now}}}})
	require.NoError(t, e.Tick(context.Background()))
	require.Empty(t, store.alerts)
}

func TestTick_ForgetsStaleEvaluatorState(t *testing.T) {
	e := NewEngine(nil, clock.NewFake(time.Unix(0, 0)), nil, nil, nil)
	// For > 0, sampled once and never again: a pending episode that can never
	// fire and would otherwise pin its key in memory forever.
	r := Rule{ID: "r", Severity: SeverityWarning, Scope: ScopeInstance, Metric: "x", Comparator: GT, Threshold: 1, For: time.Minute}
	e.eval.Step(r, Sample{Value: 2, TS: time.Unix(-7200, 0)}, time.Unix(-7200, 0))
	require.NoError(t, e.Tick(context.Background()))
	require.Empty(t, e.eval.states)
}

// TestTick_KeepsFiringEvaluatorState — the cutoff must not reap a firing
// episode. It used to: an alert that had been firing for over an hour lost
// its state on every tick, so it was re-discovered as a new episode (re-fired
// and re-notified, StartedAt reset) and never emitted a resolved transition.
func TestTick_KeepsFiringEvaluatorState(t *testing.T) {
	e := NewEngine(nil, clock.NewFake(time.Unix(0, 0)), nil, nil, nil)
	r := Rule{ID: "r", Severity: SeverityWarning, Scope: ScopeInstance, Metric: "x", Comparator: GT, Threshold: 1}
	_, tr := e.eval.Step(r, Sample{Value: 2, TS: time.Unix(-7200, 0)}, time.Unix(-7200, 0))
	require.Equal(t, TransitionFired, tr)
	require.NoError(t, e.Tick(context.Background()))
	require.Len(t, e.eval.states, 1)
}

func TestStop_IsIdempotent(t *testing.T) {
	e := NewEngine(nil, clock.NewFake(time.Unix(0, 0)), nil, nil, nil)
	e.Stop()
	e.Stop()
}

func TestNewEngineWithInterval_NormalizesInputs(t *testing.T) {
	e := NewEngineWithInterval(nil, nil, 0, nil, nil, nil)
	require.Equal(t, defaultEngineInterval, e.interval)
	e = NewEngineWithInterval(nil, nil, 5*time.Second, nil, nil, nil)
	require.Equal(t, 5*time.Second, e.interval)
}

// failingRuleSource fails for one named rule and behaves normally for the rest.
type failingRuleSource struct {
	failRuleID string
	samples    []Sample
	calls      []string
}

func (s *failingRuleSource) Kind() string { return "metric" }
func (s *failingRuleSource) Samples(_ context.Context, r Rule, _ time.Time) ([]Sample, error) {
	s.calls = append(s.calls, r.ID)
	if r.ID == s.failRuleID {
		return nil, errors.New("relation \"pg_stat_broken\" does not exist")
	}
	return s.samples, nil
}

// TestTick_OneBrokenRuleDoesNotStopTheCycle — a rule whose query fails used
// to abort Tick at that point, so every rule after it in the list (builtin
// criticals included) silently stopped being evaluated for as long as the
// broken rule stayed enabled. The failure must be reported *and* the rest of
// the cycle must still run.
func TestTick_OneBrokenRuleDoesNotStopTheCycle(t *testing.T) {
	now := time.Unix(100, 0)
	broken := Rule{ID: "aaa_broken", Enabled: true, Severity: SeverityWarning, Scope: ScopeInstance, Metric: "x", Comparator: GT, Threshold: 1, Summary: "broken"}
	healthy := Rule{ID: "zzz_healthy", Enabled: true, Severity: SeverityCritical, Scope: ScopeInstance, Metric: "y", Comparator: GT, Threshold: 1, Summary: "healthy"}

	store := &engineStore{rules: []Rule{broken, healthy}}
	notifier := &engineNotifier{}
	source := &failingRuleSource{failRuleID: broken.ID, samples: []Sample{{Value: 2, TS: now}}}
	e := NewEngine(nil, clock.NewFake(now), store, notifier, []Source{source})

	err := e.Tick(context.Background())
	require.ErrorContains(t, err, "sample rule aaa_broken")
	require.Contains(t, source.calls, healthy.ID, "the healthy rule must still be evaluated")

	var fired []string
	for _, a := range notifier.alerts {
		fired = append(fired, a.RuleID)
	}
	require.Contains(t, fired, healthy.ID, "the healthy rule must still be able to fire")
}

// failingStore fails every Upsert.
type failingStore struct{ engineStore }

func (s *failingStore) Upsert(context.Context, Alert) error { return errors.New("write failed") }

// TestTick_PersistFailureIsReportedAndCycleContinues — a single alert failing
// to persist must not stop the remaining alerts from being evaluated and
// notified.
func TestTick_PersistFailureIsReportedAndCycleContinues(t *testing.T) {
	now := time.Unix(100, 0)
	rule := Rule{ID: "custom", Enabled: true, Severity: SeverityWarning, Scope: ScopeInstance, Metric: "x", Comparator: GT, Threshold: 1, Summary: "x"}
	store := &failingStore{}
	store.rules = []Rule{rule}
	notifier := &engineNotifier{}
	e := NewEngine(nil, clock.NewFake(now), store, notifier, []Source{engineSource{kind: "metric", samples: []Sample{{Value: 2, TS: now}}}})

	err := e.Tick(context.Background())
	require.ErrorContains(t, err, "persist alert")
	require.Len(t, notifier.alerts, 1, "a persistence failure must not swallow the notification")
}
