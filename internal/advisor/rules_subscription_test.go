package advisor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func subscriptionFact(name, state string, observed time.Time, extra map[string]string) Fact {
	labels := map[string]string{
		"database": "app", "owner": "postgres", "slot_name": name, "publications": "pub",
		"apply_worker": "true", "sync_workers": "0", "last_msg_receipt_at": observed.UTC().Format(time.RFC3339),
		"apply_errors": "0", "sync_errors": "0", "conflicts": "",
	}
	for k, v := range extra {
		labels[k] = v
	}
	return Fact{Key: name, ValueText: state, Labels: labels, ObservedAt: observed}
}

func TestSubscriptionDisabledRule(t *testing.T) {
	now := time.Now()
	rule := ruleByID(t, "subscription.disabled")
	s := catalogSnapshot(now, subscriptionFactKind,
		subscriptionFact("orders", "disabled", now, map[string]string{"apply_worker": "false"}),
		subscriptionFact("Detached", "disabled", now, map[string]string{"apply_worker": "false", "slot_name": ""}),
		subscriptionFact("live", "enabled", now, nil),
	)
	assertRuleContract(t, rule, s)
	got := rule.Evaluate(s)
	require.Len(t, got, 2)
	require.Equal(t, ScopeInstance, rule.Scope())
	require.Equal(t, "subscription.disabled/subscription:Detached", got[0].ID())
	require.Contains(t, got[0].Detail, "It has no slot")
	require.Contains(t, got[0].Remediation, `ALTER SUBSCRIPTION "Detached" ENABLE;`)
	require.Contains(t, got[1].Detail, "Its slot orders on the publisher keeps every change")
}

func TestSubscriptionNotReceivingRule(t *testing.T) {
	now := time.Now()
	rule := ruleByID(t, "subscription.not_receiving")
	s := catalogSnapshot(now, subscriptionFactKind,
		subscriptionFact("crashed", "enabled", now, map[string]string{"apply_worker": "false", "last_msg_receipt_at": ""}),
		subscriptionFact("stuck", "enabled", now, map[string]string{"last_msg_receipt_at": now.Add(-12 * time.Minute).UTC().Format(time.RFC3339)}),
		subscriptionFact("fine", "enabled", now, map[string]string{"last_msg_receipt_at": now.Add(-40 * time.Second).UTC().Format(time.RFC3339)}),
		subscriptionFact("off", "disabled", now, map[string]string{"apply_worker": "false"}),
	)
	assertRuleContract(t, rule, s)
	got := rule.Evaluate(s)
	require.Len(t, got, 2)
	byObject := map[string]Finding{}
	for _, f := range got {
		byObject[f.ObjectName] = f
	}
	require.Equal(t, "Subscription crashed has no apply worker running", byObject["subscription:crashed"].Title)
	require.Equal(t, "Subscription stuck has heard nothing from its publisher for 12 minutes", byObject["subscription:stuck"].Title)
}

func TestSubscriptionErrorsRule(t *testing.T) {
	now := time.Now()
	rule := ruleByID(t, "subscription.errors")
	s := catalogSnapshot(now, subscriptionFactKind,
		subscriptionFact("failing", "enabled", now, map[string]string{"apply_errors": "7"}),
		subscriptionFact("quiet", "enabled", now, map[string]string{"apply_errors": "3"}),
	)
	s.Metrics = map[string]map[string]float64{
		"pg_subscription_apply_errors_total": {"subscription=failing": 0.01, "subscription=quiet": 0},
		"pg_subscription_sync_errors_total":  {"subscription=failing": 0, "subscription=quiet": 0},
	}
	assertRuleContract(t, rule, s)
	got := rule.Evaluate(s)
	require.Len(t, got, 1)
	require.Equal(t, "Subscription failing is failing", got[0].Title)
	require.Contains(t, got[0].Detail, "failed while applying changes since the previous scrape (7 apply")
}

func TestLogicalSlotSpillHighRule(t *testing.T) {
	rule := ruleByID(t, "logical_slot.spill_high")
	s := &Snapshot{Now: time.Now(), Metrics: map[string]map[string]float64{
		"pg_logical_slot_spill_bytes_total": {"slot_name=big": 3 << 20, "slot_name=small": 1024},
	}}
	assertRuleContract(t, rule, s)
	got := rule.Evaluate(s)
	require.Len(t, got, 1)
	require.Equal(t, "Logical slot big spills decoding to disk", got[0].Title)
	require.Contains(t, got[0].Detail, "3.0 MiB/s")
}
