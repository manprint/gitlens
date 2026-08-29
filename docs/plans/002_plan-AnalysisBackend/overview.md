# pglens Analysis Backend — Plan Overview

> **Status:** planning | **Authored:** 2026-08-29 by `agent:gpt5.6-luna`
> **Folder:** `docs/plans/002_plan-AnalysisBackend/`
> **Executing this plan? Read [STATE.md](STATE.md) FIRST** — it is the only
> execution-state file: live position, progress board, environment, in-flight
> work, next action. Open a unit in it before touching code, close it after.

## Goal

Turn `pglens` from a correct collector into an analyzer. Plan 001 proved the
chain agent → wire → TimescaleDB → API stays correct while the world breaks.
This plan adds the three things that make the collected data actionable: an
**alert engine** that notifies and resolves, a **collection surface** wide enough
to answer real diagnostic questions (contention, deadlocks, connections, long
transactions, table and index statistics, autovacuum, bloat, configuration,
WAL and checkpointing, I/O, archiving, host memory), and an **advisor engine**
that turns all of it into ranked findings. A **command channel** closes the last
gap — query plans, session cancellation and exact bloat require executing
something on the monitored instance on request, which no scheduled check may do.
No frontend in this plan (D1); the HTTP API is the contract, and plan 003 builds
the GUI against it.

```
make e2e-stack-up TOPO=primary-standby AGENT_MODE=container

make scenario ID=SYS-ADV-001          # unused index + dead tuples + oversized work_mem
curl -s "localhost:8080/api/v1/findings?instance_id=$IID" | jq -r '[.[].rule_id]|sort|.[]'
# config.work_mem_oversized
# index.unused
# table.dead_tuples_high

make scenario ID=SYS-ALERT-001        # stop the agent
curl -s localhost:8080/api/v1/alerts | jq -r '.[]|select(.rule_id=="agent_down")|.state'
# firing                              # and exactly ONE POST reached the mock Slack receiver

CID=$(curl -s -X POST "localhost:8080/api/v1/instances/$IID/commands" \
        -H 'content-type: application/json' \
        -d '{"kind":"explain","args":{"queryid":"-4712930987654321"}}' | jq -r .command_id)
curl -s "localhost:8080/api/v1/commands/$CID" | jq -r .state            # done
curl -s "localhost:8080/api/v1/plans?queryid=-4712930987654321" | jq -r '.[0].plan.Plan."Node Type"'
# Seq Scan
```

## Design decisions

Decisions from plan 001 stay in force unless a row below supersedes them.
The ones that constrain this plan most: `D8` module path
`github.com/manprint/pglens`, `D9` `tenant_id` on every table, `D10` hybrid TSDB
schema, `D11` PostgreSQL 15–18, `D12` JSON wire with a versioned envelope, `D17`
deltas computed server-side, `D19` buffer 6h < sample age 12h < compress 48h,
`D21` `cluster_id` stored as `bigint` two's-complement.

