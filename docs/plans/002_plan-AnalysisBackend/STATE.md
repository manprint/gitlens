# pglens Analysis Backend — Implementation State

> **READ THIS FILE FIRST at the start of every session, before any other plan
> file. OPEN a unit in §1 before touching code; CLOSE it after the gates pass.**
> **Last updated:** 2026-08-29 (phase 3 closed) by `agent:gpt5.6-luna` | **Session:** 8

## 0. Protocol

This is the only execution-state file — position, progress, ledger, and blockers
all live here. A **unit of work** is one sub-phase, one `task`, one `bug`, one
`verify` audit, or one correction from a verify report.

**Resume (cold start):**
1. Read this file end to end.
2. Read §1 `Status`:
   - `OPEN` — a unit was claimed and may be half-written. Read §6, then finish or
     revert it before starting anything new. If §3 has WIP commits on and `HEAD`
     is a `wip:` commit, that commit is the in-flight work: its diff is what got
     written, §6 says why it stopped. Finish and `amend` into the close commit,
     or revert it.
   - `none` — nothing in flight. Open the unit named in §1 `Next action:`.
3. Run the gate commands in §3 and compare the result with what §1, §7, and §11
   claim. The repo is the truth; correct this file if it drifted.
4. Open only the file §1 points at: the phase file at the named sub-phase, or the
   verify report for a correction. Read `overview.md` only if §2 is insufficient.

**Open a unit — before touching code, mandatory:** set §1 `Type`, `ID`,
`Status: OPEN`, `Intent`, `Next action:`, `Assigned`; set §6 to `claimed —
nothing written yet`; bump the header timestamp. Only then edit anything.

**Close a unit — after its gates are green, mandatory:** append a §4 ledger row;
reset §6 to `none — tree consistent`; update §5, §7, §8, §9, §10 and the §11
board; set §1 to the next unit with `Status: none`; bump the timestamp. When §3
has WIP commits on, commit the closed unit — code, tests, this file, docs, ledger
together — staging only the files in §5 plus the plan files, never `git add -A`,
and record the sha in the §4 row. A unit is not `DONE` until this is written.

**Interrupted mid-unit:** leave §1 `OPEN` and write into §6 exactly what is
half-finished — files written, edits still pending, temporary code to remove.
`OPEN` with an empty §6 is an execution bug. With WIP commits on, also commit
that state as `wip(<id>): <what remains>`.

**Order of execution:** phases run in order 0 → 10, and sub-phases in order
inside a phase. Two exceptions are written into the phase files themselves and
are the only ones permitted: sub-phase 5.1 must run before 5.3, and phase 2 must
be `DONE` before phase 8 starts. Do not reorder anything else to "unblock"
work — a phase that looks blocked is a signal to read §9, not to skip ahead.

## 1. Current unit

- **Type:** sub-phase
- **ID:** `4.1`
- **Status:** `none`
- **Intent:** implement the `table_stats` check and its named integration coverage.
- **Phase:** 3 — Contention: locks, activity, transactions ([phase_04.md](phase_04.md))
- **Next action:** read `phase_05.md` §4.1, open it here, then implement only its planned scope.
- **Assigned:** `agent:gpt5.6-luna`
- **Repo state:** branch `main`, phase 1 committed at `f745eb9`, phase 2 committed at `8a66327`, phase 3 committed at `11d0e61`; phase 4.1 is next.

## 2. Feature context (self-contained recap)

Plan 001 proved the chain agent → wire → TimescaleDB → API stays correct while
the world breaks. This plan turns that correct collector into an **analyzer**,
with no frontend (D1 — the GUI is plan 003, and the HTTP API is the contract it
will be built against).

Three new engines sit beside the existing ingest path, each leader-elected
through a PostgreSQL advisory lock so N server replicas stay safe:

- the **alert engine** (30 s tick) evaluates rules over TimescaleDB and `events`,
  and notifies exactly once per `alert_key + started_at` (invariant I-4);
- the **advisor engine** (15 m tick) takes one bounded snapshot per instance and
  runs code-defined rules that produce persisted, ranked findings;
- the **command dispatcher** executes on-demand work — query plans, session
  cancellation, exact bloat — through an agent long-poll with atomic claim,
  at-most-once semantics (I-7) and an audit row per execution (I-1).

The agent gains **ten checks** registered against the *unchanged* `check.Check`
interface: `locks`, `table_stats`, `index_stats`, `vacuum_progress`,
`bloat_estimate`, `settings`, `wal`, `checkpointer`, `io`, `archiver`, plus an
extended `activity` check. The wire envelope goes to **v2** with an additive
`facts` array, because `wire.Metric.Value` is a `float64` and cannot carry index
definitions, GUC values, lock trees or query plans. Relation-level series get
three typed hypertables and a **shared per-instance cardinality budget**, so a
5 000-table schema truncates by rank and says so (I-2) instead of flooding the
store.

Everything still runs at permission tier T0 (I-6); anything needing more declares
it in `Requires()` and reports a named `degraded` entry rather than failing (I-3).
The only write path to a monitored instance is the command channel.

Design decisions D1–D26, the full interface table, invariants I-1 to I-9 and the
risk register are in [overview.md](overview.md); read it only when this recap is
insufficient.

## 3. Environment and commands

The authoritative gate commands are the Makefile targets. These are identical to
`overview.md` § Verification summary and must not drift from it.

- **Repo root:** `/mnt/fabio/dati/Git/SperimentazioniAI/postgres-analyze`
- **Build:** `make build` · **Fmt:** `make fmt-check` · **Lint:** `make lint`
- **Unit tests:** `make test` (`go test -race -shuffle=on ./...`)
- **Coverage gate:** `make coverage-gate`
- **Integration (L2):** `make test-integration`
- **E2E (L3):** `make test-e2e` (smoke) / `make test-e2e-full`
- **Images:** `make build-images` — required before any L3 run
- **Toolchain:** Go 1.26.1 · Docker 29.7.2 · Docker Compose v5.5.0
- **Setup / caveats:** Docker daemon required from phase 0 (L2 uses
  testcontainers). L3 publishes host ports 8080 and 8081 (second server
  replica), 5432+, 8474 (Toxiproxy) and 9099 (mock receiver) — free them first.
  **L2 tests inside one container run serially by design** (plan 001 D4): never
  call `t.Parallel()` inside a `pgtest.ForEach` body. `make test-e2e` uses
  `-count=1`; a cached E2E pass is meaningless.
- **WIP commits:** `off` — phase-close commits are required by the user; interrupted units remain in the working tree with §1 OPEN and §6 details.

## 4. Work ledger (append-only, one row per closed unit)

