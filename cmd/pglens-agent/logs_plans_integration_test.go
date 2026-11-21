//go:build integration

package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/internal/wire"
	"github.com/manprint/pglens/test/pgtest"
)

// TestINTAEX001: PostgreSQL 18 with auto_explain in JSON format; after
// SELECT count(*) FROM pg_class the loop ships a plan fact of source
// auto_explain whose plan_hash is agent.PlanHash of its body.
func TestINTAEX001(t *testing.T) {
	ctx := context.Background()
	pg := pgtest.StartLogging(t, 18, "jsonlog",
		"shared_preload_libraries=auto_explain", "auto_explain.log_min_duration=0",
		"auto_explain.log_format=json", "auto_explain.log_analyze=on", "compute_query_id=on")
	l, super, results := sqlLogLoop(t, pg)

	// The first scrape only marks the end of the log.
	l.scrape(ctx)
	_, err := super.Exec(ctx, "SELECT count(*) FROM pg_class")
	require.NoError(t, err)

	var found *wire.Fact
	var body struct {
		Plan json.RawMessage `json:"plan"`
	}
	require.Eventually(t, func() bool {
		l.scrape(ctx)
		for _, r := range results() {
			for _, f := range r.Facts {
				if f.Kind != planFactKind || !strings.Contains(string(f.ValueJSON), `"Relation Name":"pg_class"`) ||
					!strings.Contains(string(f.ValueJSON), `"Node Type":"Aggregate"`) {
					continue
				}
				found = &f
				return true
			}
		}
		return false
	}, 20*time.Second, 300*time.Millisecond)

	require.Equal(t, "auto_explain", found.Labels["source"])
	require.Equal(t, "true", found.Labels["analyzed"])
	require.NotEqual(t, "0", found.Labels["queryid"])
	require.NotEmpty(t, found.Labels["duration_ms"])
	require.NotContains(t, string(found.ValueJSON), "Query Text")
	require.NoError(t, json.Unmarshal(found.ValueJSON, &body))
	hash, err := agent.PlanHash(body.Plan)
	require.NoError(t, err)
	require.Equal(t, hash, found.Labels["plan_hash"])
	for _, r := range results() {
		require.Empty(t, r.SkipReason, "no coverage note expected: %s", r.SkipReason)
	}
}
