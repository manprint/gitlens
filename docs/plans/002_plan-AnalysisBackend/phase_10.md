# Phase 9 — L3 end-to-end scenarios

> **Intent:** prove that everything the previous nine phases built actually works
> against real PostgreSQL containers, driven the way an operator would drive it,
> with no mocks anywhere in the path.
> **Shippable alone?** it ships no product code at all — it ships the evidence
> that the product code works.
> **Preconditions:** phases 0–8 `DONE`. A scenario for a phase that is not done
> cannot pass, and skipping it to "come back later" is how a plan ends with
> untested features.

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

## How L3 works in this repo — read before writing a scenario

Plan 001 built the harness; this phase only adds scenarios to it. Do not invent a
second harness.

- Scenarios live in `test/e2e/scenarios/<name>.yml`, one compose stack each.
- The runner is `test/e2e/run.go`, built with `//go:build e2e` and driven by
  `make test-e2e`. It brings the stack up, waits for readiness, runs the Go
  assertions for that scenario, tears down, and fails on any container exiting
  non-zero.
- Assertions live beside the scenario as `test/e2e/<name>_test.go`, also
  `//go:build e2e`.
- Every scenario id is `SYS-<AREA>-<NNN>` and every id must appear in the
  `STATE.md` §11 test table. An id in the code but not the table, or the reverse,
  is a defect.

**The five rules that keep L3 from becoming flaky**, learned in plan 001 and
non-negotiable here:

1. **Never `sleep` to wait for a condition.** Poll the condition with a deadline.
   The helper is `e2e.Eventually(t, timeout, interval, func() bool)`.
2. **Never assert on a timestamp being "recent"** unless the scenario controls
   the clock. Assert on ordering and on presence.
3. **Every scenario cleans up what it created**, including any session it left
   open, in a `t.Cleanup`. A leaked `idle in transaction` session breaks the
   *next* scenario, and that failure looks like it belongs to the wrong test.
4. **Assert on the API, not on the database**, wherever the API exposes the
   fact. The API is the contract; the schema is an implementation detail. Query
   the store directly only for things no endpoint exposes.
5. **One scenario proves one thing.** When a scenario needs three unrelated
   assertions, it is three scenarios.

---

## Sub-phases

### 9.1 Cascading replication topology

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — testing
- **Files:** `test/e2e/scenarios/topo-cascading.yml` (new),
  `test/e2e/topo_cascading_test.go` (new)
- **Change:** a three-node stack — primary → standby A → standby B, where B
  streams from A, not from the primary. This is the topology plan 001 never
  exercised, and the one where an edge built from the wrong side of
  `pg_stat_replication` points to the wrong parent.

  `SYS-REPL-006` asserts, through `GET /api/v1/topology`:
  - exactly two edges exist,
  - the edge for B names A as its upstream and **not** the primary,
  - all three nodes share one `cluster_id`,
  - each node's role is `primary`, `standby`, `standby` respectively.

  Then stop standby A and assert that within 60 s the topology marks B's edge
  stale rather than silently reparenting it to the primary. A cascading standby
  whose parent dies does not become a direct standby, and reporting it as one
  would be a lie.

- **Unit tests:** none (this is L3).
- **e2e tests:** `SYS-REPL-006`.
- **Done:** `make test-e2e SCENARIO=topo-cascading` green three consecutive runs
  + closed in `STATE.md`.

### 9.2 Alerting end to end

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — testing
- **Files:** `test/e2e/mockreceiver/main.go` (new),
  `test/e2e/mockreceiver/Dockerfile` (new),
  `test/e2e/scenarios/alerting.yml` (new),
  `test/e2e/alerting_test.go` (new)
- **Change:** a mock receiver — a ~60-line Go HTTP server listening on **9099**
  that accepts any POST, stores the bodies in memory, and exposes
  `GET /_received` returning them as a JSON array and `POST /_reset` clearing
  them. It also supports `GET /_fail_next?n=2`, which makes the next `n` POSTs
  return `500`, because the retry path is untestable without it.

  The stack runs **two server replicas** (ports 8080 and 8081) against one
  TimescaleDB, both with `PGLENS_ALERT_SLACK_WEBHOOK_URL` pointed at the mock
  receiver. Two replicas is not a stress test here — it is the only way to
  observe invariant I-4, which is a claim about replicas and cannot be shown
  with one.

  - **`SYS-ALERT-001`** *(acceptance)* — stop the agent container. Assert the
    built-in `agent_down` rule moves to `firing` within 90 s in
    `GET /api/v1/alerts` on **both** replicas, that **exactly one** POST reached
    the mock receiver, and that its Slack payload names the instance and the
    rule. Restart the agent and assert the alert returns to `resolved` and
    exactly one further POST arrived. Two POSTs total for the whole episode —
    assert the count, because a duplicate is precisely what I-4 forbids and a
    presence check would pass anyway.
  - **`SYS-ALERT-002`** — with `_fail_next=2`, drive the same episode and assert
    the notification is still delivered exactly once after the retries and that
    its `notifications` row ends in `sent`, not `failed`.
  - **`SYS-ALERT-003`** — the dedup invariant under adversity: while an alert is
    firing, kill the replica holding the `pglens:alert-engine` advisory lock.
    Assert the other replica takes over within one evaluation interval and that
    **no** additional notification is sent for the still-firing alert. Then
    restart the killed replica and assert still none. This is I-4 across a
    process boundary and a leadership change, which no integration test reaches.
  - **`SYS-ALERT-004`** — create a silence matching the instance, drive a real
    breach (open enough connections to cross
    `pg_connections_used_ratio > 0.9`), and assert the alert still appears in
    the API as `firing` while **zero** notifications arrive. Silencing suppresses
    delivery, not detection; a silence that hid the alert from the API would hide
    an outage.

