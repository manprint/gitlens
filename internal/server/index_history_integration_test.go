//go:build integration

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func indexHistoryGet(t *testing.T, pool *pgxpool.Pool, instance uuid.UUID, v url.Values) (*httptest.ResponseRecorder, indexHistoryResponse) {
	t.Helper()
	r := chi.NewRouter()
	NewAPI(pool).RegisterRoutes(r)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+instance.String()+"/indexes/history?"+v.Encode(), http.NoBody))
	var resp indexHistoryResponse
	if w.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	}
	return w, resp
}

func seedIndexSample(t *testing.T, pool *pgxpool.Pool, instance uuid.UUID, cluster int64, index string, ts time.Time, scan, read float64, bytes int64) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `INSERT INTO metrics_indexes (ts, cluster_id, instance_id, datname, schemaname, relname, indexrelname, idx_scan, idx_tup_read, index_bytes, is_unique, is_primary, is_valid, def_hash)
		VALUES ($1, $2, $3, 'odoo', 'public', 'res_partner', $4, $5, $6, $7, true, false, true, 'h')`, ts, cluster, instance, index, scan, read, bytes)
	require.NoError(t, err)
}

// IDXH-INT-001: a bucket with no stored sample is null (never interpolated),
// rates are the mean of the bucket's stored per-second rates, the size is the
// last value of the bucket, and an unrelated index is excluded.
func TestIDXHINT001_BucketsAndNulls(t *testing.T) {
	pool := getSharedPool(t)
	instance, cluster := seedSearchInstance(t, pool)
	h := time.Now().UTC().Truncate(time.Hour).Add(-3 * time.Hour)
	seedIndexSample(t, pool, instance, cluster, "res_partner_name_idx", h.Add(2*time.Minute), 1, 10, 100)
	seedIndexSample(t, pool, instance, cluster, "res_partner_name_idx", h.Add(7*time.Minute), 3, 30, 110)
	seedIndexSample(t, pool, instance, cluster, "res_partner_name_idx", h.Add(45*time.Minute), 6, 0, 130)
	seedIndexSample(t, pool, instance, cluster, "other_idx", h.Add(2*time.Minute), 99, 99, 9999)

	w, resp := indexHistoryGet(t, pool, instance, url.Values{
		"database": {"odoo"}, "schema": {"public"}, "index": {"res_partner_name_idx"},
		"from": {h.Format(time.RFC3339)}, "to": {h.Add(time.Hour).Format(time.RFC3339)}, "step": {"20m"},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, 1200, resp.StepSecs)
	require.Len(t, resp.Points, 3)
	f := func(v float64) *float64 { return &v }
	n := func(v int64) *int64 { return &v }
	require.Equal(t, indexHistoryPoint{TS: h, IdxScanRate: f(2), IdxTupReadRate: f(20), IndexBytes: n(110)}, resp.Points[0])
	require.Equal(t, indexHistoryPoint{TS: h.Add(20 * time.Minute)}, resp.Points[1], "an empty bucket is null, not interpolated")
	require.Equal(t, indexHistoryPoint{TS: h.Add(40 * time.Minute), IdxScanRate: f(6), IdxTupReadRate: f(0), IndexBytes: n(130)}, resp.Points[2])
	require.Contains(t, w.Body.String(), `"idx_scan_rate":null`)
}

// IDXH-INT-002: an index the agent reported but with no rows in the range has
// no points; an index nobody reported is a 404.
func TestIDXHINT002_KnownWithoutRowsAndUnknown(t *testing.T) {
	pool := getSharedPool(t)
	instance, cluster := seedSearchInstance(t, pool)
	seedSearchFact(t, pool, instance, cluster, "odoo", "index_def", "public.fresh_idx",
		map[string]string{"schemaname": "public", "relname": "res_partner", "indexrelname": "fresh_idx"},
		"CREATE INDEX fresh_idx ON public.res_partner USING btree (ref)", nil)

	w, resp := indexHistoryGet(t, pool, instance, url.Values{"database": {"odoo"}, "schema": {"public"}, "index": {"fresh_idx"}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Empty(t, resp.Points)
	require.Contains(t, w.Body.String(), `"points":[]`)

	w, _ = indexHistoryGet(t, pool, instance, url.Values{"database": {"odoo"}, "schema": {"public"}, "index": {"nope_idx"}})
	require.Equal(t, http.StatusNotFound, w.Code)
	w, _ = indexHistoryGet(t, pool, uuid.New(), url.Values{"database": {"odoo"}, "schema": {"public"}, "index": {"fresh_idx"}})
	require.Equal(t, http.StatusNotFound, w.Code)
}
