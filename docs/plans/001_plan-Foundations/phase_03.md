# Phase 2 — L2 harness and real checks

> **Intent:** Stand up integration testing against real PostgreSQL across the
> supported version matrix, define the `Check` contract, and implement the first
> checks — shared-view and per-database — proving them on every version and
> under the restricted permission profiles.
> **Shippable alone?** yes — adds library code and a test harness; no runtime
> behavior changes.
> **Preconditions:** phase 1 DONE. Docker daemon available.

This phase turns `TESTING.md` level L2 from a document into a thing that runs.
Everything here is tagged `//go:build integration` and runs under
`make test-integration`.

## State contract (mandatory)

1. Before touching anything: read [STATE.md](STATE.md). If §1 `Status` is `OPEN`,
   finish or revert that unit first (§6 says how far it got). Run the gate
   commands in STATE.md **§3** and check the result against what §1, §7, and §11
   claim; the repo wins, so correct the file when they disagree.
2. **Open the sub-phase in STATE.md §1 before editing any code**: `Type:
   sub-phase`, its `ID`, `Status: OPEN`, `Intent`, `Next action:`, and §6 set to
   `claimed — nothing written yet`.
3. **Close it after the gates are green**: append the §4 ledger row, reset §6 to
   `none — tree consistent`, update §5 §7 §8 §9 §10 and the §11 board, point §1
   at the next unit with `Status: none`, bump the timestamp. When STATE.md §3 has
   WIP commits on, commit the closed sub-phase and put its sha in the §4 row. A
   sub-phase is not done until this is written.
4. If the session ends mid-sub-phase, leave §1 `OPEN` and write exactly what is
   half-finished into §6 before stopping — plus a `wip(<N.Y>)` commit when WIP
   commits are on.

---

## Sub-phases