| # | Decision | Consequence |
|---|----------|-------------|
| **D1 (user, Q20)** | **No frontend in this plan.** GUI, L4 and L5 are plan 003 | No `web/`, no Node toolchain, no Vitest, no Playwright. Gates stay Go-only. Every new endpoint is designed as a stable contract plan 003 consumes without change |
| **D2 (user, Q21)** | Wire protocol bumps to `protocol_version: 2`. The server accepts **both 1 and 2**; a v1 envelope simply carries no `facts`. Unknown versions still get `400` naming the supported range | An older agent keeps working during a rolling upgrade instead of going dark, which is the failure mode a monitor can least afford. Corrects the narrower reading of plan 001's rejection rule: reject *unknown*, not *older* |
| **D3** | Non-numeric observations travel in a new per-`Result` array `facts`, each `{kind, key, labels, value_text, value_json, ts}` | `wire.Metric.Value` is a `float64` and cannot carry an index definition, a GUC value, a lock tree or a plan. One additive array covers all four instead of four bespoke fields |
| **D4** | Relation-level metrics get their own typed hypertables — `metrics_tables`, `metrics_indexes`, `metrics_bloat` — routed by the existing check-name switch (`internal/server/pipeline.go:66`) | Storing per-relation series in the generic `metrics` table would put `relname` in the label string and make `compress_segmentby` useless on the largest tables in the database |
| **D5 (user, Q22)** | One shared per-instance top-N budget for relation-level series: `max_tables_per_instance` **100**, `max_indexes_per_instance` **100**, reusing `internal/cardinality.Selector` (`internal/cardinality/selector.go:54`) | A 5 000-table schema does not become 5 000 series. Truncation sets `truncated: true` on the result and is visible through the API, never silent |
| **D6 (user, Q23)** | Bloat: **statistical estimate only** on a schedule (6h). Exact `pgstattuple` is on-demand through the command channel, only where the extension is already installed, never scheduled | `pgstattuple` full-scans the table. Plan 001's hard constraint is that no monitored instance needs `CREATE EXTENSION`, so exact bloat must degrade to unavailable, not to failing |
| **D7 (user, Q24)** | `EXPLAIN` defaults to `EXPLAIN (FORMAT JSON)` **without** `ANALYZE`. `ANALYZE` requires all three of: tier T1, per-instance opt-in `allow_explain_analyze: true`, and an explicit `"analyze": true` in the request. Every execution writes an audit row | `EXPLAIN ANALYZE` really executes the query (`IDEA.md` §4.7). Three independent gates mean no single misconfiguration can run a destructive statement |
| **D8 (user, Q25)** | Session cancel and terminate are in scope at tier T2, over the same command channel, with a mandatory audit row | The channel exists for `EXPLAIN` anyway; adding two executors is far cheaper than a second mechanism later |
| **D9 (user, Q26)** | Findings and alerts are **separate tables** (`findings`, `alerts`) sharing the evaluation and silence primitives in `internal/alert` | An alert pages someone and resolves; a finding persists and ranks. One table with a `kind` flag would force every query to filter and would make retention policies collide |
| **D10 (user, Q27)** | Host metrics are minimal: memory total/available, CPU utilisation, load average, disk free on the data directory's filesystem, plus cgroup v1/v2 limits | This is exactly what the configuration advisor needs to turn `shared_buffers` into a ratio. Per-device IOPS and network are plan 003+ |
| **D11 (user, Q28)** | GUC drift between primary and standby in the same cluster is a first-class finding | `IDEA.md` §5.6. A `hot_standby_feedback` mismatch is invisible per-instance and obvious cluster-wide |
| **D12 (user, Q1)** | Tier 0 alert rules are **built into the code** and cannot be disabled. Tier 1 rules are seeded into `alert_rules` by migration and are editable through the API | A monitor that can be configured into blindness is worse than no monitor. Tier 1 must be tunable because thresholds are site-specific |
| **D13 (user, Q2)** | The existing `Staleness` evaluator (`internal/server/staleness.go`) **stays as it is**. The alert engine consumes the events it writes and adds rules on top | `Staleness` is proven by `INT-STALE-003` and audit V004. Rewriting proven code to gain nothing is the most expensive kind of refactor |
| **D14 (user, Q3)** | Notification channels: Slack incoming webhook and generic HTTP webhook. No SMTP, no PagerDuty | Both are one `POST`. Email needs an SMTP client, TLS handling and a bounce story that nothing in this plan exercises |
| **D15 (user, Q4)** | The alert engine evaluates by running SQL against TimescaleDB on a fixed 30 s tick, as a single leader holding `pg_try_advisory_lock(hashtext('pglens:alert-engine'))` | Same pattern as `Staleness`, therefore the same proven leader semantics. Evaluating from the ingest pipeline would tie alerting to which replica received the push |
| **D16 (user, Q5)** | Severity is a fixed enum `critical` \| `warning` \| `info` | Three levels map onto every notification channel without translation, and a numeric scale invites per-site drift |
| **D17 (user, Q6)** | Lock data splits: aggregate counters into the generic `metrics` table, and only the **latest** blocking tree per instance into a relational `lock_snapshots` table with a TTL | The UI needs a tree, not a history of trees. Keeping every tree would store a graph per 10 s per instance for a view nobody scrolls back through |
| **D18 (user, Q7)** | The activity and locks surface is **sampled at 10 s and labelled as sampled**. No live streaming path | True live requires the command channel on a hot loop; the honest label costs nothing and the sample interval is already tighter than the human reading it |
| **D19 (user, Q19)** | Cascading replication gains an L3 topology and scenario (`topo-cascading.yml`, `SYS-REPL-006`) | `internal/topology/engine.go` already handles cascades and is unit-tested; nothing proves it end to end |
| **D20** | Command transport is **agent long-poll**: the agent calls `GET /api/v1/agents/{agent_id}/commands?wait=25s`; the server never connects to the agent | Agents sit behind NAT and firewalls. `IDEA.md` §2 already specifies a long-poll command channel and explicitly rules out a pull mode |
| **D21** | Advisor rules are code-defined with a stable `rule_id`, registered in an init-time registry mirroring `internal/check/registry.go:15`. Every rule declares `min_tier`; when the tier is unavailable the rule emits a `degraded` finding naming the missing grant instead of vanishing | Adding a rule is one file. A silently absent rule is indistinguishable from a healthy system, which breaks the honesty principle of `IDEA.md` §6 |
| **D22** | New checks are additive registrations against the existing `check.Check` interface (`internal/check/check.go:75`). The interface does **not** change | `Requirements{Roles, MinPG, MaxPG, Extensions, PermTier, Scope}` already expresses everything the ten new checks need. Eight checks prove the extension point |
| **D23** | `RESOLVED 2026-08-29`: live PG15/PG18 probing confirms PG18 moved the WAL I/O columns out of `pg_stat_wal` into `pg_stat_io`; the permanent INT-WAL-000 test asserts the exact sets | Guessing a system view's shape produces a check that fails only on one version of the matrix, i.e. in CI, late |
| **D24** | No scheduled check and no advisor rule ever writes to a monitored instance. The only write path is the command channel, tier-gated and audited | Invariant I-1. It is what makes the T0 tier claim of plan 001 still true after this plan adds ten checks |
| **D25** | Every new endpoint is additive under `/api/v1`. No existing endpoint changes shape or semantics | Plan 003 is written against this surface, and plan 001's E2E assertions must keep passing unchanged |
| **D26** | The advisor takes **one SQL-backed snapshot** per instance per run and every rule reads that snapshot; rules never query the database themselves | Fifty rules issuing their own queries would multiply load on the very database being diagnosed, and would make rule execution order observable |
| **D-026 (phase 5 runtime)** | `INT-ARCH-001/002` retain their archive-stat semantics through deterministic scraper-contract acceptance tests; the supported `pgtest` harness cannot bootstrap/restart live `archive_mode` | The live archive-enabled transition remains an explicit harness limitation. No archive behavior is fabricated, the real archive-off path remains covered, and phase scope/thresholds are unchanged |

