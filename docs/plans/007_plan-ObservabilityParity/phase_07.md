# Phase 6 — PostgreSQL log ingest and classification

Intent: the agent reads the PostgreSQL log, classifies each line, and the console shows the classified events and alerts on the dangerous ones.
Prerequisites: P0.
Phase closure: P6; review by agent-1.

## State and ownership contract
Read STATE.md §1 first (and PROTOCOL.md once without this skill) for scope, ownership,
recovery, checks, and commit rules. Open before edits; checkpoint at a recovery boundary;
close with evidence. A delegated worker follows its assigned OPEN unit. Missing design goes
to agent-1, the supervisor. Executor power: low.

## Local design context
Plan revision: 1.
- D9, I-1, I-4, I-9. New package `internal/pglog` (no database import; pure functions and a file reader). The loop that runs it lives in `cmd/pglens-agent/logs.go` and is started like the watcher (read `cmd/pglens-agent/watch.go` and the `startASH(...)` call in `run.go` near line 173 before writing it).
- `Entry` (one log line):
  ```go
  type Entry struct {
      Time            time.Time
      User, Database  string
      PID             int
      SessionID       string
      LineNum         int64
      Severity        string // LOG, WARNING, ERROR, FATAL, PANIC, ...
      SQLState        string
      Message, Detail, Hint, Context, Statement string
      ApplicationName, BackendType string
      QueryID         int64
  }
  ```
- R3 `jsonlog`: one JSON object per line. Keys read: `timestamp, user, dbname, pid, session_id, line_num, error_severity, state_code, message, detail, hint, context, statement, application_name, backend_type, query_id`. Unknown keys are ignored. R3 `csvlog`: 26 columns in the documented order (overview R3); a record with 23 to 25 columns is accepted and the missing ones are empty; a record can span several lines.
- Time: the log time looks like `2026-11-21 06:08:11.123 UTC`. The zone word is not trusted. The parser drops the last word and parses the rest with layout `2006-01-02 15:04:05.000` in the location named by the server setting `log_timezone` (`time.LoadLocation`); when the location cannot be loaded it uses UTC and the loop adds a coverage note.
- Classes (function `Classify(e Entry) string`; first match wins; `""` means "not an event"):
  1. By SQLSTATE: `40P01`→`lock.deadlock`; `40001`→`serialization.failure`; `57014`→`statement.timeout` when the message contains `statement timeout`, else `statement.canceled`; `55P03`→`lock.timeout`; `25P03`→`idle_tx.timeout`; `28P01`, `28000`→`auth.failed`; `53300`→`connection.limit`; `53100`→`disk.full`; `53200`→`memory.out`; `XX001`, `XX002`→`corruption`.
  2. By message prefix (after trimming): `duration: ` followed by ` plan:` on the first line→`autoexplain.plan`; other `duration: `→`slow.statement`; `automatic vacuum`, `automatic aggressive vacuum`, `automatic analyze`→`autovacuum.run`; `checkpoints are occurring too frequently`→`checkpoint.frequent`; `checkpoint starting:` or `checkpoint complete:`→`checkpoint`; `temporary file:`→`temp_file`; `process ` + digits + ` still waiting for`→`lock.wait`; message contains `was terminated by signal`→`server.crash`; contains `conflict with recovery`→`replication.conflict`; contains `must be vacuumed within`→`wraparound.warning`; contains `No space left on device`→`disk.full`; prefix `out of memory`→`memory.out`; `database system is ready to accept connections`→`server.start`; `database system was interrupted` or `database system was not properly shut down`→`server.recovery`.
  3. By severity: `PANIC`→`panic`; `FATAL`→`fatal.other`; `ERROR`→`error.statement`; `WARNING`→`warning.other`; anything else→`""`.
  Rule 2 needs English server messages; rule 1 and 3 work in any language.
