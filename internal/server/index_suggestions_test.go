package server

import (
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/indexadvice"
)

var updateIndexSuggestionsGolden = flag.Bool("update-idxe", false, "rewrite testdata/index_suggestions_golden.json")

type stubIndexSuggestions struct {
	mu       sync.Mutex
	known    map[uuid.UUID]indexSuggestionData
	err      error
	window   time.Duration
	database string
}

func (s *stubIndexSuggestions) IndexSuggestionData(_ context.Context, id uuid.UUID, window time.Duration, database string) (indexSuggestionData, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.window, s.database = window, database
	if s.err != nil {
		return indexSuggestionData{}, false, s.err
	}
	data, ok := s.known[id]
	return data, ok, nil
}

func suggestionsFixture() indexSuggestionData {
	key := indexadvice.Key{Database: "odoo", Schema: "public", Table: "res_partner"}
	nd := -0.8
	return indexSuggestionData{
		Statements: []indexadvice.Stmt{
			{Database: "odoo", QueryID: 11, Text: `SELECT id FROM res_partner WHERE ref = $1`, CallsRate: 12, ExecMsRate: 40},
			{Database: "odoo", QueryID: 12, Text: `SELECT id FROM res_partner WHERE name = $1`, CallsRate: 5, ExecMsRate: 30},
		},
		TotalExecMsRate: map[string]float64{"odoo": 100},
		Tables: map[indexadvice.Key]indexadvice.TableMeta{key: {
			RelTuples: 50000, LiveTuples: 50000, HasCounts: true, HasWrites: true,
			Columns: map[string]indexadvice.Column{
				"ref": {Type: "character varying", AvgWidth: 12, NDistinct: &nd}, "name": {Type: "character varying", AvgWidth: 20},
			},
			Writes: indexadvice.Writes{Ins: 0.5, Upd: 1, Del: 0.1, HotUpd: 0.5},
		}},
		Indexes:     map[indexadvice.Key][]indexadvice.IndexDef{key: {{Name: "res_partner_name_idx", Table: "res_partner", Method: "btree", Columns: []indexadvice.IndexColumn{{Name: "name"}}}}},
		ColumnFacts: true,
	}
}

