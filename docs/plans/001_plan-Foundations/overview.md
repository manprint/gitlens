# pglens Foundations — Plan Overview

> **Status:** planning | **Authored:** 2026-08-26 by `agent-1:opus`
> **Folder:** `docs/plans/001_plan-Foundations/`
> **Executing this plan? Read [STATE.md](STATE.md) FIRST** — it is the only
> execution-state file: live position, progress board, environment, in-flight
> work, next action. Open a unit in it before touching code, close it after.

## Goal

Build the verified foundations of `pglens`: an agent that collects from real
PostgreSQL instances, a server that ingests into TimescaleDB with correct delta
and counter-reset handling, and the L1/L2/L3 test harness that proves the chain
stays correct while the world breaks around it. The phase deliberately ships a
thin vertical slice rather than broad features — the value delivered is
*correctness that later phases can build on without re-litigating*. No frontend
in this plan (decision D14); the HTTP API is the contract.

```
make e2e-stack-up TOPO=primary-standby AGENT_MODE=container
make scenario ID=SYS-REPL-001                    # promote the standby

curl -s localhost:8080/api/v1/clusters | jq '.[0]'
# { "cluster_id": "7381...",        <- IDENTICAL before and after the failover
#   "primary": "pg-standby",
#   "instances": [ {"role":"primary"}, {"role":"standby"} ] }

go test -tags=e2e ./test/e2e/... -run Smoke      # green for AGENT_MODE=container AND binary
```

## Design decisions

