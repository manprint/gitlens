package store

import (
	"context"
	"embed"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// migrationName is the file name every .sql migration must have: a four digit
// version, an underscore, a name and the .sql suffix.
var migrationName = regexp.MustCompile(`^([0-9]{4})_.+\.sql$`)

// migration is one migration file and the version it applies.
type migration struct {
	version int
	name    string
}

// migrationUnlockTimeout bounds the unlock after the migrations ran. It starts
// when the unlock does, not when the lock was taken: a long migration must not
// use up the unlock's budget.
var migrationUnlockTimeout = 5 * time.Second

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
	defer func() {
		// The unlock runs after the migrations, possibly past the caller's
		// deadline, so it gets its own budget. If it fails, the lock must not
		// outlive the call on a pooled session that other replicas wait on:
		// closing the connection ends the session and releases the lock.
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), migrationUnlockTimeout)
		defer cancel()
		if _, err := conn.Exec(unlockCtx, `SELECT pg_advisory_unlock(hashtext('pglens:migrations'))`); err != nil {
			_ = conn.Conn().Close(unlockCtx)
		}
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
	migrations, err := parseMigrations(files)
	if err != nil {
		return err
	}
	for _, m := range migrations {
		var exists bool
		if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version=$1)`, m.version).Scan(&exists); err != nil {
			return fmt.Errorf("check migration %d: %w", m.version, err)
		}
		if exists {
			continue
		}
		data, err := migrationFS.ReadFile("migrations/" + m.name)
		if err != nil {
			return fmt.Errorf("read %s: %w", m.name, err)
		}
		// Run migration in a transaction
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin tx for %d: %w", m.version, err)
		}
		if _, err := tx.Exec(ctx, string(data)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply %s: %w", m.name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES ($1)`, m.version); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record %d: %w", m.version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit %d: %w", m.version, err)
		}
	}
	return nil
}

// parseMigrations checks every migration name and returns the migrations in
// version order. A name that does not match, or a version that two files
// share, is an error rather than a skip: a skipped file is a schema change the
// database never receives, and the server would run without it and say nothing.
// It runs over the whole set before any migration is applied.
func parseMigrations(names []string) ([]migration, error) {
	seen := make(map[int]string, len(names))
	out := make([]migration, 0, len(names))
	for _, name := range names {
		match := migrationName.FindStringSubmatch(name)
		if match == nil {
			return nil, fmt.Errorf("migration %s: name must be NNNN_name.sql", name)
		}
		version, err := strconv.Atoi(match[1])
		if err != nil {
			return nil, fmt.Errorf("migration %s: %w", name, err)
		}
		if other, dup := seen[version]; dup {
			return nil, fmt.Errorf("migrations %s and %s both have version %d", other, name, version)
		}
		seen[version] = name
		out = append(out, migration{version: version, name: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}
