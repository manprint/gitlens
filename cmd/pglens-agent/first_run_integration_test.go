//go:build integration

package main

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/wire"
	"github.com/manprint/pglens/test/pgtest"
)

// factKindsOf returns the fact kinds queued for target so far.
func factKindsOf(mu *sync.Mutex, pending map[string][]wire.Result, target string) map[string]bool {
	mu.Lock()
	defer mu.Unlock()
	kinds := map[string]bool{}
	for _, r := range pending[target] {
		for _, f := range r.Facts {
			kinds[f.Kind] = true
		}
	}
	return kinds
}

// TestINTFIRSTRUN001_HourlyChecksReportSoonAfterStart: settings, extensions and
// table_columns run every hour, yet their facts must arrive within the 600 s
// first-run cap (plus run time) of an agent start, not one interval later.
// It waits up to 11 minutes, so it runs only with PGLENS_FIRSTRUN_LONG=1.
func TestINTFIRSTRUN001_HourlyChecksReportSoonAfterStart(t *testing.T) {
	if os.Getenv("PGLENS_FIRSTRUN_LONG") != "1" {
		t.Skip("set PGLENS_FIRSTRUN_LONG=1 to run this 11-minute test")
	}
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		const target = "first-run-target"
		mgr, err := agent.NewManager(ctx, target, pg.DSN("app", pgtest.RoleT0), agent.DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		t.Cleanup(mgr.Close)

		var mu sync.Mutex
		pending := map[string][]wire.Result{}
		scheduledDBs := map[string]map[string]bool{}
		schedules := newScheduleSet(ctx, &agent.Config{}, clock.System(), []*agent.Manager{mgr}, &mu, pending, scheduledDBs)
		started := time.Now()
		require.NoError(t, schedules.start(ctx))
		t.Cleanup(schedules.stop)

		want := []string{"setting", "extension", "table_columns"}
		deadline := started.Add(11 * time.Minute)
		for {
			kinds := factKindsOf(&mu, pending, target)
			missing := []string{}
			for _, kind := range want {
				if !kinds[kind] {
					missing = append(missing, kind)
				}
			}
			if len(missing) == 0 {
				t.Logf("all hourly fact kinds arrived %s after start", time.Since(started).Round(time.Second))
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("fact kinds %v missing 11 minutes after start", missing)
			}
			time.Sleep(5 * time.Second)
		}
	})
}
