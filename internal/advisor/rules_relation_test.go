package advisor

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func relationRuleByID(t *testing.T, id string) Rule { return ruleByID(t, id) }
func TestPackB_FiringAndQuietMatrix(t *testing.T) {
	tests := []struct {
		id          string
		fire, quiet *Snapshot
	}{
		{"index.unused", &Snapshot{Indexes: []IndexStat{{Name: "i", HistoryDays: 8, SizeBytes: unusedIndexBytes + 1}}}, &Snapshot{Indexes: []IndexStat{{Name: "i", HistoryDays: 8, SizeBytes: unusedIndexBytes, IdxScan: 1}}}},
		{"index.duplicate", &Snapshot{Indexes: []IndexStat{{Name: "a", TableName: "t", DefHash: "x"}, {Name: "b", TableName: "t", DefHash: "x"}}}, &Snapshot{Indexes: []IndexStat{{Name: "a", TableName: "t", DefHash: "x"}}}},
		{"index.redundant_prefix", &Snapshot{Indexes: []IndexStat{{Name: "a", TableName: "t", IndexDef: "CREATE INDEX ON t (a)"}, {Name: "b", TableName: "t", IndexDef: "CREATE INDEX ON t (a,b)"}}}, &Snapshot{Indexes: []IndexStat{{Name: "a", TableName: "t", IndexDef: "not parseable"}, {Name: "b", TableName: "t", IndexDef: "not parseable"}}}},
		{"index.invalid", &Snapshot{Indexes: []IndexStat{{Name: "i", IsValid: false}}}, &Snapshot{Indexes: []IndexStat{{Name: "i", IsValid: true}}}},
		{"index.bloat_high", &Snapshot{Bloat: []BloatStat{{Name: "i", Ratio: .41, SizeBytes: indexBloatBytes + 1}}}, &Snapshot{Bloat: []BloatStat{{Name: "i", Ratio: .4, SizeBytes: indexBloatBytes + 1}}}},
		{"index.divergence", &Snapshot{Siblings: []SiblingInfo{{IndexHashes: map[string]string{"i": "x"}}, {IndexHashes: map[string]string{}}}}, &Snapshot{Siblings: []SiblingInfo{{IndexHashes: map[string]string{"i": "x"}}, {IndexHashes: map[string]string{"i": "x"}}}}},
		{"table.dead_tuples_high", &Snapshot{Tables: []TableStat{{Name: "t", NLiveTup: 100000, NDeadTup: 21000}}}, &Snapshot{Tables: []TableStat{{Name: "t", NLiveTup: 100000, NDeadTup: 20000}}}},
		{"table.never_autovacuumed", &Snapshot{Tables: []TableStat{{Name: "t", NLiveTup: largeTableRows + 1}}}, &Snapshot{Tables: []TableStat{{Name: "t", NLiveTup: largeTableRows}}}},
		{"table.autoanalyze_stale", &Snapshot{Tables: []TableStat{{Name: "t", NLiveTup: 100, NModSinceAnalyze: 21}}}, &Snapshot{Tables: []TableStat{{Name: "t", NLiveTup: 100, NModSinceAnalyze: 20}}}},
		{"table.wraparound_risk", &Snapshot{Tables: []TableStat{{Name: "t", RelfrozenXIDAge: wraparoundRelationAge + 1}}}, &Snapshot{Tables: []TableStat{{Name: "t", RelfrozenXIDAge: wraparoundRelationAge}}}},
		{"table.bloat_high", &Snapshot{Bloat: []BloatStat{{Name: "t", Ratio: .31, SizeBytes: tableBloatBytes + 1}}}, &Snapshot{Bloat: []BloatStat{{Name: "t", Ratio: .3, SizeBytes: tableBloatBytes + 1}}}},
		{"table.seq_scan_heavy", &Snapshot{Tables: []TableStat{{Name: "t", NLiveTup: largeTableRows + 1, SeqScanRate: 11, IdxScanRate: 1}}}, &Snapshot{Tables: []TableStat{{Name: "t", NLiveTup: largeTableRows, SeqScanRate: 10, IdxScanRate: 1}}}},
		{"vacuum.starvation", &Snapshot{Tables: makeDeadTables(11), Metrics: map[string]map[string]float64{"pg_vacuum_jobs_running": {"": 3}}, Settings: map[string]string{"autovacuum_max_workers": "3"}}, &Snapshot{Tables: makeDeadTables(10), Metrics: map[string]map[string]float64{"pg_vacuum_jobs_running": {"": 3}}, Settings: map[string]string{"autovacuum_max_workers": "3"}}},
		{"vacuum.disabled", &Snapshot{Settings: map[string]string{"autovacuum": "off"}}, &Snapshot{Settings: map[string]string{"autovacuum": "on"}}},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			r := relationRuleByID(t, tc.id)
			f := r.Evaluate(tc.fire)
			require.NotEmpty(t, f)
			assertRuleContract(t, r, tc.fire)
			require.Empty(t, r.Evaluate(tc.quiet))
		})
	}
}

func makeDeadTables(n int) []TableStat {
	out := make([]TableStat, n)
	for i := range out {
		out[i] = TableStat{Name: uuid.NewString(), NLiveTup: 100, NDeadTup: 30}
	}
	return out
}
func TestRule_index_unused_DegradesWithShortHistory(t *testing.T) {
	s := &Snapshot{Indexes: []IndexStat{{Name: "i", HistoryDays: 1, SizeBytes: unusedIndexBytes + 1}}}
	f := relationRuleByID(t, "index.unused").Evaluate(s)
	require.Len(t, f, 1)
	require.Equal(t, StateDegraded, f[0].State)
	assertRuleContract(t, relationRuleByID(t, "index.unused"), s)
}
func TestRule_index_redundant_prefix_ParsesExpressionIndex(t *testing.T) {
	s := &Snapshot{Indexes: []IndexStat{{Name: "a", TableName: "t", IndexDef: "CREATE INDEX ON t (lower(a, 'x'))"}, {Name: "b", TableName: "t", IndexDef: "CREATE INDEX ON t (lower(a, 'x'), b)"}}}
	require.NotEmpty(t, relationRuleByID(t, "index.redundant_prefix").Evaluate(s))
}
func TestRule_index_redundant_prefix_SkipsUnparseable(t *testing.T) {
	s := &Snapshot{Indexes: []IndexStat{{Name: "a", TableName: "t", IndexDef: "broken"}, {Name: "b", TableName: "t", IndexDef: "broken"}}}
	require.Empty(t, relationRuleByID(t, "index.redundant_prefix").Evaluate(s))
}
func TestRule_index_duplicate_NamesBothIndexes(t *testing.T) {
	s := &Snapshot{Indexes: []IndexStat{{Name: "a", TableName: "t", DefHash: "x"}, {Name: "b", TableName: "t", DefHash: "x"}}}
	f := relationRuleByID(t, "index.duplicate").Evaluate(s)
	require.Len(t, f, 1)
	require.Contains(t, f[0].Detail, "a")
	require.Contains(t, f[0].Detail, "b")
}
func TestPackB_AllRulesSatisfyContract(t *testing.T) {
	s := &Snapshot{Metrics: map[string]map[string]float64{}, Settings: map[string]string{}}
	for _, r := range All() {
		if len(r.ID()) > 7 && (r.ID()[:7] == "index." || r.ID()[:6] == "table." || r.ID()[:7] == "vacuum.") {
			assertRuleContract(t, r, s)
		}
	}
}