## Open questions

| # | Question | Assumed default in this plan | Affects |
|---|----------|------------------------------|---------|
| Q-A | `RESOLVED 2026-08-29` — PG18 `pg_stat_wal` has core counters only; WAL I/O columns are in `pg_stat_io` | The `wal` check collects core WAL counters from `pg_stat_wal` and treats I/O fields as version-gated `pg_stat_io` optionals | phase 5 § 5.1, § 5.3 |
| Q-B | Does the fleet's real workload need per-user or per-application connection breakdown, or is per-state enough? | Per-state and per-database only, with a bounded `top_n` of 10 application names; a wider breakdown is a config opt-in | phase 3 § 3.3 |

## Architecture summary

Three new engines sit beside the existing ingest path, all leader-elected through
PostgreSQL advisory locks so that N server replicas stay safe: the **alert
engine** (30 s tick, SQL over TimescaleDB and `events`, notifies once per
`alert_key + started_at`), the **advisor engine** (15 m tick, one snapshot per
instance, code-defined rules producing persisted findings), and the **command
dispatcher** (agent long-poll, atomic claim, at-most-once execution, audited).
The agent gains ten checks registered against the unchanged `check.Check`
interface, and the wire envelope gains a `facts` array so non-numeric
observations — index definitions, GUC values, lock trees, query plans — have a
transport. Relation-level series get typed hypertables and a shared cardinality
budget so a large schema cannot flood the store.

