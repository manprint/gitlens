package server

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

type sessapiResponse struct {
	Sessions    []map[string]any `json:"sessions"`
	Connections *struct {
		ByApplicationEnabled bool              `json:"by_application_enabled"`
		Truncated            bool              `json:"truncated"`
		ByOrigin             []connectionCount `json:"by_origin"`
		ByApplication        []connectionCount `json:"by_application"`
		ByUser               []connectionCount `json:"by_user"`
		ByClient             []connectionCount `json:"by_client"`
		ByDatabase           []connectionCount `json:"by_database"`
		ByState              []connectionCount `json:"by_state"`
		Totals               *connectionGroups `json:"totals"`
	} `json:"connections"`
}

// sessapiTree is a stored lock tree holding the given sessions.
func sessapiTree(t *testing.T, truncated bool, sessions ...map[string]any) []byte {
	t.Helper()
	tree := map[string]any{"nodes": []any{}, "sessions": sessions}
	if truncated {
		tree["sessions_truncated"] = true
	}
	raw, err := json.Marshal(tree)
	require.NoError(t, err)
	return raw
}

// sessapiGet serves /api/v1/locks from a stub pool. The pool answers the
// QueryRow calls in the order the handler issues them: the snapshot, the
// monitoring role, the agent flags, the per-application metric.
func sessapiGet(t *testing.T, pool *searchCapturePool) (sessapiResponse, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/locks?instance_id="+uuid.NewString(), http.NoBody)
	rec := httptest.NewRecorder()
	contentionTestRouter(&API{pool: pool}).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got sessapiResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	return got, rec.Body.String()
}

func sessapiPool(tree []byte, extraRows ...*mockRow) *searchCapturePool {
	rows := append([]*mockRow{{vals: []any{time.Now(), tree}}}, extraRows...)
	return &searchCapturePool{mockPool: &mockPool{queryRowVals: rows}}
}

func counts(list []connectionCount) map[string]int64 {
	out := map[string]int64{}
	for _, c := range list {
		out[c.Key] = c.Count
	}
	return out
}

// SESSAPI-001: each session carries its origin.
func TestSESSAPI001_EverySessionHasOrigin(t *testing.T) {
	pool := sessapiPool(sessapiTree(t, false,
		map[string]any{"pid": 1, "application_name": "billing", "usename": "app"},
		map[string]any{"pid": 2, "application_name": "pglens/check", "usename": "app"},
		map[string]any{"pid": 3, "application_name": "pglens-agent/x", "usename": "app"},
		map[string]any{"pid": 4, "application_name": "pglens_x", "usename": "app"},
		map[string]any{"pid": 5, "usename": "app"},
	))
	got, _ := sessapiGet(t, pool)
	var origins []any
	for _, s := range got.Sessions {
		origins = append(origins, s["origin"])
	}
	require.Equal(t, []any{"application", "pglens", "pglens", "application", "application"}, origins)
}

// SESSAPI-002: the role label of the monitoring_role fact classifies the
// sessions of that role; without the fact only the application name counts.
func TestSESSAPI002_MonitoringRoleLabelClassifies(t *testing.T) {
	sessions := []map[string]any{
		{"pid": 1, "application_name": "psql", "usename": "pglens_monitor"},
		{"pid": 2, "application_name": "psql", "usename": "alice"},
		{"pid": 3, "application_name": "", "usename": ""},
	}
	// The fact as the agent emits it: labels role, read_only_default,
	// has_pg_monitor, has_read_all_stats, value text read-only. Only the role
	// label is read.
	pool := sessapiPool(sessapiTree(t, false, sessions...), &mockRow{vals: []any{"pglens_monitor"}})
	got, _ := sessapiGet(t, pool)
	require.Equal(t, "pglens", got.Sessions[0]["origin"])
	require.Equal(t, "application", got.Sessions[1]["origin"])
	require.Equal(t, "application", got.Sessions[2]["origin"], "an empty role never matches an empty usename")
	require.Contains(t, pool.sqls[1], "kind='monitoring_role'")
	require.Contains(t, pool.sqls[1], "labels->>'role'")
	require.Equal(t, "self", pool.args[1][1])

	// No fact: the same user is an application session.
	pool = sessapiPool(sessapiTree(t, false, sessions...))
	got, _ = sessapiGet(t, pool)
	require.Equal(t, "application", got.Sessions[0]["origin"])

	// A fact whose role label is NULL behaves as no fact.
	pool = sessapiPool(sessapiTree(t, false, sessions...), &mockRow{vals: []any{nil}})
	got, _ = sessapiGet(t, pool)
	require.Equal(t, "application", got.Sessions[0]["origin"])
}

