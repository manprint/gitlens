//go:build integration

package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/wire"
	"github.com/manprint/pglens/test/pgtest"
)

// TestINTAVH001: PostgreSQL 18 logging every autovacuum run; after most rows of
// a table are deleted the loop ships an autovacuum_run fact for that table with
// tuples_removed > 0.
func TestINTAVH001(t *testing.T) {
	ctx := context.Background()
	pg := pgtest.StartLogging(t, 18, "jsonlog", "log_autovacuum_min_duration=0", "autovacuum_naptime=1s")
	l, super, results := sqlLogLoop(t, pg)

	l.scrape(ctx)
	_, err := super.Exec(ctx, `CREATE TABLE avh_int(id int, pad text);
INSERT INTO avh_int SELECT g, repeat('x', 50) FROM generate_series(1, 20000) g;
DELETE FROM avh_int WHERE id > 100`)
	require.NoError(t, err)

	var found *wire.Fact
	require.Eventually(t, func() bool {
		l.scrape(ctx)
		for _, r := range results() {
			for _, f := range r.Facts {
				if f.Kind == autovacuumRunKind && f.Labels["relname"] == "avh_int" && f.Labels["kind"] == "vacuum" {
					found = &f
					return true
				}
			}
		}
		return false
	}, 90*time.Second, 500*time.Millisecond)

	require.Equal(t, "public", found.Labels["schemaname"])
	var value map[string]float64
	require.NoError(t, json.Unmarshal(found.ValueJSON, &value))
	require.Greater(t, value["tuples_removed"], 0.0)
}
