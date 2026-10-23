//go:build integration

package pgtest

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// StartPreparedTransactions starts a container of the given major version for
// one test, with max_prepared_transactions above zero. The shared containers
// leave it at PostgreSQL's default of 0, which refuses PREPARE TRANSACTION, so
// a test that needs a prepared transaction starts its own. The container is
// terminated when the test ends. Only the superuser DSN is set up; the
// monitoring roles of the shared containers are not created here.
func StartPreparedTransactions(t *testing.T, major int) *PG {
	t.Helper()
	ctx := context.Background()
	img, ok := images[major]
	require.True(t, ok, "no image for PostgreSQL %d", major)
	c, err := postgres.Run(ctx, img,
		postgres.WithDatabase("app"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(90*time.Second)),
		testcontainers.WithCmd("postgres",
			"-c", "max_prepared_transactions=5",
			"-c", "fsync=off",
			"-c", "synchronous_commit=off",
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })

	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()
	var versionNum int
	require.NoError(t, pool.QueryRow(ctx, "SELECT current_setting('server_version_num')::int").Scan(&versionNum))
	return &PG{Major: major, Version: pgtype.PGVersion(versionNum), container: c, dsn: dsn}
}