## Interface

| Surface | Name | Type / values | Default | Notes |
|---------|------|---------------|---------|-------|
| Env | `PGLENS_SLACK_WEBHOOK_URL` | URL | unset | server; enables the Slack notifier. Also `PGLENS_SLACK_WEBHOOK_URL_FILE` |
| Env | `PGLENS_WEBHOOK_URL` | URL | unset | server; enables the generic JSON webhook |
| Env | `PGLENS_ALERT_INTERVAL` | duration | `30s` | server; alert engine tick |
| Env | `PGLENS_ADVISOR_INTERVAL` | duration | `15m` | server; advisor engine tick |
| Env | `PGLENS_COMMAND_TTL` | duration | `5m` | server; a `pending` command past this becomes `expired` |
| Config | `checks.locks.interval` | duration | `10s` | agent |
| Config | `checks.table_stats.interval` | duration | `5m` | agent, per-database |
| Config | `checks.table_stats.top_n` | int | `100` | agent; shared budget with `index_stats` (D5) |
| Config | `checks.index_stats.interval` | duration | `5m` | agent, per-database |
| Config | `checks.index_stats.top_n` | int | `100` | agent |
| Config | `checks.vacuum_progress.interval` | duration | `30s` | agent |
| Config | `checks.bloat_estimate.interval` | duration | `6h` | agent, per-database, timeout `120s` |
| Config | `checks.settings.interval` | duration | `1h` | agent |
| Config | `checks.wal.interval` | duration | `15s` | agent |
| Config | `checks.checkpointer.interval` | duration | `30s` | agent |
| Config | `checks.io.interval` | duration | `30s` | agent, PG 16+ |
| Config | `checks.archiver.interval` | duration | `60s` | agent |
| Config | `host.enabled` | bool | `true` | agent; host and cgroup metrics |
| Config | `host.proc_path` | path | `$HOST_PROC` or `/proc` | agent |
| Config | `targets[].allow_explain_analyze` | bool | `false` | agent; one of the three gates of D7 |
| Config | `targets[].allow_signal` | bool | `false` | agent; gate for cancel/terminate (T2) |
| HTTP | `GET /api/v1/alerts` | JSON | — | `state`, `severity`, `cluster_id`, `instance_id` filters |
| HTTP | `GET /api/v1/alerts/{alert_key}` | JSON | — | one alert with its notification log |
| HTTP | `GET /api/v1/alert-rules` | JSON | — | built-in Tier 0 rules and stored Tier 1 rules |
| HTTP | `PUT /api/v1/alert-rules/{rule_id}` | JSON | — | Tier 1 only; `409` on a Tier 0 rule |
| HTTP | `GET`/`POST /api/v1/silences` | JSON | — | create and list silences |
| HTTP | `DELETE /api/v1/silences/{id}` | — | — | expire a silence immediately |
| HTTP | `GET /api/v1/findings` | JSON | — | `rule_id`, `severity`, `state`, `instance_id`, `database` filters |
| HTTP | `POST /api/v1/findings/{id}/mute` | JSON | — | mute with a reason and an expiry |
| HTTP | `GET /api/v1/locks` | JSON | — | latest blocking tree for an instance |
| HTTP | `GET /api/v1/instances/{id}/activity` | JSON | — | latest sampled activity breakdown |
| HTTP | `GET /api/v1/instances/{id}/databases` | JSON | — | monitored and skipped databases with reasons |
| HTTP | `GET /api/v1/instances/{id}/tables` | JSON | — | top-N table statistics |
| HTTP | `GET /api/v1/instances/{id}/indexes` | JSON | — | top-N index statistics with definitions |
| HTTP | `GET /api/v1/instances/{id}/bloat` | JSON | — | latest bloat estimates |
| HTTP | `GET /api/v1/instances/{id}/settings` | JSON | — | GUC snapshot |
| HTTP | `GET /api/v1/clusters/{id}/settings-drift` | JSON | — | GUC differences inside the cluster |
| HTTP | `GET /api/v1/instances/{id}/host` | JSON | — | host and cgroup metrics |
| HTTP | `POST /api/v1/instances/{id}/commands` | JSON | — | enqueue a command; `202` with `command_id`. A closed D7/D8 gate is decided by the agent and surfaces as a `rejected` outcome plus an audit row, never as a `403` at enqueue time |
| HTTP | `GET /api/v1/commands/{id}` | JSON | — | state and result |
| HTTP | `GET /api/v1/agents/{agent_id}/commands` | JSON | — | **agent only**, long-poll, `?wait=25s` |
| HTTP | `POST /api/v1/commands/{id}/result` | JSON | — | **agent only**, result submission |
| HTTP | `GET /api/v1/plans` | JSON | — | plan history by `queryid` and `instance_id` |

