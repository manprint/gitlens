# Phase 2 — ASH dimensions: user, client address, application

Intent: Activity (ASH) can be grouped and filtered by `usename`, `client_addr`, `application_name`.
Prerequisites: P0.
Phase closure: P2; review by agent-1.

## State and ownership contract
Read STATE.md §1 first (and PROTOCOL.md once without this skill) for scope, ownership,
recovery, checks, and commit rules. Open before edits; checkpoint at a recovery boundary;
close with evidence. A delegated worker follows its assigned OPEN unit. Missing design goes
to agent-1, the supervisor. Executor power: low.

## Local design context
Plan revision: 1.
- D4: three text dimensions; unknown value is the empty string `''` (never NULL); `client_addr` is `host(client_addr)` (no netmask), `''` for a Unix socket; values longer than 64 bytes are cut on a UTF-8 boundary. Agent key `checks.ash.dimensions` is a list; default `[usename, client_addr, application_name]`; an empty list `[]` turns all three off; an unknown name is a configuration error.
- I-2: when the key cap (`ashMaxKeys = 100`) cuts keys, the sum of samples is unchanged (`capConserved`).
- R7: on TimescaleDB 2.31, inside one transaction, adding `text NOT NULL DEFAULT ''` columns to a hypertable with compressed chunks works, and so does dropping and recreating the unique index.
- Sampler query after this phase (unit 2.1):
  ```sql
  SELECT COALESCE(datname, '') AS datname, state AS state,
         COALESCE(wait_event_type, 'CPU') AS wait_event_type,
         COALESCE(wait_event, 'CPU') AS wait_event, query_id,
         COALESCE(usename, '') AS usename,
         COALESCE(host(client_addr), '') AS client_addr,
         COALESCE(application_name, '') AS application_name
    FROM pg_stat_activity
   WHERE backend_type IN ('client backend', 'parallel worker')
     AND state <> 'idle' AND pid <> pg_backend_pid()
     AND COALESCE(application_name, '') NOT LIKE 'pglens/%'
  ```
  Keep the existing leading comment and anything else the current constant has.
- Wire labels (metric `ash_samples`): `usename`, `client_addr`, `application_name`. A label is omitted when its value is `''` or its dimension is off.
- Migration 0033 (unit 2.3):
  ```sql
  ALTER TABLE metrics_ash
    ADD COLUMN IF NOT EXISTS usename          text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS client_addr      text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS application_name text NOT NULL DEFAULT '';
  DROP INDEX IF EXISTS metrics_ash_dedup_idx;
  CREATE UNIQUE INDEX metrics_ash_dedup_idx ON metrics_ash
    (instance_id, datname, wait_event_type, wait_event, state, COALESCE(queryid, 0),
     usename, client_addr, application_name, ts);
  ```
  The compression `segmentby` setting is not changed.

## Sub-phases

### 2.1 Sampler and aggregator carry the three dimensions
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** READ and WRITE `internal/ash/sampler.go` (`SampleRow`, the sample query, the row scan), `internal/ash/aggregator.go` (`Key`, `Add`, `keyLess`, `capConserved`), `internal/ash/sampler_test.go`, the aggregator test file; READ callers of `Add` (`cmd/pglens-agent/ash.go`, `cmd/pglens-agent/watch.go`).
- **Change:**
  Preconditions: P0 DONE.
  Contract: `SampleRow` gains `Usename, ClientAddr, ApplicationName string`. `Key` gains the same three fields. `Add` takes a `SampleRow`-shaped argument: change the signature to `Add(r SampleRow)` only if every caller is in this repository (it is); otherwise add the three strings at the end. `keyLess` orders by the old fields first, then `Usename`, `ClientAddr`, `ApplicationName`. A helper `cut64(s string) string` cuts to 64 bytes on a UTF-8 boundary and is applied when a `SampleRow` is scanned.
  Scope: no change to tick handling, failure handling, or the cap value.
  Steps:
  1. S1 — sampler: query, struct, scan, `cut64`.
  2. S2 — aggregator: `Key`, `Add`, `keyLess`; `capConserved` must copy the new fields for kept keys and leave them `''` in the overflow bucket.
  3. S3 — update callers so the code compiles; they pass the row through (label emission is 2.2).
  Failure handling: a caller outside `cmd/pglens-agent` and `internal/ash` uses `Add` → list it and ask agent-1.
