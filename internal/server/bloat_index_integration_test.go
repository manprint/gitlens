//go:build integration

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/wire"
)

// TestBloatIndex_IngestAndAPI: one bloat_estimate scrape carries a table row,
// an index row, the skipped-table gauge and the skipped-table fact. The store
// keeps the index row with its kind and the gauge as a metric, and the API
// returns the index row, the count and the named table. The object_kind
// filter returns only the index.
func TestBloatIndex_IngestAndAPI(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	instance := uuid.New()
	labels := func(relname, index, kind string) map[string]string {
		return map[string]string{"schemaname": "public", "relname": relname, "indexrelname": index, "object_kind": kind, "method": "estimate"}
	}
	fact, err := json.Marshal(wire.BloatSkippedValue{Count: 2, Tables: []wire.BloatSkippedTable{{SchemaName: "public", RelName: "secret"}}})
	require.NoError(t, err)
	p := NewPipeline(pool, clock.NewFake(now))
	res, err := p.Process(ctx, wire.Envelope{Instances: []wire.Instance{{InstanceID: instance.String(), ClusterID: pgtype.ClusterID(1).String(), Results: []wire.Result{{
		Check: "bloat_estimate", TS: now, Database: "app",
		Metrics: []wire.Metric{
			{Name: "pg_bloat_real_bytes", Value: 4 << 20, Kind: "gauge", Labels: labels("orders", "", "table")},
			{Name: "pg_bloat_bytes", Value: 1 << 20, Kind: "gauge", Labels: labels("orders", "", "table")},
			{Name: "pg_bloat_ratio", Value: 0.25, Kind: "gauge", Labels: labels("orders", "", "table")},
			{Name: "pg_bloat_real_bytes", Value: 2 << 20, Kind: "gauge", Labels: labels("orders", "orders_pkey", "index")},
			{Name: "pg_bloat_bytes", Value: 1800000, Kind: "gauge", Labels: labels("orders", "orders_pkey", "index")},
			{Name: "pg_bloat_ratio", Value: 0.9, Kind: "gauge", Labels: labels("orders", "orders_pkey", "index")},
			{Name: wire.BloatSkippedTablesMetric, Value: 2, Kind: "gauge", Labels: map[string]string{}},
		},
		Facts: []wire.Fact{{Kind: wire.BloatUnreadableFact, Key: "unreadable_tables", ValueJSON: fact}},
	}}}}})
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)

	var kinds []string
	rows, err := pool.Query(ctx, `SELECT object_kind || ':' || indexrelname FROM metrics_bloat WHERE instance_id=$1 ORDER BY 1`, instance)
	require.NoError(t, err)
	for rows.Next() {
		var k string
		require.NoError(t, rows.Scan(&k))
		kinds = append(kinds, k)
	}
	require.NoError(t, rows.Err())
	rows.Close()
	require.Equal(t, []string{"index:orders_pkey", "table:"}, kinds)

	var skipped float64
	require.NoError(t, pool.QueryRow(ctx, `SELECT value FROM metrics WHERE instance_id=$1 AND metric=$2 AND datname='app'`, instance, wire.BloatSkippedTablesMetric).Scan(&skipped))
	require.Equal(t, float64(2), skipped)

	get := func(path string) map[string]any {
		t.Helper()
		rec := httptest.NewRecorder()
		relationRouter(NewAPI(pool)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+instance.String()+"/bloat"+path, http.NoBody))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var payload map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
		return payload
	}
	all := get("")
	require.Len(t, all["items"], 2)
	require.Equal(t, float64(2), all["skipped_tables"])
	require.Equal(t, []any{map[string]any{"datname": "app", "schemaname": "public", "relname": "secret"}}, all["skipped_relations"])

	indexOnly := get("?object_kind=index")
	items, _ := indexOnly["items"].([]any)
	require.Len(t, items, 1)
	require.Equal(t, "index", items[0].(map[string]any)["object_kind"])
	require.Equal(t, "orders_pkey", items[0].(map[string]any)["indexrelname"])
	require.Equal(t, float64(2), indexOnly["skipped_tables"], "the skipped count is the instance's, whatever the filter")
}