- Budgets (I-9): `max_bytes_per_interval` default 1 048 576 (range 65 536–16 777 216); a line longer than 262 144 bytes is cut there; at most 200 `log_event` facts per scrape, the rest are only counted; if the unread backlog is larger than 16 × `max_bytes_per_interval`, the reader jumps to the end of the file and reports the skipped bytes.
- Position: `{"file": "<name>", "offset": <bytes>}` per target, kept in one JSON file `state_path` (default: the directory of `identity_path` + `/pglog-state.json`), written with temp file + rename after each scrape. First run: start at the end of the current file. Size smaller than the offset: start at 0. Current file name changed: read the old file to its end (inside the budget), then the new file from 0. Delivery is at most once: a crash between read and push loses those lines (documented).
- What is shipped: fact kind `log_event`, `Key` = `<session_id>/<line_num>`, labels `ts` (RFC 3339 with nanoseconds, UTC), `class`, `severity`, `sqlstate`, `datname`, `usename`, `application_name`, `pid`, `queryid` (empty labels are left out), `ValueJSON` = `{"message":"…"}` plus `detail`, `hint`, `context`, `statement` only with `statement_text: true`. A fact sets `ValueText` or `ValueJSON`, not both (read `Fact.validate` in `internal/wire/envelope.go`). `message` is cut to 2000 bytes, the other texts to 8000. Any text for which `redact.MayHoldCredential` is true is replaced by the constant `[withheld: may hold a credential]`. Classes `autoexplain.plan` and `""` produce no `log_event` fact. For `autovacuum.run` only the first line of the message is shipped.
- Metrics per scrape (check name `logs`, gauges, value = lines read in this scrape): `pg_log_events{class}`, and without labels `pg_log_panic_events` (classes `panic`, `server.crash`), `pg_log_corruption_events`, `pg_log_disk_full_events`, `pg_log_oom_events` (always emitted, 0 when none), `pg_log_bytes_read`, `pg_log_bytes_skipped`, `pg_log_events_dropped` (over the 200 budget).

## Sub-phases

### 6.1 Parser: `jsonlog` and `csvlog`
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** NEW `internal/pglog/entry.go`, `jsonlog.go`, `csvlog.go`, `reader.go`, `*_test.go`, `internal/pglog/testdata/` (`pg15.jsonl`, `pg18.jsonl`, `pg18.csv`).
- **Change:**
  Preconditions: P0 DONE.
  Contract: `ParseJSONLine(line []byte, loc *time.Location) (Entry, error)`; `ParseCSVRecord(fields []string, loc *time.Location) (Entry, error)`; `Split(format string, data []byte, maxLine int) (records [][]byte or [][]string, consumed int)` — returns only complete records and how many bytes they used, so a half-written last line stays unread; a JSON line over `maxLine` is cut and still returned as an `Entry` with `Message` = the raw prefix and `Severity` = `LOG`. CSV uses `encoding/csv` with `FieldsPerRecord = -1`; a parse error at the end of the data means "incomplete record" and stops the split there; a parse error in the middle skips to the next line start and counts one bad record.
  Steps: 1. S1 — produce the fixtures: start `postgres:18.6-alpine` and `postgres:15.19-alpine` with `-c logging_collector=on -c log_destination=jsonlog,csvlog -c log_min_duration_statement=0 -c log_lock_waits=on`, run a failing statement, a multi-line statement and `SELECT pg_sleep(0.1)`, copy 20–40 lines of each log into `testdata/`, remove the containers. 2. S2 — parsers and tests. 3. S3 — `Split` and tests.
  Failure handling: Docker cannot pull an image → use the lines of overview R3–R5 as fixtures and record it in the unit notes.
- **Unit tests:** `TestPGL001` (every line of both `jsonl` fixtures parses; one ERROR line has `SQLState` and `Statement`), `TestPGL002` (csv fixture: a multi-line statement is one record; 26 columns), `TestPGL003` (time: `2026-11-21 07:08:11.123 CET` with location `Europe/Rome` → 06:08:11.123 UTC; unknown key ignored), `TestPGL004` (`Split` leaves a half line unread; `consumed` is the byte count of the complete records), `TestPGL005` (csv record with 23 columns is accepted; garbage line counted as bad and skipped), `TestPGL015` (a 300 000-byte line is cut to 262 144; guards I-9). Gate G-GO-FOCUS `./internal/pglog/`.
- **e2e tests:** N/A — pure functions; the real server is covered in 6.4.
- **Done:** tests pass; fixtures contain no real host names or user data; reviewed; unit closed and committed.

