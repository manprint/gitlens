# Phase 10 — Validated index suggestions

Intent: an operator asks pglens to check a suggested index with `hypopg`; the answer says whether the planner would use it and how much the cost drops, without creating anything.
Prerequisites: P0 (test image `pglens-pgext:18`).
Phase closure: P10; review by agent-1.

## State and ownership contract
Read STATE.md §1 first (and PROTOCOL.md once without this skill) for scope, ownership,
recovery, checks, and commit rules. Open before edits; checkpoint at a recovery boundary;
close with evidence. A delegated worker follows its assigned OPEN unit. Missing design goes
to agent-1, the supervisor. Executor power: low.

## Local design context
Plan revision: 1.
- D13, I-1, I-3, I-6. R8: `hypopg` 1.4.5 works for a `pg_monitor` role inside `BEGIN TRANSACTION READ ONLY`; `hypopg_list_indexes` is denied; a hypothetical index lives in the session, not the transaction.
- New command kind `index_check` (`command.KindIndexCheck`). It travels through the existing command path: `POST /api/v1/instances/{id}/commands` (`internal/server/commands.go`), agent dispatcher (`dispatcher.AddExecutor(...)` in `cmd/pglens-agent/run.go` near line 241), result stored by `persistCommandArtifact`.
- `command.Args` new fields (only for `index_check`): `IndexSchema string` (`index_schema`), `IndexRelation string` (`index_relation`), `IndexColumns []string` (`index_columns`), `IndexInclude []string` (`index_include`). Together with the existing `QueryID` (required) and `Datname` (required for this kind).
  Validation: schema and relation 1–63 bytes; 1–32 columns and 0–32 include columns, each 1–63 bytes; no NUL byte, no control character; a column listed twice is an error; any field of another kind set (`PID`, `Analyze`, `Params`, …) is an error; these four fields set on another kind is an error. `Args.UnmarshalJSON` (custom, near line 62) must decode the new fields. Columns are column names, never expressions: the executor quotes each as an identifier.
- Gate (`internal/command/gate.go`): same as `explain` without analyze — tier T1.
- Executor result JSON: `{"datname","index_name","index_size_bytes","cost_before","cost_after","index_used","plan_after"}`. `cost_*` = `Total Cost` of the root plan node. `index_used` = the plan after contains a node whose `Index Name` equals `index_name`. Error codes (in the command error text, as the explain executor reports its own): `hypopg_missing`, `statement_unavailable` (no text for the queryid), `relation_not_found`, `column_not_found`.
- Storage (migration 0038):
  ```sql
  DO $$
  DECLARE c text;
  BEGIN
    FOR c IN SELECT conname FROM pg_constraint
              WHERE conrelid = 'commands'::regclass AND contype = 'c'
                AND pg_get_constraintdef(oid) LIKE '%pgstattuple%' LOOP
      EXECUTE format('ALTER TABLE commands DROP CONSTRAINT %I', c);
    END LOOP;
  END $$;
  ALTER TABLE commands ADD CONSTRAINT commands_kind_check
    CHECK (kind IN ('explain','cancel','terminate','pgstattuple','index_check'));
  CREATE TABLE IF NOT EXISTS index_validations (
    validation_id    bigserial   PRIMARY KEY,
    tenant_id        text        NOT NULL DEFAULT 'default',
    instance_id      uuid        NOT NULL,
    cluster_id       bigint      NOT NULL,
    datname          text        NOT NULL,
    queryid          bigint      NOT NULL,
    schemaname       text        NOT NULL,
    relname          text        NOT NULL,
    columns          text[]      NOT NULL,
    include          text[]      NOT NULL DEFAULT '{}',
    index_used       boolean     NOT NULL,
    cost_before      double precision NOT NULL,
    cost_after       double precision NOT NULL,
    index_size_bytes bigint,
    plan_after       jsonb,
    validated_at     timestamptz NOT NULL,
    command_id       uuid        NOT NULL
  );
  CREATE UNIQUE INDEX IF NOT EXISTS index_validations_key_idx
    ON index_validations (tenant_id, instance_id, datname, queryid, schemaname, relname, columns, include);
  ```
  A new validation of the same key replaces the old one (`ON CONFLICT … DO UPDATE`).
- A suggestion (`internal/indexadvice/suggest.go`) has a table, ordered columns (`name`, `desc`), optional include columns and a list of queries with `queryid` and `share_pct`. The console validates against the query with the highest `share_pct`. The hypothetical index is always ascending btree; when the suggestion's columns have mixed directions the result is shown as approximate.

## Sub-phases

### 10.1 Command kind and arguments
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** WRITE `internal/command/types.go`, `gate.go`, their tests (`types_test.go`, `gate_test.go`), `web/src/lib/commands.ts` and test (kind union and labels), `web/src/i18n/messages/{en,it}/*.ts` where command kinds are named (find with `grep -rn 'pgstattuple' web/src --include='*.ts' --include='*.tsx' -l`), `api/openapi.yaml` (command kind enum and the four argument fields), G-API outputs.
- **Change:**
  Preconditions: P0 DONE.
  Contract: as in Local design context. `knownKind` accepts `index_check`.
  Steps: 1. S1 — kind, fields, `UnmarshalJSON`, `Validate`. 2. S2 — gate. 3. S3 — console kind list and texts (a command-audit row of this kind must render). 4. S4 — OpenAPI, G-API.
