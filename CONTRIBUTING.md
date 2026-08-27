# Contributing

## Test levels

- **L1 — Unit tests** (`make test`). Pure logic, no I/O, no Docker. Uses fake clock where timing is needed (the `clock.Frozen` fake time and `test.Eventually`/`Consistently` in harness for polling, never `time.Sleep`). Tests pass `-race -shuffle=on` for data-race detection and randomized execution order. Runs in seconds.
  - Belongs here: parsing, validation, calculations, marshaling, single-component behavior.

- **L2 — Integration tests** (`make test-integration`). One real PostgreSQL container per major version (15, 18), tests inside each container run serially (exclusive access needed for `postgres.Restore()`). Parallelism comes from the version matrix (`PGLENS_PG_VERSIONS`) and profile matrix (`vanilla` / `rds-like`). Covers agent-to-database behavior and wire format across versions. See `test/pgtest/`.
  - Belongs here: agent connection, scheduling, query execution, per-version codec differences.
  - Runs in 15 minutes.

- **L3 — E2E system tests** (`make test-e2e` for smoke subset, `make test-e2e-full` for complete suite). Real multi-container topologies (standalone, primary-standby, with optional Toxiproxy for fault injection and workload generator). Uses the test harness in `test/harness/` to orchestrate stacks, assert on invariants, and inject network faults. No retries; a flaky E2E is a real race in the product. Artifacts on failure.
  - Belongs here: end-to-end flows with real concurrency, failure modes, timing, global invariants.
  - **ASH scenarios** (`SYS-LOAD-003`, `SYS-LOAD-005`, `SYS-ASH-001`, `SYS-ASH-002` in `test/scenario/ash.go`): test statistical correctness, conservation of sample counts, significance warnings at <60 samples, and cardinality capping at 100 distinct wait keys per window. These scenarios are part of the smoke set.
  - Smoke subset: ~10 minutes. Full suite: ~60 minutes.
  - Requires: Docker, ports 8080 (server), 5432+ (PostgreSQL), 8474 (Toxiproxy).

- **L4/L5** — frontend/API testing and performance profiling (not in this plan; documented in `TESTING.md` but unimplemented).

**Key principle:** No `time.Sleep` anywhere except background wait loops. Use `Eventually` (poll 200ms, return last error), `Consistently` (assert non-event for a window), or `clock.Frozen` (fake time in tests that need absolute timing).

## Running one scenario against a live stack

The scenario API and harness are implemented (phase 5.5-5.6), but scenario bodies (phase 5.7) have not yet been implemented. Once scenarios are registered, the workflow will be:

```sh
make e2e-stack-up          # brings up the full stack and leaves it running
make scenario ID=SYS-REPL-001  # runs one scenario against that stack
# explore the API between runs
curl -s localhost:8080/api/v1/clusters | jq .
make e2e-stack-down        # clean up when done
```

The test harness (`test/harness/Harness`) is the core E2E infrastructure. Its API includes:
- `Start(t, cfg)` — brings up a complete stack (topology + agent + optional Toxiproxy) with dynamic port allocation and unique compose project name.
- `API()`, `DB(service)`, `PG(service)` — clients for server HTTP, metrics DB, and monitored PostgreSQL instances.
- `Exec(service, ...)`, `Compose(...)` — container and compose control.
- `Toxic(link, t)` — injects network faults (latency, timeout, bandwidth limit, connection reset, data truncation) via Toxiproxy; returns a removal function.
- `Eventually(timeout, fn)` — polls `fn` every 200ms until it returns nil or timeout; fails with the last error (the diagnosis).
- `Consistently(window, fn)` — asserts `fn` stays nil for the entire window (used to prove non-events, e.g., "no second restart was logged").
- `AssertInvariants(t)` — checks global invariants (no duplicate samples, no negative rates, no orphan metrics, cardinality limits, connection ceilings, no goroutine leaks, stable cluster_id, clean teardown).
- `Dump(t)` — captures logs, API state, row counts, and health status on failure; auto-called by `t.Cleanup`.

The scenario registry (`test/scenario`) defines the interface for scenarios (not yet populated):
- `Register(scenario)` — registers a scenario by stable ID.
- `Get(id)`, `All()`, `Smoke()` — retrieves scenarios (no scenarios registered yet).

Compose topologies in `test/compose/`:
- `base.yml` — TimescaleDB and pglens-server with healthchecks.
- `topo-standalone.yml` — one PostgreSQL with `pg_stat_statements` and `compute_query_id=on`.
- `topo-primary-standby.yml` — PostgreSQL primary + standby with streaming replication.
- `agent-container.yml`, `agent-binary.yml`, `agent-container-no-volume.yml` — three deployment variants.
- `toxiproxy.yml` — optional fault injection layer.