| # | Type | ID | Agent | What changed | Files | Gates | Commit |
|---|------|----|-------|--------------|-------|-------|--------|
| 1 | sub-phase | 0.1 | agent:gpt5.6-luna | Added alert migration and migration parser test | `internal/store/migrations/0006_alerts.sql`, `internal/store/migrate_test.go`, `phase_01.md`, `STATE.md` | fmt-check, lint, build, test green | phase-0 close |
| 2 | sub-phase | 0.2 | agent:gpt5.6-luna | Added core alert types and validation tests | `internal/alert/doc.go`, `internal/alert/types.go`, `internal/alert/types_test.go`, `STATE.md` | alert unit test green | phase-0 close |
| 3 | sub-phase | 0.3 | agent:gpt5.6-luna | Added comparator, evaluator hysteresis, transitions, and tests | `internal/alert/eval.go`, `internal/alert/eval_test.go`, `internal/alert/key.go`, `internal/alert/key_test.go`, `STATE.md` | `go test -cover ./internal/alert/...` PASS at 94.7% | phase-0 close |
| 4 | sub-phase | 0.4 | agent:gpt5.6-luna | Added silence validation, active-window and matcher logic with tests | `internal/alert/silence.go`, `internal/alert/silence_test.go`, `STATE.md` | `go test -cover ./internal/alert/...` PASS at 91.4% | phase-0 close |
| 5 | sub-phase | 0.5 | agent:gpt5.6-luna | Completed stable alert key and dedup identity tests | `internal/alert/key.go`, `internal/alert/key_test.go`, `STATE.md` | `go test ./internal/alert/...` PASS | phase-0 close |
| 6 | sub-phase | 0.6 | agent:gpt5.6-luna | Added deterministic Tier 0 catalogue and tests | `internal/alert/builtin.go`, `internal/alert/builtin_test.go`, `internal/alert/eval_test.go`, `STATE.md` | fmt-check, lint, build, test, coverage-gate green; alert coverage 91.5% | phase-0 close |
| 7 | sub-phase | 0.7 | agent:gpt5.6-luna | Verified README and removed an obsolete alerting capability claim | `README.md`, `STATE.md` | README verification and all phase-0 gates green | `ab5394f` |
| 8 | sub-phase | 1.1 | agent:gpt5.6-luna | Added alert evaluation lifecycle, advisory-lock leadership, ticker shutdown, and transition tests | `internal/alert/engine.go`, `internal/alert/engine_test.go`, `STATE.md` | fmt-check, lint, build, test, race alert tests green | phase-1 close |
| 9 | sub-phase | 1.2 | agent:gpt5.6-luna | Added metric/event SQL sources, typed routing, lookback handling, and L2 coverage | `internal/alert/source_sql.go`, `internal/alert/source_test.go`, `internal/alert/source_sql_integration_test.go`, `STATE.md` | fmt-check, lint, build, test, race alert, targeted L2 green | phase-1 close |
| 10 | sub-phase | 1.3 | agent:gpt5.6-luna | Added Slack/webhook channels, retry policy, JSON rendering, URL redaction, and tests | `internal/alert/notify/notify.go`, `internal/alert/notify/http.go`, `internal/alert/notify/slack.go`, `internal/alert/notify/webhook.go`, `internal/alert/notify/notify_test.go`, `STATE.md` | fmt-check, lint, build, unit and race notifier tests green | phase-1 close |
| 11 | sub-phase | 1.4 | agent:gpt5.6-luna | Added PostgreSQL alert store, rule/silence loading, alert upsert, once-only claim, delivery marking, active query, and mock-backed tests | `internal/alert/store.go`, `internal/alert/store_test.go`, `STATE.md` | fmt-check, lint, build, make test, coverage-gate, full controlled L2 green | phase-1 close |
| 12 | sub-phase | 1.5 | agent:gpt5.6-luna | Added alert/rule/silence HTTP routes, validation, Tier 0 conflict handling, soft-delete, and real-database API tests | `internal/server/api_alerts.go`, `internal/server/api_alerts_test.go`, `internal/server/api_alerts_integration_test.go`, `internal/server/http.go`, `internal/alert/engine.go`, `internal/alert/store.go`, `STATE.md` | unit and targeted integration tests green; INT-ALERTAPI-001/002 green | phase-1 close |
| 13 | sub-phase | 1.6 | agent:gpt5.6-luna | Wired alert configuration, sources, channels, notifier, engine lifecycle, and alert API into server startup and routing | `internal/server/config.go`, `internal/server/config_test.go`, `cmd/pglens-server/main.go`, `internal/server/http.go`, `STATE.md` | fmt-check, lint, build, make test, race-alert, L2, coverage-gate green | phase-1 close |
| 14 | sub-phase | 1.7 | agent:gpt5.6-luna | Added INT-ALERT-008..012 integration coverage for threshold transitions, deduplicated delivery, silence, resolve, and leadership behavior | `internal/alert/engine_integration_test.go`, `STATE.md` | full L2 green; INT-ALERT-008..012 green | phase-1 close |
| 15 | sub-phase | 1.8 | agent:gpt5.6-luna | Documented alert configuration, API examples, Tier 0 rules, and operational limits | `README.md`, `STATE.md` | README verification and complete phase gate matrix green | phase-1 close |
| 16 | sub-phase | 2.1 | agent:gpt5.6-luna | Added protocol v2 Fact transport, validation, backward-compatible facts omission, and v1/v2 ingest acceptance range | `internal/wire/envelope.go`, `internal/wire/envelope_test.go`, `internal/server/ingest.go`, `internal/server/ingest_test.go`, `STATE.md` | focused wire/server/agent tests and fmt-check green | phase-2 close |
| 17 | sub-phase | 2.2 | agent:gpt5.6-luna | Added migration 0007 for typed relation hypertables and relational facts | `internal/store/migrations/0007_facts.sql`, `internal/store/migrate_test.go`, `internal/server/facts_integration_test.go`, `STATE.md` | INT-FACT-001 and full L2 green | phase-2 close |
| 18 | sub-phase | 2.3 | agent:gpt5.6-luna | Added typed row models, bulk writers, deduplication, and fact history upsert | `internal/store/write.go`, `internal/store/write_test.go`, `internal/server/facts_integration_test.go`, `STATE.md` | INT-FACT-002/003 and full L2 green | phase-2 close |
| 19 | sub-phase | 2.4 | agent:gpt5.6-luna | Routed relation metrics and validated/routed object facts in the ingest pipeline | `internal/server/pipeline.go`, `internal/server/pipeline_test.go`, `internal/server/facts_integration_test.go`, `STATE.md` | INT-FACT-004/005/006 and full L2 green | phase-2 close |
| 20 | sub-phase | 2.5 | agent:gpt5.6-luna | Preserved v1 goldens and added independent v2 fact fixtures | `internal/wire/envelope_integration_test.go`, `internal/wire/envelope_golden_v2_test.go`, `internal/wire/testdata/payload_v2_pg15.json`, `internal/wire/testdata/payload_v2_pg18.json`, `STATE.md` | INT-GOLDEN-001/002 green | phase-2 close |
| 21 | sub-phase | 2.6 | agent:gpt5.6-luna | Added check-local Fact result and agent conversion to validated wire facts with protocol v2 | `internal/check/check.go`, `internal/agent/scheduler.go`, `cmd/pglens-agent/run.go`, `internal/wire/envelope.go`, `STATE.md` | check/agent/wire tests and full L2 green | phase-2 close |
| 22 | sub-phase | 2.7 | agent:gpt5.6-luna | Documented protocol compatibility and upgrade ordering | `README.md`, `phase_03.md`, `STATE.md` | README review and full gate matrix green | phase-2 close |
| 23 | sub-phase | 3.1 | agent:gpt5.6-luna | Added the T0 locks check with bounded wait-event metrics, lock-tree fact, query truncation, and INT-LOCK-001/002 coverage | `internal/check/locks.go`, `internal/check/locks_test.go`, `internal/check/locks_integration_test.go`, `STATE.md` | locks unit tests and INT-LOCK-001/002 green on PG15/18 | phase-3 close |
| 24 | sub-phase | 3.2 | agent:gpt5.6-luna | Added lock snapshot migration, newest-wins persistence, stale pruning, pipeline routing, and structural INT-LOCK-004 assertion | `internal/store/migrations/0008_locks.sql`, `internal/store/write.go`, `internal/store/write_test.go`, `internal/server/pipeline.go`, `internal/server/pipeline_integration_test.go`, `STATE.md` | focused INT-LOCK-004 and activity regression tests green | phase-3 close |
| 25 | sub-phase | 3.3 | agent:gpt5.6-luna | Extended activity with connection ratios, bounded database/application breakdowns, state age, prepared transactions, and frozen-xid age | `internal/check/activity.go`, `internal/check/activity_test.go`, `internal/check/activity_integration_test.go`, `STATE.md` | activity/database unit tests, INTACT-001/002/004, and INTCHECK-015 green | phase-3 close |
| 26 | sub-phase | 3.4 | agent:gpt5.6-luna | Added lifetime transaction rollback and buffer-hit ratio gauges with zero-denominator omission | `internal/check/database_stats.go`, `internal/check/database_stats_test.go`, `STATE.md` | database-stats unit matrix green | phase-3 close |
| 27 | sub-phase | 3.5 | agent:gpt5.6-luna | Added contention API routes, staleness contract, lock/activity round-trip integration tests, and database companion response | `internal/server/api_locks.go`, `internal/server/api_locks_test.go`, `internal/server/api_locks_integration_test.go`, `internal/server/api.go`, `STATE.md` | API unit tests, INT-LOCK-005, and INTACT-003 green | phase-3 close |
| 28 | sub-phase | 3.6 | agent:gpt5.6-luna | Added activity by-application configuration and enabled the locks check in the agent example/configuration path | `internal/agent/config.go`, `cmd/pglens-agent/run.go`, `deploy/agent.example.yaml`, `internal/agent/config_test.go`, `STATE.md` | config and locks registration tests green | phase-3 close |
| 29 | sub-phase | 3.7 | agent:gpt5.6-luna | Completed the PostgreSQL version sweep with structural blocker assertions and stable no-contention metric-shape checks | `internal/check/locks_integration_test.go`, `internal/check/activity_integration_test.go`, `STATE.md` | INT-LOCK-001/003/006 and INTACT-004 green on PG15/18 | phase-3 close |
| 30 | sub-phase | 3.8 | agent:gpt5.6-luna | Finalized contention documentation and corrected the v1 golden harness to ignore only additive phase-3 ratio metrics while preserving frozen fixtures; synchronized blocked-session cleanup before connection release | `README.md`, `internal/wire/envelope_integration_test.go`, `internal/check/locks_integration_test.go`, `STATE.md`, `phase_04.md` | fmt-check, lint, build, unit/race, coverage 75.1%, L2 green; INT-GOLDEN-001 and lock race sweep green | `11d0e61` |

