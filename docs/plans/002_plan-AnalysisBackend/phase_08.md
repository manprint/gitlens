# Phase 7 — Advisor engine and rule packs

> **Intent:** turn everything collected so far into ranked, persistent,
> explainable findings — slow queries, index problems, autovacuum problems,
> bloat, connection and transaction problems, memory misconfiguration and broken
> archiving.
> **Shippable alone?** yes — a new engine, a new table, one endpoint. Nothing
> existing changes.
> **Preconditions:** phases 2, 3, 4, 5 and 6 `DONE`. A rule whose inputs are
> missing degrades rather than failing, so a partially completed prerequisite
> does not break the build — but the acceptance scenario needs them all.

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

## The two rules that shape this phase

**One snapshot per run (plan 002 D26).** Rules never touch the database. The
engine takes one snapshot per instance, hands it to every rule, and collects
findings. Thirty rules issuing their own queries would multiply load on the
database being diagnosed and would make rule ordering observable.

**A rule that cannot run says so (plan 002 D21, invariant I-3).** Every rule
declares what it needs. When the input is missing — the permission tier is too
low, a check is disabled, a metric has no data yet — the rule emits a finding
with `state = "degraded"` naming exactly what is missing. It never returns
nothing, because a silently absent rule is indistinguishable from a healthy
system.

---

## Sub-phases