- **Unit tests:** `TestIDXV001` (valid arguments pass; each rule of the validation list fails with a message naming the field), `TestIDXV002` (the kind has no field that carries SQL: a JSON body with `"sql"`, `"statement"` or `"index_definition"` never reaches `Args` — assert the decoded struct equals the one without those keys — and `IndexColumns` containing `id); DROP TABLE x; --` is accepted only as an identifier that the executor quotes: the helper `indexDDL` of 10.2 is not in this unit, so here assert only that validation keeps the string intact and under 64 bytes; guards I-3), `TestIDXV003` (gate: tier below T1 → closed). `UI-IDXV-001` (the kind has a label in `en` and `it`). Gates G-GO-FOCUS `./internal/command/`, G-WEB-UNIT, G-API.
- **e2e tests:** N/A — types only.
- **Done:** tests pass; reviewed; unit closed and committed.

### 10.2 Agent executor
- **Agent:** agent-2
- **Assignment:** implement; agent-1 reviews the statement sequence before code and the diff after.
- **Files:** READ `internal/agent/exec_explain.go` (`Execute`: how it connects to the database, opens the read-only transaction, calls `dropExplainPrivileges`, `guardPlanning`, looks the statement up, explains, and calls `discardConn`), `internal/agent/exec_pgstattuple.go` (extension check per database); NEW `internal/agent/exec_index_check.go`, `exec_index_check_test.go`, `exec_index_check_integration_test.go`; WRITE `cmd/pglens-agent/run.go` (register the executor next to the explain one), `internal/agent/readonly_audit_integration_test.go`.
- **Change:**
  Preconditions: 10.1 DONE; image `pglens-pgext:18` exists (unit 0.2).
  Contract: `IndexCheckExecutor` implements the executor interface for `command.KindIndexCheck`. Statement sequence on one dedicated connection to `Datname`:
  1. `SELECT 1 FROM pg_extension WHERE extname = 'hypopg'` — no row → error `hypopg_missing`.
  2. Look up the statement text for `QueryID` exactly as the explain executor does (same helper); none → `statement_unavailable`; `redact.MayHoldCredential` → the same refusal the explain executor gives.
  3. `BEGIN TRANSACTION READ ONLY`, then the same privilege drop and planning guard as a non-analyze explain.
  4. `SELECT hypopg_reset()`.
  5. Plan the statement the way the explain executor plans it without analyze (same code path, including its handling of `$n` placeholders) → `cost_before`.
  6. Check the relation and columns exist: `SELECT attname FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = $1 AND c.relname = $2 AND a.attnum > 0 AND NOT a.attisdropped` → `relation_not_found` / `column_not_found`.
  7. `SELECT indexrelid, indexname FROM hypopg_create_index($1)` with `$1` = `indexDDL(args)` = `CREATE INDEX ON <schema>.<relation> USING btree (<col>, …)` plus ` INCLUDE (<col>, …)` when include columns exist; every identifier written with `pgx.Identifier{name}.Sanitize()`.
  8. `SELECT hypopg_relation_size($1)` with the returned `indexrelid`.
  9. Plan again → `cost_after`, `plan_after`, `index_used`.
  10. `SELECT hypopg_reset()`, `ROLLBACK`, then `discardConn` — these three run on every path after step 4, also on error (use `defer`).
  Scope: no change to the explain executor's behavior; shared code is extracted into unexported helpers only when needed, with the explain tests unchanged and green.
  Steps: 1. S1 — write the sequence with the real helper names found in `exec_explain.go` and send it to agent-1. 2. S2 — `indexDDL` and unit tests. 3. S3 — executor with a fake connection. 4. S4 — integration tests on `pglens-pgext:18`. 5. S5 — register; extend G-RO's allowed statements.
  Recovery boundary: after S3.
  Failure handling: `dropExplainPrivileges` removes the right to call `hypopg_create_index` → stop and ask agent-1 (do not weaken the privilege drop).
- **Unit tests:** `TestIDXV004` (`indexDDL`: plain names; a name with a double quote and one with `); DROP TABLE x; --` are quoted so the DDL has exactly one statement and the name survives literally), `TestIDXV005` (no extension row → `hypopg_missing`, and no later statement is sent), `TestIDXV007` (error after step 4 → `hypopg_reset`, `ROLLBACK` and discard still happen, in that order), `TestIDXV008` (`index_used` true only when `plan_after` names the returned index). Gate G-GO-FOCUS `./internal/agent/`.
- **e2e tests:** `TestINTIDXV001` (`pglens-pgext:18`, table of 100 000 rows, statement `SELECT … WHERE col = $1` present in `pg_stat_statements`): result has `index_used: true`, `cost_after < cost_before`, `index_size_bytes > 0`. `TestINTIDXV002` (guards I-6): after the command, on a new connection and on any pooled connection of the agent, `SELECT count(*) FROM hypopg()` is 0 and `pg_indexes` has no new index. `TestINTIDXV003`: on `postgres:18.6-alpine` (no `hypopg`) → `hypopg_missing`. `TestINTRO001` passes. Skip rule: if `pglens-pgext:18` is absent the first two are skipped with the message `pglens-pgext:18 not built (make pgext-image)` and P10 cannot close until they ran. Gates G-GO-INT, G-RO.
- **Done:** tests pass, none skipped; sequence approved; reviewed; unit closed and committed.

