package advisor

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/pgtype"
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
		{"index.bloat_high", &Snapshot{Bloat: []BloatStat{{Name: "i", Kind: "index", Ratio: .41, SizeBytes: indexBloatBytes + 1}}}, &Snapshot{Bloat: []BloatStat{{Name: "i", Kind: "index", Ratio: .4, SizeBytes: indexBloatBytes + 1}}}},
		{"index.divergence", &Snapshot{Facts: ownIndexDefs("x"), Siblings: []SiblingInfo{{IndexHashes: map[string]string{indexHashKey("app", "public.other"): "y"}}}}, &Snapshot{Facts: ownIndexDefs("x"), Siblings: []SiblingInfo{{IndexHashes: map[string]string{indexHashKey("app", "public.i"): "x"}}}}},
		{"table.dead_tuples_high", &Snapshot{Tables: []TableStat{{Name: "t", NLiveTup: 100000, NDeadTup: 21000}}}, &Snapshot{Tables: []TableStat{{Name: "t", NLiveTup: 100000, NDeadTup: 20000}}}},
		{"table.never_autovacuumed", &Snapshot{Tables: []TableStat{{Name: "t", NLiveTup: largeTableRows + 1}}}, &Snapshot{Tables: []TableStat{{Name: "t", NLiveTup: largeTableRows}}}},
		{"table.autoanalyze_stale", &Snapshot{Tables: []TableStat{{Name: "t", NLiveTup: 100, NModSinceAnalyze: 21}}}, &Snapshot{Tables: []TableStat{{Name: "t", NLiveTup: 100, NModSinceAnalyze: 20}}}},
		{"table.wraparound_risk", &Snapshot{Tables: []TableStat{{Name: "t", RelfrozenXIDAge: wraparoundRelationAge + 1}}}, &Snapshot{Tables: []TableStat{{Name: "t", RelfrozenXIDAge: wraparoundRelationAge}}}},
		{"table.bloat_high", &Snapshot{Bloat: []BloatStat{{Name: "t", Kind: "table", Ratio: .31, SizeBytes: tableBloatBytes + 1}}}, &Snapshot{Bloat: []BloatStat{{Name: "t", Kind: "table", Ratio: .3, SizeBytes: tableBloatBytes + 1}}}},
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

func ownIndexDefs(hash string) map[string]map[string]Fact {
	return map[string]map[string]Fact{"index_def": {indexHashKey("app", "public.i"): {Key: "public.i", Labels: map[string]string{"def_hash": hash}}}}
}

// TestRule_BloatKindsDoNotCross: a bloated index is not a bloated table and
// a bloated table is not a bloated index.
func TestRule_BloatKindsDoNotCross(t *testing.T) {
	s := &Snapshot{Bloat: []BloatStat{
		{Name: "public.big_idx", Kind: "index", Ratio: .9, SizeBytes: 10 * tableBloatBytes},
		{Name: "public.big", Kind: "table", Ratio: .9, SizeBytes: 10 * tableBloatBytes},
	}}
	idx := relationRuleByID(t, "index.bloat_high").Evaluate(s)
	require.Len(t, idx, 1)
	require.Equal(t, "public.big_idx", idx[0].ObjectName)
	tbl := relationRuleByID(t, "table.bloat_high").Evaluate(s)
	require.Len(t, tbl, 1)
	require.Equal(t, "public.big", tbl[0].ObjectName)
}

// TestRule_IndexDuplicateStaysWithinDatabaseAndMethod: the same definition
// in two databases, or under two access methods, is not a duplicate.
func TestRule_IndexDuplicateStaysWithinDatabaseAndMethod(t *testing.T) {
	s := &Snapshot{Indexes: []IndexStat{
		{Datname: "app1", Name: "public.a", TableName: "public.t", DefHash: "x", IndexDef: "CREATE INDEX a ON public.t USING btree (c)"},
		{Datname: "app2", Name: "public.a", TableName: "public.t", DefHash: "x", IndexDef: "CREATE INDEX a ON public.t USING btree (c)"},
		{Datname: "app1", Name: "public.g", TableName: "public.t", DefHash: "x", IndexDef: "CREATE INDEX g ON public.t USING gin (c)"},
	}}
	require.Empty(t, relationRuleByID(t, "index.duplicate").Evaluate(s))
	s.Indexes = append(s.Indexes, IndexStat{Datname: "app1", Name: "public.b", TableName: "public.t", DefHash: "x", IndexDef: "CREATE INDEX b ON public.t USING btree (c)"})
	f := relationRuleByID(t, "index.duplicate").Evaluate(s)
	require.Len(t, f, 1)
	require.Equal(t, "app1", f[0].Datname)
	require.Equal(t, []string{"public.a", "public.b"}, f[0].Evidence["indexes"])
}