### 7.1 Migration `0009_findings.sql`

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/store/migrations/0009_findings.sql` (new)
- **Change:**

  ```sql
  CREATE TABLE IF NOT EXISTS findings (
    finding_id   text        NOT NULL,   -- rule_id + '/' + subject, stable
    tenant_id    text        NOT NULL DEFAULT 'default',
    rule_id      text        NOT NULL,
    severity     text        NOT NULL CHECK (severity IN ('critical','warning','info')),
    state        text        NOT NULL CHECK (state IN ('open','degraded','muted','resolved')),
    scope        text        NOT NULL CHECK (scope IN ('instance','cluster','database','relation')),
    cluster_id   bigint,
    instance_id  uuid,
    datname      text        NOT NULL DEFAULT '',
    object_name  text        NOT NULL DEFAULT '',  -- schema.relation, index name, GUC name
    title        text        NOT NULL,
    detail       text        NOT NULL,
    remediation  text        NOT NULL DEFAULT '',
    evidence     jsonb       NOT NULL DEFAULT '{}'::jsonb,
    degraded_reason text,
    first_seen   timestamptz NOT NULL,
    last_seen    timestamptz NOT NULL,
    resolved_at  timestamptz,
    muted_until  timestamptz,
    mute_reason  text,
    PRIMARY KEY (tenant_id, finding_id)
  );
  CREATE INDEX IF NOT EXISTS findings_lookup_idx
    ON findings (tenant_id, state, severity, last_seen DESC);
  CREATE INDEX IF NOT EXISTS findings_instance_idx
    ON findings (tenant_id, instance_id, datname, last_seen DESC);
  CREATE INDEX IF NOT EXISTS findings_rule_idx
    ON findings (tenant_id, rule_id, state);
  ```

  There is deliberately **no history table**. A finding is a durable statement
  about the current configuration or schema, and `first_seen` already answers
  "since when". Alerts carry the temporal story; findings carry the standing
  one (plan 002 D9).

  A finding disappears from a run — the index was dropped, the setting was fixed
  — and the engine sets `state = 'resolved'` and `resolved_at`. Rows are never
  deleted by the engine, so "we fixed 14 findings this quarter" stays
  answerable. Add a retention `DELETE` of resolved findings older than 90 days
  to the engine, not to the migration.

- **Unit tests:** `TestMigrations_0009_ContainsFindings`.
- **e2e tests:** `INT-ADV-001` — apply migrations to a fresh database and assert
  the table and its three indexes exist.
- **Done:** gates green + closed in `STATE.md`.

### 7.2 The rule contract and registry

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — this contract is what makes adding a rule
  cheap forever; review gate
- **Files:** `internal/advisor/doc.go` (new), `internal/advisor/rule.go` (new),
  `internal/advisor/registry.go` (new), `internal/advisor/rule_test.go` (new)
- **Change:**

  ```go
  package advisor

  type Severity = alert.Severity   // reuse the enum, do not define a second one

  type Scope string
  const (
      ScopeInstance Scope = "instance"
      ScopeCluster  Scope = "cluster"
      ScopeDatabase Scope = "database"
      ScopeRelation Scope = "relation"
  )

  type State string
  const (
      StateOpen     State = "open"
      StateDegraded State = "degraded"
      StateMuted    State = "muted"
      StateResolved State = "resolved"
  )

  // Finding is one statement about one subject.
  type Finding struct {
      RuleID         string
      Severity       Severity
      State          State
      Scope          Scope
      ClusterID      *int64
      InstanceID     *uuid.UUID
      Datname        string
      ObjectName     string
      Title          string
      Detail         string
      Remediation    string
      Evidence       map[string]any
      DegradedReason string
  }

  // ID is rule_id + "/" + the subject, and must be stable across runs so a
  // finding keeps its first_seen.
  func (f Finding) ID() string

  // Rule is one advisor check. Rules are pure: they read the snapshot and
  // return findings. They never perform I/O.
  type Rule interface {
      ID() string
      Severity() Severity
      Scope() Scope
      // Needs lists what the rule reads, as metric names, fact kinds or the
      // literal "host". The engine uses it to decide whether the rule can run
      // and, when it cannot, what to name in the degraded finding.
      Needs() []string
      MinTier() pgtype.PermTier
      Evaluate(s *Snapshot) []Finding
  }

  // Register panics on a duplicate id, exactly like check.Register.
  func Register(r Rule)
  func All() []Rule            // sorted by ID, deterministic
  func ResetForTest()
  ```

  Rules register in package `init()`, one file per pack, mirroring
  `internal/check/registry.go:15` so a reader who knows one knows the other.

  A `Finding` returned with an empty `Title` or `Detail` is a programming error;
  the engine rejects it with a loud log rather than persisting an unreadable row.
  Every rule that returns `StateOpen` must also set a non-empty `Remediation` —
  a finding a reader cannot act on is a complaint, not advice. Assert both in
  the shared rule test helper of 7.4.

- **Unit tests:** `TestRegister_PanicsOnDuplicate`,
  `TestAll_IsSortedAndDeterministic`,
  `TestFinding_IDIsStable`,
  `TestFinding_IDDiffersPerSubject`.
- **e2e tests:** none.
- **Done:** gates green + closed in `STATE.md`.

### 7.3 The snapshot

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — the shape of this struct decides what every
  rule can express; review gate
- **Files:** `internal/advisor/snapshot.go` (new),
  `internal/advisor/snapshot_integration_test.go` (new)
- **Change:** define what a rule sees, and load it with a bounded number of
  queries.

  ```go
  // Snapshot is everything the rules read for one instance, plus the cluster
  // context they need for replica-aware rules. It is built once per engine run.
  type Snapshot struct {
      Now        time.Time
      ClusterID  int64
      InstanceID uuid.UUID
      Role       pgtype.Role
      PGVersion  pgtype.PGVersion
      PermTier   pgtype.PermTier

      // Latest value per metric, keyed by name then by canonical label string.
      Metrics map[string]map[string]float64
      // Latest object_facts rows, keyed by kind then key.
      Facts map[string]map[string]Fact
      // Latest per-relation rows.
      Tables  []TableStat
      Indexes []IndexStat
      Bloat   []BloatStat
      // Top statements by total execution time in the window.
      Statements []StatementStat
      // Host figures; Available is false for a remote instance.
      Host HostInfo
      // Sibling instances in the same cluster, for drift and divergence rules.
      Siblings []SiblingInfo
      // Which checks reported a skip, and why. This is what a degraded
      // finding quotes.
      SkippedChecks map[string]string
      // Seven-day baselines, for regression rules. Nil when there is not
      // enough history; a rule must handle that rather than dividing by zero.
      Baseline *Baseline
  }
  ```

  `SiblingInfo` carries `InstanceID`, `Role`, the `setting` facts and the
  index definition hashes — enough for GUC drift and for index divergence
  between primary and standby, and no more.

  `Baseline` carries, per `queryid`, the median `mean_exec_time` over the
  previous seven days excluding the last hour. Compute it with one windowed
  query, not one query per statement.

  Loading budget: **at most 12 queries per instance per run**, and the test
  below asserts it. If a rule pack needs a thirteenth, the pack is wrong, not
  the budget.

  Provide `func LoadSnapshot(ctx context.Context, pool *pgxpool.Pool, instanceID uuid.UUID, now time.Time) (*Snapshot, error)`
  and a `func (s *Snapshot) Metric(name string, labels map[string]string) (float64, bool)`
  helper so rules never build canonical label strings by hand — reuse
  `pgtype.CanonicalLabels` inside it.

- **Unit tests:** `TestSnapshot_MetricHelperUsesCanonicalLabels`,
  `TestSnapshot_MetricMissingReturnsFalse`.
- **e2e tests:** `INT-ADV-002` — against a seeded database, `LoadSnapshot`
  populates every field and issues no more than 12 queries; count them with a
  pgx query tracer, not by reading the code.
  `INT-ADV-003` — with no history, `Baseline` is nil and no rule panics.
- **Done:** gates green + `INT-ADV-002` asserting the query budget + closed in
  `STATE.md`.

### 7.4 Rule pack A — queries and transactions

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/advisor/rules_query.go` (new),
  `internal/advisor/rules_query_test.go` (new),
  `internal/advisor/ruletest_test.go` (new — the shared helper)