// SESSAPI-003: the normalised text is resolved by query_id with one query over
// the distinct ids and carries its source.
func TestSESSAPI003_NormalisedTextResolved(t *testing.T) {
	pool := sessapiPool(sessapiTree(t, false,
		map[string]any{"pid": 1, "datname": "app", "query_id": "-42"},
		map[string]any{"pid": 2, "datname": "app", "query_id": "-42"},
		map[string]any{"pid": 3, "datname": "other", "query_id": "7"},
	))
	pool.queryResults = []*mockRows{{rows: [][]any{
		{"app", int64(-42), "SELECT * FROM t WHERE id = $1"},
	}}}
	got, _ := sessapiGet(t, pool)
	for _, i := range []int{0, 1} {
		require.Equal(t, "SELECT * FROM t WHERE id = $1", got.Sessions[i]["query_text"])
		require.Equal(t, "normalised", got.Sessions[i]["query_text_source"])
	}
	require.NotContains(t, got.Sessions[2], "query_text")
	require.Equal(t, 1, pool.queryCalls, "one lookup for the whole sample")
	var args []any
	for i, sql := range pool.sqls {
		if strings.Contains(sql, "FROM query_texts") {
			args = pool.args[i]
		}
	}
	require.NotNil(t, args, "the lookup query ran")
	require.Equal(t, []string{"app", "other"}, args[1], "distinct pairs only")
	require.Equal(t, []int64{-42, 7}, args[2])
}

// A text that may hold a credential is withheld, not shown.
func TestSESSAPI003_NormalisedTextWithheldWhenCredentialLike(t *testing.T) {
	pool := sessapiPool(sessapiTree(t, false, map[string]any{"pid": 1, "datname": "app", "query_id": "5"}))
	pool.queryResults = []*mockRows{{rows: [][]any{{"app", int64(5), "ALTER ROLE x PASSWORD 'hunter2'"}}}}
	got, body := sessapiGet(t, pool)
	require.Equal(t, "normalised", got.Sessions[0]["query_text_source"])
	require.NotContains(t, body, "hunter2")
}

// SESSAPI-004: no text, and no source, when the id is unknown and the agent
// sent no raw text; a session with no id issues no lookup at all.
func TestSESSAPI004_NoTextWhenUnknown(t *testing.T) {
	pool := sessapiPool(sessapiTree(t, false,
		map[string]any{"pid": 1, "datname": "app", "query_id": "99"},
		map[string]any{"pid": 2, "datname": "app"},
		map[string]any{"pid": 3, "datname": "app", "query_id": "not-a-number"},
	))
	got, _ := sessapiGet(t, pool)
	for _, s := range got.Sessions {
		require.NotContains(t, s, "query_text")
		require.NotContains(t, s, "query_text_source")
	}
	require.Equal(t, 1, pool.queryCalls)

	pool = sessapiPool(sessapiTree(t, false, map[string]any{"pid": 2, "datname": "app"}))
	sessapiGet(t, pool)
	require.Zero(t, pool.queryCalls, "no id, no lookup")
}

// SESSAPI-005: raw text passes through when present and is never mixed with
// the normalised one, even when a normalised text exists for the same id.
func TestSESSAPI005_RawTextNeverMixed(t *testing.T) {
	pool := sessapiPool(sessapiTree(t, false,
		map[string]any{"pid": 1, "datname": "app", "query_id": "5", "query": "SELECT 1 /* raw */"},
		map[string]any{"pid": 2, "datname": "app", "query_id": "5"},
	))
	pool.queryResults = []*mockRows{{rows: [][]any{{"app", int64(5), "SELECT $1"}}}}
	got, _ := sessapiGet(t, pool)
	require.Equal(t, "SELECT 1 /* raw */", got.Sessions[0]["query_text"])
	require.Equal(t, "raw", got.Sessions[0]["query_text_source"])
	require.Equal(t, "SELECT $1", got.Sessions[1]["query_text"])
	require.Equal(t, "normalised", got.Sessions[1]["query_text_source"])

	// A raw-only sample issues no normalised lookup.
	pool = sessapiPool(sessapiTree(t, false, map[string]any{"pid": 1, "datname": "app", "query_id": "5", "query": "SELECT 2"}))
	got, _ = sessapiGet(t, pool)
	require.Equal(t, "raw", got.Sessions[0]["query_text_source"])
	require.Zero(t, pool.queryCalls)
}

