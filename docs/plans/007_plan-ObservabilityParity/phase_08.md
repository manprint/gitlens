# Phase 7 — Automatic plans from `auto_explain`

Intent: plans that PostgreSQL logs through `auto_explain` appear in the plan history with their duration, without anyone asking for an EXPLAIN.
Prerequisites: P6.
Phase closure: P7; review by agent-1.

## State and ownership contract
Read STATE.md §1 first (and PROTOCOL.md once without this skill) for scope, ownership,
recovery, checks, and commit rules. Open before edits; checkpoint at a recovery boundary;
close with evidence. A delegated worker follows its assigned OPEN unit. Missing design goes
to agent-1, the supervisor. Executor power: low.

## Local design context
Plan revision: 1.
- D10, I-1, I-4. The agent changes no server setting; the operator enables `auto_explain`.
- R4: the log line's message is `duration: <ms> ms  plan:\n` (two spaces before `plan:`) followed by one JSON object with keys `Query Text`, `Plan`, and optionally `Query Parameters`, `Planning Time`, `Execution Time`, `Triggers`, `JIT`. The line carries `query_id` when `compute_query_id` is on.
- Stored plan shape: `query_plans.plan` holds what `EXPLAIN (FORMAT JSON)` returns, an array with one object. The agent builds the same shape: `[{"Plan": <Plan>, …the other top-level keys except "Query Text" and "Query Parameters"…}]`. `Query Text` is never shipped (the statement text comes from `query_texts` by queryid).
- `analyzed` is true when the root `Plan` object has the key `Actual Total Time`.
- Hash: the existing `planHash` in `internal/agent/exec_explain.go`, exposed as `agent.PlanHash(plan json.RawMessage) (string, error)` (one-line wrapper; no change to `planHash`).
- Fact: kind `plan` (exists in `internal/wire/factkinds.go`), `Key` = `<queryid>:<plan_hash>:<ts unix nanoseconds>`, labels `source=auto_explain`, `ts`, `datname`, `queryid`, `plan_hash`, `analyzed` (`true`|`false`), `duration_ms`; `ValueJSON` = `{"plan": <array>}` or `{}` when the body was already shipped. The loop remembers the `(datname, queryid, plan_hash)` it shipped with a body during the last hour (map with times, at most 5000 entries) and ships later samples without the body.
- Limits per scrape: at most 50 plan facts; a plan body over 262 144 bytes is not shipped (the sample is, without body); a sample whose line has no `query_id` is counted and dropped. Metrics: `pg_log_plans` (samples read), `pg_log_plans_dropped{reason}` with reasons `no_queryid`, `budget`, `credential`, `format`.
- Credential rule (I-4): if `redact.MayHoldCredential` is true for `Query Text`, for the plan body, or for `Query Parameters`, the whole sample is dropped (reason `credential`). The server repeats the check on what it receives.
- Today the server pipeline skips `plan` facts (`internal/server/pipeline.go` near line 531: `if f.Kind == "plan" { continue }`); plans arrive only through command results (`persistCommandArtifact` in `internal/server/commands.go`). This phase makes the pipeline store `plan` facts whose label `source` is `auto_explain`; any other `plan` fact is still skipped.
- Migration 0036:
  ```sql
  ALTER TABLE query_plans ADD COLUMN IF NOT EXISTS source text NOT NULL DEFAULT 'command';
  CREATE TABLE IF NOT EXISTS plan_samples (
    ts          timestamptz      NOT NULL,
    tenant_id   text             NOT NULL DEFAULT 'default',
    instance_id uuid             NOT NULL,
    cluster_id  bigint           NOT NULL,
    datname     text             NOT NULL,
    queryid     bigint           NOT NULL,
    plan_hash   text             NOT NULL,
    duration_ms double precision NOT NULL,
    analyzed    boolean          NOT NULL DEFAULT false,
    parameters  text,
    trace_id    text
  );
  SELECT create_hypertable('plan_samples', by_range('ts', INTERVAL '1 day'), if_not_exists => TRUE);
  CREATE UNIQUE INDEX IF NOT EXISTS plan_samples_dedup_idx
    ON plan_samples (instance_id, datname, queryid, plan_hash, ts);
  CREATE INDEX IF NOT EXISTS plan_samples_lookup_idx
    ON plan_samples (tenant_id, instance_id, queryid, ts DESC);
  SELECT add_retention_policy('plan_samples', INTERVAL '30 days', if_not_exists => TRUE);
  ```
  (`trace_id` is filled from phase 11; `parameters` from unit 7.3.)

## Sub-phases

