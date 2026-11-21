# Phase 11 — Query to service link

Intent: load, log events and plans can be read by the application service and endpoint that issued the query, and a trace id links to the tracing tool.
Prerequisites: P2, P6, P7.
Phase closure: P11; review by agent-1.

## State and ownership contract
Read STATE.md §1 first (and PROTOCOL.md once without this skill) for scope, ownership,
recovery, checks, and commit rules. Open before edits; checkpoint at a recovery boundary;
close with evidence. A delegated worker follows its assigned OPEN unit. Missing design goes
to agent-1, the supervisor. Executor power: low.

## Local design context
Plan revision: 1.
- D14, I-2, I-4. R11: sqlcommenter writes one comment at the end of the statement: `/*key='value',key2='value2'*/`; keys and values are URL-encoded; a `'` inside a value is written `\'`.
- Package `internal/sqltag` (no imports from other pglens packages except `internal/redact`):
  `Parse(text string) map[string]string` — trims trailing spaces and one trailing `;`; the text must end with `*/`; takes the last `/*` before it; the comment body must be at most 4096 bytes and match pairs `key='value'` separated by commas (spaces around commas allowed); at most 20 pairs; each key and value is decoded with `url.PathUnescape` after turning `\'` into `'`; a pair that fails to decode is skipped; anything else returns an empty map. Hint comments (`/*+ … */`) are not tags.
  `Service(tags) string` = `application`. `Endpoint(tags) string` = `route` if present, else `controller#action` when both exist, else `controller`, else `action`, else `""`. `TraceID(tags) string` = the second field of `traceparent` when the value matches `^[0-9a-f]{2}-([0-9a-f]{32})-[0-9a-f]{16}-[0-9a-f]{2}$` and the id is not 32 zeros, else `""`.
  `Service` and `Endpoint` results are cut to 128 bytes, must be valid UTF-8 without control characters, and are `""` when `redact.MayHoldCredential` is true for them.
- ASH: the sampler query (`internal/ash/sampler.go` near line 213) selects one more column `right(query, 700) AS query_tail` only when `service` or `endpoint` is in `checks.ash.dimensions`. The tail is parsed in the agent and thrown away; only the two derived strings travel. `checks.ash.dimensions` accepts two more names, `service` and `endpoint`; the default list becomes all five.
  Limit: PostgreSQL cuts `pg_stat_activity.query` at `track_activity_query_size` (default 1024 bytes); a longer statement loses its trailing comment and its tags read `''`.
- Migration 0039 (same pattern as 0033 in phase 2; R7 confirms it runs in the migration transaction):
  ```sql
  ALTER TABLE metrics_ash
    ADD COLUMN IF NOT EXISTS service  text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS endpoint text NOT NULL DEFAULT '';
  DROP INDEX IF EXISTS metrics_ash_dedup_idx;
  CREATE UNIQUE INDEX metrics_ash_dedup_idx ON metrics_ash
    (instance_id, datname, wait_event_type, wait_event, state, COALESCE(queryid, 0),
     usename, client_addr, application_name, service, endpoint, ts);
  ```
- Trace id: `log_events.trace_id` and `plan_samples.trace_id` exist since phases 6 and 7. The agent fills the fact label `trace_id` from the tags of `Entry.Statement` (log events) or of `Query Text` (plans); this works with `statement_text: false` because only the id is shipped.
- Link: server environment `PGLENS_TRACE_URL_TEMPLATE`, an `http://` or `https://` URL that contains `{trace_id}` at least once; anything else is a start-up error. `/api/v1/session` returns it as `trace_url_template` (empty when unset).

## Sub-phases

### 11.1 sqlcommenter parser
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** NEW `internal/sqltag/sqltag.go`, `sqltag_test.go`.
- **Change:**
  Preconditions: P2, P6, P7 DONE.
  Contract: as in Local design context.
  Steps: 1. S1 — `Parse`. 2. S2 — the three accessors. 3. S3 — table tests and a fuzz test (`FuzzParse`: never panics, never returns more than 20 pairs).