## 5. Files touched

*Files touched by closed units.* One row per file once work starts, with
the units that touched it, so a later audit can attribute every diff.

| File | Units | Notes |
|------|-------|-------|
| `internal/store/migrations/0006_alerts.sql` | 0.1 | alert schema and seed rules |
| `internal/store/migrate_test.go` | 0.1 | migration DDL presence test |
| `phase_01.md` | 0.1 | reconciled missing test-file anchor and assignment |
| `STATE.md` | 0.1 | execution state and ledger |
| `internal/alert/doc.go` | 0.2 | alert package documentation |
| `internal/alert/types.go` | 0.2 | alert domain types and validation |
| `internal/alert/types_test.go` | 0.2 | validation tests |
| `internal/alert/eval.go` | 0.3 | pure threshold evaluator |
| `internal/alert/eval_test.go` | 0.3 | evaluator tests |
| `internal/alert/key.go` | 0.3 | prerequisite stable alert identity |
| `internal/alert/key_test.go` | 0.3 | key and dedup tests |
| `internal/alert/silence.go` | 0.4 | silence matching |
| `internal/alert/silence_test.go` | 0.4 | silence tests |
| `internal/alert/builtin.go` | 0.6 | Tier 0 rule catalogue |
| `internal/alert/builtin_test.go` | 0.6 | catalogue tests |
| `README.md` | 0.7 | removed obsolete unshipped alert API claim |
| `internal/alert/engine.go` | 1.1 | alert loop, leadership, persistence and notification orchestration |
| `internal/alert/engine_test.go` | 1.1 | engine lifecycle and transition tests |
| `internal/alert/source_sql.go` | 1.2 | metric and event SQL sources |
| `internal/alert/source_test.go` | 1.2 | source routing and lookback tests |
| `internal/alert/source_sql_integration_test.go` | 1.2 | INT-ALERT-001/002 |
| `internal/alert/notify/notify.go` | 1.3 | notifier interfaces |
| `internal/alert/notify/http.go` | 1.3 | shared retry and redaction |
| `internal/alert/notify/slack.go` | 1.3 | Slack channel |
| `internal/alert/notify/webhook.go` | 1.3 | generic webhook channel |
| `internal/alert/notify/notify_test.go` | 1.3 | notifier tests |
| `internal/alert/store.go` | 1.4 | PostgreSQL persistence implementation |
| `internal/alert/store_test.go` | 1.4 | persistence unit tests |
| `internal/server/api_alerts.go` | 1.5 | alert/rule/silence HTTP API |
| `internal/server/api_alerts_test.go` | 1.5 | API validation and rendering tests |
| `internal/server/api_alerts_integration_test.go` | 1.5 | INT-ALERTAPI-001/002 |
| `internal/alert/store_integration_test.go` | 1.4 | INT-ALERT-003..007 |
| `internal/store/migrations/0007_facts.sql` | 2.2 | typed relation hypertables and object facts |
| `internal/server/facts_integration_test.go` | 2.2–2.4 | INT-FACT-001..006 |
| `internal/store/migrate_test.go` | 2.2 | migration structure assertion |
| `internal/store/write.go` | 2.3 | typed row models and writers |
| `internal/store/write_test.go` | 2.3 | typed row argument mapping |
| `internal/server/pipeline.go` | 2.4 | relation metric and fact routing |
| `internal/server/pipeline_test.go` | 2.4 | typed routing and validation tests |
| `internal/wire/envelope_integration_test.go` | 2.5 | v1 golden regression harness |
| `internal/wire/envelope_golden_v2_test.go` | 2.5 | INT-GOLDEN-002 fixture validation |
| `internal/wire/testdata/payload_v2_pg15.json` | 2.5 | protocol-v2 PG15 golden |
| `internal/wire/testdata/payload_v2_pg18.json` | 2.5 | protocol-v2 PG18 golden |
| `internal/check/check.go` | 2.6 | check-local Fact result type |
| `internal/agent/scheduler.go` | 2.6 | Fact result propagation |
| `cmd/pglens-agent/run.go` | 2.6 | Fact conversion and validation |
| `internal/wire/envelope.go` | 2.1, 2.6 | protocol constants and Fact transport |
| `internal/check/locks.go` | 3.1 | lock contention metrics and lock-tree fact |
| `internal/check/locks_test.go` | 3.1 | locks unit coverage |
| `internal/check/locks_integration_test.go` | 3.1 | INT-LOCK-001/002 |
| `internal/check/locks_integration_test.go` | 3.7–3.8 | version sweep and deterministic blocked-session cleanup |
| `internal/check/activity_integration_test.go` | 3.3, 3.7 | activity integration and version sweep |
| `internal/check/locks.go` | 3.1 | lock contention metrics and lock-tree fact |
| `internal/check/locks_test.go` | 3.1 | locks unit coverage |
| `internal/check/activity.go` | 3.3 | extended activity metrics |
| `internal/check/activity_test.go` | 3.3 | activity unit coverage |
| `internal/check/database_stats.go` | 3.4 | ratio gauges |
| `internal/check/database_stats_test.go` | 3.4 | ratio tests |
| `internal/store/migrations/0008_locks.sql` | 3.2 | lock snapshot schema |
| `internal/store/write.go` | 2.3, 3.2 | typed writers and lock snapshots |
| `internal/store/write_test.go` | 2.3, 3.2 | writer tests |
| `internal/server/pipeline.go` | 2.4, 3.2 | fact and lock snapshot routing |
| `internal/server/api.go` | 3.5 | contention API registration |
| `internal/server/api_locks.go` | 3.5 | lock/activity API |
| `internal/server/api_locks_test.go` | 3.5 | API contract tests |
| `internal/server/api_locks_integration_test.go` | 3.5 | INT-LOCK-005 and INTACT-003 |
| `internal/server/inventory_integration_test.go` | 2.2–2.4 | shared fixture cleanup |
| `internal/server/pipeline_integration_test.go` | 3.2 | INT-LOCK-004 |
| `internal/agent/config.go` | 3.6 | activity/locks configuration |
| `internal/agent/config_test.go` | 3.6 | configuration tests |
| `cmd/pglens-agent/run.go` | 2.6, 3.6 | check registration/config wiring |
| `deploy/agent.example.yaml` | 3.6 | shipped example configuration |
| `internal/wire/envelope_integration_test.go` | 2.5, 3.8 | v1 golden compatibility harness |
| `README.md` | 0.7, 2.7, 3.8 | user-facing feature and compatibility documentation |
| `phase_04.md` | 3.8 | phase execution record |
| `internal/wire/envelope_test.go` | 2.1 | Fact and v1/v2 round-trip tests |
| `internal/server/ingest.go` | 2.1 | protocol range acceptance |
| `internal/server/ingest_test.go` | 2.1 | future protocol rejection test |
| `internal/server/inventory_integration_test.go` | 2.2–2.4 | shared fixture cleanup for typed tables |
| `README.md` | 2.7 | compatibility and upgrade ordering |
| `phase_03.md` | 2.7 | execution record |

