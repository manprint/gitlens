# Phase 5 — L3 E2E harness and founding scenarios

> **Intent:** Build the system-level test harness — real topologies in Docker,
> fault injection, a workload generator, a reusable scenario library and global
> invariants — then use it to prove the chain survives the failures that matter.
> **Shippable alone?** yes — adds test infrastructure and scenarios; no product
> behavior changes.
> **Preconditions:** phase 4 DONE. `make build-images` works.

This is the phase that decides whether the project can be trusted. Everything
before it proves components; this proves the system, while it is being broken on
purpose.

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

### 5.1 The E2E harness

- **Model:** `agent-1:opus`
- **Assignment:** `agent-1:opus` — harness contract. **`agent-1` review gate by construction:** every scenario in this and every later phase is written against this API, and the anti-flake properties have to be built in rather than retrofitted.
- **Files:** `test/harness/harness.go`, `test/harness/eventually.go`, `test/harness/dump.go`, `test/harness/api.go`, `Makefile`, `.github/workflows/pr.yml`
- **Change:**
  ```go
  //go:build e2e

  type Config struct {
  	Topology  Topology  // Standalone | PrimaryStandby
  	PGVersion int       // default from PGLENS_PG_VERSIONS
  	AgentMode AgentMode // Container | Binary | ContainerNoVolume
  	Toxiproxy bool
  	Pgbouncer bool      // reserved; not used in this plan
  }

  type Harness struct{ /* project name, compose files, ports, clients */ }

  // Start brings up the stack and blocks until every service is healthy. It
  // registers teardown on t.Cleanup, so a panicking test never leaks containers.
  func Start(t *testing.T, cfg Config) *Harness

  func (h *Harness) API() *APIClient
  func (h *Harness) DB() *pgxpool.Pool          // direct TimescaleDB access for invariants
  func (h *Harness) PG(service string) *pgxpool.Pool
  func (h *Harness) Exec(service string, argv ...string) (string, error)
  func (h *Harness) Compose(argv ...string) error   // pause, unpause, stop, start, rm
  func (h *Harness) Scenario(t *testing.T, id string)
  func (h *Harness) Dump(t *testing.T)          // deferred by Start; runs only on failure
  ```
  Mandatory properties, each of which prevents a specific known failure of E2E
  suites:
  - **Isolation.** Every `Start` uses a unique compose project name and
    dynamically allocated host ports, so two tests can run concurrently and a
    leftover stack from a killed run never poisons the next one.
  - **Health, not "started".** Every service declares a `healthcheck` and `Start`
    waits for `healthy`. Waiting for "container running" is the single most
    common source of E2E flake.
  - **Compressed intervals.** The agent is configured with `activity: 1s`,
    `database_stats: 1s`, `stat_statements: 2s`, `push_interval: 1s` and
    `--testing.no-jitter`. Production defaults would make every scenario a
    minute long, and slow suites stop being run.
  - **`Eventually`, never `sleep`.**
    ```go
    // Eventually polls fn every 200ms until it returns nil or the timeout
    // elapses, then fails with the LAST error rather than a bare timeout — the
    // last error is the diagnosis.
    func Eventually(t *testing.T, timeout time.Duration, fn func() error)

    // Consistently asserts fn stays nil for the whole window. Used to prove a
    // NON-event, e.g. "no second agent_down was emitted".
    func Consistently(t *testing.T, window time.Duration, fn func() error)
    ```
    `Consistently` matters as much as `Eventually`: half the invariants in this
    phase are about something *not* happening.
  - **`Dump` on failure.** Writes to `test/e2e/_artifacts/<test>/`: `docker compose
    logs` per service, the output of `pglens-agent check`, `/api/v1/clusters`,
    `/api/v1/events`, the agent health body, and row counts plus the most recent
    100 rows per metric table. Uploaded as a CI artifact. **A red E2E in CI with
    no artifact costs half a day; this is what prevents that.**
  - **Zero retries.** L3 never retries. A flaky system test is a real race in the
    product, and retrying it hides the only evidence.
  1. Makefile:
     ```makefile
     test-e2e:
     	$(GO) test -tags=e2e -timeout=35m -count=1 ./test/e2e/... -run 'Smoke'
     test-e2e-full:
     	$(GO) test -tags=e2e -timeout=45m -count=1 ./test/e2e/...
     e2e-stack-up:
     	./test/harness/stack.sh up
     e2e-stack-down:
     	./test/harness/stack.sh down
     scenario:
     	./test/harness/stack.sh scenario $(ID)
     ```
     `-count=1` disables the test result cache; a cached E2E pass is a lie.
     `stack.sh` brings up a stack and leaves it running for manual exploration,
     which is how anyone will actually develop against this.
  2. CI job in `.github/workflows/pr.yml`:
     ```yaml
       e2e-smoke:
         runs-on: ubuntu-latest
         timeout-minutes: 45
         strategy:
           fail-fast: false
           matrix:
             agent_mode: ['container', 'binary']
         steps:
           - uses: actions/checkout@v4
           - uses: actions/setup-go@v5
             with: { go-version: '1.26', cache: true }
           - run: make build-images
           - run: make test-e2e
             env:
               AGENT_MODE: ${{ matrix.agent_mode }}
               PGLENS_PG_VERSIONS: '18'
           - uses: actions/upload-artifact@v4
             if: failure()
             with:
               name: e2e-artifacts-${{ matrix.agent_mode }}
               path: test/e2e/_artifacts/
     ```