### 6.2 Classifier
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** NEW `internal/pglog/classify.go`, `classify_test.go`.
- **Change:**
  Preconditions: 6.1 DONE.
  Contract: `Classify(e Entry) string` per Local design context; `Classes() []string` returns the sorted list of all class names (the server and console use it); `Shipped(class string) bool` is false for `""` and `autoexplain.plan`.
  Steps: 1. S1 — table-driven rules in the three groups, in that order. 2. S2 — tests: one case per class plus the precedence cases.
- **Unit tests:** `TestPGL006` (one entry per class → that class; table lists every name of `Classes()`), `TestPGL007` (precedence: SQLSTATE `40P01` with severity ERROR → `lock.deadlock`, not `error.statement`; `57014` with `canceling statement due to user request` → `statement.canceled`), `TestPGL008` (a LOG line that matches nothing → `""`; a German ERROR message with SQLSTATE `53100` → `disk.full`). Gate G-GO-FOCUS.
- **e2e tests:** N/A — pure function.
- **Done:** tests pass; reviewed; unit closed and committed.

### 6.3 Sources and saved position
- **Agent:** agent-2
- **Assignment:** implement; agent-1 reviews the design note before code and the diff after.
- **Files:** NEW `internal/pglog/source.go`, `source_file.go`, `source_sql.go`, `position.go`, tests.
- **Change:**
  Preconditions: 6.2 DONE.
  Contract:
  `type Position struct { File string; Offset int64 }`.
  `type Source interface { Current(ctx) (name string, size int64, err error); Size(ctx, name string) (int64, bool, error); ReadAt(ctx, name string, off, n int64) ([]byte, error) }`.
  `NewFileSource(dir string, current func(ctx) (string, error))` reads files under `dir` only: a name with `/` or `..` is refused. `NewSQLSource(q Querier, format string)` uses `pg_current_logfile($1)`, `pg_ls_logdir()` for sizes, and `pg_read_binary_file($1, $2, $3, true)` (statements prefixed with the `/* pglens: … */` comment style of `internal/check`); a permission error becomes the typed error `ErrLogReadDenied`.
  `func Advance(ctx, src Source, pos Position, first bool, budget int64) (chunks [][]byte, next Position, skipped int64, err error)` implements the position rules and the backlog rule of Local design context.
  `LoadPositions(path) (map[string]Position, error)` (missing file → empty map; broken JSON → empty map and an error to log) and `SavePositions(path, m)` (temp file in the same directory, `0600`, rename).
  Steps: 1. S1 — write a 15-line design note in the unit record (cases: first run, same file grown, rotated, truncated, backlog, file gone) and get agent-1's approval. 2. S2 — `Advance` against a fake `Source`. 3. S3 — file source. 4. S4 — SQL source. 5. S5 — positions.
  Recovery boundary: after S2.
  Failure handling: `pg_current_logfile` returns NULL (collector off or format not enabled) → `Current` returns `ErrNoLogFile`; the caller turns it into a coverage note.
- **Unit tests:** `TestPGL009` (first run starts at the end: zero bytes read), `TestPGL010` (rotation: rest of the old file, then the new one from 0), `TestPGL011` (truncation restarts at 0), `TestPGL012` (file source refuses `../x` and an absolute name), `TestPGL013` (positions survive save and load; a broken file gives an empty map), `TestPGL014` (budget: with 3 MiB unread and budget 1 MiB exactly 1 MiB is returned; with 20 MiB unread the reader jumps to the end and `skipped` is the gap; guards I-9). Gate G-GO-FOCUS.
- **e2e tests:** `TestINTPGL001` (`./internal/pglog/`, integration): on PG 18.6 with `jsonlog`, the SQL source as a superuser reads the bytes the server wrote; as a `pg_monitor` role without the grant it returns `ErrLogReadDenied`. Gate G-GO-INT.
- **Done:** tests pass; design note approved and kept in the unit record; reviewed; unit closed and committed.