- **Change:** first write the shared test helper, then the rules.

  The helper `assertRuleContract(t, r Rule, s *Snapshot)` runs a rule and
  asserts, for every finding returned: non-empty `Title` and `Detail`; a
  non-empty `Remediation` whenever `State == StateOpen`; a stable `ID()` across
  two evaluations of the same snapshot; a `Severity` inside the enum; and, when
  `State == StateDegraded`, a non-empty `DegradedReason`. Every rule test in
  packs A, B and C calls it. It is what keeps thirty rules honest without thirty
  copies of the same assertions.

  The rules — these covers the user's points 1 and 10:

  | `rule_id` | Severity | Reads | Fires when |
  |-----------|----------|-------|------------|
  | `query.slow_mean` | warning | `Statements` | a statement's mean execution time exceeds 500 ms and it ran at least 10 times in the window |
  | `query.total_time_share` | warning | `Statements` | one `queryid` accounts for more than 30 % of total execution time across the window |
  | `query.regression` | critical | `Statements`, `Baseline` | mean execution time is at least 3× the seven-day median and at least 100 ms; degraded when `Baseline` is nil |
  | `query.temp_bytes_high` | warning | `Statements` | average temporary bytes written per call exceeds `work_mem`; the remediation names the current `work_mem` |
  | `query.cache_miss_high` | info | `Statements` | shared block read ratio above 30 % for a statement with more than 1 000 calls |
  | `txn.long_running` | warning | `pg_max_xact_age_seconds` | a transaction has been open for more than 15 minutes |
  | `txn.idle_in_transaction` | warning | `pg_max_idle_in_txn_seconds` | idle in transaction for more than 5 minutes |
  | `txn.prepared_orphan` | critical | `pg_oldest_prepared_xact_seconds` | a prepared transaction is older than 1 hour; these block vacuum indefinitely |
  | `txn.high_rollback_ratio` | info | `pg_xact_rollback_ratio` | above 10 % on a database with more than 10 000 transactions |
  | `txn.wraparound_risk` | critical | `pg_max_datfrozenxid_age` | above 1 000 000 000 |

  Every threshold above is a package-level constant with a doc comment giving
  the reason for the number. A magic number inside an `if` is the thing a later
  reader cannot tune safely.

  `Evidence` must carry the numbers the finding is based on — the observed
  value, the threshold, the `queryid` where relevant — because the API renders
  it and an operator needs to see why before acting.

- **Unit tests:** one test per rule, named
  `TestRule_<ruleID>_FiresWhenAboveThreshold` and
  `TestRule_<ruleID>_QuietWhenBelow`, plus
  `TestRule_query_regression_DegradesWithoutBaseline`,
  `TestPackA_AllRulesSatisfyContract` (iterates the pack through
  `assertRuleContract`).
