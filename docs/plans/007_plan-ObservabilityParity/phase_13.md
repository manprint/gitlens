# Phase 12 — Per-process OS metrics

Intent: the console shows which PostgreSQL processes use CPU, memory and disk I/O on the database host, joined to the session they serve.
Prerequisites: P0.
Phase closure: P12; review by agent-1.

## State and ownership contract
Read STATE.md §1 first (and PROTOCOL.md once without this skill) for scope, ownership,
recovery, checks, and commit rules. Open before edits; checkpoint at a recovery boundary;
close with evidence. A delegated worker follows its assigned OPEN unit. Missing design goes
to agent-1, the supervisor. Executor power: low.

## Local design context
Plan revision: 1.
- D15, I-1. R12: `/proc/<pid>/stat` (the command name is field 2 in parentheses and can contain spaces and parentheses, so fields are counted from the last `)`; after it: field 4 = parent pid, 14 = user ticks, 15 = system ticks, 22 = start time in ticks), `/proc/<pid>/status` (`VmRSS: <n> kB`), `/proc/<pid>/io` (`read_bytes: <n>`, `write_bytes: <n>`; readable only by the process owner or root). One tick = 1/100 s (Linux `USER_HZ` is 100 on the supported platforms; stated in the README).
- Only host-local targets (the test the host loop already uses in `cmd/pglens-agent/run.go` near line 178 and `host_loop.go`).
- Which processes belong to a target: the postmaster is the parent of the agent's own backend. The loop asks `SELECT pg_backend_pid()` on its connection, reads field 4 of `/proc/<that pid>/stat` → postmaster pid. Processes of the target = the postmaster and every process whose parent pid is the postmaster. No file of the data directory is read.
- Sample of one process: `PID int`, `StartTicks uint64`, `CPUTicks uint64` (user + system), `RSSBytes uint64`, `ReadBytes, WriteBytes *uint64` (nil when `io` is unreadable).
- Rates need two samples of the same process (same pid and same start ticks): `cpu_ratio` = tick difference / 100 / seconds between samples (1.0 = one full core); `read_bytes_per_s`, `write_bytes_per_s` likewise. A process seen for the first time has no rates in that sample and is not shipped.
- Selection: the union of the `top_n` processes by `cpu_ratio` and the `top_n` by `RSSBytes` (so at most 2 × `top_n`), plus the postmaster.
- Join: `SELECT pid, COALESCE(backend_type,''), COALESCE(datname,''), COALESCE(usename,''), COALESCE(application_name,''), COALESCE(state,''), query_id FROM pg_stat_activity` in the same interval; a pid not listed gets `backend_type` = `postmaster` for the postmaster, else `other`.
- Fact: kind `host_process`, `Key` = `<pid>`, labels `pid`, `backend_type`, `datname`, `usename`, `application_name`, `state`, `queryid` (empty ones left out), `ValueJSON` = `{"cpu_ratio":…,"rss_bytes":…,"read_bytes_per_s":…,"write_bytes_per_s":…}` (I/O keys absent when unknown). The time is the result's time. The process command line is never shipped.
- Config: `host.processes.enabled` (default true, effective only when `host.enabled` and the target is host-local), `host.processes.top_n` (default 20, range 1–100).
- Migration 0040:
  ```sql
  CREATE TABLE IF NOT EXISTS metrics_processes (
    ts                timestamptz NOT NULL,
    tenant_id         text        NOT NULL DEFAULT 'default',
    instance_id       uuid        NOT NULL,
    cluster_id        bigint      NOT NULL,
    pid               integer     NOT NULL,
    backend_type      text        NOT NULL DEFAULT '',
    datname           text        NOT NULL DEFAULT '',
    usename           text        NOT NULL DEFAULT '',
    application_name  text        NOT NULL DEFAULT '',
    state             text        NOT NULL DEFAULT '',
    queryid           bigint,
    cpu_ratio         double precision NOT NULL,
    rss_bytes         bigint      NOT NULL,
    read_bytes_per_s  double precision,
    write_bytes_per_s double precision
  );
  SELECT create_hypertable('metrics_processes', by_range('ts', INTERVAL '1 day'), if_not_exists => TRUE);
  CREATE UNIQUE INDEX IF NOT EXISTS metrics_processes_dedup_idx ON metrics_processes (instance_id, pid, ts);
  SELECT add_retention_policy('metrics_processes', INTERVAL '7 days', if_not_exists => TRUE);
  ```