- **Unit tests:** none (this is L3).
- **e2e tests:** `SYS-ALERT-001`, `SYS-ALERT-002`, `SYS-ALERT-003`,
  `SYS-ALERT-004`.
- **Done:** all four green three consecutive runs + closed in `STATE.md`.

### 9.3 Locks, blocking and deadlocks

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — testing
- **Files:** `test/e2e/scenarios/contention.yml` (new),
  `test/e2e/contention_test.go` (new)
- **Change:** the user's analysis points 5 and 6, proven rather than asserted.

  - **`SYS-LOCK-001`** — session 1 takes `SELECT ... FOR UPDATE` on a row and
    holds it; session 2 tries the same row and blocks. Assert
    `GET /api/v1/contention?instance_id=...` shows one blocked session whose
    `blocked_by` names session 1's pid, that the wait event type is `Lock`, and
    that the blocking session's query text is present and truncated to at most
    2048 bytes. Release and assert the entry disappears within one scrape
    interval.
  - **`SYS-DEADLOCK-001`** — provoke a genuine deadlock: two sessions lock two
    rows in opposite order. PostgreSQL kills one after `deadlock_timeout`. Assert
    the `pg_deadlocks_total` counter for that database increases by exactly one,
    and that an advisor finding or alert (whichever phase 7 assigned it) appears
    naming the database. Set `deadlock_timeout = '100ms'` in the scenario so the
    test does not wait a second per deadlock.

  > Deadlock detection is a **counter delta**, so the assertion must be
  > "increased by one from the value read before", never "equals one". A
  > container reused across scenarios may already have deadlocks recorded.

- **Unit tests:** none (this is L3).
- **e2e tests:** `SYS-LOCK-001`, `SYS-DEADLOCK-001`.
- **Done:** both green three consecutive runs + closed in `STATE.md`.

### 9.4 Vacuum, bloat, index usage and relation cardinality

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — testing
- **Files:** `test/e2e/scenarios/maintenance.yml` (new),
  `test/e2e/maintenance_test.go` (new)
- **Change:** the user's analysis points 2, 3 and 4.

  The scenario seeds a table of ~200k rows with an index, then shapes the
  workload per assertion.

  - **`SYS-VAC-001`** — set `autovacuum = off` on the table, delete 60 % of the
    rows, and assert `pg_table_dead_tuple_ratio` for it crosses 0.2 and that
    `GET /api/v1/relations?instance_id=...&order=dead_ratio` ranks it first.
    Then `VACUUM` it manually and assert the ratio falls and
    `pg_table_last_vacuum_age_seconds` resets to a small value.
  - **`SYS-BLOAT-001`** — after the same delete without vacuum, assert the
    estimated bloat for the table is reported as above 20 %, and that
    `GET /api/v1/relations` returns it with `method = 'estimate'`. Where the
    container has `pgstattuple`, additionally run the phase 8 command and assert
    an exact row appears with `method = 'pgstattuple'` **beside** the estimate,
    both retrievable.
  - **`SYS-IDX-001`** — create a second index that no query uses, run a workload
    for a while, and assert the advisor produces the unused-index finding naming
    exactly that index and **not** the one the workload uses. Naming the wrong
    index is worse than naming none, so assert the exact set.
  - **`SYS-IDX-002`** — cardinality under pressure (risk register). Create a
    schema with **5 000 tables**, each with one index, using a single
    `DO $$ ... $$` block so setup takes seconds rather than minutes. Assert:
    the relation series count for the instance stays at or below the configured
    budget, `GET /api/v1/relations` reports `truncated: true` with a
    `relations_not_reported` count greater than zero, the agent stays under its
    scrape timeout for the whole run, and the ingest volume per interval does not
    grow after the second scrape. Silent truncation is the failure this catches;
    invariant I-2 is exactly this assertion.
  - **`SYS-IDX-003`** — create a duplicate index (same columns, same order,
    different name) and assert the redundant-index finding names the pair. Then
    drop one and assert the finding resolves on the next advisor run rather than
    lingering.

