//go:build integration

package server

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestINTAVH002: the same autovacuum_run pushed twice is stored once, no object
// fact is written, and the 90-day retention policy exists.
func TestINTAVH002(t *testing.T) {
	router, count := planRouter(t)
	pool := getSharedPool(t)
	instance := uuid.NewString()
	now := time.Now().UTC().Truncate(time.Microsecond)
	f := autovacuumFact(map[string]string{"ts": now.Add(-time.Second).Format(time.RFC3339Nano)}, `{"tuples_removed":5,"elapsed_seconds":0.5}`)
	pushPlanFacts(t, router, instance, now, f)
	pushPlanFacts(t, router, instance, now, f)
	require.Equal(t, 1, count("autovacuum_runs", instance))

	ctx := context.Background()
	var removed int64
	var scanned *int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT tuples_removed, pages_scanned FROM autovacuum_runs WHERE instance_id = $1 AND relname = 't'`, instance).Scan(&removed, &scanned))
	require.Equal(t, int64(5), removed)
	require.Nil(t, scanned)
	var facts int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM object_facts WHERE kind = 'autovacuum_run'`).Scan(&facts))
	require.Zero(t, facts)

	var dropAfter string
	require.NoError(t, pool.QueryRow(ctx, `SELECT config->>'drop_after' FROM timescaledb_information.jobs WHERE proc_name = 'policy_retention' AND hypertable_name = 'autovacuum_runs'`).Scan(&dropAfter))
	require.Equal(t, "90 days", dropAfter)
}
