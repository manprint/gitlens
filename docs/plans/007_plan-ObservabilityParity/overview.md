# ObservabilityParity — Plan overview

Authored 2026-11-21 by agent-1 (claude-fable-5-2). Folder: `docs/plans/007_plan-ObservabilityParity/`.
Execution starts at [STATE.md](STATE.md); all live readiness and progress are there.

## Goal and reference scenario

Close fourteen gaps between pglens and CloudWatch Database Insights / pganalyze. The row
numbers are those of the comparison table agreed with the user on 2026-11-21.

| Row | Feature | Phase |
|-----|---------|-------|
| 3 | Multixact wraparound rule | 1 |
| 1 | ASH dimensions `usename`, `client_addr`, `application_name` | 2 |
| 2 | Notification channels: Microsoft Teams, PagerDuty, email | 3 |
| 4 | Period A vs period B comparison on top SQL and waits | 4 |
| 10 | Rollups for retention beyond 365 days | 5 |
| 6 | PostgreSQL log ingest with event classification | 6 |
| 7 | Automatic plans from `auto_explain`, viewer, samples with parameters | 7 |
| 9 | Per-table autovacuum activity history | 8 |
| 8 | Plan-node insights, plan statistics, plan comparison | 9 |
| 12 | Index suggestions validated with `hypopg` | 10 |
| 13 | Query to service link (query tags, trace id) | 11 |
| 14 | Per-process OS metrics | 12 |
| 16 | Tuning workbook (`pg_hint_plan`, variants, parameters) | 13 |
| 5 | MCP server over the API | 14 |

Out of scope (user decision): row 11 users/roles/SSO, row 15 PgBouncer view, row 17 saved
fleet views and cross-account. Also out of scope: deployment to the real VMs, any push.

Reference scenario (proved at the PLAN closure, gate G-FINAL): on the lab stack an operator
1. groups Activity (ASH) by user, client address, application and service;
2. receives a firing and a resolved alert on local Teams and email test endpoints (PagerDuty is proved by its tests: its endpoint is the real service and the lab never calls it);
3. sees a finding `txn.multixact_wraparound_risk` when the metric crosses the threshold;
4. compares yesterday with today on top SQL and waits;
5. reads hourly rollups for a range older than the raw retention;
6. opens Logs, filters by class, and sees `auto_explain` plans and autovacuum runs that came from the log;
7. validates a suggested index and reads cost before and after;
8. runs a workbook variant with one planner setting and compares the two plans;
9. calls `tools/list` and `tools/call` on `/mcp` with an API token and gets the same data as the REST API.

## Decisions

All decisions below are `autonomous` (agent-1, 2026-11-21) unless the source says `user`.

