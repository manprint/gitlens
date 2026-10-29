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
)

func TestINTTBL004_RelationTableEndpointReturnsIngestedSample(t *testing.T) {
	pool := getSharedPool(t)
	ctx := context.Background()
	id := uuid.New()
	now := time.Now().UTC()
	_, err := pool.Exec(ctx, `INSERT INTO metrics_tables
		(ts, cluster_id, instance_id, datname, schemaname, relname, n_dead_tup, total_bytes, seq_scan)
		VALUES ($1, 1, $2, 'app', 'public', 'orders', 12, 2097152, 7)`, now, id)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM metrics_tables WHERE instance_id=$1`, id) })

	rec := httptest.NewRecorder()
	r := relationRouter(NewAPI(pool))
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+id.String()+"/tables", http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"relname":"orders"`)
}

func TestINTIDX004_RelationIndexEndpointReturnsDefinitionFactFields(t *testing.T) {
	pool := getSharedPool(t)
	ctx := context.Background()
	id := uuid.New()
	now := time.Now().UTC()
	_, err := pool.Exec(ctx, `INSERT INTO metrics_indexes
		(ts, cluster_id, instance_id, datname, schemaname, relname, indexrelname, idx_scan, index_bytes, is_unique, is_primary, is_valid,
		 idx_tup_read, idx_tup_fetch, idx_blks_read, idx_blks_hit)
		VALUES ($1, 1, $2, 'app', 'public', 'orders', 'orders_id_idx', 0, 4096, true, false, true, 2.5, 1.5, 1, 3)`, now, id)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO object_facts (cluster_id, instance_id, datname, kind, key, labels, value_text, first_seen, last_seen, changed_at)
		VALUES (1, $1, 'app', 'index_def', 'public.orders_id_idx', '{}'::jsonb,
		        'CREATE UNIQUE INDEX orders_id_idx ON public.orders USING btree (id)', $2, $2, $2)`, id, now)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM metrics_indexes WHERE instance_id=$1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM object_facts WHERE instance_id=$1`, id)
	})

	rec := httptest.NewRecorder()
	relationRouter(NewAPI(pool)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/v1/instances/"+id.String()+"/indexes", http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"indexrelname":"orders_id_idx"`)
	var body struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Items, 1)
	item := body.Items[0]
	require.Equal(t, "CREATE UNIQUE INDEX orders_id_idx ON public.orders USING btree (id)", item["definition"])
	require.InDelta(t, 2.5, item["idx_tup_read_rate"], 1e-9)
	require.InDelta(t, 1.5, item["idx_tup_fetch_rate"], 1e-9)
	require.InDelta(t, 0.75, item["cache_hit_ratio"], 1e-9)
}