- **Unit tests:** `TestEventually_ReturnsLastError` — a function that always fails leaves the final error in the failure message. `TestConsistently_FailsOnFirstBreach`. Both run without Docker.
- **e2e tests:** `SYS-HARNESS-001` — `Start` with `Standalone` reaches healthy, `/readyz` returns 200, `Dump` produces a non-empty artifact directory when forced, and teardown leaves no container or volume behind (asserted with `docker ps -a` and `docker volume ls` filtered by the project name).
- **Done:** `make test-e2e -run Smoke/Harness` green for both `AGENT_MODE` values; running the suite twice leaves no orphan containers or volumes; `grep -rn "time.Sleep" test/harness/ test/e2e/` returns nothing; closed in `STATE.md`.

### 5.2 Topologies and the agent deployment matrix

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — compose composition.
- **Files:** `test/compose/base.yml`, `topo-standalone.yml`, `topo-primary-standby.yml`, `agent-container.yml`, `agent-binary.yml`, `agent-container-no-volume.yml`, `test/fixtures/sql/replica_setup.sh`
- **Change:**
  1. `base.yml` — `timescaledb` and `pglens-server`, both with healthchecks.
  2. `topo-standalone.yml` — one PostgreSQL with `pg_stat_statements` preloaded,
     `compute_query_id=on`, and `deploy/sql/monitoring_user.sql` applied at init.
  3. `topo-primary-standby.yml` — `pg-primary` plus `pg-standby` built with
     `pg_basebackup -R`, sharing the primary's `system_identifier` by
     construction. This is the topology that makes invariant I-1 testable at all.
     Configuration on the primary: `wal_level=replica`, `max_wal_senders=10`,
     `hot_standby=on`, a replication slot `standby1`, and
     `primary_conninfo` carrying `application_name=pg-standby` so the edge match
     in phase 6 has a reliable key.
  4. The three agent variants, which the same scenario suite runs against
     unchanged (decision D20 and `TESTING.md` §5.2):

     | `AGENT_MODE` | Shape | What only this mode proves |
     |---|---|---|
     | `container` | service with the named volume, `/proc` and `/sys` mounted read-only | container path, cgroup v2, identity persistence |
     | `binary` | `bin/pglens-agent` run as a host subprocess against published ports | native path, no `HOST_PROC`, systemd-shaped deployment |
     | `container-no-volume` | as `container`, volume removed | **negative test** — the documented consequence of the misconfiguration |

     `AgentMode.Binary` builds the binary via `make build` if absent, starts it
     with `os/exec`, streams its output into the artifact directory, and kills it
     on cleanup.
- **Unit tests:** none (compose configuration).
- **e2e tests:** `SYS-TOPO-001` — `PrimaryStandby` reaches a state where `pg_stat_replication` on the primary has exactly one row and the standby reports `pg_is_in_recovery() = true`; both report the **same** `system_identifier`. This is the precondition every replication scenario depends on, so it fails loudly and early rather than as a confusing symptom elsewhere.
- **Done:** all three `AGENT_MODE` values bring up a stack whose agent reaches the server; `SYS-TOPO-001` green; closed in `STATE.md`.

