# Phase 13 — Tuning workbook

Intent: an operator keeps, per query, a set of named variants (planner settings, `pg_hint_plan` hints, parameter values), runs each as an EXPLAIN through the agent and compares the plans.
Prerequisites: P9.
Phase closure: P13; review by agent-1.

## State and ownership contract
Read STATE.md §1 first (and PROTOCOL.md once without this skill) for scope, ownership,
recovery, checks, and commit rules. Open before edits; checkpoint at a recovery boundary;
close with evidence. A delegated worker follows its assigned OPEN unit. Missing design goes
to agent-1, the supervisor. Executor power: low.

## Local design context
Plan revision: 1.
- D16, I-1, I-3. R9: `pg_hint_plan` reads hints only from the first block comment `/*+ … */`; a role that is not superuser cannot `LOAD` it, so it works only when the server preloads it.
- A variant run is an ordinary `explain` command (`command.KindExplain`) with two new optional `command.Args` fields. Gates, tiers, audit and the read-only transaction of the explain executor stay as they are.
- `Args.Settings map[string]string` (JSON `settings`), at most 16 entries. Allowed names and value rules (anything else is a validation error naming the setting):
  | Names | Value |
  |-------|-------|
  | `enable_seqscan`, `enable_indexscan`, `enable_indexonlyscan`, `enable_bitmapscan`, `enable_tidscan`, `enable_nestloop`, `enable_hashjoin`, `enable_mergejoin`, `enable_hashagg`, `enable_sort`, `enable_incremental_sort`, `enable_material`, `enable_memoize`, `enable_gathermerge`, `enable_parallel_append`, `enable_parallel_hash`, `enable_partition_pruning`, `enable_partitionwise_join`, `enable_partitionwise_aggregate`, `jit` | `on` or `off` |
  | `work_mem` | whole number of kB, 64 – 262144 (digits only; the agent adds `kB`) |
  | `effective_cache_size` | whole number of kB, 8 – 1073741824 (digits only; the agent adds `kB`) |
  | `random_page_cost`, `seq_page_cost`, `cpu_tuple_cost`, `cpu_index_tuple_cost`, `cpu_operator_cost` | decimal matching `^[0-9]{1,4}(\.[0-9]{1,6})?$` |
  | `hash_mem_multiplier` | decimal matching the same pattern, 1 – 1000 |
  | `join_collapse_limit`, `from_collapse_limit` | whole number 1 – 100 |
  | `max_parallel_workers_per_gather` | whole number 0 – 8 |
  | `plan_cache_mode` | `auto`, `force_generic_plan`, `force_custom_plan` |
  The list is one exported Go table (`command.WorkbookSettings`) with a validator per row; the server, the agent and (through the OpenAPI enum) the console use the same names.
