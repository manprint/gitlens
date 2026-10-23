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
	// MaxConnsPerInstance bounds the pool on the DSN's own (maintenance)
	// database: the shared connection, the ASH sampler's dedicated one and
	// the instance-scoped checks.
	MaxConnsPerInstance int
	MaxDatabases        int
	// IdleTimeout closes per-database connections that have not been used
	// for this long, so a monitored database costs a connection only while
	// its checks actually run.
	IdleTimeout time.Duration
	Include     []*regexp.Regexp
	Exclude     []*regexp.Regexp
	// ClusterName is the fallback cluster identity used when
	// pg_control_system() is not readable (targets[].cluster_name). Empty
	// falls back to the target name.
	ClusterName string
}

// maxConnsPerDatabase bounds each per-database pool. The database-scoped
// checks (statements, tables, indexes, bloat) and the occasional on-demand
// command are the only users, and two lets a long bloat scan run without
// stalling the others.
const maxConnsPerDatabase = 2

// DefaultConnOptions returns sensible defaults (D22: 4 conns per instance from phase 7.1, 3 before).
func DefaultConnOptions() ConnOptions {
	return ConnOptions{
		MaxConnsPerInstance: 4,
		MaxDatabases:        10,
		IdleTimeout:         5 * time.Minute,
		Exclude:             DefaultDatabaseExcludes(),
	}
}

// DefaultDatabaseExcludes is the exclude list applied when a target does not
// configure databases.exclude itself: templates and the managed-service
// administrative databases, which the monitoring role must not wander into.
func DefaultDatabaseExcludes() []*regexp.Regexp {
	return []*regexp.Regexp{
		regexp.MustCompile(`^template\d$`),
		regexp.MustCompile(`^rdsadmin$`),
		regexp.MustCompile(`^azure_.*$`),
	}
}

// ReadOnlySession runs on every physical connection the agent opens, before
// any caller can use it: the Manager's pools and the check command's single
// connection. pglens only ever reads from a monitored instance, so the session
// sets default_transaction_read_only: every transaction, implicit or explicit,
// from a check, from ASH or from an on-demand command, then starts READ ONLY,
// and a write the agent issued by mistake fails inside PostgreSQL. That
// parameter is a setting any session can change back, so it guards against a
// bug in the agent and is not the boundary. The boundary is the monitoring
// role: deploy/sql/monitoring_user.sql grants it reads only and refuses a role
// that could write, and the EXPLAIN path adds its own READ ONLY transaction
// (exec_explain.go). The statements are constants, so the simple protocol pgx
// uses for an argument-less Exec carries no injection risk.
//
// The session also searches pg_catalog alone. A search_path set for the
// database or the role (ALTER DATABASE ... SET search_path, which a database
// owner may run) that lists a schema before pg_catalog would otherwise let
// that schema's functions and operators stand in for the built-ins the
// agent's queries call, and run as the monitoring role, with its signal and
// read privileges. Objects an extension created are named with their schema
// instead (check.ExtensionSchema); EXPLAIN alone plans the user's statement
// with the database's own search_path, behind its guard.
func ReadOnlySession(ctx context.Context, conn *pgx.Conn) error {
	if _, err := conn.Exec(ctx, "SET default_transaction_read_only = on"); err != nil {
		return fmt.Errorf("enforce read-only session: %w", err)
	}
	if _, err := conn.Exec(ctx, "SET search_path = pg_catalog, pg_temp"); err != nil {
		return fmt.Errorf("restrict session search_path: %w", err)
	}
	return nil
}

// Manager owns a shared connection pool per target + LRU per-database pools.
type Manager struct {
	opts   ConnOptions
	target *poolHolder
	clock  clock.Clock
	mu     sync.Mutex
	dbs    map[string]*pgxpool.Pool // per-database pools
	dbtc   map[string]time.Time     // last access time per database
	// pending counts, per database, the ForDatabase calls that have picked its
	// pool and are still waiting to acquire from it. See ForDatabase.
	pending map[string]int
	// statementsDB is the database whose pg_stat_statements view stands in
	// for databases without one; empty means the maintenance database. See
	// LocateStatements.
	statementsDB string
	// monitored holds the databases the latest discovery selected, and ownDB is
	// the one the DSN names. ConnFor, which commands use, opens a pool only for
	// these; see requireMonitored. scheduled holds the databases the running
	// check schedule was built for; see SetScheduledDatabases and
	// ScheduledConnFor.
	monitored map[string]bool
	scheduled map[string]bool
	ownDB     string

	endpointOnce sync.Once
	addr         string
	port         int
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
	cfg.AfterConnect = ReadOnlySession
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	m := &Manager{
		opts:      opts,
		target:    &poolHolder{name: targetName, pool: pool, dsn: dsn},
		clock:     clk,
		dbs:       make(map[string]*pgxpool.Pool),
		dbtc:      make(map[string]time.Time),
		pending:   make(map[string]int),
		monitored: make(map[string]bool),
		ownDB:     cfg.ConnConfig.Database,
	}
	return m, nil
}