| # | Decision | Consequence |
|---|----------|-------------|
| **D1** | Go 1.26.1 toolchain, monorepo, `chi/v5` router, `pgx/v5` driver | `go.mod` pins `go 1.26`; no framework beyond `chi` |
| **D2** | `sqlc` for the relational meta schema; hand-written `pgx` on the ingest hot path | Codegen step in `make generate`; ingest stays allocation-controlled |
| **D3** | Own TSDB runs on **PostgreSQL 17 + TimescaleDB 2.29** | Compose pins `timescale/timescaledb:2.29.x-pg17`; avoid PG 17.1 (R2) |
| **D4** | L2 isolation via `postgres.WithSnapshot()` + `container.Restore(ctx)` (R3) | One container per PG version; **tests inside one container run serially** — `Restore` needs exclusive access |
| **D5** | Toxiproxy for network fault injection in L3 | No `NET_ADMIN`, no `tc netem`; deterministic across runners |
| **D6** | `TopologyProvider` is an interface from day one, but only the `streaming` provider exists in this plan | Aurora/Patroni providers add a file, never a schema change |
| **D7** | The wire payload carries `CheckPolicy` from day one, but the server sends none yet | Intervals are agent-local YAML in this plan; server-side scheduling is additive later |
| **D8 (user, Q1)** | Project name **`pglens`**; module path `github.com/manprint/pglens`; remote `https://github.com/manprint/pglens` | Avoids the PostgreSQL trademark policy and the `pganalyze` collision. The remote does not exist yet and will be created before the first push; the local repository is already initialized on branch `main` with no commits |
| **D24** | Container images are published as `ghcr.io/manprint/pglens-agent` and `ghcr.io/manprint/pglens-server`; local development builds use the same names with tag `:dev` | GitHub Container Registry pairs with the repository without a second account or a separate credential. Using the full registry path locally means `docker push` needs no retag |
| **D9 (user, Q2)** | Single-tenant behavior, but `tenant_id text NOT NULL DEFAULT 'default'` present on every table from the first migration | Future SaaS is a backfill, not a rewrite. No RLS, no per-tenant quotas in this plan |
| **D10 (user, Q3)** | Hybrid TSDB schema: **typed hypertables** for `statements`, `ash`, `replication`; one **generic** hypertable for everything else | Typed columns enable real `compress_segmentby` (10x+) on the ~95% of rows; the generic table keeps plugin checks migration-free |
| **D11 (user, Q6)** | Supported PostgreSQL range: **15 → 18** | PG13 (EOL 2025-11) and PG14 (EOL 2026-11) dropped. Removes every degraded path of `IDEA.md` §3.6: `query_id` in core, `pg_stat_statements_info.stats_reset` and `pg_read_all_data` all exist from PG14, so PG15+ has them unconditionally |
| **D12 (user, Q7)** | Wire format is **JSON with a versioned envelope**; protobuf deferred | No `buf`/`protoc` toolchain; E2E payloads are `curl`/`jq`-inspectable. Codec swap later is behind one interface |
| **D13 (user, Q8)** | Agent auth: shared **bootstrap token** + server-assigned `agent_id` + working **revocation** path. No rotation, no mTLS, no approval queue | `SYS-AGENT-005` (revoked agent stops) is in scope; enrollment lifecycle is not |
| **D14 (user, Q4)** | **No frontend in this plan.** Test levels L1, L2, L3 only | No Vitest, no Playwright, no `scenariod`. `TESTING.md` L4/L5 stay documented but unimplemented |
| **D15 (user, Q5)** | **Minimal ASH is in scope** (phase 7): 1s sampling, 10s window aggregation, typed table, verified via API and `SYS-LOAD-003` | The riskiest consumer of the data model exercises it inside this plan, not after it |
| **D16 (user, Q10)** | **Per-database level is in scope**: connect-on-demand pool with TTL, budget, whitelist, `pg_stat_statements` with top-N | Cardinality logic (2.5) is exercised by its dominant real source; `SYS-LOAD-008` proves the budget holds |
| **D17** | Deltas are computed **server-side**; the agent ships raw counters plus `stats_reset` | Typed metric tables store rates; the agent stays stateless across restarts |
| **D18** | `cluster_id` is a `uint64` transported as a **JSON string** | `uint64` exceeds IEEE-754 exact integer range; a JSON number would silently lose precision in `jq` and any JS consumer |
| **D19** | Buffer `max_age` 6h · server `max_sample_age` 12h · TimescaleDB `compress_after` 48h | Strict ordering `buffer < sample_age < compress_after` guarantees backfill never lands in a compressed chunk. Corrects the `24h`/`24h` contradiction in `IDEA.md` §4.8 |
| **D20** | Binaries live in `cmd/pglens-agent` and `cmd/pglens-server` (idiomatic Go), not `agent/` + `server/` | Deviates from the tree in `TESTING.md` §2; that document is corrected in sub-phase 0.5 |
| **D21** | `cluster_id` is stored in PostgreSQL as `bigint` holding the **two's-complement bit pattern** of the `uint64` | PostgreSQL has no unsigned 64-bit type. `numeric(20,0)` is exact but slow and bulky; `text` is unindexable as a number. Conversion is confined to `internal/store/clusterid.go` (`ToDB`/`FromDB`) and nowhere else. Values above `2^63` display as negative in `psql`, which is expected and documented on every column that holds one |
| **D23 (user, Q9)** | Licence: **Apache License 2.0** | Permissive with an explicit patent grant, the de facto standard for infrastructure tooling. `LICENSE` and `NOTICE` are created verbatim in sub-phase 0.1; dependencies must carry a compatible licence |
| **D22** | Per-instance connection budget is 3 until phase 7, then **4** (1 shared, 1 dedicated to ASH, 2 rotating for per-database checks) | The ASH sampler runs at 1 Hz and must not contend with the other checks for the shared connection. Sub-phase 7.1 carries the instruction to raise the default and update `INT-CONN-001` and the README together |

## Open questions

| # | Question | Assumed default in this plan | Affects |
|---|----------|------------------------------|---------|
| Q-B | **resolved** — `INT-PERM-002` verified on PG 15 (and 18 in CI): `pg_monitor` alone insufficient, explicit `GRANT EXECUTE` required | `GRANT EXECUTE ON FUNCTION pg_control_system() TO pglens` in `deploy/sql/monitoring_user.sql` | `INT-PERM-002` passes on 15 and 18 |

## Architecture summary

Agent scrapes PostgreSQL through a registry of version- and permission-aware
`Check` implementations, buffers results on disk, and pushes a versioned JSON
envelope over HTTP. Server validates the envelope, resolves identity
(`system_identifier` → `cluster_id`, persisted UUID → `instance_id`), converts
raw counters to rates with explicit reset detection, and writes to TimescaleDB —
three typed hypertables plus one generic. Topology is reconstructed centrally
from `pg_is_in_recovery()`, `pg_stat_replication` and `pg_stat_wal_receiver`.
Everything is proven by an L3 harness that composes real topologies in Docker
and breaks them on purpose.

