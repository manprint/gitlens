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

// windowedOutages serves each instance_unreachable occurrence of one instance
// while the lookback still holds it, the way the event table does.
func windowedOutages(clk clock.Clock, id *uuid.UUID, occurrences *[]time.Time) func(Rule) []Sample {
	return func(r Rule) []Sample {
		if r.ID != "instance_unreachable" {
			return nil
		}
		var out []Sample
		from := clk.Now().Add(-lookback(30 * time.Second))
		for i := len(*occurrences) - 1; i >= 0; i-- { // newest first, as the query orders them
			ts := (*occurrences)[i]
			if !ts.Before(from) && !ts.After(clk.Now()) {
				out = append(out, Sample{InstanceID: id, Value: 1, TS: ts})
			}
		}
		return out
	}
}

// resolvedStore is an engineStore that also answers the read of the episodes
// resolved since a cutoff.
type resolvedStore struct {
	*engineStore
	resolved map[string]time.Time
	since    []time.Time
}

func (s *resolvedStore) RecentlyResolved(_ context.Context, since time.Time) (map[string]time.Time, error) {
	s.since = append(s.since, since)
	out := map[string]time.Time{}
	for k, at := range s.resolved {
		if !at.Before(since) {
			out[k] = at
		}
	}
	return out, nil
}

func unreachableKey(id uuid.UUID) string {
	for _, r := range Builtin() {
		if r.ID == "instance_unreachable" {
			return Key(r, Sample{InstanceID: &id})
		}
	}
	panic("instance_unreachable is not a builtin rule")
}

// HA-V-12-1: a spell closes on the first pass, once it is clearAfter old, that
// its Clearer confirms, while its event is still in the window; that old event opens nothing afterwards, and a new
// outage of the same instance opens a new episode and notifies it.
func TestHAV121SpellClosesWhileItsEventIsInTheWindowAndANewOutageReopens(t *testing.T) {
	t0 := time.Unix(10_000, 0)
	clk := clock.NewFake(t0)
	id := uuid.New()
	occ := []time.Time{t0}
	cleared := false
	src := &lifecycleSource{kind: "event", samples: windowedOutages(clk, &id, &occ), cleared: func(_ Rule, eps []Alert) ([]Alert, error) {
		if cleared {
			return eps, nil
		}
		return nil, nil
	}}
	store, notifier := &engineStore{}, &engineNotifier{}
	e := NewEngineWithInterval(nil, clk, 30*time.Second, store, notifier, []Source{src})
	ctx := context.Background()

	require.NoError(t, e.Tick(ctx))
	require.Len(t, notifier.alerts, 1)
	require.Equal(t, StateFiring, notifier.alerts[0].State)

	cleared = true
	clk.Advance(30 * time.Second)
	require.NoError(t, e.Tick(ctx))
	require.Len(t, notifier.alerts, 1, "an episode younger than clearAfter is not checked")
	clk.Advance(30 * time.Second)
	require.NoError(t, e.Tick(ctx))
	require.Len(t, notifier.alerts, 2, "the episode resolves on the pass the instance answers, with its event still in the window")
	require.Equal(t, StateResolved, notifier.alerts[1].State)

	clk.Advance(30 * time.Second)
	require.NoError(t, e.Tick(ctx))
	require.Len(t, notifier.alerts, 2, "the old event must not reopen the closed episode")

	cleared = false
	clk.Advance(30 * time.Second)
	occ = append(occ, clk.Now())
	require.NoError(t, e.Tick(ctx))
	require.Len(t, notifier.alerts, 3, "a new outage opens a new episode")
	second := notifier.alerts[2]
	require.Equal(t, StateFiring, second.State)
	require.Equal(t, unreachableKey(id), second.Key)
	require.True(t, second.StartedAt.After(notifier.alerts[0].StartedAt), "the new episode has its own started_at")
}

