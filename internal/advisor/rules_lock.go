package advisor

import (
	"slices"
	"time"

	"github.com/manprint/pglens/internal/i18n"
)

// lockLongWait is the wait from which a blocked session is reported. A
// shorter wait is ordinary contention that clears by itself.
const lockLongWait = 300 * time.Second

func init() {
	Register(instanceObjectRule{relationRule{id: "lock.long_wait", severity: SeverityWarning, needs: []string{"LockTree"}, eval: func(s *Snapshot) []Finding {
		const id = "lock.long_wait"
		sample := s.LockTree
		if sample == nil || sample.SampledAt.IsZero() {
			return nil
		}
		if !s.Now.IsZero() && s.Now.Sub(sample.SampledAt) > lockSampleFresh {
			return nil
		}
		// The longest wait in the sample is the one reported. Ties go to the
		// lower PID, so the finding does not change between two equal waits.
		var waiter *LockTreeNode
		for i := range sample.Nodes {
			n := &sample.Nodes[i]
			if len(n.BlockedBy) == 0 || n.WaitDuration < lockLongWait.Seconds() {
				continue
			}
			if waiter == nil || n.WaitDuration > waiter.WaitDuration || (n.WaitDuration == waiter.WaitDuration && n.PID < waiter.PID) {
				waiter = n
			}
		}
		if waiter == nil {
			return nil
		}
		root := lockRootBlocker(sample, waiter.PID)
		wait := time.Duration(waiter.WaitDuration * float64(time.Second))
		waitMinutes, waitSeconds := lockWaitParts(wait)
		minMinutes, minSeconds := lockWaitParts(lockLongWait)
		return []Finding{Finding{RuleID: id, Severity: SeverityWarning, State: StateOpen, Scope: ScopeInstance,
			Evidence: map[string]any{"waiting_pid": waiter.PID, "root_blocker_pid": root, "wait_seconds": waiter.WaitDuration},
		}.say("lock.long_wait", i18n.Args{"waiting_pid": waiter.PID, "root_pid": root,
			"wait_minutes": waitMinutes, "wait_seconds": waitSeconds, "min_minutes": minMinutes, "min_seconds": minSeconds})}
	}}})
}

// lockRootBlocker follows the blockers of a waiting session breadth first, the
// way the blocking tree places it, and returns the nearest session that is not
// itself blocked. A blocker outside the sample ends the walk: it holds the lock
// and the sample says nothing about what it waits for. When every session on
// the way is blocked, the waiter is in a cycle with no root; its lowest blocker
// is returned.
func lockRootBlocker(sample *LockTreeSample, waiter int32) int32 {
	byPID := make(map[int32]LockTreeNode, len(sample.Nodes))
	for _, n := range sample.Nodes {
		byPID[n.PID] = n
	}
	seen := map[int32]bool{waiter: true}
	queue := []int32{waiter}
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		node, inSample := byPID[pid]
		if pid != waiter && (!inSample || len(node.BlockedBy) == 0) {
			return pid
		}
		for _, blocker := range sortedPIDs(node.BlockedBy) {
			if !seen[blocker] {
				seen[blocker] = true
				queue = append(queue, blocker)
			}
		}
	}
	if blockers := byPID[waiter].BlockedBy; len(blockers) > 0 {
		return slices.Min(blockers)
	}
	return waiter
}

func sortedPIDs(pids []int32) []int32 {
	out := slices.Clone(pids)
	slices.Sort(out)
	return out
}

// lockWaitParts splits a wait into the whole minutes and seconds the lock
// messages name; the minutes are zero under a minute.
func lockWaitParts(d time.Duration) (minutes, seconds int) {
	whole := int(d.Seconds())
	return whole / 60, whole % 60
}