- **Unit tests:** none (this is L3).
- **e2e tests:** `SYS-VAC-001`, `SYS-BLOAT-001`, `SYS-IDX-001`, `SYS-IDX-002`,
  `SYS-IDX-003`.
- **Done:** all five green three consecutive runs; `SYS-IDX-002` in particular
  green three times, since a cardinality test that passes by luck is worthless
  + closed in `STATE.md`.

### 9.5 Advisor acceptance

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — this is the plan's acceptance criterion;
  review gate
- **Files:** `test/e2e/scenarios/advisor.yml` (new),
  `test/e2e/advisor_test.go` (new)
- **Change:** `SYS-ADV-001` is the scenario that decides whether plan 002 met its
  goal. It is deliberately the harshest test in the repo, and it is harsh in a
  specific way: it asserts an **exact set**, so an advisor that fires everything
  fails just as loudly as one that fires nothing.

  - **`SYS-ADV-001`** *(acceptance)* — one instance seeded with exactly three
    problems and nothing else:
    1. an index that no query in the workload uses,
    2. a table above the dead-tuple ratio threshold with `autovacuum` off,
    3. `work_mem` set high enough, against this `max_connections`, to trip the
       RAM overcommit rule.

    Assert that `GET /api/v1/findings?instance_id=...` returns **exactly** those
    three `rule_id`s and no others, as a set comparison that prints the missing
    and unexpected ids on failure. Every other rule in the packs must stay
    silent on a healthy instance; that is the whole claim.
  - **`SYS-ADV-002`** — a second instance in the same stack started **without**
    `pg_stat_statements` in `shared_preload_libraries`. Assert it produces the
    config findings but **no** query findings, and that each suppressed rule
    appears in the response's `degraded` list with the reason naming the missing
    extension. Silent absence is the failure mode; a named absence is the
    feature, and invariant I-3 is exactly this.
  - **`SYS-ADV-003`** — fix the three `SYS-ADV-001` problems (drop the index,
    vacuum the table, lower `work_mem` and reload) and assert all three findings
    resolve on the next advisor run and none reopens. Resolution has to be as
    precise as detection.
  - **`SYS-ADV-004`** — breadth, on a third instance deliberately misconfigured
    across the remaining rule packs: `shared_buffers = '8MB'` on a 2 GiB
    container, `effective_cache_size = '128MB'`, `fsync = off`,
    `autovacuum_vacuum_cost_delay = '100ms'`, and `archive_mode = on` with a
    failing `archive_command`. Assert the exact set of `rule_id`s again, and
    assert every returned finding carries a non-empty remediation string —
    a finding an operator cannot act on is a finding that should not exist.

- **Unit tests:** none (this is L3).
- **e2e tests:** `SYS-ADV-001`, `SYS-ADV-002`, `SYS-ADV-003`, `SYS-ADV-004`.
- **Done:** all four green three consecutive runs; the exact-set assertions in
  `SYS-ADV-001` and `SYS-ADV-004` match with no allowances and no skipped ids
  + closed in `STATE.md`.

### 9.6 Command channel acceptance

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — security acceptance; review gate
- **Files:** `test/e2e/scenarios/commands.yml` (new),
  `test/e2e/commands_test.go` (new)
- **Change:** the stack runs two instances: one with
  `allow_explain_analyze: true, allow_signal: true` at tier T2, one with both
  false at tier T0.

  - **`SYS-CMD-001`** *(acceptance)* — on the permissive instance, enqueue an
    `explain` for a `queryid` taken from `GET /api/v1/statements`, poll until
    done, and assert a plan is stored and returned by `GET /api/v1/plans` with
    the root node type visible. Enqueue the identical command again and assert
    `total_shapes` is still 1. Then force a different plan and assert
    `total_shapes` becomes 2 with `changed: true` on the newer row.
  - **`SYS-CMD-002`** — on the restrictive instance, enqueue `explain` with
    `analyze: true`, `cancel`, and `pgstattuple`. Assert all three come back
    `rejected`, each with a reason naming the closed gate, that three audit rows
    exist with `outcome = 'rejected'`, and — the important part — that the target
    database shows **no** trace of execution: no new entry in
    `pg_stat_statements` for an `EXPLAIN`, and the session that would have been
    cancelled is still running.
  - **`SYS-CMD-003`** — at-most-once across an agent restart (risk register,
    invariant I-7). Enqueue a slow command — `pgstattuple` on a large table —
    and kill the agent container **after** it has claimed the command but before
    it posts a result. Restart the agent. Assert the command is never executed a
    second time: exactly one audit row exists for it, and the result submitted
    with a stale claim token, if any, is refused. A double execution here would
    mean a double `pg_terminate_backend` in production.
  - **`SYS-CMD-004`** — enqueue a command, keep the agent stopped past
    `PGLENS_COMMAND_TTL`, then restart it and assert the command is `expired`
    and never executed. A command that outlives its deadline must not fire late,
    because the operator who queued it has long since moved on.

