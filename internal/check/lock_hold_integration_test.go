//go:build integration

package check

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// holdLock runs stmts in a transaction on a connection of its own and keeps
// the transaction open until the test ends, so the locks they take stay held
// while the check runs. The rollback runs before the test's pools close.
func holdLock(t *testing.T, pool *pgxpool.Pool, stmts ...string) {
	t.Helper()
	ctx := context.Background()
	holder, err := pool.Acquire(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = holder.Exec(context.Background(), "ROLLBACK")
		holder.Release()
	})
	_, err = holder.Exec(ctx, "BEGIN")
	require.NoError(t, err)
	for _, stmt := range stmts {
		_, err = holder.Exec(ctx, stmt)
		require.NoError(t, err)
	}
}
