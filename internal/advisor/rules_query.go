package advisor

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/manprint/pglens/internal/i18n"
	"github.com/manprint/pglens/internal/pgtype"
)

const (
	// slowMeanMS flags statements that are materially slow in the bounded window.
	slowMeanMS = 500
	// totalShare identifies one statement consuming a disproportionate budget.
	totalShare = 0.30
	// regressionFactor requires a threefold increase over the seven-day median.
	regressionFactor = 3
	// defaultWorkMemBytes is PostgreSQL's default work_mem, used when the
	// instance's own value is unknown.
	defaultWorkMemBytes = 4 * 1024 * 1024
	// cacheMissShare is the shared-buffer read ratio considered high.
	cacheMissShare = 0.30
	// longTxnSeconds is the transaction age requiring operator attention.
	longTxnSeconds = 900
	// idleTxnSeconds is the idle-in-transaction age requiring operator attention.
	idleTxnSeconds = 300
	// preparedSeconds is the age at which a prepared transaction can block vacuum.
	preparedSeconds = 3600
	// rollbackShare is the rollback ratio considered unusually high.
	rollbackShare = 0.10
	// wraparoundAge is the xid age at which wraparound risk is critical.
	wraparoundAge = 1_000_000_000
)

type queryAdvisorRule struct {
	id       string
	severity Severity
	needs    []string
	eval     func(*Snapshot) []Finding
}

func (r queryAdvisorRule) ID() string                     { return r.id }
func (r queryAdvisorRule) Severity() Severity             { return r.severity }
func (r queryAdvisorRule) Scope() Scope                   { return ScopeInstance }
func (r queryAdvisorRule) Needs() []string                { return append([]string(nil), r.needs...) }
func (r queryAdvisorRule) MinTier() pgtype.PermTier       { return pgtype.TierReadOnly }
func (r queryAdvisorRule) Evaluate(s *Snapshot) []Finding { return r.eval(s) }

// queryFinding is a finding about one statement, its message the rule's own.
func queryFinding(r string, sev Severity, q StatementStat, args i18n.Args, evidence map[string]any) Finding {
	// A string: a JSON number past 2^53 reaches the browser rounded and names
	// another statement.
	if _, ok := evidence["queryid"]; ok {
		evidence["queryid"] = strconv.FormatInt(q.QueryID, 10)
	}
	return Finding{RuleID: r, Severity: sev, State: StateOpen, Scope: ScopeInstance, Datname: q.Datname, ObjectName: fmt.Sprintf("queryid:%d", q.QueryID), Evidence: evidence}.say(r, args)
}

func metricValue(s *Snapshot, name string) (float64, bool) { return s.Metric(name, nil) }