### 2.1 The pgtest harness

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — test infrastructure. **`agent-1:opus` review gate:** every later integration test is written against this contract, and the concurrency constraint below is easy to get wrong and expensive to discover.
- **Files:** `test/pgtest/pgtest.go`, `test/pgtest/versions.go`, `Makefile`, `.github/workflows/pr.yml`
- **Change:**
  1. `versions.go` pins the exact images. Pin **exact minors**, not floating
     tags, so a CI run is reproducible, and avoid the minors listed in `R2`
     (`17.1`, `16.5`, `15.9` and their siblings introduced a binary interface
     break that was reverted in the following minor):
     ```go
     // Supported range is PostgreSQL 15 through 18 (decision D11).
     // Minors are chosen above the reverted-ABI releases documented in R2.
     var images = map[int]string{
     	15: "postgres:15.14-alpine",
     	16: "postgres:16.10-alpine",
     	17: "postgres:17.6-alpine",
     	18: "postgres:18.2-alpine",
     }

     // Versions returns the majors to exercise. PGLENS_PG_VERSIONS overrides it;
     // the default is the PR matrix (the oldest and the newest supported), and
     // CI sets the full range for the nightly run.
     func Versions() []int // default {15, 18}
     ```
     If a pinned tag no longer exists on Docker Hub at implementation time, pick
     the closest higher minor that is not on the `R2` blocklist and record the
     substitution in `STATE.md` §8.
  2. `pgtest.go` provides the harness:
     ```go
     //go:build integration

     type PG struct {
     	Major   int
     	Version pgtype.PGVersion // server_version_num, read from the running server
     	// ...container, base DSN, mutex
     }

     // ForEach runs fn once per version in Versions(), as a subtest named
     // "pg15", "pg18", ... Subtests for different versions may run in parallel;
     // subtests for the same version must not (see Lock).
     func ForEach(t *testing.T, fn func(t *testing.T, pg *PG))

     // Lock takes exclusive use of this container for the duration of the test
     // and restores the snapshot on cleanup. Every test that touches the
     // database must call it first.
     func (p *PG) Lock(t *testing.T)

     // Pool returns a pool for datname as the given role. Closed automatically.
     func (p *PG) Pool(t *testing.T, datname string, as Role) *pgxpool.Pool

     func (p *PG) Exec(t *testing.T, sql string, args ...any)
     func (p *PG) DSN(datname string, as Role) string
     ```
  3. Container startup, once per major, behind a `sync.Once`, torn down from
     `TestMain`:
     ```go
     c, err := postgres.Run(ctx, images[major],
     	postgres.WithDatabase("app"),
     	postgres.WithUsername("postgres"),
     	postgres.WithPassword("test"),
     	postgres.WithInitScripts(
     		"../fixtures/sql/00_extensions.sql",
     		"../fixtures/sql/01_schema.sql",
     	),
     	postgres.WithSnapshot(),
     	postgres.WithWaitStrategy(
     		wait.ForLog("database system is ready to accept connections").
     			WithOccurrence(2).WithStartupTimeout(90*time.Second)),
     	testcontainers.WithConfigModifier(func(cfg *container.Config) {
     		cfg.Cmd = []string{"postgres",
     			"-c", "shared_preload_libraries=pg_stat_statements",
     			"-c", "compute_query_id=on",
     			"-c", "track_io_timing=on",
     			"-c", "fsync=off",
     			"-c", "full_page_writes=off",
     			"-c", "synchronous_commit=off",
     		}
     	}),
     )
     ```
     API confirmed by `R3`. `fsync=off` and friends are safe and roughly halve
     startup: these containers are discarded.

  > **Concurrency constraint — read this before writing any test.**
  > `container.Restore(ctx)` drops and recreates the database, which requires
  > **exclusive access**. Two tests running in parallel against the same
  > container will destroy each other's state, intermittently, in a way that
  > looks like a product bug. Therefore: **parallelism comes from the version
  > matrix, never from within one version.** `Lock` serializes access with a
  > per-container mutex and registers `Restore` via `t.Cleanup`, so the
  > constraint is enforced by the harness rather than by discipline. Do not call
  > `t.Parallel()` inside a `ForEach` body.

  4. Add to the Makefile:
     ```makefile
     test-integration:
     	$(GO) test -tags=integration -race -shuffle=on -timeout=15m ./...
     ```
  5. Add the `integration-go` job to `.github/workflows/pr.yml`:
     ```yaml
       integration-go:
         runs-on: ubuntu-latest
         timeout-minutes: 25
         strategy:
           fail-fast: false
           matrix:
             pg: ['15', '18']
             profile: ['vanilla', 'rds-like']
         steps:
           - uses: actions/checkout@v4
           - uses: actions/setup-go@v5
             with: { go-version: '1.26', cache: true }
           - run: make test-integration
             env:
               PGLENS_PG_VERSIONS: ${{ matrix.pg }}
               PGLENS_PG_PROFILE: ${{ matrix.profile }}
     ```
     `fail-fast: false` is deliberate: knowing that 15 passes and 18 fails is the
     diagnosis, and stopping at the first failure discards it.
- **Unit tests:** none (the harness is exercised by every later integration test). A smoke test proves it works: `INT-HARNESS-001` — `ForEach` connects, `SELECT 1` returns 1, `Version` matches the major of the image, and after writing a row and letting `Lock`'s cleanup run, a second test in the same container sees a clean database.
- **e2e tests:** none.
- **Done:** `make test-integration` starts one container per version in `Versions()`, `INT-HARNESS-001` passes on 15 and 18, and running it twice in a row gives identical results; a deliberately added `t.Parallel()` inside a `ForEach` body is caught by the harness (documented and demonstrated once, then removed); closed in `STATE.md`.

