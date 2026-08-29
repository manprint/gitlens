package alert

import "time"

func Builtin() []Rule {
	return []Rule{
		{ID: "agent_down", Tier: Tier0, Enabled: true, Severity: SeverityCritical, Scope: ScopeInstance, Metric: "up", Comparator: LT, Threshold: 1, For: 90 * time.Second, Summary: "No payload received from the agent"},
		{ID: "instance_unreachable", Tier: Tier0, Enabled: true, Severity: SeverityCritical, Scope: ScopeInstance, EventType: "instance_unreachable", Summary: "The agent is alive but cannot reach the instance"},
		{ID: "check_failing", Tier: Tier0, Enabled: true, Severity: SeverityWarning, Scope: ScopeInstance, Metric: "pglens_check_error_rate", Comparator: GT, Threshold: 0, For: 5 * time.Minute, Summary: "A check has been failing for over five minutes"},
		{ID: "no_primary_in_cluster", Tier: Tier0, Enabled: true, Severity: SeverityCritical, Scope: ScopeCluster, EventType: "no_primary_in_cluster", Summary: "No instance in the cluster reports the primary role"},
		{ID: "agent_buffer_full", Tier: Tier0, Enabled: true, Severity: SeverityWarning, Scope: ScopeInstance, Metric: "pglens_samples_dropped_rate", Comparator: GT, Threshold: 0, For: time.Minute, Summary: "The agent is dropping samples"},
		{ID: "clock_skew", Tier: Tier0, Enabled: true, Severity: SeverityWarning, Scope: ScopeInstance, Metric: "pglens_agent_clock_skew_seconds", Comparator: GT, Threshold: 30, For: time.Minute, Summary: "Agent clock skew above 30 seconds"},
		{ID: "cardinality_budget_exceeded", Tier: Tier0, Enabled: true, Severity: SeverityWarning, Scope: ScopeInstance, Metric: "pglens_cardinality_truncated_rate", Comparator: GT, Threshold: 0, For: 5 * time.Minute, Summary: "Series are being truncated by the cardinality budget"},
		{ID: "failover_detected", Tier: Tier0, Enabled: true, Severity: SeverityCritical, Scope: ScopeCluster, EventType: "failover_detected", Summary: "A failover was observed"},
		{ID: "split_brain_detected", Tier: Tier0, Enabled: true, Severity: SeverityCritical, Scope: ScopeCluster, EventType: "split_brain_detected", Summary: "More than one primary in the cluster"},
		{ID: "slot_inactive", Tier: Tier0, Enabled: true, Severity: SeverityWarning, Scope: ScopeInstance, EventType: "slot_inactive", Summary: "A replication slot has been inactive and is retaining WAL"},
	}
}