// HA-V-12-2: the condition holds again but no new event is written yet: the
// old event, still in the window, reopens nothing.
func TestHAV122OldEventDoesNotReopenWhileNoNewEventIsWritten(t *testing.T) {
	t0 := time.Unix(10_000, 0)
	clk := clock.NewFake(t0)
	id := uuid.New()
	occ := []time.Time{t0}
	cleared := true
	src := &lifecycleSource{kind: "event", samples: windowedOutages(clk, &id, &occ), cleared: func(_ Rule, eps []Alert) ([]Alert, error) {
		if cleared {
			return eps, nil
		}
		return nil, nil
	}}
	store, notifier := &engineStore{}, &engineNotifier{}
	e := NewEngineWithInterval(nil, clk, 30*time.Second, store, notifier, []Source{src})
	ctx := context.Background()

	require.NoError(t, e.Tick(ctx))
	clk.Advance(clearAfter(30 * time.Second))
	require.NoError(t, e.Tick(ctx))
	require.Len(t, notifier.alerts, 2)

	cleared = false
	for i := 0; i < 6; i++ {
		clk.Advance(30 * time.Second)
		require.NoError(t, e.Tick(ctx))
	}
	require.Len(t, notifier.alerts, 2, "nothing reopens from the old event")
}

// HA-V-12-3: a fresh engine (restart, new leader) skips an event whose episode
// the store shows resolved within the lookback; with no such row it opens.
func TestHAV123FreshEngineSeedsItsWatermarksFromTheStore(t *testing.T) {
	now := time.Unix(10_000, 0)
	id := uuid.New()
	occ := []time.Time{now.Add(-60 * time.Second)}
	run := func(resolved map[string]time.Time) (*engineNotifier, *resolvedStore) {
		clk := clock.NewFake(now)
		src := &lifecycleSource{kind: "event", samples: windowedOutages(clk, &id, &occ)}
		store := &resolvedStore{engineStore: &engineStore{}, resolved: resolved}
		notifier := &engineNotifier{}
		e := NewEngineWithInterval(nil, clk, 30*time.Second, store, notifier, []Source{src})
		require.NoError(t, e.Tick(context.Background()))
		return notifier, store
	}

	notifier, store := run(map[string]time.Time{unreachableKey(id): now.Add(-30 * time.Second)})
	require.Empty(t, notifier.alerts, "the episode of that event was resolved 30 s ago")
	require.Equal(t, []time.Time{now.Add(-lookback(30 * time.Second))}, store.since, "the read covers the lookback")

	notifier, _ = run(nil)
	require.Len(t, notifier.alerts, 1, "with no resolved episode the event opens one")
}

// HA-V-12-4: a Clearer error closes nothing and is a pass error, also while
// the event is in the window; a later pass whose check succeeds closes.
func TestHAV124ClearerErrorClosesNothingUntilALaterPass(t *testing.T) {
	t0 := time.Unix(10_000, 0)
	clk := clock.NewFake(t0)
	id := uuid.New()
	occ := []time.Time{t0}
	var clearErr error
	src := &lifecycleSource{kind: "event", samples: windowedOutages(clk, &id, &occ), cleared: func(_ Rule, eps []Alert) ([]Alert, error) {
		if clearErr != nil {
			return nil, clearErr
		}
		return eps, nil
	}}
	store, notifier := &engineStore{}, &engineNotifier{}
	e := NewEngineWithInterval(nil, clk, 30*time.Second, store, notifier, []Source{src})
	ctx := context.Background()

	clearErr = errors.New("events unavailable")
	require.NoError(t, e.Tick(ctx), "nothing is open yet: no check")
	clk.Advance(30 * time.Second)
	require.NoError(t, e.Tick(ctx), "the episode is younger than clearAfter: no check")
	clk.Advance(30 * time.Second)
	require.ErrorContains(t, e.Tick(ctx), "events unavailable")
	require.Len(t, notifier.alerts, 1, "a failed check closes nothing")

	clearErr = nil
	clk.Advance(30 * time.Second)
	require.NoError(t, e.Tick(ctx))
	require.Len(t, notifier.alerts, 2)
	require.Equal(t, StateResolved, notifier.alerts[1].State)
}