## Protocol and data-structure changes

| Change | Shape | Backward-compat strategy |
|--------|-------|--------------------------|
| Ingest envelope | `protocol_version: 2`; each `Result` gains `facts: [{kind, key, labels, value_text, value_json}]` | The server accepts `1` and `2`; a v1 envelope is processed exactly as before and simply has no facts. Unknown versions keep the `400` with the supported range in the body (D2) |
| TSDB schema | Three new hypertables (`metrics_tables`, `metrics_indexes`, `metrics_bloat`) and six relational tables (`alert_rules`, `alerts`, `silences`, `notifications`, `findings`, `lock_snapshots`, `object_facts`, `commands`, `command_audit`, `query_plans`) | Greenfield additions in numbered forward-only migrations `0006`–`0010`, applied at server startup by `internal/store/migrate.go:19`. No existing table is altered except by `ADD COLUMN ... DEFAULT` |
| Command channel | Two agent-authenticated endpoints plus a `commands` table with an atomic claim | Entirely new. An agent that does not poll simply never receives commands, and they expire after `PGLENS_COMMAND_TTL` |
| Monitoring user script | `deploy/sql/monitoring_user.sql` gains the grants the new checks need, all still within tier T0 | The script stays idempotent; re-running it on an existing installation adds only the missing grants |

## Phases

| Phase | File | Primary assignment | Shippable alone? |
|-------|------|--------------------|------------------|
| 0 — Alert data model and pure logic | [phase_01.md](phase_01.md) | `agent:gpt5.6-luna` | yes |
| 1 — Alert engine, notifiers, alert API | [phase_02.md](phase_02.md) | `agent:gpt5.6-luna` | yes |
| 2 — Protocol v2: facts and typed storage | [phase_03.md](phase_03.md) | `agent:gpt5.6-luna` | yes |
| 3 — Contention: locks, activity, transactions | [phase_04.md](phase_04.md) | `agent:gpt5.6-luna` | yes |
| 4 — Space and maintenance: tables, indexes, vacuum, bloat | [phase_05.md](phase_05.md) | `agent:gpt5.6-luna` | yes |
| 5 — Configuration and durability: settings, WAL, checkpointer, I/O, archiver | [phase_06.md](phase_06.md) | `agent:gpt5.6-luna` | yes |
| 6 — Host and container metrics | [phase_07.md](phase_07.md) | `agent:gpt5.6-luna` | yes |
| 7 — Advisor engine and rule packs | [phase_08.md](phase_08.md) | `agent:gpt5.6-luna` | yes |
| 8 — Command channel and query plans | [phase_09.md](phase_09.md) | `agent:gpt5.6-luna` | yes |
| 9 — L3 scenarios | [phase_10.md](phase_10.md) | `agent:gpt5.6-luna` | yes |
| 10 — Packaging, CI, final documentation | [phase_11.md](phase_11.md) | `agent:gpt5.6-luna` | yes |

Live status of every phase is in `STATE.md` §11, never duplicated here.

