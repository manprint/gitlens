package server

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type stubIndexHistory struct {
	points        []indexHistoryPoint
	instanceFound bool
	indexFound    bool
	err           error
	got           indexHistoryQuery
	calls         int
}

func (s *stubIndexHistory) IndexHistory(_ context.Context, q indexHistoryQuery) ([]indexHistoryPoint, bool, bool, error) {
	s.calls++
	s.got = q
	return s.points, s.instanceFound, s.indexFound, s.err
}

func indexHistoryPath(id uuid.UUID, extra url.Values) string {
	v := url.Values{"database": {"odoo"}, "schema": {"public"}, "index": {"res_partner_pkey"}}
	for k, e := range extra {
		v[k] = e
	}
	return "/api/v1/instances/" + id.String() + "/indexes/history?" + v.Encode()
}

// IDXH-001: a missing index, a bad step or a range over 30 days is a 400.
func TestIDXH001_InvalidParameters(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	st := &stubIndexHistory{instanceFound: true, indexFound: true}
	api := &API{indexHistory: st}
	to := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	rfc := func(d time.Duration) string { return to.Add(d).Format(time.RFC3339) }
	cases := []struct {
		name  string
		path  string
		error string
	}{
		{"no index", "/api/v1/instances/" + id.String() + "/indexes/history?database=odoo&schema=public", "missing index"},
		{"no database", "/api/v1/instances/" + id.String() + "/indexes/history?schema=public&index=x", "missing database"},
		{"no schema", "/api/v1/instances/" + id.String() + "/indexes/history?database=odoo&index=x", "missing schema"},
		{"oversized index", indexHistoryPath(id, url.Values{"index": {string(make([]byte, 300))}}), "invalid index"},
		{"bad id", "/api/v1/instances/nope/indexes/history?database=a&schema=b&index=c", "invalid instance_id"},
		{"step not a duration", indexHistoryPath(id, url.Values{"step": {"abc"}}), "invalid step"},
		{"step negative", indexHistoryPath(id, url.Values{"step": {"-10m"}}), "invalid step"},
		{"step under the sampling interval", indexHistoryPath(id, url.Values{"step": {"5m"}}), "invalid step"},
		{"step gives too many points", indexHistoryPath(id, url.Values{"step": {"10m"}, "from": {rfc(-30 * 24 * time.Hour)}, "to": {rfc(0)}}), "invalid step"},
		{"range over 30 days", indexHistoryPath(id, url.Values{"from": {rfc(-31 * 24 * time.Hour)}, "to": {rfc(0)}}), "invalid range"},
		{"from after to", indexHistoryPath(id, url.Values{"from": {rfc(0)}, "to": {rfc(-time.Hour)}}), "invalid range"},
		{"bad from", indexHistoryPath(id, url.Values{"from": {"yesterday"}}), "invalid from"},
		{"bad to", indexHistoryPath(id, url.Values{"to": {"tomorrow"}}), "invalid to"},
	}
	for _, c := range cases {
		requireAPIError(t, retentionRequest(t, api, http.MethodGet, c.path, ""), http.StatusBadRequest, c.error)
	}
	require.Zero(t, st.calls, "a rejected request never reaches the store")
}

// IDXH-002: an unknown instance or index is a 404.
func TestIDXH002_UnknownInstanceOrIndex(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	requireAPIError(t, retentionRequest(t, &API{indexHistory: &stubIndexHistory{}}, http.MethodGet, indexHistoryPath(id, nil), ""), http.StatusNotFound, "instance not found")
	requireAPIError(t, retentionRequest(t, &API{indexHistory: &stubIndexHistory{instanceFound: true}}, http.MethodGet, indexHistoryPath(id, nil), ""), http.StatusNotFound, "index not found")
	requireAPIError(t, retentionRequest(t, &API{}, http.MethodGet, indexHistoryPath(id, nil), ""), http.StatusNotFound, "instance not found")
	requireAPIError(t, retentionRequest(t, &API{indexHistory: &stubIndexHistory{err: context.DeadlineExceeded}}, http.MethodGet, indexHistoryPath(id, nil), ""), http.StatusServiceUnavailable, "history timed out")
	requireAPIError(t, retentionRequest(t, &API{indexHistory: &stubIndexHistory{err: context.Canceled}}, http.MethodGet, indexHistoryPath(id, nil), ""), http.StatusInternalServerError, "index history query failed")
}

// IDXH-003: stub rows give the exact JSON, with null for a bucket without a
// sample and the default step for the range.
func TestIDXH003_ExactJSON(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	t0 := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	scan, tup, bytes := 0.25, 3.5, int64(1_380_000)
	st := &stubIndexHistory{instanceFound: true, indexFound: true, points: []indexHistoryPoint{
		{TS: t0, IdxScanRate: &scan, IdxTupReadRate: &tup, IndexBytes: &bytes},
		{TS: t0.Add(10 * time.Minute)},
	}}
	rec := retentionRequest(t, &API{indexHistory: st}, http.MethodGet, indexHistoryPath(id, url.Values{
		"from": {t0.Format(time.RFC3339)}, "to": {t0.Add(20 * time.Minute).Format(time.RFC3339)},
	}), "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `{
	  "index": {"datname": "odoo", "schemaname": "public", "indexrelname": "res_partner_pkey"},
	  "step_seconds": 600,
	  "points": [
	    {"ts": "2026-11-05T10:00:00Z", "idx_scan_rate": 0.25, "idx_tup_read_rate": 3.5, "index_bytes": 1380000},
	    {"ts": "2026-11-05T10:10:00Z", "idx_scan_rate": null, "idx_tup_read_rate": null, "index_bytes": null}
	  ]}`, rec.Body.String())
	require.Equal(t, 600*time.Second, st.got.Step, "a short range is clamped to the 10 minute sampling floor")
	require.Equal(t, id, st.got.Instance)

	// Default range 24 h → 120 buckets of 12 minutes; an explicit step wins.
	st.points = nil
	rec = retentionRequest(t, &API{indexHistory: st}, http.MethodGet, indexHistoryPath(id, nil), "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"index": {"datname": "odoo", "schemaname": "public", "indexrelname": "res_partner_pkey"}, "step_seconds": 720, "points": []}`, rec.Body.String())
	rec = retentionRequest(t, &API{indexHistory: st}, http.MethodGet, indexHistoryPath(id, url.Values{"step": {"1h"}}), "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, time.Hour, st.got.Step)
}