- **Unit tests:** none (this is L3).
- **e2e tests:** `SYS-CMD-001`, `SYS-CMD-002`, `SYS-CMD-003`, `SYS-CMD-004`.
- **Done:** all four green three consecutive runs + closed in `STATE.md`.

### 9.7 Backup and archiving

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — testing
- **Files:** `test/e2e/scenarios/archiving.yml` (new),
  `test/e2e/archiving_test.go` (new)
- **Change:** the user's analysis point 11, within what pglens honestly observes.

  `SYS-ARCH-001` — `archive_mode = on` with an `archive_command` that fails
  (`archive_command = '/bin/false'`). Force WAL switches until the archiver
  records failures. Assert:
  - `pg_archiver_failed_count` rises,
  - `pg_archiver_last_failed_age_seconds` is present and small,
  - the alert or finding for a stalled archiver fires,
  - `GET /api/v1/findings` names the instance and states that WAL is
    accumulating.

  Then repair the command to `/bin/true`, force another switch, and assert the
  finding resolves and `pg_archiver_archived_count` advances.

  > **State the limit in the test's own comment**, because a reader will
  > otherwise assume more: pglens observes the *archiver*, not the backup. It
  > cannot tell you a `pg_basebackup` succeeded, that a restore works, or that
  > the archive destination is readable. Phase 10 records this in the declared
  > limits, and this comment is why the test does not try to prove more.

- **Unit tests:** none (this is L3).
- **e2e tests:** `SYS-ARCH-001`.
- **Done:** green three consecutive runs + closed in `STATE.md`.

### 9.8 Update README.md

Mandatory closing sub-phase of every phase. User guide only — no implementation
detail.

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — documentation
- **Files:** `README.md` (repo root), `CONTRIBUTING.md` (repo root, modified)
- **Change:**
  - `README.md` **Testing** section: the five test levels and what each covers in
    one line, how to run each (`make test`, `make test-integration`,
    `make test-e2e`), what each needs (Docker for L2 and L3), and roughly how
    long the full E2E suite takes.
  - `CONTRIBUTING.md`: **How to add an E2E scenario** — the file layout, the
    `SYS-<AREA>-<NNN>` id convention, the requirement to register the id in the
    `STATE.md` §11 test table, and the five anti-flake rules from the top of this
    phase reproduced verbatim. A contributor who never reads this phase file must
    still get them.
- **Unit tests:** none (documentation).
- **e2e tests:** none — the documented commands were executed as written.
- **Done:** a contributor can add a scenario from `CONTRIBUTING.md` alone; gates
  green; closed in `STATE.md` with the §11 docs row for phase 9 set.

---

## Phase gates

- **Fmt:** `make fmt-check`
- **Lint:** `make lint` — including the `e2e` build tag, which the default lint
  run excludes; add the tag to the lint config if it is missing rather than
  leaving these files unlinted.
- **Build:** `make build`; plus `go vet -tags e2e ./test/e2e/...`
- **Test subset:** `make test-e2e-full` — the **whole** suite, not one scenario.
  `make test-e2e` (the smoke subset) must also stay green.
- **Agent-mode gate:** the three acceptance scenarios `SYS-ADV-001`,
  `SYS-ALERT-001` and `SYS-CMD-001` pass with both `AGENT_MODE=container` and
  `AGENT_MODE=binary`, as `overview.md` requires. An agent that only works in
  one packaging is half an agent.
- **Flake gate:** the full suite green **three consecutive times**. A scenario
  that passes two runs in three is a failing scenario, and the third run is what
  catches it.
- **Runtime gate:** the full suite completes within 25 minutes on CI. Over that,
  scenarios get parallelised across stacks in phase 10, not deleted here.
- **README:** testing docs match the commands that actually work.

## Phase done criterion

Every `SYS-*` id listed in this phase exists, runs, and passes three consecutive
full-suite runs, in both agent modes for the three acceptance scenarios.
`SYS-ADV-001` passes with an exact-set assertion and no allowances.
`SYS-CMD-002` proves that a closed gate executes nothing on the target.
`SYS-ALERT-001` delivers exactly two notifications for a full firing-to-resolved
episode with two server replicas running. `STATE.md` §11 lists every scenario id
with status `DONE`, README.md reflects this phase's shipped behavior, and
`STATE.md` §11 shows phase 9 `DONE` with every sub-phase closed.
