package topology

import (
	"sync"
	"time"

	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
)

// Observation represents one instance's topology state at a moment in time.
type Observation struct {
	InstanceID pgtype.InstanceID
	ClusterID  pgtype.ClusterID
	Role       pgtype.Role
	Timestamp  time.Time
	Edges      []Edge
}

// Edge describes a replication connection from one instance to another.
type Edge struct {
	From       pgtype.InstanceID
	To         pgtype.InstanceID
	Type       string // "streaming"
	SyncState  string // "sync", "async", ""
	Confidence string // "high", "low"
}

// EventType describes the kind of topology change.
type EventType string

const (
	EventFailoverDetected   EventType = "failover_detected"
	EventRoleChange         EventType = "role_change"
	EventSplitBrainDetected EventType = "split_brain_detected"
	EventOrphanStandby      EventType = "orphan_standby"
	EventCascadeChanged     EventType = "cascade_changed"
)

// Event is emitted when a topology transition occurs.
type Event struct {
	Type       EventType
	Timestamp  time.Time
	InstanceID pgtype.InstanceID
	ClusterID  pgtype.ClusterID
	Payload    map[string]any // old_primary, new_primary, etc.
}

// instanceState tracks the last seen state of an instance.
type instanceState struct {
	role       pgtype.Role
	clusterID  pgtype.ClusterID
	timestamp  time.Time
	orphanAt   time.Time            // when the standby's upstream became unresolvable
	orphanedAt time.Time            // when orphan_standby was last emitted for this standby
	edgeSeenAt map[string]time.Time // per-edge last-seen time (key = "to_instance_id")
}

// Engine detects topology changes in a cluster and emits events.
type Engine struct {
	mu               sync.RWMutex
	clock            clock.Clock
	instances        map[pgtype.InstanceID]*instanceState  // per instance
	lastPrimary      map[pgtype.ClusterID]*lastPrimaryInfo // track recent primary per cluster
	lastFailoverTime map[pgtype.ClusterID]time.Time        // track last failover time per cluster
	lastSplitBrain   map[pgtype.ClusterID]time.Time        // last split_brain_detected emission per cluster
	failoverWindow   time.Duration
	orphanWindow     time.Duration
	splitBrainWindow time.Duration
	edgeTTL          time.Duration
}

// lastPrimaryInfo tracks the most recent primary in a cluster for failover detection.
type lastPrimaryInfo struct {
	instanceID pgtype.InstanceID
	timestamp  time.Time // when we last saw it as primary
}

// NewEngine creates a topology engine with injectable clock.
func NewEngine(clk clock.Clock) *Engine {
	if clk == nil {
		clk = clock.System()
	}
	return &Engine{
		clock:            clk,
		instances:        make(map[pgtype.InstanceID]*instanceState),
		lastPrimary:      make(map[pgtype.ClusterID]*lastPrimaryInfo),
		lastFailoverTime: make(map[pgtype.ClusterID]time.Time),
		lastSplitBrain:   make(map[pgtype.ClusterID]time.Time),
		failoverWindow:   120 * time.Second,
		orphanWindow:     60 * time.Second,
		splitBrainWindow: 60 * time.Second,
		edgeTTL:          3 * time.Second, // placeholder: 3x sampling interval (assumed 1s)
	}
}

// Apply processes one instance observation and returns the events emitted.
func (e *Engine) Apply(obs Observation) []Event {
	e.mu.Lock()
	defer e.mu.Unlock()

	var events []Event
	now := e.clock.Now()

	// Get or create the instance state.
	inst, exists := e.instances[obs.InstanceID]
	if !exists {
		inst = &instanceState{
			edgeSeenAt: make(map[string]time.Time),
		}
		e.instances[obs.InstanceID] = inst
	}

	// Detect role change.
	if inst.role != obs.Role {
		// Check if this is a failover.
		if obs.Role == pgtype.RolePrimary && inst.role == pgtype.RoleStandby {
			if lastPrim, ok := e.lastPrimary[obs.ClusterID]; ok && now.Sub(lastPrim.timestamp) < e.failoverWindow {
				// This is a failover: standby -> primary with recent prior primary
				events = append(events, Event{
					Type:       EventFailoverDetected,
					Timestamp:  now,
					InstanceID: obs.InstanceID,
					ClusterID:  obs.ClusterID,
					Payload: map[string]any{
						"old_primary": lastPrim.instanceID,
						"new_primary": obs.InstanceID,
					},
				})
				e.lastFailoverTime[obs.ClusterID] = now
			} else {
				// No recent primary or first sighting: role_change, not failover
				events = append(events, Event{
					Type:       EventRoleChange,
					Timestamp:  now,
					InstanceID: obs.InstanceID,
					ClusterID:  obs.ClusterID,
					Payload:    map[string]any{"new_role": obs.Role},
				})
			}
		} else if inst.role != "" {
			// Any other role transition (not a first sighting)
			events = append(events, Event{
				Type:       EventRoleChange,
				Timestamp:  now,
				InstanceID: obs.InstanceID,
				ClusterID:  obs.ClusterID,
				Payload:    map[string]any{"old_role": inst.role, "new_role": obs.Role},
			})
		} else {
			// First sighting
			events = append(events, Event{
				Type:       EventRoleChange,
				Timestamp:  now,
				InstanceID: obs.InstanceID,
				ClusterID:  obs.ClusterID,
				Payload:    map[string]any{"new_role": obs.Role},
			})
		}
	}

	// Update instance state.
	inst.role = obs.Role
	inst.clusterID = obs.ClusterID
	inst.timestamp = now

	// Track most recent primary per cluster.
	if obs.Role == pgtype.RolePrimary {
		e.lastPrimary[obs.ClusterID] = &lastPrimaryInfo{
			instanceID: obs.InstanceID,
			timestamp:  now,
		}
	}

	// Process edges: mark seen edges and detect orphan standby.
	if obs.Role == pgtype.RoleStandby {
		hasResolvedEdge := false
		for _, edge := range obs.Edges {
			if edge.Confidence == "high" {
				hasResolvedEdge = true
				edgeKey := edge.To.String()
				inst.edgeSeenAt[edgeKey] = now
				inst.orphanAt = time.Time{}
				inst.orphanedAt = time.Time{}
			}
		}

		// If no high-confidence edge and we have low-confidence edges, check orphan.
		if !hasResolvedEdge && len(obs.Edges) > 0 {
			if inst.orphanAt.IsZero() {
				inst.orphanAt = now
			} else if now.Sub(inst.orphanAt) >= e.orphanWindow && now.Sub(inst.orphanedAt) >= e.orphanWindow {
				// Emit orphan_standby at most once per orphanWindow, and keep
				// reporting the age of the *whole* orphan episode.
				//
				// The re-emission guard is orphanedAt, not orphanAt: without it
				// this fired on every single observation once the window had
				// passed, so a standby orphaned for an hour wrote one event per
				// scrape (240 rows at a 15s interval) — flooding the events
				// table and re-notifying the split_brain/orphan alert rules over
				// and over for one unchanged condition. Resetting orphanAt
				// instead would have thrown away the episode's start time and
				// pinned duration_seconds at ~60s forever.
				inst.orphanedAt = now
				events = append(events, Event{
					Type:       EventOrphanStandby,
					Timestamp:  now,
					InstanceID: obs.InstanceID,
					ClusterID:  obs.ClusterID,
					Payload:    map[string]any{"duration_seconds": now.Sub(inst.orphanAt).Seconds()},
				})
			}
		}
	}

	// Detect split-brain: 2+ primaries in same cluster within 30s of each other
	// (unless a failover just happened).
	events = append(events, e.detectSplitBrain(obs.ClusterID, now)...)

	return events
}