### 7.1 Agent: recognise and ship automatic plans
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** NEW `internal/pglog/autoexplain.go`, `autoexplain_test.go`, `internal/pglog/testdata/autoexplain_*.txt`; WRITE `internal/agent/exec_explain.go` (add `PlanHash`), `cmd/pglens-agent/logs.go` and tests.
- **Change:**
  Preconditions: P6 DONE.
  Contract: `pglog.ParseAutoExplain(message string) (AutoPlan, error)` with `AutoPlan{DurationMS float64; Plan json.RawMessage /* normalized array */; QueryText string; Parameters string; Analyzed bool}`; errors `ErrNotAutoExplain` (no `duration: … plan:` prefix) and `ErrPlanFormat` (prefix present, body is not a JSON object — text, XML or YAML format). The loop handles class `autoexplain.plan` entries per Local design context and adds the coverage note `auto_explain_format_not_json` when it sees `ErrPlanFormat`.
  Steps: 1. S1 — fixtures: the R4 message and one with `log_analyze` (create them on `postgres:18.6-alpine` started with `-c shared_preload_libraries=auto_explain -c auto_explain.log_min_duration=0 -c auto_explain.log_format=json -c auto_explain.log_analyze=on -c compute_query_id=on -c logging_collector=on -c log_destination=jsonlog`; copy the `message` values; remove the container), plus one text-format message. 2. S2 — parser. 3. S3 — `PlanHash`. 4. S4 — loop: facts, the shipped-body map, limits, metrics.
- **Unit tests:** `TestAEX001` (fixture → duration, normalized array with `Plan`, no `Query Text` key inside), `TestAEX002` (`Analyzed` true only for the analyze fixture), `TestAEX003` (same plan twice in one hour: first fact has a body, second has `{}`; both have `duration_ms`), `TestAEX004` (51 samples → 50 facts and `pg_log_plans_dropped{reason="budget"}` = 1; entry without `QueryID` → reason `no_queryid`), `TestAEX005` (text-format message → `ErrPlanFormat`, note `auto_explain_format_not_json`, reason `format`), `TestAEX006` (a `Query Text` with `PASSWORD 'x'` → no fact, reason `credential`, the secret is nowhere in the result; guards I-4). Gate G-GO-FOCUS `./internal/pglog/ ./internal/agent/ ./cmd/pglens-agent/`.
- **e2e tests:** `TestINTAEX001` (`./cmd/pglens-agent/`): the container of S1 with its log directory mounted; after `SELECT count(*) FROM pg_class` the loop yields one `plan` fact with `source=auto_explain` whose `plan_hash` equals `agent.PlanHash` of its body. Gate G-GO-INT.
- **Done:** tests pass; G-RO passes; reviewed; unit closed and committed.

### 7.2 Server: store plans and samples
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** NEW `internal/store/migrations/0036_plan_samples.sql` (D19); WRITE `internal/store/write.go` (`QueryPlanRow.Source`, `WriteQueryPlan`, NEW `PlanSampleRow`, `WritePlanSamples`), `internal/server/pipeline.go` (the `plan` case), `internal/server/commands.go` (`persistCommandArtifact` passes `Source: "command"`), `internal/server/retention.go` (family `plan_samples`, default 30), tests.
- **Change:**
  Preconditions: 7.1 DONE.
  Contract: for a `plan` fact with `source=auto_explain`: validate labels (`queryid` int64, `plan_hash` non-empty, `duration_ms` ≥ 0, `ts` valid; else `IncIngestRejected("invalid_fact")`); when `ValueJSON.plan` is present and `redact.MayHoldCredential` is false for it, insert into `query_plans` with `source = 'auto_explain'`, `captured_at = ts`, same conflict rule as today (the first stored plan wins; `source` of an existing row is not changed); always insert one `plan_samples` row with `ON CONFLICT DO NOTHING`. A plan body that may hold a credential drops the whole fact.
  Steps: 1. S1 — migration. 2. S2 — store functions. 3. S3 — pipeline. 4. S4 — retention family.
- **Unit tests:** `TestAEX007` (fact with body → plan row and sample row; fact without body → sample row only), `TestAEX008` (bad `duration_ms` → rejected), `TestAEX009` (a `plan` fact without `source` is skipped as before). Gate G-GO-FOCUS.
- **e2e tests:** `TestINTAEX002` (`./internal/server/`): two pushes of the same plan with different `ts` → one `query_plans` row with `source = 'auto_explain'`, two `plan_samples` rows. `TestINTAEX003`: a pushed body containing `PASSWORD 'x'` stores no plan row and no sample row (guards I-4). Gate G-GO-INT.
- **Done:** tests pass; `/api/v1/plans` unchanged for command plans; reviewed; unit closed and committed.