| ID | Decision and consequence | Authority/source | Supersedes |
|----|--------------------------|------------------|------------|
| D1 | Scope is rows 1–10, 12, 13, 14, 16; rows 11, 15, 17 are excluded. | user, 2026-11-21 | — |
| D2 | Roster: agent-1 `claude-fable-5-2` supervises and reviews; agent-2 `claude-haiku-5-5` implements. Options: Haiku (user's default for mechanical work, matches `executor-power:low`) or Sonnet. Risk: Haiku misreads a complex unit. Guard: elementary units, fixed contracts, agent-1 review before design on flagged units and at every P<N>. Reversal: change the roster with an Execute prefix. | autonomous | — |
| D3 | Order is by dependency and cost; every phase ships alone and leaves all gates green. | autonomous | — |
| D4 | ASH dimensions are three text columns with `''` for "unknown"; `client_addr` is `host(client_addr)`; the cap of 100 keys per 10 s window stays; the agent key `checks.ash.dimensions` (list, default all three) can drop any of them. Risk: cardinality; guard: existing cap with conserved overflow bucket (I-2). | autonomous | — |
| D5 | New channel kinds `teams`, `pagerduty`, `email` keep the model "one address per kind". Teams address is a Workflows webhook URL. PagerDuty address is the 32-character routing key; the endpoint is fixed. Email address is a recipient list; SMTP settings come only from the environment (`PGLENS_SMTP_*`); without them the kind answers 409. No new Go dependency (`net/smtp`). Rejected: an SMTP library (dependency), SMTP settings in the console (secret storage). | autonomous | — |
| D6 | Multixact rule: metric `pg_max_datminmxid_age`, rule `txn.multixact_wraparound_risk`, severity critical, threshold 1 000 000 000 (same as the XID rule). | autonomous | — |
| D7 | Comparison is two new read endpoints (`/api/v1/statements/compare`, `/api/v1/ash/compare`) and one console view; each period is at most 31 days and inside the retention. | autonomous | — |
| D8 | Rollups are TimescaleDB continuous aggregates at 1 hour: `metrics_1h`, `metrics_statements_1h`, `metrics_ash_1h`. Default retention 730 days, maximum 1825. The raw maximum stays 365. A read whose range starts before the raw horizon uses the rollup and says so (`resolution: "1h"`). Rejected: a second raw copy, daily rollups only. | autonomous | — |
| D9 | Logs are read by the agent. Source `file` (default, agent on the database host) or `sql` (opt-in, needs `EXECUTE` on `pg_read_binary_file(text,bigint,bigint,boolean)`; bytes, so a multi-byte character is never cut by the server). Formats `jsonlog` and `csvlog`; plain `stderr` text is reported as a coverage note, not parsed. Reading starts at the end of the file; positions persist in a state file. Default ships `message` only; `statement`, `detail`, `context` need `checks.logs.statement_text: true`. Every shipped text passes `redact.MayHoldCredential`. Risk: literals in messages; guard: I-4 and its tests. Rejected: server-side reading (no file access), syslog. | autonomous | — |
| D10 | Automatic plans come from `auto_explain` log lines with `auto_explain.log_format=json`. The agent never changes a server setting. Samples keep duration and time; parameters ship only with `checks.logs.plan_parameters: true` (default false). | autonomous | — |
| D11 | Autovacuum history is parsed from `automatic vacuum` / `automatic analyze` log lines into `autovacuum_runs`. It needs `log_autovacuum_min_duration >= 0`; readiness reports `-1`. | autonomous | — |
| D12 | Plan insights are computed in the console (`web/src/lib/planTree.ts`), as today. Six new insights (phase 9). Plan statistics come from `plan_samples`. | autonomous | — |
| D13 | Index validation needs the `hypopg` extension in the target database. Without it the command answers `hypopg_missing` and the suggestion stays heuristic. The command carries a structured index (schema, relation, columns, include), never SQL text. Rejected: creating a real index, planner-cost guess without `hypopg`. | autonomous | — |
| D14 | Service link: sqlcommenter tags are parsed from the trailing comment of `pg_stat_activity.query`. ASH gains `service` (`application` tag) and `endpoint` (`route`, else `controller#action`). The trace id (from `traceparent`) is kept only on log events and plan samples; a link template `PGLENS_TRACE_URL_TEMPLATE` with `{trace_id}` turns it into a link. No OpenTelemetry SDK. Risk: comment cut by `track_activity_query_size`; guard: documented, empty tag. | autonomous | — |
| D15 | Per-process metrics: top 20 PostgreSQL processes by CPU and by RSS per host sample, read from `/proc`, joined to `pg_stat_activity` by pid; stored in `metrics_processes`, retention 7 days; only when the agent is host-local. | autonomous | — |
| D16 | Workbook variants change only: a whitelist of planner settings applied with `SET LOCAL`, a `pg_hint_plan` hint string, and a parameter set. The agent never runs `LOAD`; hints are refused with `hint_plan_inactive` when `current_setting('pg_hint_plan.enable_hint', true)` is NULL. The command still carries no SQL text. | autonomous | — |
| D17 | MCP is a stdlib JSON-RPC handler at `POST /mcp` (Streamable HTTP, JSON responses only, no SSE, no session). Protocol versions `2025-11-25` and `2025-06-18`. Tools are read-only and call the existing REST handlers in-process with the caller's credential. Off unless `PGLENS_MCP_ENABLED=true`. Rejected: an MCP SDK (dependency), a separate binary. | autonomous | — |
| D18 | Every new console text exists in `en` and `it`; every new Go text in `internal/i18n/locales/*.en.json` and `*.it.json`. | repo rule (CLAUDE.md) | — |
| D19 | Migration numbers: a unit takes the next free number at open and records it in STATE.md §6. The numbers in the phase files (0032–0041) are the expected ones; unit 6.6 adds one more migration (alert rule seeds) at the next free number, so later files may shift by one. The `trace_id` columns are created with their tables (0035, 0036); 0039 adds only the ASH columns. | autonomous | — |
| D20 | Baseline has 403 uncommitted paths of plan 006. Units stage only their own hunks (PROTOCOL.md step 7). Recommended before Execute: commit the plan 006 work. Risk if not: slow, error-prone staging on shared files. | autonomous | — |

## Open questions

None.

## Architecture

```
PostgreSQL ── pg_stat_activity ─┐                       ┌─ TimescaleDB (hypertables, caggs)
           ── log files ────────┤  agent (read-only)    │
/proc ──────────────────────────┤  checks → wire.Result ─→ server pipeline → store
commands (explain, index_check) ┘                       └─ API /api/v1/* ─→ console (web/)
                                                              └─ /mcp (JSON-RPC, same handlers)
```

- Agent side: new packages `internal/pglog` (parse, classify, sources), `internal/sqltag`
  (sqlcommenter), `internal/host` process collector; new check `logs`; new command kind
  `index_check`; `explain` gains `settings` and `hints`.
- Wire: new fact kinds `log_event`, `autovacuum_run`, `host_process`; `plan` facts gain
  optional labels `source`, `duration_ms`, `trace_id`. New ASH labels.
- Server: migrations 0032–0041; new tables `log_events`, `plan_samples`, `autovacuum_runs`,
  `metrics_processes`, `workbooks`, `workbook_variants`; three continuous aggregates.
- Console: new views under existing features (`ash`, `queries`, `indexes`, `instance`,
  `settings`, `alerts`) plus `features/logs` and `features/workbook`.

## Interfaces and compatibility

| Surface | Exact name/type | Default | Errors/conflicts | Compatibility |
|---------|-----------------|---------|------------------|---------------|
| Agent config | `checks.ash.dimensions: [usename, client_addr, application_name]` | all three | unknown name: config error | old agents send no label; server stores `''` |
| Agent config | `checks.logs.{enabled, source, path, statement_text, plan_parameters, max_bytes_per_interval, state_path}` | `enabled: false`, `source: file` | bad source: config error | absent key = off |
| Agent config | `host.processes.{enabled, top_n}` | `true`, `20` | `top_n` 1–100 | absent = default |
| API | `GET /api/v1/ash`, `/ash/top`: `group_by` adds `usename`, `client_addr`, `application_name`, `service`, `endpoint`; same names as filters | — | 400 on unknown value | additive |
| API | `GET /api/v1/statements/compare`, `GET /api/v1/ash/compare` | — | 400 bad range, 404 unknown instance | new |
| API | `NotificationChannelKind` adds `teams`, `pagerduty`, `email` | — | 400 bad address, 409 `smtp_not_configured` | additive enum |
| API | `GET /api/v1/logs`, `/logs/classes`, `/plans/samples`, `/plans/stats`, `/autovacuum/runs`, `/autovacuum/summary`, `/instances/{id}/host/processes`, `/instances/{id}/host/processes/top`; `DELETE /api/v1/plans/samples/parameters`; `/api/v1/workbooks*` (CRUD and variant run); index validation uses the existing `POST /api/v1/instances/{id}/commands` with kind `index_check`, and `/instances/{id}/index-suggestions` gains `validation` | — | per phase | new |
| API | `POST /mcp` | off | 404 when disabled, 401 no credential, 403 bad Origin, 405 on GET, 415 wrong content type, 413 body over 1 MiB, 400 unsupported `MCP-Protocol-Version` | new |
| Server env | `PGLENS_SMTP_HOST/PORT/USERNAME/PASSWORD/FROM/TLS`, `PGLENS_MCP_ENABLED`, `PGLENS_MCP_ALLOWED_ORIGINS`, `PGLENS_TRACE_URL_TEMPLATE` | unset | invalid value: start-up error | unset = feature off |
| Schema | migrations 0032–0041, all additive | — | — | forward only, idempotent (`IF NOT EXISTS`) |
| Command | kind `index_check`; `Args` adds `IndexSchema`, `IndexRelation`, `IndexColumns`, `IndexInclude`, `Settings`, `Hints` | — | `Validate` errors | old agents reject the unknown kind; server shows the error |

## Phase map

| Logical phase | File | Sub-phase IDs | Depends on | Assignment |
|---------------|------|---------------|------------|------------|
| 0 Baseline and test images | [phase_01.md](phase_01.md) | 0.1, 0.2 | none | agent-2 |
| 1 Multixact rule | [phase_02.md](phase_02.md) | 1.1–1.3 | 0 | agent-2 |
| 2 ASH dimensions | [phase_03.md](phase_03.md) | 2.1–2.5 | 0 | agent-2 |
| 3 Notification channels | [phase_04.md](phase_04.md) | 3.1–3.4 | 0 | agent-2 |
| 4 Period comparison | [phase_05.md](phase_05.md) | 4.1–4.3 | 2 | agent-2 |
| 5 Rollups | [phase_06.md](phase_06.md) | 5.1–5.4 | 2 | agent-2 |
| 6 Log ingest | [phase_07.md](phase_07.md) | 6.1–6.6 | 0 | agent-2 |
| 7 Automatic plans | [phase_08.md](phase_08.md) | 7.1–7.4 | 6 | agent-2 |
| 8 Autovacuum history | [phase_09.md](phase_09.md) | 8.1–8.3 | 6 | agent-2 |
| 9 Plan insights and statistics | [phase_10.md](phase_10.md) | 9.1–9.3 | 7 | agent-2 |
| 10 Validated index suggestions | [phase_11.md](phase_11.md) | 10.1–10.4 | 0 | agent-2 |
| 11 Query to service link | [phase_12.md](phase_12.md) | 11.1–11.4 | 2, 6, 7 | agent-2 |
| 12 Per-process OS metrics | [phase_13.md](phase_13.md) | 12.1–12.4 | 0 | agent-2 |
| 13 Tuning workbook | [phase_14.md](phase_14.md) | 13.1–13.4 | 9 | agent-2 |
| 14 MCP server | [phase_15.md](phase_15.md) | 14.1–14.4 | 1–13 | agent-2 |
| 15 Integration and documents | [phase_16.md](phase_16.md) | 15.1–15.3 | 1–14 | agent-2 |

No live status in this table.

## Reuse map

| Need | Path and symbol | Contract to preserve |
|------|-----------------|----------------------|
| ASH sampling | `internal/ash/sampler.go` — `SampleRow`, `sampleBatch` | filters own sessions (`pglens/%`), holds a tick until it reads cleanly |
| ASH aggregation | `internal/ash/aggregator.go` — `Key`, `Add`, `capConserved`, `keyLess` | total samples conserved when the cap cuts keys |
| ASH emit | `cmd/pglens-agent/ash.go` — `ashWindowToMetrics`, `ashMaxKeys` | metric `ash_samples`, existing labels unchanged |
| ASH store | `internal/store/write.go` — `ASHRow`, `WriteASH`, `(*Writer).WriteASH`; `internal/server/pipeline.go` — `buildASHRow` | `ON CONFLICT DO NOTHING`, explicit column lists in both writers |
| ASH API | `internal/server/api_ash.go` — `group_by` whitelist, `ashFilters` | 400 on unknown `group_by` |
| Channels | `internal/server/notification_channels.go` — `notificationKind`, `notificationKinds`, `validateWebhookURL`, `notificationHTTPClient`; `internal/alert/notify` — `Channel`, `Message`, `sendJSON`, `Slot` | no redirects; store write before slot swap; address never returned (only `host`) |
| Metric rule | `internal/advisor/rules_query.go` — `metricRule`, `wraparoundAge` | finding texts through `Finding.say` and locale catalogs |
| Database-wide metric | `internal/check/activity.go` — `pg_max_datfrozenxid_age` query | one scalar query, `COALESCE(..., 0)::float8` |
| Fact kinds | `internal/wire/factkinds.go` — `factKinds`, `FactKind` | unknown kind is dropped by the agent and refused by the server |
| Incident to event | `cmd/pglens-agent/watch.go` — `incidentResult`; pipeline handling of `incident` | events table `events(type, payload)` |
| Plans | `internal/agent/exec_explain.go` — `planHash`, `ExplainExecutor`; `internal/store/write.go` insert into `query_plans`; `internal/server/api_plans.go` | dedup on `(tenant, instance, datname, queryid, plan_hash, analyzed)` |
| Explain safety | `internal/agent/explain_guard.go` — `dropExplainPrivileges`, `guardPlanning`, `discardConn` | read-only transaction, privileges dropped, connection discarded |
| Command types | `internal/command/types.go` — `Kind`, `Args`, `Args.Validate`, `knownKind` | "queryid and options, never SQL text" |
| Redaction | `internal/redact/redact.go` — `MayHoldCredential`; migrations 0020, 0021 | texts that may hold a credential are never stored |
| Retention | `internal/server/retention.go` — `retentionFamilies`, `retentionMaxDays`, `policyStore` | whitelist only; optimistic replace |
| Host metrics | `internal/host/collector.go` — `Collector`, `Sample`; `internal/agent/config.go` — `HostConfig` | `proc_path` override for tests |
| Plan tree | `web/src/lib/planTree.ts` — `parsePlan`, `PlanInsight`, `diffPlans` | insight titles through `translate('queries.plan.insight.*')` |
| Index advice | `internal/indexadvice/suggest.go`, `internal/server/index_suggestions.go` | suggestion shape of `/api/v1/index-suggestions` |
| Auth | `internal/server/session.go` — `RequireCredential` | bearer token or session cookie |
| Migrations | `internal/store/migrate.go` | each file runs in one transaction; forward only |

## Research and evidence

Scope: external contracts the plan depends on. Network authorization: the user's global
instructions allow outbound internet from this sandbox (CLAUDE.md, in force 2026-11-21).
All sources accessed 2026-11-21. Local probes ran in throwaway Docker containers, removed after use.

| ID | Question / required or optional | Evidence state | Affected decisions / units / tests |
|----|---------------------------------|----------------|------------------------------------|
| R1 | PagerDuty Events API v2 payload — required | CONFIRMED | D5, 3.2, NCH-010…013 |
| R2 | Teams Workflows webhook payload — required | CONFIRMED (card version is an inference) | D5, 3.1, NCH-001…004 |
| R3 | `jsonlog` and `csvlog` fields, log file discovery, read permissions — required | CONFIRMED | D9, 6.1, 6.3, PGL-* |
| R4 | `auto_explain` log line format — required | CONFIRMED | D10, 7.1, AEX-* |
| R5 | Autovacuum and analyze log line format on PG 15 and 18 — required | CONFIRMED | D11, 8.1, AVH-* |
| R6 | `mxid_age(datminmxid)` — required | CONFIRMED | D6, 1.1, MXW-* |
| R7 | Continuous aggregates and column adds on a compressed hypertable inside a transaction — required | CONFIRMED | D4, D8, 2.3, 5.1, 11.2 |
| R8 | `hypopg` in a read-only transaction with a `pg_monitor` role; scope of a hypothetical index — required | CONFIRMED | D13, I-6, 10.2, IDXV-* |
| R9 | `pg_hint_plan` activation and hint syntax — required | ACCEPTED (autonomous) for the test image build; rest CONFIRMED | D16, 0.2, 13.2, WBK-* |
| R10 | MCP Streamable HTTP and lifecycle — required | CONFIRMED for 2025-06-18 and 2025-11-25 | D17, 14.1–14.3, MCP-* |
| R11 | sqlcommenter comment format — required | CONFIRMED | D14, 11.1, QTAG-* |
| R12 | `/proc/<pid>` files for per-process metrics — required | CONFIRMED (local, Linux 7.0) | D15, 12.1, PROC-* |

### R1 — PagerDuty Events API v2
- Sources: https://developer.pagerduty.com/docs/send-alert-event (Endpoint, Parameters).
- Documented fact: `POST https://events.pagerduty.com/v2/enqueue`; `routing_key` required, 32 characters; `event_action` required: `trigger`, `acknowledge`, `resolve`; `dedup_key` optional, at most 255 characters; `payload.summary`, `payload.source`, `payload.severity` required for a trigger; severity is `critical`, `error`, `warning`, `info`; optional `payload.timestamp`, `component`, `group`, `class`, `custom_details`.
- Supervisor inference: success status is `202`; any 2xx is accepted by the channel.
- Decision impact: 3.2 payload; `dedup_key = Message.DedupID`; `Phase == "resolved"` sends `resolve`.
- Unresolved/replacement: none.

### R2 — Teams Workflows webhook
- Sources: https://learn.microsoft.com/en-us/microsoftteams/platform/webhooks-and-connectors/how-to/add-incoming-webhook (sample code, limitations).
- Documented fact: body `{"type":"message","attachments":[{"contentType":"application/vnd.microsoft.card.adaptive","content":{"type":"AdaptiveCard","$schema":"http://adaptivecards.io/schemas/adaptive-card.json","version":"1.0","body":[{"type":"TextBlock","text":"…"}]}}]}`, `Content-Type: application/json`. Workflows webhooks accept Adaptive Card and MessageCard payloads.
- Supervisor inference: `version: "1.4"` with `TextBlock` (`wrap`) and `FactSet` renders in current Teams; both elements exist since 1.0.
- Decision impact: 3.1 payload; the URL passes `validateWebhookURL`.
- Unresolved/replacement: none.

### R3 — PostgreSQL log formats and access
- Sources: https://www.postgresql.org/docs/18/runtime-config-logging.html (19.8.4, 19.8.5); https://www.postgresql.org/docs/18/functions-admin.html (generic file access); https://www.postgresql.org/docs/18/functions-info.html (`pg_current_logfile`).
- Documented fact: `jsonlog` needs `logging_collector=on`; null fields are omitted; more fields may be added. `csvlog` columns in order: log_time, user_name, database_name, process_id, connection_from, session_id, session_line_num, command_tag, session_start_time, virtual_transaction_id, transaction_id, error_severity, sql_state_code, message, detail, hint, internal_query, internal_query_pos, context, query, query_pos, location, application_name, backend_type, leader_pid, query_id. `pg_current_logfile(text)` returns the path of the file in use, relative to the data directory.
- Local check (postgres:18.6-alpine and 15.19-alpine, `log_destination=jsonlog`): keys seen `timestamp, user, dbname, pid, remote_host, session_id, line_num, ps, session_start, vxid, txid, error_severity, state_code, message, hint, statement, application_name, backend_type, query_id`. A role in `pg_monitor` can call `pg_current_logfile('jsonlog')` and `pg_ls_logdir()`; `pg_read_file` answers `permission denied for function pg_read_file`; the same page documents `pg_read_binary_file` with the same default (superuser only, `EXECUTE` can be granted), which is the function unit 6.3 uses.
- Decision impact: D9; unknown JSON keys are ignored; source `sql` needs an explicit grant.
- Unresolved/replacement: none.

### R4 — `auto_explain`
- Sources: https://www.postgresql.org/docs/18/auto-explain.html.
- Documented fact: `auto_explain.log_format` is `text` (default), `xml`, `json`, `yaml`; `auto_explain.log_parameter_max_length` (`-1` full, `0` off) controls parameter logging; all settings are superuser-only.
- Local check (18.6, `log_format=json`, `jsonlog`): the line's `message` is `duration: 0.007 ms  plan:\n{\n  "Query Text": "select 1",\n  "Plan": {…}\n}`, `error_severity` `LOG`, `query_id` is set on the line.
- Decision impact: 7.1 recogniser `^duration: ([0-9.]+) ms  plan:\n`, the rest is one JSON object with keys `Query Text`, `Plan`, optional `Query Parameters`.
- Unresolved/replacement: none.

### R5 — Autovacuum log lines
- Sources: local probe, PostgreSQL 15.19 and 18.6 (`log_autovacuum_min_duration=0`).
- Local result, 15.19: `automatic vacuum of table "postgres.public.t": index scans: 1\npages: 88 removed, 1 remain, 89 scanned (100.00% of total)\ntuples: 19900 removed, 100 remain, 0 are dead but not yet removable\nremovable cutoff: 739, which was 1 XIDs old when operation ended\n…\nbuffer usage: 838 hits, 2 misses, 150 dirtied\nWAL usage: 433 records, 147 full page images, 592466 bytes\nsystem usage: CPU: user: 0.00 s, system: 0.00 s, elapsed: 0.04 s`.
- Local result, 18.6: same first line; `pages:` line ends with `, 0 eagerly scanned`; `buffer usage: 846 hits, 0 reads, 150 dirtied`; `WAL usage: 527 records, 151 full page images, 1313332 bytes, 0 buffers full`. Analyze: `automatic analyze of table "db.schema.rel"\n…\nsystem usage: CPU: user: 0.00 s, system: 0.00 s, elapsed: 0.01 s`.
- Supervisor inference: the prefixes `automatic aggressive vacuum of table` and `automatic vacuum to prevent wraparound of table` use the same body (PostgreSQL source, `vacuumlazy.c`); each line is parsed on its own and a missing line leaves its fields NULL.
- Decision impact: 8.1 per-line regular expressions; both spellings `misses|reads`.
- Unresolved/replacement: none.

### R6 — Multixact age
- Sources: https://www.postgresql.org/docs/18/functions-info.html (`mxid_age ( xid ) → integer`); https://www.postgresql.org/docs/18/routine-vacuuming.html#VACUUM-FOR-MULTIXACT-WRAPAROUND.
- Local check: `SELECT max(mxid_age(datminmxid)) FROM pg_database` returns `0` on a fresh 18.6.
- Decision impact: 1.1 query.

### R7 — TimescaleDB 2.31.0-pg17
- Sources: local probe on `timescale/timescaledb:2.31.0-pg17`; https://docs.tigerdata.com/api/latest/continuous-aggregates/create_materialized_view/.
- Local result: on a hypertable with compressed chunks, inside one transaction, `ALTER TABLE … ADD COLUMN … text NOT NULL DEFAULT ''` (three columns), `DROP INDEX` and `CREATE UNIQUE INDEX` succeed and old rows read `''`. Inside one transaction `CREATE MATERIALIZED VIEW … WITH (timescaledb.continuous) … WITH NO DATA`, `add_continuous_aggregate_policy`, `add_retention_policy` succeed; `CALL refresh_continuous_aggregate` (outside a transaction) fills the view.
- Decision impact: 2.3, 5.1, 11.2 migrations run inside the migration transaction; the first refresh is done by the policy, not by the migration.

### R8 — `hypopg`
- Sources: https://hypopg.readthedocs.io/en/rel1_stable/usage.html; local probe, hypopg 1.4.5 built on postgres:18.6-alpine (`apk add build-base`, `make USE_PGXS=1 with_llvm=no install`).
- Local result: a `pg_monitor` role inside `BEGIN TRANSACTION READ ONLY` runs `SELECT indexrelid, indexname FROM hypopg_create_index('CREATE INDEX ON public.h USING btree (id)')` and the next `EXPLAIN` uses `Index Scan using "<13674>btree_h_id"`. `SELECT … FROM hypopg_list_indexes` answers `permission denied for view hypopg_list_indexes`. After `ROLLBACK` the hypothetical index still exists in the session.
- Decision impact: I-6 — the executor calls `hypopg_reset()` before and after, and discards the connection; size comes from `hypopg_relation_size(indexrelid)`, never from the view.

### R9 — `pg_hint_plan`
- Sources: https://github.com/ossc-db/pg_hint_plan `docs/installation.md`, `docs/hint_details.md` (master); tags `REL18_1_8_1`, `REL15_1_5_4` exist.
- Documented fact: no `CREATE EXTENSION` needed; activated by `LOAD 'pg_hint_plan'` or a preload setting. Hints are read only from the first block comment `/*+ … */`; hint parsing stops at any character other than letters, digits, spaces, underscores, commas, parentheses.
- Local result: a non-superuser `LOAD 'pg_hint_plan'` answers `access to library "pg_hint_plan" is not allowed`. The build on postgres:18.6-alpine with only `build-base` fails: `/usr/bin/flex: No such file or directory`.
- ACCEPTED (autonomous), agent-1, 2026-11-21: the test image builds once `flex` and `bison` are added. Risk: it still fails. Guard: unit 0.2 builds the image; on failure the hint integration test WBK-INT-003 is skipped with a recorded reason and hints stay covered by unit tests WBK-004…006.
- Decision impact: D16.

### R10 — MCP
- Sources: https://modelcontextprotocol.io/specification/2025-06-18/basic/transports; https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle.
- Documented fact: one MCP endpoint path for POST and GET; the server must validate `Origin`; a POST carries one JSON-RPC message; for a request the server answers `Content-Type: application/json` with one JSON object, or an SSE stream; for a notification or response it answers `202 Accepted` with no body; a server without an SSE stream answers `405 Method Not Allowed` to GET; the client sends `MCP-Protocol-Version` after initialization; if the server supports the requested version it answers the same one, else another one it supports.
- Applicability: a `2026-07-28` revision is listed on the site and was not evaluated. A client that asks for it receives `2025-11-25` by the negotiation rule.
- Decision impact: D17.

### R11 — sqlcommenter
- Sources: https://google.github.io/sqlcommenter/spec/.
- Documented fact: one comment at the end of the statement: `/*key='value',key2='value2'*/`; keys and values are URL-encoded, values in single quotes with `'` escaped by a backslash, pairs sorted by key and joined by commas. Example keys: `action`, `controller`, `framework`, `route`, `application`, `db_driver`, `traceparent`, `tracestate`.
- Decision impact: 11.1 parser and its table tests.

### R12 — `/proc`
- Sources: proc(5) on the build host; existing `internal/host/collector.go` (gopsutil with path overrides).
- Fact: `/proc/<pid>/stat` fields 14 and 15 are user and system CPU ticks; `/proc/<pid>/status` has `VmRSS`; `/proc/<pid>/io` has `read_bytes` and `write_bytes` and is readable only by the same user or root; `/proc/<pid>/cmdline` of a backend starts with `postgres: `.
- Decision impact: 12.1; unreadable `io` leaves the I/O fields NULL.

## Invariants

| ID | Meaning | Guarding test |
|----|---------|---------------|
| I-1 | The agent stays read-only on the monitored database: no DDL, no DML, no `LOAD`, no setting change outside `SET LOCAL` in its own read-only transaction. | `TestINTRO001` (G-RO) extended in 6.4, 10.2, 13.2 |
| I-2 | ASH sample totals are conserved when the key cap cuts keys. | `TestASHD003` |
| I-3 | A command carries queryid and options, never SQL text. | `TestIDXV002`, `TestWBK002` |
| I-4 | A text that may hold a credential is never shipped or stored. | `TestPGL020`, `TestAEX006`, `TestINTAEX003` |
| I-5 | Locale parity: every key exists in every locale with the same placeholders. | `TestCatalogParity`, `make i18n-check` |
| I-6 | No hypothetical index outlives its command. | `TestINTIDXV002` |
| I-7 | Notification addresses and SMTP credentials are never returned by the API or written to a log. | `TestNCH005`, `TestNCH014`, `TestNCH024` |
| I-8 | The MCP endpoint exposes read tools only and enforces the same credential as the REST API. | `TestMCP006`, `TestMCP010` |
| I-9 | Log reading is bounded: at most `max_bytes_per_interval` per interval, lines over 256 KiB are cut. | `TestPGL014`, `TestPGL015` |

## Risks

| Risk | Design mitigation | Verification |
|------|-------------------|--------------|
| Dirty baseline (D20) | own-hunk staging; recommendation to commit plan 006 first | P0 records the dirty paths |
| Cardinality of new ASH dimensions | cap 100 keys per window, dimensions can be turned off | ASHD-003, ASHD-004 |
| Secrets in logs and plans | off by default, credential filter, purge migration pattern | I-4 tests |
| Log volume | byte budget per interval, start at end of file | I-9 tests |
| Old agents with a new server and the reverse | additive labels and kinds; defaults `''` | ASHD-INT-002, IDXV-006 |
| Extension missing (`hypopg`, `pg_hint_plan`, `auto_explain`) | explicit error codes and help texts | IDXV-005, WBK-005, AEX-005 |
| Low-power executor on large units | elementary units, agent-1 review before design where flagged | review records in STATE.md §7 |

## Verification strategy

STATE.md §3 is the gate registry. Each unit runs its focused Go or web gate with its test
IDs (zero discovery fails). Each phase closure runs G-GO-FULL, G-WEB-FULL when the phase
touches `web/`, G-API when it touches `api/openapi.yaml`, G-GO-INT for the phase's
integration IDs, G-RO when the agent changed, G-I18N when texts changed. The PLAN closure
runs G-FINAL: all full gates plus the reference scenario on the lab stack (unit 15.2).

## Agent-assignment summary

Executor power: low — 60 elementary sub-phases; each names its files, symbols, contract,
steps and test oracle; the executor chooses nothing about interfaces.

| Unit(s) | Implementer | Required supervisor review |
|---------|-------------|----------------------------|
| all sub-phases | agent-2 | agent-1 after diff |
| 2.3, 5.1, 6.3, 6.4, 7.3, 10.2, 11.2, 13.2, 14.2 | agent-2 | agent-1 before design and after diff |
| P0–P15, PLAN | agent-1 | closure review |
