# Phase 9 — Plan insights, statistics and comparison

Intent: the plan viewer explains more problems per node, shows how each plan performed over time, and compares any two plans of a query.
Prerequisites: P7.
Phase closure: P9; review by agent-1.

## State and ownership contract
Read STATE.md §1 first (and PROTOCOL.md once without this skill) for scope, ownership,
recovery, checks, and commit rules. Open before edits; checkpoint at a recovery boundary;
close with evidence. A delegated worker follows its assigned OPEN unit. Missing design goes
to agent-1, the supervisor. Executor power: low.

## Local design context
Plan revision: 1.
- D12. Insights are computed in the console: `web/src/lib/planTree.ts` (`PlanInsight` near line 83, the function that builds the list near line 413). Existing insight kinds: `slowest`, `estimate`, `discards`, `hashBatches`, `hashAggSpill`, `heapFetches`, `seqLoop`, `never`, `planOnly`. Read how `heapFetches` is built and tested, and copy that pattern (kind, node reference, translated title through `queries.plan.insight.*`, numbers passed as arguments).
- Six new kinds. Plan JSON key names are PostgreSQL's. "Loops" = the node's `Actual Loops` (1 when absent).
  | Kind | Fires when | Arguments | English title |
  |------|-----------|-----------|---------------|
  | `lossyBitmap` | node type `Bitmap Heap Scan` and `Lossy Heap Blocks` > 0 | `lossy`, `exact` (`Exact Heap Blocks`) | `Bitmap scan went lossy on {lossy} of {total} heap blocks; work_mem is too small for the bitmap` |
  | `workersShort` | node type `Gather` or `Gather Merge` and `Workers Launched` < `Workers Planned` | `launched`, `planned` | `Only {launched} of {planned} parallel workers started` |
  | `sortSpill` | node type `Sort` or `Incremental Sort` and `Sort Space Type` = `Disk` | `kb` (`Sort Space Used`) | `Sort spilled {size} to disk` |
  | `jitShare` | top-level `JIT.Timing.Total` ≥ 50 ms and ≥ 20 % of `Execution Time` | `ms`, `pct` | `JIT compilation took {ms} ({pct} of execution)` |
  | `triggerShare` | sum of top-level `Triggers[].Time` ≥ 20 % of `Execution Time` | `ms`, `pct`, `name` (slowest trigger) | `Triggers took {ms} ({pct} of execution); slowest: {name}` |
  | `joinFilterDiscards` | join node (`Nested Loop`, `Hash Join`, `Merge Join`) where `Rows Removed by Join Filter` × Loops ≥ 10 000 and ≥ 10 × (`Actual Rows` × Loops) | `removed`, `kept` | `Join filter discarded {removed} rows to keep {kept}` |
  `jitShare` and `triggerShare` have no node; follow how `planOnly` (also node-less) is represented. Order of the list stays "most severe first" by the existing rule; the new kinds rank after `estimate` and before `never`.
- Statistics come from `plan_samples` (phase 7). `p95` uses `percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms)`.

## Sub-phases

### 9.1 Six new plan insights
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** WRITE `web/src/lib/planTree.ts`, `web/src/lib/planTree.test.ts`, `web/src/i18n/messages/{en,it}/queries.ts`, `web/src/features/queries/PlanView.tsx` and test only if a new kind needs an icon or style entry.
- **Change:**
  Preconditions: P7 DONE.
  Contract: per the table. Numbers are formatted by the caller with `useFormat()` (sizes from kB, durations from ms, percentages). A plan without `Execution Time` (not analyzed) never yields `jitShare` or `triggerShare`.
  Steps: 1. S1 — add the kinds to the union type and one builder function per kind. 2. S2 — messages in `en` and `it` (technical words such as `work_mem`, JIT, `Bitmap Heap Scan` stay English). 3. S3 — tests: one small plan JSON per kind that fires and one that does not.
