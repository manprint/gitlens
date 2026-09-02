package store

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Migrate applies all pending migrations in order. It is forward-only and
// idempotent. It creates the schema_migrations table if needed and records
// each applied version. Running it twice is a no-op.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	// Multiple server replicas can start against the same store at once. Keep
	// the advisory lock and every migration query on one physical connection so
	// the session-level lock covers the complete check/apply/record sequence.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(hashtext('pglens:migrations'))`); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer func() {
		_, _ = conn.Exec(unlockCtx, `SELECT pg_advisory_unlock(hashtext('pglens:migrations'))`)
	}()

	// Ensure schema_migrations exists
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version int PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	for _, name := range files {
		var version int
		if _, err := fmt.Sscanf(name, "%04d_", &version); err != nil {
			continue
		}
		var exists bool
		if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version=$1)`, version).Scan(&exists); err != nil {
			return fmt.Errorf("check migration %d: %w", version, err)
		}
		if exists {
			continue
		}
		data, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		// Run migration in a transaction
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin tx for %d: %w", version, err)
		}
		if _, err := tx.Exec(ctx, string(data)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES ($1)`, version); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record %d: %w", version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit %d: %w", version, err)
		}
	}
	return nil
}
