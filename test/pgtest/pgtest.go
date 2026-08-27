//go:build integration

package pgtest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	_ "github.com/lib/pq"
)

type PG struct {
	Major   int
	Version pgtype.PGVersion

	container *postgres.PostgresContainer
	dsn       string
	mu        sync.Mutex
}

var (
	globalMu  sync.Mutex
	pgByMajor = map[int]*PG{}
)

// ForEach runs fn once per version in Versions(), as a subtest named
// "pg15", "pg18", ... Subtests for different versions may run in parallel;
// subtests for the same version must not (see Lock).
func ForEach(t *testing.T, fn func(t *testing.T, pg *PG)) {
	t.Helper()
	for _, major := range Versions() {
		major := major
		t.Run(fmt.Sprintf("pg%d", major), func(t *testing.T) {
			t.Parallel()
			pg := getOrCreate(t, major)
			fn(t, pg)
		})
	}
}

func getOrCreate(t *testing.T, major int) *PG {
	t.Helper()
	globalMu.Lock()
	if pg, ok := pgByMajor[major]; ok {
		globalMu.Unlock()
		return pg
	}
	globalMu.Unlock()

	ctx := context.Background()
	img := images[major]
	_, filename, _, _ := runtime.Caller(0)
	baseDir := filepath.Dir(filename)
	script1 := filepath.Join(baseDir, "../fixtures/sql/00_extensions.sql")
	script2 := filepath.Join(baseDir, "../fixtures/sql/01_schema.sql")
	c, err := postgres.Run(ctx, img,
		postgres.WithDatabase("app"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("test"),
		postgres.WithInitScripts(script1, script2),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(90*time.Second)),
		testcontainers.WithCmd("postgres",
			"-c", "shared_preload_libraries=pg_stat_statements",
			"-c", "compute_query_id=on",
			"-c", "track_io_timing=on",
			"-c", "fsync=off",
			"-c", "full_page_writes=off",
			"-c", "synchronous_commit=off",
		),
	)
	require.NoError(t, err)

	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	// Setup roles and apply rds-like profile if needed
	setupRoles(ctx, pool)
	if profile := os.Getenv("PGLENS_PG_PROFILE"); profile == "rds-like" {
		_, fname, _, _ := runtime.Caller(0)
		script := filepath.Join(filepath.Dir(fname), "../fixtures/sql/02_rds_like.sql")
		if data, err := os.ReadFile(script); err == nil {
			_, _ = pool.Exec(ctx, string(data))
		}
	}
	var versionNum int
	err = pool.QueryRow(ctx, "SELECT current_setting('server_version_num')::int").Scan(&versionNum)
	require.NoError(t, err)
	pool.Close()
	// Take snapshot for Restore after roles are created
	require.NoError(t, c.Snapshot(ctx))

	pg := &PG{
		Major:     major,
		Version:   pgtype.PGVersion(versionNum),
		container: c,
		dsn:       dsn,
	}

	globalMu.Lock()
	if existing, ok := pgByMajor[major]; ok {
		_ = c.Terminate(ctx)
		globalMu.Unlock()
		return existing
	}
	pgByMajor[major] = pg
	globalMu.Unlock()

	return pg
}

