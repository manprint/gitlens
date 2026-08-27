# Phase 7 — ASH (Active Session History)

> **Intent:** Implement the project's principal differentiator — 1-second
> sampling of `pg_stat_activity`, aggregated agent-side into 10-second windows,
> stored in a typed hypertable and queryable through the API — and prove it
> against real contention.
> **Shippable alone?** yes — adds one check, one aggregator, one migration and
> API surface.
> **Preconditions:** phase 6 DONE.

`IDEA.md` §3.5 argues this is what separates the product from pgwatch2 and PMM:
sampling `pg_stat_activity` every 5 to 15 seconds misses short queries entirely
and makes wait-event analysis useless, and the existing open-source answer
(`pg_wait_sampling`) needs `shared_preload_libraries` and therefore cannot be
installed on any managed platform. This approach is pure SQL and works
everywhere, RDS included.

Decision D15 places it inside this plan rather than after it, deliberately: ASH
is the hardest consumer of the data model, and discovering in a later plan that
the schema cannot carry it would be the worst possible time to find out.

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

### 7.1 The 1-second sampler

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — a high-frequency loop that must stay cheap and must never interfere with anything else.
- **Files:** `internal/ash/sampler.go`, `internal/check/ash.go`, `internal/agent/conn.go` (connection reservation), `internal/ash/sampler_test.go`
- **Change:**
  ```sql
  SELECT COALESCE(datname, '')             AS datname,
         COALESCE(state, 'unknown')        AS state,
         COALESCE(wait_event_type, 'CPU')  AS wait_event_type,
         COALESCE(wait_event, 'CPU')       AS wait_event,
         query_id
    FROM pg_stat_activity
   WHERE backend_type = 'client backend'
     AND state <> 'idle'
     AND pid <> pg_backend_pid();
  ```
  - `wait_event_type IS NULL` means the backend is **running, not waiting**. The
    convention here maps that to the synthetic type `CPU`, matching how every
    other ASH implementation presents it. Discarding those rows instead would
    make the resulting chart claim the database spends no time computing.
  - `state <> 'idle'` excludes sessions doing nothing, which are the majority and
    carry no information. `idle in transaction` is deliberately **kept**: an idle
    transaction is not doing work but is actively causing harm.
  - `pid <> pg_backend_pid()` excludes the sampler itself. Without it the sampler
    is permanently one active session in its own results, which is both wrong and
    self-referential in a way that would quietly bias every measurement.
  - `query_id` is unconditionally available across the supported range because
    decision D11 sets the floor at PostgreSQL 15 and `compute_query_id` puts it
    in core from 14. When `compute_query_id` is `off` the column is NULL; the
    sampler records that and the check reports a warning once, since ASH without
    query correlation is a much weaker product and the user should know.

  **Connection ownership.** The sampler runs on its **own dedicated connection**,
  not the shared one: a 1-second loop contending with the other checks for a
  single connection would distort both. This raises the per-instance connection
  budget from 3 to 4 (1 shared, 1 ASH, 2 rotating for per-database checks).

  > **Cross-phase change, must be recorded.** Update the `MaxConnsPerInstance`
  > default in `internal/agent/conn.go` from 3 to 4, update the assertion in
  > `INT-CONN-001` accordingly, update the README configuration table, and record
  > the change in `STATE.md` §8 as a deviation with this sub-phase as its cause.
  > Do not leave the two values disagreeing.

  - `Timeout` is **500ms**. An ASH sample that takes longer than half its own
    period is worthless, and the right response is to drop that tick and count
    it, not to queue up.
  - a missed or failed tick increments `pglens_ash_ticks_missed_total` and is
    excluded from the window's tick count. **The denominator must be ticks that
    actually happened**; using the expected count instead would silently
    understate load whenever the sampler was struggling, which is exactly when
    the numbers matter.
  - configurable: `ash: {interval: 1s, enabled: true}`; setting `interval: 5s`
    or `enabled: false` is supported for sensitive instances.
- **Unit tests:** parsing tests over fixture rows: NULL `wait_event_type` becomes `CPU`; NULL `datname` becomes `""`; NULL `query_id` is preserved as absent, never as 0 (`queryid` 0 is not a valid id and would collide with a real one in the dedup index). `TestSampler_ExcludesSelf` verified at L2. `TestSampler_TickBudget` — with the fake clock, 60 ticks are attempted over `Advance(60s)` and a tick that exceeds its timeout is counted as missed rather than retried.
- **Integration tests:** `INT-ASH-001` — against a real container with 3 sleeping sessions, one sample returns exactly 3 rows and none of them is the sampler. `INT-ASH-002` — a session in `idle in transaction` appears; a plain `idle` session does not. `INT-ASH-003` — with `compute_query_id=on`, `query_id` is non-NULL for an active query and matches the `queryid` reported by `stat_statements` for the same statement. **This is the join that makes ASH useful, so it is asserted rather than assumed.** `INT-ASH-004` — with `compute_query_id=off`, the sampler still works, `query_id` is NULL, and the warning is emitted once rather than per tick.
- **e2e tests:** `SYS-LOAD-003`, `SYS-LOAD-005` in 7.4.
- **Done:** `make test-integration` green on 15 and 18; the connection-budget change is applied consistently in code, tests and README and recorded in `STATE.md` §8; closed in `STATE.md`.

