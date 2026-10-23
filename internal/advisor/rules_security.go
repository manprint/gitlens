package advisor

import (
	"fmt"
	"strings"
	"time"
)

// The monitoring_role check (internal/check/monitoring_role.go) reports one
// fact under this kind and key on every scrape. Its value lists what lets the
// role pglens connects as write, "" when nothing does.
const (
	monitoringRoleFactKind = "monitoring_role"
	monitoringRoleFactKey  = "self"
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
		if fact.ValueText == "" {
			return nil
		}
		role := fact.Labels["role"]
		return []Finding{configFinding(id, SeverityCritical,
			"The monitoring role could write to the database",
			fmt.Sprintf("pglens connects as %s, which %s. pglens only reads, but anyone holding its credentials could change data.", role, fact.ValueText),
			"Run deploy/sql/monitoring_user.sql again as an administrator: it refuses the role and lists the statement that removes each problem.",
			map[string]any{"role": role, "problems": strings.Split(fact.ValueText, "; ")})}
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
		return []Finding{configFinding(id, SeverityWarning,
			"The monitoring role does not default to read-only transactions",
			fmt.Sprintf("default_transaction_read_only is %q for %s in this database. pglens sets it in every session it opens; the role default is the second safeguard.", value, role),
			"Run deploy/sql/monitoring_user.sql again: it sets the role default and removes database-specific overrides.",
			map[string]any{"role": role, "read_only_default": value})}
	}})
}
