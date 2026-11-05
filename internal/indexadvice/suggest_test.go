package indexadvice

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const testDB = "odoo"

var partner = Key{testDB, "public", "res_partner"}

func nd(v float64) *float64 { return &v }

func col(typ string, n float64) Column {
	c := Column{Type: typ, AvgWidth: 8}
	if n != 0 {
		c.NDistinct = nd(n)
	}
	return c
}

func partnerMeta() TableMeta {
	return TableMeta{
		RelTuples: 50000, LiveTuples: 50000, HasCounts: true, HasWrites: true,
		Columns: map[string]Column{
			"ref": col("text", 40000), "name": col("text", 1500), "active": col("boolean", 2),
			"company_id": col("integer", 5), "write_date": col("timestamp", -0.9), "id": col("integer", -1),
			"data": col("json", 0), "a": col("integer", 100), "b": col("integer", 200), "c": col("integer", 300),
			"d": col("integer", 400), "e": col("integer", 500),
		},
	}
}

func input(texts ...string) Input {
	in := Input{
		Window:          24 * time.Hour,
		TotalExecMsRate: map[string]float64{testDB: 100},
		Tables:          map[Key]TableMeta{partner: partnerMeta()},
		Indexes:         map[Key][]IndexDef{},
	}
	for i, text := range texts {
		in.Statements = append(in.Statements, Stmt{Database: testDB, QueryID: int64(i + 1), Text: text, CallsRate: 10, ExecMsRate: 10})
	}
	return in
}

func names(s Suggestion) string { return columnsText(s.Columns) }

func TestIDXS001_EqualityUnindexed(t *testing.T) {
	out := Suggest(input(`SELECT id FROM res_partner WHERE ref = $1`))
	require.Len(t, out.Suggestions, 1)
	s := out.Suggestions[0]
	require.Equal(t, "ref", names(s))
	require.Equal(t, "res_partner", s.Table)
	require.Equal(t, "public", s.Schema)
	require.Equal(t, "CREATE INDEX CONCURRENTLY idx_res_partner_ref ON public.res_partner (ref);", s.CreateStatement)
	require.Len(t, s.ID, 12)
	require.Equal(t, 86400, out.WindowSeconds)
}

func TestIDXS002_ExistingIndexCovers(t *testing.T) {
	in := input(`SELECT id FROM res_partner WHERE ref = $1`)
	in.Indexes[partner] = []IndexDef{{Name: "p_ref", Method: "btree", Columns: []IndexColumn{{Name: "ref"}}}}
	require.Empty(t, Suggest(in).Suggestions)
}

func TestIDXS003_LongerIndexCoversPrefix(t *testing.T) {
	in := input(`SELECT id FROM res_partner WHERE ref = $1`)
	in.Indexes[partner] = []IndexDef{{Name: "p_ref_name", Method: "btree", Columns: []IndexColumn{{Name: "ref"}, {Name: "name"}}}}
	require.Empty(t, Suggest(in).Suggestions)
}

func TestIDXS004_WrongOrderDoesNotCover(t *testing.T) {
	in := input(`SELECT id FROM res_partner WHERE ref = $1`)
	in.Indexes[partner] = []IndexDef{{Name: "p_name_ref", Method: "btree", Columns: []IndexColumn{{Name: "name"}, {Name: "ref"}}}}
	out := Suggest(in)
	require.Len(t, out.Suggestions, 1)
	require.Empty(t, out.Suggestions[0].OverlappingIndexes)
}

func TestIDXS004_OverlapListed(t *testing.T) {
	in := input(`SELECT id FROM res_partner WHERE ref = $1 AND name = $2`)
	in.Indexes[partner] = []IndexDef{{Name: "p_ref", Method: "btree", Columns: []IndexColumn{{Name: "ref"}}}, {Name: "p_hash", Method: "hash", Columns: []IndexColumn{{Name: "ref"}}}}
	out := Suggest(in)
	require.Len(t, out.Suggestions, 1)
	require.Equal(t, []string{"p_hash", "p_ref"}, out.Suggestions[0].OverlappingIndexes)
	require.Equal(t, "ref,name", names(out.Suggestions[0]))
}

func TestIDXS005_EqualityBySelectivity(t *testing.T) {
	out := Suggest(input(`SELECT id FROM res_partner WHERE company_id = $1 AND name = $2 AND ref = $3`))
	require.Equal(t, "ref,name,company_id", names(out.Suggestions[0]))
}

