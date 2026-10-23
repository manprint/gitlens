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

// TestINTRUN001_ShutdownReturnsWhileASHHoldsAConnection is the SIGTERM path
// with the default config: the ASH sampler holds its dedicated connection when
// shutdown starts, and closing the target must not wait for it to be released.
func TestINTRUN001_ShutdownReturnsWhileASHHoldsAConnection(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		mgr, err := agent.NewManager(ctx, "shutdown-target", pg.DSN("postgres", pgtest.RoleT0), agent.DefaultConnOptions(), clock.System())
		require.NoError(t, err)

		var mu sync.Mutex
		pending := map[string][]wire.Result{}
		stop, enabled := startASH(ctx, mgr, "shutdown-target", agent.CheckConfig{}, clock.System(), &mu, pending)
		require.True(t, enabled, "ASH must be enabled for the default config")

		// The dedicated connection is acquired on the sampler's first tick, so
		// wait until the server shows it before shutting down.
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		require.Eventually(t, func() bool {
			var n int
			err := admin.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity WHERE application_name = 'pglens/ash'").Scan(&n)
			return err == nil && n > 0
		}, 10*time.Second, 50*time.Millisecond)

		cancel()
		done := make(chan struct{})
		go func() {
			closeTargets([]func(){stop}, []*agent.Manager{mgr})
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			// Release the sampler so the blocked Close can return and the
			// container can be restored for the next test.
			stop()
			<-done
			t.Fatal("shutdown blocked on the ASH connection: closeTargets must stop the samplers before closing the managers")
		}
	})
}