## 6. In-flight work

`none — tree consistent; phase 3.8 README, golden-harness compatibility correction, and deterministic lock-probe cleanup are complete. Full phase gates are green and no command is active.`

## 7. Verification state

| Gate | Command | Result | When |
|------|---------|--------|------|
| fmt-check | `make fmt-check` | PASS | 2026-08-29 |
| lint | `make lint` | PASS | 2026-08-29 |
| build | `make build` | PASS | 2026-08-29 |
| unit | `make test` | PASS | 2026-08-29 |
| race-alert | `go test -race ./internal/alert/...` | PASS | 2026-08-29 |
| integration-alert | `timeout 120s go test -tags=integration -race -shuffle=on -timeout=110s ./internal/alert/...` | PASS | 2026-08-29 |
| coverage | `make coverage-gate` | FAIL — global 74.9%, threshold 75% | 2026-08-29 |
| coverage-final | `make coverage-gate` | PASS — global 75.1%, threshold 75% | 2026-08-29 |
| integration-final | `timeout 120s make test-integration` | PASS | 2026-08-29 |
| phase3-focused-lock-activity | `timeout --signal=TERM 300s go test -tags=integration ./internal/server ./internal/check -run 'TestINTLOCK004\|TestINTACT00[124]' -count=1` | FAIL — INT-LOCK-004 timestamp assertion was corrected, then JSON assertion failed on whitespace (`"pid":2` vs `"pid": 2`); INTACT-001/002/004 PASS | 2026-08-29 |
| integration-alert-store-api | `go test -tags=integration -run 'TestINTALERT003_Through007_AlertStorePersistence\|TestAlertAPI_INT_ALERTAPI_' ./internal/alert ./internal/server` | PASS | 2026-08-29 |
| phase1-post-modification-coverage | `make coverage-gate` | FAIL — global 72.7% (3343/4598), threshold 75%; L2 not started | 2026-08-29 |
| phase1-focused-tests-coverage | `make coverage-gate` | FAIL — global 74.5% (3426/4598), threshold 75%; focused package tests passed; no phase commit | 2026-08-29 |
| phase1-final-matrix | `make fmt-check && make lint && make build && make test && go test -race ./internal/alert/... && timeout --signal=TERM 180s make test-integration && make coverage-gate` | PASS — all gates green; global coverage 75.0% (3449/4598) | 2026-08-29 |
| phase2-focused-coverage | `make coverage-gate` | PASS — global 75.1% (3574/4762) | 2026-08-29 |
| phase2-golden-regression | `go test ./internal/wire && timeout --signal=TERM 120s go test -tags=integration -run TestINTGOLDEN001_NormalizedEnvelopeStructure ./internal/wire -count=1` | PASS — v1 fixtures unchanged and v2 golden fixture test green | 2026-08-29 |
| phase2-gate-matrix | `make fmt-check && make lint && make build && make test && go test -race ./internal/alert/... && timeout --signal=TERM 180s make test-integration` | PASS — all packages and L2 integration green; `make test-integration` exited `0` | 2026-08-29 |
| phase2-fact-acceptance | `timeout --signal=TERM 180s go test -tags=integration -run 'TestINTFACT00[1-6]' ./internal/server -count=1` | PASS — INT-FACT-001..006 green | 2026-08-29 |
| phase3.1-unit | `go test ./internal/check -run 'TestLocks_' -count=1` | PASS | 2026-08-29 |
| phase3.1-integration | `timeout --signal=TERM 240s go test -tags=integration ./internal/check -run 'TestINTLOCK00[12]' -count=1` | PASS — INT-LOCK-001/002 green on PG15 and PG18 | 2026-08-29 |
| phase3.2-focused-lock-activity | `timeout --signal=TERM 300s go test -tags=integration ./internal/server ./internal/check -run 'TestINTLOCK004\|TestINTACT00[124]' -count=1` | PASS — INT-LOCK-004 and INTACT-001/002/004 green | 2026-08-29 |
| phase3.3-activity | `go test ./internal/check -run 'TestActivity_|TestDatabaseStats_' -count=1 && timeout --signal=TERM 300s go test -tags=integration ./internal/check -run 'TestINTACT00[124]|TestINTCHECK015' -count=1` | PASS — activity/database unit tests, INTACT-001/002/004, and INTCHECK-015 green | 2026-08-29 |
| phase3.5-api | `go test ./internal/server -run 'TestLocksAPI_|TestActivityAPI_|TestDatabasesAPI_' -count=1 && timeout --signal=TERM 300s go test -tags=integration ./internal/server -run 'TestINTLOCK005|TestINTACT003' -count=1` | PASS — API contract tests, INT-LOCK-005, and INTACT-003 green | 2026-08-29 |
| phase3.6-config | `go test ./internal/agent -run 'TestConfig_|Test.*Check|Test.*Command' -count=1 && go test ./internal/check -run 'TestLocks_' -count=1` | PASS — config defaults/example and locks registration tests green | 2026-08-29 |
| phase3.7-sweep-attempt | `timeout --signal=TERM 360s go test -tags=integration ./internal/check -run 'TestINTLOCK00[136]' -count=1` | INTERRUPTED by user after startup; no test result was produced and no command remains active | 2026-08-29 |
| phase3.7-sweep | `timeout --signal=TERM 360s go test -tags=integration ./internal/check -run 'TestINTLOCK00[136]|TestINTACT004' -count=1` | PASS — INT-LOCK-001/003/006 and INTACT-004 green on PG15/18 | 2026-08-29 |
| phase3-coverage | `timeout --signal=TERM 300s make coverage-gate` | PASS — global 75.1% (3720/4956), threshold 75% | 2026-08-29 |
| phase3-l2 | `timeout --signal=TERM 900s make test-integration` | FAIL — INT-GOLDEN-001 v1 normalized envelope mismatch after additive phase-3 ratio metrics; run also emitted race warnings while blocked-session cleanup raced with the probe goroutine | 2026-08-29 |
| phase3-focused-rerun | `timeout --signal=TERM 300s go test -tags=integration -race ./internal/check -run 'TestINTLOCK00[13]' -count=1 && timeout --signal=TERM 300s go test -tags=integration ./internal/wire -run 'TestINTGOLDEN001_NormalizedEnvelopeStructure' -count=1` | INTERRUPTED by user after 0.4s; no result produced and no command remains active | 2026-08-29 |
| phase3-focused-final | `timeout --signal=TERM 300s go test -tags=integration -race ./internal/check -run 'TestINTLOCK00[13]' -count=1 && timeout --signal=TERM 300s go test -tags=integration ./internal/wire -run 'TestINTGOLDEN001_NormalizedEnvelopeStructure' -count=1` | PASS — deterministic cleanup has no race report and INT-GOLDEN-001 passes with v1 fixtures unchanged | 2026-08-29 |
| phase3-final-matrix | `make fmt-check && make lint && make build && timeout 600s make test && make coverage-gate && timeout 1200s make test-integration` | PASS — all gates green; coverage 75.1% (3720/4956); L2 all integration packages PASS | 2026-08-29 |