### 7.2 The aggregator

- **Model:** `agent-1:opus`
- **Assignment:** `agent-1:opus` — aggregation correctness. **`agent-1` review gate by construction:** this is where a plausible-looking chart can be quietly wrong. Coverage gate 90%.
- **Files:** `internal/ash/aggregator.go`, `internal/ash/aggregator_test.go`
- **Change:** raw samples are never shipped. Sixty samples per second per
  instance would dominate the payload and the storage; the agent aggregates into
  10-second windows and sends counts.
  ```go
  type Key struct {
  	Datname       string
  	WaitEventType string
  	WaitEvent     string
  	State         string
  	QueryID       int64 // 0 means absent; carried as NULL on the wire
  	HasQueryID    bool
  }

  type Bucket struct {
  	Key     Key
  	Samples int
  }

  type Window struct {
  	Start, End time.Time
  	Ticks      int // sampling ticks that actually succeeded
  	Buckets    []Bucket
  	Truncated  bool
  }

  type Aggregator struct{ /* clock, window length, cap, counts map[Key]int */ }

  func New(clk clock.Clock, window time.Duration, maxKeys int) *Aggregator
  func (a *Aggregator) Add(datname, state, wet, we string, queryID *int64)
  func (a *Aggregator) TickFailed()
  // Flush closes the current window and returns it, resetting internal state.
  func (a *Aggregator) Flush(now time.Time) Window
  ```
  **Cardinality cap with conservation.** When more than `maxKeys` (default 100)
  distinct keys appear in a window, keep the top `maxKeys - 1` by `Samples` and
  fold everything else into a single bucket with
  `WaitEventType = "other"`, `WaitEvent = "other"`, `State = "other"`,
  `Datname = ""`, no `QueryID`. Set `Truncated = true`.

  > **The conservation invariant:** the sum of `Samples` across all buckets of a
  > window is **exactly** the number of `Add` calls in that window, before and
  > after capping. Dropping the tail instead of folding it would silently
  > understate total database activity, and a wait-event chart whose total is
  > wrong is worse than no chart — it looks authoritative and is not. This is
  > asserted directly, including in the fuzz test.

  Derived quantity, computed by the API and never stored: **average active
  sessions = `sum(samples) / ticks`**. Using the window length in seconds as the
  denominator would be wrong whenever ticks were missed, which is precisely when
  the instance is under stress.
- **Unit tests:**
  `TestAggregator_CountsPerKey` — 30 samples across 3 keys produce 3 buckets with the right counts.
  `TestAggregator_NullWaitBecomesCPU` — handled upstream in 7.1, asserted here as a round-trip through the key.
  `TestAggregator_QueryIDNilIsNotZero` — a nil `queryID` and a `queryID` of 0 produce **different** keys.
  `TestAggregator_WindowBoundaries` — samples added after `Flush` belong to the next window; `Flush` on an empty window returns `Ticks` and zero buckets, not nil.
  `TestAggregator_TicksCounted` — 10 successful ticks and 2 failed ticks yield `Ticks == 10`.
  `TestAggregator_CapConservesTotal` — 500 distinct keys with `maxKeys` 100 produce exactly 100 buckets, `Truncated == true`, and **the sum of `Samples` equals the number of `Add` calls**.
  `TestAggregator_CapKeepsTopKeys` — the 99 highest-count keys survive individually.
  `TestAggregator_NoCapNoTruncated` — under the cap, `Truncated` is false and there is no `other` bucket.
  `TestAggregator_Concurrent` — concurrent `Add` and `Flush` clean under `-race`.
  `FuzzAggregator_ConservesTotal` — for any fuzzed sequence of adds and flushes, the total is conserved and nothing panics.
- **Integration tests:** none — this is pure logic and belongs at L1.
- **e2e tests:** `SYS-ASH-001` in 7.4 proves conservation end to end.
- **Done:** `make test` green under `-race -shuffle=on`; `go tool cover -func` reports **>= 90%** for `internal/ash`; the conservation property holds in both the unit and the fuzz test; closed in `STATE.md`.