- **e2e tests:** covered by `SYS-ADV-002` in phase 9.
- **Done:** gates green + every pack A rule covered by both a firing and a quiet
  test + closed in `STATE.md`.

### 7.5 Rule pack B — indexes, tables, autovacuum, bloat

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/advisor/rules_relation.go` (new),
  `internal/advisor/rules_relation_test.go` (new)
- **Change:** the rules covering the user's points 2, 3 and 4.

  | `rule_id` | Severity | Reads | Fires when |
  |-----------|----------|-------|------------|
  | `index.unused` | warning | `Indexes` | `idx_scan` is 0 over at least 7 days of history, the index is larger than 10 MiB, and it is neither primary nor unique. Degraded when history is shorter than 7 days — an index unused for an hour is not unused |
  | `index.duplicate` | warning | `Indexes` | two or more indexes on the same table share a `def_hash`. Names both, recommends dropping the one that is not primary or unique |
  | `index.redundant_prefix` | info | `Indexes` | index A's column list is a strict prefix of index B's, both on the same table, and A is not unique |
  | `index.invalid` | critical | `Indexes` | `is_valid` is 0 — an invalid index is not used by the planner but is still maintained on every write |
  | `index.bloat_high` | warning | `Bloat` | `bloat_ratio` above 0.4 on an index larger than 50 MiB |
  | `index.divergence` | warning | `Siblings` | an index exists on the primary and not on a standby, or the reverse. `IDEA.md` §5.6 calls this a finding in itself |
  | `table.dead_tuples_high` | warning | `Tables` | `n_dead_tup / NULLIF(n_live_tup, 0)` above 0.2 with at least 10 000 dead tuples |
  | `table.never_autovacuumed` | warning | `Tables` | `last_autovacuum` is null, `last_vacuum` is null, and the table has more than 100 000 live tuples |
  | `table.autoanalyze_stale` | info | `Tables` | `n_mod_since_analyze` exceeds 20 % of `n_live_tup` |
  | `table.wraparound_risk` | critical | `Tables` | `relfrozenxid_age` above 1 000 000 000 for a single relation |
  | `table.bloat_high` | warning | `Bloat` | `bloat_ratio` above 0.3 on a table larger than 100 MiB |
  | `table.seq_scan_heavy` | info | `Tables` | `seq_scan` rate is high, `idx_scan` is near zero, and the table has more than 100 000 live tuples — a missing-index candidate, phrased as a candidate and never as a certainty |
  | `vacuum.starvation` | critical | `Tables`, `pg_vacuum_jobs_running`, `settings` | more than 10 tables are above the dead-tuple threshold while `autovacuum_max_workers` jobs are continuously busy |
  | `vacuum.disabled` | critical | `settings` | `autovacuum` is `off` |

  `index.redundant_prefix` needs the column list, which the `def_hash` throws
  away by design. Parse it from the `index_def` fact text: take the substring
  between the outermost parentheses of the `USING` clause and split on commas at
  depth zero, so an expression index containing a comma inside a function call
  is not mis-split. An index whose definition cannot be parsed is **skipped
  silently by this rule only** — it is an optimisation hint, not a
  correctness signal — while every other index rule still evaluates it.

- **Unit tests:** one firing and one quiet test per rule, plus
  `TestRule_index_unused_DegradesWithShortHistory`,
  `TestRule_index_redundant_prefix_ParsesExpressionIndex`,
  `TestRule_index_redundant_prefix_SkipsUnparseable`,
  `TestRule_index_duplicate_NamesBothIndexes`,
  `TestPackB_AllRulesSatisfyContract`.
- **e2e tests:** covered by `SYS-ADV-001`, `SYS-IDX-001` and `SYS-IDX-003` in
  phase 9.
- **Done:** gates green + closed in `STATE.md`.

### 7.6 Rule pack C — configuration, connections, durability

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/advisor/rules_config.go` (new),
  `internal/advisor/rules_config_test.go` (new)
