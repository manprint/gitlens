//go:build integration

package server

import (
	"strings"
	"testing"

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