## 8. Runtime deviations from the plan

| # | Unit | Plan said | Actual | Reason |
|---|------|-----------|--------|--------|
| D-001 | 0.1 | Extend existing `internal/store/migrate_test.go` | Create the file because it does not exist | Repository reality; phase file reconciled before implementation |
| D-002 | 0.1 | Use `agent-2:sonnet` | Use `agent:gpt5.6-luna` throughout plan assignment tags | Explicit user assignment for single-agent execution |
| D-003 | 0.1 | WIP setting was enabled during initial claim | WIP setting reset to `off`; one local commit will close the complete phase | User explicitly requires a coherent commit after each complete phase |
| D-004 | 0.3 | Key derivation is specified in later sub-phase 0.5 | Added `key.go` and its tests as a compile-time prerequisite for evaluator state keys; remaining 0.5 validation will still be completed | Go package symbols must compile as a unit; no behavior beyond the planned key API was added |
| D-005 | 0.7 | Verify README unchanged because phase 0 has no user-visible behavior | Removed one existing unshipped alerting claim, because it contradicted phase 0's shipped behavior and the README accuracy gate | Corrected documentation without adding product behavior |
| D-006 | 1.2 → 1.3 | Complete and close each unit before starting the next | Notifier files were created before 1.2's final documentation close; recorded as in-flight 1.3 work and kept isolated from the 1.2 ledger row | User interruption occurred during the L2 gate; no 1.2 source scope was changed |
| D-007 | 1.3 → 1.4 | Complete and close each unit before starting the next | `store.go` was created before the 1.3 documentation close; it is recorded as the open 1.4 in-flight implementation and will be verified before closure | Continued execution after the user-requested resume exposed the procedural overlap; no scope was silently dropped |
| D-008 | 1.4 | Coverage gate remains green after each phase | Initial store coverage correction left global coverage at 74.9%; an additional error-path test is pending verification | New planned persistence paths lowered global coverage by 0.1 percentage point |
| D-011 | 1.6–1.8 | Close the phase only after all required gates are green | Added focused in-scope tests for alert API, source constructors, engine interval normalization, and channel names; final coverage is 75.0% and all phase gates pass | Current alert/server implementation added uncovered paths; threshold and package scope remain unchanged |
| D-012 | 1.6–1.8 | Test constructors with nil dependencies | Initial constructor test called through a typed-nil pool and panicked; removed the invalid database call while retaining constructor/kind coverage | Go interface typed-nil behavior; corrected and verified |
| D-009 | 1.4 | Add dedicated `store_integration_test.go` for INT-ALERT-003..007 | Added dedicated PostgreSQL integration coverage with a test-local alert schema; all five IDs are green | Existing shared fixture did not expose the alert migration schema, so the test provisions only the planned alert tables |
| D-010 | 1.6–1.8 | Keep the global coverage gate green | New API/config/notifier/engine integration code lowered measured global coverage to 72.7%; phase close is held for targeted in-scope tests | Coverage threshold and scope remain unchanged; no production code is removed to mask the gap |
| D-013 | 2.1–2.5 | Keep legacy v1 goldens unchanged while adding protocol-v2 goldens | The current protocol changed the existing integration harness to v2 and exposed a fixture mismatch; the harness now pins INT-GOLDEN-001 to `ProtocolVersionMin`, while separate v2 fixtures and INT-GOLDEN-002 validate facts | Backward compatibility requires two explicit golden contracts; no v1 fixture was regenerated |
| D-014 | 2.2–2.7 | Close each sub-phase only after its named integration IDs exist and pass | Technical gates pass, but INT-FACT-001..006 are not yet implemented as named integration tests, so phase closure is held | Done criterion and STATE test board take precedence over aggregate L2 success |
| D-015 | 2.2–2.7 | Complete the held phase only after named fact acceptance tests exist | Added `facts_integration_test.go` with INT-FACT-001..006; focused and full L2 runs pass | The earlier implementation had aggregate L2 coverage but lacked the plan's referenceable IDs |
| D-016 | 3.1 | Lock age extraction may be scanned directly as float64 | Wrapped nullable `xact_start` and `state_change` extracts with `COALESCE(..., 0)` | Real PG15/18 idle sessions return NULL timestamps; INT-LOCK-002 initially failed until the query was corrected |
| D-017 | 3.8 | Keep INT-GOLDEN-001 as the frozen v1 compatibility check | The harness filters only the two additive phase-3 ratio metrics for v1; fixture files remain byte-for-byte unchanged and v2 golden coverage remains separate | The planned database-stats extension must not rewrite the legacy v1 contract |
| D-018 | 3.8 | Release blocked-session test connections after the probe finishes | Cleanup rolls back the holder, waits on the probe completion channel, then releases blocked and holder connections | Prevents race warnings and pool teardown overlap while preserving the real lock assertion |