### 2.2 Permission profiles and the monitoring user

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — the security-relevant setup surface users will actually run.
- **Files:** `deploy/sql/monitoring_user.sql`, `test/fixtures/sql/00_extensions.sql`, `test/fixtures/sql/01_schema.sql`, `test/fixtures/sql/02_rds_like.sql`, `test/pgtest/roles.go`, `test/pgtest/perm_test.go`
- **Change:**
  1. `deploy/sql/monitoring_user.sql` is the **real script shipped to users**
     (`IDEA.md` §7.3), and the harness applies this exact file — so the script
     users run is the script the suite tests. Never fork a test-only copy.
     ```sql
     -- pglens monitoring role. PostgreSQL 15-18.
     -- Run as a superuser, or as rds_superuser on Amazon RDS.
     \set ON_ERROR_STOP on

     CREATE ROLE pglens LOGIN PASSWORD :'pw';
     GRANT pg_monitor TO pglens;

     -- Required for a stable cluster_id. NOT covered by pg_monitor: these
     -- functions are restricted to superusers by default and EXECUTE must be
     -- granted explicitly.
     GRANT EXECUTE ON FUNCTION pg_control_system()     TO pglens;
     GRANT EXECUTE ON FUNCTION pg_control_checkpoint() TO pglens;

     -- Optional tier T1: enables EXPLAIN (plan only).
     -- GRANT pg_read_all_data TO pglens;

     -- Optional tier T2: enables cancel/terminate from the UI.
     -- GRANT pg_signal_backend TO pglens;

     -- Bound the blast radius of the monitoring session itself.
     ALTER ROLE pglens SET statement_timeout = '15s';
     ALTER ROLE pglens SET lock_timeout = '1s';
     ALTER ROLE pglens SET idle_in_transaction_session_timeout = '30s';
     ALTER ROLE pglens SET application_name = 'pglens';
     ```
  2. `02_rds_like.sql` reproduces the constraints of a managed platform without
     any AWS account (the `rds-like` profile):
     ```sql
     -- No superuser; filesystem access revoked; extension creation restricted.
     REVOKE EXECUTE ON FUNCTION pg_read_file(text)  FROM PUBLIC;
     REVOKE EXECUTE ON FUNCTION pg_ls_dir(text)     FROM PUBLIC;
     REVOKE EXECUTE ON FUNCTION pg_ls_waldir()      FROM PUBLIC;
     REVOKE CREATE ON DATABASE app FROM PUBLIC;
     ```
     Selected by `PGLENS_PG_PROFILE=rds-like`; the harness applies it after the
     base fixtures.
  3. `roles.go` defines the tiers the harness can connect as:
     ```go
     type Role int
     const (
     	RoleSuperuser Role = iota // setup only, never used by a check
     	RoleT0                    // pg_monitor + the two pg_control_* grants
     	RoleT0NoControl           // pg_monitor only — used to prove R5
     	RoleT1                    // T0 + pg_read_all_data
     	RoleT2                    // T0 + pg_signal_backend
     )
     ```
  4. `perm_test.go` resolves reference `R5`, which `overview.md` currently marks
     `UNVERIFIED`. On success, change the `R5` row in `overview.md` to drop the
     `UNVERIFIED` marker and record the result; if the finding contradicts `R5`,
     record the real behavior instead and add a `STATE.md` §8 deviation.
- **Unit tests:** none (SQL fixtures).
- **Integration tests:**
  `INT-PERM-001` — connecting as `RoleT0`, `SELECT system_identifier FROM pg_control_system()` succeeds on every version in the matrix.
  `INT-PERM-002` — connecting as `RoleT0NoControl`, the same statement fails with SQLSTATE `42501` (`insufficient_privilege`). **This is the test that proves `R5`**: `pg_monitor` alone is not enough, so the explicit grant in the setup script is load-bearing rather than superstition.
  `INT-PERM-003` — as `RoleT0`, `SELECT * FROM pg_stat_activity` returns rows including other sessions' `query` column (confirms `pg_monitor` really is in effect).
  `INT-PERM-004` — as `RoleT0`, `pg_terminate_backend()` against another session fails with `42501`; as `RoleT2` it succeeds. Proves the T2 tier boundary from `IDEA.md` §4.7.
  `INT-PERM-005` — as `RoleT0`, `EXPLAIN SELECT * FROM <fixture table>` fails with `42501`; as `RoleT1` it succeeds. Proves the T1 tier boundary — the defect `IDEA.md` §4.7 was written to prevent.
  `INT-PERM-006` — under `PGLENS_PG_PROFILE=rds-like`, `pg_ls_waldir()` fails for `RoleT0` and the failure is a clean SQLSTATE, not a panic.
