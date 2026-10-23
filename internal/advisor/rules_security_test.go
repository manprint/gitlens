package advisor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func monitoringRoleSnapshot(now time.Time, problems, readOnly string, observed time.Time) *Snapshot {
	s := &Snapshot{Now: now, Settings: map[string]string{}, Metrics: map[string]map[string]float64{}, Facts: map[string]map[string]Fact{}}
	s.Facts[monitoringRoleFactKind] = map[string]Fact{monitoringRoleFactKey: {
		Key: monitoringRoleFactKey, ValueText: problems, ObservedAt: observed,
		Labels: map[string]string{"role": "pglens", "read_only_default": readOnly},
	}}
	return s
}

func TestSecurity_RulesSatisfyContract(t *testing.T) {
	now := time.Now()
	for _, s := range []*Snapshot{
		monitoringRoleSnapshot(now, monitoringRoleReadOnly, "on", now),
		monitoringRoleSnapshot(now, "has SUPERUSER", "off", now),
		monitoringRoleSnapshot(now, monitoringRoleReadOnly, "on", now.Add(-2*time.Hour)),
	} {
		assertRuleContract(t, ruleByID(t, "security.monitoring_role_can_write"), s)
		assertRuleContract(t, ruleByID(t, "security.monitoring_role_read_only_off"), s)
	}
}

func TestSecurity_MonitoringRoleCanWrite(t *testing.T) {
	now := time.Now()
	rule := ruleByID(t, "security.monitoring_role_can_write")
	require.Empty(t, rule.Evaluate(monitoringRoleSnapshot(now, monitoringRoleReadOnly, "on", now)))

	got := rule.Evaluate(monitoringRoleSnapshot(now, "is a member of pg_write_all_data; owns 2 object(s)", "on", now))
	require.Len(t, got, 1)
	require.Equal(t, StateOpen, got[0].State)
	require.Equal(t, SeverityCritical, got[0].Severity)
	require.Contains(t, got[0].Detail, "pglens, which is a member of pg_write_all_data; owns 2 object(s)")
	require.Equal(t, []string{"is a member of pg_write_all_data", "owns 2 object(s)"}, got[0].Evidence["problems"])
}

func TestSecurity_MonitoringRoleReadOnlyOff(t *testing.T) {
	now := time.Now()
	rule := ruleByID(t, "security.monitoring_role_read_only_off")
	require.Empty(t, rule.Evaluate(monitoringRoleSnapshot(now, monitoringRoleReadOnly, "on", now)))
	for _, value := range []string{"off", ""} {
		got := rule.Evaluate(monitoringRoleSnapshot(now, monitoringRoleReadOnly, value, now))
		require.Len(t, got, 1, value)
		require.Equal(t, StateOpen, got[0].State)
		require.Equal(t, SeverityWarning, got[0].Severity)
	}
}

// A role that was never observed, or not for an hour, is not reported as
// safe: the finding degrades instead.
func TestSecurity_MonitoringRoleUnobservedDegrades(t *testing.T) {
	now := time.Now()
	stale := monitoringRoleSnapshot(now, monitoringRoleReadOnly, "on", now.Add(-2*time.Hour))
	missing := &Snapshot{Now: now, Facts: map[string]map[string]Fact{}}
	for _, id := range []string{"security.monitoring_role_can_write", "security.monitoring_role_read_only_off"} {
		rule := ruleByID(t, id)
		got := rule.Evaluate(stale)
		require.Len(t, got, 1, id)
		require.Equal(t, StateDegraded, got[0].State, id)
		require.Contains(t, got[0].DegradedReason, "has not reported for 2h0m0s")

		findings, _ := (&Engine{}).evaluateRule(rule, missing)
		require.Len(t, findings, 1, id)
		require.Equal(t, StateDegraded, findings[0].State, id)
	}
}
