package indexadvice

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIDXS014_VerdictThresholds(t *testing.T) {
	require.Equal(t, VerdictStrong, verdictFor(10, false))
	require.Equal(t, VerdictModerate, verdictFor(9.99, false))
	require.Equal(t, VerdictModerate, verdictFor(1, false))
	require.Equal(t, VerdictWriteHeavy, verdictFor(0.99, false))
}

func TestIDXSW001_WriteDenominator(t *testing.T) {
	require.InDelta(t, 5/0.001, readWriteRatio(5, Writes{Upd: 20, HotUpd: 20}), 1e-6)
	require.InDelta(t, 1, readWriteRatio(10, Writes{Upd: 10}), 1e-9)
	require.InDelta(t, 1, readWriteRatio(10, Writes{Ins: 4, Upd: 4, HotUpd: 2, Del: 4}), 1e-9, "4 + 2 + 4")
}

func TestIDXS015_LowSelectivityLowersVerdict(t *testing.T) {
	require.Equal(t, VerdictModerate, verdictFor(50, true))
	require.Equal(t, VerdictWriteHeavy, verdictFor(5, true))
	require.Equal(t, VerdictWriteHeavy, verdictFor(0.5, true))
	meta := partnerMeta()
	in := input(`SELECT 1 FROM res_partner WHERE active = $1`)
	in.Statements[0].CallsRate = 1000
	in.Tables[partner] = meta
	s := Suggest(in).Suggestions[0]
	require.Equal(t, "low", s.Selectivity.Label)
	require.Equal(t, VerdictModerate, s.Verdict)
	require.Contains(t, s.Notes, NoteLowSelectivity)
}

func TestIDXS015_SelectivityLabels(t *testing.T) {
	meta := TableMeta{RelTuples: 10000, Columns: map[string]Column{
		"hi": {NDistinct: nd(1000)}, "lo": {NDistinct: nd(20)}, "mid": {NDistinct: nd(21)}, "frac": {NDistinct: nd(-0.1)},
		"fraclo": {NDistinct: nd(-0.0005)}, "fracmid": {NDistinct: nd(-0.05)}, "zero": {NDistinct: nd(0)}, "none": {},
	}}
	want := map[string]string{"hi": "high", "lo": "low", "mid": "medium", "frac": "high", "fraclo": "low", "fracmid": "medium", "zero": "unknown", "none": "unknown", "absent": "unknown"}
	for column, label := range want {
		require.Equal(t, label, selectivityOf(meta, true, column).Label, column)
	}
	require.Equal(t, "unknown", selectivityOf(meta, false, "hi").Label)
}

func TestIDXS016_QuoteIdent(t *testing.T) {
	cases := map[string]string{
		"res_partner": "res_partner", "Order": `"Order"`, `a"b`: `"a""b"`, "order": `"order"`, "user": `"user"`,
		"1abc": `"1abc"`, "a-b": `"a-b"`, "": `""`, "_x1": "_x1", "ünï": `"ünï"`,
	}
	for in, want := range cases {
		require.Equal(t, want, quoteIdent(in), in)
	}
}

func TestIDXS017_NameTruncation(t *testing.T) {
	short := indexName("res_partner", []KeyColumn{{Name: "ref"}})
	require.Equal(t, "idx_res_partner_ref", short)
	long := indexName(strings.Repeat("t", 40), []KeyColumn{{Name: strings.Repeat("c", 30)}, {Name: "d"}})
	require.Len(t, long, 63)
	require.Equal(t, long, indexName(strings.Repeat("t", 40), []KeyColumn{{Name: strings.Repeat("c", 30)}, {Name: "d"}}))
	other := indexName(strings.Repeat("t", 40), []KeyColumn{{Name: strings.Repeat("c", 30)}, {Name: "e"}})
	require.NotEqual(t, long, other, "the hash separates names that share a prefix")
	require.Equal(t, long[:56], other[:56])
	multi := indexName(strings.Repeat("é", 40), []KeyColumn{{Name: "a"}})
	require.LessOrEqual(t, len(multi), 63)
	require.True(t, strings.ToValidUTF8(multi, "") == multi)
}

func TestIDXS017_CreateStatement(t *testing.T) {
	got := createStatement(Key{"d", "My Schema", "Order"}, []KeyColumn{{Name: "user"}, {Name: "id", Desc: true}})
	require.Equal(t, `CREATE INDEX CONCURRENTLY "idx_Order_user_id" ON "My Schema"."Order" ("user", id DESC);`, got)
}