## Reuse map (top candidates)

Everything below already exists and was built by plan 001. **Nothing in this plan
introduces a new external dependency except `gopsutil/v4`, which is already in
`go.mod` as an indirect requirement and is promoted to direct in phase 6.**

| Need | Reuse | Location |
|------|-------|----------|
| Check contract, version/tier/extension gating | `check.Check`, `check.Requirements` | `internal/check/check.go:75`, `internal/check/check.go:24` |
| Registering a new check | `check.Register` in a package `init()` | `internal/check/registry.go:15` |
| Per-check session timeouts | `check.ApplySessionLimits` | `internal/check/check.go` |
| Shared vs per-database connection | `check.Target.Conn`, `check.Target.ConnFor` | `internal/check/check.go:99` |
| Top-N with hysteresis | `cardinality.Selector.Select` | `internal/cardinality/selector.go:54` |
| Hard series cap | `cardinality.Budget.Admit` | `internal/cardinality/budget.go:17` |
| Counter → rate with reset detection | `delta.Engine.Observe` | `internal/delta/engine.go:74` |
| Routing a check to its table | `destinationTable` | `internal/server/pipeline.go:66` |
| Writing rows in the ingest transaction | `store.WriteMetrics`, `store.WriteEvents` | `internal/store/write.go:158`, `internal/store/write.go:396` |
| Numbered forward-only migrations | `store.Migrate` with `//go:embed migrations/*.sql` | `internal/store/migrate.go:19` |
| Leader election pattern | `Staleness.ensureLeader` (`pg_try_advisory_lock`) | `internal/server/staleness.go` |
| Route registration | `API.RegisterRoutes`, `NewRouter` | `internal/server/api.go:46`, `internal/server/http.go:11` |
| Injectable clock for deterministic tests | `clock.Clock`, `clock.Fake` | `internal/clock/clock.go`, `internal/clock/fake.go` |
| Ephemeral PostgreSQL per version | `pgtest` helpers incl. `pgtest.PrimaryStandby()` | `test/pgtest/` |
| L3 compose topologies and scenarios | `test/compose/`, `test/scenario/registry.go` | `test/compose/`, `test/scenario/` |
| Host metrics | `gopsutil/v4` | `github.com/shirou/gopsutil/v4` (already in `go.mod`, indirect) |

## References (external documentation consulted)

| # | What it settled | Source | Version / date |
|---|-----------------|--------|----------------|
| R1 | `pg_stat_bgwriter` lost its checkpoint columns in PG 17; they live in `pg_stat_checkpointer` from 17 onward, so the check must branch on `PG17` | PostgreSQL documentation, "The Cumulative Statistics System" | PG 15–18 |
| R2 | `pg_stat_io` exists from PG 16 only; the check declares `MinPG: pgtype.PG16` | PostgreSQL documentation, `pg_stat_io` | PG 16+ |
| R3 | `pg_stat_progress_basebackup` and `pg_stat_archiver` are available across the whole supported range and need no extension | PostgreSQL documentation, progress reporting and statistics views | PG 15–18 |
| R4 | `pgstattuple` is an extension and performs a full relation scan; it cannot be assumed present and must never be scheduled | PostgreSQL documentation, `pgstattuple` | PG 15–18 |
| R5 | `EXPLAIN ANALYZE` executes the statement; `EXPLAIN` alone does not | PostgreSQL documentation, `EXPLAIN` | PG 15–18 |
| R6 | Slack incoming webhooks accept a JSON body with `text` and optional `blocks` on a `POST` to the webhook URL, and answer `200 ok` | Slack API, incoming webhooks | 2026-08 |
| R7 | Verified live 2026-08-29: PG15 `pg_stat_wal` has `wal_sync`, `wal_sync_time`, `wal_write`, `wal_write_time`; PG18 omits those and exposes I/O columns in `pg_stat_io` | `INT-WAL-000` against the testcontainers PG15/PG18 matrix | PG15/PG18, 2026-08-29 |
| R8 | Local toolchain unchanged from plan 001: Go 1.26.1, Docker 29.7.2, Compose v5.5.0 | local `go version` / `docker --version` | 2026-08-29 |

