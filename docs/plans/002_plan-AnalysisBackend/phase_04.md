# Phase 3 — Contention: locks, activity, transactions

> **Intent:** answer "who is blocking whom, and for how long" — the blocking
> tree, a richer activity breakdown, long and abandoned transactions, and the
> deadlock signal.
> **Shippable alone?** yes — one new check, one extended check, two new
> endpoints. Nothing existing changes shape.
> **Preconditions:** phase 2 `DONE` (the `facts` transport is required by 3.1).

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

## How to add a check (read this before 3.1)

Every new check in phases 3 to 6 follows the same five steps. They are written
out once, here, and referenced afterwards.

1. Create `internal/check/<name>.go` in package `check`.
2. Declare an unexported struct type and register it:
   ```go
   func init() { Register(&locksCheck{}) }
   ```
   `Register` panics on a duplicate name (`internal/check/registry.go:15`), so
   the name must be unique across the whole registry.
3. Implement the five methods of `check.Check` (`internal/check/check.go:75`):
   `Name`, `Requires`, `DefaultInterval`, `Timeout`, `Scrape`.
4. `Scrape` obtains its connection from the `Target`: `t.Conn(ctx)` for an
   instance-scope check, `t.ConnFor(ctx, t.Database())` for a database-scope
   one. Call `check.ApplySessionLimits(ctx, conn, c.Timeout())` as the first
   statement after acquiring the connection, always, and `defer conn.Release()`.
5. Add a unit test using the mock connection in
   `internal/check/mockconn_test.go` and an integration test in
   `<name>_integration_test.go` behind the `integration` build tag.

Requirements are declarative and are what keeps invariant I-6 true: a check that
needs more than tier T0 says so in `Requires()` and is excluded before it runs,
never failing at runtime.

---

## Sub-phases

### 3.1 The `locks` check

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation; `agent:gpt5.6-luna` reviews the
  lock query, because a wrong join here reports the wrong blocker
- **Files:** `internal/check/locks.go` (new),
  `internal/check/locks_test.go` (new),
  `internal/check/locks_integration_test.go` (new)
- **Change:** implement a check named `locks`.

  ```go
  Requirements{
      Scope:    ScopeInstance,
      PermTier: pgtype.TierReadOnly, // T0
      // no Roles restriction: a standby can have lock contention too
  }
  DefaultInterval() = 10 * time.Second
  Timeout()         = 10 * time.Second   // IDEA.md section 4.4
  ```

  `Scrape` runs exactly one query. Use `pg_blocking_pids()`, which is available
  across the whole supported range and does the lock-graph resolution in the
  server, instead of self-joining `pg_locks`, which is easy to get subtly wrong:

  ```sql
  SELECT a.pid,
         a.datname,
         a.usename,
         a.application_name,
         a.client_addr::text                                    AS client_addr,
         a.state,
         COALESCE(a.wait_event_type, '')                         AS wait_event_type,
         COALESCE(a.wait_event, '')                              AS wait_event,
         EXTRACT(epoch FROM now() - a.xact_start)                AS xact_age,
         EXTRACT(epoch FROM now() - a.state_change)              AS state_age,
         COALESCE(a.query, '')                                   AS query,
         pg_blocking_pids(a.pid)                                 AS blocked_by
  FROM pg_stat_activity a
  WHERE a.backend_type = 'client backend'
    AND a.pid <> pg_backend_pid()
  ```

  From those rows produce **both** outputs.

  **Metrics** (they go to the generic `metrics` table; no new hypertable):

  | Metric | Kind | Labels | Meaning |
  |--------|------|--------|---------|
  | `pg_blocked_sessions` | gauge | none | rows with a non-empty `blocked_by` |
  | `pg_blocking_sessions` | gauge | none | distinct pids appearing in some `blocked_by` |
  | `pg_max_block_age_seconds` | gauge | none | greatest `state_age` among blocked rows |
  | `pg_lock_waits` | gauge | `wait_event` | count of blocked rows per lock wait event |

  Keep the `wait_event` label bounded: emit at most the ten most frequent wait
  events and fold the rest into `wait_event="other"`. This is the same discipline
  the existing `activity` check applies and it stops a pathological workload from
  creating a series per lock type.

  **One fact** of kind `lock_tree`, key `"current"`, `ValueJSON` holding:

  ```json
  {
    "sampled_at": "2026-08-29T10:00:00Z",
    "nodes": [
      {"pid": 123, "datname": "app", "usename": "app", "application_name": "api",
       "client_addr": "10.0.0.4", "state": "active", "wait_event_type": "Lock",
       "wait_event": "transactionid", "xact_age": 41.2, "state_age": 39.8,
       "query": "UPDATE ...", "blocked_by": [456]}
    ]
  }
  ```

  Emit the fact **only when at least one row is blocked**. A tree of zero
  blocked sessions is noise, and writing it every 10 s per instance would churn
  `lock_snapshots` for nothing.

  > **Query text is potentially sensitive.** Truncate `query` to 2 048 bytes and
  > do not attempt to normalise or parameterise it — the existing
  > `stat_statements` check already owns query-text handling and its rules apply
  > here too. State the truncation in the README in sub-phase 3.8.

