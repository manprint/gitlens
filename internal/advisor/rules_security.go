package advisor

import (
	"strconv"
	"strings"
	"time"

	"github.com/manprint/pglens/internal/i18n"
)

// The monitoring_role check (internal/check/monitoring_role.go) reports one
// fact under this kind and key on every scrape. Its value lists what lets the
// role pglens connects as write, or is monitoringRoleReadOnly when nothing
// does.
const (
	monitoringRoleFactKind = "monitoring_role"
	monitoringRoleFactKey  = "self"
	monitoringRoleReadOnly = "read-only"
)

// monitoringRoleFresh is how old the monitoring_role fact may be. The check
// runs every ten minutes; a fact the agent stopped refreshing says nothing
// about the role any more.
const monitoringRoleFresh = time.Hour

// monitoringRoleFact returns the latest monitoring_role fact, or a degraded
// finding when there is none recent enough to judge the role by.
func monitoringRoleFact(s *Snapshot, id string, sev Severity) (Fact, *Finding) {
	fact, ok := s.Facts[monitoringRoleFactKind][monitoringRoleFactKey]
	reason := ""
	switch {
	case !ok:
		reason = "the monitoring_role check has not reported"
	case !s.Now.IsZero() && s.Now.Sub(fact.ObservedAt) > monitoringRoleFresh:
		reason = "the monitoring_role check has not reported for " + s.Now.Sub(fact.ObservedAt).Round(time.Minute).String()
	default:
		return fact, nil
	}
	return Fact{}, &Finding{RuleID: id, Severity: sev, State: StateDegraded, Scope: ScopeInstance,
		Title: "Monitoring role not observed", Detail: reason, DegradedReason: reason}
}

func init() {
	Register(configRule{id: "security.monitoring_role_can_write", severity: SeverityCritical, needs: []string{monitoringRoleFactKind}, eval: func(s *Snapshot) []Finding {
		const id = "security.monitoring_role_can_write"
		fact, degraded := monitoringRoleFact(s, id, SeverityCritical)
		if degraded != nil {
			return []Finding{*degraded}
		}
		if fact.ValueText == monitoringRoleReadOnly {
			return nil
		}
		role := fact.Labels["role"]
		return []Finding{configFinding(id, SeverityCritical, "", "", "",
			map[string]any{"role": role, "problems": strings.Split(fact.ValueText, "; ")}).
			say(id, i18n.Args{"role": role, "problems": fact.ValueText})}
	}})
	Register(configRule{id: "security.monitoring_role_read_only_off", severity: SeverityWarning, needs: []string{monitoringRoleFactKind}, eval: func(s *Snapshot) []Finding {
		const id = "security.monitoring_role_read_only_off"
		fact, degraded := monitoringRoleFact(s, id, SeverityWarning)
		if degraded != nil {
			return []Finding{*degraded}
		}
		value := fact.Labels["read_only_default"]
		if strings.EqualFold(value, "on") {
			return nil
		}
		role := fact.Labels["role"]
		return []Finding{configFinding(id, SeverityWarning, "", "", "",
			map[string]any{"role": role, "read_only_default": value}).
			say(id, i18n.Args{"value": strconv.Quote(value), "role": role})}
	}})
}