func TestIDXS006_RangeLast(t *testing.T) {
	out := Suggest(input(`SELECT id FROM res_partner WHERE write_date > $1 AND company_id = $2`))
	require.Equal(t, "company_id,write_date", names(out.Suggestions[0]))
}

func TestIDXS007_OrderByLimitLast(t *testing.T) {
	out := Suggest(input(`SELECT id FROM res_partner WHERE company_id = $1 ORDER BY write_date DESC LIMIT 10`))
	require.Equal(t, "company_id,write_date DESC", names(out.Suggestions[0]))
	require.Contains(t, out.Suggestions[0].CreateStatement, "(company_id, write_date DESC);")
	require.Empty(t, Suggest(input(`SELECT id FROM res_partner ORDER BY write_date`)).Suggestions, "ORDER BY without LIMIT or filter")
	out = Suggest(input(`SELECT id FROM res_partner ORDER BY name LIMIT 5`))
	require.Equal(t, "name", names(out.Suggestions[0]))
}

func TestIDXS008_FourColumnCap(t *testing.T) {
	out := Suggest(input(`SELECT 1 FROM res_partner WHERE a = $1 AND b = $2 AND c = $3 AND d = $4 AND e = $5 AND write_date > $6`))
	require.Equal(t, "e,d,c,b", names(out.Suggestions[0]))
}

func TestIDXS009_JSONColumnDropped(t *testing.T) {
	out := Suggest(input(`SELECT 1 FROM res_partner WHERE data = $1 AND ref = $2`))
	require.Equal(t, "ref", names(out.Suggestions[0]))
	require.Contains(t, out.Suggestions[0].Notes, NoteTypeNotBtree)
	require.Empty(t, Suggest(input(`SELECT 1 FROM res_partner WHERE data = $1`)).Suggestions)
}

func TestIDXS009_UnknownColumnDropped(t *testing.T) {
	out := Suggest(input(`SELECT 1 FROM res_partner WHERE nosuch = $1 AND ref = $2`))
	require.Equal(t, "ref", names(out.Suggestions[0]))
	require.Contains(t, out.Suggestions[0].Notes, NoteColumnUnknown)
}

func TestIDXS010_SmallTable(t *testing.T) {
	in := input(`SELECT 1 FROM res_partner WHERE ref = $1`)
	meta := partnerMeta()
	meta.RelTuples, meta.LiveTuples = 999, 999
	in.Tables[partner] = meta
	out := Suggest(in)
	require.Empty(t, out.Suggestions)
	require.Equal(t, 1, out.Coverage.Skipped[NoteSmallTable])
	meta.RelTuples, meta.LiveTuples = 1000, 1000
	in.Tables[partner] = meta
	require.Len(t, Suggest(in).Suggestions, 1)
}

// n_live_tup is 0 after a statistics reset or a restore: reltuples decides.
func TestIDXS029_SmallTableUsesReltuples(t *testing.T) {
	in := input(`SELECT 1 FROM res_partner WHERE ref = $1`)
	meta := partnerMeta()
	meta.RelTuples, meta.LiveTuples = 30006, 0
	in.Tables[partner] = meta
	out := Suggest(in)
	require.Len(t, out.Suggestions, 1)
	require.Zero(t, out.Coverage.Skipped[NoteSmallTable])
}

func TestIDXS011_PrefixMergesIntoLonger(t *testing.T) {
	in := input(`SELECT 1 FROM res_partner WHERE ref = $1`, `SELECT 1 FROM res_partner WHERE ref = $1 AND name = $2`, `SELECT 1 FROM res_partner WHERE ref = $1`)
	in.Statements[1].ExecMsRate = 30
	out := Suggest(in)
	require.Len(t, out.Suggestions, 1)
	s := out.Suggestions[0]
	require.Equal(t, "ref,name", names(s))
	require.Len(t, s.Statements, 3)
	require.InDelta(t, 50, s.ReadCostMsPerS, 1e-9)
	require.InDelta(t, 30, s.CallsRate, 1e-9)
	require.Equal(t, int64(2), s.Statements[0].QueryID, "heaviest statement first")
}

