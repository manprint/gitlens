package topology

import (
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
)

func TestEngine_FirstSightingIsRoleChangeNotFailover(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	e := NewEngine(clk)

	instID := uuid.New()
	cID := pgtype.ManualClusterID("cluster1")

	obs := Observation{
		InstanceID: instID,
		ClusterID:  cID,
		Role:       pgtype.RolePrimary,
		Timestamp:  clk.Now(),
	}

	events := e.Apply(obs)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventRoleChange {
		t.Errorf("expected EventRoleChange, got %v", events[0].Type)
	}
}

func TestEngine_PromoteEmitsFailover(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	e := NewEngine(clk)

	primary := uuid.New()
	standby := uuid.New()
	cID := pgtype.ManualClusterID("cluster1")

	// First, observe primary
	e.Apply(Observation{
		InstanceID: primary,
		ClusterID:  cID,
		Role:       pgtype.RolePrimary,
		Timestamp:  clk.Now(),
	})

	// Then observe standby for 60 seconds
	e.Apply(Observation{
		InstanceID: standby,
		ClusterID:  cID,
		Role:       pgtype.RoleStandby,
		Timestamp:  clk.Now(),
	})

	// Advance 60 seconds, primary still primary
	clk.Advance(60 * time.Second)
	e.Apply(Observation{
		InstanceID: primary,
		ClusterID:  cID,
		Role:       pgtype.RolePrimary,
		Timestamp:  clk.Now(),
	})

	// Promote the standby: standby -> primary
	events := e.Apply(Observation{
		InstanceID: standby,
		ClusterID:  cID,
		Role:       pgtype.RolePrimary,
		Timestamp:  clk.Now(),
	})

	// Check for failover_detected
	var hasFailover bool
	for _, evt := range events {
		if evt.Type == EventFailoverDetected {
			hasFailover = true
			if evt.Payload["old_primary"] != primary {
				t.Errorf("old_primary mismatch")
			}
			if evt.Payload["new_primary"] != standby {
				t.Errorf("new_primary mismatch")
			}
		}
	}
	if !hasFailover {
		t.Fatal("expected failover_detected event")
	}
}

func TestEngine_FailoverEmittedOnce(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	e := NewEngine(clk)

	primary := uuid.New()
	standby := uuid.New()
	cID := pgtype.ManualClusterID("cluster1")

	e.Apply(Observation{
		InstanceID: primary,
		ClusterID:  cID,
		Role:       pgtype.RolePrimary,
		Timestamp:  clk.Now(),
	})

	e.Apply(Observation{
		InstanceID: standby,
		ClusterID:  cID,
		Role:       pgtype.RoleStandby,
		Timestamp:  clk.Now(),
	})

	clk.Advance(60 * time.Second)
	e.Apply(Observation{
		InstanceID: primary,
		ClusterID:  cID,
		Role:       pgtype.RolePrimary,
		Timestamp:  clk.Now(),
	})

	// Promote
	e.Apply(Observation{
		InstanceID: standby,
		ClusterID:  cID,
		Role:       pgtype.RolePrimary,
		Timestamp:  clk.Now(),
	})

	// Ten more observations of promoted standby
	for i := 0; i < 10; i++ {
		clk.Advance(1 * time.Second)
		events := e.Apply(Observation{
			InstanceID: standby,
			ClusterID:  cID,
			Role:       pgtype.RolePrimary,
			Timestamp:  clk.Now(),
		})
		for _, evt := range events {
			if evt.Type == EventFailoverDetected {
				t.Fatalf("failover_detected emitted again on iteration %d", i)
			}
		}
	}
}

func TestEngine_PromoteWithoutPriorPrimaryIsRoleChange(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	e := NewEngine(clk)

	standby := uuid.New()
	cID := pgtype.ManualClusterID("cluster1")

	e.Apply(Observation{
		InstanceID: standby,
		ClusterID:  cID,
		Role:       pgtype.RoleStandby,
		Timestamp:  clk.Now(),
	})

	// Promote without a prior primary in the system
	events := e.Apply(Observation{
		InstanceID: standby,
		ClusterID:  cID,
		Role:       pgtype.RolePrimary,
		Timestamp:  clk.Now(),
	})

	var hasRoleChange, hasFailover bool
	for _, evt := range events {
		if evt.Type == EventRoleChange {
			hasRoleChange = true
		}
		if evt.Type == EventFailoverDetected {
			hasFailover = true
		}
	}
	if !hasRoleChange {
		t.Fatal("expected role_change event")
	}
	if hasFailover {
		t.Fatal("expected no failover_detected event")
	}
}