### 7.3 Parameters: opt-in, redaction, purge
- **Agent:** agent-2
- **Assignment:** implement; agent-1 reviews the design note before code and the diff after.
- **Files:** WRITE `cmd/pglens-agent/logs.go` and tests, `internal/server/pipeline.go`, `internal/store/write.go`, NEW `internal/server/api_plan_samples.go` (purge handler only in this unit) and tests, `api/openapi.yaml`, G-API outputs.
- **Change:**
  Preconditions: 7.2 DONE.
  Contract: with `checks.logs.plan_parameters: true` the fact's `ValueJSON` gains `"parameters": "<Query Parameters string cut to 4096 bytes>"`; with false (default) the key is absent even if the log has parameters. The server stores it in `plan_samples.parameters` after its own credential check (a hit stores NULL, the sample is kept). `DELETE /api/v1/plans/samples/parameters?instance_id=` sets `parameters = NULL` on every sample of the instance and answers `{"cleared": <rows>}`; it requires the same credential as other write endpoints and writes one line to the command audit table with kind `purge_plan_parameters` if that table accepts free kinds (it has no CHECK on `kind`), else a server log line without values.
  Steps: 1. S1 — design note: where parameters can leak (agent log lines, error texts, API responses, audit) and how each is closed; agent-1 approves. 2. S2 — agent. 3. S3 — server store. 4. S4 — purge endpoint.
- **Unit tests:** `TestAEX010` (default off: no `parameters` key), `TestAEX011` (on: present and cut to 4096), `TestAEX012` (parameters `$1 = 'secret-token-…'` judged a credential → agent drops the sample; server stores NULL when the agent did not), `TestAEX013` (purge answers the row count; no parameter value in the response or log). Gate G-GO-FOCUS.
- **e2e tests:** `TestINTAEX004` (`./internal/server/`): two samples with parameters, purge, both rows have NULL parameters and still exist. Gate G-GO-INT.
- **Done:** tests pass; design note in the unit record; reviewed; unit closed and committed.

### 7.4 API, console, readiness, documents
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** WRITE `internal/server/api_plan_samples.go`, `internal/server/api_plans.go` (rows gain `source`), tests, `api/openapi.yaml`, G-API outputs, `web/src/features/queries/PlanHistory.tsx` and test, NEW `web/src/features/queries/PlanSamples.tsx` and test, `web/src/api/queries.ts`, `web/src/i18n/messages/{en,it}/queries.ts`, `web/src/lib/help/topics/{en,it}/query-performance.ts`, `internal/readiness/rules.go`, `internal/i18n/locales/readiness.{en,it}.json`, `README.md`.
- **Change:**
  Preconditions: 7.3 DONE.
  Contract: `GET /api/v1/plans/samples?instance_id=&queryid=&database=&plan_hash=&from=&to=&limit=` (`limit` 1–1000, default 200; order `ts DESC`) → rows `ts, datname, queryid (string), plan_hash, duration_ms, analyzed, has_parameters, parameters` — `parameters` is returned only with `include_parameters=true`. `GET /api/v1/plans` rows gain `source` (`command`|`auto_explain`).
  Console: each plan in Plan history shows a badge `Automatic` / `On demand` (`Automatico` / `Su richiesta`); a `Samples` table under the selected plan (time, duration, analyzed, parameters hidden behind a `Show parameters` button); an empty state that lists the settings to enable.
  Readiness: `RDY-AEX-01` (info) when the coverage note `auto_explain_format_not_json` exists; text: set `auto_explain.log_format = json`.
  README section "Automatic plans": `shared_preload_libraries` or `session_preload_libraries` with `auto_explain`, `auto_explain.log_min_duration`, `auto_explain.log_format = json`, `compute_query_id = on`, `logging_collector = on`, `log_destination = 'jsonlog'`; cost warning for `auto_explain.log_analyze`; parameters are off by default and how to purge them.
  Steps: 1. S1 — API and OpenAPI. 2. S2 — console. 3. S3 — readiness. 4. S4 — help and README.
- **Unit tests:** `TestAEX014` (samples endpoint: parameter checks; `parameters` absent without the flag), `TestAEX015` (`source` in `/plans`), `TestAEX016` (`RDY-AEX-01`); `UI-AEX-001` (badge per source, `en` and `it`), `UI-AEX-002` (samples table renders; parameters requested only after the button), `UI-AEX-003` (empty state lists the settings). Gates G-GO-FOCUS, G-WEB-STATIC, G-WEB-UNIT, G-API.
- **e2e tests:** `TestINTAEX005` (`./internal/server/`): three samples of two plans → the endpoint filters by `plan_hash`. Gate G-GO-INT.
- **Done:** tests pass; README, help, readiness updated; reviewed; unit closed and committed.

## Phase gates and closure
- Required gates: G-GO-FULL, G-GO-INT (`TestINTAEX00[1-5]`), G-RO, G-API, G-WEB-FULL, G-I18N; assertions: I-4 tests green; existing `TestPLAN*`/plan history tests green.
- README obligation: unit 7.4.
- Every sub-phase DONE or explicitly justified SKIPPED. Run full phase gates now.
- agent-1 reviews integration, I-1, I-4, parameter handling, documentation.
- Open closure unit P7, record review and result, mark the phase DONE, close, commit `phase(007/P7): PHASE 7 COMPLETE — Automatic plans; gates and review passed`.
- P7 edits no production code or tests. Blocked gate: P7 stays OPEN and BLOCKED. Failing gate or rejected review: attempt failed; reopen the responsible sub-phase, then rerun P7.