- **Unit tests:** `TestASHD001` (two rows that differ only by `usename` give two keys), `TestASHD002` (`cut64` on a 70-byte ASCII string gives 64 bytes; on a string with a 3-byte rune across byte 64 gives 63 bytes), `TestASHD003` (120 distinct keys with cap 100: 100 buckets, total samples equal to the input total; guards I-2) in `internal/ash/`. Gate G-GO-FOCUS `./internal/ash/ -run 'TestASHD00[123]'`.
- **e2e tests:** `TestINTASHD001` (`./internal/ash/` integration, PG 15.19 and 18.6): a session with `application_name='ashd-test'` running `pg_sleep` is sampled with `ApplicationName == "ashd-test"` and a non-empty `Usename`. Gate G-GO-INT.
- **Done:** tests pass; whole module compiles (`go build ./...`); reviewed; unit closed and committed.

### 2.2 Agent emits the labels; `checks.ash.dimensions`
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** WRITE `cmd/pglens-agent/ash.go` (`ashWindowToMetrics`, `startASH`), `internal/agent/config.go` (`CheckConfig`, validation in `LoadConfig`), `internal/agent/config_test.go`, the ASH test file of `cmd/pglens-agent`; README section on agent configuration and any shipped example agent YAML (`grep -rln 'by_application' --include='*.yaml' --include='*.yml' --include='*.md' .`).
- **Change:**
  Preconditions: 2.1 DONE.
  Contract: `CheckConfig` gains `Dimensions *[]string` with tag `yaml:"dimensions,omitempty"`; method `ASHDimensions() (usename, clientAddr, applicationName bool)`: nil pointer → all true; otherwise true only for listed names. Validation: the key is allowed only under `checks.ash`; a name outside the three → error `checks.ash.dimensions: unknown dimension "<name>"`. A dimension that is off is blanked in the row before `Add` (so keys merge). `ashWindowToMetrics` adds each of the three labels only when the value is not `''`.
  Steps:
  1. S1 — config field, method, validation, tests.
  2. S2 — blank disabled dimensions where rows are added to the aggregator; emit labels.
  3. S3 — document the key in README and example YAML.
- **Unit tests:** `TestASHD004` (default: all three labels present for a row with values), `TestASHD005` (`dimensions: [usename]`: only `usename` label; two rows that differ only by `client_addr` merge into one metric), `TestASHD006` (`dimensions: [foo]` fails with the exact error), `TestASHD007` (`dimensions: []` gives none of the three labels). Gate G-GO-FOCUS `./cmd/pglens-agent/ ./internal/agent/`.
- **e2e tests:** N/A — covered by 2.3's integration test end to end.
- **Done:** tests pass; reviewed; unit closed and committed.

### 2.3 Schema, store, pipeline
- **Agent:** agent-2
- **Assignment:** implement; agent-1 reviews the migration before it is run and the diff after.
- **Files:** NEW `internal/store/migrations/0033_ash_dimensions.sql` (D19); WRITE `internal/store/write.go` (`ASHRow`, `WriteASH`, `(*Writer).WriteASH` including the `tmp_metrics_ash` definition and both column lists), `internal/server/pipeline.go` (`buildASHRow`), their tests.
- **Change:**
  Preconditions: 2.2 DONE.
  Contract: `ASHRow` gains `Usename, ClientAddr, ApplicationName string`. `buildASHRow` reads the three labels; a missing label is `''` (old agents). Both writers insert the three columns and keep `ON CONFLICT DO NOTHING`.
  Steps:
  1. S1 — write the migration exactly as in Local design context; send it to agent-1 before running anything.
  2. S2 — `ASHRow`, both writers, temp table, `buildASHRow`.
  Recovery boundary: after S1 review.
  Failure handling: the migration fails on the integration database → do not edit an applied migration of an earlier number; fix 0033 and ask agent-1.
- **Unit tests:** `TestASHD008` (`buildASHRow` with the three labels fills the fields; without them fields are `''`) in `internal/server/`. Gate G-GO-FOCUS.
- **e2e tests:** `TestINTASHD002` (`./internal/store/` integration): migrate, write two rows that differ only by `usename` at the same `ts` → 2 rows; write the same row twice → 1 row; a row written through each writer path reads back with the three values. `TestINTASHD003`: migrations applied to a database that already has `metrics_ash` rows from before 0033 keep those rows with `''`. Gate G-GO-INT.
- **Done:** tests pass; migration reviewed by agent-1; unit closed and committed.

