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

// StartLogging starts a container of the given major version for one test
// with the logging collector writing destination (jsonlog or csvlog) into
// log_directory "log". Of the monitoring roles only RoleT0 exists, as a plain
// pg_monitor member without grants on pg_read_binary_file. The container is
// terminated when the test ends.
func StartLogging(t *testing.T, major int, destination string, settings ...string) *PG {
	t.Helper()
	ctx := context.Background()
	img, ok := images[major]
	require.True(t, ok, "no image for PostgreSQL %d", major)
	cmd := []string{"postgres",
		"-c", "logging_collector=on",
		"-c", "log_destination=" + destination,
		"-c", "log_directory=log",
		"-c", "log_file_mode=0644",
		"-c", "fsync=off",
		"-c", "synchronous_commit=off",
	}
	for _, s := range settings {
		cmd = append(cmd, "-c", s)
	}
	c, err := postgres.Run(ctx, img,
		postgres.WithDatabase("app"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForListeningPort("5432/tcp").WithStartupTimeout(90*time.Second)),
		testcontainers.WithCmd(cmd...),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })

	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	pool, err := waitPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()
	_, err = pool.Exec(ctx, "CREATE ROLE "+roleUser(RoleT0)+" LOGIN PASSWORD '"+rolePassword(RoleT0)+"' IN ROLE pg_monitor")
	require.NoError(t, err)
	var versionNum int
	require.NoError(t, pool.QueryRow(ctx, "SELECT current_setting('server_version_num')::int").Scan(&versionNum))
	return &PG{Major: major, Version: pgtype.PGVersion(versionNum), container: c, dsn: dsn}
}

// waitPool connects once the server accepts queries: with the collector on,
// the "ready" line goes to the log file, not to the container output.
func waitPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	deadline := time.Now().Add(60 * time.Second)
	for {
		pool, err := pgxpool.New(ctx, dsn)
		if err == nil {
			if err = pool.Ping(ctx); err == nil {
				return pool, nil
			}
			pool.Close()
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(500 * time.Millisecond)
	}
}