// The server owns origin, query_text and query_text_source: a stored session
// cannot choose them.
func TestSESSAPI005_StoredFieldsOfTheServerAreOverwritten(t *testing.T) {
	pool := sessapiPool(sessapiTree(t, false, map[string]any{
		"pid": 1, "application_name": "app", "origin": "pglens", "query_text": "forged", "query_text_source": "raw",
	}))
	got, _ := sessapiGet(t, pool)
	require.Equal(t, "application", got.Sessions[0]["origin"])
	require.NotContains(t, got.Sessions[0], "query_text")
	require.NotContains(t, got.Sessions[0], "query_text_source")
}

// SESSAPI-006: the summary is counted from the sample, so its sums equal the
// session counts (I-15); lists are sorted by count then key.
func TestSESSAPI006_ConnectionsSumToTheSample(t *testing.T) {
	pool := sessapiPool(sessapiTree(t, false,
		map[string]any{"pid": 1, "application_name": "web", "usename": "app", "datname": "d1", "client_addr": "10.0.0.1", "state": "active"},
		map[string]any{"pid": 2, "application_name": "web", "usename": "app", "datname": "d1", "client_addr": "10.0.0.1", "state": "idle"},
		map[string]any{"pid": 3, "application_name": "batch", "usename": "app", "datname": "d2", "client_addr": "10.0.0.2", "state": "idle"},
		map[string]any{"pid": 4, "application_name": "pglens/check", "usename": "mon", "datname": "d1", "client_addr": "", "state": "active"},
		map[string]any{"pid": 5, "application_name": "", "usename": "", "datname": "", "client_addr": "10.0.0.2"},
	))
	got, _ := sessapiGet(t, pool)
	c := got.Connections
	require.NotNil(t, c)
	for name, list := range map[string][]connectionCount{
		"by_origin": c.ByOrigin, "by_application": c.ByApplication, "by_user": c.ByUser,
		"by_client": c.ByClient, "by_database": c.ByDatabase, "by_state": c.ByState,
	} {
		var sum int64
		for _, e := range list {
			sum += e.Count
		}
		require.Equal(t, int64(len(got.Sessions)), sum, name)
	}
	require.Equal(t, map[string]int64{"application": 4, "pglens": 1}, counts(c.ByOrigin))
	require.Equal(t, []connectionCount{{"web", 2}, {"", 1}, {"batch", 1}, {"pglens/check", 1}}, c.ByApplication)
	require.Equal(t, int64(1), counts(c.ByClient)["local"], "an empty client address is a local client")
	require.Equal(t, int64(1), counts(c.ByState)["unknown"])
	// Each session is counted once in the two origins.
	require.Equal(t, counts(c.ByOrigin)["application"]+counts(c.ByOrigin)["pglens"], int64(len(got.Sessions)))
	require.False(t, c.Truncated)
	require.Nil(t, c.Totals)
}

// Both origins are listed even when a sample holds one of them only.
func TestSESSAPI006_BothOriginsAlwaysListed(t *testing.T) {
	pool := sessapiPool(sessapiTree(t, false, map[string]any{"pid": 1, "application_name": "web"}))
	got, _ := sessapiGet(t, pool)
	require.Equal(t, []connectionCount{{"application", 1}, {"pglens", 0}}, got.Connections.ByOrigin)

	pool = sessapiPool(sessapiTree(t, false))
	got, body := sessapiGet(t, pool)
	require.Equal(t, []connectionCount{{"application", 0}, {"pglens", 0}}, got.Connections.ByOrigin)
	require.Contains(t, body, `"by_user":[]`)
}