- **e2e tests:** `SYS-PERM-001` in phase 5 runs the full check suite as T0 only.
- **Done:** `make test-integration` green on both profiles for both versions; `INT-PERM-002` demonstrably fails when the grant is added to `monitoring_user.sql` for `RoleT0NoControl` (verify once, then revert); the `R5` row in `overview.md` updated with the verified result; closed in `STATE.md`.

### 2.3 The Check contract and registry

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — public extension point. **`agent-1:opus` review gate:** this interface is the plugin contract `IDEA.md` §4.1 promises; changing it later invalidates every check.
- **Files:** `internal/check/check.go`, `internal/check/registry.go`, `internal/check/target.go`, and the matching `_test.go` files
- **Change:** implements the corrected interface from `IDEA.md` §4.1. Note what
  is **not** there: the v0.1 draft had both `Query()` and `Scrape()`, which were
  redundant; `Scrape` covers everything.
  ```go
  type Scope string

  const (
  	ScopeInstance Scope = "instance"
  	ScopeDatabase Scope = "database" // runs once per monitored database
  	ScopeCluster  Scope = "cluster"
  )

  type Requirements struct {
  	Roles      []pgtype.Role     // nil means any role
  	MinPG      pgtype.PGVersion  // 0 means no lower bound beyond the global one
  	MaxPG      pgtype.PGVersion  // 0 means no upper bound
  	Extensions []string          // e.g. "pg_stat_statements"
  	PermTier   pgtype.PermTier   // the minimum tier this check needs
  	Scope      Scope
  }

  // Supports reports whether the check can run against this target, and why not
  // when it cannot. The reason string is surfaced to the user as
  // check_skipped{reason}; a check that silently disappears is
  // indistinguishable from one that is broken.
  func (r Requirements) Supports(role pgtype.Role, v pgtype.PGVersion,
  	tier pgtype.PermTier, exts map[string]bool) (bool, string)

  type Result struct {
  	Metrics    []pgtype.Metric
  	StatsReset *time.Time // for counter-reset detection; nil when the source has none
  	Truncated  bool       // top-N or budget dropped rows
  	QueryTexts map[int64]string // queryid -> normalized text, first sight only
  }

  type Check interface {
  	Name() string
  	Requires() Requirements
  	DefaultInterval() time.Duration // the server may override it (decision D7)
  	Timeout() time.Duration         // per check, never a single global value
  	Scrape(ctx context.Context, t Target) (Result, error)
  }

  // Target is everything a check may know about what it is scraping. Checks
  // never open their own connections; the agent owns connection policy.
  type Target interface {
  	InstanceID() pgtype.InstanceID
  	ClusterID() pgtype.ClusterID
  	Role() pgtype.Role
  	PGVersion() pgtype.PGVersion
  	PermTier() pgtype.PermTier
  	HasExtension(name string) bool
  	// Conn returns the shared connection on the maintenance database.
  	Conn(ctx context.Context) (*pgxpool.Conn, error)
  	// ConnFor returns a connection to a specific database. Scope-database
  	// checks use it; it may block on the connection budget.
  	ConnFor(ctx context.Context, datname string) (*pgxpool.Conn, error)
  	// Database is the database this invocation is scoped to, "" for instance scope.
  	Database() string
  	Clock() clock.Clock
  }
  ```
  `registry.go`:
  ```go
  // Register panics on a duplicate name. Registration happens in package init
  // functions, so a duplicate is a programming error that must fail at startup,
  // not a runtime condition to handle.
  func Register(c Check)
  func Get(name string) (Check, bool)
  func All() []Check // sorted by name, so iteration order is deterministic
  ```
  Every `Scrape` implementation must, as its first statement on the connection,
  apply its own limits:
  ```go
  _, err := conn.Exec(ctx, "SET LOCAL statement_timeout = $1", ms(c.Timeout()))
  // plus lock_timeout = 1s and idle_in_transaction_session_timeout = 30s
  ```
  Provide `check.ApplySessionLimits(ctx, conn, timeout)` so no check reimplements
  it and none forgets.
