package alert

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/manprint/pglens/internal/clock"
	"github.com/stretchr/testify/require"
)

// deadlockRule is db.deadlock_rate as migration 0026 leaves it: any positive
// deadlock rate fires at once. Its seed (0006) asked for a rate above 0.1 per
// second held for 300 seconds, which a real deadlock never reaches: one
// deadlock in a 30-second scrape is stored as 0.033 per second.
func deadlockRule() Rule {
	return Rule{ID: "db.deadlock_rate", Enabled: true, Severity: SeverityWarning, Scope: ScopeInstance, Metric: "pg_deadlocks_total", Comparator: GT, Threshold: 0, For: 0, Summary: "Deadlocks are occurring"}
}

// deadlockSource serves what the SQL source returns for one database: the
// newest positive sample while one is inside the lookback window, and nothing
// once none is, because the source reads positive samples only. Each entry of
// deadlocks is the time of a scrape that counted one deadlock.
func deadlockSource(instance uuid.UUID, deadlocks ...time.Time) passSource {
	return func(now time.Time) []Sample {
		var newest time.Time
		for _, ts := range deadlocks {
			if !ts.After(now) && !ts.Before(now.Add(-lookback(30*time.Second))) && ts.After(newest) {
				newest = ts
			}
		}
		if newest.IsZero() {
			return nil
		}
		return []Sample{{InstanceID: &instance, Datname: "odoo", Value: 1.0 / 30, TS: newest}}
	}
}

// renamed serves a passSource under another rule ID: passSource answers only
// the rule called "custom".
type renamed struct {
	id  string
	src passSource
}

func (r renamed) Kind() string { return "metric" }
func (r renamed) Samples(_ context.Context, rule Rule, now time.Time) ([]Sample, error) {
	if rule.ID != r.id {
		return nil, nil
	}
	return r.src(now), nil
}

// DL-001: a single deadlock, stored as 0.033 per second, fires the default rule
// on the first pass that sees it. The seeded 0.1 threshold never fired on it.
func TestDL001_OneDeadlockFiresTheDefaultRuleAtOnce(t *testing.T) {
	start := time.Unix(10_000, 0)
	clk := clock.NewFake(start)
	id := uuid.New()
	rule := deadlockRule()
	store, notifier := &engineStore{rules: []Rule{rule}}, &engineNotifier{}
	src := renamed{id: rule.ID, src: deadlockSource(id, start.Add(-10*time.Second))}
	e := NewEngineWithInterval(nil, clk, 30*time.Second, store, notifier, []Source{src})

	require.NoError(t, e.Tick(context.Background()))
	require.Len(t, notifier.alerts, 1)
	require.Equal(t, StateFiring, notifier.alerts[0].State)
	require.Equal(t, "odoo", notifier.alerts[0].Datname)
	require.InDelta(t, 1.0/30, notifier.alerts[0].Value, 1e-9)
}

// DL-002: the episode stays firing while the deadlock is inside the lookback
// window (two minutes), and then for the metric grace (ten minutes) after the
// last pass that saw it, so it resolves about twelve minutes after the last
// deadlock. A deadlock inside that hold joins the open episode without a new
// notification, which keeps a trickle of deadlocks from flapping; one after
// the episode resolved opens a new episode and notifies again.
func TestDL002_DeadlockEpisodeResolvesAfterLookbackAndGrace(t *testing.T) {
	start := time.Unix(10_000, 0)
	clk := clock.NewFake(start)
	id := uuid.New()
	rule := deadlockRule()
	second := start.Add(5 * time.Minute)
	after := start.Add(30 * time.Minute)
	store, notifier := &engineStore{rules: []Rule{rule}}, &engineNotifier{}
	src := renamed{id: rule.ID, src: deadlockSource(id, start, second, after)}
	e := NewEngineWithInterval(nil, clk, 30*time.Second, store, notifier, []Source{src})
	ctx := context.Background()
	until := func(at time.Time) {
		t.Helper()
		for clk.Now().Before(at) {
			clk.Advance(30 * time.Second)
			require.NoError(t, e.Tick(ctx))
		}
	}

	require.NoError(t, e.Tick(ctx))
	require.Len(t, notifier.alerts, 1)
	require.Equal(t, StateFiring, notifier.alerts[0].State)

	// The second deadlock comes after the first left the window, inside the
	// grace: the same episode goes on.
	until(second)
	require.Len(t, notifier.alerts, 1, "a deadlock inside the hold joins the open episode")

	// The last pass that sees the second deadlock is at second+120 s; the grace
	// runs ten minutes from there.
	lastSeen := second.Add(2 * time.Minute)
	until(lastSeen.Add(10 * time.Minute))
	require.Len(t, notifier.alerts, 1, "the episode holds for the whole grace")

	until(lastSeen.Add(10*time.Minute + 30*time.Second))
	require.Len(t, notifier.alerts, 2)
	require.Equal(t, StateResolved, notifier.alerts[1].State)
	require.NotNil(t, notifier.alerts[1].ResolvedAt)
	require.True(t, notifier.alerts[1].StartedAt.Equal(start), "one episode from the first deadlock")

	until(after)
	require.Len(t, notifier.alerts, 3, "a deadlock after the episode resolved opens one of its own")
	require.Equal(t, StateFiring, notifier.alerts[2].State)
	require.True(t, notifier.alerts[2].StartedAt.Equal(after))
}

