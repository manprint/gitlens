# Settings inventory (11.0 design contract)

Plan 005, revision 1. Written by the 11.0 executor (steps S1–S3); reviewed by
the `advisor` in R-11.0 (CHANGES-REQUIRED, applied in this version) before
11.1 starts. It freezes what is and is not configurable from the console
(D16), the retention families (D17), and the API names of D17/D18 (including
the D22c observation windows). Units 11.1–11.5 implement it; they do not add
a console setting that is not listed here, and they do not change a frozen
name without a recorded plan change.

Sources: the code on branch `feat/index-analysis` at the time of writing
(line numbers below refer to it), `deploy/agent.example.yaml`,
`deploy/server.example.env`, the migrations up to `0023` (the live database
has `schema_migrations` at version 23), and the live TimescaleDB of the dev
stack (section "Live retention policies").

The phase intent "everything else is listed with its reason" means this
document plus the README Settings section (phase 11 README obligation); the
console shows no list of excluded settings. The review decisions are
recorded in the last section ("Decisions (R-11.0)").

## Legend

- **Console** — `yes`: editable from Settings after phase 11; `read-only`:
  shown in Settings but not editable; `no`: not in the console.
- **Owner** — the unit that makes it configurable or shows it: `11.2`
  preferences, `11.3` retention, `11.4` advisor rules and windows, `11.5a`
  notification status and test send, `11.5b` notification URL storage
  (**BLOCKED on Q2**), `10.2` names (done), `existing` an editor that already
  exists elsewhere in the console. `—` means excluded.
- **Reason** (excluded items). The D16 reasons, with a short tag used in the
  tables:
  - `R-PASSWORD`: the shared UI password is a secret of the deployment
    (environment or secret file); a UI path to change it would need a
    credential store and its own security review, and would weaken it.
  - `R-SERVER-FLAG`: process configuration of the server, read once at start
    (like the session TTL); changing it needs a restart, so it stays in the
    environment. D16 names the session TTL; this document applies the same
    reason to the other server environment variables and flags.
  - `R-TOKEN`: agent bearer token rotation needs coordination with every
    agent (agent-side file or environment change).
  - `R-AGENT`: remote editing of `agent.yaml` is excluded: no
    server→agent configuration channel exists, commands are operational, not
    configuration, and the agent stays read-only (I-2).
  - `R-EXISTING`: already editable on its own page (alert rules, silences,
    finding mutes, cluster and instance names).
  - `R-CONSTANT`: a code constant, not a setting today; exposing it would
    need its own design decision (not part of D16).
  - `R-ENGINE`: advisor thresholds are not editable, the engine exposes none
    (D18); fixed windows keep their semantics (D22b).

## 1. Agent (`agent.yaml`, agent environment and flags)

All agent settings are excluded (`R-AGENT`; the token also `R-TOKEN`).
Struct: `internal/agent/config.go`; example: `deploy/agent.example.yaml`.

| Setting | Defined at | Default | Console | Owner | Reason |
|---------|-----------|---------|---------|-------|--------|
| `server.url` (env override `PGLENS_SERVER_URL`) | `internal/agent/config.go:38`, env `:161` | required; `http://` only for loopback | no | — | R-AGENT |
| `server.token` / `server.token_file` (env `PGLENS_BOOTSTRAP_TOKEN`, `PGLENS_BOOTSTRAP_TOKEN_FILE`) | `config.go:39-40`, env `:164-169` | required (one of them) | no | — | R-TOKEN, R-AGENT |
| `identity_path` (env `PGLENS_IDENTITY_PATH`, set from the file by `cmd/pglens-agent/run.go:61`) | `config.go:20`; `internal/agent/conn.go:699` | required; example `/var/lib/pglens/identity.json` | no | — | R-AGENT |
| `push_interval` | `config.go:21`, default `:221`, max `:367` | 15 s, at most 90 s | no | — | R-AGENT |
| `buffer.path`, `buffer.max_size`, `buffer.max_age` | `config.go:45-47`, defaults `:240`, `:248` | path required; 512 MiB; 6 h | no | — | R-AGENT |
| `targets[].name`, `dsn`, `cluster_name`, `host_local`, `allow_explain_analyze`, `allow_signal` | `config.go:52-58` | name/dsn required; `host_local` from the DSN host; both `allow_*` false | no | — | R-AGENT (the `allow_*` flags are permission tiers, shown read-only in the Inventory tab today) |
| `targets[].databases.include`, `exclude`, `max` | `config.go:121-123`; `internal/agent/conn.go:51,60` | include all; exclude `^template\d$`, `^rdsadmin$`, `^azure_.*$`; max 10 | no | — | R-AGENT |
| `checks.<name>.interval`, `top_n` (≤ 1000), `by_application` (activity), `enabled` (ash) | `config.go:136-143`, `maxTopN` `:129`; example `deploy/agent.example.yaml:40-70` | per-check code default; `by_application` false; `enabled` true | no | — | R-AGENT (intervals are shown read-only by the D22d coverage view, 13.2) |
| Future (D28): `checks.activity.by_application` default flips to **true**; new `checks.locks.session_query_text` | D28 (`overview.md`), not in the code yet | true; false | no | — | R-AGENT |
| `commands.enabled` | `config.go:65` | true | no | — | R-AGENT (I-2: disabling makes the agent strictly read-only) |
| `host.enabled`, `host.interval`, `host.proc_path`, `host.sys_path` (env `HOST_PROC`, `HOST_SYS`) | `config.go:69-72`, default `:260`, env `:82,:91` | true; 30 s; `/proc`, `/sys` | no | — | R-AGENT |
| `PGLENS_CONFIG` / `-config` | `cmd/pglens-agent/run.go:35` | `/etc/pglens/agent.yaml` | no | — | R-AGENT |
| `PGLENS_HEALTHZ_LISTEN` | `cmd/pglens-agent/run.go:95`, `main.go:61` | `:9187` | no | — | R-AGENT |
| `PGLENS_DEBUG_CLOCK_OFFSET` (test hook) | `cmd/pglens-agent/run.go:75` | unset | no | — | R-AGENT (debug only) |
| `check` subcommand flags `-dsn`, `-identity-path` | `cmd/pglens-agent/main.go:107-108` | —; `/var/lib/pglens/identity.json` | no | — | R-AGENT (CLI) |

