//go:build integration

package check

import (
	"context"
	"testing"
	"time"

	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

// TestINTLIM001_SessionLimitsPersistOnTheConnection pins what
// ApplySessionLimits documents: the limits are session settings, so they are
// still in force after the statements that set them. Under SET LOCAL, outside
// a transaction, the server defaults would stay in place.
func TestINTLIM001_SessionLimitsPersistOnTheConnection(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		conn, err := pg.Pool(t, "app", pgtest.RoleT0).Acquire(ctx)
		require.NoError(t, err)
		defer conn.Release()

		require.NoError(t, ApplySessionLimits(ctx, conn, 2*time.Second))

		var statementTimeout, lockTimeout, idleTimeout string
		require.NoError(t, conn.QueryRow(ctx, `SELECT current_setting('statement_timeout'), current_setting('lock_timeout'), current_setting('idle_in_transaction_session_timeout')`).Scan(&statementTimeout, &lockTimeout, &idleTimeout))
		require.Equal(t, "2s", statementTimeout)
		require.Equal(t, "1s", lockTimeout)
		require.Equal(t, "30s", idleTimeout)
	})
}