### 10.3 Server: storage and the suggestions response
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** NEW `internal/store/migrations/0038_index_validations.sql` (D19); WRITE `internal/store/write.go` (`IndexValidationRow`, `UpsertIndexValidation`), `internal/server/commands.go` (`persistCommandArtifact` case for the new kind; the enqueue path accepts the kind), `internal/server/index_suggestions.go` (response), tests, `api/openapi.yaml`, G-API outputs.
- **Change:**
  Preconditions: 10.2 DONE.
  Contract: a successful `index_check` result upserts one `index_validations` row. Each suggestion in `GET /api/v1/instances/{id}/index-suggestions` gains `"validation": null | {"queryid","index_used","cost_before","cost_after","cost_ratio","index_size_bytes","validated_at","approximate"}` — the row whose schema, relation, columns (same order) and include match the suggestion, newest when several queries were validated. `cost_ratio` = `cost_after / cost_before` (null when before is 0). `approximate` = the suggestion's columns have mixed directions. An agent that does not know the kind rejects the command; the command row then shows its error text unchanged (no special case).
  Steps: 1. S1 — migration. 2. S2 — store. 3. S3 — artifact case. 4. S4 — suggestions response. 5. S5 — OpenAPI, G-API.
- **Unit tests:** `TestIDXV009` (artifact → row; a failed command stores nothing), `TestIDXV010` (suggestions response: match by columns in order; no match → `null`; `approximate` for mixed directions), `TestIDXV006` (a command result with the error text of an old agent — `unknown command kind` — is stored as a failed command and the suggestion keeps `validation: null`). Gate G-GO-FOCUS `./internal/server/ ./internal/store/`.
- **e2e tests:** `TestINTIDXV004` (`./internal/server/`): after migrations an `index_check` command row can be inserted (the CHECK allows it) and a second result for the same key replaces the first. Gate G-GO-INT.
- **Done:** tests pass; reviewed; unit closed and committed.

### 10.4 Console action, help, README
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** WRITE `web/src/features/indexes/MissingIndexesPanel.tsx` and test, `web/src/lib/missingIndexes.ts` or `indexSuggestions.ts` (whichever holds the suggestion type) and test, `web/src/api/commands.ts`, `web/src/i18n/messages/{en,it}/indexes.ts`, `web/src/lib/help/topics/{en,it}/indexes.ts`, `README.md`.
- **Change:**
  Preconditions: 10.3 DONE.
  Contract: each suggestion has a `Validate with hypopg` button (disabled with a tooltip when commands are off for the instance, using the same flag the EXPLAIN button uses). It enqueues `index_check` for the top query, shows progress like the EXPLAIN action, then the result: badge `Planner would use it` / `Planner would not use it`, cost before → after with the ratio, estimated size, time of validation, `approximate` note. Error `hypopg_missing` shows: `Install the hypopg extension in database <db> to validate suggestions (CREATE EXTENSION hypopg).` The text states that nothing is created on the database.
  Steps: 1. S1 — lib types and API call. 2. S2 — panel. 3. S3 — help and README section "Validating an index suggestion".
- **Unit tests:** `UI-IDXV-002` (button enqueues the command with schema, relation, columns, include and the top query), `UI-IDXV-003` (result badges for used / not used), `UI-IDXV-004` (`hypopg_missing` text with the database name), `UI-IDXV-005` (button disabled when commands are off), `UI-IDXV-006` (Italian texts). Gates G-WEB-STATIC, G-WEB-UNIT.
- **e2e tests:** N/A at unit level; lab check in 15.2.
- **Done:** tests pass; README and help updated; reviewed; unit closed and committed.

## Phase gates and closure
- Required gates: G-GO-FULL, G-GO-INT (`TestINTIDXV00[1-4]`, none skipped), G-RO, G-API, G-WEB-FULL, G-I18N; assertions: I-1, I-3, I-6 tests green.
- README obligation: unit 10.4.
- Every sub-phase DONE or explicitly justified SKIPPED. Run full phase gates now.
- agent-1 reviews the executor sequence against I-1 and I-6, identifier quoting, documentation.
- Open closure unit P10, record review and result, mark the phase DONE, close, commit `phase(007/P10): PHASE 10 COMPLETE — Validated index suggestions; gates and review passed`.
- P10 edits no production code or tests. Blocked gate: P10 stays OPEN and BLOCKED. Failing gate or rejected review: attempt failed; reopen the responsible sub-phase, then rerun P10.
