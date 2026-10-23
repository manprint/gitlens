//go:build integration

package server

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// explainIndexed returns the plan of query with sequential scans disabled, so
// the plan names the index the query can use rather than the cheapest scan.
// The transaction rolls back, and with it the SET LOCAL.
func explainIndexed(t *testing.T, pool *pgxpool.Pool, query string, args ...any) string {
	t.Helper()
	ctx := contextBackground()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `SET LOCAL enable_seqscan = off`)
	require.NoError(t, err)
	rows, err := tx.Query(ctx, "EXPLAIN "+query, args...)
	require.NoError(t, err)
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		plan = append(plan, line)
	}
	require.NoError(t, rows.Err())
	return strings.Join(plan, "\n")
}

// TestAlertAPI_INT_ALERTAPI_008_LookupsBindTheTenant: the alert detail and the
// active silences must seek an index on every PostgreSQL major. Without the
// leading tenant column, PostgreSQL 15 to 17 cannot seek on alert_key or on
// the silence window, so the plan has no index condition on the tenant.
func TestAlertAPI_INT_ALERTAPI_008_LookupsBindTheTenant(t *testing.T) {
	pool := getSharedPool(t)
	plan := explainIndexed(t, pool, alertDetailSQL, "agent_down/11111111-1111-1111-1111-111111111111/")
	require.Contains(t, plan, "Index Cond: ((tenant_id = 'default'::text)", plan)
	plan = explainIndexed(t, pool, activeSilencesSQL)
	require.Contains(t, plan, "Index Cond: ((tenant_id = 'default'::text)", plan)
}

// TestQueryPlan_INT_PLAN_001_SlotsLastActiveUsesActivityIndex: the last active
// reading of an inactive slot must come from metrics_replication_slot_active_idx,
// the index that binds the instance, the slot and the activity.
func TestQueryPlan_INT_PLAN_001_SlotsLastActiveUsesActivityIndex(t *testing.T) {
	pool := getSharedPool(t)
	ctx := contextBackground()
	// The planner prunes a hypertable with no chunk to a constant false, which
	// shows no index at all. One row gives it a chunk to plan against.
	instance := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO metrics_replication (ts, cluster_id, instance_id, slot_name, source, slot_active) VALUES (now(), 1, $1, 'slot_a', 'slot', false)`, instance)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM metrics_replication WHERE instance_id=$1`, instance)
	})
	plan := explainIndexed(t, pool, slotsSQL, int64(1))
	require.Contains(t, plan, "metrics_replication_slot_active_idx", plan)
}

// TestQueryPlan_INT_PLAN_002_ASHTopTextUsesClusterIndex: each top entry's text
// must come from query_texts_cluster_queryid_idx. The primary key leads with
// datname, which the join never binds.
func TestQueryPlan_INT_PLAN_002_ASHTopTextUsesClusterIndex(t *testing.T) {
	pool := getSharedPool(t)
	query := "SELECT " + ashTopTextSQL + " FROM (SELECT $1::bigint AS queryid, $2::bigint AS cluster_id) top"
	plan := explainIndexed(t, pool, query, int64(42), int64(1))
	require.Contains(t, plan, "query_texts_cluster_queryid_idx", plan)
}