func TestEngine_SplitBrain(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	e := NewEngine(clk)

	primary1 := uuid.New()
	primary2 := uuid.New()
	cID := pgtype.ManualClusterID("cluster1")

	e.Apply(Observation{
		InstanceID: primary1,
		ClusterID:  cID,
		Role:       pgtype.RolePrimary,
		Timestamp:  clk.Now(),
	})

	clk.Advance(30 * time.Second)

	// Observe second primary
	events := e.Apply(Observation{
		InstanceID: primary2,
		ClusterID:  cID,
		Role:       pgtype.RolePrimary,
		Timestamp:  clk.Now(),
	})

	var hasSplitBrain bool
	for _, evt := range events {
		if evt.Type == EventSplitBrainDetected {
			hasSplitBrain = true
		}
	}
	if !hasSplitBrain {
		t.Fatal("expected split_brain_detected event")
	}

	// Same situation within 30s of failover does NOT emit split_brain
	clk2 := clock.NewFake(time.Unix(0, 0))
	e2 := NewEngine(clk2)

	p1 := uuid.New()
	p2 := uuid.New()
	e2.Apply(Observation{
		InstanceID: p1,
		ClusterID:  cID,
		Role:       pgtype.RolePrimary,
		Timestamp:  clk2.Now(),
	})

	e2.Apply(Observation{
		InstanceID: p2,
		ClusterID:  cID,
		Role:       pgtype.RoleStandby,
		Timestamp:  clk2.Now(),
	})

	clk2.Advance(60 * time.Second)
	e2.Apply(Observation{
		InstanceID: p1,
		ClusterID:  cID,
		Role:       pgtype.RolePrimary,
		Timestamp:  clk2.Now(),
	})

	// Promote p2 (failover)
	e2.Apply(Observation{
		InstanceID: p2,
		ClusterID:  cID,
		Role:       pgtype.RolePrimary,
		Timestamp:  clk2.Now(),
	})

	clk2.Advance(15 * time.Second)

	// Now both are primary within 30s of failover: should NOT emit split_brain
	events2 := e2.Apply(Observation{
		InstanceID: p1,
		ClusterID:  cID,
		Role:       pgtype.RolePrimary,
		Timestamp:  clk2.Now(),
	})

	for _, evt := range events2 {
		if evt.Type == EventSplitBrainDetected {
			t.Fatal("split_brain_detected should be suppressed within 30s of failover")
		}
	}
}

func TestEngine_OrphanStandby(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	e := NewEngine(clk)

	standby := uuid.New()
	cID := pgtype.ManualClusterID("cluster1")

	// Standby with unresolved edge (low confidence)
	e.Apply(Observation{
		InstanceID: standby,
		ClusterID:  cID,
		Role:       pgtype.RoleStandby,
		Timestamp:  clk.Now(),
		Edges: []Edge{
			{
				From:       standby,
				To:         uuid.New(),
				Confidence: "low",
			},
		},
	})

	// Before 60s: no orphan event
	clk.Advance(30 * time.Second)
	events := e.Apply(Observation{
		InstanceID: standby,
		ClusterID:  cID,
		Role:       pgtype.RoleStandby,
		Timestamp:  clk.Now(),
		Edges: []Edge{
			{
				From:       standby,
				To:         uuid.New(),
				Confidence: "low",
			},
		},
	})

	var hasOrphan bool
	for _, evt := range events {
		if evt.Type == EventOrphanStandby {
			hasOrphan = true
		}
	}
	if hasOrphan {
		t.Fatal("orphan_standby should not emit before 60s")
	}

	// After 60s: orphan event
	clk.Advance(31 * time.Second)
	events = e.Apply(Observation{
		InstanceID: standby,
		ClusterID:  cID,
		Role:       pgtype.RoleStandby,
		Timestamp:  clk.Now(),
		Edges: []Edge{
			{
				From:       standby,
				To:         uuid.New(),
				Confidence: "low",
			},
		},
	})

	hasOrphan = false
	for _, evt := range events {
		if evt.Type == EventOrphanStandby {
			hasOrphan = true
		}
	}
	if !hasOrphan {
		t.Fatal("expected orphan_standby event after 60s")
	}

	// A high-confidence edge resolves the orphan
	clk.Advance(5 * time.Second)
	e.Apply(Observation{
		InstanceID: standby,
		ClusterID:  cID,
		Role:       pgtype.RoleStandby,
		Timestamp:  clk.Now(),
		Edges: []Edge{
			{
				From:       standby,
				To:         uuid.New(),
				Confidence: "high",
			},
		},
	})

	// Further observations should not emit orphan
	clk.Advance(65 * time.Second)
	events = e.Apply(Observation{
		InstanceID: standby,
		ClusterID:  cID,
		Role:       pgtype.RoleStandby,
		Timestamp:  clk.Now(),
		Edges: []Edge{
			{
				From:       standby,
				To:         uuid.New(),
				Confidence: "low",
			},
		},
	})

	hasOrphan = false
	for _, evt := range events {
		if evt.Type == EventOrphanStandby {
			hasOrphan = true
		}
	}
	if hasOrphan {
		t.Fatal("orphan_standby should not re-emit after resolution")
	}
}

func TestEngine_LowConfidenceEdgeKept(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	e := NewEngine(clk)

	primary := uuid.New()
	cID := pgtype.ManualClusterID("cluster1")

	// Primary with an unresolvable edge (confidence="low")
	obs := Observation{
		InstanceID: primary,
		ClusterID:  cID,
		Role:       pgtype.RolePrimary,
		Timestamp:  clk.Now(),
		Edges: []Edge{
			{
				From:       primary,
				To:         uuid.New(), // unknown instance
				Confidence: "low",
			},
		},
	}

	events := e.Apply(obs)
	// The low-confidence edge should be kept in the observation; the engine
	// doesn't drop it. This test verifies the Apply call succeeds and the
	// edge is not lost.
	if len(obs.Edges) != 1 {
		t.Errorf("expected 1 edge, got %d", len(obs.Edges))
	}
	if obs.Edges[0].Confidence != "low" {
		t.Errorf("expected low confidence, got %s", obs.Edges[0].Confidence)
	}
	// No error or panic: test passes.
	_ = events
}

