# Phase 5 — Rollups for long retention

Intent: hourly rollups keep statements, ASH and generic metrics for up to five years, and charts read them for ranges older than the raw data.
Prerequisites: P2 (the ASH columns exist).
Phase closure: P5; review by agent-1.

## State and ownership contract
Read STATE.md §1 first (and PROTOCOL.md once without this skill) for scope, ownership,
recovery, checks, and commit rules. Open before edits; checkpoint at a recovery boundary;
close with evidence. A delegated worker follows its assigned OPEN unit. Missing design goes
to agent-1, the supervisor. Executor power: low.

## Local design context
Plan revision: 1.
- D8: three continuous aggregates with bucket 1 hour. Default retention 730 days, maximum 1825, minimum 31. Raw retention rules do not change (`retentionMaxDays = 365`).
- R7: `CREATE MATERIALIZED VIEW … WITH (timescaledb.continuous) … WITH NO DATA`, `add_continuous_aggregate_policy` and `add_retention_policy` work inside the migration transaction on TimescaleDB 2.31. The migration does not call `refresh_continuous_aggregate` (it cannot run in a transaction); the policy fills the view. Refresh policy: `start_offset => INTERVAL '3 days'`, `end_offset => INTERVAL '1 hour'`, `schedule_interval => INTERVAL '30 minutes'`.
- Before writing the views, read the column lists of `metrics` (`0002_hypertables.sql`), `metrics_statements` and any later `ALTER` of these tables in `internal/store/migrations/` (`grep -n 'metrics_statements\|ALTER TABLE metrics ' internal/store/migrations/*.sql`).
- View definitions (migration 0034):
  - `metrics_statements_1h`: `time_bucket('1 hour', ts) AS bucket, tenant_id, cluster_id, instance_id, datname, queryid`, and for every rate column `c` of `metrics_statements`: `avg(c) AS c`. Plus `count(*) AS samples`.
  - `metrics_ash_1h`: `bucket, tenant_id, cluster_id, instance_id, datname, wait_event_type, wait_event, queryid, sum(samples) AS samples, sum(window_ticks) AS window_ticks, sum(window_seconds) AS window_seconds`. Use the real column names of `metrics_ash` (after 0004); user, client and application are not kept in the rollup.
  - `metrics_1h`: `bucket`, the series key columns of `metrics`, `avg(value) AS value_avg, max(value) AS value_max, min(value) AS value_min, count(*) AS samples`.
  - Each view: `WITH (timescaledb.continuous, timescaledb.materialized_only = true)`, then the two policies with `if_not_exists => true`, retention `INTERVAL '730 days'`. Guard each `CREATE MATERIALIZED VIEW` with `IF NOT EXISTS`.
- Rollup read rule (unit 5.3): for a request range `[from, to]`, let `horizon = now − raw retention of that family` (read the live policy as `retention.go` does; fall back to the family default). If `from < horizon + 1 hour`, the whole request is answered from the rollup and the step is at least 1 hour; otherwise nothing changes. The response gains `"resolution": "raw" | "1h"`.

## Sub-phases

### 5.1 Migration: three continuous aggregates
- **Agent:** agent-2
- **Assignment:** implement; agent-1 reviews the SQL before it is run and the diff after.
- **Files:** NEW `internal/store/migrations/0034_rollups.sql` (D19), `internal/store/rollups_integration_test.go`.
- **Change:**
  Preconditions: P2 DONE.
  Contract: after migration the three views exist, each with one refresh policy and one retention policy; running the migrations again is a no-op.
  Steps: 1. S1 — read the source table columns and write the three definitions; send the SQL to agent-1. 2. S2 — after review, add the integration tests.
  Recovery boundary: after S1 review.
  Failure handling: a column needed by a definition does not exist → ask agent-1; do not guess a name.
- **Unit tests:** N/A — SQL only; covered by the integration tests.
- **e2e tests:** `TestINTRLP001`: `timescaledb_information.continuous_aggregates` lists the three views; `timescaledb_information.jobs` has `policy_refresh_continuous_aggregate` and `policy_retention` for each. `TestINTRLP002`: insert 3 hours of `metrics_statements` rows with a constant rate 6, `CALL refresh_continuous_aggregate('metrics_statements_1h', NULL, now())`, expect buckets with `calls_rate = 6`. `TestINTRLP003`: same for `metrics_ash_1h` — sum of `samples` equals the raw sum. Gate G-GO-INT `./internal/store/`.
- **Done:** tests pass; SQL reviewed; unit closed and committed.

### 5.2 Retention API covers the rollups
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** WRITE `internal/server/retention.go` (`retentionFamily`, `retentionFamilies`, `retentionMaxDays` use, `minRetentionDays`, `checkRetentionDays`, `pgPolicyStore`), `internal/server/retention*_test.go`, `api/openapi.yaml`, G-API outputs.
- **Change:**
  Preconditions: 5.1 DONE.
  Contract: `retentionFamily` gains `MaxDays int` and `MinDays int` (zero → the current global constants) and `Rollup bool`. Three families are appended: `metrics_1h`, `metrics_statements_1h`, `metrics_ash_1h` with `DefaultDays: 730`, `MaxDays: 1825`, `MinDays: 31`, `Rollup: true`. For a rollup family the policy lives on the materialization hypertable: resolve it with
  `SELECT materialization_hypertable_schema, materialization_hypertable_name FROM timescaledb_information.continuous_aggregates WHERE view_name = $1`, and pass the view name to `remove_retention_policy` / `add_retention_policy` (they accept the continuous aggregate name). The API row gains `"rollup": true|false` and `"max_days"`.
  Steps: 1. S1 — struct, list, per-family bounds. 2. S2 — policy store for rollups. 3. S3 — OpenAPI, G-API.