- **Unit tests:**
  `TestRequirements_Supports` — table-driven over the cross product of role (primary, standby, unknown), version (below `MinPG`, inside, above `MaxPG`), tier (below and at the requirement) and extension presence; each rejection returns a non-empty, distinct reason string.
  `TestRequirements_EmptyRolesMeansAny` — a check with `Roles: nil` supports every role.
  `TestRegistry_DuplicatePanics` — registering the same name twice panics.
  `TestRegistry_AllIsSorted` — `All()` is sorted by name and stable across calls.
  `TestRegistry_GetMissing` — returns `false`, does not panic.
- **Integration tests:** `INT-CHECK-001` — `ApplySessionLimits` really sets the three GUCs: after calling it, `SHOW statement_timeout` returns the expected value on the same connection, and a different connection is unaffected (proves `SET LOCAL` scoping).
- **e2e tests:** none yet.
- **Done:** `make test` and `make test-integration` green; `internal/check` has no import of `internal/agent` or `internal/server` (the dependency arrow points one way); closed in `STATE.md`.

### 2.4 Shared-view checks

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — first real SQL against every supported version.
- **Files:** `internal/check/instance_info.go`, `internal/check/activity.go`, `internal/check/database_stats.go`, and `it_*_test.go` beside each
- **Change:** three checks, all `ScopeInstance`, all `PermTier: TierReadOnly`,
  all using only the shared connection.

  **`instance_info`** — `DefaultInterval` 10s, `Timeout` 2s. The identity and
  role check; everything else depends on it.
  ```sql
  SELECT (SELECT system_identifier FROM pg_control_system())::text AS system_identifier,
         pg_is_in_recovery()                                        AS in_recovery,
         current_setting('server_version_num')::int                 AS version_num,
         pg_postmaster_start_time()                                 AS start_time,
         inet_server_addr()::text                                   AS addr,
         inet_server_port()                                         AS port;
  ```
  Emits gauges `pg_up`, `pg_in_recovery`, `pg_version_num`, `pg_uptime_seconds`.
  `system_identifier` is returned as **text** because it is a `uint64` and would
  overflow a signed 64-bit integer in the driver.
  When `pg_control_system()` is denied (tier T0 without the grant), the check
  must **still succeed**, omit `system_identifier`, and set a
  `pg_cluster_id_source="manual"` label — the agent then falls back to the
  configured cluster name. Losing the identifier degrades identity quality; it
  must not take the instance offline.

  **`activity`** — `DefaultInterval` 10s, `Timeout` 2s.
  ```sql
  SELECT COALESCE(state, 'unknown') AS state,
         COALESCE(wait_event_type, 'CPU') AS wait_event_type,
         count(*) AS n,
         COALESCE(max(EXTRACT(epoch FROM now() - xact_start)), 0) AS max_xact_age,
         COALESCE(max(EXTRACT(epoch FROM now() - state_change))
                  FILTER (WHERE state = 'idle in transaction'), 0) AS max_idle_in_txn
  FROM pg_stat_activity
  WHERE backend_type = 'client backend'
    AND pid <> pg_backend_pid()
  GROUP BY 1, 2;
  ```
  Emits gauges `pg_backends{state,wait_event_type}`, `pg_max_xact_age_seconds`,
  `pg_max_idle_in_transaction_seconds`, plus
  `pg_connections_used` / `pg_connections_limit` from
  `current_setting('max_connections')`.
  `pid <> pg_backend_pid()` excludes the monitoring session itself — without it
  the monitor is permanently counted as one active backend, which is both wrong
  and the kind of small lie that erodes trust in the whole tool.

  **`database_stats`** — `DefaultInterval` 30s, `Timeout` 2s. Reads
  `pg_stat_database` for all databases from the shared connection (this view is
  cluster-wide, no per-database connection needed).
  Emits **counters** `pg_xact_commit_total`, `pg_xact_rollback_total`,
  `pg_blks_read_total`, `pg_blks_hit_total`, `pg_deadlocks_total`,
  `pg_temp_bytes_total`, `pg_conflicts_total`, each labelled `database`, plus
  gauge `pg_numbackends`.
  Sets `Result.StatsReset` from `max(stats_reset)` — this is what feeds
  `internal/delta` rule 3.