- **Unit tests:** `UI-PLI-001` … `UI-PLI-006` (one per kind in the table order: fires on the positive plan with the expected arguments, silent on the negative one), `UI-PLI-007` (a plan that triggers nothing new yields exactly the insights it yielded before — snapshot of an existing fixture), `UI-PLI-008` (Italian titles render with the numbers). Gates G-WEB-STATIC, G-WEB-UNIT `src/lib/planTree.test.ts`.
- **e2e tests:** N/A — pure library.
- **Done:** tests pass; no existing planTree test changed; reviewed; unit closed and committed.

### 9.2 `GET /api/v1/plans/stats`
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** WRITE `internal/server/api_plan_samples.go` and tests, `api/openapi.yaml`, G-API outputs.
- **Change:**
  Preconditions: 9.1 DONE.
  Contract: `GET /api/v1/plans/stats?instance_id=&queryid=&database=&from=&to=` (`queryid` required). Response `{"plans":[{"plan_hash","samples","avg_ms","p95_ms","min_ms","max_ms","first_ts","last_ts","analyzed_samples","has_plan"}], "changes":[{"ts","from_plan_hash","to_plan_hash"}]}`. `has_plan` = a `query_plans` row with that hash exists. `changes` = points in time where the plan hash of consecutive samples (ordered by `ts`) differs, at most 100, computed with `lag(plan_hash) OVER (ORDER BY ts)`. Plans ordered by `last_ts DESC`.
  Steps: 1. S1 — SQL and handler. 2. S2 — OpenAPI schema `PlanStats`, G-API.
- **Unit tests:** `TestPLI001` (parameter checks: missing `queryid` → 400), `TestPLI002` (response mapping from fake rows; `p95_ms` passed through). Gate G-GO-FOCUS `./internal/server/`.
- **e2e tests:** `TestINTPLI001`: 20 samples of plan A at 10 ms, then 20 of plan B at 100 ms → two plans with `avg_ms` 10 and 100, `p95_ms` 100 for B, one change A→B. Gate G-GO-INT.
- **Done:** tests pass; reviewed; unit closed and committed.

### 9.3 Console: statistics, plan changes, compare any two plans, documents
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** WRITE `web/src/features/queries/PlanHistory.tsx` and test, NEW `web/src/features/queries/PlanStats.tsx`, `PlanCompare.tsx` and tests, `web/src/api/queries.ts`, `web/src/i18n/messages/{en,it}/queries.ts`, `web/src/lib/help/topics/{en,it}/query-performance.ts`, `README.md`.
- **Change:**
  Preconditions: 9.2 DONE.
  Contract: Plan history shows, per plan, `samples`, average, p95, first and last seen (from `/plans/stats`), and a line `Plan changed at <time>` per change. Two plans can be ticked and compared: `PlanCompare` shows both trees side by side with the output of the existing `diffPlans`, plus a table of their statistics. A plan with `has_plan: false` shows `Plan body not captured` and cannot be ticked. The help text states what the page compares (plans of one query, identified by structure hash) and what each number means.
  Steps: 1. S1 — statistics and changes. 2. S2 — selection and compare. 3. S3 — help and README section "Plan history".
- **Unit tests:** `UI-PLI-009` (statistics render per plan), `UI-PLI-010` (change line renders with the time in the session zone), `UI-PLI-011` (ticking two plans shows the compare view; a third tick replaces the older one), `UI-PLI-012` (plan without body cannot be ticked), `UI-PLI-013` (Italian texts). Gates G-WEB-STATIC, G-WEB-UNIT.
- **e2e tests:** N/A at unit level; lab check in 15.2.
- **Done:** tests pass; README and help updated; reviewed; unit closed and committed.

## Phase gates and closure
- Required gates: G-GO-FULL, G-GO-INT (`TestINTPLI001`), G-API, G-WEB-FULL, G-I18N.
- README obligation: unit 9.3.
- Every sub-phase DONE or explicitly justified SKIPPED. Run full phase gates now.
- agent-1 reviews insight thresholds against the fixtures, I-5, documentation.
- Open closure unit P9, record review and result, mark the phase DONE, close, commit `phase(007/P9): PHASE 9 COMPLETE — Plan insights and statistics; gates and review passed`.
- P9 edits no production code or tests. Blocked gate: P9 stays OPEN and BLOCKED. Failing gate or rejected review: attempt failed; reopen the responsible sub-phase, then rerun P9.