// DL-003: a rule an operator tuned keeps its threshold and duration. At 0.1 per
// second for five minutes, a single deadlock does not fire; a sustained rate
// above it fires once the five minutes are served.
func TestDL003_ATunedDeadlockRuleKeepsItsThreshold(t *testing.T) {
	start := time.Unix(10_000, 0)
	id := uuid.New()
	tuned := deadlockRule()
	tuned.Threshold = 0.1
	tuned.For = 5 * time.Minute

	t.Run("single deadlock", func(t *testing.T) {
		clk := clock.NewFake(start)
		store, notifier := &engineStore{rules: []Rule{tuned}}, &engineNotifier{}
		src := renamed{id: tuned.ID, src: deadlockSource(id, start)}
		e := NewEngineWithInterval(nil, clk, 30*time.Second, store, notifier, []Source{src})
		for i := 0; i < 20; i++ {
			require.NoError(t, e.Tick(context.Background()))
			clk.Advance(30 * time.Second)
		}
		require.Empty(t, notifier.alerts, "0.033 per second is below the tuned threshold")
	})

	t.Run("sustained rate", func(t *testing.T) {
		clk := clock.NewFake(start)
		store, notifier := &engineStore{rules: []Rule{tuned}}, &engineNotifier{}
		src := renamed{id: tuned.ID, src: func(now time.Time) []Sample {
			return []Sample{{InstanceID: &id, Datname: "odoo", Value: 0.5, TS: now}}
		}}
		e := NewEngineWithInterval(nil, clk, 30*time.Second, store, notifier, []Source{src})
		for i := 0; i < 10; i++ { // 0 s to 270 s: pending
			require.NoError(t, e.Tick(context.Background()))
			clk.Advance(30 * time.Second)
		}
		require.Empty(t, notifier.alerts, "the tuned rule waits for its five minutes")
		require.NoError(t, e.Tick(context.Background())) // 300 s
		require.Len(t, notifier.alerts, 1)
		require.Equal(t, StateFiring, notifier.alerts[0].State)
	})
}

// DL-004: the deadlock query reads positive samples only, the newest of each
// database in the lookback window, so a zero scrape after a deadlock does not
// hide it. The window starts two minutes before the pass.
func TestDL004_DeadlockQueryReadsPositiveSamplesOnly(t *testing.T) {
	instance := uuid.New()
	ts := time.Unix(10_000, 0).UTC()
	db := &fakeQueryer{results: []*fakeRows{{rows: [][]any{{int64(1), instance, "odoo", 1.0 / 30, ts.Add(-time.Minute)}}}}}
	s := &metricSource{db: db, interval: 30 * time.Second}

	got, err := s.Samples(t.Context(), deadlockRule(), ts)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.InDelta(t, 1.0/30, got[0].Value, 1e-9)
	require.Len(t, db.calls, 1)
	require.Contains(t, db.calls[0].sql, "AND value > 0 ORDER BY cluster_id, instance_id, datname, ts DESC")
	require.Equal(t, "pg_deadlocks_total", db.calls[0].args[0])
	require.Equal(t, ts.Add(-2*time.Minute), db.calls[0].args[1])
}