### 5.3 Fault injection with Toxiproxy

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — fault-injection plumbing.
- **Files:** `test/compose/toxiproxy.yml`, `test/harness/toxic.go`
- **Change:** Toxiproxy sits between the agent and PostgreSQL, and between the
  agent and the server (decision D5). Chosen over `tc netem` because it needs no
  `NET_ADMIN`, behaves identically on every runner, and has a Go client — a
  network fault that behaves differently in CI than locally is worse than no
  test.
  ```go
  func (h *Harness) Toxic(link Link, t Toxic) RemoveFunc
  // Links: AgentToServer, AgentToPG(service)
  // Toxics: Latency{Ms, JitterMs}, Timeout{} (black hole), Bandwidth{KBps},
  //         LimitData{Bytes} (truncate mid-response), ResetPeer{}
  ```
  `Timeout{}` — a connection that accepts and then never answers — is the most
  valuable of these: it is what a hung database or a dropped firewall state
  actually looks like, and it separates a client that honors deadlines from one
  that hangs forever. A plain "connection refused" does not test that at all.
  Also expose `h.Compose("pause"|"unpause", service)`: `docker pause` freezes a
  container without closing its sockets, reproducing a suspended VM or a long
  stop-the-world pause, which no network toxic can imitate.
- **Unit tests:** none.
- **e2e tests:** `SYS-NET-002`, `SYS-NET-003`, `SYS-NET-005` (below).
- **Done:** a toxic can be added and removed within a running stack and the effect is observable in agent metrics; closed in `STATE.md`.

### 5.4 The workload generator

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — deterministic pathology generation.
- **Files:** `test/workload/main.go`, `test/workload/deadlock.go`, `lockstorm.go`, `slowquery.go`, `idleintxn.go`, `distinct.go`, `oltp.go`
- **Change:** `workloadctl`, producing **deterministic** pathologies and a JSON
  report on stdout. Tests assert on the report, never on the hope that the load
  did what it was asked to do.
  ```
  workloadctl deadlock         --dsn … --pairs 10 --duration 30s
  workloadctl lock-storm       --dsn … --sessions 50 --duration 20s
  workloadctl slow-query       --dsn … --sleep 30s --count 3
  workloadctl idle-in-txn      --dsn … --sessions 5 --hold 2m
  workloadctl distinct-queries --dsn … --count 5000
  workloadctl oltp             --dsn … --tps 200 --duration 60s
  ```
  Implementation notes that decide whether these work at all:
  - **`deadlock`** — two transactions updating two rows in opposite order, with a
    `sync.WaitGroup` barrier between the first update and the second. Without the
    barrier the deadlock happens only sometimes and the test becomes flaky; with
    it, every pair deadlocks. The report states how many actually did.
  - **`distinct-queries`** — this is the subtle one. `pg_stat_statements`
    normalizes literals, so `SELECT 1`, `SELECT 2`, … collapse into a single
    `queryid`. Distinct ids need distinct **parse trees**. Emit expressions of
    increasing length — `SELECT 1+1`, `SELECT 1+1+1`, … — which is cheap and
    guaranteed distinct on every supported version. Do **not** use `IN` lists of
    varying length: PostgreSQL 18 squashes lists of constants, so the trick that
    works on 15 silently stops working on 18, which is the worst kind of test
    dependency.
  - **`lock-storm`** — N sessions doing `SELECT … FOR UPDATE` on one row while
    one holds it. The report gives the observed maximum wait.
  - **`idle-in-txn`** — `BEGIN`, one statement, then hold without commit.
  - **`oltp`** — `pgbench`-shaped read/write mix used as realistic background
    noise so scenarios do not run against an unnaturally idle database.
  Every command supports `--seed` and prints the seed it used, so any run is
  reproducible.
- **Unit tests:** `TestDistinct_ParseTreesDiffer` — generated statements are pairwise distinct after literal normalization (asserted by a table of expected shapes, no database needed). `TestReport_JSONShape`.
- **Integration tests:** `INT-WL-001` — `deadlock --pairs 5` against a real container reports exactly 5 deadlocks and `pg_stat_database.deadlocks` increases by 5. `INT-WL-002` — `distinct-queries --count 500` yields 500 distinct `queryid` values in `pg_stat_statements` on **every** supported version, 18 included.
- **e2e tests:** `SYS-LOAD-002`, `SYS-LOAD-008`.
- **Done:** `make test-integration` green; `INT-WL-002` passes on 15 and 18; closed in `STATE.md`.