// SESSAPI-007: a truncated sample says so and carries the totals of the latest
// activity scrape; a complete one carries none.
func TestSESSAPI007_TruncationFlagAndTotals(t *testing.T) {
	pool := sessapiPool(sessapiTree(t, true, map[string]any{"pid": 1, "application_name": "web", "state": "active"}))
	pool.queryResults = []*mockRows{{rows: [][]any{
		{"pg_connections_by_origin", []byte(`{"origin":"application"}`), 240.0},
		{"pg_connections_by_origin", []byte(`{"origin":"pglens"}`), 4.0},
		{"pg_connections_by_application", []byte(`{"application_name":"web"}`), 200.0},
		{"pg_connections_by_application", []byte(`{"application_name":"other"}`), 44.0},
		{"pg_backends", []byte(`{"state":"active","wait_event_type":"CPU"}`), 3.0},
		{"pg_backends", []byte(`{"state":"active","wait_event_type":"Lock"}`), 2.0},
		{"pg_backends", []byte(`{"state":"idle","wait_event_type":"Client"}`), 239.0},
	}}}
	got, _ := sessapiGet(t, pool)
	c := got.Connections
	require.True(t, c.Truncated)
	require.NotNil(t, c.Totals)
	require.Equal(t, []connectionCount{{"application", 240}, {"pglens", 4}}, c.Totals.ByOrigin)
	require.Equal(t, []connectionCount{{"web", 200}, {"other", 44}}, c.Totals.ByApplication)
	require.Equal(t, []connectionCount{{"idle", 239}, {"active", 5}}, c.Totals.ByState)
	require.Empty(t, c.Totals.ByUser)
	require.Contains(t, pool.sqls[len(pool.sqls)-1], "FROM metrics")
	// The summary of the sample itself stays the sample's.
	require.Equal(t, map[string]int64{"application": 1, "pglens": 0}, counts(c.ByOrigin))
}

// SESSAPI-008: a sample of an older agent (no query_id, ages, text) answers
// without error, with the new fields omitted and the existing ones kept.
func TestSESSAPI008_OldAgentSample(t *testing.T) {
	pool := sessapiPool([]byte(`{"nodes":[],"sessions":[{"pid":7,"state":"active","xact_age":3.5},{"pid":8}]}`))
	got, body := sessapiGet(t, pool)
	require.Len(t, got.Sessions, 2)
	for _, s := range got.Sessions {
		require.Equal(t, "application", s["origin"])
		for _, absent := range []string{"query_text", "query_text_source", "query_id", "query_age", "leader_pid"} {
			require.NotContains(t, s, absent)
		}
	}
	require.Equal(t, 3.5, got.Sessions[0]["xact_age"])
	require.Equal(t, float64(7), got.Sessions[0]["pid"])
	require.NotContains(t, body, "null")
	require.Zero(t, pool.queryCalls)
}

// A session that is not an object is kept as it came and left out of the
// summary; the response is still an answer.
func TestSESSAPI008_NonObjectSessionKept(t *testing.T) {
	pool := sessapiPool([]byte(`{"sessions":[17,{"pid":1,"application_name":"web"}]}`))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/locks?instance_id="+uuid.NewString(), http.NoBody)
	rec := httptest.NewRecorder()
	contentionTestRouter(&API{pool: pool}).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"sessions":[17,`)
	var got sessapiResponse
	require.Error(t, json.Unmarshal(rec.Body.Bytes(), &got), "the non-object session is not decodable as a map")
	var generic struct {
		Connections struct {
			ByOrigin []connectionCount `json:"by_origin"`
		} `json:"connections"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &generic))
	require.Equal(t, int64(1), counts(generic.Connections.ByOrigin)["application"])
}