- **Unit tests:** in `internal/check/locks_test.go`, driving the mock connection —
  `TestLocks_NoBlockedSessionsEmitsNoFact`,
  `TestLocks_CountsBlockedAndBlocking`,
  `TestLocks_MaxBlockAgeIsTheGreatest`,
  `TestLocks_WaitEventLabelIsBounded` (feed 30 distinct wait events, assert 11
  series with one labelled `other`),
  `TestLocks_TreeJSONShape`,
  `TestLocks_QueryIsTruncated`,
  `TestLocks_RequiresTierT0`.
- **e2e tests:** `INT-LOCK-001` — against a real container, open a transaction
  holding a row lock, start a second session that blocks on it, run `Scrape`,
  and assert `pg_blocked_sessions == 1`, that the tree names the blocker's pid in
  the blocked node's `blocked_by`, and that `wait_event` is
  `transactionid`.
  `INT-LOCK-002` — with no contention, `Scrape` emits the gauges at zero and no
  fact.
  `INT-LOCK-003` — a session holding an `ACCESS EXCLUSIVE` lock while another
  runs `SELECT` is reported with `wait_event = relation`, and `Scrape` returns
  within its 10 s timeout even though the blocked query would wait forever.
- **Done:** gates green (`make fmt-check`, `make lint`, `make build`,
  `make test`, `make test-integration`) + closed in `STATE.md`.

### 3.2 Lock snapshot storage

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/store/migrations/0008_locks.sql` (new),
  `internal/store/write.go` (modified),
  `internal/server/pipeline.go` (modified),
  `internal/server/pipeline_integration_test.go` (modified)
- **Change:**
  1. Migration:

     ```sql
     -- Only the latest tree per instance is kept (plan 002 D17). A history of
     -- lock trees has no consumer and would be the highest-churn table in the
     -- store.
     CREATE TABLE IF NOT EXISTS lock_snapshots (
       tenant_id   text        NOT NULL DEFAULT 'default',
       instance_id uuid        NOT NULL,
       cluster_id  bigint      NOT NULL,
       ts          timestamptz NOT NULL,
       tree        jsonb       NOT NULL,
       PRIMARY KEY (tenant_id, instance_id)
     );
     ```

  2. `store.WriteLockSnapshot` upserts on the primary key, replacing `ts` and
     `tree`, and **only when the incoming `ts` is newer** than the stored one:
     `WHERE EXCLUDED.ts > lock_snapshots.ts`. A buffered replay after an outage
     must not overwrite a fresh tree with a stale one.
  3. In `internal/server/pipeline.go`, replace the `// phase 3` placeholder left
     by sub-phase 2.4 with the real routing for `lock_tree` facts.
  4. Prune: rows whose `ts` is older than 15 minutes are deleted on each ingest
     of a lock fact for that instance — one `DELETE` bounded by the primary key,
     not a table scan. A stale tree is worse than none because it looks live.

- **Unit tests:** `TestWriteLockSnapshot_ArgsOrder`,
  `TestPipeline_RoutesLockTreeFact`.
