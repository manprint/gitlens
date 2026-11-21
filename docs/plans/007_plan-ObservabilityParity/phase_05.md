# Phase 4 — Period A versus period B

Intent: an operator compares two time ranges on top SQL and on waits and sees what got worse.
Prerequisites: P2.
Phase closure: P4; review by agent-1.

## State and ownership contract
Read STATE.md §1 first (and PROTOCOL.md once without this skill) for scope, ownership,
recovery, checks, and commit rules. Open before edits; checkpoint at a recovery boundary;
close with evidence. A delegated worker follows its assigned OPEN unit. Missing design goes
to agent-1, the supervisor. Executor power: low.

## Local design context
Plan revision: 1.
- D7: two read endpoints. Common query parameters: `instance_id` (uuid, required), `database` (optional), `a_from`, `a_to`, `b_from`, `b_to` (RFC 3339, required), `limit` (1–200, default 50).
  Checks, each a 400 with a field name: `a_from < a_to`, `b_from < b_to`, each period at most 31 days, no period in the future by more than 1 minute. Periods may overlap. Unknown instance → 404 (same helper as `handleStatements`).
- Statements compare: `GET /api/v1/statements/compare`, extra parameter `order_by` with the values `handleStatements` accepts today. Per period, aggregate `metrics_statements` exactly as `handleStatements` does for one range (read that SQL first and reuse it as a CTE per period). Join the two results with `FULL OUTER JOIN` on `(datname, queryid)`.
  Response:
  ```json
  {"a":{"from":"…","to":"…"},"b":{"from":"…","to":"…"},"order_by":"…",
   "rows":[{"datname":"…","queryid":"…","query_text":"…|null","status":"both|new|gone",
            "a":{"calls":…,"exec_time_ms":…,"rows":…,"shared_blks_read":…,"mean_time_ms":…}|null,
            "b":{…}|null,
            "delta":{"calls":…,"exec_time_ms":…,"mean_time_ms":…},
            "ratio":{"exec_time_ms":…|null,"mean_time_ms":…|null}}]}
  ```
  The metric names inside `a` and `b` are the ones `handleStatements` returns today; use those names, not the example ones, when they differ. `status`: `new` = only in B, `gone` = only in A. `delta` = B − A with a missing side counted as 0. `ratio` = B / A, `null` when A is 0 or missing. Because the periods can have different lengths, totals are compared as rates: each total is divided by its period length in seconds before `delta` and `ratio` are computed, and the response has `"normalized":"per_second"`. Order: by `abs(delta.<order_by metric>)` descending, then `queryid`. `queryid` is a string, as in the existing API.
- ASH compare: `GET /api/v1/ash/compare`, extra parameters `group_by` (the whitelist of `/api/v1/ash` after phase 2) and the same filters. Per period compute average active sessions per group with the same formula `/api/v1/ash/top` uses (read it; it divides samples by ticks). Response: same shape with `"key"` (group value) instead of `datname/queryid`, and `a`, `b`, `delta`, `ratio` holding `aas`.
- Console: Queries and Activity each get a `Compare` view. Presets: `Previous period` (A = the range of equal length just before B), `Same time yesterday`, `Same time last week`, `Custom`. B defaults to the page's current range.

## Sub-phases

### 4.1 `GET /api/v1/statements/compare`
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** READ `internal/server/api.go` (`handleStatements` near line 1262, its SQL, range parsing helpers, route registration near line 92); NEW `internal/server/api_compare.go`, `api_compare_test.go`, `api_compare_integration_test.go`; WRITE `internal/server/api.go` (one route line), `api/openapi.yaml`, G-API outputs.
- **Change:**
  Preconditions: P2 DONE.
  Contract: as in Local design context. Function `parseComparePeriods(q url.Values, now time.Time) (a, b period, err error)` is shared with 4.2. The handler runs one SQL statement with eight time bind parameters.
  Steps: 1. S1 — `parseComparePeriods` and tests. 2. S2 — SQL and handler; `query_text` joined as `handleStatements` does. 3. S3 — route, OpenAPI schema `StatementCompare`, G-API.
