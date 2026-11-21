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

// TestINTQTAG001: a session running a statement with sqlcommenter tags shows
// up in an ash_samples metric labelled with its service and endpoint.
func TestINTQTAG001_TaggedStatementLabelsASH(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		mgr, err := agent.NewManager(ctx, "qtag-target", pg.DSN("postgres", pgtest.RoleT0), agent.DefaultConnOptions(), nil)
		require.NoError(t, err)
		defer mgr.Close()

		off := false
		var mu sync.Mutex
		pending := map[string][]wire.Result{}
		stop, enabled := startASH(ctx, mgr, "qtag-target", agent.CheckConfig{}, agent.CheckConfig{Enabled: &off}, clock.System(), &mu, pending)
		require.True(t, enabled)
		defer stop()

		sleeper := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		done := make(chan error, 1)
		go func() {
			_, err := sleeper.Exec(ctx, `SELECT pg_sleep(3) /*application='shop',route='%2Fcart'*/`)
			done <- err
		}()

		found := func() bool {
			mu.Lock()
			defer mu.Unlock()
			for _, res := range pending["qtag-target"] {
				for _, m := range res.Metrics {
					if m.Name == "ash_samples" && m.Labels["service"] == "shop" && m.Labels["endpoint"] == "/cart" {
						return true
					}
				}
			}
			return false
		}
		require.Eventually(t, found, 25*time.Second, 200*time.Millisecond)
		require.NoError(t, <-done)
	})
}