- **Unit tests:** row-parsing tests with in-memory fixture rows for each check: a NULL `state`, a NULL `wait_event_type`, an empty result set, and a row with every column at its zero value. None of these open a connection.
- **Integration tests:**
  `INT-CHECK-010` — `instance_info` on every version returns a non-zero `system_identifier`, `in_recovery == false`, and a `version_num` matching `pg.Version`.
  `INT-CHECK-011` — `instance_info` as `RoleT0NoControl` still returns a `Result` with no error, omits `system_identifier`, and sets the manual-source label.
  `INT-CHECK-012` — `activity` never counts the scraping session: open 3 extra idle connections, assert `sum(pg_backends) == 3`.
  `INT-CHECK-013` — `activity` reports `idle in transaction`: open a transaction, leave it idle, assert `pg_max_idle_in_transaction_seconds > 0`.
  `INT-CHECK-014` — `database_stats` returns a row per database and a non-nil `StatsReset`; after `SELECT pg_stat_reset()`, the returned `StatsReset` is strictly later than before. **This is the end-to-end proof that reset detection has a real signal to work with.**
  `INT-CHECK-015` — every check in `All()` runs clean as `RoleT0`, on every version, on both profiles. Any check needing more must declare it in `Requires()` and be skipped upstream; the test asserts that a skip carries a non-empty reason. This is the `TestAllChecks_WorkOnT0` contract from `TESTING.md` §4.4.
- **e2e tests:** `SYS-PERM-001` in phase 5.
- **Done:** `make test-integration` green for `{15,18} x {vanilla,rds-like}`; `INT-CHECK-015` passes with zero checks failing and every skip carrying a reason; closed in `STATE.md`.

### 2.5 Per-database check: pg_stat_statements with top-N

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — the dominant cardinality source; first consumer of `internal/cardinality`.
- **Files:** `internal/check/stat_statements.go`, `internal/check/it_stat_statements_test.go`
- **Change:** implements decision D16 (Q10). `ScopeDatabase`, `PermTier:
  TierReadOnly`, `Extensions: ["pg_stat_statements"]`, `DefaultInterval` 60s,
  `Timeout` 15s.

  The query selects the **union of two rankings**, because ranking by time alone
  misses the "many fast queries" pattern that is frequently the real problem:
  ```sql
  WITH me AS (SELECT oid FROM pg_database WHERE datname = current_database()),
  ranked AS (
      (SELECT queryid FROM pg_stat_statements, me
        WHERE dbid = me.oid AND queryid IS NOT NULL
        ORDER BY total_exec_time DESC LIMIT $1)
      UNION
      (SELECT queryid FROM pg_stat_statements, me
        WHERE dbid = me.oid AND queryid IS NOT NULL
        ORDER BY calls DESC LIMIT $1)
  )
  SELECT s.queryid, s.calls, s.total_exec_time, s.rows,
         s.shared_blks_hit, s.shared_blks_read, s.wal_bytes,
         left(s.query, 8192) AS query
    FROM pg_stat_statements s
    JOIN ranked r USING (queryid), me
   WHERE s.dbid = me.oid;
  ```
  Rules, all mandatory:
  - the raw counters are emitted as `KindCounter` metrics labelled
    `queryid`; the server derives rates (decision D17). Never compute rates in
    the agent
  - the returned `queryid` set is passed through `cardinality.Selector` so
    hysteresis and `MaxKeys` apply, and `Result.Truncated` is set from it
  - `Result.QueryTexts` carries the text **only for queryid values not seen
    before**, tracked in a bounded LRU on the check (capacity 4096 per database).
    Query text is kilobytes; sending it every cycle would dominate the payload
  - `StatsReset` comes from `pg_stat_statements_info.stats_reset`, available from
    PG14 and therefore unconditionally present across the supported range
    (D11 is what makes this simple)
  - `queryid` is `bigint` and may be negative; never store it in a `uint64` and
    never format it as unsigned

  Document on the type, so it reaches the API and later the UI: **`queryid` is
  computed from the parse tree including object OIDs**, so it is comparable
  between a primary and its physical replicas, but **not** between different
  clusters or different major versions.