- **Change:** the rules covering the user's points 7, 8 and 11. Every memory
  rule needs `Snapshot.Host.Available`; when it is false the rule emits a
  degraded finding naming "host metrics unavailable for this instance", which is
  the honest answer and is what invariant I-3 requires.

  | `rule_id` | Severity | Fires when |
  |-----------|----------|------------|
  | `config.work_mem_oversized` | warning | `work_mem × max_connections` exceeds 25 % of usable memory. Usable memory is the cgroup limit when present, otherwise host total |
  | `config.work_mem_low` | info | `work_mem` below 4 MiB while `query.temp_bytes_high` is also firing for the same instance |
  | `config.shared_buffers_low` | warning | `shared_buffers` below 15 % of usable memory on an instance with more than 4 GiB |
  | `config.shared_buffers_high` | warning | `shared_buffers` above 40 % of usable memory |
  | `config.effective_cache_size_mismatch` | info | `effective_cache_size` differs from 50–75 % of usable memory by more than a factor of two |
  | `config.maintenance_work_mem_low` | info | below 256 MiB on an instance with more than 8 GiB of usable memory |
  | `config.max_connections_high` | warning | above 200 with no pooler observed and `pg_connections_used_ratio` peak below 0.3 — many connections configured, few used, each one costing memory |
  | `config.track_io_timing_off` | info | `track_io_timing` is off; always emitted as `degraded` on the I/O rules that need it |
  | `config.checkpoints_too_frequent` | warning | requested checkpoints are more than 20 % of all checkpoints over the window; remediation names `max_wal_size` |
  | `config.wal_keep_size_low` | warning | observed peak replication lag in bytes exceeds `wal_keep_size` and no slot protects the standby |
  | `config.fsync_off` | critical | `fsync` is off. This is data loss on power failure and is never a tuning choice on a production system |
  | `config.full_page_writes_off` | critical | `full_page_writes` is off without a storage guarantee |
  | `config.drift` | warning | a GUC differs between instances of the same cluster, excluding the legitimately per-instance list from phase 5 sub-phase 5.7 |
  | `conn.saturation` | warning | `pg_connections_used_ratio` above 0.8 at any point in the window |
  | `conn.idle_share_high` | info | more than 70 % of connections are idle while the count is above 100 |
  | `archive.disabled` | info | `archive_mode` is off. Informational, not a fault: some deployments back up another way |
  | `archive.failing` | critical | `pg_archiver_failed_ratio` is 1 |
  | `archive.stalled` | critical | `pg_archiver_last_archived_age_seconds` exceeds ten times `archive_timeout`, or 1 hour when `archive_timeout` is 0 |
  | `backup.no_basebackup_seen` | warning | archiving is on and no `pg_stat_progress_basebackup` activity has been observed for 7 days. Degraded when history is shorter than 7 days |
  | `backup.no_strategy` | critical | `archive_mode` is off **and** no base backup has ever been observed — the instance has no visible backup strategy at all |

  `config.fsync_off` and `config.full_page_writes_off` deserve a remediation
  that says plainly what is at risk, in one sentence, without alarmism.

- **Unit tests:** one firing and one quiet test per rule, plus
  `TestPackC_MemoryRulesDegradeWithoutHost`,
  `TestPackC_UsableMemoryPrefersCgroupLimit`,
  `TestPackC_DriftExcludesPerInstanceSettings`,
  `TestPackC_AllRulesSatisfyContract`.
- **e2e tests:** covered by `SYS-ADV-001` and `SYS-ADV-003` in phase 9.
- **Done:** gates green + closed in `STATE.md`.

### 7.7 The advisor engine

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — degradation and lifecycle; review gate
- **Files:** `internal/advisor/engine.go` (new),
  `internal/advisor/store.go` (new),
  `internal/advisor/engine_test.go` (new),
  `internal/advisor/engine_integration_test.go` (new),
  `cmd/pglens-server/main.go` (modified)