// DedicatedConn acquires a connection from the pool for exclusive, long-lived
// use — the ASH sampler's 1-second loop, specifically (phase 7.1: "the
// sampler runs on its own dedicated connection, not the shared one," so a
// high-frequency loop never contends with the other checks for the same
// connection). Unlike Shared(), which callers acquire and release promptly,
// the caller here holds this connection until the sampler itself stops; the
// maintenance-database budget (DefaultConnOptions: 4) is sized for this — 1
// ASH plus up to 3 for the shared connection and instance-scoped checks.
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
// Each database has its own small pool (maxConnsPerDatabase, never more than
// MaxConnsPerInstance) that keeps no idle minimum and closes connections
// after IdleTimeout; Acquire blocks up to the context deadline when that pool
// is exhausted.
func (m *Manager) ForDatabase(ctx context.Context, datname string) (*pgxpool.Conn, error) {
	m.mu.Lock()
	pool, exists := m.dbs[datname]
	if !exists {
		// Evict the least recently used database while at capacity, but
		// never one whose pool has a connection checked out or a caller still
		// waiting to acquire from it: closing that pool fails the caller with
		// a closed pool. When every database is busy the new one is opened
		// anyway, so the bound is nominal until those connections are released.
		for len(m.dbs) >= m.opts.MaxDatabases {
			oldest := ""
			var oldestT time.Time
			for name, t := range m.dbtc {
				if m.pending[name] > 0 || m.dbs[name].Stat().AcquiredConns() > 0 {
					continue
				}
				if oldest == "" || t.Before(oldestT) {
					oldest = name
					oldestT = t
				}
			}
			if oldest == "" {
				break
			}
			// pgxpool.Close waits for every acquired connection to be
			// released; doing that under m.mu stalled every other
			// database lookup behind a check still scanning the evicted
			// database.
			evicted := m.dbs[oldest]
			delete(m.dbs, oldest)
			delete(m.dbtc, oldest)
			go evicted.Close()
		}
		// Create new per-database pool.
		cfg, err := pgxpool.ParseConfig(m.target.dsn)
		if err != nil {
			m.mu.Unlock()
			return nil, fmt.Errorf("parse DSN for %s: %w", datname, err)
		}
		cfg.ConnConfig.Database = datname
		cfg.MaxConns = int32(perDatabaseMaxConns(m.opts))
		cfg.MinConns = 0
		if m.opts.IdleTimeout > 0 {
			cfg.MaxConnIdleTime = m.opts.IdleTimeout
		}
		// The database name comes from pg_database, i.e. from whoever may
		// CREATE DATABASE on the monitored instance. It used to be spliced
		// into `SET application_name = '...'` and sent through the simple
		// protocol, where a name containing a quote closed the literal and
		// ran whatever followed as the monitoring role. A startup parameter
		// is a plain key/value pair that is never parsed as SQL.
		cfg.ConnConfig.RuntimeParams["application_name"] = "pglens/" + datname
		cfg.AfterConnect = ReadOnlySession
		pool, err = pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			m.mu.Unlock()
			return nil, fmt.Errorf("create pool for %s: %w", datname, err)
		}
		m.dbs[datname] = pool
	}
	m.dbtc[datname] = m.clock.Now()
	m.pending[datname]++
	m.mu.Unlock()

	conn, err := pool.Acquire(ctx)
	m.mu.Lock()
	m.pending[datname]--
	if m.pending[datname] == 0 {
		delete(m.pending, datname)
	}
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if err := check.ApplySessionLimits(ctx, conn, 30*time.Second); err != nil {
		conn.Release()
		return nil, err
	}
	return conn, nil
}