## 2. Server environment and flags

Loaded by `internal/server/config.go` and `cmd/pglens-server/main.go`;
documented in `deploy/server.example.env`. Deployment compose variables
(`PGLENS_DB_*`, `PGLENS_MONITORING_PASSWORD`) only feed the compose files and
are out of scope.

| Setting | Defined at | Default | Console | Owner | Reason |
|---------|-----------|---------|---------|-------|--------|
| `PGLENS_DSN` (incl. `pool_max_conns`) | `cmd/pglens-server/main.go:55-91` | unset = no database; pool floor 11 conns | no | — | R-SERVER-FLAG |
| `PGLENS_LISTEN` | `main.go:48-51`, also `:215` (`-healthcheck`) | `:8080` | no | — | R-SERVER-FLAG |
| `-version`, `-healthcheck` | `main.go:34-35` | false | no | — | R-SERVER-FLAG (CLI) |
| `PGLENS_BOOTSTRAP_TOKEN` / `_FILE` (inline wins) | `internal/server/config.go:42-65` | required, no default | no | — | R-TOKEN |
| `PGLENS_UI_ENABLED` | `config.go:130-136` | true | no | — | R-SERVER-FLAG |
| `PGLENS_UI_PASSWORD` / `PGLENS_UI_PASSWORD_FILE` (file wins) | `config.go:138-147` | unset (API refused) | no | — | R-PASSWORD |
| `PGLENS_UI_SESSION_TTL` | `config.go:149-158` | 24 h (5 m–720 h) | no | — | R-SERVER-FLAG (D16: session TTL) |
| `PGLENS_UI_COOKIE_SECURE` | `config.go:160-166` | `auto` | no | — | R-SERVER-FLAG |
| `PGLENS_ALERT_INTERVAL` | `config.go:81-94`; also `internal/alert/engine.go:169` | 30 s (`defaultEngineInterval`, `engine.go:20`) | no | — | R-SERVER-FLAG |
| `PGLENS_ADVISOR_INTERVAL` | `internal/advisor/engine.go:428-440` | 15 min (`defaultAdvisorInterval`, `engine.go:21`) | read-only (Advisor tab may show it as the evaluation cadence) | 11.4 (display only) | R-SERVER-FLAG |
| `PGLENS_COMMAND_TTL` | `internal/server/commands.go:78-85` | 5 min (`defaultCommandTTL`, `commands.go:26`) | no | — | R-SERVER-FLAG |
| Slack URL: `PGLENS_ALERT_SLACK_WEBHOOK_URL`, legacy `PGLENS_SLACK_WEBHOOK_URL`, `…_FILE` of both (file wins over inline) | `config.go:74-105` | unset | status + test (11.5a); URL write-only (11.5b) | 11.5a / 11.5b (BLOCKED Q2) | URL itself: I-10 secret |
| Generic webhook URL: `PGLENS_WEBHOOK_URL` (no `_FILE` variant) | `config.go:80` | unset | status + test (11.5a); URL write-only (11.5b) | 11.5a / 11.5b (BLOCKED Q2) | URL itself: I-10 secret |
| Google Chat URL: `PGLENS_GOOGLE_CHAT_WEBHOOK_URL` / `_FILE` (file wins) | `config.go:106-113` | unset | status + test (11.5a); URL write-only (11.5b) | 11.5a / 11.5b (BLOCKED Q2) | URL itself: I-10 secret |

## 3. Database (TimescaleDB policies, schema) and server-side retention constants

