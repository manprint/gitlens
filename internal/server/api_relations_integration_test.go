//go:build integration

package server

import (
	"context"
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
		(ts, cluster_id, instance_id, datname, schemaname, relname, indexrelname, idx_scan, index_bytes, is_unique, is_primary, is_valid)
		VALUES ($1, 1, $2, 'app', 'public', 'orders', 'orders_id_idx', 0, 4096, true, false, true)`, now, id)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM metrics_indexes WHERE instance_id=$1`, id) })

	rec := httptest.NewRecorder()
	relationRouter(NewAPI(pool)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/v1/instances/"+id.String()+"/indexes", http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"indexrelname":"orders_id_idx"`)
}