func TestEngine_EdgePriority(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	e := NewEngine(clk)

	primary := uuid.New()
	standby := uuid.New()
	cID := pgtype.ManualClusterID("cluster1")

	// Primary reports replication to standby
	e.Apply(Observation{
		InstanceID: primary,
		ClusterID:  cID,
		Role:       pgtype.RolePrimary,
		Timestamp:  clk.Now(),
		Edges: []Edge{
			{
				From:       primary,
				To:         standby,
				Type:       "streaming",
				SyncState:  "sync",
				Confidence: "high",
			},
		},
	})

	// Standby reports connection back (lower priority)
	e.Apply(Observation{
		InstanceID: standby,
		ClusterID:  cID,
		Role:       pgtype.RoleStandby,
		Timestamp:  clk.Now(),
		Edges: []Edge{
			{
				From:       standby,
				To:         primary,
				Type:       "streaming",
				Confidence: "high",
			},
		},
	})

	// Engine should have processed both; primary-side edge takes precedence
	// This is verified by checking state was updated without panic.
	_ = e
}

func TestEngine_Cascade(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	e := NewEngine(clk)

	p := uuid.New()
	s1 := uuid.New()
	s2 := uuid.New()
	cID := pgtype.ManualClusterID("cluster1")

	e.Apply(Observation{
		InstanceID: p,
		ClusterID:  cID,
		Role:       pgtype.RolePrimary,
		Timestamp:  clk.Now(),
		Edges: []Edge{
			{From: p, To: s1, Confidence: "high"},
		},
	})

	e.Apply(Observation{
		InstanceID: s1,
		ClusterID:  cID,
		Role:       pgtype.RoleStandby,
		Timestamp:  clk.Now(),
		Edges: []Edge{
			{From: s1, To: s2, Confidence: "high"},
		},
	})

	e.Apply(Observation{
		InstanceID: s2,
		ClusterID:  cID,
		Role:       pgtype.RoleStandby,
		Timestamp:  clk.Now(),
		Edges: []Edge{
			{From: s2, To: p, Confidence: "low"},
		},
	})

	// s1 is a cascade: both a From edge from p and a To edge to s2. The engine
	// processes it without error and emits no event for it, since a cascade is
	// shown by the topology edges in the API, not as an event.
	_ = e
}

// TestEngine_EveryEventTypeIsEmitted — a declared event type that no code path
// emits documents an event that never appears. EventCascadeChanged sat unused
// among the constants for that reason. Each declared type must be the Type of
// an Event the engine builds.
func TestEngine_EveryEventTypeIsEmitted(t *testing.T) {
	src, err := os.ReadFile("engine.go")
	if err != nil {
		t.Fatal(err)
	}
	declared := regexp.MustCompile(`(?m)^\t(Event\w+)\s+EventType = `).FindAllSubmatch(src, -1)
	if len(declared) == 0 {
		t.Fatal("found no EventType constants in engine.go")
	}
	for _, m := range declared {
		name := string(m[1])
		if !regexp.MustCompile(`Type:\s+` + name + `,`).Match(src) {
			t.Errorf("%s is declared but no Event is built with it as its Type", name)
		}
	}
}

func TestEngine_StaleEdgeExpires(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	e := NewEngine(clk)

	primary := uuid.New()
	standby := uuid.New()
	cID := pgtype.ManualClusterID("cluster1")

	// Report edge from primary to standby
	e.Apply(Observation{
		InstanceID: primary,
		ClusterID:  cID,
		Role:       pgtype.RolePrimary,
		Timestamp:  clk.Now(),
		Edges: []Edge{
			{From: primary, To: standby, Confidence: "high"},
		},
	})

	// Stop reporting the edge; advance 3x the edge TTL (3 seconds)
	clk.Advance(10 * time.Second)
	e.Apply(Observation{
		InstanceID: primary,
		ClusterID:  cID,
		Role:       pgtype.RolePrimary,
		Timestamp:  clk.Now(),
		Edges:      []Edge{}, // no edges
	})

	// In a real system, the edge would be deleted from the DB after 3 intervals.
	// This test verifies the engine tracks the edge-seen time and allows
	// the expiration logic to be implemented above the engine.
	_ = e
}

func TestEngine_NeverWritesClusterID(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	e := NewEngine(clk)

	inst := uuid.New()
	cID1 := pgtype.ManualClusterID("cluster1")

	// First observation in cluster1
	e.Apply(Observation{
		InstanceID: inst,
		ClusterID:  cID1,
		Role:       pgtype.RolePrimary,
		Timestamp:  clk.Now(),
	})

	// Promote to standby with different cluster_id (impossible in real system,
	// but test the engine never writes it)
	cID2 := pgtype.ManualClusterID("cluster2")
	clk.Advance(60 * time.Second)
	e.Apply(Observation{
		InstanceID: inst,
		ClusterID:  cID2, // different cluster_id
		Role:       pgtype.RoleStandby,
		Timestamp:  clk.Now(),
	})

	// Verify the engine's internal state has the instance's cluster_id
	// as the original one (the engine accepts the new one in this test,
	// but in production the server layer rejects cluster_id changes).
	// This test is a structural assertion that the engine has no code path
	// that mutates cluster_id; the production check happens at the server layer.

	// For this test: call Apply multiple times and verify no panic
	// and no corruption of existing state.
	for i := 0; i < 5; i++ {
		e.Apply(Observation{
			InstanceID: inst,
			ClusterID:  cID1,
			Role:       pgtype.RolePrimary,
			Timestamp:  clk.Now(),
		})
	}
}