func TestIDXE001_ParameterValidation(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	st := &stubIndexSuggestions{known: map[uuid.UUID]indexSuggestionData{id: {}}}
	for _, query := range []string{
		"?window=0", "?window=abc", "?window=59m", "?window=8d", "?window=-1h", "?window=xd",
		"?limit=0", "?limit=101", "?limit=abc", "?limit=-3",
	} {
		rec := retentionRequest(t, &API{indexSuggestions: st}, http.MethodGet, "/api/v1/instances/"+id.String()+"/index-suggestions"+query, "")
		require.Equal(t, http.StatusBadRequest, rec.Code, query)
	}
	require.Zero(t, st.window, "a rejected request never reaches the store")
	for query, want := range map[string]time.Duration{
		"": 24 * time.Hour, "?window=1h": time.Hour, "?window=90m": 90 * time.Minute, "?window=3d": 72 * time.Hour, "?window=7d": 7 * 24 * time.Hour, "?window=168h": 7 * 24 * time.Hour,
	} {
		rec := retentionRequest(t, &API{indexSuggestions: st}, http.MethodGet, "/api/v1/instances/"+id.String()+"/index-suggestions"+query, "")
		require.Equal(t, http.StatusOK, rec.Code, query)
		require.Equal(t, want, st.window, query)
	}
	rec := retentionRequest(t, &API{indexSuggestions: st}, http.MethodGet, "/api/v1/instances/"+id.String()+"/index-suggestions?limit=100", "")
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestIDXE002_UnknownInstance(t *testing.T) {
	t.Parallel()
	st := &stubIndexSuggestions{known: map[uuid.UUID]indexSuggestionData{}}
	api := &API{indexSuggestions: st}
	requireAPIError(t, retentionRequest(t, api, http.MethodGet, "/api/v1/instances/"+uuid.NewString()+"/index-suggestions", ""), http.StatusNotFound, "instance not found")
	requireAPIError(t, retentionRequest(t, api, http.MethodGet, "/api/v1/instances/not-a-uuid/index-suggestions", ""), http.StatusBadRequest, "invalid instance_id")
	requireAPIError(t, retentionRequest(t, NewAPI(nil), http.MethodGet, "/api/v1/instances/"+uuid.NewString()+"/index-suggestions", ""), http.StatusNotFound, "instance not found")
	failing := &API{indexSuggestions: &stubIndexSuggestions{err: context.Canceled}}
	requireAPIError(t, retentionRequest(t, failing, http.MethodGet, "/api/v1/instances/"+uuid.NewString()+"/index-suggestions", ""), http.StatusInternalServerError, "index suggestions query failed")
}

func TestIDXE003_EmptyInstance(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	st := &stubIndexSuggestions{known: map[uuid.UUID]indexSuggestionData{id: {}}}
	rec := retentionRequest(t, &API{indexSuggestions: st}, http.MethodGet, "/api/v1/instances/"+id.String()+"/index-suggestions", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
	require.JSONEq(t, `[]`, string(raw["suggestions"]))
	var got indexSuggestionsJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, 0, got.Coverage.StatementsConsidered)
	require.Equal(t, 86400, got.WindowSeconds)
	require.Equal(t, id.String(), got.InstanceID)
	require.Len(t, got.Notes, 1)
	require.Contains(t, got.Notes[0], "pg_stat_statements")
	require.NotEmpty(t, got.EstimatesNote)
}

// IDXE-004: stub data gives exactly one suggestion, compared with a golden file.
func TestIDXE004_GoldenJSON(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("00000000-0000-4000-8000-0000000000a1")
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	body, err := json.MarshalIndent(buildIndexSuggestions(id, 24*time.Hour, 20, suggestionsFixture(), now), "", "  ")
	require.NoError(t, err)
	const path = "testdata/index_suggestions_golden.json"
	if *updateIndexSuggestionsGolden {
		require.NoError(t, os.WriteFile(path, append(body, '\n'), 0o644))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	require.JSONEq(t, string(want), string(body))

	var got indexSuggestionsJSON
	require.NoError(t, json.Unmarshal(body, &got))
	require.Len(t, got.Suggestions, 1)
	s := got.Suggestions[0]
	require.Equal(t, "ref", columnsLine(s))
	require.InDelta(t, 40, s.WeightPct, 1e-9)
}

func columnsLine(s indexadvice.Suggestion) string {
	names := make([]string, len(s.Columns))
	for i, c := range s.Columns {
		names[i] = c.Name
	}
	return strings.Join(names, ",")
}

func TestIDXE005_DatabaseFilterReachesTheStore(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	st := &stubIndexSuggestions{known: map[uuid.UUID]indexSuggestionData{id: suggestionsFixture()}}
	rec := retentionRequest(t, &API{indexSuggestions: st}, http.MethodGet, "/api/v1/instances/"+id.String()+"/index-suggestions?database=odoo%27%3B+DROP+TABLE+x%3B--", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "odoo'; DROP TABLE x;--", st.database, "passed through as a bound parameter, never into SQL text")
	for _, sql := range []string{indexSuggestionInstanceSQL, indexSuggestionStatementsSQL, indexSuggestionIndexesSQL, indexSuggestionColumnsSQL, indexSuggestionTablesSQL} {
		require.NotContains(t, sql, "%s")
		require.NotContains(t, sql, "Sprintf")
	}
}

func TestIDXE006_CreateStatementQuoting(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	data := suggestionsFixture()
	data.Statements = []indexadvice.Stmt{{Database: "odoo", QueryID: 1, Text: `SELECT 1 FROM "Order" WHERE "user" = $1`, CallsRate: 1, ExecMsRate: 10}}
	data.Tables = map[indexadvice.Key]indexadvice.TableMeta{}
	st := &stubIndexSuggestions{known: map[uuid.UUID]indexSuggestionData{id: data}}
	rec := retentionRequest(t, &API{indexSuggestions: st}, http.MethodGet, "/api/v1/instances/"+id.String()+"/index-suggestions", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var got indexSuggestionsJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Suggestions, 1)
	require.Equal(t, `CREATE INDEX CONCURRENTLY "idx_Order_user" ON public."Order" ("user");`, got.Suggestions[0].CreateStatement)
}

func TestIDXE007_TimeoutIs503(t *testing.T) {
	t.Parallel()
	failing := &API{indexSuggestions: &stubIndexSuggestions{err: context.DeadlineExceeded}}
	requireAPIError(t, retentionRequest(t, failing, http.MethodGet, "/api/v1/instances/"+uuid.NewString()+"/index-suggestions", ""), http.StatusServiceUnavailable, "analysis timed out")
}

func TestIDXE008_NoTextAndMissingColumns(t *testing.T) {
	t.Parallel()
	data := suggestionsFixture()
	data.NoText = 3
	data.ColumnFacts = false
	got := buildIndexSuggestions(uuid.New(), time.Hour, 20, data, time.Now())
	require.Equal(t, 5, got.Coverage.StatementsConsidered)
	require.Equal(t, 3, got.Coverage.Skipped[skipNoText])
	require.Len(t, got.Notes, 1)
	require.Contains(t, got.Notes[0], "table_columns")
}
