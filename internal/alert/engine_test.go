package alert

import (
	"context"
	"testing"
	"time"

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
	r := Rule{ID: "r", Severity: SeverityWarning, Scope: ScopeInstance, Metric: "x", Comparator: GT, Threshold: 1}
	e.eval.Step(r, Sample{Value: 2, TS: time.Unix(-7200, 0)}, time.Unix(-7200, 0))
	require.NoError(t, e.Tick(context.Background()))
	require.Empty(t, e.eval.states)
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