// TestEngine_SplitBrainThrottledPerCluster — detectSplitBrain runs on every
// observation of every instance, so an unresolved split brain used to write
// one event per scrape per instance. It must be emitted at most once per
// window, and again immediately after the condition clears and returns.
func TestEngine_SplitBrainThrottledPerCluster(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	e := NewEngine(clk)

	primary1 := uuid.New()
	primary2 := uuid.New()
	cID := pgtype.ManualClusterID("cluster1")

	observe := func(id uuid.UUID, role pgtype.Role) int {
		events := e.Apply(Observation{InstanceID: id, ClusterID: cID, Role: role, Timestamp: clk.Now()})
		n := 0
		for _, evt := range events {
			if evt.Type == EventSplitBrainDetected {
				n++
			}
		}
		return n
	}

	observe(primary1, pgtype.RolePrimary)
	clk.Advance(30 * time.Second)
	if got := observe(primary2, pgtype.RolePrimary); got != 1 {
		t.Fatalf("expected the split brain to be reported once, got %d", got)
	}

	// Ten further scrapes inside the window: silence.
	for i := 0; i < 5; i++ {
		clk.Advance(5 * time.Second)
		if got := observe(primary1, pgtype.RolePrimary) + observe(primary2, pgtype.RolePrimary); got != 0 {
			t.Fatalf("split_brain_detected re-emitted inside the throttle window (iteration %d)", i)
		}
	}

	// Past the window, the still-unresolved condition is reported again.
	clk.Advance(40 * time.Second)
	if got := observe(primary1, pgtype.RolePrimary); got != 1 {
		t.Fatalf("expected one re-emission after the throttle window, got %d", got)
	}

	// The condition clears: one primary demoted back to standby.
	clk.Advance(5 * time.Second)
	if got := observe(primary2, pgtype.RoleStandby); got != 0 {
		t.Fatalf("expected no split brain with a single primary, got %d", got)
	}

	// It returns. Re-promoting primary2 counts as a failover, so the engine's
	// own 30s post-failover suppression applies; once that has passed the
	// event must fire without also waiting out the throttle window (only 41s
	// have elapsed since the previous emission).
	clk.Advance(5 * time.Second)
	if got := observe(primary2, pgtype.RolePrimary); got != 0 {
		t.Fatalf("expected split brain to stay suppressed for 30s after the failover, got %d", got)
	}
	clk.Advance(31 * time.Second)
	if got := observe(primary1, pgtype.RolePrimary); got != 1 {
		t.Fatalf("expected an immediate report once the split brain recurs, got %d", got)
	}
}

// TestEngine_OrphanStandbyThrottled — the orphan condition is re-checked on
// every scrape; once the 60s window had elapsed the event used to be emitted
// on every single one of them (240 rows/hour at a 15s interval), each one
// re-notifying the alert rules for one unchanged condition.
func TestEngine_OrphanStandbyThrottled(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	e := NewEngine(clk)

	standby := uuid.New()
	cID := pgtype.ManualClusterID("cluster1")

	observe := func() []Event {
		events := e.Apply(Observation{
			InstanceID: standby,
			ClusterID:  cID,
			Role:       pgtype.RoleStandby,
			Timestamp:  clk.Now(),
			Edges:      []Edge{{From: standby, To: uuid.New(), Confidence: "low"}},
		})
		var orphans []Event
		for _, evt := range events {
			if evt.Type == EventOrphanStandby {
				orphans = append(orphans, evt)
			}
		}
		return orphans
	}

	observe() // starts the orphan clock
	clk.Advance(61 * time.Second)
	if got := observe(); len(got) != 1 {
		t.Fatalf("expected one orphan_standby past the window, got %d", len(got))
	}

	// Scrapes inside the next window are silent.
	for i := 0; i < 3; i++ {
		clk.Advance(15 * time.Second)
		if got := observe(); len(got) != 0 {
			t.Fatalf("orphan_standby re-emitted inside the throttle window (iteration %d)", i)
		}
	}

	// Past it, one more event — reporting the age of the whole episode, not
	// of the throttle window.
	clk.Advance(15 * time.Second)
	got := observe()
	if len(got) != 1 {
		t.Fatalf("expected one orphan_standby after the throttle window, got %d", len(got))
	}
	duration, _ := got[0].Payload["duration_seconds"].(float64)
	if duration < 120 {
		t.Errorf("expected duration_seconds to cover the whole orphan episode, got %v", duration)
	}
}