## 9. Blockers and open questions

Carried from `overview.md` § Open questions. Neither blocks the start of phase 0.

| # | Question | Assumed default | Resolve at | Status |
|---|----------|-----------------|-----------|--------|
| Q-A | `UNVERIFIED` (D23) — does PG 18 keep `wal_write`, `wal_sync`, `wal_write_time`, `wal_sync_time` on `pg_stat_wal`, or are they only in `pg_stat_io`? | the `wal` check collects LSN-derived rates unconditionally and treats the I/O columns as version-gated optionals | **sub-phase 5.1**, which exists solely to verify this against a real PG 18 container *before* 5.3 writes any code | `OPEN` |
| Q-B | Does the fleet's real workload need per-user or per-application connection breakdown, or is per-state enough? | per-state and per-database only, plus a bounded `top_n` of 10 application names | sub-phase 3.3 — implement the default; widen only if the user asks | `OPEN` |

No blockers. Phase 0 can start immediately.

## 10. Do-not-repeat

Approaches already ruled out, so nobody spends a session rediscovering them.

| # | Do not | Why | Recorded |
|---|--------|-----|----------|
| 1 | Do not add relation-level series to the generic `metrics` table | the relation label would land in the `compress_segmentby` key and destroy the compression ratio; D4 gives them three typed hypertables instead | plan authoring (D4) |
| 2 | Do not widen `wire.Metric.Value` or add a second numeric field to carry non-numeric observations | `float64` cannot hold an index definition, a GUC value, a lock tree or a plan; D3 adds one additive `facts` array and nothing else | plan authoring (D3) |
| 3 | Do not let the server connect to the agent for commands | agents live behind NAT; D20 makes the agent long-poll and the server never dial outward | plan authoring (D20) |
| 4 | Do not `CREATE EXTENSION` from the agent, for any reason | plan 001's constraint is that no monitored instance needs one; the moment the agent creates one, that constraint is gone (D6) | plan authoring (D6) |
| 5 | Do not evaluate the command gates server-side | the agent holds the authoritative tier and per-target flags; duplicating them on the server lets the two drift silently | plan authoring (phase 8 § 8.3) |
| 6 | Do not give each advisor rule its own queries | D26 forces one bounded snapshot per instance per run, so adding a rule costs no database load | plan authoring (D26) |

## 11. Progress board

Whole-plan status at a glance. Updated when a unit closes; never allowed to
disagree with §1 and §4.

### Phases

| Phase | File | Status | Notes |
|-------|------|--------|-------|
| 0 — Alert data model and pure logic | phase_01.md | `DONE` | 7/7 sub-phases closed; primary `agent:gpt5.6-luna`; commit `ab5394f` |
| 1 — Alert engine, notifiers, alert API | phase_02.md | `DONE` | 8/8 sub-phases closed; primary `agent:gpt5.6-luna`; phase commit pending |
| 2 — Protocol v2: facts and typed storage | phase_03.md | `DONE` | 7/7 sub-phases closed; INT-FACT-001..006 and INT-GOLDEN-002 green; commit `8a66327`; primary `agent:gpt5.6-luna` |
| 3 — Contention: locks, activity, transactions | phase_04.md | `DONE` | 8/8 sub-phases closed; INT-LOCK-001..006 and INT-ACT-001..004 green; phase gates green; commit `11d0e61`; primary `agent:gpt5.6-luna` |
| 4 — Space and maintenance: tables, indexes, vacuum, bloat | phase_05.md | `TODO` | 0/8 sub-phases closed; primary `agent:gpt5.6-luna` |
| 5 — Configuration and durability: settings, WAL, checkpointer, I/O, archiver | phase_06.md | `TODO` | 0/9 sub-phases closed; primary `agent:gpt5.6-luna` |
| 6 — Host and container metrics | phase_07.md | `TODO` | 0/6 sub-phases closed; primary `agent:gpt5.6-luna` |
| 7 — Advisor engine and rule packs | phase_08.md | `TODO` | 0/10 sub-phases closed; primary `agent:gpt5.6-luna` |
| 8 — Command channel and query plans | phase_09.md | `TODO` | 0/11 sub-phases closed; primary `agent:gpt5.6-luna` |
| 9 — L3 end-to-end scenarios | phase_10.md | `TODO` | 0/8 sub-phases closed; primary `agent:gpt5.6-luna` |
| 10 — Packaging, CI, final documentation | phase_11.md | `TODO` | 0/5 sub-phases closed; primary `agent:gpt5.6-luna` |

Status values: `TODO` · `IN_PROGRESS` · `DONE` · `SKIPPED` · `BLOCKED`
A `SKIPPED` sub-phase or phase keeps its row and carries the reason.

**3 of 11 phases `DONE`. Plan at 27% — phase 4 is next.**

### Tests

Unit tests are named in the phase files and tracked by the coverage gate. This
table tracks the referenceable integration and system tests — the ones a phase
done-criterion names. Every id here must exist in the code when its phase closes,
and every `INT-*`/`SYS-*` id in the code must appear here. `EXTEND` means the id
already exists from plan 001 and this plan widens it; it must stay green
throughout.

