# Phase 8 — Autovacuum history per table

Intent: every autovacuum and autoanalyze run is kept with its numbers, and the console shows the history per table.
Prerequisites: P6.
Phase closure: P8; review by agent-1.

## State and ownership contract
Read STATE.md §1 first (and PROTOCOL.md once without this skill) for scope, ownership,
recovery, checks, and commit rules. Open before edits; checkpoint at a recovery boundary;
close with evidence. A delegated worker follows its assigned OPEN unit. Missing design goes
to agent-1, the supervisor. Executor power: low.

## Local design context
Plan revision: 1.
- D11. Source: log entries of class `autovacuum.run` (phase 6). Needs `log_autovacuum_min_duration >= 0` on the server.
- R5 line formats. Each line of the message is matched alone; a line that is missing or does not match leaves its fields NULL. Regular expressions (Go `regexp`, `(?m)` where a line start is needed):
  - header: `^automatic (aggressive )?(vacuum|analyze) (to prevent wraparound )?of table "([^"]+)"(?:: index scans: (\d+))?`
  - `^pages: (\d+) removed, (\d+) remain, (\d+) scanned`
  - `^tuples: (\d+) removed, (\d+) remain, (\d+) are dead but not yet removable`
  - `^buffer usage: (\d+) hits, (\d+) (?:misses|reads), (\d+) dirtied`
  - `^WAL usage: (\d+) records, (\d+) full page images, (\d+) bytes`
  - `^system usage: CPU: user: ([0-9.]+) s, system: ([0-9.]+) s, elapsed: ([0-9.]+) s`
  The table name is `database.schema.relation`: database = text before the first dot, schema = text between the first and second dot, relation = the rest (a name that itself contains a dot is split wrongly; documented limit).
- `VacuumRun` (Go): `Kind string` (`vacuum`|`analyze`), `Aggressive, Wraparound bool`, `Database, Schema, Relation string`, and pointers (nil = unknown): `IndexScans, PagesRemoved, PagesRemain, PagesScanned, TuplesRemoved, TuplesRemain, TuplesDeadNotRemovable, BufferHits, BufferReads, BufferDirtied, WALRecords, WALFullPageImages, WALBytes *int64`, `CPUUserSeconds, CPUSystemSeconds, ElapsedSeconds *float64`.
- Fact: kind `autovacuum_run`, `Key` = `<session_id>/<line_num>`, labels `ts`, `datname`, `schemaname`, `relname`, `kind`, `aggressive`, `wraparound`; `ValueJSON` = the numeric fields with snake_case names (`index_scans`, `pages_removed`, …, `elapsed_seconds`), nil fields left out. At most 500 per scrape; over that, counted in `pg_log_autovacuum_dropped`.
- Migration 0037:
  ```sql
  CREATE TABLE IF NOT EXISTS autovacuum_runs (
    ts          timestamptz NOT NULL,
    tenant_id   text        NOT NULL DEFAULT 'default',
    instance_id uuid        NOT NULL,
    cluster_id  bigint      NOT NULL,
    datname     text        NOT NULL,
    schemaname  text        NOT NULL,
    relname     text        NOT NULL,
    kind        text        NOT NULL CHECK (kind IN ('vacuum','analyze')),
    aggressive  boolean     NOT NULL DEFAULT false,
    wraparound  boolean     NOT NULL DEFAULT false,
    index_scans bigint, pages_removed bigint, pages_remain bigint, pages_scanned bigint,
    tuples_removed bigint, tuples_remain bigint, tuples_dead_not_removable bigint,
    buffer_hits bigint, buffer_reads bigint, buffer_dirtied bigint,
    wal_records bigint, wal_fpi bigint, wal_bytes bigint,
    cpu_user_seconds double precision, cpu_system_seconds double precision,
    elapsed_seconds double precision
  );
  SELECT create_hypertable('autovacuum_runs', by_range('ts', INTERVAL '7 days'), if_not_exists => TRUE);
  CREATE UNIQUE INDEX IF NOT EXISTS autovacuum_runs_dedup_idx
    ON autovacuum_runs (instance_id, datname, schemaname, relname, kind, ts);
  CREATE INDEX IF NOT EXISTS autovacuum_runs_lookup_idx
    ON autovacuum_runs (tenant_id, instance_id, datname, schemaname, relname, ts DESC);
  SELECT add_retention_policy('autovacuum_runs', INTERVAL '90 days', if_not_exists => TRUE);
  ```

## Sub-phases

### 8.1 Parser and agent facts
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** NEW `internal/pglog/autovacuum.go`, `autovacuum_test.go`, `internal/pglog/testdata/autovacuum_{pg15,pg18}_{vacuum,analyze}.txt`; WRITE `internal/wire/factkinds.go` (kind `autovacuum_run`), `cmd/pglens-agent/logs.go` and tests.
- **Change:**
  Preconditions: P6 DONE.
  Contract: `pglog.ParseAutovacuum(message string) (VacuumRun, bool)`; false when the header does not match. The loop builds one fact per parsed run.
  Steps: 1. S1 — fixtures: the two R5 messages of the overview, completed by a real capture (`postgres:15.19-alpine` and `18.6-alpine` with `-c log_autovacuum_min_duration=0 -c autovacuum_naptime=1s`, a table with 20 000 rows of which 19 900 deleted; copy the `message` of the vacuum and of the analyze line; remove the containers). 2. S2 — parser. 3. S3 — fact kind and loop.