## Interface

| Surface | Name | Type / values | Default | Notes |
|---------|------|---------------|---------|-------|
| CLI | `pglens-agent run` | subcommand | — | reads `--config` |
| CLI | `pglens-agent check` | subcommand | — | probes a DSN, prints tier, version, enabled/disabled checks and why |
| CLI flag | `--config` | path | `/etc/ghcr.io/manprint/pglens-agent.yaml` | agent |
| CLI flag | `--identity-path` | path | `/var/lib/pglens/identity.json` | agent; **must be a persistent volume in Docker** |
| CLI flag | `--testing.no-jitter` | bool | `false` | agent; disables scheduler randomization for tests |
| CLI flag | `--testing.clock` | RFC3339 | unset | agent and server; fixed clock for deterministic tests |
| Env | `PGLENS_SERVER_URL` | URL | — | agent; overrides config |
| Env | `PGLENS_BOOTSTRAP_TOKEN` | string | — | agent; also `PGLENS_BOOTSTRAP_TOKEN_FILE` |
| Env | `HOST_PROC`, `HOST_SYS` | path | `/proc`, `/sys` | agent; container host-metrics indirection |
| Env | `PGLENS_DSN` | DSN | — | server; TimescaleDB connection |
| Env | `PGLENS_LISTEN` | `host:port` | `:8080` | server |
| HTTP | `POST /api/v1/push` | JSON envelope | — | agent ingest; `401` when the agent is revoked |
| HTTP | `GET /api/v1/clusters` | JSON | — | cluster list with role-resolved instances |
| HTTP | `GET /api/v1/instances/{id}` | JSON | — | instance detail |
| HTTP | `GET /api/v1/metrics/query` | JSON | — | `metric`, `instance_id`, `from`, `to`, `step` |
| HTTP | `GET /api/v1/events` | JSON | — | `failover_detected`, `counter_reset_detected`, … |
| HTTP | `GET /healthz`, `/readyz` | text | — | server liveness/readiness |

## Protocol and data-structure changes

| Change | Shape | Backward-compat strategy |
|--------|-------|--------------------------|
| Agent → server ingest envelope | JSON, top-level `protocol_version: 1`; `cluster_id` as decimal **string** (D18); per-instance `results[]` each carrying `check`, `ts`, `datname`, `stats_reset`, `truncated`, `error`, `metrics[]` | Server rejects unknown `protocol_version` with `400` and a body naming the minimum supported version — an incompatible agent fails loudly, never silently |
| TimescaleDB schema | 4 hypertables (`metrics`, `metrics_statements`, `metrics_ash`, `metrics_replication`) + 8 relational meta tables, all carrying `tenant_id` | Greenfield; migrations are numbered, forward-only and idempotent, applied by the server at startup |
| On-disk agent identity | `identity.json`: `{agent_id, instances: {dsn_fingerprint: instance_uuid}}`, mode `0600` | Missing file regenerates UUIDs; the server merges on `(cluster_id, addr, port)` and raises a duplicate-instance warning rather than silently splitting the series |

## Phases

| Phase | File | Primary assignment | Shippable alone? |
|-------|------|--------------------|------------------|
| 0 — Scaffolding and toolchain | [phase_01.md](phase_01.md) | `agent-3:haiku` | yes |
| 1 — Data model and pure logic | [phase_02.md](phase_02.md) | `agent-2:sonnet` | yes |
| 2 — L2 harness and real checks | [phase_03.md](phase_03.md) | `agent-2:sonnet` | yes |
| 3 — Server: schema, ingest, API | [phase_04.md](phase_04.md) | `agent-2:sonnet` | yes |
| 4 — Agent: connections, scheduler, buffer, push | [phase_05.md](phase_05.md) | `agent-2:sonnet` | yes |
| 5 — L3 E2E harness and founding scenarios | [phase_06.md](phase_06.md) | `agent-2:sonnet` | yes |
| 6 — Replication and topology engine | [phase_07.md](phase_07.md) | `agent-2:sonnet` | yes |
| 7 — ASH (Active Session History) | [phase_08.md](phase_08.md) | `agent-2:sonnet` | yes |

Live status of every phase is in `STATE.md` §11, never duplicated here.

## Reuse map (top candidates)