- **Unit tests:** `TestRLP001` (list has 10 families, the last three with `rollup: true`, `max_days: 1825`), `TestRLP002` (set 1826 days on a rollup → 400; 366 on a raw family → 400 as before; 1000 on a rollup → accepted by the fake store), `TestRLP003` (30 days on a rollup → 400 with the minimum in the text). Gate G-GO-FOCUS.
- **e2e tests:** `TestINTRLP004` (`./internal/server/`): `PUT` retention 400 days on `metrics_ash_1h`, then `GET` shows 400; the raw `metrics_ash` policy is unchanged. Gate G-GO-INT.
- **Done:** tests pass; existing `TestRET*` green; reviewed; unit closed and committed.

### 5.3 Read path uses rollups for old ranges
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** READ `internal/server/workload_series.go` (`workloadSQL`, `parseWorkloadQuery`), `internal/server/api.go` (`handleStatements`), `internal/server/api_ash.go`; NEW `internal/server/rollup_route.go`, `rollup_route_test.go`; WRITE the three handlers' SQL builders, their tests, `api/openapi.yaml` (`resolution` field), G-API outputs.
- **Change:**
  Preconditions: 5.2 DONE.
  Contract: `func rollupFor(family string, from, now time.Time, rawDays int) bool` implements the rule in Local design context. When true: `handleStatements` reads `metrics_statements_1h` (`bucket` as `ts`), `/api/v1/ash` and `/ash/top` read `metrics_ash_1h` and reject `group_by`/filters on `state`, `usename`, `client_addr`, `application_name`, `service`, `endpoint` with 400 `this range is served from hourly rollups, which do not keep <name>`; workload series read `metrics_1h.value_avg`; a requested step under 1 hour is raised to 1 hour. Table names come from a fixed map.
  Steps: 1. S1 — `rollupFor` and tests. 2. S2 — statements. 3. S3 — ASH. 4. S4 — workload series. 5. S5 — OpenAPI, G-API.
  Recovery boundary: after each of S2, S3, S4 (each leaves the build green).
- **Unit tests:** `TestRLP004` (`rollupFor` boundaries: `from` exactly at horizon + 1 h → false; one second earlier → true), `TestRLP005` (statements SQL names `metrics_statements_1h` for an old range and `metrics_statements` for a recent one), `TestRLP006` (ASH old range with `group_by=usename` → 400 with the exact text), `TestRLP007` (workload step 5 m on an old range becomes 1 h and `resolution` is `1h`). Gate G-GO-FOCUS.
- **e2e tests:** `TestINTRLP005`: raw rows only inside the last day, rollup rows inserted directly into the materialization for 60 days ago (insert raw rows with old `ts`, refresh the aggregate, then delete the raw rows with `drop_chunks`); a request for that old day returns data with `resolution: "1h"`. Gate G-GO-INT.
- **Done:** tests pass; reviewed; unit closed and committed.

### 5.4 Console: retention panel, resolution note, help, README
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** WRITE `web/src/lib/retention.ts`, `web/src/features/settings/RetentionPanel.tsx`, the chart caption components that show range information on Queries, Activity and Instance pages (find with `grep -rn 'resolution\|step' web/src/features --include='*.tsx' -l`), `web/src/i18n/messages/{en,it}/{settings,ash,queries,instance}.ts`, help topic on retention (`en`, `it`), tests, `README.md`.
- **Change:**
  Preconditions: 5.3 DONE.
  Contract: the retention panel shows two groups, `Raw data` and `Hourly rollups`, each row with its own bounds from the API (`max_days`). When a response has `resolution: "1h"` the page shows the note `Hourly averages (older than raw retention)` / `Medie orarie (oltre la retention dei dati grezzi)`; controls for dimensions the rollup does not keep are disabled with a tooltip.
  Steps: 1. S1 — panel. 2. S2 — note and disabled controls. 3. S3 — help and README section "Retention and rollups" (what is kept, defaults, limits).
- **Unit tests:** `UI-RLP-001` (two groups, bounds 365 and 1825), `UI-RLP-002` (1826 → validation text), `UI-RLP-003` (note renders for `resolution: "1h"` in `en` and `it`), `UI-RLP-004` (group-by `User` disabled on a rollup response). Gates G-WEB-STATIC, G-WEB-UNIT.
- **e2e tests:** N/A at unit level; lab check in 15.2.
- **Done:** tests pass; README and help updated; reviewed; unit closed and committed.

## Phase gates and closure
- Required gates: G-GO-FULL, G-GO-INT (`TestINTRLP00[1-5]`), G-API, G-WEB-FULL, G-I18N; assertions: all existing `TestRET*` and `TestWSER*` green.
- README obligation: unit 5.4.
- Every sub-phase DONE or explicitly justified SKIPPED. Run full phase gates now.
- agent-1 reviews migration, read rule, bounds, documentation.
- Open closure unit P5, record review and result, mark the phase DONE, close, commit `phase(007/P5): PHASE 5 COMPLETE — Rollups; gates and review passed`.
- P5 edits no production code or tests. Blocked gate: P5 stays OPEN and BLOCKED. Failing gate or rejected review: attempt failed; reopen the responsible sub-phase, then rerun P5.
