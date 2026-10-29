# Phase 5 — Per-index history and the index tax

Intent: each index shows how its use and size evolved, and each table shows what its indexes cost (count, bytes, unused bytes, write amplification), completing the 360° index view.
Prerequisites: 4 (Suggestions tab and page shell final), 9.1, 15.1.
Phase closure: P5; review by `advisor`.

## State and ownership contract
Read STATE.md §0–1 first. Open before edits; checkpoint at a recovery boundary; close with evidence. Missing design goes to the `advisor`.

## Local design context
Plan revision: 1.
- Data: `metrics_indexes(ts, instance_id, datname, schemaname, relname, indexrelname, idx_scan, idx_tup_read, idx_tup_fetch, idx_blks_read, idx_blks_hit, index_bytes, is_unique, is_primary, is_valid, def_hash)` sampled every 5 min; `metrics_tables` for table size and write rates. Follow the series conventions of the existing `handleTableHistory` (`internal/server/api_relations.go`): same range/step parameters, same null-bucket semantics (I-1: counters are stored as per-scrape rates and averaged per bucket, so a bucket needs at least one stored sample: step ≥ 600 s for 5 min sampling).
- D33: `GET /api/v1/instances/{id}/indexes/history?database=&schema=&index=&from=&to=&step=` (`getIndexHistory`) → `{index, step_seconds, points:[{ts, idx_scan_rate, idx_tup_read_rate, index_bytes}]}`; a whitelist of fixed columns (I-3); 400 missing/oversized parameters, 404 unknown instance or index.
- Index tax (computed in the browser from data already loaded by `useInstanceIndexes` plus table facts, pure functions in `web/src/lib/indexTax.ts`): per table `indexCount`, `indexBytes`, `indexToTableRatio`, `unusedBytes` (indexes with zero scans in the observation window **and** not unique/primary/exclusion backing), `writeAmplification = (ins + upd_non_hot + del) per second × indexCount` (an insert/delete touches every index; a HOT update touches none), each labelled estimate (I-5); the basis (advisor-confirmed vs raw counters) uses the same wording rule as `indexCleanup.ts`.
- I-1, I-3, I-5, I-6, I-9, I-13 apply.

## Sub-phases

