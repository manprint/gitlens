package advisor

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func ruleByID(t *testing.T, id string) Rule {
	t.Helper()
	for _, r := range All() {
		if r.ID() == id {
			return r
		}
	}
	t.Fatalf("rule %s is not registered", id)
	return nil
}

func statementSnapshot(q StatementStat) *Snapshot {
	return &Snapshot{Statements: []StatementStat{q}, Metrics: map[string]map[string]float64{"work_mem_bytes": {"": 64}}}
}

func TestRule_query_slow_mean_FiresWhenAboveThreshold(t *testing.T) {
	assertRuleContract(t, ruleByID(t, "query.slow_mean"), statementSnapshot(StatementStat{QueryID: 1, Calls: 10, MeanExecTimeMs: 501}))
}
func TestRule_query_slow_mean_QuietWhenBelow(t *testing.T) {
	require.Empty(t, ruleByID(t, "query.slow_mean").Evaluate(statementSnapshot(StatementStat{Calls: 10, MeanExecTimeMs: 500})))
}
func TestRule_query_total_time_share_FiresWhenAboveThreshold(t *testing.T) {
	s := &Snapshot{Statements: []StatementStat{{QueryID: 1, TotalExecTimeMs: 31}, {QueryID: 2, TotalExecTimeMs: 69}}}
	assertRuleContract(t, ruleByID(t, "query.total_time_share"), s)
}
func TestRule_query_total_time_share_QuietWhenBelow(t *testing.T) {
	s := &Snapshot{Statements: []StatementStat{{TotalExecTimeMs: 25}, {TotalExecTimeMs: 25}, {TotalExecTimeMs: 25}, {TotalExecTimeMs: 25}}}
	require.Empty(t, ruleByID(t, "query.total_time_share").Evaluate(s))
}
func TestRule_query_regression_FiresWhenAboveThreshold(t *testing.T) {
	s := statementSnapshot(StatementStat{QueryID: 1, MeanExecTimeMs: 301})
	s.Baseline = &Baseline{MeanExecTimeByQueryID: map[int64]float64{1: 100}}
	assertRuleContract(t, ruleByID(t, "query.regression"), s)
}
func TestRule_query_regression_QuietWhenBelow(t *testing.T) {
	s := statementSnapshot(StatementStat{QueryID: 1, MeanExecTimeMs: 299})
	s.Baseline = &Baseline{MeanExecTimeByQueryID: map[int64]float64{1: 100}}
	require.Empty(t, ruleByID(t, "query.regression").Evaluate(s))
}
func TestRule_query_regression_DegradesWithoutBaseline(t *testing.T) {
	f := ruleByID(t, "query.regression").Evaluate(statementSnapshot(StatementStat{QueryID: 1}))
	require.Len(t, f, 1)
	require.Equal(t, StateDegraded, f[0].State)
	assertRuleContract(t, ruleByID(t, "query.regression"), statementSnapshot(StatementStat{QueryID: 1}))
}
func TestRule_query_temp_bytes_high_FiresWhenAboveThreshold(t *testing.T) {
	assertRuleContract(t, ruleByID(t, "query.temp_bytes_high"), statementSnapshot(StatementStat{QueryID: 1, Calls: 10, TempBytes: 1000}))
}
func TestRule_query_temp_bytes_high_QuietWhenBelow(t *testing.T) {
	require.Empty(t, ruleByID(t, "query.temp_bytes_high").Evaluate(statementSnapshot(StatementStat{Calls: 10, TempBytes: 1})))
}
func TestRule_query_cache_miss_high_FiresWhenAboveThreshold(t *testing.T) {
	assertRuleContract(t, ruleByID(t, "query.cache_miss_high"), statementSnapshot(StatementStat{QueryID: 1, Calls: 1001, SharedBlksRead: 4, SharedBlksHit: 1}))
}
func TestRule_query_cache_miss_high_QuietWhenBelow(t *testing.T) {
	require.Empty(t, ruleByID(t, "query.cache_miss_high").Evaluate(statementSnapshot(StatementStat{Calls: 1001, SharedBlksRead: 3, SharedBlksHit: 7})))
}

func metricSnapshot(name string, value float64) *Snapshot {
	return &Snapshot{Metrics: map[string]map[string]float64{name: {"": value}}}
}
func TestRule_txn_long_running_FiresWhenAboveThreshold(t *testing.T) {
	assertRuleContract(t, ruleByID(t, "txn.long_running"), metricSnapshot("pg_max_xact_age_seconds", 901))
}
func TestRule_txn_long_running_QuietWhenBelow(t *testing.T) {
	require.Empty(t, ruleByID(t, "txn.long_running").Evaluate(metricSnapshot("pg_max_xact_age_seconds", 900)))
}
func TestRule_txn_idle_in_transaction_FiresWhenAboveThreshold(t *testing.T) {
	assertRuleContract(t, ruleByID(t, "txn.idle_in_transaction"), metricSnapshot("pg_max_idle_in_txn_seconds", 301))
}
func TestRule_txn_idle_in_transaction_QuietWhenBelow(t *testing.T) {
	require.Empty(t, ruleByID(t, "txn.idle_in_transaction").Evaluate(metricSnapshot("pg_max_idle_in_txn_seconds", 300)))
}
func TestRule_txn_prepared_orphan_FiresWhenAboveThreshold(t *testing.T) {
	assertRuleContract(t, ruleByID(t, "txn.prepared_orphan"), metricSnapshot("pg_oldest_prepared_xact_seconds", 3601))
}
func TestRule_txn_prepared_orphan_QuietWhenBelow(t *testing.T) {
	require.Empty(t, ruleByID(t, "txn.prepared_orphan").Evaluate(metricSnapshot("pg_oldest_prepared_xact_seconds", 3600)))
}
func TestRule_txn_high_rollback_ratio_FiresWhenAboveThreshold(t *testing.T) {
	assertRuleContract(t, ruleByID(t, "txn.high_rollback_ratio"), metricSnapshot("pg_xact_rollback_ratio", .11))
}
func TestRule_txn_high_rollback_ratio_QuietWhenBelow(t *testing.T) {
	require.Empty(t, ruleByID(t, "txn.high_rollback_ratio").Evaluate(metricSnapshot("pg_xact_rollback_ratio", .10)))
}
func TestRule_txn_wraparound_risk_FiresWhenAboveThreshold(t *testing.T) {
	assertRuleContract(t, ruleByID(t, "txn.wraparound_risk"), metricSnapshot("pg_max_datfrozenxid_age", wraparoundAge+1))
}
func TestRule_txn_wraparound_risk_QuietWhenBelow(t *testing.T) {
	require.Empty(t, ruleByID(t, "txn.wraparound_risk").Evaluate(metricSnapshot("pg_max_datfrozenxid_age", wraparoundAge)))
}

func TestPackA_AllRulesSatisfyContract(t *testing.T) {
	s := &Snapshot{Metrics: map[string]map[string]float64{}, Statements: []StatementStat{}}
	for _, r := range All() {
		assertRuleContract(t, r, s)
	}
}