- `Args.Hints string` (JSON `hints`), at most 1024 bytes. Allowed characters: ASCII letters, digits, space, and `_ , ( ) # + - . "`. Any other character is an error (so `*` and `/` can never close the comment). A text matching `(?i)\bset\s*\(` is an error with the message `use settings instead of a Set hint` (a `Set` hint would bypass the settings list). Limit of revision 1: the `Rows` correction form with `*` is not available.
- Both fields are valid only on `explain`; set on another kind they are an error.
- Agent behavior (inside the explain executor's existing read-only transaction, after its privilege drop and planning guard, before the statement is planned):
  1. `Hints` not empty → `SELECT current_setting('pg_hint_plan.enable_hint', true)`; NULL → error `hint_plan_inactive`; the value `off` → error `hint_plan_disabled`. The agent never sends `LOAD`.
  2. For each setting in name order: `SELECT set_config($1, $2, true)` (third argument `true` = local to the transaction). Name and value are bound parameters; no SQL text is built from them.
  3. `Hints` not empty → the text `/*+ <hints> */ ` is placed immediately before the statement text in the `EXPLAIN` the executor already builds.
  Because step 2 is transaction-local and the executor ends with `ROLLBACK` and `discardConn`, nothing survives the command.
- Result: the explain result JSON gains `"settings"` and `"hints"` (echo of what was applied) so the stored artifact is self-describing.
- A plan produced with settings or hints is an experiment: the server does not write it to `query_plans` (plan history shows only plans of the real workload); it is stored on the variant.
- Storage (migration 0041):
  ```sql
  CREATE TABLE IF NOT EXISTS workbooks (
    workbook_id uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   text        NOT NULL DEFAULT 'default',
    instance_id uuid        NOT NULL,
    datname     text        NOT NULL,
    queryid     bigint      NOT NULL,
    name        text        NOT NULL,
    notes       text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
  );
  CREATE INDEX IF NOT EXISTS workbooks_lookup_idx ON workbooks (tenant_id, instance_id, queryid);
  CREATE TABLE IF NOT EXISTS workbook_variants (
    variant_id   uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    workbook_id  uuid        NOT NULL REFERENCES workbooks ON DELETE CASCADE,
    position     integer     NOT NULL,
    name         text        NOT NULL,
    settings     jsonb       NOT NULL DEFAULT '{}',
    hints        text        NOT NULL DEFAULT '',
    params       jsonb,
    analyze      boolean     NOT NULL DEFAULT false,
    status       text        NOT NULL DEFAULT 'never' CHECK (status IN ('never','pending','done','failed')),
    command_id   uuid,
    error        text        NOT NULL DEFAULT '',
    plan         jsonb,
    plan_hash    text,
    total_cost   double precision,
    planning_ms  double precision,
    execution_ms double precision,
    ran_at       timestamptz
  );
  CREATE UNIQUE INDEX IF NOT EXISTS workbook_variants_command_idx ON workbook_variants (command_id) WHERE command_id IS NOT NULL;
  ```
  `params` is `null` or an array of strings and nulls, with the limits the explain command already has (`command.MaxParams`, `command.MaxParamValueBytes`). The values are what the operator typed; they are stored like the `params` of an explain command are stored in `commands` today, and deleted with the workbook.
- Limits: 100 workbooks per instance, 20 variants per workbook, names 1–120 characters, notes at most 4000 characters.

## Sub-phases

### 13.1 Command arguments `settings` and `hints`
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** WRITE `internal/command/types.go` (fields, `UnmarshalJSON`, `Validate`), NEW `internal/command/workbook.go` (`WorkbookSettings`, `ValidateSetting(name, value string) error`, `ValidateHints(h string) error`, `SettingValue(name, value string) string` — the value the agent passes to `set_config`, with `kB` added for the two size settings), tests, `api/openapi.yaml` (the two fields on the explain arguments; `settings` as an object whose property names are the enum), G-API outputs.
- **Change:**
  Preconditions: P9 DONE.
  Contract: as in Local design context. An explain command without the two fields is byte-identical on the wire to today's (both fields `omitempty`).
  Steps: 1. S1 — table and validators. 2. S2 — `Args` fields and decoding. 3. S3 — OpenAPI, G-API.
- **Unit tests:** `TestWBK001` (every name of the table with one valid and one invalid value; unknown name; 17 entries), `TestWBK002` (guards I-3: a value containing `; DROP TABLE x` or `on'; --` fails validation for every setting; hints containing `*/`, `/*`, `;`, a newline or a quote `'` fail; a JSON body with keys `sql` or `statement` decodes to the same `Args` as without them), `TestWBK003` (hints: `SeqScan(t) Leading(a b)` valid; `Set(work_mem "1GB")` and `set (x y)` rejected with the settings message; 1025 bytes rejected), `TestWBK013` (the two fields on kind `cancel` → error; explain without them encodes without the keys). Gate G-GO-FOCUS `./internal/command/`, G-API.
- **e2e tests:** N/A — types only.
- **Done:** tests pass; reviewed; unit closed and committed.

### 13.2 Agent applies settings and hints
- **Agent:** agent-2
- **Assignment:** implement; agent-1 reviews the statement sequence before code and the diff after.
- **Files:** READ `internal/agent/exec_explain.go` (`Execute`, `explainVerbatim`, `explainBound`: where the EXPLAIN text is built on each path), `internal/agent/explain_guard.go`; WRITE `internal/agent/exec_explain.go`, `exec_explain_test.go`, NEW `internal/agent/exec_explain_workbook_integration_test.go`, `internal/agent/readonly_audit_integration_test.go`.
- **Change:**
  Preconditions: 13.1 DONE.
  Contract: as in Local design context, on both explain paths (verbatim text and bound parameters). With neither field set, the statements sent to PostgreSQL are exactly today's (existing explain tests unchanged and green).
  Steps: 1. S1 — write, for both paths, the list of statements sent in order with settings and hints set, using the real helper names; send to agent-1. 2. S2 — `applyWorkbook(ctx, tx, args)` (steps 1 and 2 of the design) and the hint prefix. 3. S3 — unit tests with the fake connection. 4. S4 — integration tests. 5. S5 — result echo; G-RO allowed statements.
  Recovery boundary: after S3.
  Failure handling: on the bound-parameter path the hint has no effect in `TestINTWBK003` → stop and ask agent-1 (do not move the comment by trial). The privilege drop blocks `set_config` → stop and ask agent-1.
- **Unit tests:** `TestWBK004` (settings are applied with `set_config($1,$2,true)` in name order, inside the transaction, before the EXPLAIN; `work_mem` value `65536` is sent as `65536kB`), `TestWBK005` (`current_setting` NULL → `hint_plan_inactive`; `off` → `hint_plan_disabled`; in both cases no EXPLAIN is sent), `TestWBK006` (hint comment sits immediately before the statement text on both paths; no statement containing `LOAD` is ever sent). Gate G-GO-FOCUS `./internal/agent/`.
- **e2e tests:** `TestINTWBK001` (PG 18.6 and 15.19: table with an index, statement in `pg_stat_statements`; baseline plan uses the index; with `enable_indexscan=off`, `enable_bitmapscan=off`, `enable_indexonlyscan=off` the plan root scan is `Seq Scan`), `TestINTWBK002` (after the command, a new statement on any agent connection reads `SHOW enable_indexscan` = `on`), `TestINTWBK003` (`pglens-pgext:18` started with `-c shared_preload_libraries=pg_stat_statements,pg_hint_plan`: hints `SeqScan(<table>)` → `Seq Scan`, on the verbatim and on the bound path; on `postgres:18.6-alpine` → `hint_plan_inactive`). Skip rule (R9): when unit 0.2 recorded that `pg_hint_plan` could not be built, the first half of `TestINTWBK003` is skipped with the reason `pg_hint_plan not in pglens-pgext:18`; the skip is recorded in STATE.md §7 and in the README limit note. `TestINTRO001` passes. Gates G-GO-INT, G-RO.
- **Done:** tests pass (or the recorded skip); sequence approved; reviewed; unit closed and committed.

### 13.3 Server: workbooks storage and API
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** NEW `internal/store/migrations/0041_workbooks.sql` (D19), `internal/server/api_workbooks.go` and tests, `internal/store/workbooks.go` and tests; WRITE `internal/server/http.go` (routes), `internal/server/commands.go` (result handler and artifact rule), `api/openapi.yaml`, G-API outputs.
- **Change:**
  Preconditions: 13.2 DONE.
  Contract: routes (same credential and write rules as `POST /api/v1/instances/{id}/commands`):
  - `GET /api/v1/workbooks?instance_id=&queryid=` → list without variants' plans.
  - `POST /api/v1/workbooks` `{instance_id, database, queryid (string), name, notes}` → 201; creates the workbook and a first variant `Baseline` (no settings, no hints, no parameters).
  - `GET /api/v1/workbooks/{id}` → workbook with its variants (with plans).
  - `PATCH /api/v1/workbooks/{id}` `{name?, notes?}`; `DELETE /api/v1/workbooks/{id}`.
  - `POST /api/v1/workbooks/{id}/variants` `{name, settings, hints, params, analyze}`; `PUT …/variants/{variant_id}` (same body; resets `status` to `never` and clears the result columns); `DELETE …/variants/{variant_id}`.
  - `POST /api/v1/workbooks/{id}/variants/{variant_id}/run` → 202 `{command_id}`: builds `command.Args` from the workbook and the variant and enqueues it through the same function the commands route uses (same gates: a closed gate gives the same status and body as the commands route); sets `status = 'pending'`, `command_id`. A variant already `pending` → 409 `variant_pending`.
  Validation uses `command.ValidateSetting`, `command.ValidateHints` and the limits; errors are 400 with the field name; unknown id or another tenant's id → 404.
  Result handler (`POST /api/v1/commands/{id}/result`), in its transaction after the existing artifact step: when a variant has this `command_id`, set `status` (`done` or `failed`), `error`, `plan`, `plan_hash`, `total_cost` (root `Total Cost`), `planning_ms` (`Planning Time`), `execution_ms` (`Execution Time`, NULL when not analyzed), `ran_at`. A command that expires or is rejected marks the variant `failed` with the command's error text (find where a command reaches a final state without a result and call the same update there).
  Artifact rule: an explain result whose arguments have settings or hints is not written to `query_plans`.
  Steps: 1. S1 — migration and store. 2. S2 — CRUD handlers. 3. S3 — run and result linking. 4. S4 — artifact rule. 5. S5 — OpenAPI, G-API.
  Recovery boundary: after S2 and after S3.
- **Unit tests:** `TestWBK007` (create → baseline variant exists; limits 101st workbook and 21st variant → 409 `limit_reached`), `TestWBK008` (bad setting, bad hints, too many params → 400 naming the field), `TestWBK009` (run builds `Args` with settings, hints, params, analyze and the workbook's queryid and database; pending variant → 409), `TestWBK010` (result → variant columns; failed command → `failed` with the error), `TestWBK011` (explain result with settings → no `query_plans` write; without → written as today), `TestWBK012` (editing a variant resets status and result). Gate G-GO-FOCUS `./internal/server/ ./internal/store/`.
- **e2e tests:** `TestINTWBK004` (`./internal/server/`): create, add a variant, run, post a result as the agent would, read back `done` with `total_cost`; delete the workbook removes its variants. Gate G-GO-INT.
- **Done:** tests pass; reviewed; unit closed and committed.

### 13.4 Console workbook, help, README
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** NEW `web/src/features/workbook/{WorkbookPage,VariantEditor,VariantCompare}.tsx` and tests, `web/src/lib/workbook.ts` and test, `web/src/api/workbooks.ts`, `web/src/i18n/messages/{en,it}/workbook.ts` (register the area where the other areas are registered — see `web/src/i18n/messages/en/index.ts`), `web/src/lib/help/topics/{en,it}/workbook.ts` (register like the other topics); WRITE `web/src/routes/index.tsx` (route `/instances/:id/queries/:queryid/workbooks/:workbookId` through `lazyRoute`), the query detail page (button `Tuning workbook` listing the query's workbooks and `New workbook`), `README.md`.
- **Change:**
  Preconditions: 13.3 DONE.
  Contract: the page lists variants as columns of one table: name, settings (only changed ones), hints, parameters (count, values behind a "show" toggle), status, total cost, planning and execution time, plan hash, and the ratio of cost and time to the `Baseline` variant. `VariantEditor`: settings chosen from the list with the right input per value rule (toggle, number with range, select), hints as text with the allowed-characters rule shown, parameters as a list, `ANALYZE` checkbox with the same warning text the EXPLAIN panel uses. `Run` per variant and `Run all` (one after the other); status polls like the EXPLAIN action. Ticking two done variants opens `VariantCompare`, which reuses `PlanCompare` of unit 9.3. Errors `hint_plan_inactive` / `hint_plan_disabled` show: `pg_hint_plan is not active on this server. It must be in shared_preload_libraries or session_preload_libraries; pglens never loads it.` The page states that every run is an EXPLAIN in a read-only transaction and that settings last only for that transaction.
  README section "Tuning workbook": purpose, the settings list, hint rules and limits (no `Set` hint, no `*`), `pg_hint_plan` requirement, where parameters are stored, the recorded skip of 13.2 if any.
  Steps: 1. S1 — lib and API client. 2. S2 — page and editor. 3. S3 — compare. 4. S4 — help and README.
- **Unit tests:** `UI-WBK-001` (variants table renders ratios against Baseline), `UI-WBK-002` (editor rejects a hint with `*/` and a `work_mem` out of range before sending), `UI-WBK-003` (`Run all` enqueues variants one after the other), `UI-WBK-004` (`hint_plan_inactive` text), `UI-WBK-005` (two done variants → compare view), `UI-WBK-006` (Italian texts; setting names stay English). Gates G-WEB-STATIC, G-WEB-UNIT.
- **e2e tests:** N/A at unit level; lab check in 15.2.
- **Done:** tests pass; README and help updated; reviewed; unit closed and committed.

## Phase gates and closure
- Required gates: G-GO-FULL, G-GO-INT (`TestINTWBK00[1-4]`), G-RO, G-API, G-WEB-FULL, G-I18N; assertions: I-1 and I-3 tests green; all existing explain tests unchanged and green.
- README obligation: unit 13.4.
- Every sub-phase DONE or explicitly justified SKIPPED. Run full phase gates now.
- agent-1 reviews that no SQL text is built from user input, that nothing outlives the transaction, the skip record, documentation.
- Open closure unit P13, record review and result, mark the phase DONE, close, commit `phase(007/P13): PHASE 13 COMPLETE — Tuning workbook; gates and review passed`.
- P13 edits no production code or tests. Blocked gate: P13 stays OPEN and BLOCKED. Failing gate or rejected review: attempt failed; reopen the responsible sub-phase, then rerun P13.