- **Unit tests:** parsing tests with fixture rows: a negative `queryid`, a NULL `query` (possible when the text was evicted from the query text file), a row where `calls` is 0, and an empty result. LRU behavior: the 4097th distinct queryid evicts the oldest and its text is re-sent.
- **Integration tests:**
  `INT-STMT-001` — run 5 known queries, scrape, assert each `queryid` appears with `calls >= 1` and a non-empty text on first sight, empty on the second scrape.
  `INT-STMT-002` — generate 500 structurally distinct queries (`SELECT 1+1`, `SELECT 1+1+1`, … — literals are normalized by `pg_stat_statements`, so the queries must differ in **parse-tree shape**, not in constants); assert the result is capped at `MaxKeys` and `Truncated` is true.
  `INT-STMT-003` — a query that is 1st by `calls` and far down by `total_exec_time` is present in the result. Proves the union of rankings.
  `INT-STMT-004` — `StatsReset` changes after `SELECT pg_stat_statements_reset()`, on every version.
  `INT-STMT-005` — the check is correctly skipped with a reason when the extension is absent: create a second database without it, assert `Supports` returns false and a reason mentioning `pg_stat_statements`.
  `INT-STMT-006` — negative `queryid` values survive the round trip through `Metric.Labels` unchanged.
- **e2e tests:** `SYS-LOAD-008` in phase 5 — 5000 distinct queries against a live instance, budget holds, `truncated` reaches the API.
- **Done:** `make test-integration` green on the matrix; `INT-STMT-002` shows the cap holding; closed in `STATE.md`.

### 2.6 Golden payload per version

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — regression net for cross-version drift.
- **Files:** `internal/wire/envelope.go`, `internal/wire/golden.go`, `internal/wire/testdata/payload_pg15.json`, `internal/wire/testdata/payload_pg18.json`, `Makefile`
- **Change:**
  1. Define the wire envelope (decision D12: JSON, versioned). This is the
     contract phase 3 and phase 4 both build against, so it lands here where
     both can see it.
     ```go
     const ProtocolVersion = 1

     type Envelope struct {
     	ProtocolVersion int         `json:"protocol_version"`
     	AgentID         string      `json:"agent_id"`
     	AgentVersion    string      `json:"agent_version"`
     	SentAt          time.Time   `json:"sent_at"`
     	Instances       []Instance  `json:"instances"`
     }

     type Instance struct {
     	InstanceID      string     `json:"instance_id"`
     	ClusterID       string     `json:"cluster_id"`         // decimal string (D18)
     	ClusterIDSource string     `json:"cluster_id_source"`  // system_identifier | manual
     	Addr            string     `json:"addr"`
     	Port            int        `json:"port"`
     	PGVersion       int        `json:"pg_version"`
     	Role            string     `json:"role"`
     	PermTier        string     `json:"perm_tier"`
     	Capabilities    []string   `json:"capabilities"`
     	Databases       []Database `json:"databases"`
     	Results         []Result   `json:"results"`
     	TopologyEdges   []Edge     `json:"topology_edges,omitempty"`
     }

     type Result struct {
     	Check      string            `json:"check"`
     	TS         time.Time         `json:"ts"`
     	Database   string            `json:"database,omitempty"`
     	StatsReset *time.Time        `json:"stats_reset,omitempty"`
     	Truncated  bool              `json:"truncated,omitempty"`
     	Error      string            `json:"error,omitempty"`
     	SkipReason string            `json:"skip_reason,omitempty"`
     	Metrics    []Metric          `json:"metrics,omitempty"`
     	QueryTexts map[string]string `json:"query_texts,omitempty"` // queryid -> text
     }
     ```
     `cluster_id` is a **string**, per D18. Add a test that proves why, so nobody
     "simplifies" it back to a number later.
  2. `golden.go` provides `AssertGolden(t, got any, path string, update bool)`
     with a `-update` flag, canonical JSON (sorted keys, two-space indent) and a
     unified diff on mismatch.
  3. Makefile: replace the placeholder from 0.3 with
     `golden: $(GO) test -tags=integration -run Golden -update ./...`
  4. An integration test builds a full envelope from the checks of 2.4 and 2.5
     against each version, **normalizes the volatile fields** (timestamps, UUIDs,
     `system_identifier`, addresses, ports, durations, `queryid`) to fixed
     placeholders, and compares against the golden file.

  > **Review rule, applies for the rest of the project:** a changed golden file
  > in a pull request must be explained in the description. This is the file
  > where a column renamed between major versions becomes visible; an unexplained
  > golden diff is a request-changes.