Workload generator (`test/workload/workloadctl`):
```
workloadctl deadlock --dsn ... --pairs 10 --duration 30s
workloadctl lock-storm --dsn ... --sessions 50 --duration 20s
workloadctl slow-query --dsn ... --sleep 30s --count 3
workloadctl idle-in-txn --dsn ... --sessions 5 --hold 2m
workloadctl distinct-queries --dsn ... --count 5000
workloadctl oltp --dsn ... --tps 200 --duration 60s
```
Each command outputs a JSON report with seed, duration, succeeded/failed counts, and command-specific details. Supports `--seed` for reproducibility.

## Adding a new scenario (phase 5.7+)

When scenario bodies are implemented, new scenarios are added to `test/scenario/`. Outline:

1. Create a file `test/scenario/sys_<feature>.go` (e.g., `sys_replication.go` for replication scenarios).

2. Define the scenario and register it:
```go
func init() {
	scenario.Register(scenario.Scenario{
		ID:          "SYS-REPL-001",
		Title:       "Failover detected on promote",
		Topology:    scenario.TopologyPrimaryStandby,
		EstDuration: 30 * time.Second,
		Covers:      []string{"IDEA.md#5.1", "I-1"},
		Smoke:       true,  // include in 10-minute smoke test
		Run:         runSysRepl001,
		Expect: scenario.Expectations{
			Events:     []string{"failover_detected"},
			Invariants: []string{"cluster_id unchanged", "no duplicate samples"},
		},
	})
}

func runSysRepl001(ctx context.Context, e *scenario.Env) error {
	// 1. Use e.PG("pg-primary") and e.PG("pg-standby") to connect
	// 2. Trigger the failure (e.g., promote standby with pg_ctl promote)
	// 3. Assert on API or database state via e.API or e.DB
	// 4. Use e.Eventually or e.Consistently to wait for or verify events
	// 5. MUST call e.AssertInvariants(e.T) at the end
	return nil
}
```

3. Each scenario **must**:
   - Have at least one `Covers` entry (what design decision or invariant it tests).
   - Have at least one `Expect.Events` or `Expect.Invariants` entry.
   - Call `e.AssertInvariants(e.T)` at the end.
   - Use `e.Eventually` or `e.Consistently` instead of `time.Sleep`.
   - Return `nil` on success, `error` on failure.

4. Test locally:
```sh
make e2e-stack-up
make scenario ID=SYS-REPL-001
# repeat with different topologies and agent modes
make scenario ID=SYS-REPL-001 TOPOLOGY=primary-standby AGENT_MODE=binary
make e2e-stack-down
```

5. Once ready, add to the smoke set in `phase_06.md §5.7` and verify `make test-e2e` passes.

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

**Timing:** No `time.Sleep` anywhere below L5. Use precise coordination mechanisms instead:
- In L1 (unit tests): fake time with `clock.Frozen` or `clock.NewFake()` and advance it explicitly.
- In L2/L3 (integration/E2E): `Eventually(timeout, fn)` polls every 200ms until `fn() error` returns nil, then reports the last error on timeout (the actual diagnosis). Use `Consistently(window, fn)` to assert that something does NOT happen for a full window.

**Retries:** Never retry a test below L5. A flaky test at L2 or L3 is evidence of a real race in the product:
- Add synchronization primitives (channels, condition variables).
- Fix the race (add a lock, make an operation atomic).
- If the test itself is genuinely racy (e.g., it depends on timing it cannot control), quarantine it (see below).

**Quarantine:** When a test must be skipped:
```go
if os.Getenv("ALLOW_FLAKY_TEST_SYS_LOAD_002") != "1" {
	t.Skip("SYS-LOAD-002: flaky on slow runners; owner: alice@example.com; expires: 2026-09-27")
}
```
- Use a distinct env var and test ID.
- Name the owner (who is responsible for fixing or removing it).
- Set an expiry date (14 days from now, or when the underlying issue is expected to be fixed).

**Parallelism:** 
- `t.Parallel()` only at the top level of test functions or via `pgtest.ForEach` (which parallelizes across versions).
- Never use `t.Parallel()` inside a `pgtest.ForEach` body — `postgres.Restore()` needs exclusive access to the container.
- L3 tests do not call `t.Parallel()`; each test uses a unique compose project name, so they can run concurrently anyway (orchestrated by `-parallel` in the test runner).

**ASH (Active Session History):** ASH counts are independent observations (samples), not cumulative counters. Never pipe them through `internal/delta` — each sample is a point-in-time snapshot, not a delta from the previous one. ASH rates and totals are computed by the query layer, not by the ingestion path.

## Project layout

```
cmd/pglens-agent/  cmd/pglens-server/  internal/{pgtype,clock,identity,delta,cardinality,check,wire,store,server,agent,topology,ash}
deploy/sql/ deploy/compose/ test/{pgtest,fixtures,harness,compose,scenario,workload}
```

Follow this layout; no new top-level directory unless the plan names it.