| Setting | Defined at | Default | Console | Owner | Reason |
|---------|-----------|---------|---------|-------|--------|
| Retention policy of `metrics`, `metrics_statements`, `metrics_ash`, `metrics_replication` | `internal/store/migrations/0003_policies.sql:34-37` | 30 days | yes (Data retention tab) | 11.3 | — |
| Retention policy of `metrics_tables`, `metrics_indexes` | `0007_facts.sql:58-59` | 30 days | yes | 11.3 | — |
| Retention policy of `metrics_bloat` | `0007_facts.sql:60` | 90 days | yes | 11.3 | — |
| Compression policies (`compress_after`) | `0003_policies.sql:11,18,25,32` (48 h); `0007_facts.sql:53,55` (48 h), `:57` (7 days) | 48 h; `metrics_bloat` 7 days | read-only (drives `min_days`) | 11.3 | R-CONSTANT (storage tuning; retention never drops below it) |
| Compression `segmentby`/`orderby` | `0003_policies.sql`, `0007_facts.sql:52-56` | per table | no | — | R-CONSTANT |
| Chunk time interval | `0002_hypertables.sql:13,34,51,73`; `0007_facts.sql:15,27,39` | 6 h; `metrics_bloat` 1 day | no | — | R-CONSTANT |
| Static "Raw retention" text | `web/src/features/settings/ServerInfo.tsx:11` | "30 days; there are no rollups" | becomes dynamic or links to the tab | 11.3 | — |
| `events` retention (purge) | `internal/server/staleness.go:40` | 90 days | no | — | R-CONSTANT (R6: listed in README and `docs/LIMITS.md`) |
| `query_texts` retention (unseen) | `staleness.go:49` | 90 days | no | — | R-CONSTANT (R6) |
| Resolved alerts, their deliveries, expired silences | `internal/alert/engine.go:27`; `internal/alert/store.go:130-136` | 90 days | no | — | R-CONSTANT (R6) |
| Resolved findings | `internal/advisor/engine.go:269`; `internal/advisor/store.go:88-90` | 90 days | no | — | R-CONSTANT (R6) |
| Lock snapshots | `internal/store/write.go:567` | latest + 15 min | no | — | R-CONSTANT |
| `commands` (queued commands) | `0010_commands.sql:1` | no purge, kept | no | — | R-CONSTANT |
| `command_audit` | `0010_commands.sql:26` | no purge, kept | no | — | R-CONSTANT |
| `query_plans` | `0010_commands.sql:41` | no purge, kept forever (only the one-off credential purge of `0021`) | no | — | R-CONSTANT |
| Staleness thresholds (no primary 60 s, slot inactive 30 s, evaluator 15 s) | `staleness.go:27-33` | as listed | no | — | R-CONSTANT |
| New table `advisor_rule_overrides` | 11.4 migration (next free number, `0024` at the time of writing) | empty = all enabled | via Advisor tab | 11.4 | — |
| New table `server_settings` | 11.4 migration | empty = defaults | via Advisor tab (windows) | 11.4 | — |
| New table `notification_channels` | 11.5b migration | — | via Notifications tab | 11.5b (BLOCKED Q2) | — |

Every new table has `tenant_id text NOT NULL DEFAULT 'default'` (as
`findings`).

## 4. Advisor

Registry: `internal/advisor/registry.go` (`All()`, sorted by id). A static
scan of the rule constructors finds 71 rule ids in 21 categories (`config`
13, `index` 7, `table` 6, `query` 5, `txn` 5, …); the exact list is what
`GET /api/v1/advisor/rules` returns. The endpoint is registered at
`internal/server/api_findings.go:54`, handled by `listAdvisorRules`
(`api_findings.go:273`), OpenAPI operationId `getAdvisorRules`
(`api/openapi.yaml:2988`), schema `AdvisorRule` (`openapi.yaml:1154`:
`id, severity, scope, needs, min_tier`).

| Setting | Defined at | Default | Console | Owner | Reason |
|---------|-----------|---------|---------|-------|--------|
| Rule enabled / disabled (per rule id) | new `advisor_rule_overrides` | enabled | yes (Advisor rules tab) | 11.4 | — |
| `window.unused_index` (`unusedWindow`) | `internal/advisor/rules_relation.go:128` | 7 days (604800 s), bounds 3600–7776000 s | yes | 11.4 (stored); 13.2 consumes (`advisor.Windows`) | — |
| `window.backup_history` (`backupHistory`) | `internal/advisor/rules_config.go:24` | 7 days, bounds as above | yes | 11.4; 13.2 consumes | — (replaces only the monitored-history comparisons at `rules_config.go:395` and `:411`) |
| Base-backup recent age (`age < backupHistory`) | `rules_config.go:389` | 7 days | no | — | R-ENGINE (D22b: stays fixed, not driven by `window.backup_history`) |
| `basebackupMaxAge` | `rules_config.go:27` | 30 days | no | — | R-ENGINE (rule threshold) |
| `pgstattupleWindow` | `internal/advisor/snapshot.go:32` | 7 days | no | — | R-ENGINE (D22b: freshness window keeps its semantics; a `window.pgstattuple` key is rejected with 400, ADVW-004) |
| `partitionHorizon` | `internal/advisor/rules_catalog.go:469` | 7 days | no | — | R-ENGINE (D22b: forward horizon) |
| Other snapshot windows (`recentWindow` 15 min, `bloatEstimateWindow` 13 h, `checkpointWindow` 24 h, `replicationWindow` 5 min, `baselineWindow` 8 days, `siblingActiveWindow` 1 h) | `snapshot.go:30-36` | as listed | no | — | R-ENGINE |
| Rule thresholds (ratios, ages, sizes) | rule constructors, e.g. `rules_config.go:243,311` | per rule | no | — | R-ENGINE |
| Evaluation cadence | `PGLENS_ADVISOR_INTERVAL` (section 2) | 15 min | read-only | 11.4 (display only) | R-SERVER-FLAG |
| Finding mute / unmute | `POST/DELETE /api/v1/findings/*` (`api_findings.go:51-52`) | — | yes, on the Findings page | existing | R-EXISTING |