- **e2e tests:** `INT-LOCK-004` — ingest a lock fact, assert one row; ingest an
  older one, assert the row did not change; ingest a newer one, assert it did.
- **Done:** gates green + `make test-integration` green + closed in `STATE.md`.

### 3.3 Extend the `activity` check

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/check/activity.go` (modified),
  `internal/check/activity_test.go` (modified),
  `internal/check/activity_integration_test.go` (new)
- **Change:** the check already emits per-`(state, wait_event_type)` counts,
  `max_xact_age`, `max_idle_in_txn` and reads `max_connections`
  (`internal/check/activity.go:41`). Add, without removing or renaming anything
  that exists — plan 001's assertions depend on the current metric names:

  | Metric | Kind | Labels | Source |
  |--------|------|--------|--------|
  | `pg_connections_used_ratio` | gauge | none | `numbackends` over `max_connections`; feeds the `conn.near_max` rule seeded in phase 0 |
  | `pg_connections_by_database` | gauge | `datname` | count per `datname`, top 10 by count with the rest folded into `datname="other"` |
  | `pg_connections_by_application` | gauge | `application_name` | top 10, rest folded into `other`; **disabled unless `checks.activity.by_application: true`** (see Q-B) |
  | `pg_max_state_age_seconds` | gauge | `state` | greatest `now() - state_change` per state |
  | `pg_prepared_xacts` | gauge | none | `SELECT count(*) FROM pg_prepared_xacts` |
  | `pg_oldest_prepared_xact_seconds` | gauge | none | `EXTRACT(epoch FROM now() - min(prepared))` from `pg_prepared_xacts`, zero when empty |
  | `pg_max_datfrozenxid_age` | gauge | none | `SELECT max(age(datfrozenxid)) FROM pg_database`; feeds `txn.wraparound_risk` |
  | `pg_backends_waiting_ratio` | gauge | none | backends with a non-null `wait_event` over total client backends |

  Add the three extra queries as separate statements after the existing one, in
  the same connection and inside the same session limits. Do not merge them into
  the existing aggregate query: it is asserted verbatim by existing tests, and a
  rewrite risks a silent behavior change for no gain.

  `pg_prepared_xacts` is empty on almost every installation. When it is empty,
  still emit both metrics at zero — a metric that appears only when broken is a
  metric nobody has a dashboard for.

- **Unit tests:** in `internal/check/activity_test.go` —
  `TestActivity_ExistingMetricsUnchanged` (assert the exact set of metric names
  the check emitted before this sub-phase is still emitted),
  `TestActivity_ConnectionsUsedRatio`,
  `TestActivity_ByDatabaseIsBounded`,
  `TestActivity_ByApplicationDisabledByDefault`,
  `TestActivity_PreparedXactsZeroWhenEmpty`,
  `TestActivity_MaxDatfrozenxidAge`.
- **e2e tests:** `INT-ACT-001` — against a real container with three idle
  connections open, `pg_connections_by_database` reports them under the right
  `datname` and `pg_connections_used_ratio` is between 0 and 1.
  `INT-ACT-002` — with `PREPARE TRANSACTION` executed and left dangling,
  `pg_prepared_xacts` is 1 and `pg_oldest_prepared_xact_seconds` is positive;
  the test rolls the prepared transaction back in its cleanup so the container
  snapshot restore is not left holding a lock.
- **Done:** gates green + `INT-CHECK-015` (every check green as T0) still passes
  + closed in `STATE.md`.

### 3.4 Transaction and rollback signals

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/check/database_stats.go` (modified),
  `internal/check/database_stats_test.go` (modified)
- **Change:** the check already emits `pg_xact_commit_total`,
  `pg_xact_rollback_total` and `pg_deadlocks_total`
  (`internal/check/database_stats.go:79`). Add two derived gauges the advisor
  needs and the alert rules reference:

  | Metric | Kind | Labels | Definition |
  |--------|------|--------|------------|
  | `pg_xact_rollback_ratio` | gauge | `datname` | `xact_rollback / NULLIF(xact_commit + xact_rollback, 0)`, computed in SQL, omitted when the denominator is zero |
  | `pg_blks_hit_ratio` | gauge | `datname` | `blks_hit / NULLIF(blks_hit + blks_read, 0)`, omitted when the denominator is zero |

  Both are **ratios over the lifetime counters**, not over the interval. State
  that in the metric documentation, because the difference matters when reading
  them: a database that ran badly last year still shows it here. The interval
  view is available by taking the rate of the underlying counters, which the
  delta engine already produces.

  > Omitting a metric when the denominator is zero is deliberate and is the
  > honesty principle: a freshly started database has no ratio, and emitting
  > zero would render as "0 % cache hit" on every dashboard.

