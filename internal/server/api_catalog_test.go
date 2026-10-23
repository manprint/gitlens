package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// catalogFactRow is one row of latestCatalogFacts' query, in its column order.
func catalogFactRow(datname, key, valueText string, labels map[string]string, seen time.Time) []any {
	rawLabels, _ := json.Marshal(labels)
	return []any{datname, key, valueText, []byte(nil), rawLabels, seen, seen, seen}
}

// catalogRoutes are the catalog endpoints that read one fact kind and the
// databases whose latest result of the check was cut, in that order.
var catalogRoutes = []struct {
	path      string
	listField string
}{
	{"triggers", "triggers"},
	{"unindexed-foreign-keys", "foreign_keys"},
	{"extensions", "extensions"},
	{"spatial-columns", "columns"},
	{"sequences", "sequences"},
	{"partitioned-tables", "tables"},
}

func serveCatalog(t *testing.T, pool dbPool, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	api := &API{}
	if pool != nil {
		api.pool = pool
	}
	api.registerCatalogRoutes(r)
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, path, http.NoBody))
	return resp
}

// Each catalog endpoint lists the facts of its kind and names the databases
// whose list was cut, and every label the agent sends reaches its field.
func TestCatalogAPI_ListsFactsAndTruncatedDatabases(t *testing.T) {
	seen := time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC)
	labels := map[string]string{
		"schemaname": "public", "relname": "orders", "trigger": "audit", "kind": "user", "function": "audit_fn",
		"timing": "AFTER", "events": "INSERT", "level": "ROW", "constraint": "orders_customer_fk",
		"constraint_type": "f", "table_bytes": "8192", "referenced": "customers", "schema": "public",
		"default_version": "3.5", "upgrade": "true", "column": "geom", "subtype": "Point", "index_method": "gist",
		"srid": "4326", "srid_known": "true", "rows": "120", "sequence": "orders_id_seq", "data_type": "integer",
		"start": "1", "increment": "1", "min": "1", "max": "2147483647", "cycle": "false", "last_value": "42",
		"limit": "2147483647", "limit_source": "column", "exhausts": "false", "remaining": "2147483605",
		"table_schema": "public", "table": "orders", "column_type": "integer", "link": "owned",
		"used_ratio": "0.00002", "columns": "1", "partition_key": "RANGE (created_at)", "partitions": "3",
		"direct_partitions": "3", "depth": "1", "bytes": "24576", "default_partition": "orders_default",
		"default_rows": "5", "invalid_indexes": "orders_p1_idx,orders_p2_idx", "analyzed_at": seen.Format(time.RFC3339),
		"covered_until": "2026-11-01",
	}
	for _, route := range catalogRoutes {
		t.Run(route.path, func(t *testing.T) {
			id := uuid.New()
			pool := &mockPool{queryResults: []*mockRows{
				{rows: [][]any{catalogFactRow("app", "public.orders", "enabled", labels, seen)}},
				{rows: [][]any{{"app"}}},
			}}
			resp := serveCatalog(t, pool, "/api/v1/instances/"+id.String()+"/"+route.path+"?datname=app")
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

			var got map[string]any
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
			require.Equal(t, id.String(), got["instance_id"])
			require.Equal(t, []any{"app"}, got["truncated_databases"])
			list, ok := got[route.listField].([]any)
			require.True(t, ok, "missing %s in %s", route.listField, resp.Body.String())
			require.Len(t, list, 1)
			require.Equal(t, "app", list[0].(map[string]any)["datname"])
		})
	}
}

// A partitioned table without a default partition, invalid indexes or an
// analyze time leaves those fields empty rather than inventing them.
func TestCatalogAPI_PartitionedTableOptionalFields(t *testing.T) {
	seen := time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC)
	pool := &mockPool{queryResults: []*mockRows{
		{rows: [][]any{catalogFactRow("app", "public.events", "range", map[string]string{"relname": "events", "partitions": "2"}, seen)}},
	}}
	resp := serveCatalog(t, pool, "/api/v1/instances/"+uuid.NewString()+"/partitioned-tables")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	var got partitionedTablesAPIResponse
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	require.Len(t, got.Tables, 1)
	table := got.Tables[0]
	require.Equal(t, 2, table.Partitions)
	require.Nil(t, table.DefaultPartition)
	require.Nil(t, table.DefaultRows)
	require.Nil(t, table.AnalyzedAt)
	require.Nil(t, table.CoveredUntil)
	require.Empty(t, table.InvalidIndexes)
	require.Empty(t, got.TruncatedDatabases)
}

// A bad instance id is refused, a server without a store answers an empty
// list, and a failed read of the facts or of the cut databases is an error,
// never a partial list presented as complete.
func TestCatalogAPI_Errors(t *testing.T) {
	for _, route := range catalogRoutes {
		t.Run(route.path, func(t *testing.T) {
			resp := serveCatalog(t, &mockPool{}, "/api/v1/instances/not-a-uuid/"+route.path)
			require.Equal(t, http.StatusBadRequest, resp.Code)

			path := "/api/v1/instances/" + uuid.NewString() + "/" + route.path
			resp = serveCatalog(t, nil, path)
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
			var got map[string]any
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
			require.Equal(t, []any{}, got[route.listField])

			resp = serveCatalog(t, &mockPool{queryErrs: []error{errors.New("boom")}}, path)
			require.Equal(t, http.StatusInternalServerError, resp.Code)

			resp = serveCatalog(t, &mockPool{queryErrs: []error{nil, errors.New("boom")}}, path)
			require.Equal(t, http.StatusInternalServerError, resp.Code)

			resp = serveCatalog(t, &mockPool{queryResults: []*mockRows{{err: errors.New("reset")}}}, path)
			require.Equal(t, http.StatusInternalServerError, resp.Code)

			resp = serveCatalog(t, &mockPool{queryResults: []*mockRows{{rows: [][]any{{"too few columns"}}}}}, path)
			require.Equal(t, http.StatusInternalServerError, resp.Code)

			resp = serveCatalog(t, &mockPool{queryResults: []*mockRows{nil, {rows: [][]any{{1, 2}}}}}, path)
			require.Equal(t, http.StatusInternalServerError, resp.Code)
		})
	}
}

// indexLastScan tells an index never scanned since the reset from one whose
// last scan the server cannot know, and parses the time of a scanned one.
func TestIndexLastScan(t *testing.T) {
	at, status := indexLastScan(nil)
	require.Nil(t, at)
	require.Equal(t, "not_available", status)

	empty := ""
	at, status = indexLastScan(&empty)
	require.Nil(t, at)
	require.Equal(t, "never_since_reset", status)

	garbled := "yesterday"
	at, status = indexLastScan(&garbled)
	require.Nil(t, at)
	require.Equal(t, "not_available", status)

	scanned := "2026-10-24T08:00:00.123456Z"
	at, status = indexLastScan(&scanned)
	require.Equal(t, "scanned", status)
	require.NotNil(t, at)
	require.Equal(t, time.Date(2026, 10, 24, 8, 0, 0, 123456000, time.UTC), at.UTC())
}

// ageToTime turns an age back into the time it was measured from, at the
// microsecond precision timestamptz keeps.
func TestAgeToTime(t *testing.T) {
	ts := time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC)
	require.Equal(t, ts.Add(-90250*time.Millisecond), *ageToTime(ts, 90.25))
	// 400ns past a whole second is cut to the microsecond below it.
	require.Equal(t, ts.Add(-90*time.Second-time.Microsecond), *ageToTime(ts, 90.0000004))
}