## Sub-phases

### 12.1 Process collector
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** READ `internal/host/collector.go` (`Collector`, `NewCollector`, how `procPath` is used); NEW `internal/host/process.go`, `process_test.go`, `internal/host/testdata/proc/` (a fake `/proc` tree: postmaster 100, backends 101–105 with parent 100, an unrelated process 200, one backend without a readable `io` file, one whose command name is `(post gres)`).
- **Change:**
  Preconditions: P0 DONE.
  Contract: `func (c *Collector) ParentPID(pid int) (int, error)`; `func (c *Collector) Processes(postmaster int) ([]ProcessSample, error)` (postmaster and its children, read from the collector's proc path; a process that disappears while being read is skipped without error); `func ProcessRates(prev, cur []ProcessSample, seconds float64) []ProcessRate` (only processes present in both with the same start ticks; negative differences are treated as "new process"); `func TopProcesses(rates []ProcessRate, topN int, postmaster int) []ProcessRate`. Reads use `os.ReadFile` on paths built from the proc path and a numeric pid only.
  Steps: 1. S1 — fake tree. 2. S2 — stat, status, io parsers. 3. S3 — the four functions.
- **Unit tests:** `TestPROC001` (stat line with command `(post gres)` → right parent and ticks), `TestPROC002` (`Processes(100)` returns 100–105 and not 200), `TestPROC003` (unreadable `io` → nil I/O fields, no error), `TestPROC004` (`ProcessRates`: 250 ticks over 5 s → `cpu_ratio` 0.5; a reused pid with new start ticks is dropped), `TestPROC005` (`TopProcesses` with `topN` 2 over 10 processes → union of the two top lists plus the postmaster, no duplicate). Gate G-GO-FOCUS `./internal/host/`.
- **e2e tests:** N/A — fake `/proc`; the real one is covered in 12.2.
- **Done:** tests pass; reviewed; unit closed and committed.

### 12.2 Agent wiring and configuration
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** READ `cmd/pglens-agent/host_loop.go`, `run.go` (`collectHostByDataDir`, `hostWireResult`); WRITE `cmd/pglens-agent/host_loop.go` and tests, `internal/agent/config.go` and tests (`HostConfig.Processes` with `Enabled *bool` and `TopN int`, validation, accessors), `internal/wire/factkinds.go` (kind `host_process`), `internal/agent/readonly_audit_integration_test.go`, the agent example configuration.
- **Change:**
  Preconditions: 12.1 DONE.
  Contract: each host interval, for each host-local target with processes enabled: postmaster pid (cached; asked again when the connection is new), `Processes`, rates against the previous sample of that target, `TopProcesses`, the join query, then one `wire.Result` with check name `host_processes` holding the facts. An error on the connection or on `/proc` skips this interval for the target and is logged once per error kind per hour (no log flood). The two SQL statements carry the `/* pglens: … */` comment style.
  Steps: 1. S1 — config. 2. S2 — fact kind. 3. S3 — loop with an injected collector and querier. 4. S4 — G-RO and example config.
- **Unit tests:** `TestPROC006` (config: default on and 20; `top_n: 0` and `101` are errors), `TestPROC007` (two intervals with the fake collector → facts with rates and joined labels; first interval ships nothing), `TestPROC008` (processes disabled → no query, no fact), `TestPROC009` (target not host-local → nothing). Gate G-GO-FOCUS `./cmd/pglens-agent/ ./internal/agent/ ./internal/wire/`.
- **e2e tests:** `TestINTPROC001` (`./cmd/pglens-agent/`): the test starts PostgreSQL 18.6 in a container that shares the test's PID namespace view through a mounted `/proc` of the container (`docker run --pid=host` is not allowed; instead read the container's processes from the host's `/proc`, which lists them when the test runs on the Docker host) — if the container's processes are not visible in the test's `/proc`, the test is skipped with the reason `container processes not visible in /proc` and the unit relies on `TestPROC007`; record which case happened. `TestINTRO001` passes. Gates G-GO-INT, G-RO.
- **Done:** tests pass or the documented skip is recorded; reviewed; unit closed and committed.

### 12.3 Server storage, ingest and API
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** NEW `internal/store/migrations/0040_metrics_processes.sql` (D19); WRITE `internal/store/write.go` (`ProcessRow`, `WriteProcesses`), `internal/server/pipeline.go`, `internal/server/retention.go` (family `metrics_processes`, default 7), `internal/server/api_host.go` (two handlers), tests, `api/openapi.yaml`, G-API outputs.
- **Change:**
  Preconditions: 12.2 DONE.
  Contract: one row per fact (`pid` integer > 0, `cpu_ratio` ≥ 0, `rss_bytes` ≥ 0, else rejected as `invalid_fact`); `ON CONFLICT DO NOTHING`.
  `GET /api/v1/instances/{id}/host/processes?at=&order_by=&limit=` → the sample set with the newest `ts` ≤ `at` (default now, and not older than 5 minutes before `at`; otherwise an empty list): `{"ts":…,"processes":[{pid, backend_type, datname, usename, application_name, state, queryid (string), cpu_ratio, rss_bytes, read_bytes_per_s, write_bytes_per_s}]}`; `order_by` `cpu` (default) | `rss` | `read` | `write`; `limit` 1–200, default 40.
  `GET /api/v1/instances/{id}/host/processes/top?from=&to=&group_by=&limit=` → per group (`backend_type` | `datname` | `usename` | `application_name`): `avg_cpu_ratio` (sum of `cpu_ratio` per sample time, averaged over the sample times in range), `max_rss_bytes`, `avg_read_bytes_per_s`, `avg_write_bytes_per_s`.
  Steps: 1. S1 — migration. 2. S2 — store and pipeline. 3. S3 — retention family. 4. S4 — handlers, OpenAPI, G-API.
- **Unit tests:** `TestPROC010` (fact → row; bad numbers rejected), `TestPROC011` (parameter checks of both handlers), `TestPROC012` (group maths from fake rows). Gate G-GO-FOCUS.
- **e2e tests:** `TestINTPROC002` (`./internal/server/`): two sample times; the snapshot returns the newer one ordered by CPU; `top?group_by=backend_type` averages over both. Gate G-GO-INT.
- **Done:** tests pass; reviewed; unit closed and committed.

### 12.4 Console panel, help, README
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** NEW `web/src/lib/processes.ts` and test, `web/src/features/instance/ProcessesPanel.tsx` and test; WRITE `web/src/features/instance/HostSection.tsx` and test, `web/src/i18n/messages/{en,it}/instance.ts`, `web/src/lib/help/topics/{en,it}/instance-overview.ts`, `README.md`.
- **Change:**
  Preconditions: 12.3 DONE.
  Contract: the Host section gets a `Processes` panel: a table of the newest sample (process type, database, user, application, state, CPU as percent of one core, memory, read/s, write/s, link to the query when `queryid` exists), sortable, with a group selector that switches to the `top` endpoint for the page's range. Unknown I/O shows `—` with the tooltip `The agent cannot read I/O counters of processes owned by another user`. Empty state names `host.processes.enabled` and "agent on the database host". Units through `useFormat()`.
  README section "Per-process metrics": requirements (agent on the host, same user or root for I/O), what is shipped, retention.
  Steps: 1. S1 — lib. 2. S2 — panel. 3. S3 — help and README.
- **Unit tests:** `UI-PROC-001` (table renders and sorts by CPU), `UI-PROC-002` (group selector calls `top` with `group_by`), `UI-PROC-003` (null I/O shows `—` and the tooltip), `UI-PROC-004` (empty state), `UI-PROC-005` (Italian texts). Gates G-WEB-STATIC, G-WEB-UNIT.
- **e2e tests:** N/A at unit level; lab check in 15.2.
- **Done:** tests pass; README and help updated; reviewed; unit closed and committed.

## Phase gates and closure
- Required gates: G-GO-FULL, G-GO-INT (`TestINTPROC00[12]`), G-RO, G-API, G-WEB-FULL, G-I18N; assertions: no command line or file path of a process in any fact.
- README obligation: unit 12.4.
- Every sub-phase DONE or explicitly justified SKIPPED. Run full phase gates now.
- agent-1 reviews parsing of `/proc`, I-1, the skip record of `TestINTPROC001`, documentation.
- Open closure unit P12, record review and result, mark the phase DONE, close, commit `phase(007/P12): PHASE 12 COMPLETE — Per-process OS metrics; gates and review passed`.
- P12 edits no production code or tests. Blocked gate: P12 stays OPEN and BLOCKED. Failing gate or rejected review: attempt failed; reopen the responsible sub-phase, then rerun P12.