func perDatabaseMaxConns(opts ConnOptions) int {
	n := maxConnsPerDatabase
	if opts.MaxConnsPerInstance > 0 && opts.MaxConnsPerInstance < n {
		n = opts.MaxConnsPerInstance
	}
	return n
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
	latest := make(map[string]bool, len(selected))
	for _, db := range selected {
		if db.Monitored {
			latest[db.Name] = true
		}
	}
	// Each discovery replaces the set a command may open, so a database the
	// budget ranks out is refused to commands. The first discovery also seeds
	// the scheduled set, which SetScheduledDatabases replaces with the set of
	// each schedule built; a scheduled check keeps its database when the budget
	// later ranks it out.
	m.mu.Lock()
	m.monitored = latest
	if m.scheduled == nil {
		m.scheduled = latest
	}
	m.mu.Unlock()
	return selected, nil
}

// dbRow is an internal type for database discovery.
type dbRow struct {
	name       string
	xactCommit int64
}

// DatabaseCandidate is a connectable database with the commit count the
// database budget ranks by.
type DatabaseCandidate struct {
	Name       string
	XactCommit int64
}

// SelectDatabases applies Discover's selection rules to candidates found by
// someone else, so the check command reports the choice the agent makes
// instead of calling every database monitored. Excluded databases are left
// out of the result, as Discover leaves them out.
func SelectDatabases(candidates []DatabaseCandidate, opts ConnOptions) []wire.Database {
	dbs := make([]dbRow, 0, len(candidates))
	for _, c := range candidates {
		dbs = append(dbs, dbRow{c.Name, c.XactCommit})
	}
	return selectDatabases(dbs, opts)
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

// cacheReady reports whether the identity cache has been populated. It takes
// the read lock: reading `initialized` unlocked (as the classic
// double-checked-locking shape used to) is a data race under Go's memory
// model, and not a benign one — a reader could observe the flag set while
// the fields it guards were still invisible to it, handing out a zero
// InstanceID or a RoleUnknown that the caller has no way to tell apart from
// a genuine one.
func (m *Manager) cacheReady() bool {
	m.target.cache.mu.RLock()
	defer m.target.cache.mu.RUnlock()
	return m.target.cache.initialized
}

// ensureCache populates the cache if not already done.
//
// The discovery queries run under initMu and NOT under cache.mu. Holding the
// write lock across a pool acquisition plus five round trips meant every
// accessor — Role(), PGVersion(), HasExtension(), each called per scrape from
// its own goroutine — blocked on RLock for as long as that took. Worse, with
// a bounded pool (MaxConnsPerInstance: 4) it is a genuine deadlock shape:
// a check that already holds a pooled connection and then calls PGVersion()
// (checkpointer.go does exactly this) waits on the lock, while the holder of
// the lock waits for a connection that only that check can give back.
func (m *Manager) ensureCache(ctx context.Context) error {
	if m.cacheReady() {
		return nil
	}

	m.target.cache.initMu.Lock()
	defer m.target.cache.initMu.Unlock()

	if m.cacheReady() {
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

	// Query pg_is_in_recovery
	var inRecovery bool
	if err := conn.QueryRow(ctx, "SELECT pg_is_in_recovery()").Scan(&inRecovery); err != nil {
		return fmt.Errorf("query role: %w", err)
	}

	// Query for system_identifier to derive ClusterID
	clusterID, idSource, err := queryClusterIdentity(ctx, conn, m.fallbackClusterName())
	if err != nil {
		return err
	}

	// Query permission tier and extensions. RefreshCapabilities re-reads them
	// on every scrape/command, so a DBA grant takes effect without an agent
	// restart. The cluster identity above is not re-read: it is decided once
	// per process.
	permTier, extensions, err := m.queryCapabilities(ctx, conn)
	if err != nil {
		// Nothing was read before this, so the conservative state stands in
		// until RefreshCapabilities reads the grants again.
		permTier, extensions = pgtype.TierReadOnly, map[string]bool{}
	}

	// A target is not usable without its identity: every envelope carries it.
	if _, _, err := m.localIdentity(); err != nil {
		return err
	}

	// One critical section, after every query has succeeded: a partially
	// populated cache is never observable, and `initialized` is published
	// together with the fields it promises.
	m.target.cache.mu.Lock()
	m.target.cache.pgVersion = pgtype.PGVersion(versionNum)
	m.target.cache.role = pgtype.RoleFromRecovery(inRecovery)
	m.target.cache.clusterID = clusterID
	m.target.cache.idSource = idSource
	m.target.cache.permTier = permTier
	m.target.cache.extensions = extensions
	m.target.cache.initialized = true
	m.target.cache.mu.Unlock()
	return nil
}

// queryClusterIdentity reads the cluster's system_identifier. Only a
// permission denial falls back to the manual id. A denial is kept for the
// life of the process, so a manual id is never replaced by a later read: the
// id a cluster was reported under cannot change (invariant I-1), and a grant
// takes effect after a restart. Any other failure, such as a timeout or a
// reset connection, is returned and nothing is cached, so the next call asks
// again: latching the manual id on a transient error would split one cluster
// into several ids.
func queryClusterIdentity(ctx context.Context, conn capabilityConn, fallbackName string) (pgtype.ClusterID, pgtype.IDSource, error) {
	var systemID uint64
	err := conn.QueryRow(ctx, "SELECT system_identifier FROM pg_control_system()").Scan(&systemID)
	if err == nil {
		return pgtype.ClusterID(systemID), pgtype.IDSourceSystemIdentifier, nil
	}
	if !isInsufficientPrivilege(err) {
		return 0, "", fmt.Errorf("query system identifier: %w", err)
	}
	// Fall back to the configured cluster_name (documented), or to the
	// target name when none is configured. The target name alone used to
	// be the only fallback, so the cluster_name every instance of a
	// cluster was told to share was silently ignored and each target
	// became its own cluster.
	return pgtype.ManualClusterID(fallbackName), pgtype.IDSourceManual, nil
}

// queryCapabilities reads the privileges and extensions relevant to checks
// and commands. A failed read returns its error and no partial answer. A tier
// or extension set that is only incomplete would look like a change, so the
// caller decides what to keep.
func (m *Manager) queryCapabilities(ctx context.Context, conn capabilityConn) (pgtype.PermTier, map[string]bool, error) {
	// pg_has_role asks whether the session can use the privilege, through
	// group roles and with the membership's inherit option applied. A direct
	// membership lookup missed a privilege inherited from a group role, and it
	// kept a NOINHERIT role at its tier on PostgreSQL 15, where that role
	// cannot use the privilege. pg_has_role is true for every role in a
	// superuser session, so a superuser is kept at T0: the tier describes the
	// monitoring role, and the deploy script refuses a superuser as one.
	tier := pgtype.TierReadOnly
	var hasReadAllData bool
	if err := conn.QueryRow(ctx, `SELECT pg_catalog.pg_has_role('pg_read_all_data', 'USAGE') AND pg_catalog.current_setting('is_superuser') = 'off'`).Scan(&hasReadAllData); err != nil {
		return pgtype.TierReadOnly, nil, fmt.Errorf("query pg_read_all_data membership: %w", err)
	}
	if hasReadAllData {
		tier = pgtype.TierExplain
	}

	var hasSignalBackend bool
	if err := conn.QueryRow(ctx, `SELECT pg_catalog.pg_has_role('pg_signal_backend', 'USAGE') AND pg_catalog.current_setting('is_superuser') = 'off'`).Scan(&hasSignalBackend); err != nil {
		return pgtype.TierReadOnly, nil, fmt.Errorf("query pg_signal_backend membership: %w", err)
	}
	if hasSignalBackend {
		tier = pgtype.TierSignal
	}

	exts := make(map[string]bool)
	rows, err := conn.Query(ctx, "SELECT extname FROM pg_extension")
	if err != nil {
		return pgtype.TierReadOnly, nil, fmt.Errorf("query extensions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return pgtype.TierReadOnly, nil, fmt.Errorf("scan extension: %w", err)
		}
		exts[name] = true
	}
	if err := rows.Err(); err != nil {
		return pgtype.TierReadOnly, nil, fmt.Errorf("read extensions: %w", err)
	}
	return tier, exts, nil
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
	return m.refreshCapabilities(ctx, conn)
}

// refreshCapabilities replaces the cached tier and extensions with what conn
// reads now. A failed read leaves the last snapshot in place. A transient error
// must not drop the tier to read-only, because the schedule rebuilds on a change
// to it, and the command dispatcher refuses a command whose refresh failed.
func (m *Manager) refreshCapabilities(ctx context.Context, conn capabilityConn) error {
	tier, extensions, err := m.queryCapabilities(ctx, conn)
	if err != nil {
		return fmt.Errorf("refresh capabilities: %w", err)
	}
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
//
// Neither identity needs the target: both live in the local identity file.
// They used to be read only after the first successful connection, so an
// agent whose first target was down at startup handed the command
// dispatcher a nil agent id (commands disabled until restart) and registered
// every unreachable target under the zero instance id.
func (m *Manager) AgentID() uuid.UUID {
	store, err := identityStore()
	if err != nil {
		return uuid.Nil
	}
	return store.AgentID()
}

func (m *Manager) InstanceID() pgtype.InstanceID {
	instanceID, _, err := m.localIdentity()
	if err != nil {
		return pgtype.InstanceID{}
	}
	return instanceID
}

// localIdentity reads this target's instance id and the agent id from the
// process-wide identity store, allocating the instance id on first sight.
func (m *Manager) localIdentity() (pgtype.InstanceID, uuid.UUID, error) {
	store, err := identityStore()
	if err != nil {
		return pgtype.InstanceID{}, uuid.Nil, fmt.Errorf("open identity store: %w", err)
	}
	instanceID, _, err := store.InstanceIDForDSN(m.target.dsn)
	if err != nil {
		return pgtype.InstanceID{}, uuid.Nil, fmt.Errorf("get instance id: %w", err)
	}
	return instanceID, store.AgentID(), nil
}

var (
	identityMu     sync.Mutex
	identityStores = map[string]*identity.Store{}
)

// identityStore returns the one identity.Store this process uses for
// PGLENS_IDENTITY_PATH. Each Manager used to open its own copy of the file:
// two targets initialising at the same time each allocated an instance id in
// their private copy, the second atomic rename erased the first one's entry,
// and that instance came back under a new id after the next restart (and on
// a fresh install both copies could even mint different agent ids).
func identityStore() (*identity.Store, error) {
	path := os.Getenv("PGLENS_IDENTITY_PATH")
	if path == "" {
		path = "/var/lib/pglens/identity.json"
	}
	identityMu.Lock()
	defer identityMu.Unlock()
	if store, ok := identityStores[path]; ok {
		return store, nil
	}
	store, err := identity.Open(path)
	if err != nil {
		return nil, err
	}
	identityStores[path] = store
	return store, nil
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

// ClusterIDSource reports how ClusterID was derived: from the instance's
// system_identifier, or manually from cluster_name/the target name.
func (m *Manager) ClusterIDSource() pgtype.IDSource {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.ensureCache(ctx); err != nil {
		return pgtype.IDSourceManual
	}
	m.target.cache.mu.RLock()
	defer m.target.cache.mu.RUnlock()
	if m.target.cache.idSource == "" {
		return pgtype.IDSourceSystemIdentifier
	}
	return m.target.cache.idSource
}

func (m *Manager) fallbackClusterName() string {
	if m.opts.ClusterName != "" {
		return m.opts.ClusterName
	}
	return m.target.name
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
// cached value forever. The checks are re-evaluated by the schedule reconcile
// (cmd/pglens-agent/schedule.go, reconcile), not by this call. This keeps the
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

// SessionIsSuperuser reports whether the monitoring role of this target is a
// superuser. The agent does not refuse one: it logs a warning at start, and the
// security advisor reports the role as critical.
func (m *Manager) SessionIsSuperuser(ctx context.Context) (bool, error) {
	conn, err := m.Shared(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Release()
	var superuser bool
	if err := conn.QueryRow(ctx, "SELECT rolsuper FROM pg_catalog.pg_roles WHERE rolname = current_user").Scan(&superuser); err != nil {
		return false, fmt.Errorf("query rolsuper: %w", err)
	}
	return superuser, nil
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
	if err := m.requireMonitored(ctx, datname); err != nil {
		return nil, err
	}
	return m.ForDatabase(ctx, datname)
}

// ScheduledConnFor is ConnFor for the checks scheduled at startup. A database
// the first discovery selected stays reachable after the budget ranks it out,
// because its checks are still scheduled. The statements database and the one
// the DSN names are reachable as well. Any other name is refused without a
// discovery, since the schedule never had it.
func (m *Manager) ScheduledConnFor(ctx context.Context, datname string) (check.Conn, error) {
	if datname == "" {
		return m.Shared(ctx)
	}
	if !m.isScheduled(datname) {
		return nil, rejectedf("database %q is not scheduled on this target", datname)
	}
	return m.ForDatabase(ctx, datname)
}

// requireMonitored refuses a database the latest discovery did not select.
// ForDatabase opens a pool for whatever name it is given: a command naming
// rdsadmin would connect the monitoring role there, and every unknown name would
// take one of the LRU slots that hold the real pools. A name the latest selection
// does not hold gets one discovery first, which picks up a database created since
// the last one, or one the budget has ranked in since.
func (m *Manager) requireMonitored(ctx context.Context, datname string) error {
	if m.isMonitored(datname) {
		return nil
	}
	if _, err := m.Discover(ctx); err != nil {
		return fmt.Errorf("discover databases: %w", err)
	}
	if m.isMonitored(datname) {
		return nil
	}
	return rejectedf("database %q is not monitored on this target", datname)
}

// isMonitored reports whether a command may open datname: a database the latest
// discovery selected, or the one the DSN names, which the agent connects to anyway.
func (m *Manager) isMonitored(datname string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return datname == m.ownDB || m.monitored[datname]
}

// isScheduled reports whether a scheduled check may open datname: a database the
// first discovery selected, the statements database, or the one the DSN names.
// See ScheduledConnFor.
func (m *Manager) isScheduled(datname string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return datname == m.ownDB || datname == m.statementsDB || m.scheduled[datname]
}
func (m *Manager) Database() string {
	return m.target.name
}

// SetScheduledDatabases records the databases a check schedule runs for. The
// schedule set calls it each time it builds a schedule, before that schedule
// starts, so a database a reconcile adds after startup is reachable to the
// checks scheduled for it, and one it drops is not.
func (m *Manager) SetScheduledDatabases(names map[string]bool) {
	set := make(map[string]bool, len(names))
	for name, ok := range names {
		if ok {
			set[name] = true
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scheduled = set
}

// MonitorsDatabase implements check.DatabaseMonitor: a check may read the
// databases the running schedule collects from, and the two the agent
// connects to anyway.
func (m *Manager) MonitorsDatabase(datname string) bool {
	return m.isScheduled(datname)
}

var _ check.DatabaseMonitor = (*Manager)(nil)

const statementsInstalled = `SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_stat_statements')`

// LocateStatements chooses the database whose pg_stat_statements view the
// stat_statements check and EXPLAIN read for a database without its own,
// and reports whether any has one. The statistics are cluster-wide, but
// CREATE EXTENSION creates the view in one database only: the maintenance
// database is used when it has the view, and otherwise the first monitored
// database that does. Scheduling used to look at the maintenance database
// alone, so an extension created only in an application database left the
// check disabled and every statement unseen.
//
// A database that cannot be checked may hold the view, so when none is found
// and one could not be checked, the error reports that the absence is not
// known. The choice of database is then left as it was too.
func (m *Manager) LocateStatements(ctx context.Context, dbs []wire.Database) (bool, error) {
	found, where := m.HasExtension("pg_stat_statements"), "" //nolint:contextcheck // reads the capability cache, which bounds its own refresh
	var unchecked error
	for _, db := range dbs {
		if found {
			break
		}
		if !db.Monitored {
			continue
		}
		conn, err := m.ForDatabase(ctx, db.Name)
		if err != nil {
			unchecked = fmt.Errorf("check %s for pg_stat_statements: %w", db.Name, err)
			continue
		}
		var installed bool
		err = conn.QueryRow(ctx, statementsInstalled).Scan(&installed)
		conn.Release()
		if err != nil {
			unchecked = fmt.Errorf("check %s for pg_stat_statements: %w", db.Name, err)
			continue
		}
		if installed {
			found, where = true, db.Name
		}
	}
	if found || unchecked == nil {
		m.mu.Lock()
		m.statementsDB = where
		m.mu.Unlock()
	}
	if found {
		return true, nil
	}
	return false, unchecked
}

// StatementsDatabase implements check.StatementsSource.
func (m *Manager) StatementsDatabase() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statementsDB
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
// The DSN never changes for the life of a Manager, so its endpoint is parsed
// once. pgxpool.ParseConfig walks the environment and the service files on
// every call; Addr() and Port() are called per instance per push cycle.
func (m *Manager) endpoint() (string, int) {
	m.endpointOnce.Do(func() {
		cfg, err := pgxpool.ParseConfig(m.target.dsn)
		if err != nil {
			return
		}
		m.addr = cfg.ConnConfig.Host
		m.port = int(cfg.ConnConfig.Port)
	})
	return m.addr, m.port
}

func (m *Manager) Addr() string {
	addr, _ := m.endpoint()
	return addr
}

func (m *Manager) Port() int {
	_, port := m.endpoint()
	return port
}
func (m *Manager) Clock() clock.Clock {
	return m.clock
}