**Go seams.** The server owns `server_settings` and reads it through
`settings.Get(ctx, key)` (11.4); a window value is stored as a jsonb integer
number of seconds. The advisor receives the windows through an injected
`WindowsSource func(ctx context.Context) Windows` (nil → `DefaultWindows`),
cached 30 s, and the overrides through
`OverridesSource func(ctx context.Context) (map[string]bool, error)` (nil →
nothing disabled; an error keeps the previous snapshot). The `advisor`
package never reads `server_settings` or `advisor_rule_overrides` itself
(`phase_14.md` 13.2), so the hand-built test schemas need no new table.

## 5. Notifications

Channels: `internal/alert/notify/slack.go` (`Name()` = `slack`),
`webhook.go` (`webhook`), `googlechat.go` (`google_chat`); built in
`cmd/pglens-server/channels.go:9` (`buildChannels`, one channel per configured
URL) and wrapped by `alert.NewChannelNotifier` (`internal/alert/notifier.go:75`,
which records deliveries). Today the channels are built inside the
`pool != nil` branch of `cmd/pglens-server/main.go:122-131`. HTTP send:
`internal/alert/notify/http.go:13` (`sendJSON`: 45 s overall, 3 attempts of
10 s each with backoff; a 4xx stops retrying), redaction `redactError`
(`http.go:60`). The Alerts page `web/src/features/alerts/DeliverySection.tsx:1-12`
lists the environment variable names statically and says the console
receives no URL or status.

| Setting | Defined at | Default | Console | Owner | Reason |
|---------|-----------|---------|---------|-------|--------|
| Channel status (configured, source, host) | section 2 rows | not configured | read-only | 11.5a | — |
| Send a test message | new `POST …/{kind}/test` | — | yes (action) | 11.5a | — |
| Webhook URL storage (write-only, DB overrides environment) | new `notification_channels` | environment only | yes, write-only | 11.5b | **BLOCKED on Q2** (STATE §9) |
| Alert rules (threshold, severity, `for`, enabled) | `PUT /api/v1/alert-rules/{rule_id}` (`internal/server/api_alerts.go:42`, operationId `updateAlertRule`) | built-in rules | yes, on `/alerts` | existing | R-EXISTING |
| Silences | `POST/DELETE /api/v1/silences` (`api_alerts.go:44-45`) | — | yes, on `/alerts` | existing | R-EXISTING |
| Notification retry/timeout (45 s, 3 × 10 s) | `http.go:17,24` | as listed | no | — | R-CONSTANT |

## 6. Console preferences (browser)

Today `web/src/lib/preferences.ts` stores only `theme` under the key
`pglens.preferences` (best effort, never throws). `pglens.lastInstance`
(`web/src/lib/lastInstance.ts:3`) is a convenience memory, not a setting.

| Setting | Defined at | Default | Console | Owner | Reason |
|---------|-----------|---------|---------|-------|--------|
| `theme` (`dark`\|`light`\|`system`) | `web/src/lib/preferences.ts:3-12` | `light` | yes (header toggle today; Preferences tab) | 11.2 | — |
| `defaultTimeRange` (`1h`\|`6h`\|`24h`\|`7d`) | new in `preferences.ts`; today `DEFAULT_RANGE` `web/src/lib/timerange.ts:1` | `1h` | yes | 11.2 | — (`15m` deliberately not offered; `?range=15m` keeps working) |
| `refreshInterval` (`auto`\|`off`\|`15s`\|`30s`\|`60s`) | new; today per-surface `REFRESH` in `web/src/api/policy.ts:1` | `auto` = today's per-surface policy (P1) | yes | 11.2 | — |
| `tablePageSize` (20\|50\|100, D23) | new; today `DEFAULT_PAGE_SIZE` `web/src/lib/tableControls.ts:13` | 20 | yes | 11.2 | — |
| Per-table state in the URL (`page`, `size`, sort, filters) | `web/src/hooks/useTableState.ts` | from the URL | no (per page) | — | not a setting; a URL `size` wins over `tablePageSize` |
| `pglens.lastInstance` | `web/src/lib/lastInstance.ts:3` | — | no | — | not a setting |

`refreshInterval` semantics (P1): `auto` keeps every surface's `REFRESH`
policy. A chosen value (`15s`, `30s`, `60s`) replaces the interval only of
the surfaces whose policy interval is ≥ 15 s, and their `staleAfter` scales
with it (same ratio as the policy; `useFreshness.ts:28` and
`FreshnessBadge.tsx:15` read `REFRESH[policy].staleAfter`). `off` stops those
surfaces and sets their `staleAfter` to 0 (no stale badge). `command` (1 s),
`locks` and `activity` (5 s) and `static` always keep their policy.