func TestIDXS012_WeightArithmetic(t *testing.T) {
	in := input(`SELECT 1 FROM res_partner WHERE ref = $1`, `SELECT 1 FROM res_partner WHERE name = $1`)
	in.Statements[0].ExecMsRate = 30
	in.Statements[1].ExecMsRate = 5
	out := Suggest(in)
	require.Len(t, out.Suggestions, 2)
	require.Equal(t, "ref", names(out.Suggestions[0]))
	require.InDelta(t, 30, out.Suggestions[0].WeightPct, 1e-9)
	require.InDelta(t, 5, out.Suggestions[1].WeightPct, 1e-9)
	require.InDelta(t, 100, out.Suggestions[0].Statements[0].SharePct, 1e-9)
}

func TestIDXS019_OrderStable(t *testing.T) {
	texts := []string{
		`SELECT 1 FROM res_partner WHERE ref = $1`, `SELECT 1 FROM res_partner WHERE name = $1`,
		`SELECT 1 FROM res_partner WHERE company_id = $1 AND write_date > $2`, `SELECT 1 FROM res_partner WHERE a = $1 AND b = $2`,
		`SELECT 1 FROM res_partner WHERE ref = $1 AND name = $2`, `SELECT 1 FROM res_partner WHERE c = $1`,
	}
	base := input(texts...)
	for i := range base.Statements {
		base.Statements[i].ExecMsRate = float64(i%3 + 1)
	}
	want := Suggest(base)
	rng := rand.New(rand.NewSource(7))
	for n := 0; n < 100; n++ {
		in := base
		in.Statements = append([]Stmt(nil), base.Statements...)
		rng.Shuffle(len(in.Statements), func(i, j int) { in.Statements[i], in.Statements[j] = in.Statements[j], in.Statements[i] })
		require.Equal(t, want, Suggest(in))
	}
}

func TestIDXS020_Limit(t *testing.T) {
	var texts []string
	for i := 0; i < 30; i++ {
		texts = append(texts, fmt.Sprintf(`SELECT 1 FROM t%d WHERE x = $1`, i))
	}
	in := input(texts...)
	require.Len(t, Suggest(in).Suggestions, DefaultLimit)
	in.Limit = 5
	require.Len(t, Suggest(in).Suggestions, 5)
	in.Limit = 1000
	require.Len(t, Suggest(in).Suggestions, 30)
	for i := 0; i < 100; i++ {
		in.Statements = append(in.Statements, Stmt{Database: testDB, QueryID: int64(1000 + i), Text: fmt.Sprintf(`SELECT 1 FROM u%d WHERE x = $1`, i)})
	}
	require.Len(t, Suggest(in).Suggestions, MaxLimit)
}

func TestIDXS021_SkipsPropagate(t *testing.T) {
	out := Suggest(input(
		`SELECT 1 FROM res_partner WHERE lower(name) = $1`,
		`SELECT 1 FROM res_partner WHERE ref = $1 OR name = $2`,
		`SELECT 1 FROM pg_catalog.pg_class WHERE relname = $1`,
		`BEGIN`,
		`SELECT 1 FROM res_partner WHERE ref = $1`,
	))
	require.Equal(t, 5, out.Coverage.StatementsConsidered)
	require.Equal(t, 1, out.Coverage.StatementsAnalysed)
	require.Equal(t, 1, out.Coverage.Skipped["expression"])
	require.Equal(t, 1, out.Coverage.Skipped["or_group"])
	require.Equal(t, 1, out.Coverage.Skipped["catalog"])
	require.Equal(t, 1, out.Coverage.Skipped["unsupported_statement"])
}

func TestIDXS018_PartialIndex(t *testing.T) {
	partial := []IndexDef{{Name: "p_ref_active", Method: "btree", Columns: []IndexColumn{{Name: "ref"}}, Where: "(active)"}}
	in := input(`SELECT 1 FROM res_partner WHERE ref = $1 AND active`)
	in.Indexes[partner] = partial
	require.Empty(t, Suggest(in).Suggestions)
	in = input(`SELECT 1 FROM res_partner WHERE ref = $1`)
	in.Indexes[partner] = partial
	require.Len(t, Suggest(in).Suggestions, 1)
}

