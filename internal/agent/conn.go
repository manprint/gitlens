package agent

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/manprint/pglens/internal/check"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/identity"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/wire"
)

// ConnOptions configures the connection manager.
type ConnOptions struct {
	MaxConnsPerInstance int
	MaxDatabases        int
	IdleTimeout         time.Duration
	Include             []*regexp.Regexp
	Exclude             []*regexp.Regexp
}

// DefaultConnOptions returns sensible defaults (D22: 4 conns per instance from phase 7.1, 3 before).
func DefaultConnOptions() ConnOptions {
	return ConnOptions{
		MaxConnsPerInstance: 4,
		MaxDatabases:        10,
		IdleTimeout:         5 * time.Minute,
		Exclude: []*regexp.Regexp{
			regexp.MustCompile(`^template\d$`),
			regexp.MustCompile(`^rdsadmin$`),
			regexp.MustCompile(`^azure_.*$`),
		},
	}
}

// Manager owns a shared connection pool per target + LRU per-database pools.
type Manager struct {
	opts   ConnOptions
	target *poolHolder
	clock  clock.Clock
	mu     sync.Mutex
	dbs    map[string]*pgxpool.Pool // per-database pools
	dbtc   map[string]time.Time     // last access time per database
}

type capabilityConn interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// NewManager creates a connection manager for the given target DSN.
func NewManager(ctx context.Context, targetName string, dsn string, opts ConnOptions, clk clock.Clock) (*Manager, error) {
	if clk == nil {
		clk = clock.System()
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse DSN: %w", err)
	}
	cfg.MaxConns = int32(opts.MaxConnsPerInstance)
	cfg.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	m := &Manager{
		opts:   opts,
		target: &poolHolder{name: targetName, pool: pool, dsn: dsn},
		clock:  clk,
		dbs:    make(map[string]*pgxpool.Pool),
		dbtc:   make(map[string]time.Time),
	}
	return m, nil
}

// DedicatedConn acquires a connection from the pool for exclusive, long-lived
// use — the ASH sampler's 1-second loop, specifically (phase 7.1: "the
// sampler runs on its own dedicated connection, not the shared one," so a
// high-frequency loop never contends with the other checks for the same
// connection). Unlike Shared(), which callers acquire and release promptly,
// the caller here holds this connection until the sampler itself stops; the
// connection budget (DefaultConnOptions: 4) is already sized for this — 1
// shared, 1 ASH, 2 rotating per-database.
func (m *Manager) DedicatedConn(ctx context.Context) (*pgxpool.Conn, error) {
	conn, err := m.target.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Exec(ctx, "SET application_name = 'pglens/ash'"); err != nil {
		conn.Release()
		return nil, err
	}
	return conn, nil
}

// Shared returns the long-lived shared connection on the maintenance database.
func (m *Manager) Shared(ctx context.Context) (*pgxpool.Conn, error) {
	conn, err := m.target.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Exec(ctx, "SET application_name = 'pglens/shared'"); err != nil {
		conn.Release()
		return nil, err
	}
	if err := check.ApplySessionLimits(ctx, conn, 30*time.Second); err != nil {
		conn.Release()
		return nil, err
	}
	return conn, nil
}

// ForDatabase returns a connection to the named database, opening one on demand.
// Blocks up to context deadline when budget exhausted; never exceeds MaxConnsPerInstance.
func (m *Manager) ForDatabase(ctx context.Context, datname string) (*pgxpool.Conn, error) {
	m.mu.Lock()
	pool, exists := m.dbs[datname]
	if !exists {
		// Evict oldest idle database if at capacity.
		if len(m.dbs) >= m.opts.MaxDatabases {
			oldest := ""
			var oldestT time.Time
			for name, t := range m.dbtc {
				if oldest == "" || t.Before(oldestT) {
					oldest = name
					oldestT = t
				}
			}
			if oldest != "" {
				m.dbs[oldest].Close()
				delete(m.dbs, oldest)
				delete(m.dbtc, oldest)
			}
		}
		// Create new per-database pool.
		cfg, err := pgxpool.ParseConfig(m.target.dsn)
		if err != nil {
			m.mu.Unlock()
			return nil, fmt.Errorf("parse DSN for %s: %w", datname, err)
		}
		cfg.ConnConfig.Database = datname
		cfg.MaxConns = int32(m.opts.MaxConnsPerInstance)
		cfg.MinConns = 1
		pool, err = pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			m.mu.Unlock()
			return nil, fmt.Errorf("create pool for %s: %w", datname, err)
		}
		m.dbs[datname] = pool
	}
	m.dbtc[datname] = m.clock.Now()
	m.mu.Unlock()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf("SET application_name = 'pglens/%s'", datname)); err != nil {
		conn.Release()
		return nil, err
	}
	if err := check.ApplySessionLimits(ctx, conn, 30*time.Second); err != nil {
		conn.Release()
		return nil, err
	}
	return conn, nil
}