// TestINTIDX007_RelationIndexEndpointReportsLastScan: the indexes endpoint says
// when an index was last scanned since the statistics reset. A timestamp is a
// scan, an empty label is an index never scanned since the reset (PostgreSQL 16
// and later), and a fact without the label is not available (PostgreSQL 15).
func TestINTIDX007_RelationIndexEndpointReportsLastScan(t *testing.T) {
	pool := getSharedPool(t)
	ctx := context.Background()
	id := uuid.New()
	now := time.Now().UTC().Truncate(time.Microsecond)
	scanned := now.Add(-2 * time.Hour)
	indexes := []string{"orders_scanned_idx", "orders_never_idx", "orders_v15_idx"}
	labels := map[string]string{
		"orders_scanned_idx": `{"last_idx_scan":"` + scanned.Format(time.RFC3339Nano) + `"}`,
		"orders_never_idx":   `{"last_idx_scan":""}`,
		"orders_v15_idx":     `{"def_hash":"h"}`,
	}
	for _, name := range indexes {
		_, err := pool.Exec(ctx, `INSERT INTO metrics_indexes
			(ts, cluster_id, instance_id, datname, schemaname, relname, indexrelname, idx_scan, index_bytes, is_unique, is_primary, is_valid)
			VALUES ($1, 1, $2, 'app', 'public', 'orders', $3, 0, 4096, false, false, true)`, now, id, name)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `INSERT INTO object_facts (cluster_id, instance_id, datname, kind, key, labels, value_text, first_seen, last_seen, changed_at)
			VALUES (1, $1, 'app', 'index_def', $2, $3::jsonb, 'CREATE INDEX ' || $2 || ' ON public.orders (a)', $4, $4, $4)`, id, "public."+name, labels[name], now)
		require.NoError(t, err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM metrics_indexes WHERE instance_id=$1`, id)
		_, _ = pool.Exec(ctx, `DELETE FROM object_facts WHERE instance_id=$1`, id)
	})

	rec := httptest.NewRecorder()
	relationRouter(NewAPI(pool)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/v1/instances/"+id.String()+"/indexes", http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Items []struct {
			IndexRelName string     `json:"indexrelname"`
			LastScanAt   *time.Time `json:"last_scan_at"`
			Status       string     `json:"last_scan_status"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	type lastScan struct {
		at     *time.Time
		status string
	}
	got := map[string]lastScan{}
	for _, item := range body.Items {
		got[item.IndexRelName] = lastScan{item.LastScanAt, item.Status}
	}
	require.Len(t, got, 3)
	require.Equal(t, "scanned", got["orders_scanned_idx"].status)
	require.NotNil(t, got["orders_scanned_idx"].at)
	require.True(t, got["orders_scanned_idx"].at.Equal(scanned))
	require.Equal(t, "never_since_reset", got["orders_never_idx"].status)
	require.Nil(t, got["orders_never_idx"].at)
	require.Equal(t, "not_available", got["orders_v15_idx"].status)
	require.Nil(t, got["orders_v15_idx"].at)
}

// TestINTIDX010_TableHistoryReturnsTheDeadTupleTrendOfTheWorstTables: the history
// endpoint picks the tables with the most dead tuples now and returns their
// dead and live counts per bucket, oldest first, one series per table.
func TestINTIDX010_TableHistoryReturnsTheDeadTupleTrendOfTheWorstTables(t *testing.T) {
	pool := getSharedPool(t)
	ctx := context.Background()
	id := uuid.New()
	now := time.Now().UTC().Truncate(time.Minute)
	for minute := 0; minute < 30; minute += 5 {
		at := now.Add(-time.Duration(minute) * time.Minute)
		for _, row := range []struct {
			table string
			dead  int64
		}{{"orders", int64(1000 - minute*10)}, {"items", int64(50 - minute)}} {
			_, err := pool.Exec(ctx, `INSERT INTO metrics_tables
				(ts, cluster_id, instance_id, datname, schemaname, relname, n_dead_tup, n_live_tup)
				VALUES ($1, 1, $2, 'app', 'public', $3, $4, 5000)`, at, id, row.table, row.dead)
			require.NoError(t, err)
		}
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM metrics_tables WHERE instance_id=$1`, id) })

	rec := httptest.NewRecorder()
	relationRouter(NewAPI(pool)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/v1/instances/"+id.String()+"/tables/history?limit=1&from="+now.Add(-time.Hour).Format(time.RFC3339)+"&to="+now.Add(time.Minute).Format(time.RFC3339), http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body tableHistoryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Tables, 1, "limit=1 keeps only the table with the most dead tuples")
	series := body.Tables[0]
	require.Equal(t, "orders", series.Relname)
	require.Len(t, series.Points, 6)
	for i := 1; i < len(series.Points); i++ {
		require.True(t, series.Points[i].TS.After(series.Points[i-1].TS), "points are oldest first")
	}
	require.EqualValues(t, 1000, *series.Points[len(series.Points)-1].NDeadTup)
	require.EqualValues(t, 5000, *series.Points[0].NLiveTup)

	rec = httptest.NewRecorder()
	relationRouter(NewAPI(pool)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/v1/instances/"+id.String()+"/tables/history?limit=99", http.NoBody))
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