// TestRule_RedundantPrefixNeedsBtreeAndSamePredicate: a partial index or a
// non-btree index is not made redundant by a longer one.
func TestRule_RedundantPrefixNeedsBtreeAndSamePredicate(t *testing.T) {
	partial := &Snapshot{Indexes: []IndexStat{
		{Name: "public.a", TableName: "public.t", IndexDef: "CREATE INDEX a ON public.t USING btree (a) WHERE (b > 0)"},
		{Name: "public.ab", TableName: "public.t", IndexDef: "CREATE INDEX ab ON public.t USING btree (a, b)"},
	}}
	require.Empty(t, relationRuleByID(t, "index.redundant_prefix").Evaluate(partial))
	gin := &Snapshot{Indexes: []IndexStat{
		{Name: "public.a", TableName: "public.t", IndexDef: "CREATE INDEX a ON public.t USING gin (a)"},
		{Name: "public.ab", TableName: "public.t", IndexDef: "CREATE INDEX ab ON public.t USING gin (a, b)"},
	}}
	require.Empty(t, relationRuleByID(t, "index.redundant_prefix").Evaluate(gin))
	btree := &Snapshot{Indexes: []IndexStat{
		{Name: "public.a", TableName: "public.t", IndexDef: "CREATE INDEX a ON public.t USING btree (a)"},
		{Name: "public.ab", TableName: "public.t", IndexDef: "CREATE INDEX ab ON public.t USING btree (a, b)"},
	}}
	require.NotEmpty(t, relationRuleByID(t, "index.redundant_prefix").Evaluate(btree))
}

// TestRule_NeverAutovacuumedSkipsStandbys: a standby has no vacuum
// timestamps for any table.
func TestRule_NeverAutovacuumedSkipsStandbys(t *testing.T) {
	s := &Snapshot{Role: pgtype.RoleStandby, Tables: []TableStat{{Name: "public.t", NLiveTup: largeTableRows + 1}}}
	require.Empty(t, relationRuleByID(t, "table.never_autovacuumed").Evaluate(s))
}

// TestRule_RelationFindingsAreQualifiedByDatabase: the same relation in two
// databases yields two findings with two identities.
func TestRule_RelationFindingsAreQualifiedByDatabase(t *testing.T) {
	s := &Snapshot{Tables: []TableStat{
		{Datname: "app1", Name: "public.t", RelfrozenXIDAge: wraparoundRelationAge + 1},
		{Datname: "app2", Name: "public.t", RelfrozenXIDAge: wraparoundRelationAge + 1},
	}}
	f := relationRuleByID(t, "table.wraparound_risk").Evaluate(s)
	require.Len(t, f, 2)
	require.Equal(t, "table.wraparound_risk/app1/public.t", f[0].ID())
	require.Equal(t, "table.wraparound_risk/app2/public.t", f[1].ID())
}

// TestRule_IndexDivergenceComparesThisInstance: with a single standby the
// primary's missing and differing indexes are reported, once per sibling.
func TestRule_IndexDivergenceComparesThisInstance(t *testing.T) {
	sibling := uuid.New()
	s := &Snapshot{
		Facts: map[string]map[string]Fact{"index_def": {
			indexHashKey("app", "public.a"): {Key: "public.a", Labels: map[string]string{"def_hash": "1"}},
			indexHashKey("app", "public.b"): {Key: "public.b", Labels: map[string]string{"def_hash": "2"}},
			indexHashKey("app", "public.c"): {Key: "public.c", Labels: map[string]string{"def_hash": "3"}},
		}},
		Siblings: []SiblingInfo{{InstanceID: sibling, IndexHashes: map[string]string{indexHashKey("app", "public.a"): "1", indexHashKey("app", "public.b"): "other"}}},
	}
	f := relationRuleByID(t, "index.divergence").Evaluate(s)
	require.Len(t, f, 2)
	require.Equal(t, "app", f[0].Datname)
	require.Equal(t, "public.b@"+sibling.String(), f[0].ObjectName)
	require.Equal(t, sibling.String(), f[0].Evidence["differs_on"])
	require.Equal(t, "public.c@"+sibling.String(), f[1].ObjectName)
	require.Equal(t, sibling.String(), f[1].Evidence["missing_on"])
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