### 6.4 Agent loop, configuration, redaction, coverage
- **Agent:** agent-2
- **Assignment:** implement; agent-1 reviews the wiring plan before code and the diff after.
- **Files:** READ `cmd/pglens-agent/watch.go`, `ash.go`, `run.go`, `internal/agent/config.go` (`CheckConfig`, its validation), `internal/wire/factkinds.go`, `internal/check/query_redact.go`; NEW `cmd/pglens-agent/logs.go`, `logs_test.go`, `logs_integration_test.go`; WRITE `internal/agent/config.go` and tests, `internal/wire/factkinds.go` (kind `log_event`), `cmd/pglens-agent/run.go` (start the loop), `internal/agent/readonly_audit_integration_test.go` (G-RO covers the new statements), the agent example configuration file in the repository.
- **Change:**
  Preconditions: 6.3 DONE.
  Contract: `CheckConfig` gains `Source string` (`source`, `file`|`sql`, default `file`), `Path string` (`path`, log directory; default: discovered), `StatementText *bool` (`statement_text`, default false), `PlanParameters *bool` (`plan_parameters`, default false; used in phase 7), `MaxBytesPerInterval int64` (`max_bytes_per_interval`), `StatePath string` (`state_path`). They are valid only under `checks.logs`; on any other check they are a configuration error. `checks.logs.enabled` defaults to false; `interval` defaults to `15s`.
  Each interval the loop: (1) once per start and then hourly reads `SELECT current_setting('data_directory'), current_setting('log_directory'), current_setting('log_destination'), current_setting('logging_collector'), current_setting('log_timezone'), current_setting('lc_messages')`; format = `jsonlog` if listed, else `csvlog` if listed, else none; (2) `Advance`; (3) parse, classify, build facts and metrics per Local design context; (4) send one `wire.Result` with check `logs`; (5) save the position.
  Coverage notes (sent the way `check_skip` facts are sent by the watcher; copy that): `logs_no_structured_format` (neither format enabled or collector off), `logs_file_unreadable` (file source cannot open the file; the text names the directory), `logs_sql_denied` (`ErrLogReadDenied`; the text gives the `GRANT EXECUTE ON FUNCTION pg_read_binary_file(text,bigint,bigint,boolean)` statement), `logs_messages_not_english` (`lc_messages` is not `C`, `POSIX` or `en_*`), `logs_timezone_unknown`.
  The file source is used only when the target is host-local (same test the host loop uses); otherwise `logs_file_unreadable`.
  Steps: 1. S1 — wiring plan (which goroutine, how the result reaches the pusher, how notes are sent) to agent-1. 2. S2 — config and tests. 3. S3 — fact kind. 4. S4 — loop with injected `Source` and clock. 5. S5 — redaction and budgets. 6. S6 — G-RO extension, example config.
  Recovery boundary: after S2 and after S4.
- **Unit tests:** `TestPGL016` (config: defaults; `source: syslog` is an error; `source` under `checks.activity` is an error; budget outside the range is an error), `TestPGL017` (loop with a fake source and three lines: three `pg_log_events` values, facts only for shipped classes, position saved), `TestPGL018` (201 ERROR lines → 200 facts and `pg_log_events_dropped` = 1), `TestPGL019` (`statement_text` off → `ValueJSON` has only `message`; on → `statement` present), `TestPGL020` (a message containing `ALTER ROLE x PASSWORD 'secret'` is shipped as the withheld constant and the secret appears nowhere in the result; guards I-4), `TestPGL021` (format none → note `logs_no_structured_format` and no read). Gate G-GO-FOCUS `./cmd/pglens-agent/ ./internal/agent/ ./internal/wire/`.
- **e2e tests:** `TestINTPGL002` (`./cmd/pglens-agent/`): PG 18.6 with `jsonlog` and its log directory mounted into the test; a statement that fails with a unique violation produces one `log_event` fact with class `error.statement` and SQLSTATE `23505`. `TestINTPGL003`: same on PG 15.19 with `csvlog`. `TestINTRO001` still passes. Gates G-GO-INT, G-RO.
- **Done:** tests pass; default configuration reads nothing; reviewed; unit closed and committed.