## 7. Names and other existing editors

| Setting | Defined at | Console | Owner | Reason |
|---------|-----------|---------|-------|--------|
| Cluster name (`PATCH /api/v1/clusters/{id}`, `renameCluster`) | `internal/server/api.go:57`; `0022_cluster_name_unique.sql` | yes (Names tab) | 10.2 (done) | — |
| Instance display name (`PATCH /api/v1/instances/{id}`, `renameInstance`) | `api.go:58`; `0023_instance_display_name.sql` | yes (Names tab) | 10.2 (done) | — |
| Agent `cluster_name` in `agent.yaml` | section 1 | no | — | R-AGENT (the console name overrides the display only) |

## Live retention policies (S2)

Read-only check on the dev stack, 2026-10-30, TimescaleDB 2.31.0 (image
`timescale/timescaledb:2.31.0-pg17`), database `pglens`, user `pglens`, over
the container's local socket (no password needed or read). Queries:

```sql
SELECT j.job_id, j.proc_name, j.hypertable_name, j.config, j.schedule_interval, j.next_start
  FROM timescaledb_information.jobs j
 WHERE j.proc_name IN ('policy_retention','policy_compression')
 ORDER BY j.hypertable_name, j.proc_name;

SELECT hypertable_schema, hypertable_name, num_dimensions, num_chunks, compression_enabled
  FROM timescaledb_information.hypertables ORDER BY hypertable_name;

SELECT hypertable_name, column_name, time_interval
  FROM timescaledb_information.dimensions ORDER BY hypertable_name;
```

Command: `docker exec pglens-odoo-timescaledb-1 psql -U pglens -d pglens -X -c "<query>"`.

Results (`schedule_interval`: retention jobs 1 day each; compression jobs
3 h, `metrics_bloat` 12 h; no continuous aggregates; the other jobs are
TimescaleDB's own `policy_telemetry` (1) and
`policy_job_stat_history_retention` (3)):

| Family | Hypertable | Chunk interval | Chunks | Compression job | `compress_after` | Retention job | `drop_after` | `source` | `min_days` |
|--------|-----------|----------------|--------|-----------------|------------------|---------------|--------------|----------|-----------|
| `metrics` | `metrics` | 6 h | 5 | 1003 | `48:00:00` | 1004 | `30 days` | default | 3 |
| `metrics_statements` | `metrics_statements` | 6 h | 5 | 1000 | `48:00:00` | 1005 | `30 days` | default | 3 |
| `metrics_ash` | `metrics_ash` | 6 h | 5 | 1001 | `48:00:00` | 1006 | `30 days` | default | 3 |
| `metrics_replication` | `metrics_replication` | 6 h | 0 | 1002 | `48:00:00` | 1007 | `30 days` | default | 3 |
| `metrics_tables` | `metrics_tables` | 6 h | 5 | 1008 | `48:00:00` | 1011 | `30 days` | default | 3 |
| `metrics_indexes` | `metrics_indexes` | 6 h | 5 | 1009 | `48:00:00` | 1012 | `30 days` | default | 3 |
| `metrics_bloat` | `metrics_bloat` | 1 day | 2 | 1010 | `7 days` | 1013 | `90 days` | default | 8 |

`min_days = max(3, ceil(compress_after_days) + 1)`: 48 h = 2 days → max(3, 3)
= 3; 7 days → max(3, 8) = 8. All seven hypertables have
`compression_enabled = t`, schema `public`, one dimension (`ts`).

**Conclusion: the D17 family list is confirmed** — exactly seven hypertables,
each with one retention and one compression policy, values equal to the
migrations (`0003`, `0007`); no other hypertable or policy exists. Each
family maps to the hypertable of the same name (the whitelist of I-3). Notes
for 11.3: the policy drops whole chunks, so data is removed in 6 h steps
(1 day for `metrics_bloat`) once a chunk's end is older than `drop_after`;
`show_chunks(older_than => …)` uses the same rule, so the preview matches
the effect. Retention jobs run once a day, so a shortening takes effect up to
24 h after the PUT. `remove_retention_policy` + `add_retention_policy` creates
a new job id; tests read the policy by `hypertable_name`, never by job id.
The tables `advisor_rule_overrides`, `server_settings` and
`notification_channels` do not exist yet.

## Frozen API

Names below are the phase text's (`phase_12.md` design context, D17/D18,
D22c), verbatim; shapes the phase left open are frozen by the R-11.0
decisions. Every route is under the session-authenticated API, so in
addition to the listed codes: 401 without a session; 403 `cross_origin` for
a write from another origin (`refuseCrossOriginWrites`,
`internal/server/http.go:93`); 413/415 for an oversized or non-JSON write
body, as `updateAlertRule`; errors use the shared `Error` schema
`{error, detail}` (`writeError`, `internal/server/api.go:100`) unless a body
is listed.

### Retention (11.3, D17)