- **Unit tests:** `TestDatabaseStats_RollbackRatio`,
  `TestDatabaseStats_HitRatio`,
  `TestDatabaseStats_RatiosOmittedWhenNoTraffic`,
  `TestDatabaseStats_ExistingMetricsUnchanged`.
- **e2e tests:** covered by `INT-ACT-001`'s container; no separate test needed.
- **Done:** gates green + closed in `STATE.md`.

### 3.5 Contention API

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/server/api_locks.go` (new),
  `internal/server/api_locks_test.go` (new),
  `internal/server/api_locks_integration_test.go` (new),
  `internal/server/http.go` (modified)
- **Change:** two endpoints, registered the same way as sub-phase 1.5.

  `GET /api/v1/locks?instance_id=<uuid>` returns the stored tree:

  ```json
  {
    "instance_id": "…",
    "sampled_at": "2026-08-29T10:00:00Z",
    "stale": false,
    "nodes": [ … ]
  }
  ```

  `stale` is `true` when `sampled_at` is older than three times the check
  interval. When there is no row at all, return `200` with
  `{"instance_id": "…", "sampled_at": null, "stale": true, "nodes": []}` — not
  `404`. An absent tree means "no contention observed", which is a valid answer,
  and a `404` would make a client show an error for a healthy system.

  `GET /api/v1/instances/{id}/activity` returns the latest values of the
  activity metrics, grouped: totals, per state, per database, per application
  when enabled, plus the three age gauges. Read them from `metrics` with a
  lookback of three intervals and mark each group `stale` on the same rule.

  `GET /api/v1/instances/{id}/databases` also lands here, because it is the
  companion the same client needs: monitored databases and skipped ones with
  their reason, read from the `databases` meta table
  (`internal/store/migrations/0001_meta.sql:52`), plus a `not_monitored_count`.

- **Unit tests:** `TestLocksAPI_AbsentTreeIsEmptyNotFound`,
  `TestLocksAPI_StalenessThreshold`,
  `TestActivityAPI_GroupsByState`,
  `TestDatabasesAPI_ReportsSkipReason`.
- **e2e tests:** `INT-LOCK-005` — end to end from an ingested lock fact to the
  API response, asserting the blocked pid and its blocker survive the round
  trip.
  `INT-ACT-003` — the activity endpoint reports a database whose connections
  were just observed.
- **Done:** gates green + `make test-integration` green + closed in `STATE.md`.

### 3.6 Agent configuration for the new check

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/agent/config.go` (modified),
  `deploy/agent.example.yaml` (modified),
  `internal/agent/config_test.go` (modified)
- **Change:** the config already carries a generic `checks: map[string]CheckConfig`
  (`internal/agent/config.go:59`) with `interval` and `top_n`, so `locks` needs
  no new structure. Add only the one flag that has no generic home:

  ```go
  type CheckConfig struct {
      Interval      string `yaml:"interval"`
      TopN          int    `yaml:"top_n"`
      ByApplication bool   `yaml:"by_application"` // activity check only
      Enabled       *bool  `yaml:"enabled"`
  }
  ```

  Update `deploy/agent.example.yaml` with a commented block showing `locks` with
  its default interval and `activity` with `by_application: false`, in the style
  of the entries already there.

- **Unit tests:** `TestConfig_LocksDefaultInterval`,
  `TestConfig_ByApplicationDefaultsFalse`,
  `TestConfig_ExampleFileParses` (parse `deploy/agent.example.yaml` from the
  test so a typo in the shipped example fails the build; check whether such a
  test already exists before adding a second one).