Greenfield repository — nothing internal to reuse, and therefore **no `path:line`
anchors exist anywhere in this plan**: every file a sub-phase names is created by
that sub-phase or by an earlier one in the same plan. Phase files compensate by
giving exact package paths, file names and symbol signatures instead. External
dependencies are chosen once here so no phase re-decides them.

| Need | Reuse | Location |
|------|-------|----------|
| PostgreSQL driver and pool | `pgx/v5`, `pgxpool` | `github.com/jackc/pgx/v5` |
| HTTP routing | `chi/v5` | `github.com/go-chi/chi/v5` |
| Typed queries on the meta schema | `sqlc` | `github.com/sqlc-dev/sqlc` (dev tool) |
| Assertions | `testify/require` | `github.com/stretchr/testify/require` |
| Ephemeral PostgreSQL in L2 | `testcontainers-go` postgres module | `github.com/testcontainers/testcontainers-go/modules/postgres` |
| Network fault injection in L3 | Toxiproxy Go client | `github.com/Shopify/toxiproxy/v2/client` |
| UUIDs | `google/uuid` | `github.com/google/uuid` |
| YAML config | `goccy/go-yaml` | `github.com/goccy/go-yaml` |
| Structured logging | `log/slog` (stdlib) | — |
| Host/OS metrics (phase 4, minimal) | `gopsutil/v4` | `github.com/shirou/gopsutil/v4` |

## References (external documentation consulted)

| # | What it settled | Source | Version / date |
|---|-----------------|--------|----------------|
| R1 | TimescaleDB 2.29 supports PG 16/17/18; PG15 support removed; PG18 supported from 2.23 | https://github.com/timescale/timescaledb/releases · https://github.com/timescale/timescaledb/issues/8233 | 2.29.0, 2026-08 |
| R2 | Avoid PG 17.1 / 16.5 / 15.9 / 14.14 / 13.17 — reverted ABI break; use 17.2+ / 16.6+ | https://www.tigerdata.com/docs/deploy/self-hosted/upgrades/upgrade-pg | 2026-08 |
| R3 | `postgres.Run(ctx, img, opts...)`; `postgres.WithWaitStrategy(...)`; `postgres.WithSnapshot()` + `container.Restore(ctx)`; `WithInitScripts` copies into `/docker-entrypoint-initdb.d` | https://github.com/testcontainers/testcontainers-go/blob/main/docs/modules/postgres.md | main, 2026-08 |
| R4 | Local toolchain: Go 1.26.1, Node 24.14.1, Docker 29.7.2, Compose v5.5.0 | local `go version` / `docker --version` | 2026-08-26 |
| R5 | `pg_control_system()` is superuser-restricted by default and **not** covered by `pg_monitor`; EXECUTE is grantable | PostgreSQL docs, system information functions | PG 15–18 · **verified** by `INT-PERM-002` (T0NoControl fails 42501, T0 succeeds) |

## Invariants

- **I-1:** `cluster_id` never changes for a cluster across failover, promote, rename, or IP change.
- **I-2:** A counter reset never produces a negative rate and never produces a false spike; the affected interval emits no point at all.
- **I-3:** The same `(series, ts)` is never written twice, whatever the buffer replay path did.
- **I-4:** Every metric row resolves to an existing `instances` row (no orphan metrics).
- **I-5:** The whole check suite runs green with permission tier T0 only; a check needing more declares it in `Requires()` and is excluded upstream, never fails at runtime.
- **I-6:** The agent never holds more than `max_connections_per_instance` connections to a monitored instance, and never blocks on a lock (`lock_timeout` set on every session).
- **I-7:** The agent never crashes and never fills the host disk when the server is unreachable; it drops oldest and counts the drops.
- **I-8:** Series cardinality per instance stays under `max_series_per_instance`; exceeding it truncates and reports, never silently drops.

## Risk register

