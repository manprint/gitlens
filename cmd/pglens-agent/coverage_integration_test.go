//go:build integration

package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/wire"
	"github.com/manprint/pglens/test/pgtest"
)

// WIRE-COV-005: the envelope a flush queues carries the agent start, the same
// on every flush, and the check intervals of the instance's own target, not
// those of another target.
func TestWIRECOV005_FlushCarriesTheAgentCoverage(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		mgr, err := agent.NewManager(ctx, "covered", pg.DSN("postgres", pgtest.RoleT0), agent.DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		t.Cleanup(mgr.Close)
		buf, pusher := newFlushBuffer(t)

		started := time.Date(2026, 10, 30, 8, 0, 0, 0, time.FixedZone("CET", 3600))
		coverage := agentCoverage{startedAt: started, intervals: map[string]map[string]int{
			"covered": {"activity": 15, "settings": 3600},
			"other":   {"locks": 30},
		}}
		var mu sync.Mutex
		for range 2 {
			flushEnvelope(ctx, []*agent.Manager{mgr}, &mu, map[string][]wire.Result{}, &edgeStates{byName: map[string]edgeState{}}, map[string]bool{}, map[string]map[string]bool{}, coverage, pusher)
			env := readEnvelope(t, buf)
			require.Len(t, env.Instances, 1)
			inst := env.Instances[0]
			require.NotNil(t, inst.AgentStartedAt)
			require.True(t, started.Equal(*inst.AgentStartedAt))
			require.Equal(t, time.UTC, inst.AgentStartedAt.Location(), "the start is sent in UTC")
			require.Equal(t, map[string]int{"activity": 15, "settings": 3600}, inst.CheckIntervals)
		}
	})
}