- **Unit tests:** `TestQTAG001` (R11 example `SELECT 1 /*action='run',controller='orders',route='%2Forders%2F%3Aid',traceparent='00-5bd66ef5095369c7b0d1f8f4bd33716a-c532cb4098ac3dd2-01'*/` → four tags, route `/orders/:id`), `TestQTAG002` (escaped quote, trailing `;`, spaces), `TestQTAG003` (no trailing comment, a comment in the middle, a hint comment, a body over 4096 bytes, 21 pairs → empty map), `TestQTAG004` (`Endpoint` precedence; `TraceID` rejects a wrong shape and all zeros), `TestQTAG005` (a `Service` value that looks like a credential → `""`; a value of 300 bytes is cut to 128). Gate G-GO-FOCUS `./internal/sqltag/`.
- **e2e tests:** N/A — pure functions.
- **Done:** tests pass; fuzz test runs 10 s clean (`go test -fuzz=FuzzParse -fuzztime=10s ./internal/sqltag/`); reviewed; unit closed and committed.

### 11.2 ASH dimensions `service` and `endpoint`
- **Agent:** agent-2
- **Assignment:** implement; agent-1 reviews the change list before code and the diff after.
- **Files:** the same files phase 2 changed for `usename` (read the phase 2 unit records in history.md for the exact list): `internal/ash/sampler.go`, `internal/ash/aggregator.go`, `cmd/pglens-agent/ash.go`, `internal/agent/config.go`, `internal/store/write.go` (`ASHRow`, both writers), `internal/server/pipeline.go` (`buildASHRow`), `internal/server/api_ash.go`, their tests; NEW `internal/store/migrations/0039_ash_service.sql` (D19); `api/openapi.yaml`, G-API outputs.
- **Change:**
  Preconditions: 11.1 DONE.
  Contract: `SampleRow` and `Key` gain `Service` and `Endpoint`; labels `service` and `endpoint` on `ash_samples`; columns per migration 0039; `group_by` and filters accept both names; the hourly rollup (phase 5) does not keep them and already rejects them (unit 5.3). The key cap of 100 per window and the conserved overflow bucket are unchanged (I-2). With neither name in `dimensions` the sampler query text is byte-identical to the phase 2 one.
  Steps: 1. S1 — list every place phase 2 touched and the matching edit; send to agent-1. 2. S2 — sampler and aggregator. 3. S3 — agent emit and config. 4. S4 — migration, store, pipeline. 5. S5 — API and OpenAPI.
  Recovery boundary: after S3 and after S4.
- **Unit tests:** `TestQTAG006` (sampler row with a tagged tail → `Service`, `Endpoint`; the tail itself is in no field of the row), `TestQTAG007` (totals conserved under the cap with 150 distinct endpoints; guards I-2), `TestQTAG008` (dimensions without the two names → query has no `query_tail`), `TestQTAG009` (pipeline maps the labels; missing labels store `''`), `TestQTAG010` (`group_by=service` accepted; SQL uses the column). Gate G-GO-FOCUS on the touched packages.
- **e2e tests:** `TestINTQTAG001` (`./cmd/pglens-agent/`): a session running `SELECT pg_sleep(3) /*application='shop',route='%2Fcart'*/` on PG 18.6 → an `ash_samples` metric with `service=shop`, `endpoint=/cart`. `TestINTQTAG002` (`./internal/server/`): migration applies on a database that has compressed `metrics_ash` chunks; old rows read `''`; `/api/v1/ash/top?group_by=service` returns `shop`. Gates G-GO-INT, G-RO.
- **Done:** tests pass; all `TestASHD*` still green; reviewed; unit closed and committed.

