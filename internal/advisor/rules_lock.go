package advisor

import (
	"fmt"
	"slices"
	"time"
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
		return []Finding{{RuleID: id, Severity: SeverityWarning, State: StateOpen, Scope: ScopeInstance,
			Title: fmt.Sprintf("Session %d has waited %s for a lock", waiter.PID, formatLockWait(wait)),
			Detail: fmt.Sprintf("Session %d has waited %s. Its blocking chain reaches session %d, and the sessions queued behind it wait too, so a wait this long holds up every session in the tree. The lock sample reports waits of %s or more.",
				waiter.PID, formatLockWait(wait), root, formatLockWait(lockLongWait)),
			Remediation: fmt.Sprintf("Open the Locks page and look at session %d: its state and transaction age show whether it is idle in transaction or still working. If its transaction can end, ask its owner to end it. If the work can be lost, cancel or terminate it there, and the waiting session proceeds.", root),
			Evidence:    map[string]any{"waiting_pid": waiter.PID, "root_blocker_pid": root, "wait_seconds": waiter.WaitDuration},
		}}
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

// formatLockWait renders a wait in whole seconds, or minutes and seconds.
func formatLockWait(d time.Duration) string {
	whole := int(d.Seconds())
	if whole < 60 {
		return plural(whole, "second")
	}
	return plural(whole/60, "minute") + " " + plural(whole%60, "second")
}
