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
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"

	_ "github.com/lib/pq"
)

type PG struct {
	Major   int
	Version pgtype.PGVersion

	container *postgres.PostgresContainer
	// genericContainer is set instead of container for a PrimaryStandby()
	// member (GenericContainer, not the postgres module — see
	// PrimaryStandby's own comment for why). Lets callers actually stop the
	// standby mid-test, e.g. to prove a replication slot's retained_bytes
	// grows once nothing is consuming its WAL (INT-REPL-004).
	genericContainer testcontainers.Container
	dsn              string
	mu               sync.Mutex
}

// Stop stops (does not remove) the underlying container — only meaningful
// for a PG returned by PrimaryStandby(); a no-op otherwise. Used to
// simulate a standby going away without tearing down the whole pair.
func (p *PG) Stop(t *testing.T) {
	t.Helper()
	if p.genericContainer == nil {
		return
	}
	require.NoError(t, p.genericContainer.Stop(context.Background(), nil))
}

var (
	globalMu  sync.Mutex
	pgByMajor = map[int]*PG{}

	// pgPrimaryStandbyMu and pgPrimaryStandbyByMajor cache primary-standby pairs
	// per major version. Unlike single instances, we do NOT snapshot/restore
	// these between tests, since restoring the primary alone would desync it from
	// the standby. Instead, we tear down and recreate the pair for each test that
	// calls PrimaryStandby. This is slower but avoids complex cleanup logic.
	pgPrimaryStandbyMu      sync.Mutex
	pgPrimaryStandbyByMajor = map[int]*PrimaryStandbyPair{}
)

// PrimaryStandbyPair holds both a primary and standby PostgreSQL instance.
type PrimaryStandbyPair struct {
	Primary          *PG
	Standby          *PG
	network          testcontainers.Network
	primaryContainer *postgres.PostgresContainer
	standbyContainer testcontainers.Container
}

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