- **e2e tests:** none.
- **Done:** gates green + `INT-CFG-001` (the `pglens-agent check` command) still
  passes and now lists `locks` among the enabled checks + closed in `STATE.md`.

### 3.7 Contention integration test sweep

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — testing
- **Files:** `internal/check/locks_integration_test.go` (extended),
  `internal/check/activity_integration_test.go` (extended)
- **Change:** run the contention checks across the full version matrix, using
  the existing `test/pgtest` per-version harness. The lock and activity views
  differ subtly between 15 and 18 — for example the set of `backend_type` values
  — and a check that works on 17 and silently returns nothing on 15 is exactly
  the failure this sweep exists to catch.
- **Unit tests:** none (this sub-phase is the tests).
- **e2e tests:** `INT-LOCK-006` — `INT-LOCK-001` repeated on every version in
  the matrix, asserting identical metric names and identical tree shape.
  `INT-ACT-004` — the activity metric name set is identical on every version.
- **Done:** both tests green on all four versions + closed in `STATE.md`.

### 3.8 Update README.md

Mandatory closing sub-phase of every phase. User guide only — no implementation
detail.

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — documentation
- **Files:** `README.md` (repo root)
- **Change:** update these sections for what this phase actually made usable:
  - **Checks** — add `locks` to the table of collected checks with its interval,
    scope and permission tier, and note the extended `activity` metrics.
  - **Configuration** — `checks.locks.interval` and
    `checks.activity.by_application` with their defaults.
  - **Usage** — a `curl` against `/api/v1/locks` and one against
    `/api/v1/instances/{id}/activity`, each with its expected JSON.
  - **Limitations** — the lock view is **sampled every 10 seconds, not live**; a
    contention episode shorter than the interval can be missed entirely. Query
    text in the lock tree is truncated to 2 048 bytes. Deadlocks are reported as
    a counter and a rate; identifying the statements involved in a specific
    deadlock requires PostgreSQL log analysis, which pglens does not do.
  Preserve the existing README structure, tone and language; edit, do not
  rewrite.
- **Unit tests:** none (documentation).
- **e2e tests:** none — the `curl` examples were executed and produced the
  documented output.
- **Done:** a new user can read the lock tree from the README alone; the
  sampling limitation is stated where a reader will find it before trusting the
  data; gates green; closed in `STATE.md` with the §11 docs row for phase 3 set.

---

## Phase gates

- **Fmt:** `make fmt-check`
- **Lint:** `make lint`
- **Build:** `make build`
- **Test subset:** `make test`, `make test-integration`
- **Coverage:** `make coverage-gate`
- **Regression guard:** `INT-CHECK-015` (every check green at T0),
  `INT-CFG-001`, and every existing `activity` and `database_stats` unit test
  must pass unchanged.
- **README:** the sampling limitation and the truncation are documented.

## Phase done criterion

With two sessions in conflict on a real instance, `GET /api/v1/locks` returns a
tree naming the blocker and the blocked session, `pg_blocked_sessions` is 1, and
the `locks` check completes inside its 10 s timeout while the blocked statement
is still waiting. The activity endpoint reports connections per database and the
prepared-transaction gauges. `INT-LOCK-001` to `INT-LOCK-006` and `INT-ACT-001`
to `INT-ACT-004` are green on every supported PostgreSQL version. README.md
reflects this phase's shipped behavior, and `STATE.md` §11 shows phase 3 `DONE`
with every sub-phase closed.

## Execution record

- **Assignment:** `agent:gpt5.6-luna`
- **Status:** complete
- **Result:** 3.1–3.8 completed in order. INT-LOCK-001..006 and INT-ACT-001..004
  pass across the supported PostgreSQL matrix. INT-GOLDEN-001 remains an active
  v1 compatibility check with its fixtures unchanged; the harness excludes only
  the additive phase-3 ratio metrics, while v2 golden coverage remains separate.
  Blocked-session cleanup waits deterministically for the probe goroutine before
  releasing pooled connections.
- **Final gates:** `make fmt-check`, `make lint`, `make build`, `make test`,
  `make coverage-gate` (75.1%, 3720/4956), and `make test-integration` all pass.