### 11.3 Trace id on log events and plan samples
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** WRITE `cmd/pglens-agent/logs.go` and tests, `internal/server/pipeline.go` (store the `trace_id` label of `log_event` and `plan` facts; a value that is not 32 lowercase hex characters is stored as NULL), `internal/store/write.go`, `internal/server/config.go` (`PGLENS_TRACE_URL_TEMPLATE`), the session handler that serves `/api/v1/session`, `internal/server/api_logs.go`, `api_plan_samples.go` (filter `trace_id=`), tests, `api/openapi.yaml`, G-API outputs.
- **Change:**
  Preconditions: 11.2 DONE.
  Contract: as in Local design context. `GET /api/v1/logs` and `/plans/samples` accept `trace_id=` (exact match, validated as 32 hex).
  Steps: 1. S1 — agent labels. 2. S2 — pipeline and store. 3. S3 — configuration and session field. 4. S4 — filters, OpenAPI, G-API.
- **Unit tests:** `TestQTAG011` (log entry whose statement carries `traceparent` → fact label `trace_id`; `statement_text` off still ships the id and no statement), `TestQTAG012` (pipeline: `trace_id` label `xyz` → NULL), `TestQTAG013` (template without `{trace_id}`, or with scheme `javascript:` → start-up error; a valid one appears in the session response), `TestQTAG014` (`trace_id=` filter with a bad value → 400). Gate G-GO-FOCUS.
- **e2e tests:** `TestINTQTAG003` (`./internal/server/`): pushed log event and plan sample with a trace id are found by `trace_id=`. Gate G-GO-INT.
- **Done:** tests pass; reviewed; unit closed and committed.

### 11.4 Console, help, README
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** WRITE `web/src/lib/ashGroups.ts` and test (two more group-by entries and filters), `web/src/features/ash/*` where the group-by list is rendered, NEW `web/src/lib/traceLink.ts` and test, `web/src/features/logs/LogsPage.tsx`, `web/src/features/queries/PlanSamples.tsx`, tests, `web/src/i18n/messages/{en,it}/{ash,logs,queries}.ts`, `web/src/lib/help/topics/{en,it}/wait-events.ts`, `README.md`.
- **Change:**
  Preconditions: 11.3 DONE.
  Contract: Activity offers `Service` and `Endpoint` as group-by and filter (`Servizio`, `Endpoint`); the empty value is shown as `(untagged)` / `(senza tag)`. `traceLink(template, id)` returns `null` unless the id is 32 hex characters and the template starts with `http://` or `https://`; it replaces every `{trace_id}`. Rows with a trace id show the first 8 characters as a link (`rel="noopener noreferrer"`, new tab) when a link exists, else as plain text with a copy button.
  README section "Linking queries to services": what sqlcommenter is, one configuration example each for a Python and a Java stack as short pointers to the sqlcommenter documentation (no copied code), the `track_activity_query_size` limit, `PGLENS_TRACE_URL_TEMPLATE` with an example `https://tracing.example.com/trace/{trace_id}`.
  Steps: 1. S1 — group-by and filters. 2. S2 — trace link. 3. S3 — help and README.
- **Unit tests:** `UI-QTAG-001` (group-by list has the two entries in `en` and `it`), `UI-QTAG-002` (`traceLink`: valid, bad id, bad scheme, two placeholders), `UI-QTAG-003` (Logs row renders the link; without template renders text and copy), `UI-QTAG-004` (`(untagged)` label for the empty value). Gates G-WEB-STATIC, G-WEB-UNIT.
- **e2e tests:** N/A at unit level; lab check in 15.2.
- **Done:** tests pass; README and help updated; reviewed; unit closed and committed.

## Phase gates and closure
- Required gates: G-GO-FULL, G-GO-INT (`TestINTQTAG00[123]`), G-RO, G-API, G-WEB-FULL, G-I18N; assertions: I-2 and I-4 tests green; `TestASHD*`, `TestPGL*`, `TestAEX*` green.
- README obligation: unit 11.4.
- Every sub-phase DONE or explicitly justified SKIPPED. Run full phase gates now.
- agent-1 reviews that no query text leaves the agent through this phase, cardinality, link safety, documentation.
- Open closure unit P11, record review and result, mark the phase DONE, close, commit `phase(007/P11): PHASE 11 COMPLETE — Query to service link; gates and review passed`.
- P11 edits no production code or tests. Blocked gate: P11 stays OPEN and BLOCKED. Failing gate or rejected review: attempt failed; reopen the responsible sub-phase, then rerun P11.