### 2.4 API: `group_by` and filters
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** WRITE `internal/server/api_ash.go` (`group_by` whitelist near line 137, `ashFilters`, the SQL builders that use them, `/api/v1/ash` and `/api/v1/ash/top`), `internal/server/api_ash*_test.go`, `api/openapi.yaml` (the `group_by` enum and the new query parameters), then G-API outputs `web/src/api/generated.ts`, `docs/api.md`.
- **Change:**
  Preconditions: 2.3 DONE.
  Contract: `group_by` accepts `usename`, `client_addr`, `application_name` in addition to the current values. New optional query parameters with the same three names filter by equality; an empty parameter means "no filter"; the literal value `-` filters for `''` (unknown). Column names come only from the whitelist map, never from the request text. Responses label an empty group value as `""`.
  Steps:
  1. S1 — whitelist and filters; every place that builds SQL from `ashFilters` gets the three conditions with bind parameters.
  2. S2 — OpenAPI; run G-API; stage the generated files.
- **Unit tests:** `TestASHD009` (`group_by=usename` accepted; `group_by=foo` → 400), `TestASHD010` (the built SQL for `usename=app` has one more bind parameter and no request text in the SQL string), `TestASHD011` (`client_addr=-` builds `client_addr = ''`). Gate G-GO-FOCUS `./internal/server/`.
- **e2e tests:** `TestINTASHD004` (`./internal/server/` integration): with rows for users `a` and `b`, `/api/v1/ash?group_by=usename` returns two groups whose samples sum to the total; `/api/v1/ash/top?usename=a` returns only `a`'s queries. Gate G-GO-INT.
- **Done:** tests pass; G-API clean; reviewed; unit closed and committed.

### 2.5 Console: group and filter controls, help, README
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** READ `web/CLAUDE.md`; WRITE `web/src/lib/ashGroups.ts`, `web/src/lib/ash.ts`, `web/src/features/ash/{AshPage,AshBreakdown,AshLegend,AshTopQueries}.tsx` as needed, `web/src/i18n/messages/en/ash.ts`, `web/src/i18n/messages/it/ash.ts`, the help topic files for Activity in `web/src/lib/help/topics/{en,it}/`, tests beside each file, `README.md`.
- **Change:**
  Preconditions: 2.4 DONE.
  Contract: the "group by" control lists three more options with labels `User`, `Client address`, `Application` (`it`: `Utente`, `Indirizzo client`, `Applicazione`). An empty group value is shown as `(unknown)` / `(sconosciuto)`. Clicking a group value sets the matching filter; the filter shows as a removable chip; filters are kept in the URL query like the existing ones. Follow the chart rules of the project (independent charts, no wheel zoom).
  Steps:
  1. S1 — group options and labels; S2 — filter chips and URL state; S3 — help text and README ("Activity" section: the three dimensions and `checks.ash.dimensions`).
- **Unit tests:** `UI-ASHD-001` (the three options render in `en` and `it`), `UI-ASHD-002` (choosing `User` requests `group_by=usename`), `UI-ASHD-003` (click on a group adds `usename=<value>` to the request and a chip), `UI-ASHD-004` (empty value renders `(unknown)` and filters with `-`). Gates G-WEB-STATIC, G-WEB-UNIT.
- **e2e tests:** N/A at unit level; the lab check is unit 15.2.
- **Done:** tests pass; README updated; reviewed; unit closed and committed.

## Phase gates and closure
- Required gates: G-GO-FULL, G-GO-INT (`TestINTASHD00[1-4]`), G-RO, G-API, G-WEB-FULL, G-I18N; assertions: old ASH tests unchanged and green.
- README obligation: units 2.2 and 2.5.
- Every sub-phase DONE or explicitly justified SKIPPED. Run full phase gates now.
- agent-1 reviews integration, I-1, I-2, I-5, tests, documentation.
- Open closure unit P2, record review and result, mark the phase DONE, close, commit `phase(007/P2): PHASE 2 COMPLETE — ASH dimensions; gates and review passed`.
- P2 edits no production code or tests. Blocked gate: P2 stays OPEN and BLOCKED. Failing gate or rejected review: attempt failed; reopen the responsible sub-phase, then rerun P2.