### 5.5 The scenario library

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — the reusable definition of "break it this way".
- **Files:** `test/scenario/registry.go`, `test/scenario/*.go`, `test/harness/stack.sh`
- **Change:** each scenario is registered once with a stable ID and is invocable
  from any test and from the command line. `TESTING.md` §8 designs this as a
  bridge to a future frontend suite; decision D14 keeps the frontend out of this
  plan, but the library is built now so that later work reuses it instead of
  reinventing it.
  ```go
  type Scenario struct {
  	ID          string        // e.g. "SYS-REPL-001"
  	Title       string
  	Topology    Topology
  	EstDuration time.Duration
  	Covers      []string      // e.g. "IDEA.md#5.1", "I-1"
  	Run         func(ctx context.Context, e *Env) error
  	Expect      Expectations  // events that must appear, invariants that must hold
  }

  func Register(s Scenario)   // panics on duplicate ID
  func Get(id string) (Scenario, bool)
  func All() []Scenario       // sorted by ID
  ```
  `make scenario ID=SYS-REPL-001` runs one against a stack started by
  `make e2e-stack-up`, which is the loop anyone will actually use when
  developing.
- **Unit tests:** `TestRegistry_DuplicateIDPanics`. `TestRegistry_AllScenariosHaveCoversAndExpectations` — every registered scenario names at least one `Covers` entry and at least one expectation; a scenario that asserts nothing is not a scenario.
- **e2e tests:** exercised by 5.7.
- **Done:** `make scenario ID=…` runs against a live stack; `TestRegistry_AllScenariosHaveCoversAndExpectations` passes; closed in `STATE.md`.

### 5.6 Global invariants

- **Model:** `agent-1:opus`
- **Assignment:** `agent-1:opus` — cross-cutting correctness. **`agent-1` review gate by construction.**
- **Files:** `test/harness/invariants.go`, `test/e2e/invariants_test.go`
- **Change:** a single helper, run at the end of **every** scenario, that checks
  the properties nobody thought to assert. In practice this catches more real
  defects than the scenario-specific assertions do, because it catches the bugs
  no one was looking for.
  ```go
  func (h *Harness) AssertInvariants(t *testing.T)
  ```
  | Check | SQL / method | Invariant |
  |---|---|---|
  | No duplicate samples | `SELECT count(*) FROM (SELECT series_id, ts FROM metrics GROUP BY 1,2 HAVING count(*)>1) x` is 0, and the equivalent on each typed table | I-3 |
  | No negative rates | `SELECT count(*) FROM metrics WHERE value < 0` is 0 for every rate metric; same on `metrics_statements` | I-2 |
  | No orphan metrics | every `instance_id` in every metric table exists in `instances` | I-4 |
  | No unexpected errors | `check_error_total` unchanged except for the checks the scenario declared as expected to fail | — |
  | Cardinality within budget | `pglens_series_total` per instance below `max_series_per_instance` | I-8 |
  | Connection ceiling | the peak backend count with `application_name LIKE 'pglens/%'` never exceeded `MaxConnsPerInstance` (sampled during the run) | I-6 |
  | No goroutine leak | the agent's `/healthz` goroutine count is stable within 10% between start and end | — |
  | `cluster_id` stability | no `cluster_id_changed` event exists | I-1 |
  | Clean teardown | no container, volume or network survives with the project name | — |

  Failures name the invariant and dump the offending rows, so the message alone
  is enough to start debugging.
- **Unit tests:** each SQL predicate tested against a fixture database with a deliberately planted violation, proving the check actually detects it. **An invariant check that has never been seen to fail is not known to work** — plant a violation for every one of them.
- **e2e tests:** invoked by every scenario in 5.7.
- **Done:** each invariant has a planted-violation test that fails as expected; `AssertInvariants` runs in under 5 seconds against a populated stack; closed in `STATE.md`.