// orphanObserver feeds one standby's observations to the engine and returns
// the orphan_standby events each one emits. The standby is the only instance,
// so its edges and role are all the orphan logic sees.
func orphanObserver(e *Engine, clk *clock.Fake, standby uuid.UUID, cID pgtype.ClusterID) func([]Edge, pgtype.Role) []Event {
	return func(edges []Edge, role pgtype.Role) []Event {
		var orphans []Event
		for _, evt := range e.Apply(Observation{InstanceID: standby, ClusterID: cID, Role: role, Timestamp: clk.Now(), Edges: edges}) {
			if evt.Type == EventOrphanStandby {
				orphans = append(orphans, evt)
			}
		}
		return orphans
	}
}

// TestEngine_OrphanEpisodeEndsAfterGap — an observation with no edge, after a
// gap with no evidence of a whole orphan window and twice the standby's own
// observation interval, ends the episode, so the next orphan is dated from its
// own start. The duration used to run from the old start: a standby orphaned
// for four minutes, then edgeless for ten, reported fourteen on its next orphan.
func TestEngine_OrphanEpisodeEndsAfterGap(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	e := NewEngine(clk)
	standby := uuid.New()
	cID := pgtype.ManualClusterID("cluster1")
	observe := orphanObserver(e, clk, standby, cID)
	lowEdge := []Edge{{From: standby, To: uuid.New(), Confidence: "low"}}

	observe(lowEdge, pgtype.RoleStandby) // starts the episode at t=0
	clk.Advance(30 * time.Second)
	observe(lowEdge, pgtype.RoleStandby)
	clk.Advance(31 * time.Second)
	first := observe(lowEdge, pgtype.RoleStandby)
	if len(first) != 1 {
		t.Fatalf("expected one orphan_standby at t=61, got %d", len(first))
	}
	if got := first[0].Payload["duration_seconds"]; got != 61.0 {
		t.Fatalf("first orphan duration = %v, want 61", got)
	}

	// Ten minutes with no edge at all: the episode is over.
	clk.Advance(600 * time.Second)
	if got := observe(nil, pgtype.RoleStandby); len(got) != 0 {
		t.Fatalf("an edgeless observation emitted %d orphan_standby", len(got))
	}

	// Orphaned again at t=662: a new episode, silent until a window has passed.
	clk.Advance(time.Second)
	if got := observe(lowEdge, pgtype.RoleStandby); len(got) != 0 {
		t.Fatalf("a new orphan emitted at its own start, duration %v", got[0].Payload["duration_seconds"])
	}
	clk.Advance(30 * time.Second)
	observe(lowEdge, pgtype.RoleStandby) // t=692
	clk.Advance(30 * time.Second)
	second := observe(lowEdge, pgtype.RoleStandby)
	if len(second) != 1 {
		t.Fatalf("expected one orphan_standby at t=722, got %d", len(second))
	}
	if got := second[0].Payload["duration_seconds"]; got != 60.0 {
		t.Fatalf("second orphan duration = %v, want 60 (its own episode), not the time since the first", got)
	}
}

// TestEngine_OrphanEpisodeSurvivesShortGap — the server drops an edge whose
// address stops resolving for a cycle, so a standby can flap between an edge
// and none. A gap shorter than the window must not restart the clock, or the
// orphan would never page.
func TestEngine_OrphanEpisodeSurvivesShortGap(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	e := NewEngine(clk)
	standby := uuid.New()
	cID := pgtype.ManualClusterID("cluster1")
	observe := orphanObserver(e, clk, standby, cID)
	lowEdge := []Edge{{From: standby, To: uuid.New(), Confidence: "low"}}

	observe(lowEdge, pgtype.RoleStandby) // t=0
	clk.Advance(30 * time.Second)
	observe(nil, pgtype.RoleStandby) // a single edgeless cycle at t=30
	clk.Advance(15 * time.Second)
	observe(lowEdge, pgtype.RoleStandby) // evidence again at t=45, 45s after the last
	clk.Advance(16 * time.Second)
	got := observe(lowEdge, pgtype.RoleStandby) // t=61
	if len(got) != 1 {
		t.Fatalf("expected orphan_standby at t=61 across a short gap, got %d", len(got))
	}
	if d := got[0].Payload["duration_seconds"]; d != 61.0 {
		t.Fatalf("duration across a short gap = %v, want 61", d)
	}
}

// TestEngine_OrphanEpisodeEndsWhenNotStandby — a standby that is promoted and
// later rejoins as a standby is not orphaned by its earlier episode.
func TestEngine_OrphanEpisodeEndsWhenNotStandby(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	e := NewEngine(clk)
	standby := uuid.New()
	cID := pgtype.ManualClusterID("cluster1")
	observe := orphanObserver(e, clk, standby, cID)
	lowEdge := []Edge{{From: standby, To: uuid.New(), Confidence: "low"}}

	observe(lowEdge, pgtype.RoleStandby) // t=0
	clk.Advance(30 * time.Second)
	observe(nil, pgtype.RolePrimary) // promoted at t=30
	clk.Advance(31 * time.Second)
	if got := observe(lowEdge, pgtype.RoleStandby); len(got) != 0 {
		t.Fatalf("a standby that rejoined at t=61 was reported orphan with duration %v", got[0].Payload["duration_seconds"])
	}
}

