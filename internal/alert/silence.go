package alert

import (
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
)

type Matcher struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	IsRegex bool   `json:"is_regex,omitempty"`
	Negate  bool   `json:"negate,omitempty"`
}
type Silence struct {
	ID       uuid.UUID
	Matchers []Matcher
	Reason   string
	StartsAt time.Time
	EndsAt   time.Time
}

func (s Silence) Validate() error {
	if len(s.Matchers) == 0 {
		return fmt.Errorf("silence has no matchers")
	}
	if s.EndsAt.Before(s.StartsAt) {
		return fmt.Errorf("silence ends before it starts")
	}
	for _, m := range s.Matchers {
		if m.Name == "" {
			return fmt.Errorf("matcher name is empty")
		}
		if m.IsRegex {
			if _, e := regexp.Compile("^(?:" + m.Value + ")$"); e != nil {
				return fmt.Errorf("invalid matcher regex: %w", e)
			}
		}
	}
	return nil
}
func (s Silence) Active(now time.Time) bool { return !now.Before(s.StartsAt) && now.Before(s.EndsAt) }
func (s Silence) Matches(a Alert) bool {
	if len(s.Matchers) == 0 {
		return false
	}
	for _, m := range s.Matchers {
		v := reserved(m.Name, a)
		if v == "" {
			v = a.Labels[m.Name]
		}
		ok := v == m.Value
		if m.IsRegex {
			re, e := regexp.Compile("^(?:" + m.Value + ")$")
			ok = e == nil && re.MatchString(v)
		}
		if m.Negate {
			ok = !ok
		}
		if !ok {
			return false
		}
	}
	return true
}
func reserved(k string, a Alert) string {
	switch k {
	case "rule_id":
		return a.RuleID
	case "severity":
		return string(a.Severity)
	case "cluster_id":
		if a.ClusterID != nil {
			return fmt.Sprint(*a.ClusterID)
		}
	case "instance_id":
		if a.InstanceID != nil {
			return a.InstanceID.String()
		}
	case "datname":
		return a.Datname
	}
	return ""
}
func FirstMatch(ss []Silence, a Alert, now time.Time) *Silence {
	for i := range ss {
		if ss[i].Active(now) && ss[i].Matches(a) {
			return &ss[i]
		}
	}
	return nil
}