| Risk | Mitigation |
|------|-----------|
| `postgres.Restore()` requires exclusive DB access; parallel L2 tests corrupt each other | D4 mandates serial execution inside a container; parallelism comes from the version matrix. Enforced in sub-phase 2.1 and stated in the harness doc comment |
| `pg_control_system()` grant unavailable on some managed platforms | Manual `cluster_id` fallback implemented in 1.3 and proven by `INT-IDENT-002` |
| Cardinality explosion from `pg_stat_statements` in real workloads | Top-N with hysteresis in 1.5, budget guard in 4.1, proven end-to-end by `SYS-LOAD-008` (phase 5) |
| TimescaleDB compression policy colliding with buffered backfill | D19 orders the three windows strictly; `SYS-NET-001` replays a 10-minute outage and asserts no duplicate and no gap |
| E2E suite becoming slow and flaky, and therefore ignored | Compressed intervals via config, `Eventually` instead of sleeps, zero retries at L3, `Dump()` artifacts on failure — all built in phase 5 before scenarios are written |
| The plan's own ASH design turning out not to fit the schema | D15 pulls ASH into this plan precisely so the schema is validated by its hardest consumer before later work depends on it |

## Verification summary

| Gate | Command | Where it runs |
|------|---------|---------------|
| Format | `make fmt-check` (`gofmt -l .` must be empty) | every phase |
| Lint | `make lint` (`golangci-lint run`) | every phase |
| Unit (L1) | `make test` (`go test -race -shuffle=on ./...`) | every phase |
| Coverage gate | `make coverage-gate` | from phase 1 |
| Integration (L2) | `make test-integration` (`go test -tags=integration -race ./...`) | from phase 2 |
| E2E (L3) | `make test-e2e` (`go test -tags=e2e -timeout=20m ./test/e2e/...`) | from phase 5 |

**Acceptance:** the reference scenario is proven by `SYS-REPL-001` — a real
`pg_ctl promote` on the standby, after which the API reports the roles inverted,
a `failover_detected` event exists, and the `cluster_id` is byte-identical to
the value observed before the promote (invariant I-1) — together with
`SYS-RESET-001` (restart produces no negative rate, I-2) and `SYS-NET-001`
(10-minute server outage replays with no duplicate and no gap, I-3). All three
must pass with `AGENT_MODE=container` and `AGENT_MODE=binary`.

**Run caveats:** Docker daemon required from phase 2; L3 publishes ports
8080 (server), 5432+ (PostgreSQL), 8474 (Toxiproxy) — free them before running.
L2 tests inside a single container run serially by design (D4). `make
build-images` must precede any L3 run.

## Model-assignment summary

| Phase | Sub-phases by assignment | Primary | `agent-1` review gates |
|-------|--------------------------|---------|------------------------|
| 0 | 0.1, 0.3–0.6 → `agent-3:haiku`; 0.2 → `agent-2:sonnet` | `agent-3:haiku` | 0.2 (module path, layout), 0.5 (design-record corrections) |
| 1 | 1.1–1.3 → `agent-2:sonnet`; 1.4, 1.5 → `agent-1:opus`; 1.6, 1.7 → `agent-3:haiku` | `agent-2:sonnet` | 1.4 (correctness core), 1.5 (cardinality) |
| 2 | 2.1–2.6 → `agent-2:sonnet`; 2.7 → `agent-3:haiku` | `agent-2:sonnet` | 2.1 (harness contract), 2.3 (check interface) |
| 3 | 3.1 → `agent-1:opus`; 3.2–3.6 → `agent-2:sonnet`; 3.7 → `agent-3:haiku` | `agent-2:sonnet` | 3.1 (data model), 3.3 (delta wiring) |
| 4 | 4.1–4.6 → `agent-2:sonnet`; 4.7 → `agent-3:haiku` | `agent-2:sonnet` | 4.1 (concurrency/lifecycle), 4.3 (buffer durability) |
| 5 | 5.1, 5.6 → `agent-1:opus`; 5.2–5.5, 5.7 → `agent-2:sonnet`; 5.8 → `agent-3:haiku` | `agent-2:sonnet` | 5.1 (harness contract), 5.6 (invariants) |
| 6 | 6.2 → `agent-1:opus`; 6.1, 6.3, 6.4 → `agent-2:sonnet`; 6.5 → `agent-3:haiku` | `agent-2:sonnet` | 6.2 (topology fusion), 6.4 (acceptance assertions) |
| 7 | 7.2 → `agent-1:opus`; 7.1, 7.3, 7.4 → `agent-2:sonnet`; 7.5 → `agent-3:haiku` | `agent-2:sonnet` | 7.2 (aggregation correctness), 7.5 (final docs read) |
