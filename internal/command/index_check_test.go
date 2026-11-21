package command

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/pgtype"
)

func indexCheckArgs() Args {
	q := int64(77)
	return Args{QueryID: &q, Datname: "app", IndexSchema: "public", IndexRelation: "orders", IndexColumns: []string{"customer_id", "created_at"}, IndexInclude: []string{"total"}}
}

// TestIDXV001: valid index_check arguments pass; each rule fails with a
// message naming the field.
func TestIDXV001(t *testing.T) {
	require.NoError(t, indexCheckArgs().Validate(KindIndexCheck))
	noInclude := indexCheckArgs()
	noInclude.IndexInclude = nil
	require.NoError(t, noInclude.Validate(KindIndexCheck))

	long := strings.Repeat("x", 64)
	many := make([]string, 33)
	for i := range many {
		many[i] = "c" + strings.Repeat("x", i)
	}
	pid := 1
	started := time.Now()
	value := "1"
	cases := []struct {
		name    string
		mutate  func(*Args)
		wantErr string
	}{
		{"no queryid", func(a *Args) { a.QueryID = nil }, "queryid"},
		{"no datname", func(a *Args) { a.Datname = "" }, "datname"},
		{"empty schema", func(a *Args) { a.IndexSchema = "" }, "index_schema"},
		{"long schema", func(a *Args) { a.IndexSchema = long }, "index_schema"},
		{"empty relation", func(a *Args) { a.IndexRelation = "" }, "index_relation"},
		{"long relation", func(a *Args) { a.IndexRelation = long }, "index_relation"},
		{"no columns", func(a *Args) { a.IndexColumns = nil }, "index_columns"},
		{"too many columns", func(a *Args) { a.IndexColumns = many }, "index_columns"},
		{"too many include", func(a *Args) { a.IndexInclude = many }, "index_include"},
		{"long column", func(a *Args) { a.IndexColumns = []string{long} }, "index_columns"},
		{"empty include", func(a *Args) { a.IndexInclude = []string{""} }, "index_include"},
		{"NUL in column", func(a *Args) { a.IndexColumns = []string{"a\x00b"} }, "index_columns"},
		{"control in include", func(a *Args) { a.IndexInclude = []string{"a\nb"} }, "index_include"},
		{"control in schema", func(a *Args) { a.IndexSchema = "pub\tlic" }, "index_schema"},
		{"column twice", func(a *Args) { a.IndexColumns = []string{"a", "a"} }, "index_columns"},
		{"column also included", func(a *Args) { a.IndexInclude = []string{"customer_id"} }, "index_include"},
		{"pid", func(a *Args) { a.PID = &pid }, "unrelated"},
		{"analyze", func(a *Args) { a.Analyze = true }, "unrelated"},
		{"params", func(a *Args) { a.Params = []*string{&value} }, "unrelated"},
		{"backend_start", func(a *Args) { a.BackendStart = &started }, "unrelated"},
		{"pgstattuple schema", func(a *Args) { a.Schema = "public" }, "unrelated"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := indexCheckArgs()
			tc.mutate(&a)
			err := a.Validate(KindIndexCheck)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}

	// The index fields on another kind are an error.
	q := int64(7)
	for _, k := range []Kind{KindExplain, KindPgstattuple, KindCancel} {
		a := Args{QueryID: &q, Schema: "public", Relation: "t", PID: &pid, IndexColumns: []string{"a"}}
		switch k {
		case KindExplain:
			a = Args{QueryID: &q, IndexSchema: "public"}
		case KindPgstattuple:
			a = Args{Schema: "public", Relation: "t", IndexInclude: []string{"a"}}
		case KindCancel:
			a = Args{PID: &pid, IndexRelation: "t"}
		}
		err := a.Validate(k)
		require.Error(t, err, k)
		require.Contains(t, err.Error(), "index_schema, index_relation, index_columns and index_include")
	}
}

// TestIDXV002: no field of the kind carries SQL. A body with a statement or
// an index definition is refused before it reaches Args, and a column name
// that looks like SQL stays one intact identifier.
func TestIDXV002(t *testing.T) {
	base := `{"queryid":"77","datname":"app","index_schema":"public","index_relation":"orders","index_columns":["customer_id"]}`
	var want Args
	require.NoError(t, json.Unmarshal([]byte(base), &want))
	require.Equal(t, []string{"customer_id"}, want.IndexColumns)
	for _, key := range []string{"sql", "statement", "index_definition"} {
		body := strings.TrimSuffix(base, "}") + `,"` + key + `":"CREATE INDEX ON orders (customer_id)"}`
		var got Args
		require.Error(t, json.Unmarshal([]byte(body), &got), key)
		require.Equal(t, Args{}, got, key)
	}

	evil := "id); DROP TABLE x; --"
	a := indexCheckArgs()
	a.IndexColumns = []string{evil}
	require.NoError(t, a.Validate(KindIndexCheck))
	require.Equal(t, []string{evil}, a.IndexColumns)
	require.Less(t, len(evil), 64)

	data, err := json.Marshal(a)
	require.NoError(t, err)
	var round Args
	require.NoError(t, json.Unmarshal(data, &round))
	require.Equal(t, a, round)
}

// TestIDXV003: index_check needs tier T1, like a plain EXPLAIN.
func TestIDXV003(t *testing.T) {
	ok, reason := Allowed(KindIndexCheck, indexCheckArgs(), Gates{Tier: pgtype.TierExplain - 1})
	require.False(t, ok)
	require.Equal(t, "permission tier T1 is required", reason)
	ok, _ = Allowed(KindIndexCheck, indexCheckArgs(), Gates{Tier: pgtype.TierExplain})
	require.True(t, ok)
}