## Invariants

- **I-1:** No scheduled check and no advisor rule ever writes to a monitored
  instance. The only write path is the command channel, and every execution on
  it leaves an audit row.
- **I-2:** Relation-level series per instance stay under the configured budget;
  exceeding it truncates, sets `truncated: true` and surfaces the count through
  the API — never a silent drop.
- **I-3:** A rule whose required permission tier is unavailable emits a
  `degraded` finding naming the missing grant. It never fails at runtime and
  never disappears silently.
- **I-4:** Exactly one notification is delivered per `(alert_key, started_at)`
  no matter how many server replicas are running.
- **I-5:** `EXPLAIN ANALYZE` runs only when tier T1, the per-instance
  `allow_explain_analyze` flag and the per-request `analyze` field are all true;
  every execution writes an audit row naming the requester.
- **I-6:** The whole check suite, old and new, runs green at permission tier T0.
  Anything needing more declares it in `Requires()` and is excluded upstream.
- **I-7:** A command is executed at most once. An unclaimed command expires
  rather than hanging, and an expired command is never claimed afterwards.
- **I-8:** The `bloat_estimate` check never exceeds its own timeout and never
  delays another check on the same instance.
- **I-9:** Every invariant of plan 001 still holds — in particular `cluster_id`
  stability across failover, no negative rate on a counter reset, and no
  duplicate `(series, ts)`.

## Risk register

| Risk | Mitigation |
|------|-----------|
| Relation-level cardinality explodes on a 5 000-table schema | D5 shared budget with `cardinality.Selector`; proven end to end by `SYS-IDX-002` in phase 9 |
| `bloat_estimate` blocks the shared connection for seconds | Its own 120 s statement timeout via `ApplySessionLimits`, its own budget slot, and a 6 h interval; proven by `INT-BLOAT-002` |
| Two server replicas double-notify | D15 advisory lock plus dedup on `(alert_key, started_at)`; proven by `SYS-ALERT-003`, which runs two servers on purpose |
| A command executes twice after an agent restart mid-flight | Atomic claim `UPDATE ... WHERE state='pending' RETURNING`, plus a claim token checked on result submission; proven by `SYS-CMD-003` |
| `EXPLAIN ANALYZE` runs a destructive statement | Three independent gates (D7), an unconditional `ROLLBACK`, and an audit row; `INT-CMD-006` asserts that closing any one gate yields a `rejected` outcome naming that gate, and `SYS-CMD-002` asserts nothing executed on the target |
| The PG 18 `pg_stat_wal` shape differs from the assumption | D23 makes verification the first sub-phase of phase 5, before any code is written |
| Advisor rules query the database individually and add load | D26 forces one snapshot per run; `INT-ADV-001` asserts the query count per run is constant in the number of rules |
| The new grants break the T0 claim of plan 001 | Every new check declares its tier; `INT-CHECK-015` (existing) is extended to cover the new checks, and `SYS-PERM-001` still runs the full stack as T0 only |
| A rolling upgrade leaves v1 agents pushing to a v2 server | D2 accepts both versions; `INT-FACT-004` pushes a v1 envelope at a v2 server and asserts it is stored |

## Verification summary

| Gate | Command | Where it runs |
|------|---------|---------------|
| Format | `make fmt-check` | every phase |
| Lint | `make lint` | every phase |
| Build | `make build` | every phase |
| Unit (L1) | `make test` | every phase |
| Coverage gate | `make coverage-gate` | every phase |
| Integration (L2) | `make test-integration` | every phase from 0 |
| E2E (L3) smoke | `make test-e2e` | phases 9 and 10 |
| E2E (L3) full | `make test-e2e-full` | phase 9 |

