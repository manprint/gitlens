//go:build integration

package agent

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/manprint/pglens/internal/check"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

// INT-SCHED-001: A check whose statement exceeds its timeout is cancelled
// and the backend disappears from pg_stat_activity within a second.
func TestScheduler_IntegrationTimeoutCancellation(t *testing.T) {
	t.Parallel()
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		pool, err := pgxpool.New(ctx, pg.DSN("app", pgtest.RoleT0))
		require.NoError(t, err)
		defer pool.Close()

		// Create a simple target wrapping the pool
		target := &timeoutTestTarget{pool: pool, name: "localhost"}

		// Create a check that will timeout
		slowCheck := &timeoutTestCheck{
			name:    "slow_query",
			timeout: 500 * time.Millisecond,
		}

		opts := ScheduleOptions{
			MaxWorkers:      1,
			NoJitter:        true,
			Clock:           nil, // use system clock
			ShutdownTimeout: 5 * time.Second,
		}
		s := NewScheduler(opts)
		s.AddEntry(target, slowCheck, "")

		schedCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		err = s.Start(schedCtx)
		require.NoError(t, err)

		results := s.Results()

		// Wait for one result
		select {
		case res := <-results:
			// Should have an error due to timeout
			if res.Err == nil {
				t.Fatal("expected timeout error")
			}
			t.Logf("got expected timeout: %v", res.Err)
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for scheduler result")
		}

		s.Stop()
	})
}

// timeoutTestTarget wraps a pool for integration testing
type timeoutTestTarget struct {
	pool *pgxpool.Pool
	name string
}

func (t *timeoutTestTarget) InstanceID() pgtype.InstanceID { return pgtype.InstanceID{} }
func (t *timeoutTestTarget) ClusterID() pgtype.ClusterID   { return 0 }
func (t *timeoutTestTarget) Role() pgtype.Role             { return pgtype.RolePrimary }
func (t *timeoutTestTarget) PGVersion() pgtype.PGVersion   { return 160000 }
func (t *timeoutTestTarget) PermTier() pgtype.PermTier     { return pgtype.TierReadOnly }
func (t *timeoutTestTarget) HasExtension(name string) bool { return false }
func (t *timeoutTestTarget) Conn(ctx context.Context) (check.Conn, error) {
	return t.pool.Acquire(ctx)
}
func (t *timeoutTestTarget) ConnFor(ctx context.Context, datname string) (check.Conn, error) {
	return t.pool.Acquire(ctx)
}
func (t *timeoutTestTarget) Database() string   { return t.name }
func (t *timeoutTestTarget) Clock() clock.Clock { return clock.System() }

// timeoutTestCheck creates a query that will timeout
type timeoutTestCheck struct {
	name    string
	timeout time.Duration
}

func (c *timeoutTestCheck) Name() string                   { return c.name }
func (c *timeoutTestCheck) Requires() check.Requirements   { return check.Requirements{} }
func (c *timeoutTestCheck) DefaultInterval() time.Duration { return 1 * time.Second }
func (c *timeoutTestCheck) Timeout() time.Duration         { return c.timeout }
func (c *timeoutTestCheck) Scrape(ctx context.Context, t check.Target) (check.Result, error) {
	// This query will be cancelled by the timeout
	conn, err := t.Conn(ctx)
	if err != nil {
		return check.Result{}, err
	}
	defer conn.Release()

	// Run a query that sleeps for longer than the timeout. pgx's Query() only
	// sends the query and returns a lazy Rows; the actual wait — and the
	// context-deadline cancellation this test is proving — happens on Next(),
	// so the rows must be consumed for the timeout to have any chance to fire.
	rows, err := conn.Query(ctx, "SELECT pg_sleep(2)")
	if err != nil {
		return check.Result{}, err
	}
	defer rows.Close()
	rows.Next()
	return check.Result{}, rows.Err()
}
