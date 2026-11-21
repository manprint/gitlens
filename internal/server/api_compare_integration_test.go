//go:build integration

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func comparePath(endpoint string, instance uuid.UUID, a, b period, extra string) string {
	f := func(t time.Time) string { return t.UTC().Format(time.RFC3339) }
	return endpoint + "?instance_id=" + instance.String() + "&a_from=" + f(a.From) + "&a_to=" + f(a.To) + "&b_from=" + f(b.From) + "&b_to=" + f(b.To) + extra
}

// INT-CMP-001: query 1 runs at rate 10 in A and 30 in B, query 2 only in A,
// query 3 only in B; the statuses are both, gone and new, query 1's ratio is
// 3, and the rows come by absolute change; an unknown instance is a 404.
func TestINTCMP001(t *testing.T) {
	pool := getSharedPool(t)
	instance, cluster := seedSearchInstance(t, pool)
	now := time.Now().UTC().Truncate(time.Minute)
	a := period{From: now.Add(-3 * time.Hour), To: now.Add(-2 * time.Hour)}
	b := period{From: now.Add(-time.Hour), To: now}
	seedSearchStatement(t, pool, instance, cluster, 1, 10, "SELECT one", a.From.Add(10*time.Minute))
	_, err := pool.Exec(context.Background(), `INSERT INTO metrics_statements (ts, cluster_id, instance_id, datname, queryid, calls_rate, exec_time_rate_ms, sample_seconds)
		VALUES ($1, $2, $3, 'app', 1, 30, 30, 60)`, b.From.Add(10*time.Minute), cluster, instance)
	require.NoError(t, err)
	seedSearchStatement(t, pool, instance, cluster, 2, 5, "SELECT two", a.From.Add(20*time.Minute))
	seedSearchStatement(t, pool, instance, cluster, 3, 15, "SELECT three", b.From.Add(20*time.Minute))

	code, body := searchGet(t, pool, comparePath("/api/v1/statements/compare", instance, a, b, ""))
	require.Equal(t, http.StatusOK, code, string(body))
	var resp statementCompareResp
	require.NoError(t, json.Unmarshal(body, &resp))
	require.Equal(t, "per_second", resp.Normalized)
	require.Equal(t, "total_exec_time", resp.OrderBy)
	var ids, statuses []string
	for _, r := range resp.Rows {
		ids = append(ids, r.QueryID)
		statuses = append(statuses, r.Status)
	}
	require.Equal(t, []string{"1", "3", "2"}, ids)
	require.Equal(t, []string{"both", "new", "gone"}, statuses)
	require.InDelta(t, 3.0, *resp.Rows[0].Ratio.TotalExec, 1e-9)
	require.InDelta(t, 3.0, *resp.Rows[0].Ratio.Calls, 1e-9)
	require.InDelta(t, 600.0/3600, *resp.Rows[0].A.Calls, 1e-9)
	require.Equal(t, "SELECT one", *resp.Rows[0].QueryText)

	code, body = searchGet(t, pool, comparePath("/api/v1/statements/compare", instance, a, b, "&limit=1&order_by=calls"))
	require.Equal(t, http.StatusOK, code, string(body))
	require.NoError(t, json.Unmarshal(body, &resp))
	require.Len(t, resp.Rows, 1)
	require.Equal(t, "1", resp.Rows[0].QueryID)

	code, _ = searchGet(t, pool, comparePath("/api/v1/statements/compare", uuid.New(), a, b, ""))
	require.Equal(t, http.StatusNotFound, code)
}

// INT-CMP-002: the Lock wait averages 1 active session in A and 4 in B, so
// it is the first row with ratio 4.
func TestINTCMP002(t *testing.T) {
	pool := getSharedPool(t)
	instance, cluster := seedSearchInstance(t, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM metrics_ash WHERE instance_id=$1`, instance)
	})
	base := time.Now().UTC().Truncate(time.Minute).Add(-10 * time.Minute)
	a := period{From: base, To: base.Add(time.Minute)}
	b := period{From: base.Add(time.Minute), To: base.Add(2 * time.Minute)}
	seed := func(ts time.Time, waitType string, samples int) {
		_, err := pool.Exec(context.Background(), `INSERT INTO metrics_ash (ts, cluster_id, instance_id, datname, wait_event_type, wait_event, state, queryid, samples, window_seconds, window_ticks)
			VALUES ($1, $2, $3, 'app', $4, $4, 'active', 7, $5, 10, 10)`, ts, cluster, instance, waitType, samples)
		require.NoError(t, err)
	}
	for i := 0; i < 6; i++ {
		seed(a.From.Add(time.Duration(i*10)*time.Second), "Lock", 10)
		seed(a.From.Add(time.Duration(i*10)*time.Second), "IO", 5)
		seed(b.From.Add(time.Duration(i*10)*time.Second), "Lock", 40)
		seed(b.From.Add(time.Duration(i*10)*time.Second), "IO", 5)
	}
	r := chi.NewRouter()
	NewAshAPI(pool).RegisterRoutes(r)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, comparePath("/api/v1/ash/compare", instance, a, b, "&group_by=wait_event_type"), http.NoBody))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp ashCompareResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Rows, 2)
	require.Equal(t, "Lock", resp.Rows[0].Key)
	require.Equal(t, "both", resp.Rows[0].Status)
	require.InDelta(t, 1.0, *resp.Rows[0].A.AAS, 1e-9)
	require.InDelta(t, 4.0, *resp.Rows[0].Ratio.AAS, 1e-9)
	require.InDelta(t, 1.0, *resp.Rows[1].Ratio.AAS, 1e-9)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, comparePath("/api/v1/ash/compare", instance, a, b, "&group_by=wait_event_type&wait_event_type=IO"), http.NoBody))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Rows, 1)
	require.InDelta(t, 0.5, *resp.Rows[0].A.AAS, 1e-9, "the filter narrows the samples, not the ticks")
}