func TestIDXS018_NotNullPartialAndHash(t *testing.T) {
	notNull := []IndexDef{{Name: "p_ref_nn", Method: "btree", Columns: []IndexColumn{{Name: "ref"}}, Where: "(ref IS NOT NULL)"}}
	for _, text := range []string{
		`SELECT 1 FROM res_partner WHERE ref = $1`,
		`SELECT 1 FROM res_partner WHERE "res_partner"."ref" IN ($1, $2)`,
		`SELECT 1 FROM res_partner WHERE ref > $1`,
		`SELECT 1 FROM res_partner WHERE ref LIKE 'ab%'`,
	} {
		in := input(text)
		in.Indexes[partner] = notNull
		require.Empty(t, Suggest(in).Suggestions, text)
	}
	in := input(`SELECT 1 FROM res_partner WHERE name = $1 ORDER BY ref LIMIT 5`)
	in.Indexes[partner] = notNull
	out := Suggest(in)
	require.Len(t, out.Suggestions, 1, "ORDER BY alone does not prove non-null")
	in = input(`SELECT 1 FROM res_partner WHERE ref IS NULL ORDER BY ref LIMIT 5`)
	in.Indexes[partner] = notNull
	require.Len(t, Suggest(in).Suggestions, 1)

	hash := []IndexDef{{Name: "p_ref_hash", Method: "hash", Columns: []IndexColumn{{Name: "ref"}}}}
	in = input(`SELECT 1 FROM res_partner WHERE ref = $1`)
	in.Indexes[partner] = hash
	require.Empty(t, Suggest(in).Suggestions, "a hash index serves single-column equality")
	for _, text := range []string{`SELECT 1 FROM res_partner WHERE ref > $1`, `SELECT 1 FROM res_partner WHERE ref = $1 AND name = $2`} {
		in = input(text)
		in.Indexes[partner] = hash
		out = Suggest(in)
		require.Len(t, out.Suggestions, 1, text)
		require.Equal(t, []string{"p_ref_hash"}, out.Suggestions[0].OverlappingIndexes, text)
	}
}

func TestIDXS018_UnmatchedPartialIsOverlap(t *testing.T) {
	in := input(`SELECT 1 FROM res_partner WHERE ref = $1`)
	in.Indexes[partner] = []IndexDef{{Name: "p_ref_active", Method: "btree", Columns: []IndexColumn{{Name: "ref"}}, Where: "(active)"}}
	out := Suggest(in)
	require.Equal(t, []string{"p_ref_active"}, out.Suggestions[0].OverlappingIndexes)
}

func TestIDXS022_IncludeAndExpressionNeverCover(t *testing.T) {
	in := input(`SELECT 1 FROM res_partner WHERE ref = $1`)
	in.Indexes[partner] = []IndexDef{
		{Name: "p_inc", Method: "btree", Columns: []IndexColumn{{Name: "name"}}, Include: []string{"ref"}},
		{Name: "p_expr", Method: "btree", Columns: []IndexColumn{{Name: "ref", Expr: true}}},
	}
	require.Len(t, Suggest(in).Suggestions, 1)
}

func TestIDXS023_MissingMetadata(t *testing.T) {
	in := input(`SELECT 1 FROM other_table WHERE x = $1 AND y = $2`)
	out := Suggest(in)
	require.Len(t, out.Suggestions, 1)
	s := out.Suggestions[0]
	require.Nil(t, s.EstSizeBytes)
	require.Contains(t, s.Notes, NoteSizeUnknown)
	require.Contains(t, s.Notes, NoteSelectivityUnknown)
	require.Equal(t, "low", s.Confidence)
	require.Equal(t, "x,y", names(s), "source order without statistics")
	require.Equal(t, "public", s.Schema)
	require.Equal(t, "unknown", s.Selectivity.Label)
	require.Contains(t, s.Notes, NoteWritesUnknown)
	require.Equal(t, VerdictModerate, s.Verdict, "ratio 10000 would be strong, but the writes are unknown")
	require.Equal(t, "low", s.Confidence)
}

func TestIDXS023_CountsAndWritesUnknown(t *testing.T) {
	in := input(`SELECT 1 FROM res_partner WHERE ref = $1`)
	meta := partnerMeta()
	meta.HasCounts, meta.HasWrites = false, false
	meta.RelTuples, meta.LiveTuples = 0, 0
	in.Tables[partner] = meta
	in.Statements[0].CallsRate = 1000
	out := Suggest(in)
	require.Len(t, out.Suggestions, 1, "unknown counts must not suppress the table as small")
	s := out.Suggestions[0]
	require.Zero(t, out.Coverage.Skipped[NoteSmallTable])
	require.Nil(t, s.EstSizeBytes)
	require.Contains(t, s.Notes, NoteSizeUnknown)
	require.Contains(t, s.Notes, NoteWritesUnknown)
	require.Equal(t, VerdictModerate, s.Verdict)
	require.Equal(t, "medium", s.Confidence, "high selectivity but unknown writes")
	meta.HasWrites = true
	in.Tables[partner] = meta
	require.Equal(t, VerdictStrong, Suggest(in).Suggestions[0].Verdict)
}

