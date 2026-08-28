//go:build integration

package server

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/manprint/pglens/internal/store"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	sharedPool      *pgxpool.Pool
	sharedContainer testcontainers.Container
	sharedOnce      sync.Once
	sharedErr       error
)

func getSharedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	sharedOnce.Do(func() {
		ctx := context.Background()
		c, err := postgres.Run(ctx, "timescale/timescaledb:2.29.0-pg17",
			postgres.WithDatabase("test"),
			postgres.WithUsername("postgres"),
			postgres.WithPassword("postgres"),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2)),
		)
		if err != nil {
			sharedErr = fmt.Errorf("start postgres: %w", err)
			return
		}
		sharedContainer = c
		dsn, err := c.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			sharedErr = err
			return
		}
		pool, err := pgxpool.New(ctx, dsn)
		if err != nil {
			sharedErr = err
			return
		}
		if _, err := pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS timescaledb`); err != nil {
			sharedErr = fmt.Errorf("create timescaledb extension: %w", err)
			pool.Close()
			return
		}
		if err := store.Migrate(ctx, pool); err != nil {
			sharedErr = fmt.Errorf("migrate: %w", err)
			pool.Close()
			return
		}
		sharedPool = pool
	})
	if sharedErr != nil {
		t.Fatalf("getSharedPool: %v", sharedErr)
	}
	if sharedPool == nil {
		t.Fatal("shared pool not initialized")
	}
	return sharedPool
}

func TestMain(m *testing.M) {
	code := m.Run()
	if sharedPool != nil {
		sharedPool.Close()
	}
	if sharedContainer != nil {
		_ = sharedContainer.Terminate(context.Background())
	}
	os.Exit(code)
}