### 6.5 Server storage and ingest
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** NEW `internal/store/migrations/0035_log_events.sql` (D19); WRITE `internal/store/write.go` (`LogEventRow`, `WriteLogEvents`), `internal/server/pipeline.go` (fact routing next to the `plan` and incident cases near line 527), `internal/server/retention.go` (family `log_events`, default 14), tests.
- **Change:**
  Preconditions: 6.4 DONE.
  Contract: migration:
  ```sql
  CREATE TABLE IF NOT EXISTS log_events (
    ts               timestamptz NOT NULL,
    tenant_id        text        NOT NULL DEFAULT 'default',
    cluster_id       bigint      NOT NULL,
    instance_id      uuid        NOT NULL,
    datname          text        NOT NULL DEFAULT '',
    usename          text        NOT NULL DEFAULT '',
    application_name text        NOT NULL DEFAULT '',
    pid              integer,
    session_id       text        NOT NULL DEFAULT '',
    line_num         bigint      NOT NULL DEFAULT 0,
    severity         text        NOT NULL,
    sqlstate         text        NOT NULL DEFAULT '',
    class            text        NOT NULL,
    queryid          bigint,
    message          text        NOT NULL,
    extra            jsonb,
    trace_id         text
  );
  SELECT create_hypertable('log_events', by_range('ts', INTERVAL '1 day'), if_not_exists => TRUE);
  CREATE UNIQUE INDEX IF NOT EXISTS log_events_dedup_idx ON log_events (instance_id, session_id, line_num, ts);
  CREATE INDEX IF NOT EXISTS log_events_class_idx ON log_events (tenant_id, instance_id, class, ts DESC);
  SELECT add_retention_policy('log_events', INTERVAL '14 days', if_not_exists => TRUE);
  ```
  (`trace_id` is filled from phase 11.) The pipeline turns each `log_event` fact into one row (`ts` from the label; a fact without a valid `ts`, `class` or `severity` is counted with `IncIngestRejected("invalid_fact")` and dropped), runs `redact.MayHoldCredential` again on `message` and every text of `extra` (a hit stores the withheld constant), and writes with `ON CONFLICT DO NOTHING`. The class must be one of `pglog.Classes()`. The facts are not written to the object facts table.
  Steps: 1. S1 — migration. 2. S2 — row and writer. 3. S3 — pipeline. 4. S4 — retention family.
- **Unit tests:** `TestPGL022` (fact → row mapping; unknown class rejected; missing `ts` rejected), `TestPGL023` (server-side withhold when the agent did not), `TestPGL024` (retention list contains `log_events` with default 14). Gate G-GO-FOCUS.
- **e2e tests:** `TestINTPGL004` (`./internal/server/`): push the same result twice → one row; retention policy exists with 14 days. Gate G-GO-INT.
- **Done:** tests pass; reviewed; unit closed and committed.