func TestIDXS024_SchemaResolution(t *testing.T) {
	in := input(`SELECT 1 FROM res_partner WHERE ref = $1`, `SELECT 1 FROM sales.res_partner WHERE ref = $1`)
	k := Key{testDB, "sales", "res_partner"}
	in.Tables[k] = partnerMeta()
	out := Suggest(in)
	require.Len(t, out.Suggestions, 2)
	schemas := out.Suggestions[0].Schema + "," + out.Suggestions[1].Schema
	require.Equal(t, "public,sales", schemas)
	only := input(`SELECT 1 FROM res_partner WHERE ref = $1`)
	delete(only.Tables, partner)
	only.Tables[k] = partnerMeta()
	require.Equal(t, "sales", Suggest(only).Suggestions[0].Schema)
}

func TestIDXS025_TextTruncated(t *testing.T) {
	long := `SELECT 1 FROM res_partner WHERE ref = $1 AND name = '` + strings.Repeat("é", 600) + `'`
	require.Len(t, []rune(Suggest(input(long)).Suggestions[0].Statements[0].QueryText), 400)
}

func TestIDXS026_WritesAndVerdict(t *testing.T) {
	in := input(`SELECT 1 FROM res_partner WHERE ref = $1`)
	meta := partnerMeta()
	meta.Writes = Writes{Ins: 1, Upd: 4, Del: 0, HotUpd: 2}
	in.Tables[partner] = meta
	in.Statements[0].CallsRate = 30
	s := Suggest(in).Suggestions[0]
	require.InDelta(t, 0.5, s.Write.HotUpdateRatio, 1e-9)
	require.InDelta(t, 10, s.ReadWriteRatio, 1e-9)
	require.Equal(t, VerdictStrong, s.Verdict)
	require.Equal(t, "high", s.Selectivity.Label)
	require.Equal(t, "high", s.Confidence)
}

func TestIDXS027_EmptyInput(t *testing.T) {
	out := Suggest(Input{})
	require.NotNil(t, out.Suggestions)
	require.Empty(t, out.Suggestions)
	require.NotNil(t, out.Coverage.Skipped)
}

func TestIDXS005_FractionRanksByRows(t *testing.T) {
	out := Suggest(input(`SELECT 1 FROM res_partner WHERE ref = $1 AND write_date = $2 AND zzz = $3`))
	require.Equal(t, "write_date,ref", names(out.Suggestions[0]), "-0.9 of 50000 rows = 45000 distinct > 40000; unknown column dropped")
	require.Contains(t, out.Suggestions[0].Notes, NoteColumnUnknown)
}

func TestIDXS028_QueryIDJSONString(t *testing.T) {
	for _, id := range []int64{-6200000000000000000, 9223372036854775807, 11} {
		out, err := json.Marshal(StmtShare{QueryID: id, SharePct: 1, QueryText: "q"})
		require.NoError(t, err)
		require.Contains(t, string(out), fmt.Sprintf(`"queryid":"%d"`, id))
	}
}

// A statement is analysed when one predicate group became a candidate; a
// statement can carry several skip reasons, so they do not sum to the rest.
func TestIDXS030_CoverageCounts(t *testing.T) {
	in := input(
		`SELECT 1 FROM res_partner WHERE ref = $1`,
		`SELECT 1 FROM res_partner WHERE ref = $1 OR name = $2`,
		`SELECT 1 FROM res_partner WHERE lower(name) = $1 AND (ref = $2 OR name = $3)`,
		`SELECT 1 FROM tiny WHERE x = $1`,
	)
	tiny := partnerMeta()
	tiny.RelTuples, tiny.LiveTuples = 10, 10
	in.Tables[Key{testDB, "public", "tiny"}] = tiny
	out := Suggest(in)
	c := out.Coverage
	require.Equal(t, 4, c.StatementsConsidered)
	require.Equal(t, 1, c.StatementsAnalysed, "only the first statement became a candidate")
	require.Equal(t, 1, c.Skipped[NoteSmallTable])
	require.GreaterOrEqual(t, c.Skipped[string(SkipOrGroup)], 2, "statements 2 and 3 have an OR")
	require.GreaterOrEqual(t, c.Skipped[string(SkipExpression)], 1, "statement 3 filters on lower(name), and also has an OR")
	sum := 0
	for _, n := range c.Skipped {
		sum += n
	}
	require.Greater(t, sum, c.StatementsConsidered-c.StatementsAnalysed, "a statement can have several reasons")
}