| ID | Type | Phase | Status | Notes |
|----|------|-------|--------|-------|
| INT-ACT-001 | integration | 3 | `PASS` | phase 3 § 3.3 — Extend the `activity` check |
| INT-ACT-002 | integration | 3 | `PASS` | phase 3 § 3.3 — Extend the `activity` check |
| INT-ACT-003 | integration | 3 | `PASS` | phase 3 § 3.5 — Contention API |
| INT-ACT-004 | integration | 3 | `PASS` | phase 3 § 3.7 — Contention integration test sweep |
| INT-ADV-001 | integration | 7 | `TODO` | phase 7 § 7.1 — Migration `0009_findings.sql` |
| INT-ADV-002 | integration | 7 | `TODO` | phase 7 § 7.3 — The snapshot |
| INT-ADV-003 | integration | 7 | `TODO` | phase 7 § 7.3 — The snapshot |
| INT-ADV-004 | integration | 7 | `TODO` | phase 7 § 7.7 — The advisor engine |
| INT-ADV-005 | integration | 7 | `TODO` | phase 7 § 7.7 — The advisor engine |
| INT-ADV-006 | integration | 7 | `TODO` | phase 7 § 7.8 — Findings API |
| INT-ADV-007 | integration | 7 | `TODO` | phase 7 § 7.9 — Advisor integration sweep |
| INT-ADV-008 | integration | 7 | `TODO` | phase 7 § 7.9 — Advisor integration sweep |
| INT-ALERT-001 | integration | 1 | `PASS` | phase 1 § 1.2 — SQL sources |
| INT-ALERT-002 | integration | 1 | `PASS` | phase 1 § 1.2 — SQL sources |
| INT-ALERT-003 | integration | 1 | `PASS` | phase 1 § 1.4 — Alert persistence and once-only delivery |
| INT-ALERT-004 | integration | 1 | `PASS` | phase 1 § 1.4 — Alert persistence and once-only delivery |
| INT-ALERT-005 | integration | 1 | `PASS` | phase 1 § 1.4 — Alert persistence and once-only delivery |
| INT-ALERT-006 | integration | 1 | `PASS` | phase 1 § 1.4 — Alert persistence and once-only delivery |
| INT-ALERT-007 | integration | 1 | `PASS` | phase 1 § 1.4 — Alert persistence and once-only delivery |
| INT-ALERT-008 | integration | 1 | `PASS` | phase 1 § 1.7 — Engine integration tests |
| INT-ALERT-009 | integration | 1 | `PASS` | phase 1 § 1.7 — Engine integration tests |
| INT-ALERT-010 | integration | 1 | `PASS` | phase 1 § 1.7 — Engine integration tests |
| INT-ALERT-011 | integration | 1 | `PASS` | phase 1 § 1.7 — Engine integration tests |
| INT-ALERT-012 | integration | 1 | `PASS` | phase 1 § 1.7 — Engine integration tests |
| INT-ALERTAPI-001 | integration | 1 | `PASS` | phase 1 § 1.5 — Alert and silence HTTP API |
| INT-ALERTAPI-002 | integration | 1 | `PASS` | phase 1 § 1.5 — Alert and silence HTTP API |
| INT-ARCH-001 | integration | 5 | `TODO` | phase 5 § 5.6 — The `archiver` check |
| INT-ARCH-002 | integration | 5 | `TODO` | phase 5 § 5.6 — The `archiver` check |
| INT-ARCH-003 | integration | 5 | `TODO` | phase 5 § 5.6 — The `archiver` check |
| INT-BGW-001 | integration | 5 | `TODO` | phase 5 § 5.4 — The `checkpointer` check |
| INT-BGW-002 | integration | 5 | `TODO` | phase 5 § 5.4 — The `checkpointer` check |
| INT-BLOAT-001 | integration | 4 | `TODO` | phase 4 § 4.4 — The `bloat_estimate` check |
| INT-BLOAT-002 | integration | 4 | `TODO` | phase 4 § 4.4 — The `bloat_estimate` check |
| INT-BLOAT-003 | integration | 4 | `TODO` | phase 4 § 4.4 — The `bloat_estimate` check |
| INT-BLOAT-004 | integration | 4 | `TODO` | phase 4 § 4.7 — Integration sweep and permission verification |
| INT-CFG-001 | integration | 3 | `EXTEND` | exists from plan 001 — extended by phase 3 § 3.6 (Agent configuration for the new check) |
| INT-CHECK-015 | integration | 2 | `EXTEND` | exists from plan 001 — extended by phase 2 § 2.7 (Update README.md) |
| INT-CHECK-016 | integration | 4 | `TODO` | phase 4 § 4.7 — Integration sweep and permission verification |
| INT-CHECK-017 | integration | 5 | `TODO` | phase 5 § 5.8 — Integration sweep and permission verification |
| INT-CMD-001 | integration | 8 | `TODO` | phase 8 § 8.1 — Migration `0010_commands.sql` |
| INT-CMD-002 | integration | 8 | `TODO` | phase 8 § 8.3 — Server-side queue and endpoints |
| INT-CMD-003 | integration | 8 | `TODO` | phase 8 § 8.3 — Server-side queue and endpoints |
| INT-CMD-004 | integration | 8 | `TODO` | phase 8 § 8.4 — Atomic claim and expiry |
| INT-CMD-005 | integration | 8 | `TODO` | phase 8 § 8.4 — Atomic claim and expiry |
| INT-CMD-006 | integration | 8 | `TODO` | phase 8 § 8.2 — Command types and gate evaluation |
| INT-CMD-007 | integration | 8 | `TODO` | phase 8 § 8.6 — The `explain` executor |
| INT-CMD-008 | integration | 8 | `TODO` | phase 8 § 8.7 — The `cancel` and `terminate` executors |
| INT-CMD-009 | integration | 8 | `TODO` | phase 8 § 8.8 — The `pgstattuple` executor |
| INT-CMD-010 | integration | 8 | `TODO` | phase 8 § 8.8 — The `pgstattuple` executor |
| INT-CMD-011 | integration | 8 | `TODO` | phase 8 § 8.10 — Audit trail verification |
| INT-FACT-001 | integration | 2 | `PASS` | phase 2 § 2.2 — Migration `0007_facts.sql` |
| INT-FACT-002 | integration | 2 | `PASS` | phase 2 § 2.3 — Store row types and writers |
| INT-FACT-003 | integration | 2 | `PASS` | phase 2 § 2.3 — Store row types and writers |
| INT-FACT-004 | integration | 2 | `PASS` | phase 2 § 2.4 — Pipeline routing for facts and relation metrics |
| INT-FACT-005 | integration | 2 | `PASS` | phase 2 § 2.4 — Pipeline routing for facts and relation metrics |
| INT-FACT-006 | integration | 2 | `PASS` | phase 2 § 2.4 — Pipeline routing for facts and relation metrics |
| INT-GOLDEN-001 | integration | 2 | `EXTEND` | exists from plan 001 — extended by phase 2 § 2.5 (Golden payload for protocol v2) |
| INT-GOLDEN-002 | integration | 2 | `PASS` | phase 2 § 2.5 — Golden payload for protocol v2 |
| INT-HOST-001 | integration | 6 | `TODO` | phase 6 § 6.2 — cgroup v1 and v2 detection |
| INT-HOST-002 | integration | 6 | `TODO` | phase 6 § 6.3 — Agent wiring |
| INT-HOST-003 | integration | 6 | `TODO` | phase 6 § 6.4 — Server routing and host API |
| INT-HOST-004 | integration | 6 | `TODO` | phase 6 § 6.5 — Container validation |
| INT-HOST-005 | integration | 6 | `TODO` | phase 6 § 6.5 — Container validation |
| INT-IDX-001 | integration | 4 | `TODO` | phase 4 § 4.2 — The `index_stats` check |
| INT-IDX-002 | integration | 4 | `TODO` | phase 4 § 4.2 — The `index_stats` check |
| INT-IDX-003 | integration | 4 | `TODO` | phase 4 § 4.2 — The `index_stats` check |
| INT-IDX-004 | integration | 4 | `TODO` | phase 4 § 4.6 — Relation API |
| INT-INGEST-002 | integration | 0 | `EXTEND` | exists from plan 001 — extended by phase 0 § 0.1 (Migration `0006_alerts.sql`) |
| INT-IO-001 | integration | 5 | `TODO` | phase 5 § 5.5 — The `io` check |
| INT-IO-002 | integration | 5 | `TODO` | phase 5 § 5.5 — The `io` check |
| INT-LOCK-001 | integration | 3 | `PASS` | phase 3 § 3.1 — The `locks` check |
| INT-LOCK-002 | integration | 3 | `PASS` | phase 3 § 3.1 — The `locks` check |
| INT-LOCK-003 | integration | 3 | `PASS` | phase 3 § 3.1 — The `locks` check |
| INT-LOCK-004 | integration | 3 | `PASS` | phase 3 § 3.2 — Lock snapshot storage |
| INT-LOCK-005 | integration | 3 | `PASS` | phase 3 § 3.5 — Contention API |
| INT-LOCK-006 | integration | 3 | `PASS` | phase 3 § 3.7 — Contention integration test sweep |
| INT-PIPE-003 | integration | 2 | `EXTEND` | exists from plan 001 — extended by phase 2 § 2.4 (Pipeline routing for facts and relation metrics) |
| INT-PLAN-001 | integration | 8 | `TODO` | phase 8 § 8.9 — Plan storage and history API |
| INT-PLAN-002 | integration | 8 | `TODO` | phase 8 § 8.9 — Plan storage and history API |
| INT-SET-001 | integration | 5 | `TODO` | phase 5 § 5.2 — The `settings` check |
| INT-SET-002 | integration | 5 | `TODO` | phase 5 § 5.2 — The `settings` check |
| INT-SET-003 | integration | 5 | `TODO` | phase 5 § 5.7 — Settings API and cluster drift |
| INT-SET-004 | integration | 5 | `TODO` | phase 5 § 5.8 — Integration sweep and permission verification |
| INT-STALE-003 | integration | 0 | `EXTEND` | exists from plan 001 — extended by phase 0 § 0.7 (Update README.md) |
| INT-STMT-002 | integration | 4 | `EXTEND` | exists from plan 001 — extended by phase 4 § 4.8 (Update README.md) |
| INT-STORE-003 | integration | 0 | `EXTEND` | exists from plan 001 — extended by phase 0 § 0.1 (Migration `0006_alerts.sql`) |
| INT-TBL-001 | integration | 4 | `TODO` | phase 4 § 4.1 — The `table_stats` check |
| INT-TBL-002 | integration | 4 | `TODO` | phase 4 § 4.1 — The `table_stats` check |
| INT-TBL-003 | integration | 4 | `TODO` | phase 4 § 4.1 — The `table_stats` check |
| INT-TBL-004 | integration | 4 | `TODO` | phase 4 § 4.6 — Relation API |
| INT-VAC-001 | integration | 4 | `TODO` | phase 4 § 4.3 — The `vacuum_progress` check |
| INT-VAC-002 | integration | 4 | `TODO` | phase 4 § 4.3 — The `vacuum_progress` check |
| INT-WAL-000 | integration | 5 | `TODO` | phase 5 § 5.1 — Verify the PG 18 WAL statistics shape |
| INT-WAL-001 | integration | 5 | `TODO` | phase 5 § 5.3 — The `wal` check |
| INT-WAL-002 | integration | 5 | `TODO` | phase 5 § 5.3 — The `wal` check |
| INT-WAL-003 | integration | 5 | `TODO` | phase 5 § 5.3 — The `wal` check |
| SYS-ADV-001 | system | 7 | `TODO` | phase 7 § 7.5 — Rule pack B — indexes, tables, autovacuum, bloat |
| SYS-ADV-002 | system | 7 | `TODO` | phase 7 § 7.4 — Rule pack A — queries and transactions |
| SYS-ADV-003 | system | 7 | `TODO` | phase 7 § 7.6 — Rule pack C — configuration, connections, durability |
| SYS-ADV-004 | system | 9 | `TODO` | phase 9 § 9.5 — Advisor acceptance |
| SYS-ALERT-001 | system | 1 | `TODO` | phase 1 § 1.3 — Notifiers |
| SYS-ALERT-002 | system | 9 | `TODO` | phase 9 § 9.2 — Alerting end to end |
| SYS-ALERT-003 | system | 9 | `TODO` | phase 9 § 9.2 — Alerting end to end |
| SYS-ALERT-004 | system | 9 | `TODO` | phase 9 § 9.2 — Alerting end to end |
| SYS-ARCH-001 | system | 9 | `TODO` | phase 9 § 9.7 — Backup and archiving |
| SYS-BLOAT-001 | system | 9 | `TODO` | phase 9 § 9.4 — Vacuum, bloat, index usage and relation cardinality |
| SYS-CMD-001 | system | 8 | `TODO` | phase 8 § 8.5 — Agent-side dispatcher |
| SYS-CMD-002 | system | 9 | `TODO` | phase 9 § 9.6 — Command channel acceptance |
| SYS-CMD-003 | system | 9 | `TODO` | phase 9 § 9.6 — Command channel acceptance |
| SYS-CMD-004 | system | 9 | `TODO` | phase 9 § 9.6 — Command channel acceptance |
| SYS-DEADLOCK-001 | system | 9 | `TODO` | phase 9 § 9.3 — Locks, blocking and deadlocks |
| SYS-DEPLOY-001 | system | 10 | `TODO` | phase 10 § 10.2 — Deploy artefacts |
| SYS-HARNESS-001 | system | 6 | `EXTEND` | exists from plan 001 — extended by phase 6 § 6.5 (Container validation) |
| SYS-IDX-001 | system | 7 | `TODO` | phase 7 § 7.5 — Rule pack B — indexes, tables, autovacuum, bloat |
| SYS-IDX-002 | system | 4 | `TODO` | phase 4 § 4.5 — The shared relation cardinality budget |
| SYS-IDX-003 | system | 7 | `TODO` | phase 7 § 7.5 — Rule pack B — indexes, tables, autovacuum, bloat |
| SYS-LOCK-001 | system | 9 | `TODO` | phase 9 § 9.3 — Locks, blocking and deadlocks |
| SYS-PERM-001 | system | 4 | `EXTEND` | exists from plan 001 — extended by phase 4 § 4.7 (Integration sweep and permission verification) |
| SYS-PERM-002 | system | 10 | `TODO` | phase 10 § 10.3 — Permission-tier grant scripts |
| SYS-REPL-006 | system | 9 | `TODO` | phase 9 § 9.1 — Cascading replication topology |
| SYS-VAC-001 | system | 9 | `TODO` | phase 9 § 9.4 — Vacuum, bloat, index usage and relation cardinality |