- **Change:** the run loop, on the same leader pattern as the alert engine but a
  **different lock key**, `hashtext('pglens:advisor')`, so the two can be led by
  different replicas.

  ```go
  func NewEngine(pool *pgxpool.Pool, clk clock.Clock, interval time.Duration) *Engine
  func (e *Engine) Start(ctx context.Context)
  func (e *Engine) Stop()
  func (e *Engine) Run(ctx context.Context) error   // one full pass, exported for tests
  ```

  One pass does:
  1. Acquire or confirm leadership; a follower returns immediately.
  2. List instances that pushed within the last hour. An instance that has gone
     silent keeps its existing findings untouched — stale advice is better than
     advice that vanishes because the agent died, and the `agent_down` alert
     already covers the silence.
  3. For each instance: `LoadSnapshot`, then for each rule in `All()`:
     - if `snapshot.PermTier < rule.MinTier()`, emit a degraded finding naming
       the tier;
     - else if any name in `rule.Needs()` is absent from the snapshot or appears
       in `SkippedChecks`, emit a degraded finding naming the missing input;
     - else call `Evaluate` and collect what it returns.
     Recover from a panic in a single rule, log it with the rule id, emit a
     degraded finding for that rule, and continue. One bad rule must not stop
     the other twenty-nine.
  4. Upsert every finding by `finding_id`: insert with `first_seen = now` or
     update `last_seen`, `severity`, `detail`, `evidence` and clear
     `resolved_at`. A finding whose `muted_until` is in the future keeps
     `state = 'muted'` regardless of what the rule returned.
  5. Any finding for this instance that exists in the table with
     `state IN ('open','degraded')` and was **not** returned this run gets
     `state = 'resolved'` and `resolved_at = now`.
  6. Delete resolved findings older than 90 days.

  Default interval 15 minutes, from `PGLENS_ADVISOR_INTERVAL`.

  > Step 5 is the one a weak implementer usually gets wrong by resolving
  > findings belonging to another instance. Scope the resolution query by
  > `instance_id` and by the rule ids that actually ran, so a rule that panicked
  > does not resolve its own findings as a side effect.

- **Unit tests:** `TestRun_FollowerDoesNothing`,
  `TestRun_DegradesWhenTierTooLow`,
  `TestRun_DegradesWhenInputMissing`,
  `TestRun_PanicInOneRuleDoesNotStopOthers`,
  `TestRun_ResolvesFindingsNoLongerReturned`,
  `TestRun_DoesNotResolveOtherInstancesFindings`,
  `TestRun_DoesNotResolveFindingsOfAPanickedRule`,
  `TestRun_MutedFindingStaysMuted`,
  `TestRun_SkipsInstancesSilentForOverAnHour`.
- **e2e tests:** `INT-ADV-004` — a full pass against a seeded database produces
  the expected finding set and, on a second pass with the problem fixed, marks
  exactly those findings resolved.
  `INT-ADV-005` — two engines against one database produce one set of findings,
  not two.
- **Done:** gates green + `make test-integration` green + closed in `STATE.md`.

