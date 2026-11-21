//go:build integration

package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/store"
)

// TestINTIDXV004: after migrations an index_check command row is accepted by
// the kind CHECK, and a second result for the same key replaces the first.
func TestINTIDXV004(t *testing.T) {
	pool := getSharedPool(t)
	instanceID, _ := seedCommandInstance(t, pool)
	ctx := context.Background()
	commandID := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO commands (command_id, tenant_id, agent_id, instance_id, cluster_id, kind, args, state, expires_at) SELECT $1, tenant_id, agent_id, instance_id, cluster_id, 'index_check', '{"queryid":"7","datname":"app","index_schema":"public","index_relation":"orders","index_columns":["customer_id"]}', 'pending', now()+interval '5 minutes' FROM instances WHERE instance_id=$2`, commandID, instanceID)
	require.NoError(t, err)

	var clusterID int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT cluster_id FROM instances WHERE instance_id=$1`, instanceID).Scan(&clusterID))
	t0 := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	size := int64(8192)
	write := func(r store.IndexValidationRow) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()
		require.NoError(t, store.UpsertIndexValidation(ctx, tx, r))
		require.NoError(t, tx.Commit(ctx))
	}
	row := store.IndexValidationRow{TenantID: "default", InstanceID: instanceID, ClusterID: clusterID, Datname: "app", QueryID: 7, Schemaname: "public", Relname: "orders", Columns: []string{"customer_id"}, IndexUsed: false, CostBefore: 100, CostAfter: 100, ValidatedAt: t0, CommandID: commandID}
	write(row)
	row.IndexUsed, row.CostAfter, row.IndexSizeBytes, row.PlanAfter, row.ValidatedAt = true, 8.5, &size, json.RawMessage(`[{"Plan":{"Node Type":"Index Scan"}}]`), t0.Add(time.Hour)
	write(row)

	var (
		count     int
		used      bool
		costAfter float64
		gotSize   *int64
		at        time.Time
	)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) OVER (), index_used, cost_after, index_size_bytes, validated_at FROM index_validations WHERE instance_id=$1`, instanceID).Scan(&count, &used, &costAfter, &gotSize, &at))
	require.Equal(t, 1, count)
	require.True(t, used)
	require.InDelta(t, 8.5, costAfter, 1e-9)
	require.Equal(t, size, *gotSize)
	require.True(t, at.Equal(t0.Add(time.Hour)))
}