// tempFiles is the number of temporary files the instance has written, summed
// over its databases from the latest sample of each. Temporary bytes alone
// cannot tell many small spills from a few large ones, and the file count can.
func tempFiles(s *Snapshot) (float64, bool) {
	values, ok := s.Metrics["pg_temp_files_total"]
	if !ok {
		return 0, false
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum, true
}

// sortedLabelSets lists the canonical label sets a metric has, in order.
func sortedLabelSets(s *Snapshot, name string) []string {
	out := make([]string, 0, len(s.Metrics[name]))
	for labels := range s.Metrics[name] {
		out = append(out, labels)
	}
	sort.Strings(out)
	return out
}

// canonicalLabel reads one label back out of a pgtype.CanonicalLabels key.
func canonicalLabel(canonical, key string) string {
	for _, pair := range strings.Split(canonical, "\x1f") {
		if k, v, ok := strings.Cut(pair, "="); ok && pgtype.UnescapeLabel(k) == key {
			return pgtype.UnescapeLabel(v)
		}
	}
	return ""
}

func metricRule(id string, sev Severity, metric string, threshold float64) queryAdvisorRule {
	return queryAdvisorRule{id: id, severity: sev, needs: []string{metric}, eval: func(s *Snapshot) []Finding {
		v, ok := metricValue(s, metric)
		if !ok || v <= threshold {
			return nil
		}
		return []Finding{Finding{RuleID: id, Severity: sev, State: StateOpen, Scope: ScopeInstance,
			Evidence: map[string]any{"metric": metric, "observed": v, "threshold": threshold},
		}.say(id, i18n.Args{"metric": metric, "observed": i18n.Fixed{V: v, Digits: 2}, "threshold": i18n.Fixed{V: threshold, Digits: 2}})}
	}}
}

func init() {
	rules := []queryAdvisorRule{
		{id: "query.slow_mean", severity: SeverityWarning, needs: []string{"Statements"}, eval: func(s *Snapshot) []Finding {
			var out []Finding
			for _, q := range s.Statements {
				if q.Calls >= 10 && q.MeanExecTimeMs > slowMeanMS {
					out = append(out, queryFinding("query.slow_mean", SeverityWarning, q,
						i18n.Args{"mean_ms": i18n.Fixed{V: q.MeanExecTimeMs, Digits: 1}, "calls": fmt.Sprintf("%.0f", q.Calls), "threshold_ms": slowMeanMS},
						map[string]any{"queryid": q.QueryID, "observed_ms": q.MeanExecTimeMs, "threshold_ms": slowMeanMS}))
				}
			}
			return out
		}},
		{id: "query.total_time_share", severity: SeverityWarning, needs: []string{"Statements"}, eval: func(s *Snapshot) []Finding {
			total := 0.0
			for _, q := range s.Statements {
				total += q.TotalExecTimeMs
			}
			var out []Finding
			for _, q := range s.Statements {
				share := 0.0
				if total > 0 {
					share = q.TotalExecTimeMs / total
				}
				if share > totalShare {
					out = append(out, queryFinding("query.total_time_share", SeverityWarning, q,
						i18n.Args{"share_pct": i18n.Fixed{V: share * 100, Digits: 1}, "calls": fmt.Sprintf("%.0f", q.Calls), "mean_ms": i18n.Fixed{V: q.MeanExecTimeMs, Digits: 1}},
						map[string]any{"queryid": q.QueryID, "share": share, "threshold": totalShare}))
				}
			}
			return out
		}},
		{id: "query.regression", severity: SeverityCritical, needs: []string{"Statements", "Baseline"}, eval: func(s *Snapshot) []Finding {
			if s.Baseline == nil {
				return []Finding{Finding{RuleID: "query.regression", Severity: SeverityCritical, State: StateDegraded, Scope: ScopeInstance,
					DegradedReason: "insufficient query history"}.say("query.regression.unavailable", nil)}
			}
			var out []Finding
			for _, q := range s.Statements {
				base := s.Baseline.MeanExecTimeByQueryID[q.QueryID]
				if base > 0 && q.MeanExecTimeMs >= regressionFactor*base && q.MeanExecTimeMs >= 100 {
					out = append(out, queryFinding("query.regression", SeverityCritical, q,
						i18n.Args{"mean_ms": i18n.Fixed{V: q.MeanExecTimeMs, Digits: 1}, "ratio": i18n.Fixed{V: q.MeanExecTimeMs / base, Digits: 1}, "base_ms": i18n.Fixed{V: base, Digits: 1}},
						map[string]any{"queryid": q.QueryID, "baseline_ms": base, "observed_ms": q.MeanExecTimeMs}))
				}
			}
			return out
		}},
		{id: "query.temp_bytes_high", severity: SeverityWarning, needs: []string{"Statements"}, eval: func(s *Snapshot) []Finding {
			// work_mem is a setting, not a metric: it used to be looked up
			// among the metrics, never found, and every statement that
			// spilled a single byte was reported.
			workMem, ok := settingFloat(s, "work_mem_bytes")
			if !ok || workMem <= 0 {
				workMem = defaultWorkMemBytes
			}
			files, hasFiles := tempFiles(s)
			var out []Finding
			for _, q := range s.Statements {
				if q.Calls <= 0 {
					continue
				}
				avg := q.TempBytes / q.Calls
				if avg > workMem {
					evidence := map[string]any{"queryid": q.QueryID, "bytes_per_call": avg, "work_mem_bytes": workMem}
					if hasFiles {
						evidence["pg_temp_files_total"] = files
					}
					out = append(out, queryFinding("query.temp_bytes_high", SeverityWarning, q,
						i18n.Args{"avg_bytes": fmt.Sprintf("%.0f", avg), "work_mem_bytes": fmt.Sprintf("%.0f", workMem)}, evidence))
				}
			}
			return out
		}},
		{id: "query.cache_miss_high", severity: SeverityInfo, needs: []string{"Statements"}, eval: func(s *Snapshot) []Finding {
			var out []Finding
			for _, q := range s.Statements {
				total := q.SharedBlksRead + q.SharedBlksHit
				miss := 0.0
				if total > 0 {
					miss = q.SharedBlksRead / total
				}
				if q.Calls > 1000 && miss > cacheMissShare {
					out = append(out, queryFinding("query.cache_miss_high", SeverityInfo, q,
						i18n.Args{"read_pct": i18n.Fixed{V: miss * 100, Digits: 1}},
						map[string]any{"queryid": q.QueryID, "read_ratio": miss, "threshold": cacheMissShare}))
				}
			}
			return out
		}},
		withXminHolder(metricRule("txn.long_running", SeverityWarning, "pg_max_xact_age_seconds", longTxnSeconds)),
		metricRule("txn.idle_in_transaction", SeverityWarning, "pg_max_idle_in_transaction_seconds", idleTxnSeconds),
		metricRule("txn.prepared_orphan", SeverityCritical, "pg_oldest_prepared_xact_seconds", preparedSeconds),
		{id: "txn.high_rollback_ratio", severity: SeverityInfo, needs: []string{"pg_xact_rollback_ratio"}, eval: func(s *Snapshot) []Finding {
			// database_stats emits the ratio per database. The rule read the
			// unlabelled series, which does not exist, so it never fired.
			var out []Finding
			for _, labels := range sortedLabelSets(s, "pg_xact_rollback_ratio") {
				v := s.Metrics["pg_xact_rollback_ratio"][labels]
				if v <= rollbackShare {
					continue
				}
				db := canonicalLabel(labels, "database")
				out = append(out, Finding{RuleID: "txn.high_rollback_ratio", Severity: SeverityInfo, State: StateOpen, Scope: ScopeDatabase, Datname: db,
					Evidence: map[string]any{"metric": "pg_xact_rollback_ratio", "database": db, "observed": v, "threshold": rollbackShare},
				}.say("txn.high_rollback_ratio", i18n.Args{"rollback_pct": i18n.Fixed{V: v * 100, Digits: 1}, "database_quoted": strconv.Quote(db), "threshold_pct": fmt.Sprintf("%.0f", rollbackShare*100)}))
			}
			return out
		}},
		metricRule("txn.wraparound_risk", SeverityCritical, "pg_max_datfrozenxid_age", wraparoundAge),
	}
	for _, r := range rules {
		Register(r)
	}
}

func sortedRuleIDs(rules []Rule) []string {
	out := make([]string, 0, len(rules))
	for _, r := range rules {
		out = append(out, r.ID())
	}
	sort.Strings(out)
	return out
}