- **Unit tests:** `TestAVH001` (PG 15 vacuum fixture: every field equals the number in the text; `BufferReads` comes from `misses`), `TestAVH002` (PG 18 vacuum fixture: `reads`, and the extra `eagerly scanned` / `buffers full` text does not break the match), `TestAVH003` (analyze fixtures: `Kind` `analyze`, page fields nil, `ElapsedSeconds` set), `TestAVH004` (headers `automatic aggressive vacuum to prevent wraparound of table "d.s.r"` → both flags true), `TestAVH005` (a message with only the header → run with all numbers nil), `TestAVH006` (loop: fact labels and `ValueJSON` names; 501 runs → 500 facts and dropped = 1). Gate G-GO-FOCUS `./internal/pglog/ ./internal/wire/ ./cmd/pglens-agent/`.
- **e2e tests:** `TestINTAVH001` (`./cmd/pglens-agent/`): PG 18.6 with the settings of S1 and the log directory mounted → at least one `autovacuum_run` fact for the test table with `tuples_removed` > 0. Gate G-GO-INT.
- **Done:** tests pass; reviewed; unit closed and committed.

### 8.2 Server storage and ingest
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** NEW `internal/store/migrations/0037_autovacuum_runs.sql` (D19); WRITE `internal/store/write.go` (`AutovacuumRunRow`, `WriteAutovacuumRuns`), `internal/server/pipeline.go`, `internal/server/retention.go` (family `autovacuum_runs`, default 90), tests.
- **Change:**
  Preconditions: 8.1 DONE.
  Contract: one row per fact; labels validated (`kind` in the two values, `ts` valid, `datname`/`schemaname`/`relname` non-empty, else rejected as `invalid_fact`); negative numbers rejected; `ON CONFLICT DO NOTHING`. The fact is not written to the object facts table.
  Steps: 1. S1 — migration. 2. S2 — row and writer. 3. S3 — pipeline. 4. S4 — retention family.
- **Unit tests:** `TestAVH007` (fact → row; missing optional numbers → NULL), `TestAVH008` (bad kind or empty relation → rejected). Gate G-GO-FOCUS.
- **e2e tests:** `TestINTAVH002` (`./internal/server/`): the same push twice → one row; retention 90 days exists. Gate G-GO-INT.
- **Done:** tests pass; reviewed; unit closed and committed.

### 8.3 API, console, readiness, documents
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** NEW `internal/server/api_autovacuum.go` and tests, `web/src/lib/autovacuum.ts` and test, `web/src/features/instance/AutovacuumRuns.tsx` and test; WRITE `api/openapi.yaml`, G-API outputs, `web/src/features/instance/RelationsSection.tsx` (table history view gets the runs of that table; the section gets an instance-wide `Autovacuum activity` view), `web/src/i18n/messages/{en,it}/instance.ts`, `web/src/lib/help/topics/{en,it}/instance-overview.ts`, `internal/readiness/rules.go`, `internal/i18n/locales/readiness.{en,it}.json`, `README.md`.
- **Change:**
  Preconditions: 8.2 DONE.
  Contract: `GET /api/v1/autovacuum/runs?instance_id=&database=&schema=&relation=&kind=&from=&to=&order_by=&limit=` — `order_by` `ts` (default) | `elapsed_seconds` | `tuples_removed`, always descending; `limit` 1–1000, default 200. Rows carry every column of the table. `GET /api/v1/autovacuum/summary?instance_id=&from=&to=&limit=` → per table: `runs`, `total_elapsed_seconds`, `max_elapsed_seconds`, `tuples_removed`, `last_ts`, `wraparound_runs`, ordered by `total_elapsed_seconds`.
  Console: `Autovacuum activity` view (summary table, sortable, rows open the table history); in the table history a `Runs` table with time, kind, flags (`aggressive`, `to prevent wraparound` shown as badges), elapsed, tuples removed, tuples dead not removable, pages scanned, WAL bytes, buffer reads; values formatted with `useFormat()`. Empty state: names `checks.logs.enabled` and `log_autovacuum_min_duration`.
  Readiness: `RDY-AV-01` (info) when the logs check is on and the setting `log_autovacuum_min_duration` is `-1` (read it with `settingValue`; how the "logs check is on" flag reaches readiness: add `LogsEnabled *bool` to `wire.AgentFlags` in `internal/wire/factkinds.go` and set it where the other flags are set).
  README section "Autovacuum history".
  Steps: 1. S1 — API and OpenAPI. 2. S2 — agent flag and readiness. 3. S3 — console. 4. S4 — help and README.
- **Unit tests:** `TestAVH009` (parameter checks; unknown `order_by` → 400), `TestAVH010` (summary maths from fake rows), `TestAVH011` (`RDY-AV-01` only with logs on and the setting `-1`); `UI-AVH-001` (summary renders and sorts), `UI-AVH-002` (runs table shows the wraparound badge), `UI-AVH-003` (empty state names both settings), `UI-AVH-004` (Italian texts). Gates G-GO-FOCUS, G-WEB-STATIC, G-WEB-UNIT, G-API.
- **e2e tests:** `TestINTAVH003` (`./internal/server/`): rows for two tables → `runs` filters by relation; `summary` orders by total elapsed. Gate G-GO-INT.
- **Done:** tests pass; README, help, readiness updated; reviewed; unit closed and committed.

## Phase gates and closure
- Required gates: G-GO-FULL, G-GO-INT (`TestINTAVH00[123]`), G-RO, G-API, G-WEB-FULL, G-I18N.
- README obligation: unit 8.3.
- Every sub-phase DONE or explicitly justified SKIPPED. Run full phase gates now.
- agent-1 reviews parser coverage on both PostgreSQL versions, integration, documentation.
- Open closure unit P8, record review and result, mark the phase DONE, close, commit `phase(007/P8): PHASE 8 COMPLETE — Autovacuum history; gates and review passed`.
- P8 edits no production code or tests. Blocked gate: P8 stays OPEN and BLOCKED. Failing gate or rejected review: attempt failed; reopen the responsible sub-phase, then rerun P8.