// HA-V-12-5: a pulse keeps its behaviour: no Clearer call, two occurrences
// within the lookback plus one pass share one episode.
func TestHAV125PulseKeepsItsWindowSemantics(t *testing.T) {
	start := time.Unix(1000, 0)
	clk := clock.NewFake(start)
	cluster := int64(7)
	occ := []time.Time{start, start.Add(90 * time.Second)}
	var asked []string
	src := &lifecycleSource{kind: "event", samples: windowedFailovers(clk, &cluster, &occ), cleared: func(r Rule, eps []Alert) ([]Alert, error) {
		asked = append(asked, r.ID)
		return eps, nil
	}}
	store, notifier := &engineStore{}, &engineNotifier{}
	e := NewEngineWithInterval(nil, clk, 30*time.Second, store, notifier, []Source{src})
	ctx := context.Background()

	for i := 0; i < 20; i++ {
		require.NoError(t, e.Tick(ctx))
		clk.Advance(30 * time.Second)
	}
	require.Empty(t, asked, "a pulse never asks the Clearer")
	require.Len(t, notifier.alerts, 2, "one episode: fire and resolve")
	require.Equal(t, StateFiring, notifier.alerts[0].State)
	require.Equal(t, StateResolved, notifier.alerts[1].State)
}

// HA-V-12-6: the event writer's clock runs 50 s behind the engine's. A new
// outage stamped before the engine's close time, but after the closed
// episode's own occurrence, still opens a new episode.
func TestHAV126WatermarkIsTheWritersStampNotTheEnginesClock(t *testing.T) {
	t0 := time.Unix(10_000, 0)
	skew := -50 * time.Second
	clk := clock.NewFake(t0)
	id := uuid.New()
	occ := []time.Time{t0.Add(skew)}
	cleared := true
	src := &lifecycleSource{kind: "event", samples: windowedOutages(clk, &id, &occ), cleared: func(_ Rule, eps []Alert) ([]Alert, error) {
		if cleared {
			return eps, nil
		}
		return nil, nil
	}}
	store, notifier := &engineStore{}, &engineNotifier{}
	e := NewEngineWithInterval(nil, clk, 30*time.Second, store, notifier, []Source{src})
	ctx := context.Background()

	require.NoError(t, e.Tick(ctx))
	clk.Advance(30 * time.Second)
	require.NoError(t, e.Tick(ctx))
	require.Len(t, notifier.alerts, 2, "fired and resolved")
	closedAt := clk.Now()

	cleared = false
	clk.Advance(30 * time.Second)
	occ = append(occ, clk.Now().Add(skew))
	require.True(t, occ[1].Before(closedAt), "the new stamp precedes the engine's close time")
	require.NoError(t, e.Tick(ctx))
	require.Len(t, notifier.alerts, 3, "the new outage opens a new episode")
	require.Equal(t, StateFiring, notifier.alerts[2].State)
}

// failResolveStore fails to persist a resolution while fail is set.
type failResolveStore struct {
	*engineStore
	fail bool
}

func (s *failResolveStore) Upsert(ctx context.Context, a Alert) error {
	if s.fail && a.State == StateResolved {
		return errors.New("store down")
	}
	return s.engineStore.Upsert(ctx, a)
}

// HA-V-12-7: a resolution that cannot be persisted keeps its episode open and
// records no watermark; a later pass closes it.
func TestHAV127UnpersistedResolutionKeepsTheEpisode(t *testing.T) {
	t0 := time.Unix(10_000, 0)
	clk := clock.NewFake(t0)
	id := uuid.New()
	occ := []time.Time{t0}
	src := &lifecycleSource{kind: "event", samples: windowedOutages(clk, &id, &occ), cleared: func(_ Rule, eps []Alert) ([]Alert, error) {
		return eps, nil
	}}
	store, notifier := &failResolveStore{engineStore: &engineStore{}}, &engineNotifier{}
	e := NewEngineWithInterval(nil, clk, 30*time.Second, store, notifier, []Source{src})
	ctx := context.Background()

	require.NoError(t, e.Tick(ctx))
	store.fail = true
	clk.Advance(clearAfter(30 * time.Second))
	require.ErrorContains(t, e.Tick(ctx), "store down")
	require.Len(t, notifier.alerts, 1, "nothing resolved")
	require.True(t, e.eval.Holds(unreachableKey(id)), "the episode stays open")
	require.False(t, e.closedBy(unreachableKey(id), t0), "no watermark")

	store.fail = false
	clk.Advance(30 * time.Second)
	require.NoError(t, e.Tick(ctx))
	require.Len(t, notifier.alerts, 2)
	require.Equal(t, StateResolved, notifier.alerts[1].State)
	require.True(t, e.closedBy(unreachableKey(id), t0))
}