func setupRoles(ctx context.Context, pool *pgxpool.Pool) {
	// Create test roles for permission tiers.
	// All passwords are 'test'.
	stmts := []string{
		`REVOKE EXECUTE ON FUNCTION pg_control_system() FROM PUBLIC`,
		`REVOKE EXECUTE ON FUNCTION pg_control_checkpoint() FROM PUBLIC`,
		`DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='perm_victim') THEN CREATE ROLE perm_victim LOGIN PASSWORD 'test'; END IF; END $$`,
		`DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='pglens') THEN CREATE ROLE pglens LOGIN PASSWORD 'test'; END IF; END $$`,
		`GRANT pg_monitor TO pglens`,
		`GRANT EXECUTE ON FUNCTION pg_control_system() TO pglens`,
		`GRANT EXECUTE ON FUNCTION pg_control_checkpoint() TO pglens`,
		`ALTER ROLE pglens SET statement_timeout = '15s'`,
		`ALTER ROLE pglens SET lock_timeout = '1s'`,
		`ALTER ROLE pglens SET idle_in_transaction_session_timeout = '30s'`,
		`ALTER ROLE pglens SET application_name = 'pglens'`,

		`DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='pglens_nocontrol') THEN CREATE ROLE pglens_nocontrol LOGIN PASSWORD 'test'; END IF; END $$`,
		`GRANT pg_monitor TO pglens_nocontrol`,

		`DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='pglens_t1') THEN CREATE ROLE pglens_t1 LOGIN PASSWORD 'test'; END IF; END $$`,
		`GRANT pg_monitor TO pglens_t1`,
		`GRANT EXECUTE ON FUNCTION pg_control_system() TO pglens_t1`,
		`GRANT EXECUTE ON FUNCTION pg_control_checkpoint() TO pglens_t1`,
		`GRANT pg_read_all_data TO pglens_t1`,

		`DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='pglens_t2') THEN CREATE ROLE pglens_t2 LOGIN PASSWORD 'test'; END IF; END $$`,
		`GRANT pg_monitor TO pglens_t2`,
		`GRANT EXECUTE ON FUNCTION pg_control_system() TO pglens_t2`,
		`GRANT EXECUTE ON FUNCTION pg_control_checkpoint() TO pglens_t2`,
		`GRANT pg_signal_backend TO pglens_t2`,
	}
	for _, s := range stmts {
		_, _ = pool.Exec(ctx, s)
	}
}

// Lock takes exclusive use of this container for the duration of the test
// and restores the snapshot on cleanup. Every test that touches the
// database must call it first.
func (p *PG) Lock(t *testing.T) {
	t.Helper()
	p.mu.Lock()
	t.Cleanup(func() {
		ctx := context.Background()
		if p.container != nil {
			if err := p.container.Restore(ctx); err != nil {
				t.Logf("restore failed: %v", err)
			}
		}
		p.mu.Unlock()
	})
}

// Pool returns a pool for datname as the given role. Closed automatically.
func (p *PG) Pool(t *testing.T, datname string, role Role) *pgxpool.Pool {
	t.Helper()
	dsn := p.dsnForRole(role, datname)
	ctx := context.Background()
	// Ensure database exists when datname is not app
	if datname != "" && datname != "app" {
		basePool, err := pgxpool.New(ctx, p.dsn)
		require.NoError(t, err)
		defer basePool.Close()
		_, _ = basePool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %q", datname))
		// Create extension in new db if needed
		pool2, err := pgxpool.New(ctx, dsn)
		if err == nil {
			_, _ = pool2.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_stat_statements")
			pool2.Close()
		}
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { pool.Close() })
	return pool
}

func (p *PG) dsnForRole(role Role, datname string) string {
	if role == RoleSuperuser {
		// superuser DSN is p.dsn directly, just adjust datname
		dsn := p.dsn
		if datname != "" && datname != "app" {
			dsn = strings.Replace(dsn, "/app?", "/"+datname+"?", 1)
			if !strings.Contains(dsn, "/"+datname+"?") {
				dsn = strings.Replace(dsn, "/app", "/"+datname, 1)
			}
		}
		return dsn
	}
	user := roleUser(role)
	pass := rolePassword(role)
	// Extract hostPort from p.dsn: find '@' and then '/'
	atIdx := strings.Index(p.dsn, "@")
	slashIdx := strings.Index(p.dsn[atIdx:], "/")
	if atIdx == -1 || slashIdx == -1 {
		return p.dsn
	}
	hostPort := p.dsn[atIdx+1 : atIdx+slashIdx]
	// Find query start
	qIdx := strings.Index(p.dsn, "?")
	query := ""
	if qIdx != -1 {
		query = p.dsn[qIdx:] // includes ?
	} else {
		query = "?sslmode=disable"
	}
	db := datname
	if db == "" {
		db = "app"
	}
	return fmt.Sprintf("postgres://%s:%s@%s/%s%s", user, pass, hostPort, db, query)
}

// Exec executes SQL on the default database.
func (p *PG) Exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	pool := p.Pool(t, "app", 0)
	_, err := pool.Exec(context.Background(), sql, args...)
	require.NoError(t, err)
}

// DSN returns a DSN for datname.
func (p *PG) DSN(datname string, role Role) string {
	return p.dsnForRole(role, datname)
}