// by_application_enabled: agent_flags.by_application when the fact names it,
// otherwise whether the latest scrape carries per-application counts.
func TestSESSAPI009_ByApplicationEnabled(t *testing.T) {
	tree := sessapiTree(t, false, map[string]any{"pid": 1})
	role := &mockRow{err: nil, vals: []any{nil}}
	for name, tc := range map[string]struct {
		rows []*mockRow
		want bool
	}{
		"flag true":          {[]*mockRow{role, {vals: []any{[]byte(`{"by_application":true}`)}}}, true},
		"flag false":         {[]*mockRow{role, {vals: []any{[]byte(`{"by_application":false}`)}}}, false},
		"flag false wins":    {[]*mockRow{role, {vals: []any{[]byte(`{"by_application":false}`)}}, {vals: []any{true}}}, false},
		"no fact, metric":    {[]*mockRow{role, {err: pgx.ErrNoRows}, {vals: []any{true}}}, true},
		"no fact, no metric": {[]*mockRow{role, {err: pgx.ErrNoRows}, {vals: []any{false}}}, false},
		"fact w/o key":       {[]*mockRow{role, {vals: []any{[]byte(`{"allow_signal":true}`)}}, {vals: []any{true}}}, true},
		"fact unreadable":    {[]*mockRow{role, {vals: []any{[]byte(`[1]`)}}, {vals: []any{false}}}, false},
	} {
		got, _ := sessapiGet(t, sessapiPool(tree, tc.rows...))
		require.Equal(t, tc.want, got.Connections.ByApplicationEnabled, name)
	}
}

// A tree that was never sampled has no summary.
func TestSESSAPI009_NoTreeNoSummary(t *testing.T) {
	pool := &searchCapturePool{mockPool: &mockPool{}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/locks?instance_id="+uuid.NewString(), http.NoBody)
	rec := httptest.NewRecorder()
	contentionTestRouter(&API{pool: pool}).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), "connections")
}

// The activity endpoint reads the three new connection metrics.
func TestSESSAPI010_ActivityListsNewMetrics(t *testing.T) {
	pool := &searchCapturePool{mockPool: &mockPool{}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+uuid.NewString()+"/activity", http.NoBody)
	rec := httptest.NewRecorder()
	contentionTestRouter(&API{pool: pool}).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotEmpty(t, pool.sqls)
	for _, m := range []string{"pg_connections_by_origin", "pg_connections_by_user", "pg_connections_by_client"} {
		require.Contains(t, pool.sqls[0], "'"+m+"'")
	}
}

// SESSAPI-011: the SQL of api_locks.go is a string literal at every call of the
// pool, so no input reaches a statement by interpolation (I-3). Input travels as
// bound parameters only.
func TestSESSAPI011_SQLHasNoInterpolation(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "api_locks.go", nil, 0)
	require.NoError(t, err)
	var calls int
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != "Query" && sel.Sel.Name != "QueryRow" && sel.Sel.Name != "Exec") {
			return true
		}
		if inner, ok := sel.X.(*ast.SelectorExpr); !ok || inner.Sel.Name != "pool" {
			return true
		}
		calls++
		require.GreaterOrEqual(t, len(call.Args), 2, fset.Position(call.Pos()).String())
		lit, ok := call.Args[1].(*ast.BasicLit)
		require.True(t, ok, "%s: SQL must be one string literal, not built", fset.Position(call.Pos()))
		text, err := strconv.Unquote(lit.Value)
		require.NoError(t, err)
		require.NotContains(t, text, "%s")
		require.False(t, strings.Contains(text, "' +"), "%s", fset.Position(call.Pos()))
		return true
	})
	require.GreaterOrEqual(t, calls, 8, "the handler's queries were found")
}

// SESSAPI-012: blocking-tree nodes carry the same origin as sessions, an
// agent-sent origin never survives, and a tree with nodes only still reads the
// monitoring role.
func TestSESSAPI012_NodesCarryOrigin(t *testing.T) {
	tree, err := json.Marshal(map[string]any{
		"nodes": []any{
			map[string]any{"pid": 1, "application_name": "psql", "usename": "pglens_monitor", "origin": "application"},
			map[string]any{"pid": 2, "application_name": "billing", "usename": "alice"},
			map[string]any{"pid": 3, "application_name": "pglens/check", "usename": "alice"},
		},
		"sessions": []any{},
	})
	require.NoError(t, err)
	pool := sessapiPool(tree, &mockRow{vals: []any{"pglens_monitor"}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/locks?instance_id="+uuid.NewString(), http.NoBody)
	rec := httptest.NewRecorder()
	contentionTestRouter(&API{pool: pool}).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct {
		Nodes []map[string]any `json:"nodes"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Nodes, 3)
	require.Equal(t, "pglens", got.Nodes[0]["origin"])
	require.Equal(t, "application", got.Nodes[1]["origin"])
	require.Equal(t, "pglens", got.Nodes[2]["origin"])
}
