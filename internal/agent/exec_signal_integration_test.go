//go:build integration

package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/command"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

// INT-CMD-008: cancel leaves the victim connection usable; terminate closes it.
func TestINTCMD008_SignalCancelAndTerminate(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		victim := pg.Pool(t, "postgres", pgtest.RoleVictim)
		defer victim.Close()
		observer := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		defer observer.Close()
		victimConn, err := victim.Acquire(ctx)
		require.NoError(t, err)
		defer victimConn.Release()
		var pid int
		require.NoError(t, victimConn.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid))

		queryDone := make(chan error, 1)
		go func() {
			_, execErr := victimConn.Exec(ctx, "SELECT pg_sleep(30)")
			queryDone <- execErr
		}()
		require.Eventually(t, func() bool {
			var state string
			return observer.QueryRow(ctx, "SELECT state FROM pg_stat_activity WHERE pid = $1", pid).Scan(&state) == nil && state == "active"
		}, 5*time.Second, 50*time.Millisecond)

		mgr, err := NewManager(ctx, "signal-target", pg.DSN("postgres", pgtest.RoleT2), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()
		result, err := NewCancelExecutor().Execute(ctx, mgr, command.Args{PID: &pid})
		require.NoError(t, err)
		var cancelled struct {
			Signalled bool `json:"signalled"`
		}
		require.NoError(t, json.Unmarshal(result, &cancelled))
		require.True(t, cancelled.Signalled)
		require.Error(t, <-queryDone)
		_, err = victimConn.Exec(ctx, "SELECT 1")
		require.NoError(t, err)

		queryDone = make(chan error, 1)
		go func() {
			_, execErr := victimConn.Exec(ctx, "SELECT pg_sleep(30)")
			queryDone <- execErr
		}()
		require.Eventually(t, func() bool {
			var state string
			return observer.QueryRow(ctx, "SELECT state FROM pg_stat_activity WHERE pid = $1", pid).Scan(&state) == nil && state == "active"
		}, 5*time.Second, 50*time.Millisecond)
		result, err = NewTerminateExecutor().Execute(ctx, mgr, command.Args{PID: &pid})
		require.NoError(t, err)
		require.Error(t, <-queryDone)
		_, err = victimConn.Exec(ctx, "SELECT 1")
		require.Error(t, err, "terminated victim connection must no longer accept queries")
	})
}