### 7.3 Storage and API

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — extends the phase 3 pipeline and API.
- **Files:** `internal/store/migrations/0004_ash_ticks.sql`, `internal/server/pipeline.go`, `internal/server/api_ash.go`, `internal/server/it_api_ash_test.go`
- **Change:**
  1. Migration `0004_ash_ticks.sql` adds the denominator to the table created in
     sub-phase 3.1:
     ```sql
     ALTER TABLE metrics_ash ADD COLUMN window_ticks int NOT NULL DEFAULT 0;
     ```
     Forward-only and idempotent like every other migration. It exists because
     the honest average requires the tick count, and inferring it from
     `window_seconds` would be wrong exactly when the sampler was struggling.
  2. Pipeline routing: ASH results go to `metrics_ash`. **The buckets are counts,
     not counters** — they never pass through `internal/delta`. Each window is an
     independent observation, so a reset has no meaning here. Assert this with an
     explicit branch and a test, because routing ASH through the delta engine
     would produce a chart that is subtly and permanently wrong.
  3. `GET /api/v1/ash?instance_id=&from=&to=&group_by=&database=&limit=`
     - `group_by` accepts `wait_event_type`, `wait_event`, `state`, `queryid`,
       or a comma-separated combination
     - returns, per time bucket and group: `samples`, `ticks`, and
       `avg_active_sessions = samples / ticks`
     - a bucket with `ticks == 0` returns `avg_active_sessions: null` — never a
       division by zero, and never a zero standing in for "unknown"
     - the response carries `"resolution_seconds": 1` and
       `"statistical": true`, so any consumer knows what it is looking at
       without having to read the documentation
     - `GET /api/v1/ash/top?…` returns the top queries by ASH samples, joined to
       `query_texts`, which is the "where is my database spending time" answer
  4. **Significance guard:** when a requested range contains fewer than 60 total
     ticks, the response includes
     `"warning": "fewer than 60 samples in range; results are not statistically significant"`.
     Reporting a confident-looking breakdown built on nine samples is precisely
     the way an honest sampling method turns into a misleading one.
- **Unit tests:** handler tests with a fake store: the `null` for `ticks == 0`; the significance warning at 59 and its absence at 60; `group_by` validation rejecting unknown fields with 400; the `avg_active_sessions` arithmetic.
- **Integration tests:** `INT-ASH-010` — ingest three ASH windows and assert the API returns the right `avg_active_sessions`. `INT-ASH-011` — the significance warning appears below 60 ticks and not at or above it. `INT-ASH-012` — `group_by=queryid` joins to `query_texts` and returns the text. `INT-ASH-013` — migration `0004` applies cleanly to a database already migrated to `0003`, and re-running changes nothing. `INT-ASH-014` — ASH rows never appear in `metrics` and never pass through the delta engine, asserted by a row count on both tables.
- **e2e tests:** `SYS-LOAD-003`, `SYS-LOAD-005`, `SYS-ASH-001`.
- **Done:** `make test-integration` green; migration `0004` applies to an existing database; closed in `STATE.md`.

### 7.4 ASH scenarios

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — scenario implementation.
- **Files:** `test/scenario/ash.go`, `test/e2e/ash_test.go`
- **Change:**

  | ID | Scenario | Assertion |
  |----|----------|-----------|
  | `SYS-LOAD-003` | `workloadctl slow-query --sleep 30s --count 3` | ASH shows roughly 3 average active sessions during the window; the dominant `wait_event` is `PgSleep` (type `Timeout`); `group_by=queryid` attributes the samples to the sleeping statement's `queryid`, and that `queryid` also appears in `/api/v1/statements`. **The cross-check is the point:** ASH and `pg_stat_statements` must agree about which query it was |
  | `SYS-LOAD-005` | `workloadctl race --workers 20 --rows 100` | the dominant `wait_event_type` is `Lock` with `wait_event = transactionid`; `avg_active_sessions` rises above 5 |
  | `SYS-LOAD-002` | the lock storm from phase 5, re-asserted through ASH | the storm is visible as `Lock` waits, and **the ASH sampler kept sampling throughout** — `ticks` for every window in the storm is at least 8 of 10. This is the concrete proof that a 500ms timeout on a lightweight query survives an incident, which is the whole premise of the design |
  | `SYS-ASH-001` | idle instance, then 10 known sessions for 30 seconds | **conservation end to end:** the sum of `samples` over the window equals `ticks x observed_sessions` within a 10% tolerance, and no window has `samples > ticks x max_connections`, which would be arithmetically impossible and would indicate double counting |
  | `SYS-ASH-002` | ASH disabled in configuration | no `metrics_ash` rows are written, no error is produced, and `/api/v1/ash` returns an empty result with an explicit `"enabled": false` rather than an empty result indistinguishable from an idle database |

  `SYS-LOAD-003` joins the **smoke set**. Every scenario ends with
  `AssertInvariants`.

  > `SYS-ASH-002` matters more than it looks. "No data" and "feature switched
  > off" render identically unless the API says which one it is, and a user
  > staring at an empty chart deserves to be told the difference.
