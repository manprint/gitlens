//go:build integration

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

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