### 5.7 Founding scenarios

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — scenario implementation.
- **Files:** `test/scenario/reset.go`, `net.go`, `agent.go`, `perm.go`, `load.go`, `db.go`; `test/e2e/smoke_test.go`, `test/e2e/full_test.go`
- **Change:** implement the scenarios below. Every one calls `AssertInvariants`
  at the end. Every one runs under all three `AGENT_MODE` values unless the table
  says otherwise.

  | ID | Scenario | Topology | Assertion |
  |----|----------|----------|-----------|
  | `SYS-RESET-001` | `docker compose kill -s SIGKILL` PostgreSQL, then `start` (**not** a graceful `docker restart` — empirically verified live in session 12: PostgreSQL 15+'s durable stats survive a clean shutdown/restart intact via the on-disk stats file, so `pg_stat_database`'s counters never actually reset that way; only an unclean shutdown does, matching a real crash/OOM-kill) | standalone | a `counter_reset_detected` event exists; **no negative rate and no spike above 10x the pre-restart rate** exists in the window; the series resumes afterwards. Backs I-2 |
  | `SYS-RESET-002` | `SELECT pg_stat_statements_reset()` from an external session | standalone | the delta for that interval is discarded; a `counter_reset_detected` event names `stat_statements`; the next interval produces a normal rate |
  | `SYS-NET-001` | server stopped 10 minutes (compressed to 60s of stack time by the 1s intervals), then started | standalone | `agent_buffer_bytes`/`agent_samples_dropped_total` do not exist (`internal/agent/pusher.go` queues undelivered envelopes in an unbounded in-memory slice, not the disk-backed `internal/agent/buffer.Buffer` that `cmd/pglens-agent/run.go` opens but never writes to — a known, documented gap, STATE.md §6) — verified live in session 12. Asserted instead: `/healthz`'s real `buffer_stats.dropped` stays 0 throughout a 60s outage; after recovery, samples with timestamps from *during* the outage land in `metrics` (the backlog was actually delivered, not skipped); I-3 (no duplicate `(series_id,ts)`, schema-enforced) backs "exactly once" |
  | `SYS-NET-003` | black hole between agent and PostgreSQL (`Toxic Timeout{}`) | standalone | the agent process stays alive and responsive on `/healthz`; **it does not hang** — asserted by `Consistently` on the health endpoint answering within 1s throughout. **Not** `instance_unreachable`: verified live in session 12 that event is driven purely by `instances.last_seen` staleness (`internal/server/staleness.go`) — the whole agent process going silent to the server — which a PG-only black hole never causes, since the agent keeps pushing (with the affected checks' errors embedded, not omitted). Assert instead that `instance_unreachable`/`agent_down` do **not** fire: conflating one check's unreachable target with the whole agent being down would be the wrong behavior |
  | `SYS-NET-005` | `docker pause` on PostgreSQL, then unpause | standalone | treated as unreachable, recovers cleanly, no duplicate rows after resume |
  | `SYS-AGENT-001` | restart the agent **with** its volume | container, binary | the `instance_id` is unchanged; the series is continuous; **no `duplicate_instance_suspected` event** |
  | `SYS-AGENT-002` | restart the agent **without** its volume | container-no-volume | a new `instance_id` appears and `duplicate_instance_suspected` is emitted. **This test asserts the documented failure, not success** — it freezes the known regression from `IDEA.md` §2.2, and if the agent ever learns to recover its identity without a volume, this test fails and forces the documentation to be updated |
  | `SYS-AGENT-003` | buffer directory on a 16 MiB tmpfs, server stopped until it fills | container | `agent_samples_dropped_total` grows, the agent does not crash, PostgreSQL is unaffected, and the host disk does not fill. Backs I-7 |
  | `SYS-AGENT-004` | agent container started with a 5-minute clock offset | container | `agent_clock_skew_seconds` is about 300 and a warning is logged; samples are still accepted, since the threshold is 12h |
  | `SYS-AGENT-005` | `UPDATE agents SET revoked_at = now()` | all | the next push returns 401; the agent stops collecting and reports `revoked` on `/healthz`; **`Consistently` proves no further rows are written for 30 seconds** |
  | `SYS-PERM-001` | the whole stack running with a tier T0 role only | standalone, `rds-like` | every check either succeeds or is skipped with a reason; `check_error_total` stays 0; the API reports `perm_tier: "T0"` |
  | `SYS-LOAD-002` | `workloadctl lock-storm --sessions 50` | standalone | the `activity` check **completes throughout** the storm (its result timestamps keep advancing); `pg_max_xact_age_seconds` reflects the blocked sessions. This is the concrete payoff of per-check timeouts from `IDEA.md` §4.4 |
  | `SYS-LOAD-008` | `workloadctl distinct-queries --count 5000` | standalone | `truncated` reaches the API on `/api/v1/statements`; `pglens_series_total` stays under budget; the top queries by time and by calls are both present. Backs I-8 |
  | `SYS-DB-001` | 15 databases with `max: 10` | standalone | the API reports 10 monitored and 5 with `skip_reason="db_budget"`; the 10 are the most active |

  **Smoke set** (runs on every PR, target under 10 minutes):
  `SYS-HARNESS-001`, `SYS-TOPO-001`, `SYS-RESET-001`, `SYS-NET-001`,
  `SYS-NET-003`, `SYS-AGENT-001`, `SYS-PERM-001`, `SYS-LOAD-008`.
  Everything else runs in `make test-e2e-full`.

  > **No silent caps.** When the suite skips a scenario — an `AGENT_MODE` it does
  > not apply to, a version it cannot run on — it must log the skip and the
  > reason. A suite that quietly runs 8 of 14 scenarios reads as full coverage
  > and is not.
- **Unit tests:** none (these are the tests).
- **e2e tests:** the fourteen above.
- **Done:** `make test-e2e` (smoke) green for `AGENT_MODE` `container` and `binary`; `make test-e2e-full` green for all three modes; `SYS-AGENT-002` fails if the duplicate detection is removed (verify once by disabling it, then restore); every scenario ends with `AssertInvariants`; closed in `STATE.md`.

### 5.8 Update README.md

Mandatory closing sub-phase of every phase. User guide only — no implementation
detail.

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — documentation; `agent-1:opus` reads it on the final phase.
- **Files:** `README.md`, `CONTRIBUTING.md`
- **Change:**
  1. `README.md` — this phase ships **no new user-visible product behavior**. Say
     so. Extend **Running the checks** with `make test-e2e` and `make
     test-e2e-full`, their Docker requirement, their approximate duration and the
     ports they occupy (8080, 5432 and up, 8474). Extend **Known limits** with
     the two facts a user deserves to know because the suite proved them: an
     agent container without a persistent volume duplicates its instances on
     restart, and an unclean shutdown may lose the tail of the current buffer
     segment.
  2. `CONTRIBUTING.md` — new file, and the right home for the developer-facing
     material that must **not** go in the README: the five test levels and what
     belongs in each, how to run one scenario against a live stack
     (`make e2e-stack-up`, `make scenario ID=…`), how to read a CI failure
     artifact, the rule that golden-file changes must be explained in the pull
     request description, and the anti-flake rules — no `sleep`, no retries below
     L5, quarantine with an owner and a fourteen-day expiry.
- **Unit tests:** none (documentation).
- **e2e tests:** none — every command documented was executed from a clean checkout.
- **Done:** a contributor can run one scenario against a live stack from `CONTRIBUTING.md` alone; the README gained no implementation detail; all gates green; closed in `STATE.md` with the §11 docs row for phase 5 set.

---

## Phase gates

- **Fmt:** `make fmt-check`
- **Lint:** `make lint`
- **Test subset:** `make test`, `make test-integration`, `make test-e2e` for `AGENT_MODE={container,binary}`
- **Coverage:** `make coverage-gate`
- **Regression guard:** every phase 0 to 4 gate still green
- **README:** states that this phase ships no user-visible change and documents the new test commands; `CONTRIBUTING.md` created

## Phase done criterion

`make test-e2e` passes for both agent deployment modes with zero retries, and
`make test-e2e-full` passes for all three. Every scenario ends with
`AssertInvariants`, and every invariant has a planted-violation test proving it
detects what it claims to. A failing run produces a complete artifact directory.
Running the suite twice leaves no orphaned container, volume or network.
README.md and `CONTRIBUTING.md` reflect this phase, and `STATE.md` §11 shows
phase 5 `DONE` with every sub-phase closed.