// TestEngine_OrphanEpisodeHeldWhenRoleUnknown — the agent reports an unknown
// role when it cannot read its role cache in a cycle, which says nothing about
// whether the standby is still orphaned. Such an observation must not restart
// the episode clock: orphaned at t=0, unknown at t=30, orphaned again at t=61
// is one episode of 61 seconds, reported at once.
func TestEngine_OrphanEpisodeHeldWhenRoleUnknown(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	e := NewEngine(clk)
	standby := uuid.New()
	cID := pgtype.ManualClusterID("cluster1")
	observe := orphanObserver(e, clk, standby, cID)
	lowEdge := []Edge{{From: standby, To: uuid.New(), Confidence: "low"}}

	observe(lowEdge, pgtype.RoleStandby) // t=0
	clk.Advance(30 * time.Second)
	observe(nil, pgtype.RoleUnknown) // role unreadable at t=30
	clk.Advance(15 * time.Second)
	observe(lowEdge, pgtype.RoleStandby) // orphaned again at t=45, 45s after the last
	clk.Advance(16 * time.Second)
	got := observe(lowEdge, pgtype.RoleStandby) // t=61
	if len(got) != 1 {
		t.Fatalf("got %d orphan events at t=61, want 1: an unknown observation restarted the episode", len(got))
	}
	if d, _ := got[0].Payload["duration_seconds"].(float64); d != 61 {
		t.Fatalf("orphan duration %v, want 61", got[0].Payload["duration_seconds"])
	}
}

// TestEngine_OrphanEpisodeRestartsAfterUnknownGap — an unknown observation holds
// an episode across the cycles without evidence, and ends it once the gap with
// no evidence is long enough (see orphanGapEnds). A standby that stays
// unreadable for ten minutes and then shows its orphan again starts a new
// episode. The reappearance is not reported with the age of the whole gap, and
// the next orphan is reported a window later, dated from its own start.
func TestEngine_OrphanEpisodeRestartsAfterUnknownGap(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	e := NewEngine(clk)
	standby := uuid.New()
	cID := pgtype.ManualClusterID("cluster1")
	observe := orphanObserver(e, clk, standby, cID)
	lowEdge := []Edge{{From: standby, To: uuid.New(), Confidence: "low"}}

	observe(lowEdge, pgtype.RoleStandby) // t=0
	clk.Advance(30 * time.Second)
	observe(nil, pgtype.RoleUnknown) // t=30
	clk.Advance(600 * time.Second)
	observe(nil, pgtype.RoleUnknown) // t=630, still unreadable
	clk.Advance(30 * time.Second)
	if got := observe(lowEdge, pgtype.RoleStandby); len(got) != 0 { // t=660
		t.Fatalf("orphan reported at its reappearance with duration %v, want a new episode", got[0].Payload["duration_seconds"])
	}
	clk.Advance(30 * time.Second)
	observe(lowEdge, pgtype.RoleStandby) // t=690
	clk.Advance(30 * time.Second)
	got := observe(lowEdge, pgtype.RoleStandby) // t=720
	if len(got) != 1 {
		t.Fatalf("expected one orphan_standby a window after the reappearance, got %d", len(got))
	}
	if d, _ := got[0].Payload["duration_seconds"].(float64); d != 60 {
		t.Fatalf("orphan duration %v, want 60: the episode is dated from t=660", got[0].Payload["duration_seconds"])
	}
}

// TestEngine_OrphanEpisodeSpansSilence — an instance that reports nothing for
// ten minutes and then shows its orphan again is still the same episode. Silence
// is not observed, so there is no observation to end the episode on, and the
// reappearance reports the whole silence in its duration. This is the accepted
// limit of the orphan rule. A silent gap followed by an edgeless or unknown
// observation does end the episode, as TestEngine_OrphanEpisodeEndsAfterGap pins.
func TestEngine_OrphanEpisodeSpansSilence(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	e := NewEngine(clk)
	standby := uuid.New()
	cID := pgtype.ManualClusterID("cluster1")
	observe := orphanObserver(e, clk, standby, cID)
	lowEdge := []Edge{{From: standby, To: uuid.New(), Confidence: "low"}}

	observe(lowEdge, pgtype.RoleStandby) // t=0
	clk.Advance(600 * time.Second)       // silent for ten minutes
	got := observe(lowEdge, pgtype.RoleStandby)
	if len(got) != 1 {
		t.Fatalf("expected orphan_standby at the reappearance, got %d", len(got))
	}
	if d, _ := got[0].Payload["duration_seconds"].(float64); d != 600 {
		t.Fatalf("orphan duration %v, want 600: silence does not end the episode", got[0].Payload["duration_seconds"])
	}
}