- **Unit tests:** none (these are the tests).
- **e2e tests:** the five above.
- **Done:** `make test-e2e` (smoke, now including `SYS-LOAD-003`) green for `AGENT_MODE={container,binary}`; `make test-e2e-full` green for all three; closed in `STATE.md`.

### 7.5 Update README.md — final documentation pass

Mandatory closing sub-phase of every phase, and the final documentation gate of
the plan. User guide only — no implementation detail.

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — documentation. **`agent-1:opus` review gate: the final read of the complete README.**
- **Files:** `README.md`, `CONTRIBUTING.md`
- **Change:**
  1. Add **Wait-event analysis** to the README:
     - what it answers: where the database is spending its time, right now and
       historically
     - that it needs no extension and no restart, and therefore works on managed
       PostgreSQL including Amazon RDS
     - that `compute_query_id = on` is strongly recommended and what is lost
       without it (samples can no longer be attributed to a query)
     - `curl` examples for `/api/v1/ash` grouped by wait event and by query, with
       real trimmed output
     - configuration: `ash.interval`, `ash.enabled`, and when to lower the rate
  2. Extend **Known limits** with the honest description of the method, in the
     user's words rather than the implementer's:
     - it is **statistical sampling at 1 second**, not exact tracing; queries
       shorter than about a second are under-represented
     - fewer than 60 samples in a range is not statistically meaningful, and the
       API says so in the response
     - the same trade-off as Oracle ASH and AWS Performance Insights — worth
       stating, because it tells a reader this is a known technique rather than a
       shortcut
     - at most 100 distinct wait keys per window; the remainder is folded into
       `other` and the totals stay correct
  3. **Full README review by `agent-1:opus`**, covering the whole document, not
     just this phase's additions:
     - every command and `curl` in it executes from a clean checkout and produces
       the documented output — verify each one
     - no implementation detail anywhere: no Go package, type, table, column or
       internal file path
     - the **Known limits** section is complete against everything the plan
       actually proved: PostgreSQL 15 to 18 only; streaming replication only; no
       token rotation or mTLS; single-tenant; 30-day raw retention with no
       rollups; ASH is statistical; an agent container without a persistent
       volume duplicates its instances; an unclean shutdown may lose the tail of
       the buffer; 10 databases per instance by default; oversized envelopes are
       dropped rather than split; alerts are available through the API only, with
       no delivery channels
     - the structure reads as one document rather than seven appended phases
  4. `CONTRIBUTING.md` — add the ASH level to the test-level guidance and the
     rule that ASH counts must never be routed through the delta engine.
- **Unit tests:** none (documentation).
- **e2e tests:** none — every command in the README was executed from a clean checkout during the final review.
- **Done:** `agent-1:opus` has read the complete README and confirms every command works as documented, no implementation detail is present, and the limits section matches what the plan proved; a new user can go from a clean machine to a monitored replicated cluster with wait-event analysis using the README alone; all gates green; closed in `STATE.md` with the §11 docs row for phase 7 set.

---

## Phase gates

- **Fmt:** `make fmt-check`
- **Lint:** `make lint`
- **Test subset:** `make test`, `make test-integration`, `make test-e2e`
- **Coverage:** `make coverage-gate` — `internal/ash` now enforced at 90%; every other floor still met
- **Regression guard:** every phase 0 to 6 gate still green; the acceptance scenario `SYS-REPL-001` still passes unchanged; the connection-budget change of 7.1 has not broken `INT-CONN-001`
- **README:** complete, reviewed end to end by `agent-1:opus`, free of implementation detail

## Phase done criterion

ASH samples every second, aggregates into 10-second windows with provable count
conservation, stores into `metrics_ash`, and answers "where is the database
spending its time" through the API with an explicit statistical-significance
warning when the range is too thin. `SYS-LOAD-003` proves ASH and
`pg_stat_statements` agree on which query was responsible. `SYS-LOAD-002` proves
the sampler keeps sampling through a lock storm. `SYS-ASH-002` proves a disabled
feature is distinguishable from an idle database. The README is complete and
reviewed. `STATE.md` §11 shows phase 7 `DONE`, every phase `DONE`, and every
planned test row resolved.