### 7.8 Findings API

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/server/api_findings.go` (new),
  `internal/server/api_findings_test.go` (new),
  `internal/server/api_findings_integration_test.go` (new),
  `internal/server/http.go` (modified)
- **Change:**

  | Route | Behavior |
  |-------|----------|
  | `GET /api/v1/findings` | filters `state` (default `open,degraded`), `severity`, `rule_id`, `instance_id`, `cluster_id`, `datname`, `scope`; ordered by severity then `last_seen` descending; `limit` default 100, max 1 000 |
  | `GET /api/v1/findings/{id}` | one finding with its full `evidence`; `404` when unknown |
  | `POST /api/v1/findings/{id}/mute` | body `{"reason": "...", "until": "<RFC3339>"}`; both required; `400` when `until` is in the past; sets `state = 'muted'` |
  | `DELETE /api/v1/findings/{id}/mute` | clears the mute; the next engine pass restores the real state |
  | `GET /api/v1/advisor/rules` | the registered rule catalogue: id, severity, scope, `needs`, `min_tier`. This is how an operator learns which rules exist without reading the source |

  Findings are ordered `critical`, `warning`, `info` — sort by an explicit
  severity rank expression in SQL, not alphabetically, because alphabetically
  `critical` comes before `info` before `warning`, which is nearly right and
  therefore the kind of bug that survives review.

- **Unit tests:** `TestFindingsAPI_DefaultStateFilter`,
  `TestFindingsAPI_SeverityOrderIsRanked`,
  `TestFindingsAPI_LimitCapped`,
  `TestFindingsAPI_MuteRequiresReasonAndUntil`,
  `TestFindingsAPI_MuteRejectsPastUntil`,
  `TestFindingsAPI_UnmuteClearsFields`,
  `TestAdvisorRulesAPI_ListsCatalogue`.
- **e2e tests:** `INT-ADV-006` — mute a finding, assert it is excluded from the
  default listing and included with `?state=muted`; unmute it and assert the next
  engine pass restores `open`.
- **Done:** gates green + `make test-integration` green + closed in `STATE.md`.

### 7.9 Advisor integration sweep

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — testing
- **Files:** `internal/advisor/rules_integration_test.go` (new),
  `test/fixtures/sql/advisor_seed.sql` (new)
- **Change:** build one seed script that creates, in a real database, a subject
  for every rule that can be provoked without a cluster: an unused index over
  10 MiB, a duplicate index pair, a redundant prefix pair, a table with more than
  20 % dead tuples, a never-vacuumed table above 100 000 rows, a bloated table
  above 100 MiB, a slow query run enough times to be ranked, and a database with
  a rollback ratio above 10 %.

  Then assert, in one test, that a full engine pass over that database returns
  **exactly** the expected `rule_id` set — no more, no fewer. Asserting the
  absence of unexpected findings is what catches a rule whose threshold is too
  loose, and it is the assertion most easily forgotten.

  Rules that need a cluster (`config.drift`, `index.divergence`,
  `config.wal_keep_size_low`) or a host (`config.*_mem_*`) are covered in phase 9
  instead, where a real topology exists.

- **Unit tests:** none (this sub-phase is the tests).
- **e2e tests:** `INT-ADV-007` — the exact-set assertion above.
  `INT-ADV-008` — the same seed at tier T0 with `pg_stats` invisible produces
  degraded findings for the bloat rules rather than open ones.
- **Done:** both green + closed in `STATE.md`.

### 7.10 Update README.md

Mandatory closing sub-phase of every phase. User guide only — no implementation
detail.

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — documentation
- **Files:** `README.md` (repo root)
- **Change:** update these sections for what this phase actually made usable:
  - **Advisor** — a new section: what a finding is and how it differs from an
    alert, the four states with what each means, that a degraded finding tells
    you what is missing rather than hiding, and how muting works.
  - **Configuration** — `PGLENS_ADVISOR_INTERVAL` with its default.
  - **Usage** — `curl` for listing findings filtered by severity, for muting one,
    and for listing the rule catalogue, each with expected JSON.
  - **Rules** — the full catalogue as a table: `rule_id`, severity, and one
    sentence on what it means. This is the reference an operator returns to, so
    it must list every rule, not a sample.
  - **Limitations** — findings are computed from collected statistics, not from
    query plans; index recommendations are candidates, not certainties. Rules
    that need seven days of history report as degraded until that history
    exists. Rules needing host memory are unavailable for remote instances.
- **Unit tests:** none (documentation).
- **e2e tests:** none — the `curl` examples were executed and produced the
  documented output.
- **Done:** an operator can read the whole rule catalogue and mute a finding from
  the README alone; the "candidates, not certainties" caveat sits with the index
  rules; gates green; closed in `STATE.md` with the §11 docs row for phase 7 set.

---

## Phase gates

- **Fmt:** `make fmt-check`
- **Lint:** `make lint`
- **Build:** `make build`
- **Test subset:** `make test`, `make test-integration`
- **Coverage:** `make coverage-gate`; `internal/advisor` at or above 85 %
- **Regression guard:** every phase 1 alert test and every phase 4 and 5 check
  test must still pass — the advisor reads their output and must not have
  required a change to it.
- **README:** the full rule catalogue is documented.

## Phase done criterion

A full advisor pass over a seeded database returns exactly the expected set of
`rule_id`s and nothing else, a fixed problem is marked resolved on the next
pass, a rule whose input is missing produces a degraded finding naming what is
missing, a panicking rule does not stop the others, and two server replicas
produce one set of findings. `INT-ADV-001` to `INT-ADV-008` are green. README.md
reflects this phase's shipped behavior, and `STATE.md` §11 shows phase 7 `DONE`
with every sub-phase closed.
