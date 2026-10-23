package advisor

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	subscriptionFactKind = "subscription"
	// subscriptionSilentFor is how long an apply worker may go without a
	// message from its publisher, which sends a keepalive at least every
	// wal_sender_timeout / 2 (30 s by default) when idle.
	subscriptionSilentFor = 5 * time.Minute
	// logicalSpillBytesPerSecond is the rate of decoding spilled to disk
	// from which a logical slot is reported.
	logicalSpillBytesPerSecond = 1 << 20
)

// instanceObjectRule is a rule whose findings are about one object of the
// instance, such as a subscription, rather than of one database.
type instanceObjectRule struct{ relationRule }

func (r instanceObjectRule) Scope() Scope { return ScopeInstance }

func subscriptionFinding(id string, sev Severity, f Fact, title, detail, remediation string, evidence map[string]any) Finding {
	return Finding{RuleID: id, Severity: sev, State: StateOpen, Scope: ScopeInstance, ObjectName: "subscription:" + f.Key,
		Title: title, Detail: detail, Remediation: remediation, Evidence: evidence}
}

// subscriptionWhere names a subscription with the database it lives in.
func subscriptionWhere(f Fact) string {
	if db := f.Labels["database"]; db != "" {
		return fmt.Sprintf("Subscription %s (database %s)", f.Key, db)
	}
	return "Subscription " + f.Key
}