- **Unit tests:** `TestCMP001` (period table: reversed range, 32-day period, missing parameter, future → 400 with the field name), `TestCMP002` (`delta`, `ratio`, `status` for both/new/gone from three fake aggregated rows, including the per-second normalisation for periods of 1 h and 2 h), `TestCMP003` (order by absolute delta). Gate G-GO-FOCUS `./internal/server/ -run 'TestCMP00[123]'`.
- **e2e tests:** `TestINTCMP001`: seed `metrics_statements` so that query 1 has rate 10 in A and 30 in B, query 2 only in A, query 3 only in B; expect statuses `both`, `gone`, `new`, ratio 3 for query 1, and query order by absolute delta. Gate G-GO-INT.
- **Done:** tests pass; G-API clean; reviewed; unit closed and committed.

### 4.2 `GET /api/v1/ash/compare`
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** READ `internal/server/api_ash.go` (the top handler near line 564 and its SQL builder); WRITE `internal/server/api_compare.go` and tests, `internal/server/api_ash.go` (route registration only), `api/openapi.yaml`, G-API outputs.
- **Change:**
  Preconditions: 4.1 DONE.
  Contract: as in Local design context; `group_by` required; reuses the whitelist map and `ashFilters` of `api_ash.go` (do not copy them).
  Steps: 1. S1 — SQL builder that calls the existing per-range builder twice. 2. S2 — handler, route, OpenAPI schema `AshCompare`, G-API.
- **Unit tests:** `TestCMP004` (`group_by` missing or unknown → 400), `TestCMP005` (row maths for `aas`). Gate G-GO-FOCUS.
- **e2e tests:** `TestINTCMP002`: seed `metrics_ash` with wait `Lock` at 1 average session in A and 4 in B → ratio 4, first row. Gate G-GO-INT.
- **Done:** tests pass; reviewed; unit closed and committed.

### 4.3 Console compare views, help, README
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** NEW `web/src/lib/compare.ts`, `compare.test.ts`, `web/src/features/queries/QueryCompare.tsx`, `web/src/features/ash/AshCompare.tsx` and tests; WRITE `web/src/features/queries/QueryListPage.tsx`, `web/src/features/ash/AshPage.tsx` (a `Compare` tab or toggle following the page's existing view-switch pattern), `web/src/i18n/messages/{en,it}/{queries,ash}.ts`, help topics for Queries and Activity (`en`, `it`), `README.md`.
- **Change:**
  Preconditions: 4.2 DONE.
  Contract: `compare.ts` exports `presetPeriods(preset, bFrom, bTo)` returning A, and `formatRatio(r: number | null)` (`×3.0`, `new`, `gone`, `—`). The table has columns: query or group, A, B, change, ratio; a row that got worse by ratio ≥ 2 is marked with the existing warning style; sortable with the shared table component; rows link to the query inspector as the list page does. Texts in `en` and `it`.
  Steps: 1. S1 — `compare.ts` and tests. 2. S2 — the two views. 3. S3 — help and README section "Comparing two periods".
- **Unit tests:** `UI-CMP-001` (`presetPeriods` for the three presets), `UI-CMP-002` (`formatRatio` cases), `UI-CMP-003` (Queries compare requests `/statements/compare` with A and B of the chosen preset), `UI-CMP-004` (Activity compare renders `new` and `gone` rows), `UI-CMP-005` (Italian texts render). Gates G-WEB-STATIC, G-WEB-UNIT.
- **e2e tests:** N/A at unit level; lab check in 15.2.
- **Done:** tests pass; README and help updated; reviewed; unit closed and committed.

## Phase gates and closure
- Required gates: G-GO-FULL, G-GO-INT (`TestINTCMP00[12]`), G-API, G-WEB-FULL, G-I18N.
- README obligation: unit 4.3.
- Every sub-phase DONE or explicitly justified SKIPPED. Run full phase gates now.
- agent-1 reviews the SQL reuse (no drift from `handleStatements`), normalisation, I-5, documentation.
- Open closure unit P4, record review and result, mark the phase DONE, close, commit `phase(007/P4): PHASE 4 COMPLETE — Period comparison; gates and review passed`.
- P4 edits no production code or tests. Blocked gate: P4 stays OPEN and BLOCKED. Failing gate or rejected review: attempt failed; reopen the responsible sub-phase, then rerun P4.