// detectSplitBrain checks for multiple primary instances in one cluster.
func (e *Engine) detectSplitBrain(clusterID pgtype.ClusterID, now time.Time) []Event {
	var events []Event
	var primaries []pgtype.InstanceID

	for instID, inst := range e.instances {
		if inst.clusterID == clusterID && inst.role == pgtype.RolePrimary {
			// Only count primaries seen recently (within failover window)
			if now.Sub(inst.timestamp) < e.failoverWindow {
				primaries = append(primaries, instID)
			}
		}
	}

	// Only emit split-brain if we have 2+ primaries
	if len(primaries) >= 2 {
		// Check if there was a recent failover to suppress transient split-brain
		lastFailover := e.lastFailoverTime[clusterID]
		if lastFailover.IsZero() || now.Sub(lastFailover) > 30*time.Second {
			// Throttled per cluster: this runs on every observation of every
			// instance, so an unresolved split brain used to write one event
			// per scrape per instance for as long as it lasted.
			if last, seen := e.lastSplitBrain[clusterID]; !seen || now.Sub(last) >= e.splitBrainWindow {
				e.lastSplitBrain[clusterID] = now
				events = append(events, Event{
					Type:      EventSplitBrainDetected,
					Timestamp: now,
					ClusterID: clusterID,
					Payload: map[string]any{
						"primary_count": len(primaries),
					},
				})
			}
		}
	} else {
		// Condition cleared: the next genuine occurrence must alert at once.
		delete(e.lastSplitBrain, clusterID)
	}

	return events
}

// Evict drops per-instance and per-cluster state whose last observation is
// older than the cutoff, and returns how many instances were removed.
//
// Nothing bounded this engine before: instances, lastPrimary,
// lastFailoverTime, lastSplitBrain and every instanceState's own edgeSeenAt
// map grew for the life of the server process and were never once pruned.
// A pglens server is long-lived and its instance set is not static — an
// instance re-registers with a fresh instance_id whenever its identity file
// is lost (a recreated container, a restored volume, a re-provisioned
// replica), so every such event permanently leaked one instanceState plus
// one edgeSeenAt entry per upstream it ever pointed at. The delta engine
// beside it has had exactly this method, called from the same place, since
// it was written; the topology engine simply never got one.
func (e *Engine) Evict(before time.Time) int {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Clusters are kept alive by any instance still reporting into them, so
	// they are collected after the instance sweep rather than on their own
	// timestamps (lastFailoverTime and lastSplitBrain only advance on an
	// event, which may be far older than the cluster's last observation).
	liveClusters := make(map[pgtype.ClusterID]struct{}, len(e.instances))
	removed := 0
	for id, st := range e.instances {
		if st.timestamp.Before(before) {
			delete(e.instances, id)
			removed++
			continue
		}
		liveClusters[st.clusterID] = struct{}{}
		for edge, seen := range st.edgeSeenAt {
			if seen.Before(before) {
				delete(st.edgeSeenAt, edge)
			}
		}
	}
	for cid := range e.lastPrimary {
		if _, live := liveClusters[cid]; !live {
			delete(e.lastPrimary, cid)
		}
	}
	for cid := range e.lastFailoverTime {
		if _, live := liveClusters[cid]; !live {
			delete(e.lastFailoverTime, cid)
		}
	}
	for cid := range e.lastSplitBrain {
		if _, live := liveClusters[cid]; !live {
			delete(e.lastSplitBrain, cid)
		}
	}
	return removed
}

// Len reports how many instances the engine is currently tracking. Exposed so
// the pipeline's eviction can be asserted on without reaching into internals.
func (e *Engine) Len() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.instances)
}