// Discover queries pg_database and pg_stat_database, applying selection rules.
// Returns a list of Database records (monitored + unmonitored with skip_reason).
func (m *Manager) Discover(ctx context.Context) ([]wire.Database, error) {
	conn, err := m.Shared(ctx)
	if err != nil {
		return nil, fmt.Errorf("get shared connection: %w", err)
	}
	defer conn.Release()

	rows, err := conn.Query(ctx, `
		SELECT d.datname, COALESCE(s.xact_commit, 0) AS xact_commit
		  FROM pg_database d
		  LEFT JOIN pg_stat_database s ON s.datname = d.datname
		 WHERE d.datallowconn AND NOT d.datistemplate
		 ORDER BY 2 DESC, 1 ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dbs []dbRow
	for rows.Next() {
		var name string
		var xactCommit int64
		if err := rows.Scan(&name, &xactCommit); err != nil {
			return nil, err
		}
		dbs = append(dbs, dbRow{name, xactCommit})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	selected := selectDatabases(dbs, m.opts)
	return selected, nil
}

// dbRow is an internal type for database discovery.
type dbRow struct {
	name       string
	xactCommit int64
}

// selectDatabases applies the selection rules from phase_05.md §4.1.
func selectDatabases(dbs []dbRow, opts ConnOptions) []wire.Database {
	// Step 1: drop Exclude matches.
	var filtered []dbRow
	for _, db := range dbs {
		skip := false
		for _, ex := range opts.Exclude {
			if ex.MatchString(db.name) {
				skip = true
				break
			}
		}
		if !skip {
			filtered = append(filtered, db)
		}
	}

	// Step 2: keep only Include matches (if any).
	if len(opts.Include) > 0 {
		var included []dbRow
		for _, db := range filtered {
			for _, inc := range opts.Include {
				if inc.MatchString(db.name) {
					included = append(included, db)
					break
				}
			}
		}
		filtered = included
	}

	// Step 3: budget: keep MaxDatabases by xact_commit, deterministic tie-break by name.
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].xactCommit != filtered[j].xactCommit {
			return filtered[i].xactCommit > filtered[j].xactCommit
		}
		return filtered[i].name < filtered[j].name
	})

	var result []wire.Database
	if len(filtered) <= opts.MaxDatabases {
		for _, db := range filtered {
			result = append(result, wire.Database{Name: db.name, Monitored: true})
		}
	} else {
		// Keep the top MaxDatabases; rest get skip_reason="db_budget".
		for i, db := range filtered {
			if i < opts.MaxDatabases {
				result = append(result, wire.Database{Name: db.name, Monitored: true})
			} else {
				result = append(result, wire.Database{
					Name:       db.name,
					Monitored:  false,
					SkipReason: "db_budget",
				})
			}
		}
	}
	return result
}

// Close closes all pools.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.target.pool.Close()
	for _, pool := range m.dbs {
		pool.Close()
	}
}

// Implement check.Target interface.
var _ check.Target = (*Manager)(nil)

// ensureCache populates the cache if not already done.
func (m *Manager) ensureCache(ctx context.Context) error {
	if m.target.cache.initialized {
		return nil
	}

	m.target.cache.mu.Lock()
	defer m.target.cache.mu.Unlock()

	if m.target.cache.initialized {
		return nil
	}

	conn, err := m.Shared(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	// Query server_version_num
	var versionNum int
	if err := conn.QueryRow(ctx, "SELECT current_setting('server_version_num')::int").Scan(&versionNum); err != nil {
		return fmt.Errorf("query version: %w", err)
	}
	m.target.cache.pgVersion = pgtype.PGVersion(versionNum)

	// Query pg_is_in_recovery
	var inRecovery bool
	if err := conn.QueryRow(ctx, "SELECT pg_is_in_recovery()").Scan(&inRecovery); err != nil {
		return fmt.Errorf("query role: %w", err)
	}
	m.target.cache.role = pgtype.RoleFromRecovery(inRecovery)

	// Query for system_identifier to derive ClusterID
	var systemID uint64
	err = conn.QueryRow(ctx, "SELECT system_identifier FROM pg_control_system()").Scan(&systemID)
	if err == nil {
		m.target.cache.clusterID = pgtype.ClusterID(systemID)
	} else {
		// Fallback to manual cluster ID from target name
		m.target.cache.clusterID = pgtype.ManualClusterID(m.target.name)
	}

	// Query permission tier and extensions. The initial snapshot is refreshed
	// again by RefreshCapabilities so a DBA grant takes effect on the next
	// scrape/command without an agent restart.
	m.target.cache.permTier, m.target.cache.extensions = m.queryCapabilities(ctx, conn)

	// Get or create InstanceID via identity store
	identityPath := os.Getenv("PGLENS_IDENTITY_PATH")
	if identityPath == "" {
		identityPath = "/var/lib/pglens/identity.json"
	}

	idStore, err := identity.Open(identityPath)
	if err != nil {
		return fmt.Errorf("open identity store: %w", err)
	}

	fingerprint, err := identity.Fingerprint(m.target.dsn)
	if err != nil {
		return fmt.Errorf("fingerprint DSN: %w", err)
	}

	instanceID, _, err := idStore.InstanceID(fingerprint)
	if err != nil {
		return fmt.Errorf("get instance id: %w", err)
	}
	m.target.cache.instanceID = instanceID
	m.target.cache.agentID = idStore.AgentID()

	m.target.cache.initialized = true
	return nil
}

// queryCapabilities reads the privileges and extensions relevant to checks
// and commands. Errors are treated conservatively: T0 and an empty extension
// set keep monitoring read-only instead of making an unavailable target look
// more capable than it is.
func (m *Manager) queryCapabilities(ctx context.Context, conn capabilityConn) (pgtype.PermTier, map[string]bool) {
	tier := pgtype.TierReadOnly
	var hasReadAllData bool
	if err := conn.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM pg_auth_members m
			JOIN pg_roles r ON m.roleid = r.oid
			WHERE r.rolname = 'pg_read_all_data'
			AND m.member = current_user::regrole::oid
		)
	`).Scan(&hasReadAllData); err == nil && hasReadAllData {
		tier = pgtype.TierExplain
	}

	var hasSignalBackend bool
	if err := conn.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM pg_auth_members m
			JOIN pg_roles r ON m.roleid = r.oid
			WHERE r.rolname = 'pg_signal_backend'
			AND m.member = current_user::regrole::oid
		)
	`).Scan(&hasSignalBackend); err == nil && hasSignalBackend {
		tier = pgtype.TierSignal
	}

	exts := make(map[string]bool)
	rows, err := conn.Query(ctx, "SELECT extname FROM pg_extension")
	if err != nil {
		return tier, exts
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil {
			exts[name] = true
		}
	}
	return tier, exts
}

// RefreshCapabilities re-reads privilege membership and installed
// extensions after the initial identity cache has been established. PostgreSQL
// grants are intended to become effective at the next scrape/command, so
// capability state must not be process-lifetime cached.
func (m *Manager) RefreshCapabilities(ctx context.Context) error {
	if err := m.ensureCache(ctx); err != nil {
		return err
	}
	conn, err := m.Shared(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	tier, extensions := m.queryCapabilities(ctx, conn)
	m.target.cache.mu.Lock()
	m.target.cache.permTier = tier
	m.target.cache.extensions = extensions
	m.target.cache.mu.Unlock()
	return nil
}

// AgentID returns this process's stable identity (internal/identity.Store's
// own agent_id, distinct from any single target's instance_id) — the value
// every pushed wire.Envelope should carry so the server can key per-agent
// state (e.g. revocation, SYS-AGENT-005) by the actual agent process rather
// than by whichever instance happened to flush first.
func (m *Manager) AgentID() uuid.UUID {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.ensureCache(ctx); err != nil {
		return uuid.Nil
	}
	m.target.cache.mu.RLock()
	defer m.target.cache.mu.RUnlock()
	return m.target.cache.agentID
}

func (m *Manager) InstanceID() pgtype.InstanceID {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.ensureCache(ctx); err != nil {
		return pgtype.InstanceID{}
	}
	m.target.cache.mu.RLock()
	defer m.target.cache.mu.RUnlock()
	return m.target.cache.instanceID
}

func (m *Manager) ClusterID() pgtype.ClusterID {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.ensureCache(ctx); err != nil {
		return 0
	}
	m.target.cache.mu.RLock()
	defer m.target.cache.mu.RUnlock()
	return m.target.cache.clusterID
}

func (m *Manager) Role() pgtype.Role {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.ensureCache(ctx); err != nil {
		return pgtype.RoleUnknown
	}
	m.target.cache.mu.RLock()
	defer m.target.cache.mu.RUnlock()
	return m.target.cache.role
}

// RefreshRole re-queries pg_is_in_recovery() and updates the cached role in
// place, independent of ensureCache's one-time initialization gate. Without
// this, a promoted standby (or a demoted primary) would report its
// original, now-stale role for the rest of the agent process's lifetime —
// found live building SYS-REPL-001 (the plan's own acceptance test): a real
// `pg_ctl promote` never showed up anywhere, because Role() only ever
// queries the database once, on the very first call, then serves the same
// cached value forever. This does not re-evaluate which checks are
// scheduled (addScheduleEntries still runs once at startup — a real,
// separate, larger gap, not fixed this session), but it does keep the
// pushed wire.Instance.Role current, which is what the server's topology
// engine (internal/server/pipeline.go) needs to ever detect a failover.
func (m *Manager) RefreshRole(ctx context.Context) error {
	conn, err := m.Shared(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	var inRecovery bool
	if err := conn.QueryRow(ctx, "SELECT pg_is_in_recovery()").Scan(&inRecovery); err != nil {
		return fmt.Errorf("query role: %w", err)
	}

	m.target.cache.mu.Lock()
	defer m.target.cache.mu.Unlock()
	m.target.cache.role = pgtype.RoleFromRecovery(inRecovery)
	return nil
}

func (m *Manager) PGVersion() pgtype.PGVersion {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.ensureCache(ctx); err != nil {
		return 0
	}
	m.target.cache.mu.RLock()
	defer m.target.cache.mu.RUnlock()
	return m.target.cache.pgVersion
}

func (m *Manager) PermTier() pgtype.PermTier {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.ensureCache(ctx); err != nil {
		return pgtype.TierReadOnly
	}
	m.target.cache.mu.RLock()
	defer m.target.cache.mu.RUnlock()
	return m.target.cache.permTier
}

func (m *Manager) HasExtension(name string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.ensureCache(ctx); err != nil {
		return false
	}
	m.target.cache.mu.RLock()
	defer m.target.cache.mu.RUnlock()
	return m.target.cache.extensions[name]
}
func (m *Manager) Conn(ctx context.Context) (check.Conn, error) {
	return m.Shared(ctx)
}
func (m *Manager) ConnFor(ctx context.Context, datname string) (check.Conn, error) {
	// Commands may omit datname when they target the database from the
	// instance DSN. ForDatabase would overwrite that database with an empty
	// string, which libpq interprets as the user name.
	if datname == "" {
		return m.Shared(ctx)
	}
	return m.ForDatabase(ctx, datname)
}
func (m *Manager) Database() string {
	return m.target.name
}

// TargetName returns the stable logical name from the agent configuration.
// It is carried in envelopes so the server can resolve replication receiver
// hostnames such as pg-standby-a even when the agent connects through a
// shared address with per-target ports.
func (m *Manager) TargetName() string {
	return m.target.name
}

// Addr and Port return the target's host and port, parsed from its DSN.
// Used to populate wire.Instance.Addr/Port — previously never set by any
// caller, which left the server's duplicate-instance detection
// (internal/server/inventory.go's checkDuplicateInstance, matched on
// tenant/cluster/addr/port) comparing empty strings and zeros for every
// instance instead of the real target address.
func (m *Manager) Addr() string {
	cfg, err := pgxpool.ParseConfig(m.target.dsn)
	if err != nil {
		return ""
	}
	return cfg.ConnConfig.Host
}

func (m *Manager) Port() int {
	cfg, err := pgxpool.ParseConfig(m.target.dsn)
	if err != nil {
		return 0
	}
	return int(cfg.ConnConfig.Port)
}
func (m *Manager) Clock() clock.Clock {
	return m.clock
}
