package advisor

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/manprint/pglens/internal/i18n"
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

// subscriptionArgs are the message arguments naming a subscription, with the
// database it lives in when the fact has one.
func subscriptionArgs(f Fact) i18n.Args {
	db := f.Labels["database"]
	return i18n.Args{"subscription": f.Key, "has_db": db != "", "database": db, "ident": pgx.Identifier{f.Key}.Sanitize()}
}

func subscriptionFinding(id, key string, f Fact, args i18n.Args, evidence map[string]any) Finding {
	return Finding{RuleID: id, Severity: SeverityWarning, State: StateOpen, Scope: ScopeInstance, ObjectName: "subscription:" + f.Key,
		Evidence: evidence}.say(key, args)
}

func init() {
	Register(instanceObjectRule{relationRule{id: "subscription.disabled", severity: SeverityWarning, needs: []string{"logical_replication"}, eval: func(s *Snapshot) []Finding {
		const id = "subscription.disabled"
		var out []Finding
		for _, f := range currentFacts(s, subscriptionFactKind) {
			if f.ValueText != "disabled" {
				continue
			}
			slot := f.Labels["slot_name"]
			args := subscriptionArgs(f)
			args["publications"] = f.Labels["publications"]
			args["has_slot"] = slot != ""
			args["slot_name"] = slot
			out = append(out, subscriptionFinding(id, id, f, args,
				map[string]any{"subscription": f.Key, "slot_name": slot}))
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
			if f.Labels["apply_worker"] != "true" {
				out = append(out, subscriptionFinding(id, id, f, subscriptionArgs(f),
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
			args := subscriptionArgs(f)
			args["receipt"] = receipt.UTC().Format("2006-01-02 15:04 MST")
			args["slot_name"] = f.Labels["slot_name"]
			args["silence_long"] = silent >= time.Hour
			args["silence_minutes"] = int(silent.Minutes())
			args["silence"] = ""
			if silent >= time.Hour {
				args["silence"] = formatCatalogDuration(silent)
			}
			out = append(out, subscriptionFinding(id, id+".silent", f, args,
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
			args := subscriptionArgs(f)
			args["has_apply"] = apply > 0
			args["has_sync"] = sync > 0
			args["apply_errors"] = f.Labels["apply_errors"]
			args["sync_errors"] = f.Labels["sync_errors"]
			out = append(out, subscriptionFinding(id, id, f, args,
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
				Evidence: map[string]any{"slot_name": slot, "spill_bytes_per_second": spills[labels]},
			}.say(id, i18n.Args{"slot": slot, "spill": formatSpill(spills[labels])}))
		}
		return out
	}}})
}

func formatSpill(bytesPerSecond float64) string {
	return fmt.Sprintf("%.1f MiB", bytesPerSecond/(1<<20))
}