func init() {
	Register(instanceObjectRule{relationRule{id: "subscription.disabled", severity: SeverityWarning, needs: []string{"logical_replication"}, eval: func(s *Snapshot) []Finding {
		const id = "subscription.disabled"
		var out []Finding
		for _, f := range currentFacts(s, subscriptionFactKind) {
			if f.ValueText != "disabled" {
				continue
			}
			name := pgx.Identifier{f.Key}.Sanitize()
			retains := "It has no slot (slot_name = NONE), so the publisher keeps nothing for it."
			if slot := f.Labels["slot_name"]; slot != "" {
				retains = fmt.Sprintf("Its slot %s on the publisher keeps every change it has not confirmed, and the WAL that holds them fills the publisher's disk until the subscription is enabled or the slot dropped.", slot)
			}
			out = append(out, subscriptionFinding(id, SeverityWarning, f,
				fmt.Sprintf("Subscription %s is disabled", f.Key),
				fmt.Sprintf("%s is disabled, so it applies no change from its publications (%s). %s", subscriptionWhere(f), f.Labels["publications"], retains),
				fmt.Sprintf("Find why it was disabled (with disable_on_error, the subscriber's log names the failed change), fix it, then ALTER SUBSCRIPTION %s ENABLE;. If it is no longer needed, DROP SUBSCRIPTION %s; also drops its slot on the publisher.", name, name),
				map[string]any{"subscription": f.Key, "slot_name": f.Labels["slot_name"]}))
		}
		return out
	}}})
	Register(instanceObjectRule{relationRule{id: "subscription.not_receiving", severity: SeverityWarning, needs: []string{"logical_replication"}, eval: func(s *Snapshot) []Finding {
		const id = "subscription.not_receiving"
		var out []Finding
		for _, f := range currentFacts(s, subscriptionFactKind) {
			if f.ValueText != "enabled" {
				continue
			}
			name := pgx.Identifier{f.Key}.Sanitize()
			if f.Labels["apply_worker"] != "true" {
				out = append(out, subscriptionFinding(id, SeverityWarning, f,
					fmt.Sprintf("Subscription %s has no apply worker running", f.Key),
					fmt.Sprintf("%s is enabled, but no apply worker runs for it, so no change is applied. A worker that fails, on a conflict or a lost connection to the publisher, is restarted after wal_retrieve_retry_interval, and fails again until the cause is fixed.", subscriptionWhere(f)),
					fmt.Sprintf("Read the subscriber's log for the apply worker's error. For a conflicting row, fix the row on the subscriber, or skip the transaction with ALTER SUBSCRIPTION %s SKIP (lsn = ...) once its changes are known to be unwanted.", name),
					map[string]any{"subscription": f.Key, "apply_errors": f.Labels["apply_errors"]}))
				continue
			}
			if s.Now.IsZero() {
				continue
			}
			receipt, err := time.Parse(time.RFC3339, f.Labels["last_msg_receipt_at"])
			if err != nil || s.Now.Sub(receipt) < subscriptionSilentFor {
				continue
			}
			silent := s.Now.Sub(receipt)
			out = append(out, subscriptionFinding(id, SeverityWarning, f,
				fmt.Sprintf("Subscription %s has heard nothing from its publisher for %s", f.Key, formatSilence(silent)),
				fmt.Sprintf("The apply worker of %s last received a message from the publisher at %s. The publisher sends a keepalive at least every half wal_sender_timeout when it has nothing else to send, so the connection is stuck or the publisher's walsender is not running.", subscriptionWhere(f), receipt.UTC().Format("2006-01-02 15:04 MST")),
				fmt.Sprintf("Check the publisher's walsender for slot %s (pg_stat_replication) and the network between the two. ALTER SUBSCRIPTION %s DISABLE; then ENABLE; restarts the worker.", f.Labels["slot_name"], name),
				map[string]any{"subscription": f.Key, "last_msg_receipt_at": f.Labels["last_msg_receipt_at"]}))
		}
		return out
	}}})
	Register(instanceObjectRule{relationRule{id: "subscription.errors", severity: SeverityWarning, needs: []string{"logical_replication"}, eval: func(s *Snapshot) []Finding {
		const id = "subscription.errors"
		var out []Finding
		for _, f := range currentFacts(s, subscriptionFactKind) {
			series := map[string]string{"subscription": f.Key}
			apply, _ := s.Metric("pg_subscription_apply_errors_total", series)
			sync, _ := s.Metric("pg_subscription_sync_errors_total", series)
			if apply <= 0 && sync <= 0 {
				continue
			}
			var what []string
			if apply > 0 {
				what = append(what, "applying changes")
			}
			if sync > 0 {
				what = append(what, "copying tables or sequences")
			}
			out = append(out, subscriptionFinding(id, SeverityWarning, f,
				fmt.Sprintf("Subscription %s is failing", f.Key),
				fmt.Sprintf("The workers of %s failed while %s since the previous scrape (%s apply and %s sync errors in all, since the statistics were last reset); each failure stops the worker until it is restarted.", subscriptionWhere(f), strings.Join(what, " and "), f.Labels["apply_errors"], f.Labels["sync_errors"]),
				"Read the subscriber's log for the error: most are a row that conflicts with one already on the subscriber, a missing table or column, or a permission the subscription owner lacks.",
				map[string]any{"subscription": f.Key, "apply_errors_per_second": apply, "sync_errors_per_second": sync}))
		}
		return out
	}}})
	Register(instanceObjectRule{relationRule{id: "logical_slot.spill_high", severity: SeverityInfo, needs: []string{"logical_replication"}, eval: func(s *Snapshot) []Finding {
		const id = "logical_slot.spill_high"
		var out []Finding
		spills := s.Metrics["pg_logical_slot_spill_bytes_total"]
		for _, labels := range slices.Sorted(maps.Keys(spills)) {
			slot, ok := strings.CutPrefix(labels, "slot_name=")
			if !ok || spills[labels] < logicalSpillBytesPerSecond {
				continue
			}
			out = append(out, Finding{RuleID: id, Severity: SeverityInfo, State: StateOpen, Scope: ScopeInstance, ObjectName: "slot:" + slot,
				Title:       fmt.Sprintf("Logical slot %s spills decoding to disk", slot),
				Detail:      fmt.Sprintf("Decoding for slot %s writes %s/s of transactions to disk, because they outgrow logical_decoding_work_mem before they commit. Spilled changes are written and read back, which slows replication and adds I/O on this server.", slot, formatSpill(spills[labels])),
				Remediation: "Raise logical_decoding_work_mem (64MB by default; ALTER SYSTEM SET logical_decoding_work_mem = '256MB'; SELECT pg_reload_conf();), or have the subscription stream large transactions (ALTER SUBSCRIPTION ... SET (streaming = parallel)).",
				Evidence:    map[string]any{"slot_name": slot, "spill_bytes_per_second": spills[labels]},
			})
		}
		return out
	}}})
}

func formatSilence(d time.Duration) string {
	if d < time.Hour {
		return plural(int(d.Minutes()), "minute")
	}
	return formatCatalogDuration(d)
}

func formatSpill(bytesPerSecond float64) string {
	return fmt.Sprintf("%.1f MiB", bytesPerSecond/(1<<20))
}
