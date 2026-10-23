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

// purgeStore is an engineStore that also keeps history, and records the
// cutoffs that history was purged to. A non-nil err makes the purge fail.
type purgeStore struct {
	*engineStore
	cutoffs []time.Time
	err     error
}

func (s *purgeStore) Purge(_ context.Context, before time.Time) error {
	if s.err != nil {
		return s.err
	}
	s.cutoffs = append(s.cutoffs, before)
	return nil
}

// TestTick_PurgesHistoryOncePerInterval: the first pass purges the history
// past its retention, the passes before purgeInterval has elapsed do not, and
// a purge that fails is tried again on the next pass.
func TestTick_PurgesHistoryOncePerInterval(t *testing.T) {
	t0 := time.Unix(1_000_000, 0)
	clk := clock.NewFake(t0)
	store := &purgeStore{engineStore: &engineStore{}}
	e := NewEngine(nil, clk, store, &engineNotifier{}, []Source{engineSource{kind: "metric"}})
	ctx := context.Background()

	require.NoError(t, e.Tick(ctx))
	require.Equal(t, []time.Time{t0.Add(-historyRetention)}, store.cutoffs)

	clk.Advance(30 * time.Second)
	require.NoError(t, e.Tick(ctx))
	require.Len(t, store.cutoffs, 1, "a pass inside the interval does not purge")

	clk.Advance(purgeInterval)
	require.NoError(t, e.Tick(ctx))
	require.Equal(t, t0.Add(30*time.Second+purgeInterval).Add(-historyRetention), store.cutoffs[1])

	store.err = errors.New("database is down")
	clk.Advance(purgeInterval)
	require.ErrorContains(t, e.Tick(ctx), "purge alert history")
	store.err = nil
	clk.Advance(time.Second)
	require.NoError(t, e.Tick(ctx))
	require.Len(t, store.cutoffs, 3, "a failed purge is retried on the next pass")
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
// The rule is still configured but no source observes it, so the pass has
// nothing to say about the episode and it must survive. The pass that does
// observe the rule closes an episode it did not sample only after the grace
// period (TestTick_ClosesAMetricEpisodeOnlyAfterItsSubjectIsGone).
func TestTick_KeepsFiringEvaluatorState(t *testing.T) {
	r := Rule{ID: "r", Enabled: true, Severity: SeverityWarning, Scope: ScopeInstance, Metric: "x", Comparator: GT, Threshold: 1}
	e := NewEngine(nil, clock.NewFake(time.Unix(0, 0)), &engineStore{rules: []Rule{r}}, nil, nil)
	_, tr := e.eval.Step(r, Sample{Value: 2, TS: time.Unix(-7200, 0)}, time.Unix(-7200, 0))
	require.Equal(t, TransitionFired, tr)
	require.NoError(t, e.Tick(context.Background()))
	require.Len(t, e.eval.states, 1)
}

// lifecycleSource serves samples for the rules a test names and can fail, so a
// single rule's episodes can be driven through several passes. cleared answers
// the spell check; with no answer function, nothing has cleared.
type lifecycleSource struct {
	kind    string
	samples func(Rule) []Sample
	err     error
	cleared func(Rule, []Alert) ([]Alert, error)
	asked   []Alert
}

func (s *lifecycleSource) Kind() string { return s.kind }

func (s *lifecycleSource) Samples(_ context.Context, r Rule, _ time.Time) ([]Sample, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.samples == nil {
		return nil, nil
	}
	return s.samples(r), nil
}

func (s *lifecycleSource) Cleared(_ context.Context, r Rule, episodes []Alert) ([]Alert, error) {
	s.asked = append([]Alert(nil), episodes...)
	if s.cleared == nil {
		return nil, nil
	}
	return s.cleared(r, episodes)
}

// TestTick_ClosesAMetricEpisodeOnlyAfterItsSubjectIsGone — a metric episode
// whose subject stops being sampled resolves once the lookback has passed since
// the last sample, not sooner. A collector pause shorter than the window must
// not close a real incident.
func TestTick_ClosesAMetricEpisodeOnlyAfterItsSubjectIsGone(t *testing.T) {
	start := time.Unix(1000, 0)
	clk := clock.NewFake(start)
	rule := Rule{ID: "custom", Enabled: true, Severity: SeverityWarning, Scope: ScopeInstance, Metric: "x", Comparator: GT, Threshold: 1, Summary: "x"}
	present := true
	src := &lifecycleSource{kind: "metric", samples: func(r Rule) []Sample {
		if r.ID != rule.ID || !present {
			return nil
		}
		return []Sample{{Value: 2, TS: start}}
	}}
	store, notifier := &engineStore{rules: []Rule{rule}}, &engineNotifier{}
	e := NewEngineWithInterval(nil, clk, 30*time.Second, store, notifier, []Source{src})
	ctx := context.Background()

	require.NoError(t, e.Tick(ctx))
	require.Len(t, notifier.alerts, 1)

	present = false
	for i := 0; i < 3; i++ { // up to 90 seconds after the last sample: inside the 2m lookback
		clk.Advance(30 * time.Second)
		require.NoError(t, e.Tick(ctx))
	}
	require.Len(t, notifier.alerts, 1, "a subject inside the grace period must not close")

	clk.Advance(30 * time.Second) // 120 seconds after the last sample
	require.NoError(t, e.Tick(ctx))
	require.Len(t, notifier.alerts, 2)
	require.Equal(t, StateResolved, notifier.alerts[1].State)
	require.NotNil(t, notifier.alerts[1].ResolvedAt)
}

// TestTick_ClosesTheEpisodeOfARemovedRuleAtOnce — a disabled or deleted rule
// has no later sample to close its episodes, so they close on the next pass.
func TestTick_ClosesTheEpisodeOfARemovedRuleAtOnce(t *testing.T) {
	start := time.Unix(1000, 0)
	clk := clock.NewFake(start)
	rule := Rule{ID: "custom", Enabled: true, Severity: SeverityWarning, Scope: ScopeInstance, Metric: "x", Comparator: GT, Threshold: 1, Summary: "x"}
	src := &lifecycleSource{kind: "metric", samples: func(r Rule) []Sample {
		if r.ID != rule.ID {
			return nil
		}
		return []Sample{{Value: 2, TS: start}}
	}}
	store, notifier := &engineStore{rules: []Rule{rule}}, &engineNotifier{}
	e := NewEngineWithInterval(nil, clk, 30*time.Second, store, notifier, []Source{src})
	ctx := context.Background()

	require.NoError(t, e.Tick(ctx))
	require.Len(t, notifier.alerts, 1)

	store.rules = nil // the rule is disabled
	clk.Advance(30 * time.Second)
	require.NoError(t, e.Tick(ctx))
	require.Len(t, notifier.alerts, 2)
	require.Equal(t, StateResolved, notifier.alerts[1].State)
}

// TestTick_AFailedSourceClosesNothing — a source that errors has not observed
// its subjects, so silence from it says nothing. A database blip must not close
// every open episode of the rule.
func TestTick_AFailedSourceClosesNothing(t *testing.T) {
	start := time.Unix(1000, 0)
	clk := clock.NewFake(start)
	rule := Rule{ID: "custom", Enabled: true, Severity: SeverityWarning, Scope: ScopeInstance, Metric: "x", Comparator: GT, Threshold: 1, Summary: "x"}
	src := &lifecycleSource{kind: "metric", samples: func(r Rule) []Sample {
		if r.ID != rule.ID {
			return nil
		}
		return []Sample{{Value: 2, TS: start}}
	}}
	store, notifier := &engineStore{rules: []Rule{rule}}, &engineNotifier{}
	e := NewEngineWithInterval(nil, clk, 30*time.Second, store, notifier, []Source{src})
	ctx := context.Background()

	require.NoError(t, e.Tick(ctx))
	require.Len(t, notifier.alerts, 1)

	src.err = errors.New("statement timeout")
	for i := 0; i < 5; i++ {
		clk.Advance(30 * time.Second)
		require.Error(t, e.Tick(ctx))
	}
	require.Len(t, notifier.alerts, 1, "an unobserved rule must not resolve its episodes")
}

// TestTick_APartlyFailedKindClosesNothing — a rule served by two sources of one
// kind is observed only when every source answered. The episode here was
// sampled by the source that now fails; the other source answers with nothing,
// and that alone must not close the episode.
func TestTick_APartlyFailedKindClosesNothing(t *testing.T) {
	start := time.Unix(1000, 0)
	clk := clock.NewFake(start)
	rule := Rule{ID: "custom", Enabled: true, Severity: SeverityWarning, Scope: ScopeInstance, Metric: "x", Comparator: GT, Threshold: 1, Summary: "x"}
	owner := &lifecycleSource{kind: "metric", samples: func(r Rule) []Sample {
		if r.ID != rule.ID {
			return nil
		}
		return []Sample{{Value: 2, TS: start}}
	}}
	other := &lifecycleSource{kind: "metric"}
	store, notifier := &engineStore{rules: []Rule{rule}}, &engineNotifier{}
	e := NewEngineWithInterval(nil, clk, 30*time.Second, store, notifier, []Source{owner, other})
	ctx := context.Background()

	require.NoError(t, e.Tick(ctx))
	require.Len(t, notifier.alerts, 1)

	owner.err = errors.New("statement timeout")
	for i := 0; i < 5; i++ {
		clk.Advance(30 * time.Second)
		require.Error(t, e.Tick(ctx))
	}
	require.Len(t, notifier.alerts, 1, "an episode only the failed source samples must not resolve while another source answers")
}

// TestTick_SpellEpisodeStaysOpenUntilItsSubjectClears — instance_unreachable is
// emitted once per outage. When its event ages out the outage may well still
// be going, so the episode closes only when the source confirms that the
// instance answers again.
func TestTick_SpellEpisodeStaysOpenUntilItsSubjectClears(t *testing.T) {
	start := time.Unix(1000, 0)
	clk := clock.NewFake(start)
	id := uuid.New()
	present, cleared := true, false
	src := &lifecycleSource{kind: "event", samples: func(r Rule) []Sample {
		if r.ID != "instance_unreachable" || !present {
			return nil
		}
		return []Sample{{InstanceID: &id, TS: start}}
	}, cleared: func(_ Rule, episodes []Alert) ([]Alert, error) {
		if !cleared {
			return nil, nil
		}
		return episodes, nil
	}}
	store, notifier := &engineStore{}, &engineNotifier{}
	e := NewEngineWithInterval(nil, clk, 30*time.Second, store, notifier, []Source{src})
	ctx := context.Background()

	require.NoError(t, e.Tick(ctx)) // instance_unreachable has no For: it fires at once
	require.Len(t, notifier.alerts, 1)

	present = false
	for i := 0; i < 6; i++ { // three minutes: the event is long out of the window
		clk.Advance(30 * time.Second)
		require.NoError(t, e.Tick(ctx))
	}
	require.Len(t, notifier.alerts, 1, "the outage is still going")
	require.Len(t, src.asked, 1)
	require.Equal(t, id, *src.asked[0].InstanceID)

	cleared = true
	clk.Advance(30 * time.Second)
	require.NoError(t, e.Tick(ctx))
	require.Len(t, notifier.alerts, 2)
	require.Equal(t, StateResolved, notifier.alerts[1].State)
}

// TestTick_ASpellCheckThatFailsClosesNothing — when the source cannot say
// whether the condition cleared, the episode stays open rather than closing on
// a guess.
func TestTick_ASpellCheckThatFailsClosesNothing(t *testing.T) {
	start := time.Unix(1000, 0)
	clk := clock.NewFake(start)
	id := uuid.New()
	present := true
	src := &lifecycleSource{kind: "event", samples: func(r Rule) []Sample {
		if r.ID != "instance_unreachable" || !present {
			return nil
		}
		return []Sample{{InstanceID: &id, TS: start}}
	}, cleared: func(Rule, []Alert) ([]Alert, error) {
		return nil, errors.New("events unavailable")
	}}
	store, notifier := &engineStore{}, &engineNotifier{}
	e := NewEngineWithInterval(nil, clk, 30*time.Second, store, notifier, []Source{src})
	ctx := context.Background()

	require.NoError(t, e.Tick(ctx))
	present = false
	for i := 0; i < 6; i++ {
		clk.Advance(30 * time.Second)
		err := e.Tick(ctx)
		if i >= 3 { // two minutes after the last sample the spell check runs
			require.Error(t, err)
		}
	}
	require.Len(t, notifier.alerts, 1, "an unconfirmed spell must stay open")
}

// TestTick_APulseEpisodeClosesAfterQuietWindowAndNotifiesAgain — failover_detected
// is a pulse: its episode closes once no failover has been seen for the
// lookback window, so a later failover is a new episode and notifies again.
func TestTick_APulseEpisodeClosesAfterQuietWindowAndNotifiesAgain(t *testing.T) {
	start := time.Unix(1000, 0)
	clk := clock.NewFake(start)
	cluster := int64(7)
	present := true
	src := &lifecycleSource{kind: "event", samples: func(r Rule) []Sample {
		if r.ID != "failover_detected" || !present {
			return nil
		}
		return []Sample{{ClusterID: &cluster, TS: clk.Now()}}
	}}
	store, notifier := &engineStore{}, &engineNotifier{}
	e := NewEngineWithInterval(nil, clk, 30*time.Second, store, notifier, []Source{src})
	ctx := context.Background()

	require.NoError(t, e.Tick(ctx))
	require.Len(t, notifier.alerts, 1)

	present = false
	for i := 0; i < 4; i++ { // two minutes: the failover has left the window
		clk.Advance(30 * time.Second)
		require.NoError(t, e.Tick(ctx))
	}
	require.Len(t, notifier.alerts, 2)
	require.Equal(t, StateResolved, notifier.alerts[1].State)
	require.Empty(t, src.asked, "a pulse needs no confirmation that it cleared")

	present = true
	clk.Advance(30 * time.Second)
	require.NoError(t, e.Tick(ctx))
	require.Len(t, notifier.alerts, 3)
	require.Equal(t, StateFiring, notifier.alerts[2].State)
}

// TestTick_ARestoredEpisodeGetsAFullGracePeriod — a newly elected engine
// restores the episodes it has not yet seen sampled. They get the same grace
// period as any other, measured from the restore.
func TestTick_ARestoredEpisodeGetsAFullGracePeriod(t *testing.T) {
	start := time.Unix(1000, 0)
	clk := clock.NewFake(start)
	rule := Rule{ID: "custom", Enabled: true, Severity: SeverityWarning, Scope: ScopeInstance, Metric: "x", Comparator: GT, Threshold: 1, Summary: "x"}
	stored := Alert{Key: "custom//", RuleID: rule.ID, Severity: rule.Severity, State: StateFiring, StartedAt: start.Add(-time.Minute), LastEvalAt: start.Add(-time.Second), Summary: rule.Summary}
	store, notifier := &engineStore{rules: []Rule{rule}, alerts: []Alert{stored}}, &engineNotifier{}
	e := NewEngineWithInterval(nil, clk, 30*time.Second, store, notifier, []Source{&lifecycleSource{kind: "metric"}})
	ctx := context.Background()

	require.NoError(t, e.Tick(ctx))
	require.Empty(t, notifier.alerts, "a restored episode must not close on the first pass")

	clk.Advance(2 * time.Minute)
	require.NoError(t, e.Tick(ctx))
	require.Len(t, notifier.alerts, 1)
	require.Equal(t, StateResolved, notifier.alerts[0].State)
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

// flakyStore fails its next failures writes and then behaves like engineStore.
type flakyStore struct {
	engineStore
	failures int
}

func (s *flakyStore) Upsert(ctx context.Context, a Alert) error {
	if s.failures > 0 {
		s.failures--
		return errors.New("write failed")
	}
	return s.engineStore.Upsert(ctx, a)
}

func notifiedCount(alerts []Alert, ruleID string) int {
	n := 0
	for _, a := range alerts {
		if a.RuleID == ruleID {
			n++
		}
	}
	return n
}

// TestTick_AFailedFireWriteIsNotifiedOnceOnceItIsPersisted — a fire whose write
// fails must not be notified, since nothing on record says it happened. It is
// derived again on the next pass, written and notified exactly once. The other
// alert of the same pass is not held back by it.
func TestTick_AFailedFireWriteIsNotifiedOnceOnceItIsPersisted(t *testing.T) {
	now := time.Unix(100, 0)
	first := Rule{ID: "first", Enabled: true, Severity: SeverityWarning, Scope: ScopeInstance, Metric: "x", Comparator: GT, Threshold: 1, Summary: "x"}
	second := first
	second.ID = "second"
	store := &flakyStore{failures: 1}
	store.rules = []Rule{first, second}
	notifier := &engineNotifier{}
	e := NewEngine(nil, clock.NewFake(now), store, notifier, []Source{engineSource{kind: "metric", samples: []Sample{{Value: 2, TS: now}}}})

	err := e.Tick(context.Background())
	require.ErrorContains(t, err, "persist alert first")
	require.Equal(t, 0, notifiedCount(notifier.alerts, "first"), "an unpersisted fire must not be notified")
	require.Equal(t, 1, notifiedCount(notifier.alerts, "second"), "the other alert must still be notified")

	require.NoError(t, e.Tick(context.Background()))
	require.Equal(t, 1, notifiedCount(notifier.alerts, "first"))
	require.Equal(t, 1, notifiedCount(notifier.alerts, "second"))
	require.Equal(t, 1, notifiedCount(store.alerts, "first"), "the fire must be on record once")
}

// TestTick_AFailedResolveWriteIsNotifiedOnceOnceItIsPersisted — a resolve whose
// write fails must not be notified, and the episode stays open in the evaluator
// so that the next pass resolves it again.
func TestTick_AFailedResolveWriteIsNotifiedOnceOnceItIsPersisted(t *testing.T) {
	now := time.Unix(100, 0)
	rule := Rule{ID: "custom", Enabled: true, Severity: SeverityWarning, Scope: ScopeInstance, Metric: "x", Comparator: GT, Threshold: 1, Summary: "x"}
	store := &flakyStore{}
	store.rules = []Rule{rule}
	notifier := &engineNotifier{}
	e := NewEngine(nil, clock.NewFake(now), store, notifier, []Source{engineSource{kind: "metric", samples: []Sample{{Value: 2, TS: now}}}})
	require.NoError(t, e.Tick(context.Background()))
	require.Len(t, notifier.alerts, 1)

	e.sources = []Source{engineSource{kind: "metric", samples: []Sample{{Value: 0, TS: now.Add(time.Second)}}}}
	store.failures = 1
	require.ErrorContains(t, e.Tick(context.Background()), "persist alert custom")
	require.Len(t, notifier.alerts, 1, "an unpersisted resolve must not be notified")

	require.NoError(t, e.Tick(context.Background()))
	require.Len(t, notifier.alerts, 2)
	require.Equal(t, StateResolved, notifier.alerts[1].State)
	require.Equal(t, StateResolved, store.alerts[len(store.alerts)-1].State)
}

// clockSource observes the rule "custom" on every pass, as a live source does:
// each sample carries the pass time the engine asked for.
type clockSource struct {
	kind  string
	value float64
}

func (s clockSource) Kind() string { return s.kind }
func (s clockSource) Samples(_ context.Context, r Rule, now time.Time) ([]Sample, error) {
	if r.ID != "custom" {
		return nil, nil
	}
	return []Sample{{Value: s.value, TS: now}}, nil
}

// TestTick_ALongPendingEpisodeFiresAfterItsFor — a condition that has to hold
// for longer than an hour must still fire. The pending state used to be
// forgotten an hour after its first sample while the condition was still
// being observed, so the For timer restarted every hour and never completed.
func TestTick_ALongPendingEpisodeFiresAfterItsFor(t *testing.T) {
	clk := clock.NewFake(time.Unix(100, 0))
	rule := Rule{ID: "custom", Enabled: true, Severity: SeverityWarning, Scope: ScopeInstance, Metric: "x", Comparator: GT, Threshold: 1, For: 2 * time.Hour, Summary: "x"}
	store, notifier := &engineStore{rules: []Rule{rule}}, &engineNotifier{}
	e := NewEngineWithInterval(nil, clk, 30*time.Second, store, notifier, []Source{clockSource{kind: "metric", value: 2}})
	for i := 0; i < 5*120; i++ {
		require.NoError(t, e.Tick(context.Background()))
		clk.Advance(30 * time.Second)
	}
	require.Len(t, notifier.alerts, 1)
	require.Equal(t, StateFiring, notifier.alerts[0].State)
}

// TestTick_AFiringEpisodeRefreshesItsStoredRow — while an alert fires, its row
// shows the value and the time of the last pass. Both used to stay at the
// values of the pass that fired it, so an alert still firing hours later looked
// unevaluated. Refreshing the row must not notify again.
func TestTick_AFiringEpisodeRefreshesItsStoredRow(t *testing.T) {
	start := time.Unix(100, 0)
	clk := clock.NewFake(start)
	rule := Rule{ID: "custom", Enabled: true, Severity: SeverityWarning, Scope: ScopeInstance, Metric: "x", Comparator: GT, Threshold: 1, Summary: "x"}
	store, notifier := &engineStore{rules: []Rule{rule}}, &engineNotifier{}
	e := NewEngine(nil, clk, store, notifier, []Source{clockSource{kind: "metric", value: 2}})
	require.NoError(t, e.Tick(context.Background()))

	clk.Advance(time.Hour)
	e.sources = []Source{clockSource{kind: "metric", value: 5}}
	require.NoError(t, e.Tick(context.Background()))

	last := store.alerts[len(store.alerts)-1]
	require.Equal(t, StateFiring, last.State)
	require.Equal(t, 5.0, last.Value)
	require.True(t, last.LastEvalAt.Equal(start.Add(time.Hour)), "the stored row must carry the last pass, got %s", last.LastEvalAt)
	require.Len(t, notifier.alerts, 1)
}

// passSource reports the samples of each pass, computed from the pass time.
type passSource func(now time.Time) []Sample

func (s passSource) Kind() string { return "metric" }
func (s passSource) Samples(_ context.Context, r Rule, now time.Time) ([]Sample, error) {
	if r.ID != "custom" {
		return nil, nil
	}
	return s(now), nil
}

// TestTick_DatabasesOfOneInstanceKeepTheirOwnEpisodes — an instance-scoped rule
// on a per-database metric, such as db.deadlock_rate, gets one sample per
// database. They used to share one episode, so a database below the threshold
// cancelled the episode of the database above it, whatever the order of the
// samples.
func TestTick_DatabasesOfOneInstanceKeepTheirOwnEpisodes(t *testing.T) {
	rates := map[string]float64{"app": 0.5, "batch": 0.05}
	for _, tc := range []struct {
		name  string
		order []string
	}{
		{"hot database first", []string{"app", "batch"}},
		{"cool database first", []string{"batch", "app"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.New()
			clk := clock.NewFake(time.Unix(100, 0))
			rule := Rule{ID: "custom", Enabled: true, Severity: SeverityWarning, Scope: ScopeInstance, Metric: "pg_deadlocks_total", Comparator: GT, Threshold: 0.1, For: 5 * time.Minute, Summary: "x"}
			store, notifier := &engineStore{rules: []Rule{rule}}, &engineNotifier{}
			source := passSource(func(now time.Time) []Sample {
				var out []Sample
				for _, db := range tc.order {
					out = append(out, Sample{InstanceID: &id, Datname: db, Value: rates[db], TS: now})
				}
				return out
			})
			e := NewEngineWithInterval(nil, clk, 30*time.Second, store, notifier, []Source{source})
			for i := 0; i < 12; i++ {
				require.NoError(t, e.Tick(context.Background()))
				clk.Advance(30 * time.Second)
			}
			require.Len(t, notifier.alerts, 1)
			require.Equal(t, "app", notifier.alerts[0].Datname)
			require.Equal(t, StateFiring, notifier.alerts[0].State)
		})
	}
}
