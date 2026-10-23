package alert

import (
	"sort"
	"sync"
	"time"
)

func Compare(v float64, c Comparator, threshold float64) bool {
	switch c {
	case GT:
		return v > threshold
	case GE:
		return v >= threshold
	case LT:
		return v < threshold
	case LE:
		return v <= threshold
	case EQ:
		return v == threshold
	case NE:
		return v != threshold
	}
	return false
}

type Transition string

const (
	TransitionNone      Transition = "none"
	TransitionOpened    Transition = "opened"
	TransitionFired     Transition = "fired"
	TransitionResolved  Transition = "resolved"
	TransitionCancelled Transition = "cancelled"
)

type evalState struct {
	first  time.Time
	seen   time.Time // the pass that last sampled this subject
	firing bool
	alert  Alert
}

// Evaluator is safe for concurrent use. It used to rely on "only the engine
// ticker calls this", which is true of the production wiring and of nothing
// else: Engine.Tick is exported, the integration suites drive it directly,
// and a single concurrent call turns the unguarded map into an unrecoverable
// "concurrent map writes" crash of the server.
type Evaluator struct {
	mu     sync.Mutex
	states map[string]evalState
}

func NewEvaluator() *Evaluator { return &Evaluator{states: make(map[string]evalState)} }

// Restore seeds the in-memory evaluator from a persisted non-terminal alert.
// A newly elected engine must resume the existing episode instead of deriving
// a new StartedAt from its first post-failover sample. The restored episode
// counts as sampled now, so it gets a full grace period before it can be
// closed for want of a sample.
func (e *Evaluator) Restore(a Alert, now time.Time) {
	if a.State != StatePending && a.State != StateFiring {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.states[a.Key] = evalState{
		first:  a.StartedAt,
		seen:   now,
		firing: a.State == StateFiring,
		alert:  a,
	}
}

// Reset drops every episode the evaluator holds, so that the next pass starts
// from what Restore reloads.
func (e *Evaluator) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.states = make(map[string]evalState)
}

func (e *Evaluator) Step(r Rule, s Sample, now time.Time) (*Alert, Transition) {
	e.mu.Lock()
	defer e.mu.Unlock()
	k := Key(r, s)
	st, ok := e.states[k]
	trueCond := !s.Inapplicable && (r.EventType != "" || Compare(s.Value, r.Comparator, r.Threshold))
	if !trueCond {
		return e.endLocked(k, now)
	}
	if !ok {
		st = evalState{first: s.TS, seen: now}
		if st.first.IsZero() {
			st.first = now
		}
		st.alert = Alert{Key: k, RuleID: r.ID, Severity: r.Severity, State: StatePending, ClusterID: s.ClusterID, InstanceID: s.InstanceID, Datname: s.Datname, Labels: s.Labels, Value: s.Value, Summary: r.Summary, StartedAt: st.first, LastEvalAt: now}
		if r.For == 0 {
			st.firing = true
			st.alert.State = StateFiring
			e.states[k] = st
			return &st.alert, TransitionFired
		}
		e.states[k] = st
		return &st.alert, TransitionOpened
	}
	st.seen = now
	st.alert.LastEvalAt = now
	st.alert.Value = s.Value
	if !st.firing && now.Sub(st.first) >= r.For {
		st.firing = true
		st.alert.State = StateFiring
		e.states[k] = st
		return &st.alert, TransitionFired
	}
	e.states[k] = st
	return &st.alert, TransitionNone
}

// End closes the episode of key although no sample arrived for it. The caller
// has decided the episode is over: its subject left the lookback window and
// nothing confirmed it still holds. A firing episode resolves and is returned;
// a pending one is cancelled.
func (e *Evaluator) End(key string, now time.Time) (*Alert, Transition) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.endLocked(key, now)
}

// endLocked is End for a caller that already holds e.mu.
func (e *Evaluator) endLocked(key string, now time.Time) (*Alert, Transition) {
	st, ok := e.states[key]
	if !ok {
		return nil, TransitionNone
	}
	delete(e.states, key)
	if st.firing {
		a := st.alert
		a.State = StateResolved
		a.LastEvalAt = now
		a.ResolvedAt = &now
		return &a, TransitionResolved
	}
	return nil, TransitionCancelled
}

// Checkpoint returns a function that puts back the episode of key as it stands
// now. A transition that cannot be persisted is undone with it, so the next
// pass derives that transition again instead of taking it for recorded.
func (e *Evaluator) Checkpoint(key string) (undo func()) {
	e.mu.Lock()
	prev, had := e.states[key]
	e.mu.Unlock()
	return func() {
		e.mu.Lock()
		defer e.mu.Unlock()
		if had {
			e.states[key] = prev
		} else {
			delete(e.states, key)
		}
	}
}

// Unsampled returns the episodes of the rules that include selects whose
// subject was last sampled before cutoff. A subject sampled in the pass that
// runs at cutoff is not selected, so a cutoff equal to that pass's own time
// closes nothing it saw. The alerts are copies in key order: nothing is closed
// until End is called for a key.
func (e *Evaluator) Unsampled(include func(ruleID string) bool, cutoff time.Time) []Alert {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []Alert
	for _, st := range e.states {
		if include(st.alert.RuleID) && st.seen.Before(cutoff) {
			out = append(out, st.alert)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Forget drops evaluator state for pending episodes that were last sampled
// before the cutoff — conditions that went true, never held long enough to
// fire, and stopped being sampled (an instance that disappeared, a rule that
// was disabled), which would otherwise pin their key in memory forever. An
// episode that is still sampled is kept however long ago it started: its For
// timer must be able to complete, even when that takes longer than the cutoff.
//
// Firing episodes are never forgotten, however old they are. Dropping them
// was actively harmful: the state machine is the only record that an alert is
// currently firing, so a condition that stayed true for more than the cutoff
// had its state deleted and was then re-discovered as a brand-new episode —
// re-firing and re-notifying with a reset StartedAt every cutoff period, and
// never emitting the resolved transition when it finally cleared, leaving the
// stored alert firing forever.
func (e *Evaluator) Forget(before time.Time) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for k, s := range e.states {
		if s.firing {
			continue
		}
		if s.seen.Before(before) {
			delete(e.states, k)
			n++
		}
	}
	return n
}