// PrimaryStandby creates a fresh primary-standby pair for a single test.
// It sets up streaming replication via pg_basebackup -R and a physical
// replication slot. The pair is NOT cached; each test gets a new one and is
// responsible for calling t.Cleanup to tear it down.
//
// NOTE: Unlike single-instance containers which snapshot/restore, we don't do
// that for the pair because restoring the primary alone would desync from the
// standby. Each test gets a full fresh setup.
func PrimaryStandby(t *testing.T, major int) (*PG, *PG) {
	t.Helper()

	ctx := context.Background()
	img := images[major]

	// Create a shared network for primary and standby to communicate
	net, err := network.New(ctx, network.WithLabels(map[string]string{
		"test": "pglens-repl",
	}))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = net.Remove(context.Background())
	})

	// ============ PRIMARY CONTAINER ============
	// The primary needs wal_level=replica, max_wal_senders, hot_standby=on,
	// plus pg_hba.conf modification for replication, and a replication slot.

	// Inline script to set up replication on the primary
	replicationSetupScript := `
#!/bin/bash
set -e

# Wait for PostgreSQL to be ready
until pg_isready -U postgres 2>/dev/null; do
  echo "Waiting for PostgreSQL..."
  sleep 1
done

# Allow the standby to connect via replication
PGDATA="${PGDATA:-/var/lib/postgresql/data}"
echo "host replication all all trust" >> "$PGDATA/pg_hba.conf"
psql -U postgres -d postgres -c "SELECT pg_reload_conf();"

# Create the replication slot
psql -U postgres -d postgres -c "SELECT pg_create_physical_replication_slot('standby1', true);"

echo "Primary replication setup complete"
`

	// Note: Cannot use postgres.Run() for primary because it doesn't support network
	// configuration. Use GenericContainer instead.
	primaryReq := testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        img,
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_DB":       "app",
				"POSTGRES_USER":     "postgres",
				"POSTGRES_PASSWORD": "test",
			},
			Networks:       []string{net.Name},
			NetworkAliases: map[string][]string{net.Name: {"primary"}},
			Cmd: []string{"postgres",
				"-c", "shared_preload_libraries=pg_stat_statements",
				"-c", "compute_query_id=on",
				"-c", "track_io_timing=on",
				"-c", "fsync=off",
				"-c", "full_page_writes=off",
				"-c", "synchronous_commit=off",
				"-c", "wal_level=replica",
				"-c", "max_wal_senders=10",
				"-c", "hot_standby=on",
			},
			WaitingFor: wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(90 * time.Second),
			Files: []testcontainers.ContainerFile{
				{
					Reader:            strings.NewReader(replicationSetupScript),
					ContainerFilePath: "/init_primary.sh",
					FileMode:          0o755,
				},
			},
		},
		Started: true,
	}

	primaryC, err := testcontainers.GenericContainer(ctx, primaryReq)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = primaryC.Terminate(context.Background())
	})

	// Run the replication setup script on the primary
	exitCode, reader, err := primaryC.Exec(ctx, []string{"sh", "/init_primary.sh"})
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "replication setup script failed")
	if rc, ok := reader.(interface{ Close() error }); ok {
		_ = rc.Close()
	}

	// Get primary connection info
	primaryHost, err := primaryC.Host(ctx)
	require.NoError(t, err)
	primaryPort, err := primaryC.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)
	primaryDSN := fmt.Sprintf("postgres://postgres:test@%s:%s/app?sslmode=disable", primaryHost, primaryPort.Port())

	// Set up roles on primary
	primaryPool, err := pgxpool.New(ctx, primaryDSN)
	require.NoError(t, err)
	setupRoles(ctx, primaryPool)
	primaryPool.Close()

	// ============ STANDBY CONTAINER ============
	// The standby uses a custom entrypoint that runs pg_basebackup -R,
	// then starts postgres.

	standbySetupScript := `
#!/bin/bash
set -e

PGDATA="${PGDATA:-/var/lib/postgresql/data}"

# If PGDATA is empty, bootstrap from primary via pg_basebackup
if [ ! -f "$PGDATA/PG_VERSION" ]; then
  echo "Initializing standby from primary..."

  # Wait for primary to be ready and accepting replication connections
  RETRIES=60
  while ! pg_isready -h primary -U postgres -d postgres 2>/dev/null; do
    RETRIES=$((RETRIES - 1))
    if [ $RETRIES -le 0 ]; then
      echo "Primary did not become ready in time"
      exit 1
    fi
    sleep 1
  done

  echo "Primary is ready, running pg_basebackup..."

  # Run pg_basebackup with PGAPPNAME set to pg-standby, -R to auto-create
  # recovery files, and -S standby1 to associate the standby with the
  # physical slot created on the primary above. Without -S, -R writes
  # primary_conninfo but no primary_slot_name: the standby still streams
  # fine over a plain (non-slot) connection, but the pre-created slot never
  # actually gets used, so pg_replication_slots.active stays permanently
  # false — found live via INT-REPL-004, which polls for the slot to
  # become active and never observed it without this flag.
  PGAPPNAME=pg-standby pg_basebackup -h primary -U postgres -D "$PGDATA" -Fp -Xs -R -S standby1

  # Fix permissions: this script runs as root
  chmod 700 "$PGDATA"
  chown -R postgres:postgres "$PGDATA"

  echo "Standby initialized from primary"
else
  echo "PGDATA already exists, skipping bootstrap"
fi

# Start PostgreSQL as the unprivileged postgres user
exec gosu postgres postgres
`

	standbyReq := testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        img,
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_PASSWORD": "test",
				"POSTGRES_USER":     "postgres",
				"POSTGRES_DB":       "postgres",
				"PGDATA":            "/var/lib/postgresql/data",
			},
			Networks:       []string{net.Name},
			NetworkAliases: map[string][]string{net.Name: {"standby"}},
			// Standby doesn't emit "ready for read-write" message like primary.
			// Wait for "consistent recovery state reached" which means standby recovered and is ready.
			WaitingFor: wait.ForLog("consistent recovery state reached").
				WithStartupTimeout(120 * time.Second),
			// Override entrypoint to run our custom script
			Entrypoint: []string{"sh", "-c", standbySetupScript},
		},
		Started: true,
	}

	standbyC, err := testcontainers.GenericContainer(ctx, standbyReq)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = standbyC.Terminate(context.Background())
	})

	standbyHost, err := standbyC.Host(ctx)
	require.NoError(t, err)
	standbyPort, err := standbyC.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)
	standbyDSN := fmt.Sprintf("postgres://postgres:test@%s:%s/app?sslmode=disable", standbyHost, standbyPort.Port())

	// Set up roles on standby
	standbyPool, err := pgxpool.New(ctx, standbyDSN)
	require.NoError(t, err)
	setupRoles(ctx, standbyPool)
	standbyPool.Close()

	// Get version info for both
	primaryPool, err = pgxpool.New(ctx, primaryDSN)
	require.NoError(t, err)
	var primaryVersionNum int
	err = primaryPool.QueryRow(ctx, "SELECT current_setting('server_version_num')::int").Scan(&primaryVersionNum)
	require.NoError(t, err)
	primaryPool.Close()

	standbyPool, err = pgxpool.New(ctx, standbyDSN)
	require.NoError(t, err)
	var standbyVersionNum int
	err = standbyPool.QueryRow(ctx, "SELECT current_setting('server_version_num')::int").Scan(&standbyVersionNum)
	require.NoError(t, err)
	standbyPool.Close()

	primaryPG := &PG{
		Major:            major,
		Version:          pgtype.PGVersion(primaryVersionNum),
		container:        nil, // Primary is GenericContainer, can't snapshot
		genericContainer: primaryC,
		dsn:              primaryDSN,
	}

	standbyPG := &PG{
		Major:            major,
		Version:          pgtype.PGVersion(standbyVersionNum),
		container:        nil, // Standby is not managed via postgres module, no snapshot
		genericContainer: standbyC,
		dsn:              standbyDSN,
	}

	return primaryPG, standbyPG
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
