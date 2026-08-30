# Contributing

## Test levels

- **L1 — Unit tests** (`make test`). Pure logic, no I/O, no Docker. Uses fake clock where timing is needed (the `clock.Frozen` fake time and `test.Eventually`/`Consistently` in harness for polling, never `time.Sleep`). Tests pass `-race -shuffle=on` for data-race detection and randomized execution order. Runs in seconds.
  - Belongs here: parsing, validation, calculations, marshaling, single-component behavior.

- **L2 — Integration tests** (`make test-integration`). Real PostgreSQL containers across the configured version matrix (PG 15–18 in CI); tests inside one container run serially because `postgres.Restore()` needs exclusive access. Covers agent-to-database behavior, storage and wire-format differences. See `test/pgtest/`.
  - Belongs here: agent connection, scheduling, query execution, per-version codec differences.
  - Runs in 15 minutes.

- **L3 — E2E system tests** (`make test-e2e` for smoke subset, `make test-e2e-full` for complete suite). Real multi-container topologies (standalone, primary-standby, with optional Toxiproxy for fault injection and workload generator). Uses the test harness in `test/harness/` to orchestrate stacks, assert on invariants, and inject network faults. No retries; a flaky E2E is a real race in the product. Artifacts on failure.
  - Belongs here: end-to-end flows with real concurrency, failure modes, timing, global invariants.
  - **ASH scenarios** (`SYS-LOAD-003`, `SYS-LOAD-005`, `SYS-ASH-001`, `SYS-ASH-002` in `test/scenario/ash.go`): test statistical correctness, conservation of sample counts, significance warnings at <60 samples, and cardinality capping at 100 distinct wait keys per window. These scenarios are part of the smoke set.
  - Smoke subset: ~10 minutes. Full suite: ~60 minutes.
  - Requires: Docker, ports 8080 (server), 5432+ (PostgreSQL), 8474 (Toxiproxy).

- **L4/L5** — frontend/API testing and performance profiling (not in this plan; documented in `TESTING.md` but unimplemented).

**Key principle:** No `time.Sleep` anywhere except background wait loops. Use `Eventually` (poll 200ms, return last error), `Consistently` (assert non-event for a window), or `clock.Frozen` (fake time in tests that need absolute timing).

## How to add an E2E scenario

1. Put the acceptance test in `test/e2e/<area>_test.go` and its scenario metadata in `test/e2e/scenarios/<area>.yml`. Reuse the compose and fixture files under `test/compose/` and `test/fixtures/`; add a new stack only when the existing topologies cannot express the failure.
2. Give the scenario a stable id in the form `SYS-<AREA>-<NNN>` and use that exact id in the Go test, metadata and assertions.
3. Register the id in `docs/plans/002_plan-AnalysisBackend/STATE.md` §11 test table before closing the change. An id in code but not the table, or in the table but not code, is incomplete.
4. Exercise the scenario with the E2E command, for example:

```sh
go test -tags=e2e -timeout=8m ./test/e2e -run '^TestFull_Archiving$' -count=1 -v
```

5. The test must call the harness invariant check, clean up all resources through `t.Cleanup`, and use the five rules below. Run `make test-e2e` before submitting; run `make test-e2e-full` when the scenario changes shared harness behavior.

## Reading a CI failure

A red L3 run uploads `test/e2e/_artifacts/<test_name>/` to CI. Download the artifact bundle; it contains:
- Service logs (`docker compose logs` per container — agent, server, PostgreSQL, Toxiproxy if used).
- `pglens-agent check` output (readiness probe, permission tier, extension availability).
- API state dumps (`/api/v1/clusters`, `/api/v1/events`, `/api/v1/instances`).
- Database diagnostics (row counts and samples from each metric table and invariant check table).
- Health status (`/healthz`, `/readyz`, goroutine count).

The `Dump` is automatically triggered on test failure via `t.Cleanup` and is the first place to look for root cause.

**Zero retries at L3:** a flaky E2E is a real race in the product, not a test bug. If a test fails intermittently, either fix the product (add synchronization, fix a race), or quarantine the test with an owner and a 14-day expiry (see Anti-flake rules).

## Golden files

Normalized wire envelopes for each major PostgreSQL version are stored in `internal/wire/testdata/`:
- `payload_pg15.json` — envelope structure for PostgreSQL 15.
- `payload_pg18.json` — envelope structure for PostgreSQL 18.

When the wire format changes, regenerate golden files:
```sh
make golden
```

This runs `go test -tags=integration -run Golden -update ./internal/wire`.

**Before committing a golden-file change:**
1. Review the diff in detail (`git diff internal/wire/testdata/`).
2. Explain the change in the pull request description — what broke (what change to the wire format), why it was necessary, and why it is correct.
3. An unexplained golden diff is grounds for request-changes; assume the test change is accidental unless the PR body explains it.
4. Run `make test-integration` to confirm all integration tests still pass with the updated golden files.

## Anti-flake rules

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

## Project layout

```
cmd/pglens-agent/  cmd/pglens-server/  internal/{pgtype,clock,identity,delta,cardinality,check,wire,store,server,agent,topology,ash}
deploy/sql/ deploy/compose/ test/{pgtest,fixtures,harness,compose,scenario,workload}
```

Follow this layout; no new top-level directory unless the plan names it.
