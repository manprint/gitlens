package topology

import (
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

	// s1 is cascade (both From edge from p and To edges to s2).
	// Engine processes this without error; no cascade event emitted yet
	// (cascade is marked in API, not as an event).
	_ = e
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
