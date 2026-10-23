package topology

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
)

// A failover leaves the demoted primary silent: the agent omits an unreachable
// target, so its row keeps role=primary. That row must not count as a second
// primary, and a primary that really comes back must still raise the alert.
func TestEngine_SilentOldPrimaryAfterFailoverIsNotSplitBrain(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	e := NewEngine(clk)
	cID := pgtype.ManualClusterID("cluster1")
	oldPrimary := uuid.New()
	standby := uuid.New()

	var failovers int
	var splitBrainAt []time.Duration
	start := clk.Now()
	observe := func(id uuid.UUID, role pgtype.Role) {
		t.Helper()
		for _, evt := range e.Apply(Observation{InstanceID: id, ClusterID: cID, Role: role, Timestamp: clk.Now()}) {
			switch evt.Type {
			case EventFailoverDetected:
				failovers++
			case EventSplitBrainDetected:
				splitBrainAt = append(splitBrainAt, clk.Now().Sub(start))
			}
		}
	}

	// Both report every 5s until t=60; then the old primary dies. The standby
	// is promoted at t=65 and reports as primary every 5s from then on.
	for i := 0; i <= 12; i++ {
		observe(oldPrimary, pgtype.RolePrimary)
		observe(standby, pgtype.RoleStandby)
		clk.Advance(5 * time.Second)
	}
	for clk.Now().Sub(start) <= 100*time.Second {
		observe(standby, pgtype.RolePrimary)
		clk.Advance(5 * time.Second)
	}
	if failovers != 1 {
		t.Fatalf("expected one failover_detected, got %d", failovers)
	}
	if len(splitBrainAt) != 0 {
		t.Fatalf("a silent demoted primary raised split_brain_detected at %v after the start", splitBrainAt)
	}

	// The old primary comes back as primary 40s after the failover: a real
	// split brain, which must be reported.
	observe(oldPrimary, pgtype.RolePrimary)
	observe(standby, pgtype.RolePrimary)
	if len(splitBrainAt) != 1 {
		t.Fatalf("expected split_brain_detected once the old primary is primary again, got %v", splitBrainAt)
	}
}

// The failover check needs the standby to have been seen as standby. An unknown
// role in between (the agent could not read it that cycle) hides the promotion
// from that check, but it is still a handover from the primary seen just before.
// The old primary is silent afterwards, so it must not count as a second
// primary. The promotion stays a role_change, with no failover_detected.
func TestEngine_UnknownStandbyPromotionIsNotSplitBrain(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	e := NewEngine(clk)
	cID := pgtype.ManualClusterID("cluster1")
	oldPrimary := uuid.New()
	standby := uuid.New()
	observe := func(id uuid.UUID, role pgtype.Role) []Event {
		return e.Apply(Observation{InstanceID: id, ClusterID: cID, Role: role, Timestamp: clk.Now()})
	}

	observe(standby, pgtype.RoleStandby) // t=0
	clk.Advance(55 * time.Second)
	observe(oldPrimary, pgtype.RolePrimary) // t=55, then silent
	clk.Advance(5 * time.Second)
	observe(standby, pgtype.RoleUnknown) // t=60
	clk.Advance(5 * time.Second)
	events := observe(standby, pgtype.RolePrimary) // t=65, promoted
	if len(events) != 1 || events[0].Type != EventRoleChange {
		t.Fatalf("promotion after an unknown role: got %v, want one role_change", eventTypes(events))
	}
	for clk.Now().Sub(time.Unix(0, 0)) < 125*time.Second {
		clk.Advance(5 * time.Second)
		for _, evt := range observe(standby, pgtype.RolePrimary) {
			if evt.Type == EventSplitBrainDetected {
				t.Fatalf("split_brain_detected at t=%v for a silent demoted primary", clk.Now().Sub(time.Unix(0, 0)))
			}
		}
	}
}

// A primary that is still up after the promotion is a real split brain, and it
// is reported once the 30s grace after the failover has passed.
func TestEngine_SplitBrainAfterUnknownPromotionIsReported(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	e := NewEngine(clk)
	cID := pgtype.ManualClusterID("cluster1")
	oldPrimary := uuid.New()
	standby := uuid.New()
	observe := func(id uuid.UUID, role pgtype.Role) []Event {
		return e.Apply(Observation{InstanceID: id, ClusterID: cID, Role: role, Timestamp: clk.Now()})
	}

	observe(standby, pgtype.RoleStandby) // t=0
	clk.Advance(55 * time.Second)
	observe(oldPrimary, pgtype.RolePrimary) // t=55
	clk.Advance(5 * time.Second)
	observe(standby, pgtype.RoleUnknown) // t=60
	clk.Advance(5 * time.Second)
	var splitBrainAt []time.Duration
	collect := func(events []Event) {
		for _, evt := range events {
			if evt.Type == EventSplitBrainDetected {
				splitBrainAt = append(splitBrainAt, clk.Now().Sub(time.Unix(0, 0)))
			}
		}
	}
	collect(observe(standby, pgtype.RolePrimary)) // t=65, promoted
	for clk.Now().Sub(time.Unix(0, 0)) < 105*time.Second {
		clk.Advance(5 * time.Second)
		collect(observe(oldPrimary, pgtype.RolePrimary))
		collect(observe(standby, pgtype.RolePrimary))
	}
	if len(splitBrainAt) != 1 || splitBrainAt[0] != 100*time.Second {
		t.Fatalf("split_brain_detected at %v, want once at 100s, the first sighting after the 30s grace", splitBrainAt)
	}
}

func eventTypes(events []Event) []EventType {
	var types []EventType
	for _, evt := range events {
		types = append(types, evt.Type)
	}
	return types
}