### 6.6 API, console, alert rules, readiness, documents
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** NEW `internal/server/api_logs.go` and tests, `internal/store/migrations/0035…` is closed — alert seeds go in a NEW migration (next free number, D19); NEW `web/src/lib/logs.ts`, `web/src/features/logs/LogsPage.tsx`, `LogClassSummary.tsx`, tests, `web/src/i18n/messages/{en,it}/logs.ts`, `web/src/lib/help/topics/{en,it}/logs.ts`; WRITE `api/openapi.yaml`, G-API outputs, `web/src/routes/index.tsx`, `web/src/components/layout/Sidebar.tsx`, `web/src/lib/ruleText.ts`, `web/src/i18n/domain/it/rules.ts`, `web/src/lib/help/topics/{en,it}/alert-rules.ts`, `internal/server/describe*.go`, `internal/i18n/locales/*.{en,it}.json` (describer texts), `internal/readiness/rules.go` and `readiness.{en,it}.json`, `README.md`.
- **Change:**
  Preconditions: 6.5 DONE.
  Contract:
  `GET /api/v1/logs?instance_id=&from=&to=&class=a,b&severity=&database=&q=&limit=&before=` — `limit` 1–500 (default 100); `q` is a plain substring matched with `position(lower($n) in lower(message)) > 0`; `before` is an RFC 3339 time for paging; order `ts DESC`. Row: `ts, class, severity, sqlstate, datname, usename, application_name, pid, queryid (string), message, extra, trace_id`.
  `GET /api/v1/logs/classes?instance_id=&from=&to=` → `[{"class","count","last_ts"}]` ordered by count.
  Alert seeds (copy the row shape of `0006_alerts.sql`): `log.panic` critical `pg_log_panic_events` `gt` 0 for 0 s, `log.corruption` critical `pg_log_corruption_events`, `log.disk_full` critical `pg_log_disk_full_events`, `log.out_of_memory` warning `pg_log_oom_events`; `ON CONFLICT DO NOTHING`. The describer adds to the notification of these four rules the three newest log messages of the matching classes (each cut to 300 characters) from `log_events`, read-only.
  Console: a `Logs` page per instance (route and sidebar entry registered the way the Locks page is): class summary with counts, filters (class, severity, database, text), table with expandable rows, link from a row with `queryid` to the query inspector, range picker shared with the other pages, empty state that names the agent setting `checks.logs.enabled` and the PostgreSQL settings `logging_collector = on`, `log_destination = 'jsonlog'`.
  Readiness: rule `RDY-LOG-01` (info) — the agent reports a `logs_*` coverage note; the text is the note and its fix.
  README: section "PostgreSQL logs" — what is read, the two sources, required settings, the grant for `sql`, what is never shipped, budgets, the class list.
  Steps: 1. S1 — API and OpenAPI. 2. S2 — seeds, describer, rule texts. 3. S3 — console page. 4. S4 — readiness. 5. S5 — help and README.
  Recovery boundary: after S1 and after S3.
- **Unit tests:** `TestPGL025` (parameter checks: limit 501, bad class name, bad time → 400), `TestPGL026` (`q` is bound, never spliced: a `q` of `%' OR 1=1 --` returns no row and no error), `TestPGL027` (describer adds the three newest messages; `en` and `it`), `TestPGL028` (`RDY-LOG-01` appears with a note and not without); `UI-PGL-001` (page renders rows and the class summary), `UI-PGL-002` (filter by class sends `class=`), `UI-PGL-003` (empty state names the settings), `UI-PGL-004` (Italian texts), `UI-PGL-005` (the four rules have texts in `ruleText`). Gates G-GO-FOCUS, G-WEB-STATIC, G-WEB-UNIT, G-API.
- **e2e tests:** `TestINTPGL005` (`./internal/server/`): seeded rows of three classes; `/logs?class=lock.deadlock` returns only that class; `/logs/classes` counts match. Gate G-GO-INT.
- **Done:** tests pass; README, help, readiness updated; reviewed; unit closed and committed.

## Phase gates and closure
- Required gates: G-GO-FULL, G-GO-INT (`TestINTPGL00[1-5]`), G-RO, G-API, G-WEB-FULL, G-I18N; assertions: I-1, I-4, I-9 tests green; the agent with default configuration opens no log file.
- README obligation: unit 6.6.
- Every sub-phase DONE or explicitly justified SKIPPED. Run full phase gates now.
- agent-1 reviews integration, the three invariants, the position rules, documentation.
- Open closure unit P6, record review and result, mark the phase DONE, close, commit `phase(007/P6): PHASE 6 COMPLETE — Log ingest; gates and review passed`.
- P6 edits no production code or tests. Blocked gate: P6 stays OPEN and BLOCKED. Failing gate or rejected review: attempt failed; reopen the responsible sub-phase, then rerun P6.