| operationId | Method and route | Request | 200 response | Errors |
|-------------|------------------|---------|--------------|--------|
| `getRetention` | `GET /api/v1/settings/retention` | — | `[{family, hypertable, days, source: 'default'\|'custom', min_days, max_days: 365}]`, seven rows in the D17 order; `days` is nullable (`null`, `source: 'custom'` when the family has no live retention policy) | 503 storage unavailable |
| `previewRetention` | `GET /api/v1/settings/retention/preview?family=&days=` | query `family`, `days` | `{family, days, chunks_to_drop, bytes_to_free, oldest_chunk_at, newest_dropped_chunk_end}` (from `show_chunks(hypertable, older_than => $days * interval '1 day')` + `chunks_detailed_size`); a lengthening preview returns 0 chunks | 400 `days` missing, not an integer, or outside [`min_days`, 365]; 404 unknown family; 503 |
| `setRetention` | `PUT /api/v1/settings/retention/{family}` | `{days, confirm}` (`confirm` optional, default false) | the updated row of `getRetention`; same `days` as now → 200 without rewriting the policy | 400 `days` missing, not an integer, or outside [`min_days` of the family, 365]; 404 unknown family; 409 `RetentionConfirmationRequired{error: 'confirmation required', detail?, preview}` when `days` < current (or the family has no policy) and `confirm` is not true; lengthening needs no confirmation; 503 |

Semantics: `remove_retention_policy(hypertable, if_exists => true)` then
`add_retention_policy(hypertable, drop_after => make_interval(days => $2))`
in one transaction; a failure rolls back and leaves the old policy. Family →
hypertable by whitelist; all values parameterised (I-3). `min_days` is
computed from the live `policy_compression` config, never hard-coded; a
family without a compression policy gets `min_days` 3. `source` is `default`
when `days` equals the migration default (30, or 90 for `metrics_bloat`),
else `custom`. A PUT on a family with no live retention policy counts as a
shortening (preview + `confirm`). Deletion happens at the next policy run.

### Advisor rules and observation windows (11.4, D18, D22c)

| operationId | Method and route | Request | 200 response | Errors |
|-------------|------------------|---------|--------------|--------|
| `getAdvisorRules` (existing, handler `listAdvisorRules`; no rename) | `GET /api/v1/advisor/rules` | — | `AdvisorRule[]` gains `enabled` (boolean, default true) and `overridden` (boolean: an override row exists); without a database pool every rule is `enabled: true, overridden: false` | 503 when reading the overrides fails |
| `setAdvisorRuleOverride` | `PUT /api/v1/advisor/rules/{id}/override` | `{enabled}` | the updated `AdvisorRule`; `{enabled: false}` upserts the row, `{enabled: true}` deletes it (back to the default, `overridden: false`) | 400 body without boolean `enabled`; 404 unknown rule id; 503 |
| `getAdvisorWindows` | `GET /api/v1/settings/advisor-windows` | — | `[{key, seconds, default_seconds: 604800, min_seconds: 3600, max_seconds: 7776000, source: 'default'\|'custom'}]`, always both keys in the fixed order `window.unused_index`, `window.backup_history` | 503 |
| `setAdvisorWindows` | `PUT /api/v1/settings/advisor-windows` | `{"window.unused_index"?: seconds\|null, "window.backup_history"?: seconds\|null}`; `null` resets the key to its default (deletes the row); `{}` is a 200 no-op | same body as `getAdvisorWindows` | 400 value outside 3600–7776000 or not an integer; 400 any other key, including any other `window.*` key (D22); 503 |

Storage: `advisor_rule_overrides(tenant_id text NOT NULL DEFAULT 'default',
rule_id text, enabled boolean not null, updated_at timestamptz not null
default now(), primary key (tenant_id, rule_id))`; `server_settings(tenant_id
text NOT NULL DEFAULT 'default', key text, value jsonb not null, updated_at
timestamptz not null default now(), primary key (tenant_id, key))`, a window
`value` being a jsonb integer number of seconds. A disabled rule is skipped
and its open, degraded and muted findings are resolved at the next
evaluation with `evidence = evidence || '{"resolved_reason":"rule_disabled"}'`
(no new column); `UpsertFinding` overwrites `evidence` when the finding
reopens (`internal/advisor/store.go:55`), so the marker clears itself.

### Notification channels (11.5a; 11.5b BLOCKED on Q2)

| operationId | Method and route | Request | 200 response | Errors |
|-------------|------------------|---------|--------------|--------|
| `getNotificationChannels` | `GET /api/v1/notification-channels` | — | `[{kind: 'slack'\|'webhook'\|'google_chat', configured, source: 'env', host}]`, always the three kinds in this order; `source` enum `['env']` in 11.5a (11.5b adds `ui`); `host` = `url.Parse(url).Hostname()`, `null` when not configured or unparseable; never the URL | — |
| `testNotificationChannel` | `POST /api/v1/notification-channels/{kind}/test` | none | `{delivered, status: 'delivered'\|'failed'\|'timeout', error?}` (`timeout` when `errors.Is(err, context.DeadlineExceeded)`); `error` passed through `redactError` | 404 unknown kind; 409 `{error: 'channel not configured'}` (shared `Error` schema) for a kind with `configured: false` |
| — (**BLOCKED Q2**, 11.5b) | `PUT /api/v1/notification-channels/{kind}` | `{url}` (write-only; `https://`, or `http://` only for loopback/private hosts; ≤ 2048 chars) | status row of the kind, `source: 'ui'` | 400 bad URL; 404 unknown kind |
| — (**BLOCKED Q2**, 11.5b) | `DELETE /api/v1/notification-channels/{kind}` | — | status row, back to the environment value | 404 unknown kind |