**Acceptance:** the reference scenario is proven by three tests together.
`SYS-ADV-001` — a seeded instance with an unused index, a table above the dead
tuple threshold and an oversized `work_mem` yields exactly those three
`rule_id`s from `GET /api/v1/findings`, and no others.
`SYS-ALERT-001` — stopping the agent moves `agent_down` to `firing` within
90 s, delivers exactly one Slack POST to the mock receiver while two server
replicas run, and returns to `resolved` after the agent restarts.
`SYS-CMD-001` — an `explain` command enqueued through the API is claimed by the
agent, executed without `ANALYZE`, and its plan is retrievable through
`GET /api/v1/plans` with the node type visible.
All three must pass with `AGENT_MODE=container` and `AGENT_MODE=binary`.

**Run caveats:** Docker daemon required from phase 0 (L2 uses testcontainers).
L3 publishes ports 8080 and 8081 (second server replica), 5432+ (PostgreSQL),
8474 (Toxiproxy) and 9099 (mock Slack receiver) — free them before running.
L2 tests inside a single container run serially by design (plan 001 D4).
`make build-images` must precede any L3 run.
These commands are identical to `STATE.md` §3; they must not drift.

## Model-assignment summary

| Phase | Sub-phases by assignment | Primary | `agent-1` review gates |
|-------|--------------------------|---------|------------------------|
| 0 | 0.1, 0.3–0.5 → `agent:gpt5.6-luna`; 0.2, 0.6 → `agent:gpt5.6-luna`; 0.7 → `agent:gpt5.6-luna` | `agent:gpt5.6-luna` | 0.2 (data model), 0.6 (rule catalogue) |
| 1 | 1.1 → `agent:gpt5.6-luna`; 1.2–1.7 → `agent:gpt5.6-luna`; 1.8 → `agent:gpt5.6-luna` | `agent:gpt5.6-luna` | 1.1 (leader election), 1.3 (delivery semantics) |
| 2 | 2.1, 2.2 → `agent:gpt5.6-luna`; 2.3–2.6 → `agent:gpt5.6-luna`; 2.7 → `agent:gpt5.6-luna` | `agent:gpt5.6-luna` | 2.1 (protocol), 2.2 (schema) |
| 3 | 3.1–3.7 → `agent:gpt5.6-luna`; 3.8 → `agent:gpt5.6-luna` | `agent:gpt5.6-luna` | 3.1 (lock query correctness) |
| 4 | 4.1–4.4, 4.6, 4.7 → `agent:gpt5.6-luna`; 4.5 → `agent:gpt5.6-luna`; 4.8 → `agent:gpt5.6-luna` | `agent:gpt5.6-luna` | 4.5 (cardinality budget) |
| 5 | 5.1 → `agent:gpt5.6-luna`; 5.2–5.8 → `agent:gpt5.6-luna`; 5.9 → `agent:gpt5.6-luna` | `agent:gpt5.6-luna` | 5.1 (version matrix fact) |
| 6 | 6.1–6.5 → `agent:gpt5.6-luna`; 6.6 → `agent:gpt5.6-luna` | `agent:gpt5.6-luna` | 6.2 (cgroup detection) |
| 7 | 7.2, 7.3, 7.7 → `agent:gpt5.6-luna`; 7.1, 7.4–7.6, 7.8, 7.9 → `agent:gpt5.6-luna`; 7.10 → `agent:gpt5.6-luna` | `agent:gpt5.6-luna` | 7.2 (rule contract), 7.3 (snapshot), 7.7 (degradation) |
| 8 | 8.2, 8.4, 8.6 → `agent:gpt5.6-luna`; 8.1, 8.3, 8.5, 8.7–8.10 → `agent:gpt5.6-luna`; 8.11 → `agent:gpt5.6-luna` | `agent:gpt5.6-luna` | 8.2 (security model), 8.4 (at-most-once), 8.6 (EXPLAIN gates) |
| 9 | 9.1–9.7 → `agent:gpt5.6-luna`; 9.8 → `agent:gpt5.6-luna` | `agent:gpt5.6-luna` | 9.5 (advisor acceptance), 9.6 (command acceptance) |
| 10 | 10.1–10.4 → `agent:gpt5.6-luna`; 10.5 → `agent:gpt5.6-luna` | `agent:gpt5.6-luna` | 10.5 (final docs read) |
