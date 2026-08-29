package alert

import "time"

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
	firing bool
	alert  Alert
}
type Evaluator struct{ states map[string]evalState }

func NewEvaluator() *Evaluator { return &Evaluator{states: make(map[string]evalState)} }

// Restore seeds the in-memory evaluator from a persisted non-terminal alert.
// A newly elected engine must resume the existing episode instead of deriving
// a new StartedAt from its first post-failover sample.
func (e *Evaluator) Restore(a Alert) {
	if a.State != StatePending && a.State != StateFiring {
		return
	}
	e.states[a.Key] = evalState{
		first:  a.StartedAt,
		firing: a.State == StateFiring,
		alert:  a,
	}
}

func (e *Evaluator) Step(r Rule, s Sample, now time.Time) (*Alert, Transition) {
	k := Key(r, s)
	st, ok := e.states[k]
	trueCond := r.EventType != "" || Compare(s.Value, r.Comparator, r.Threshold)
	if !trueCond {
		if !ok {
			return nil, TransitionNone
		}
		delete(e.states, k)
		if st.firing {
			a := st.alert
			a.State = StateResolved
			a.LastEvalAt = now
			a.ResolvedAt = &now
			return &a, TransitionResolved
		}
		return nil, TransitionCancelled
	}
	if !ok {
		st = evalState{first: s.TS}
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
func (e *Evaluator) Forget(before time.Time) int {
	n := 0
	for k, s := range e.states {
		if s.first.Before(before) {
			delete(e.states, k)
			n++
		}
	}
	return n
}