Test send: it calls the same `notify.Channel` objects the alert path uses,
directly (not `alert.NewChannelNotifier`, so no row lands in
`notifications`), with a timeout of 10 s ("within 10 s" in the UI: a fast
5xx may still be retried inside it). The status comes from `AlertConfig`;
the channel set is built without depending on the database pool (today it
is built inside the `pool != nil` branch, `main.go:122-131`). The test
`notify.Message` (`internal/alert/notify/notify.go:8`) is frozen as
`RuleID: "pglens.test"`, `Phase: "test"`, `Severity: "info"`,
`Summary: "pglens test notification"`, `At: now`, other fields empty — the
generic webhook receives it as JSON (`rule_id`, `phase`, `severity`,
`summary`, `at`, …), Slack and Google Chat render the summary. The phase
names no operationIds for the 11.5b routes; they are chosen when Q2 is
answered. The URL is never returned, rendered or logged (I-10).

### Unchanged routes the tabs use

`renameCluster` (`PATCH /api/v1/clusters/{id}`), `renameInstance`
(`PATCH /api/v1/instances/{id}`) — Names tab (10.2); the command audit and
server info endpoints already used by `CommandAudit` and `ServerInfo` —
Audit & server tab.

## Tab registry

URL: `/settings?tab=inventory|preferences|retention|advisor|notifications|names|audit`;
default `inventory` (never written to the URL, SETTABS-006); an unknown value
falls back to `inventory`. Order and labels as the 11.1 contract. 11.1 builds
`SETTINGS_TABS` (`web/src/features/settings/settingsTabs.ts`) from this table
and SETI-001 (`web/src/lib/settingsInventory.test.ts`) parses the rows between
the two markers below: column 1 (the backticked tab id) and column 2 (the
label), in order, so both order and labels are guarded.

<!-- settings-tabs:start -->
| Tab id | Label | Holds | Console-configurable items | Owner |
|--------|-------|-------|----------------------------|-------|
| `inventory` | Inventory | `InventoryTables` (instances, agents, permission tiers, telemetry), unchanged | none (read-only) | 11.1 (move) |
| `preferences` | Preferences | `PreferencesPanel` | `theme`, `defaultTimeRange`, `refreshInterval`, `tablePageSize` | 11.2 |
| `retention` | Data retention | `RetentionPanel` | retention days of the seven families (`metrics`, `metrics_statements`, `metrics_ash`, `metrics_replication`, `metrics_tables`, `metrics_indexes`, `metrics_bloat`) | 11.3 |
| `advisor` | Advisor rules | `AdvisorPanel`: rules table with Enabled switches; "Observation windows" form | rule enabled per rule id; `window.unused_index`, `window.backup_history` | 11.4 |
| `notifications` | Notifications | `NotificationsPanel`: per channel configured badge, host, environment variable name, **Send test**; instructions when not configured | test send per kind (`slack`, `webhook`, `google_chat`); URL storage only after Q2 (11.5b) | 11.5a / 11.5b |
| `names` | Names | `NamesPanel`, unchanged | cluster name, instance display name | 10.2 |
| `audit` | Audit & server | `CommandAudit` and `ServerInfo`, unchanged (the retention text of `ServerInfo` changes in 11.3) | none (read-only) | 11.1 (move) |
<!-- settings-tabs:end -->

Each tab is registered in `web/e2e/fullhd_routes.ts` (I-13; the `/settings`
entry loses `pendingUnit: '11.1'`), and every control carries helper text
stating its effect (I-16).

## Excluded items (summary)

| Item | Reason |
|------|--------|
| UI password change | R-PASSWORD |
| Session TTL and the other server environment variables and flags (`PGLENS_DSN`, `PGLENS_LISTEN`, `PGLENS_UI_ENABLED`, `PGLENS_UI_COOKIE_SECURE`, `PGLENS_ALERT_INTERVAL`, `PGLENS_ADVISOR_INTERVAL`, `PGLENS_COMMAND_TTL`) | R-SERVER-FLAG |
| Agent bearer token rotation (`PGLENS_BOOTSTRAP_TOKEN[_FILE]`, server and agent) | R-TOKEN |
| Remote editing of `agent.yaml` (targets, checks, intervals, buffer, host, commands, the D28 additions) | R-AGENT (I-2) |
| Alert rules, silences, finding mutes | R-EXISTING |
| Advisor thresholds and fixed windows (incl. base-backup recent age and `basebackupMaxAge`) | R-ENGINE |
| Compression, chunk intervals, non-hypertable retention (events, query texts, alert history, resolved findings: 90 days; `commands`, `command_audit`, `query_plans`: kept) | R-CONSTANT |