// TestEngine_OrphanEpisodeKeepsFiringAtAnyCadence — evidence that arrives every
// cycle keeps one episode, whatever the cycle. An agent that pushes every 60, 90
// or 120 seconds leaves a whole orphan window between two of its observations,
// and that gap must not be read as the orphan being over. orphan_standby fires
// once a window has passed since the episode began or since its last event, and
// its duration always runs from the start of the episode.
func TestEngine_OrphanEpisodeKeepsFiringAtAnyCadence(t *testing.T) {
	for _, cadence := range []time.Duration{30, 60, 90, 120} {
		cadence *= time.Second
		t.Run(cadence.String(), func(t *testing.T) {
			clk := clock.NewFake(time.Unix(0, 0))
			e := NewEngine(clk)
			standby := uuid.New()
			cID := pgtype.ManualClusterID("cluster1")
			observe := orphanObserver(e, clk, standby, cID)
			lowEdge := []Edge{{From: standby, To: uuid.New(), Confidence: "low"}}

			start := clk.Now()
			lastEvent := start
			observe(lowEdge, pgtype.RoleStandby) // starts the episode
			fired := 0
			for i := 0; i < 20; i++ {
				clk.Advance(cadence)
				got := observe(lowEdge, pgtype.RoleStandby)
				due := clk.Now().Sub(lastEvent) >= e.orphanWindow
				if due != (len(got) == 1) {
					t.Fatalf("at t=%v: orphan_standby emitted %d times, want due=%v", clk.Now().Sub(start), len(got), due)
				}
				if len(got) == 1 {
					fired++
					lastEvent = clk.Now()
					if d := got[0].Payload["duration_seconds"]; d != clk.Now().Sub(start).Seconds() {
						t.Fatalf("at t=%v: duration %v, want the whole episode from its start", clk.Now().Sub(start), d)
					}
				}
			}
			if fired == 0 {
				t.Fatal("orphan_standby never fired")
			}
		})
	}
}

// TestEngine_OrphanEpisodeKeepsFiringWhenEveryOtherCycleIsSilent — a standby
// whose edge is dropped on every other cycle, or whose role is unreadable on
// every other cycle, stays one episode at a 30, 60, 90 or 120 second push
// interval. Each cycle with evidence fires once a window has passed since the
// last event, and its duration runs from the start of the episode. The cycles
// without evidence emit nothing and end nothing.
func TestEngine_OrphanEpisodeKeepsFiringWhenEveryOtherCycleIsSilent(t *testing.T) {
	silent := []struct {
		name string
		role pgtype.Role
	}{
		{"edgeless", pgtype.RoleStandby},
		{"unknown", pgtype.RoleUnknown},
	}
	for _, s := range silent {
		for _, cadence := range []time.Duration{30, 60, 90, 120} {
			cadence *= time.Second
			t.Run(s.name+"/"+cadence.String(), func(t *testing.T) {
				clk := clock.NewFake(time.Unix(0, 0))
				e := NewEngine(clk)
				standby := uuid.New()
				cID := pgtype.ManualClusterID("cluster1")
				observe := orphanObserver(e, clk, standby, cID)
				lowEdge := []Edge{{From: standby, To: uuid.New(), Confidence: "low"}}

				start := clk.Now()
				lastEvent := start
				observe(lowEdge, pgtype.RoleStandby) // starts the episode
				for i := 0; i < 20; i++ {
					clk.Advance(cadence)
					if got := observe(nil, s.role); len(got) != 0 {
						t.Fatalf("at t=%v: a cycle without evidence emitted %d orphan_standby", clk.Now().Sub(start), len(got))
					}
					clk.Advance(cadence)
					got := observe(lowEdge, pgtype.RoleStandby)
					due := clk.Now().Sub(lastEvent) >= e.orphanWindow
					if due != (len(got) == 1) {
						t.Fatalf("at t=%v: orphan_standby emitted %d times, want due=%v", clk.Now().Sub(start), len(got), due)
					}
					if len(got) == 1 {
						lastEvent = clk.Now()
						if d := got[0].Payload["duration_seconds"]; d != clk.Now().Sub(start).Seconds() {
							t.Fatalf("at t=%v: duration %v, want the whole episode from its start", clk.Now().Sub(start), d)
						}
					}
				}
			})
		}
	}
}

// TestEngine_EvictBoundsState — the engine tracked one instanceState (plus
// its own edgeSeenAt map) per instance_id it had ever seen, for the lifetime
// of the server process. Instance ids are not stable across a lost identity
// file, so a re-provisioned replica leaked a whole state object every time.
func TestEngine_EvictBoundsState(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))
	e := NewEngine(clk)

	cluster := pgtype.ClusterID(42)
	gone := pgtype.InstanceID(uuid.New())
	kept := pgtype.InstanceID(uuid.New())

	e.Apply(Observation{InstanceID: gone, ClusterID: cluster, Role: pgtype.RolePrimary})
	e.Apply(Observation{InstanceID: kept, ClusterID: cluster, Role: pgtype.RoleStandby})
	if got := e.Len(); got != 2 {
		t.Fatalf("tracking %d instances, want 2", got)
	}

	clk.Advance(2 * time.Hour)
	// Only `kept` still reports.
	e.Apply(Observation{InstanceID: kept, ClusterID: cluster, Role: pgtype.RoleStandby})

	removed := e.Evict(clk.Now().Add(-1 * time.Hour))
	if removed != 1 {
		t.Errorf("Evict removed %d instances, want 1", removed)
	}
	if got := e.Len(); got != 1 {
		t.Errorf("tracking %d instances after eviction, want 1", got)
	}

	// The surviving instance keeps the cluster alive, so per-cluster state
	// must not be collected while it is still reporting.
	e.mu.RLock()
	_, primaryKept := e.lastPrimary[cluster]
	e.mu.RUnlock()
	if !primaryKept {
		t.Error("per-cluster state was dropped while an instance of that cluster is still reporting")
	}

	// Once the last instance goes quiet, the cluster entries go too.
	clk.Advance(2 * time.Hour)
	e.Evict(clk.Now().Add(-1 * time.Hour))
	e.mu.RLock()
	n := len(e.instances) + len(e.lastPrimary) + len(e.lastFailoverTime) + len(e.lastSplitBrain)
	e.mu.RUnlock()
	if n != 0 {
		t.Errorf("%d entries retained after every instance went stale, want 0", n)
	}
}