- **Unit tests:** `TestEnvelope_ClusterIDIsAString` — marshalling `math.MaxUint64` and unmarshalling it returns the exact value, and the raw JSON contains `"18446744073709551615"` with quotes. `TestEnvelope_RoundTrip` — marshal then unmarshal is lossless for a fully-populated envelope. `TestEnvelope_OmitEmpty` — an envelope with no optional fields produces JSON without those keys.
- **Integration tests:** `INT-GOLDEN-001` — the normalized envelope matches `testdata/payload_pg<major>.json` for every version in the matrix.
- **e2e tests:** none.
- **Done:** `make golden` regenerates deterministically (running it twice produces no diff); `make test-integration` green; the golden files are committed and contain no real hostnames, no DSNs and no credentials; closed in `STATE.md`.

### 2.7 Update README.md

Mandatory closing sub-phase of every phase. User guide only — no implementation
detail.

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — documentation; `agent-1:opus` reads it on the final phase.
- **Files:** `README.md`
- **Change:** this phase produces the first artifact a user actually consumes:
  `deploy/sql/monitoring_user.sql`. Add a **Setting up the monitoring role**
  section:
  - what the script does and the privileges it grants, in plain terms
  - how to run it: `psql -v pw="'<password>'" -f deploy/sql/monitoring_user.sql`
  - that it must run as a superuser, or as `rds_superuser` on Amazon RDS
  - **why the two `pg_control_*` grants are required** — they are not covered by
    `pg_monitor` and without them the cluster identity falls back to a
    configured name
  - the optional tiers, commented out by default, and what each enables
    (`pg_read_all_data` for EXPLAIN, `pg_signal_backend` for cancelling queries)
  Also update **Requirements** to state the supported range is PostgreSQL 15 to
  18, and extend **Running the checks** with `make test-integration` and its
  Docker requirement.
  Do not describe the check registry, the `Check` interface, package names or
  the golden-file mechanism: none of it is user-facing.
- **Unit tests:** none (documentation).
- **e2e tests:** none — the `psql` invocation in the README was executed against a container and produced the documented result.
- **Done:** a user can create the monitoring role from the README alone and verify it with the documented `psql` command; no Go package, type or file-layout detail appears in the file; `make fmt-check`, `make lint`, `make test`, `make test-integration` all green; closed in `STATE.md` with the §11 docs row for phase 2 set.

---

## Phase gates

- **Fmt:** `make fmt-check`
- **Lint:** `make lint`
- **Test subset:** `make test` and `make test-integration` for `PGLENS_PG_VERSIONS={15,18}` and `PGLENS_PG_PROFILE={vanilla,rds-like}`
- **Coverage:** `make coverage-gate`
- **Regression guard:** every phase 0 and phase 1 gate still green
- **README:** updated with the monitoring-role setup this phase made usable, free of implementation detail

## Phase done criterion

`make test-integration` runs the full check suite against real PostgreSQL 15 and
18, on both permission profiles, entirely as tier T0, with zero failures and
every skip carrying a reason (`INT-CHECK-015`). `INT-PERM-002` proves that
`pg_monitor` alone cannot read `system_identifier`, converting reference `R5`
from `UNVERIFIED` to verified. `INT-STMT-002` proves the cardinality cap holds
against 500 distinct queries. Golden payloads exist for both versions and
regenerate deterministically. README.md reflects this phase's shipped behavior,
and `STATE.md` §11 shows phase 2 `DONE` with every sub-phase closed.
