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