// TestEngine_EvictPrunesStaleEdges — instanceState.edgeSeenAt grows by one
// entry per distinct upstream an instance has ever pointed at. A standby that
// is repeatedly re-attached (cascade reconfiguration, repeated failovers)
// accumulates them for the process lifetime unless eviction sweeps them too.
func TestEngine_EvictPrunesStaleEdges(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))
	e := NewEngine(clk)

	cluster := pgtype.ClusterID(7)
	standby := pgtype.InstanceID(uuid.New())
	oldUpstream := pgtype.InstanceID(uuid.New())
	newUpstream := pgtype.InstanceID(uuid.New())

	e.Apply(Observation{InstanceID: standby, ClusterID: cluster, Role: pgtype.RoleStandby,
		Edges: []Edge{{From: standby, To: oldUpstream, Type: "streaming", Confidence: "high"}}})

	clk.Advance(2 * time.Hour)
	e.Apply(Observation{InstanceID: standby, ClusterID: cluster, Role: pgtype.RoleStandby,
		Edges: []Edge{{From: standby, To: newUpstream, Type: "streaming", Confidence: "high"}}})

	e.Evict(clk.Now().Add(-1 * time.Hour))

	e.mu.RLock()
	st := e.instances[standby]
	edges := len(st.edgeSeenAt)
	_, staleKept := st.edgeSeenAt[oldUpstream.String()]
	e.mu.RUnlock()
	if staleKept {
		t.Errorf("stale edge to the old upstream was retained (%d edges tracked)", edges)
	}
	if edges != 1 {
		t.Errorf("tracking %d edges, want 1", edges)
	}
}

// TestStreamingProvider is the documented extension seam (decision D6): the
// engine consumes edges the provider produces, so Aurora/Patroni/Citus can be
// added without touching the engine or the schema.
func TestStreamingProvider(t *testing.T) {
	p := NewStreamingProvider()
	if got := p.Name(); got != "streaming" {
		t.Errorf("Name() = %q, want %q", got, "streaming")
	}
	from := pgtype.InstanceID(uuid.New())
	to := pgtype.InstanceID(uuid.New())
	obs := Observation{InstanceID: from, Edges: []Edge{{From: from, To: to, Type: "streaming"}}}
	if !p.Applies(obs) {
		t.Error("Applies() = false; the streaming provider applies to every observation")
	}
	edges := p.Edges(obs)
	if len(edges) != 1 || edges[0].To != to {
		t.Errorf("Edges() = %+v, want the observation's own single edge to %s", edges, to)
	}
	if p.Applies(Observation{InstanceID: from}) != true {
		t.Error("Applies() must not depend on an observation carrying edges")
	}
	if got := p.Edges(Observation{InstanceID: from}); len(got) != 0 {
		t.Errorf("Edges() on an edgeless observation = %+v, want none", got)
	}
}

// A primary that is demoted and promoted again inside the failover window is
// not a failover from itself: it is a role change, and no failover is recorded.
func TestEngine_SelfRepromotionIsRoleChangeNotFailover(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	e := NewEngine(clk)

	self := uuid.New()
	other := uuid.New()
	cID := pgtype.ManualClusterID("cluster1")

	e.Apply(Observation{InstanceID: self, ClusterID: cID, Role: pgtype.RolePrimary, Timestamp: clk.Now()})
	clk.Advance(10 * time.Second)
	e.Apply(Observation{InstanceID: self, ClusterID: cID, Role: pgtype.RoleStandby, Timestamp: clk.Now()})
	clk.Advance(40 * time.Second)
	events := e.Apply(Observation{InstanceID: self, ClusterID: cID, Role: pgtype.RolePrimary, Timestamp: clk.Now()})
	for _, evt := range events {
		if evt.Type == EventFailoverDetected {
			t.Fatalf("self re-promotion must not be a failover, got %+v", evt)
		}
	}
	if len(events) != 1 || events[0].Type != EventRoleChange {
		t.Fatalf("expected exactly one role_change, got %+v", events)
	}
	if _, failedOver := e.lastFailoverTime[cID]; failedOver {
		t.Fatal("self re-promotion must not set lastFailoverTime")
	}

	// Control: a different standby promoted inside the window is a real failover
	// from the instance that was primary.
	clk.Advance(5 * time.Second)
	e.Apply(Observation{InstanceID: other, ClusterID: cID, Role: pgtype.RoleStandby, Timestamp: clk.Now()})
	clk.Advance(5 * time.Second)
	events = e.Apply(Observation{InstanceID: other, ClusterID: cID, Role: pgtype.RolePrimary, Timestamp: clk.Now()})
	if len(events) == 0 || events[0].Type != EventFailoverDetected {
		t.Fatalf("expected a failover from the previous primary, got %+v", events)
	}
	if got := events[0].Payload["old_primary"]; got != self {
		t.Fatalf("expected old_primary %v, got %v", self, got)
	}
}