### 5.1 Index history endpoint, drawer and index tax
- **Model:** agent-2:claude-opus-5-5
- **Assignment:** implement; `advisor` reviews the endpoint SQL diff and the closure screenshots.
- **Files:** READ `internal/server/api_relations.go` (`handleTableHistory` and its tests), `api/openapi.yaml` (`getInstanceTableHistory`), `web/src/features/indexes/{IndexAnalysisPage.tsx,CleanupPanel.tsx,DeadRowsPanel.tsx}`, `web/src/components/charts/{Sparkline.tsx,ChartCard.tsx,metric.options.ts}`, `web/src/lib/{indexAnalysis.ts,indexCleanup.ts,sparkline.ts}`. WRITE `internal/server/api.go` (route), `api_relations.go` or a sibling, `api/openapi.yaml`, `web/src/api/{generated.ts,queries.ts,keys.ts}`, `docs/api.md`, `IndexAnalysisPage.tsx` (+ test), `web/e2e/fullhd_routes.ts` (add the `/indexes?…&tab=tax` entry, I-13). NEW `internal/server/index_history.go` (+ `_test.go`, `_integration_test.go`), `web/src/lib/indexTax.ts` (+ test), `web/src/features/indexes/IndexDetail.tsx` (+ test), `IndexTaxPanel.tsx` (+ test), `web/src/test/fixtures/getIndexHistory.ts`. Also WRITE `web/e2e/console_visual.spec.ts` (`VIS-041`, the unit's live spec; titles start with the test ID).
- **Change:**
  Preconditions: 4.4 done (tabs final); 1.2 done (`ChartCard`); 9.1, 15.1 done.
  Contract: endpoint as in D33 (bucketing with `time_bucket`, `last(index_bytes)`, rate columns as stored — verify in S1 against the `handleTableHistory` approach and the writer in `internal/server/pipeline.go`). UI: in the Indexes tab each index row expands (`IndexDetail`) to: definition (existing `SqlText`), a two-chart panel (scan rate; size) via `ChartCard` + `buildMetricOption` (unit `count` and `bytes`), validity/uniqueness badges, and the last-scan hint (`last_idx_scan` when present, PG ≥ 16). A new tab **Index tax** (`?tab=tax`) shows the per-table table (shared controls, 20 per page; columns Table, Indexes, Index size, Table size, Index/table %, Unused size, Writes/s, Write amplification) with an estimate callout and links to the Cleanup tab filtered by that table.
  Steps: S1 inspect `handleTableHistory` + writer semantics; write the handler + tests. S2 OpenAPI + regenerate + fixture. S3 `indexTax.ts` + tests. S4 `IndexDetail` + tests. S5 `IndexTaxPanel` + tab + tests. S6 live check in 6.2.
  Batches (each ends with its focused gate and a checkpoint, ≤ 6 files): A = S1 server (`index_history.go`, `index_history_test.go`, `index_history_integration_test.go`, `api.go`; gates G-GO-FOCUS, G-GO-INT `IDXHINT`); A2 = S2 contract (`api/openapi.yaml`, regenerated `generated.ts` + `docs/api.md`, `queries.ts`, `keys.ts`, fixture `getIndexHistory.ts`; gate G-API — stage the regenerated files before `web-gen-api`); B = S3–S4 (`indexTax.ts` + test, `IndexDetail.tsx` + test; G-WEB-STATIC, vitest); C = S5 (`IndexTaxPanel.tsx` + test, `IndexAnalysisPage.tsx` + test, `fullhd_routes.ts` entry for `/indexes?…&tab=tax`, `console_visual.spec.ts`; G-WEB-UNIT).
  Recovery boundary: after batch A2 and after batch B (record the touched-file list and the green gates in STATE §6).
  Failure handling: an index that exists in facts but has no history rows → `points: []` and the UI says "No history yet (collected every 5 min)".
- **Unit tests:** IDXH-001 400 for missing `index`, bad `step`, range > 30 d; IDXH-002 404 unknown; IDXH-003 stub rows → exact JSON; IDXH-INT-001 (integration) a bucket with no stored sample is `null` (never interpolated), rate columns are returned as stored, size is the last value of the bucket, an unrelated index is excluded; INDEXTAX-001 counts and ratios; -002 unique/primary indexes never counted as unused bytes; -003 HOT updates excluded from write amplification; -004 zero writes → amplification 0; -005 no table size → ratio `null` (no NaN); INDEXDETAIL-001 expand renders definition and two charts with data; -002 empty history message; -003 axe; TAXPANEL-001 estimate callout and rows; -002 sort/search/pagination controls present; -003 link to Cleanup keeps `table` filter. Command: `go test -race -shuffle=on -count=1 ./internal/server -run IDXH` and `go test -tags=integration -race -count=1 ./internal/server -run IDXHINT`; `cd web && TZ=UTC pnpm exec vitest run src/lib/indexTax.test.ts src/features/indexes`.
- **e2e tests:** `VIS-041` in `web/e2e/console_visual.spec.ts`, run after G-DEPLOY at this unit's closure and again by 6.2 (live: expand an Odoo index → both charts non-blank with ≥ 1 point; Index tax tab lists `res_partner` with plausible numbers vs `pg_indexes_size`/`pg_table_size` from psql within 1 %). The unit's e2e calls `assertFits` for its route(s) (I-13). VIS-041 calls `test.setTimeout(liveTimeout(0, 3 * 30_000))`.
- **Done:** tests pass, regeneration leaves no diff, new charts/cards/tables pass `captionId` (I-16), route entry present and `assertFits` called, `advisor` review of SQL, unit closed.

## Phase gates and closure
- Required gates: G-GO-FOCUS (`./internal/server`), G-GO-INT (`IDXHINT`), G-API, G-WEB-STATIC, G-WEB-UNIT (`src/features/indexes src/lib`), G-DEPLOY, G-VIS (`VIS-041`).
- README obligation: `README.md` Index analysis section (history, Index tax), `docs/api.md`.
- Every sub-phase DONE. `advisor` reviews I-1/I-5. Open P5, record, mark DONE. Commit per STATE §3.
