//go:build integration

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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
		var terminated struct {
			Signalled bool `json:"signalled"`
		}
		require.NoError(t, json.Unmarshal(result, &terminated))
		require.True(t, terminated.Signalled)
		require.Error(t, <-queryDone)
		_, err = victimConn.Exec(ctx, "SELECT 1")
		require.Error(t, err, "terminated victim connection must no longer accept queries")
	})
}

// INT-CMD-031: the signal reaches only the session the check read. A start
// time from another session leaves the victim running, and the start time the
// check read signals it. The start time round-trips through pgx, so this also
// proves the statement matches a timestamp PostgreSQL itself reports.
func TestINTCMD031_SignalIsBoundToTheCheckedSession(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		victim := pg.Pool(t, "postgres", pgtest.RoleVictim)
		defer victim.Close()
		observer := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		defer observer.Close()
		signalPool := pg.Pool(t, "postgres", pgtest.RoleT2)
		defer signalPool.Close()
		victimConn, err := victim.Acquire(ctx)
		require.NoError(t, err)
		defer victimConn.Release()
		var pid int
		require.NoError(t, victimConn.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid))
		var started time.Time
		require.NoError(t, observer.QueryRow(ctx, "SELECT backend_start FROM pg_stat_activity WHERE pid = $1", pid).Scan(&started))

		queryDone := make(chan error, 1)
		go func() {
			_, execErr := victimConn.Exec(ctx, "SELECT pg_sleep(30)")
			queryDone <- execErr
		}()
		require.Eventually(t, func() bool {
			var state string
			return observer.QueryRow(ctx, "SELECT state FROM pg_stat_activity WHERE pid = $1", pid).Scan(&state) == nil && state == "active"
		}, 5*time.Second, 50*time.Millisecond)

		signalDB, err := signalPool.Acquire(ctx)
		require.NoError(t, err)
		defer signalDB.Release()
		otherSession := started.Add(time.Second)
		signalled, err := signalVerifiedBackend(ctx, signalDB, "pg_terminate_backend", pid, &otherSession)
		require.ErrorIs(t, err, pgx.ErrNoRows)
		require.False(t, signalled)
		var state string
		require.NoError(t, observer.QueryRow(ctx, "SELECT state FROM pg_stat_activity WHERE pid = $1", pid).Scan(&state))
		require.Equal(t, "active", state, "a session that does not match the checked start is left running")

		signalled, err = signalVerifiedBackend(ctx, signalDB, "pg_terminate_backend", pid, &started)
		require.NoError(t, err)
		require.True(t, signalled)
		require.Error(t, <-queryDone)
	})
}

// INT-CMD-037: a cancel that names another session's start time is refused
// before any signal, and the victim keeps running. The start time the caller
// saw cancels the query.
func TestINTCMD037_CancelRefusesAStartTimeOfAnotherSession(t *testing.T) {
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
		var started time.Time
		require.NoError(t, observer.QueryRow(ctx, "SELECT backend_start FROM pg_stat_activity WHERE pid = $1", pid).Scan(&started))

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
		otherSession := started.Add(time.Second)
		_, err = NewCancelExecutor().Execute(ctx, mgr, command.Args{PID: &pid, BackendStart: &otherSession})
		var rejected *RejectedError
		require.True(t, errors.As(err, &rejected))
		require.Contains(t, rejected.Error(), "different session")
		select {
		case queryErr := <-queryDone:
			t.Fatalf("a refused cancel reached the session: %v", queryErr)
		case <-time.After(500 * time.Millisecond):
		}
		var state string
		require.NoError(t, observer.QueryRow(ctx, "SELECT state FROM pg_stat_activity WHERE pid = $1", pid).Scan(&state))
		require.Equal(t, "active", state, "a session the caller did not name is left running")

		result, err := NewCancelExecutor().Execute(ctx, mgr, command.Args{PID: &pid, BackendStart: &started})
		require.NoError(t, err)
		var cancelled struct {
			Signalled bool `json:"signalled"`
		}
		require.NoError(t, json.Unmarshal(result, &cancelled))
		require.True(t, cancelled.Signalled)
		require.Error(t, <-queryDone)
		_, err = victimConn.Exec(ctx, "SELECT 1")
		require.NoError(t, err)
	})
}