### Docs

One row per phase README sub-phase, so a partially documented feature is visible;
other docs get their own rows.

| Doc | Phase | Status | Notes |
|-----|-------|--------|-------|
| README.md | 0 | `DONE` | verified and corrected obsolete unshipped claim in 0.7 |
| README.md | 1 | `TODO` | closing sub-phase of phase_02.md |
| README.md | 2 | `DONE` | protocol 1/2 compatibility, upgrade ordering, and supported range documented in 2.7 |
| README.md | 3 | `DONE` | locks/activity configuration, APIs, sampling, truncation, and deadlock limitations documented in 3.8 |
| README.md | 4 | `TODO` | closing sub-phase of phase_05.md |
| README.md | 5 | `TODO` | closing sub-phase of phase_06.md |
| README.md | 6 | `TODO` | closing sub-phase of phase_07.md |
| README.md | 7 | `TODO` | closing sub-phase of phase_08.md |
| README.md | 8 | `TODO` | closing sub-phase of phase_09.md |
| README.md | 9 | `TODO` | closing sub-phase of phase_10.md |
| README.md | 10 | `TODO` | closing sub-phase of phase_11.md |
| CONTRIBUTING.md | 9 | `TODO` | 9.8 — how to add an E2E scenario, plus the five anti-flake rules |
| deploy/sql/README.md | 10 | `TODO` | 10.3 — what each permission tier unlocks and what degrades without it |
| docs/LIMITS.md | 10 | `TODO` | 10.4 — the ten declared limits, including that pglens does not verify backups |

### Audits

One row per verify report, so the audit history is visible from the entry point.
Findings themselves live in `verify/index.md`.

| Report | Date | Verdict | Open findings |
|--------|------|---------|---------------|
| — | — | *no audit run yet* | — |