## Decisions (R-11.0)

Review R-11.0 by the `advisor`: CHANGES-REQUIRED (small), applied here.

Retention (11.3)
- **R1** Accepted: `setRetention` 200 returns the updated `getRetention` row;
  the same `days` as now → 200 without rewriting the policy.
- **R2** A family with no live retention policy is reported as
  `days: null`, `source: 'custom'` (`days` nullable); a PUT on it counts as a
  shortening (preview + `confirm`). A family with no compression policy gets
  `min_days` 3.
- **R3** Accepted: `previewRetention` 400 outside [`min_days`, 365] and for a
  missing or non-integer `days`; 404 unknown family; a lengthening preview
  returns 0 chunks.
- **R4** Accepted: new OpenAPI schema
  `RetentionConfirmationRequired{error, detail?, preview}` for the 409.
- **R5** Wording only: "auth/CSRF as `updateAlertRule`" names the
  operationId; the Go handler is `updateRule` (`api_alerts.go:42`).
- **R6** The four 90-day purges (events, query texts, resolved
  alerts/deliveries/silences, resolved findings) stay excluded (R-CONSTANT)
  and are not shown in the tab; README and `docs/LIMITS.md` list them.
- **R7** Accepted: 11.3 also updates the storage-policy comment of
  `deploy/server.example.env` (file-set extension of 11.3; check
  `deploy/*_test.go` first) as well as the `ServerInfo` text.

Advisor (11.4)
- **A1** Changed: no `resolved_reason` column. The disable path writes
  `evidence = evidence || '{"resolved_reason":"rule_disabled"}'`;
  `UpsertFinding` overwrites `evidence` on reopen
  (`internal/advisor/store.go:55`), so the marker clears itself. Muted
  findings of a disabled rule are resolved too.
- **A2** Accepted: the existing operationId stays `getAdvisorRules` (no
  rename); the new fields go to `AdvisorRule`.
- **A3** Response = the updated `AdvisorRule`; `{enabled: true}` deletes the
  override row; `overridden` = a row exists.
- **A4** Accepted with: `value` is a jsonb integer number of seconds; GET
  always returns both keys in fixed order; `{}` is a 200 no-op; any other key
  → 400.
- **A5** Accepted: until 13.2 wires `advisor.Windows`, the stored windows do
  not change findings; ADV-E2E-001 checks the read-back of
  `window.unused_index`, not its effect, until 13.2.
- **C5** Frozen: `getAdvisorRules` without a database pool → every rule
  `enabled: true, overridden: false`, 200; an error reading the overrides →
  503.

Notifications (11.5a)
- **N1** `status: 'delivered'|'failed'|'timeout'`; `timeout` when
  `errors.Is(err, context.DeadlineExceeded)`.
- **N2** Accepted: the 10 s timeout cuts `sendJSON`'s retries; the UI says
  "within 10 s" (a fast 5xx may still be retried inside it).
- **N3** Accepted: the test calls the same `notify.Channel` objects directly
  (no row in `notifications`); the status comes from `AlertConfig`; the
  channel set is built without depending on the pool
  (`cmd/pglens-server/main.go:122-131` builds it inside `pool != nil` today).
- **N4** 409 `{error: 'channel not configured'}` with the shared `Error`
  schema.
- **N5** `source` enum `['env']` in 11.5a; the three kinds always returned in
  order; `host: null` when not configured or unparseable.
- **N6** The static mapping kind → environment variable names moves from
  `DeliverySection.tsx` into one shared client module, used by both the
  Alerts page and the Notifications tab.
- **N7** The Google Chat variables are added to `deploy/server.example.env`
  in 11.5's docs step (file-set extension of 11.5).
- **C9** Test message fields frozen (section "Notification channels"
  above).

Preferences (11.2)
- **P1** Plan change (recorded in `phase_12.md` Preferences line and the 11.2
  contract): `refreshInterval` gains `auto` (default = today's per-surface
  policy); a chosen value overrides only surfaces with interval ≥ 15 s;
  `command` (1 s), `locks`/`activity` (5 s) and `static` keep their policy;
  `staleAfter` scales with the chosen value; `off` sets `staleAfter` to 0.
- **P2** Deliberate: `defaultTimeRange` does not offer `15m`; `?range=15m`
  keeps working.
- **P3** A `size` in the URL wins over `tablePageSize`.

Tabs (11.1)
- **T1** Markers accepted; SETI-001 parses column 1 (id) and column 2
  (label), so order and labels are guarded.

Document fixes applied: C1 (base-backup recent age row, `basebackupMaxAge`
reason, the `:395`/`:411` note), C2 (Go seams), C3 (= A1), C4 (= P1), C5,
C6 (intro), C7 (`query_plans`, `commands`, `command_audit`), C8 (D28 agent
settings), C9, C10 (`defaultCommandTTL`: the constant is at
`internal/server/commands.go:26` on this branch — line 25 is `const (`), and
the nits (`PGLENS_LISTEN` also at `main.go:215`; `tenant_id … DEFAULT
'default'` on the new tables; deployment compose variables out of scope).
