package advisor

import (
	"fmt"
	"sort"

	"github.com/manprint/pglens/internal/pgtype"
)

const (
	// slowMeanMS flags statements that are materially slow in the bounded window.
	slowMeanMS = 500
	// totalShare identifies one statement consuming a disproportionate budget.
	totalShare = 0.30
	// regressionFactor requires a threefold increase over the seven-day median.
	regressionFactor = 3
	// tempBytesPerCall is the minimum temporary-work threshold in bytes per call.
	tempBytesPerCall = 1
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

func queryFinding(r string, sev Severity, q StatementStat, title, detail, remediation string, evidence map[string]any) Finding {
	return Finding{RuleID: r, Severity: sev, State: StateOpen, Scope: ScopeInstance, ObjectName: fmt.Sprintf("queryid:%d", q.QueryID), Title: title, Detail: detail, Remediation: remediation, Evidence: evidence}
}

func metricValue(s *Snapshot, name string) (float64, bool) { return s.Metric(name, nil) }

func metricRule(id string, sev Severity, metric string, threshold float64, title, remediation string) queryAdvisorRule {
	return queryAdvisorRule{id: id, severity: sev, needs: []string{metric}, eval: func(s *Snapshot) []Finding {
		v, ok := metricValue(s, metric)
		if !ok || v <= threshold {
			return nil
		}
		return []Finding{{RuleID: id, Severity: sev, State: StateOpen, Scope: ScopeInstance, Title: title, Detail: fmt.Sprintf("%s is %.2f, above %.2f", metric, v, threshold), Remediation: remediation, Evidence: map[string]any{"metric": metric, "observed": v, "threshold": threshold}}}
	}}
}

func init() {
	rules := []queryAdvisorRule{
		{id: "query.slow_mean", severity: SeverityWarning, needs: []string{"Statements"}, eval: func(s *Snapshot) []Finding {
			var out []Finding
			for _, q := range s.Statements {
				if q.Calls >= 10 && q.MeanExecTimeMs > slowMeanMS {
					out = append(out, queryFinding("query.slow_mean", SeverityWarning, q, "Slow query", fmt.Sprintf("mean execution time is %.1f ms", q.MeanExecTimeMs), "Tune or index the query", map[string]any{"queryid": q.QueryID, "observed_ms": q.MeanExecTimeMs, "threshold_ms": slowMeanMS}))
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
					out = append(out, queryFinding("query.total_time_share", SeverityWarning, q, "Dominant query", fmt.Sprintf("query accounts for %.1f%% of execution time", share*100), "Optimize the dominant query", map[string]any{"queryid": q.QueryID, "share": share, "threshold": totalShare}))
				}
			}
			return out
		}},
		{id: "query.regression", severity: SeverityCritical, needs: []string{"Statements", "Baseline"}, eval: func(s *Snapshot) []Finding {
			if s.Baseline == nil {
				return []Finding{{RuleID: "query.regression", Severity: SeverityCritical, State: StateDegraded, Scope: ScopeInstance, Title: "Query regression unavailable", Detail: "seven-day query baseline is unavailable", DegradedReason: "insufficient query history"}}
			}
			var out []Finding
			for _, q := range s.Statements {
				base := s.Baseline.MeanExecTimeByQueryID[q.QueryID]
				if base > 0 && q.MeanExecTimeMs >= regressionFactor*base && q.MeanExecTimeMs >= 100 {
					out = append(out, queryFinding("query.regression", SeverityCritical, q, "Query regression", fmt.Sprintf("mean time %.1f ms is %.1fx baseline", q.MeanExecTimeMs, q.MeanExecTimeMs/base), "Investigate the query plan regression", map[string]any{"queryid": q.QueryID, "baseline_ms": base, "observed_ms": q.MeanExecTimeMs}))
				}
			}
			return out
		}},
		{id: "query.temp_bytes_high", severity: SeverityWarning, needs: []string{"Statements", "work_mem"}, eval: func(s *Snapshot) []Finding {
			workMem, ok := metricValue(s, "work_mem_bytes")
			if !ok {
				workMem = 0
			}
			var out []Finding
			for _, q := range s.Statements {
				avg := q.TempBytes / q.Calls
				if q.Calls > 0 && avg > tempBytesPerCall && (workMem == 0 || avg > workMem) {
					out = append(out, queryFinding("query.temp_bytes_high", SeverityWarning, q, "High temporary write volume", fmt.Sprintf("average temporary bytes per call is %.0f", avg), fmt.Sprintf("Review the query and current work_mem (%.0f bytes)", workMem), map[string]any{"queryid": q.QueryID, "bytes_per_call": avg, "work_mem_bytes": workMem}))
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
					out = append(out, queryFinding("query.cache_miss_high", SeverityInfo, q, "High cache miss ratio", fmt.Sprintf("shared block read ratio is %.1f%%", miss*100), "Review working-set size and indexing", map[string]any{"queryid": q.QueryID, "read_ratio": miss, "threshold": cacheMissShare}))
				}
			}
			return out
		}},
		metricRule("txn.long_running", SeverityWarning, "pg_max_xact_age_seconds", longTxnSeconds, "Long-running transaction", "Commit, roll back, or investigate the transaction"),
		metricRule("txn.idle_in_transaction", SeverityWarning, "pg_max_idle_in_txn_seconds", idleTxnSeconds, "Idle transaction", "Close or commit the idle transaction"),
		metricRule("txn.prepared_orphan", SeverityCritical, "pg_oldest_prepared_xact_seconds", preparedSeconds, "Orphaned prepared transaction", "Resolve the prepared transaction so vacuum can proceed"),
		metricRule("txn.high_rollback_ratio", SeverityInfo, "pg_xact_rollback_ratio", rollbackShare, "High rollback ratio", "Investigate transaction failures and retry behavior"),
		metricRule("txn.wraparound_risk", SeverityCritical, "pg_max_datfrozenxid_age", wraparoundAge, "Transaction ID wraparound risk", "Run maintenance immediately and resolve vacuum starvation"),
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
