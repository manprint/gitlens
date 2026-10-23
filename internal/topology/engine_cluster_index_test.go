package topology

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
)

// requireClusterIndex checks that the cluster index holds exactly the instances
// given per cluster, and that every indexed instance is tracked and reports that
// cluster. The split-brain check reads one cluster's set, so a stale or missing
// member would change its answer without any other symptom.
func requireClusterIndex(t *testing.T, e *Engine, want map[pgtype.ClusterID][]uuid.UUID) {
	t.Helper()
	for cid, ids := range want {
		members := e.clusterInstances[cid]
		if len(members) != len(ids) {
			t.Fatalf("cluster %v indexes %d instances, want %d", cid, len(members), len(ids))
		}
		for _, id := range ids {
			if _, ok := members[id]; !ok {
				t.Fatalf("cluster %v does not index instance %v", cid, id)
			}
		}
	}
	for cid, members := range e.clusterInstances {
		for id := range members {
			inst, ok := e.instances[id]
			if !ok || inst.clusterID != cid {
				t.Fatalf("cluster %v indexes instance %v, which the engine does not track there", cid, id)
			}
		}
	}
}

// The index follows an instance that re-registers into another cluster, and
// Evict removes an evicted instance from a cluster that stays live. A stale
// member there would name an instance the engine no longer tracks.
func TestEngine_ClusterIndexFollowsInstances(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	e := NewEngine(clk)
	a, b := pgtype.ManualClusterID("a"), pgtype.ManualClusterID("b")
	x, y, w := uuid.New(), uuid.New(), uuid.New()

	e.Apply(Observation{InstanceID: x, ClusterID: a, Role: pgtype.RolePrimary, Timestamp: clk.Now()})
	e.Apply(Observation{InstanceID: y, ClusterID: a, Role: pgtype.RoleStandby, Timestamp: clk.Now()})
	requireClusterIndex(t, e, map[pgtype.ClusterID][]uuid.UUID{a: {x, y}})

	e.Apply(Observation{InstanceID: x, ClusterID: b, Role: pgtype.RolePrimary, Timestamp: clk.Now()})
	requireClusterIndex(t, e, map[pgtype.ClusterID][]uuid.UUID{a: {y}, b: {x}})

	// y goes quiet while w joins a and x keeps reporting: only y is evicted,
	// and cluster a stays live.
	clk.Advance(time.Hour)
	e.Apply(Observation{InstanceID: w, ClusterID: a, Role: pgtype.RoleStandby, Timestamp: clk.Now()})
	e.Apply(Observation{InstanceID: x, ClusterID: b, Role: pgtype.RolePrimary, Timestamp: clk.Now()})
	if n := e.Evict(clk.Now().Add(-time.Minute)); n != 1 {
		t.Fatalf("Evict removed %d instances, want 1", n)
	}
	requireClusterIndex(t, e, map[pgtype.ClusterID][]uuid.UUID{a: {w}, b: {x}})
}

// An instance that moves to another cluster is no longer a primary of the
// cluster it left, so the split-brain check there must not count it.
func TestEngine_ClusterMoveLeavesNoPrimaryBehind(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	e := NewEngine(clk)
	a, b := pgtype.ManualClusterID("a"), pgtype.ManualClusterID("b")
	x, z := uuid.New(), uuid.New()

	e.Apply(Observation{InstanceID: x, ClusterID: a, Role: pgtype.RolePrimary, Timestamp: clk.Now()})
	if !hasEventType(e.Apply(Observation{InstanceID: z, ClusterID: a, Role: pgtype.RolePrimary, Timestamp: clk.Now()}), EventSplitBrainDetected) {
		t.Fatal("two primaries in one cluster must be a split brain")
	}

	e.Apply(Observation{InstanceID: x, ClusterID: b, Role: pgtype.RolePrimary, Timestamp: clk.Now()})
	// Past the split-brain throttle, so a count that still includes x would emit.
	clk.Advance(61 * time.Second)
	if hasEventType(e.Apply(Observation{InstanceID: z, ClusterID: a, Role: pgtype.RolePrimary, Timestamp: clk.Now()}), EventSplitBrainDetected) {
		t.Fatal("cluster a still counts the instance that moved to cluster b as a primary")
	}
}

func hasEventType(events []Event, typ EventType) bool {
	for _, evt := range events {
		if evt.Type == typ {
			return true
		}
	}
	return false
}

// BenchmarkApplyOneClusterAmongMany measures one observation while the engine
// tracks many instances of other clusters. The split-brain check used to scan
// every tracked instance on each Apply; it now scans the observed cluster only.
func BenchmarkApplyOneClusterAmongMany(b *testing.B) {
	clk := clock.NewFake(time.Unix(0, 0))
	e := NewEngine(clk)
	for i := 0; i < 1000; i++ {
		e.Apply(Observation{
			InstanceID: uuid.New(),
			ClusterID:  pgtype.ManualClusterID(fmt.Sprintf("other-%d", i%50)),
			Role:       pgtype.RoleStandby,
			Timestamp:  clk.Now(),
		})
	}
	id := uuid.New()
	cid := pgtype.ManualClusterID("observed")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Apply(Observation{InstanceID: id, ClusterID: cid, Role: pgtype.RoleStandby, Timestamp: clk.Now()})
	}
}
