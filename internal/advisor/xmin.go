package advisor

import "fmt"

// xminHolderFact is the object_facts kind and key of the oldest xmin holder
// the activity check reports (see wire.factKinds).
const (
	xminHolderFactKind = "xmin_horizon"
	xminHolderFactKey  = "oldest"
)

// xminHolderEvidence returns the oldest xmin holder that the latest activity
// sample reported, as evidence: its kind, its identity and its ages. The fact
// is replaced by every sample, so one older than the recent window no longer
// describes the instance and is not named. It returns nil when no holder is
// known. The identity is a pid, a slot name, a standby's application_name or
// a prepared transaction's gid, never query text.
func xminHolderEvidence(s *Snapshot) map[string]any {
	fact, ok := s.Facts[xminHolderFactKind][xminHolderFactKey]
	if !ok || fact.Value == nil || s.Now.Sub(fact.ObservedAt) > recentWindow {
		return nil
	}
	kind, _ := fact.Value["holder_kind"].(string)
	if kind == "" {
		return nil
	}
	evidence := map[string]any{
		"holder_kind": kind,
		"identity":    fact.Value["identity"],
		"age_xids":    fact.Value["age_xids"],
	}
	if seconds, ok := fact.Value["age_seconds"]; ok {
		evidence["age_seconds"] = seconds
	}
	return evidence
}

// withHolderDetail appends the oldest xmin holder to a finding's detail, in
// words that say what the holder is.
func withHolderDetail(detail string, holder map[string]any) string {
	kinds := map[string]string{
		"session":  "session with pid",
		"slot":     "replication slot",
		"standby":  "standby with application_name",
		"prepared": "prepared transaction with gid",
	}
	kind, _ := holder["holder_kind"].(string)
	label := kinds[kind]
	if label == "" {
		label = kind
	}
	return fmt.Sprintf("%s; the oldest xmin holder is the %s %v, %v transaction IDs old", detail, label, holder["identity"], holder["age_xids"])
}

// withXminHolder names the oldest xmin holder in the findings of a rule that
// judges transaction age: the finding's evidence carries it under xmin_holder
// and its detail says what it is. A rule with no holder known is unchanged.
func withXminHolder(r queryAdvisorRule) queryAdvisorRule {
	eval := r.eval
	r.eval = func(s *Snapshot) []Finding {
		findings := eval(s)
		holder := xminHolderEvidence(s)
		if holder == nil {
			return findings
		}
		for i := range findings {
			findings[i].Detail = withHolderDetail(findings[i].Detail, holder)
			if findings[i].Evidence == nil {
				findings[i].Evidence = map[string]any{}
			}
			findings[i].Evidence["xmin_holder"] = holder
		}
		return findings
	}
	return r
}