func TestIDXS031_NoWritesIsNeverWriteHeavy(t *testing.T) {
	in := input(`SELECT 1 FROM res_partner WHERE ref = $1`)
	in.Statements[0].CallsRate = 0.0003
	meta := partnerMeta()
	meta.Writes = Writes{}
	in.Tables[partner] = meta
	require.Equal(t, VerdictModerate, Suggest(in).Suggestions[0].Verdict, "writes known and zero")

	meta.HasWrites = false
	in.Tables[partner] = meta
	require.Equal(t, VerdictModerate, Suggest(in).Suggestions[0].Verdict, "writes unknown")

	meta.HasWrites = true
	meta.Writes = Writes{Ins: 5}
	in.Tables[partner] = meta
	require.Equal(t, VerdictWriteHeavy, Suggest(in).Suggestions[0].Verdict, "real writes still weigh")
}

// With the complete index list a primary key covers id = $1; a relation that is
// not a table of the inventory is skipped, not suggested.
func TestIDXS032_CompleteIndexList(t *testing.T) {
	in := input(
		`SELECT 1 FROM res_partner WHERE id = $1`,
		`SELECT 1 FROM public.some_view WHERE x = $1`,
		`SELECT 1 FROM public.pg_stat_statements WHERE queryid = $1`,
	)
	meta := partnerMeta()
	meta.IndexesKnown = true
	in.Tables[partner] = meta
	in.IndexesComplete = map[string]bool{testDB: true}
	pk, err := ParseIndexDef("CREATE UNIQUE INDEX res_partner_pkey ON public.res_partner USING btree (id)")
	require.NoError(t, err)
	in.Indexes[partner] = []IndexDef{pk}
	out := Suggest(in)
	require.Empty(t, out.Suggestions)
	require.Equal(t, 1, out.Coverage.Skipped[NoteNotTable])
	require.Equal(t, 1, out.Coverage.Skipped[string(SkipCatalog)])

	in.Indexes[partner] = nil
	require.Len(t, Suggest(in).Suggestions, 1, "without the primary key the id lookup is suggested")
}

// Without a complete list (an agent that does not report it) a relation unknown
// to the inventory is still analysed, as before.
func TestIDXS033_PartialIndexListKeepsOldBehaviour(t *testing.T) {
	in := input(`SELECT 1 FROM public.other WHERE x = $1`)
	require.Len(t, Suggest(in).Suggestions, 1)
	require.Zero(t, Suggest(in).Coverage.Skipped[NoteNotTable])
}

// A unique index on the equality columns already resolves to one row: a longer
// key that starts with them is not suggested, one on other columns still is.
func TestIDXS034_UniqueIndexServesLongerEqualityKey(t *testing.T) {
	in := input(`SELECT 1 FROM res_partner WHERE id = $1 AND active = $2`)
	pk, err := ParseIndexDef("CREATE UNIQUE INDEX res_partner_pkey ON public.res_partner USING btree (id)")
	require.NoError(t, err)
	in.Indexes[partner] = []IndexDef{pk}
	require.Empty(t, Suggest(in).Suggestions)

	nonUnique, err := ParseIndexDef("CREATE INDEX res_partner_id ON public.res_partner USING btree (id)")
	require.NoError(t, err)
	in.Indexes[partner] = []IndexDef{nonUnique}
	require.Len(t, Suggest(in).Suggestions, 1, "a non-unique index on id does not make (id, active) pointless")

	byName, err := ParseIndexDef("CREATE UNIQUE INDEX res_partner_ref_key ON public.res_partner USING btree (ref)")
	require.NoError(t, err)
	in.Indexes[partner] = []IndexDef{byName}
	require.Len(t, Suggest(in).Suggestions, 1, "a unique key the statement does not filter on is no help")
}
