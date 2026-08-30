# Phase 11 — ASH and wait analysis

> **Intent:** the product's differentiator as a page: where the database spent
> its time, drillable from wait-event type to wait event to query, with the
> statistical nature of the data stated rather than implied.
> **Shippable alone?** yes.
> **Preconditions:** phase 10 DONE.

## State contract (mandatory)

1. Before touching anything: read [STATE.md](STATE.md). If §1 `Status` is `OPEN`,
   finish or revert that unit first (§6 says how far it got). Run the gate
   commands in STATE.md **§3** and check the result against what §1, §7, and §11
   claim; the repo wins, so correct the file when they disagree.
2. **Open the sub-phase in STATE.md §1 before editing any code**: `Type:
   sub-phase`, its `ID`, `Status: OPEN`, `Intent`, `Next action:`, and §6 set to
   `claimed — nothing written yet`.
3. **Close it after the gates are green**: append the §4 ledger row, reset §6 to
   `none — tree consistent`, update §5 §7 §8 §9 §10 and the §11 board, point §1
   at the next unit with `Status: none`, bump the timestamp. When STATE.md §3 has
   WIP commits on, commit the closed sub-phase and put its sha in the §4 row. A
   sub-phase is not done until this is written.
4. If the session ends mid-sub-phase, leave §1 `OPEN` and write exactly what is
   half-finished into §6 before stopping — plus a `wip(<N.Y>)` commit when WIP
   commits are on.

## Contract facts this page must respect

Read these before writing any code; every one of them is a display requirement,
not a footnote.

- `avg_active_sessions = samples / ticks`. When `ticks == 0` the API returns
  `null`, never a division by zero. The UI renders `Unknown`.
- A range with fewer than 60 total samples carries a `warning` field. The UI must
  surface it prominently, not in a tooltip: conclusions from such a range are
  unreliable.
- When ASH is switched off in the agent the response is `{"enabled": false}`.
  That is **not** an empty result and must render the `Disabled` primitive naming
  `checks.ash.interval` and the configuration file.
- At most 100 distinct wait keys per 10-second window are kept individually; the
  remainder is folded into an `other` bucket, and the **total sample count is
  conserved exactly**. The UI must label `other` as folded, not as a wait event.
- Attribution to a query requires `compute_query_id = on`. Without it, grouping
  by `queryid` is meaningless and the UI must say why rather than showing an
  empty chart.
- Sampling is statistical at 1 s; queries shorter than roughly one second are
  under-represented. A permanent note states this, per the product's own
  documentation.

---

## Sub-phases

### 11.1 ASH derivation library

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/lib/ash.ts` (new)
- **Change:** pure functions:
  - `toStackedSeries(buckets, groupBy)` → one series per group over a shared
    time axis, with absent buckets as `0` **only** where the bucket exists and
    the group genuinely had no samples, and as a gap where the bucket is missing
    entirely. This distinction is the whole test surface of this phase: a group
    with no samples in a sampled window really is zero; a window that was never
    sampled is unknown.
  - `orderGroups(series)` → deterministic stacking order: `CPU` first, then wait
    types by total descending, then `other` last. A stack that reorders between
    polls is unreadable.
  - `foldOther(series, maxSeries)` → folds the tail into an `other` series for
    display while preserving the total, and returns the number folded so the UI
    can say "and N more".
  - `totalSamples(buckets)` and `isUnderSampled(buckets)` → the fewer-than-60
    rule.
  - `avgActiveSessions(samples, ticks)` → `null` when `ticks === 0`.
  - `topQueries(entries)` → sorted by samples descending with the `queryid`
    kept as a **string** (invariant I-5).
- **Unit tests (pure):**
  `UI-ASH-001 a group with no samples in a sampled window is zero`;
  `UI-ASH-002 a missing window is a gap, not zero`;
  `UI-ASH-003 stacking order is CPU, then descending total, then other`;
  `UI-ASH-004 folding preserves the total sample count exactly`;
  `UI-ASH-005 folding reports how many series were folded`;
  `UI-ASH-006 avgActiveSessions returns null for ticks=0`;
  `UI-ASH-007 isUnderSampled is true below 60 total samples and false at 60`;
  `UI-ASH-008 topQueries keeps queryid as a string above
  Number.MAX_SAFE_INTEGER`;
  `UI-ASH-009 ordering is stable across two identical inputs`.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 11.2 The stacked wait chart

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation.
  **`agent-1:opus` review gate** — the stacking, the `other` bucket and the
  gap-versus-zero distinction are the page's correctness core.
- **Files:** `web/src/components/charts/ash.options.ts`,
  `web/src/features/ash/AshChart.tsx` (new)
- **Change:**
  1. A pure ECharts option builder producing a stacked area chart over time,
     with `connectNulls: false`, a fixed colour per wait-event type from the
     palette tokens (`Lock`, `IO`, `CPU`, `LWLock`, `Client`, `IPC`, `Timeout`,
     `BufferPin`, `Extension`, `Activity`, and `other`), and a tooltip showing
     samples, ticks and `avg_active_sessions` per group.
  2. `other` is rendered in a deliberately neutral colour and its legend entry
     reads "other (folded)", with the folded count in the tooltip. It must not be
     mistakable for a wait event named "other".
  3. The y-axis is average active sessions, and the chart carries a reference
     line at the instance's CPU count when the host metrics are available —
     average active sessions above the core count is the signal operators
     actually look for. When the host is remote and the count is unknown, omit
     the line rather than guessing.
  4. Brushing writes an absolute range to the URL (phase 7 § 7.3).
- **Unit tests (pure):**
  `UI-ASH-010 the option stacks every group on one axis`;
  `UI-ASH-011 connectNulls is false`;
  `UI-ASH-012 other is last in the stack and carries the folded label`;
  `UI-ASH-013 each wait type maps to its fixed palette token`;
  `UI-ASH-014 the reference line is omitted when the core count is unknown`.
  **Component:**
  `UI-ASH-015 the wrapper passes the built option through` (mocked per T-5);
  `UI-ASH-016 brushing writes an absolute range`.
- **e2e tests:** `SYS-UI-007` asserts the chart paints in a real browser.
- **Done:** gates green; `agent-1:opus` has reviewed; closed in `STATE.md`.

### 11.3 Drill-down: type to event to query

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/ash/AshPage.tsx`,
  `AshBreakdown.tsx`, `AshTopQueries.tsx` (new)
- **Change:**
  1. `group_by` is URL state (`group`), defaulting to `wait_event_type`.
     Selecting a stacked series drills to `wait_event` filtered to that type;
     selecting again drills to `queryid`.
  2. The breadcrumb of the drill path is visible and each level is clickable back
     out. A drill-down with no way back is a dead end.
  3. `AshTopQueries` uses `getAshTop` to show queries joined to their text, each
     linking to the Query Inspector (phase 12) with the `queryid` carried as a
     string.
  4. Query text is displayed as the **normalised** text from
     `pg_stat_statements`. State once, in the section, that ASH itself never
     collects live query text — only `query_id` — because that is a deliberate
     PII decision and an operator looking for literal values needs to know they
     will not find them here.
- **Unit tests:**
  `UI-ASH-020 group_by round-trips through the URL`;
  `UI-ASH-021 selecting a series drills to wait_event filtered by type`;
  `UI-ASH-022 the drill breadcrumb navigates back out`;
  `UI-ASH-023 a top query links to the inspector with the queryid as a string`;
  `UI-ASH-024 the PII note is present in the query section`.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 11.4 Honesty: disabled, under-sampled, unattributable

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/ash/AshPage.tsx`
- **Change:**
  1. `{"enabled": false}` → the `Disabled` primitive naming `checks.ash` and the
     agent configuration file, with the sampling-cost trade-off explained in one
     sentence. Distinct from empty.
  2. `warning` present → a prominent banner stating the sample count and that
     conclusions from this range are unreliable, positioned **above** the chart.
     A warning under a chart is a warning nobody reads.
  3. `group_by=queryid` with `compute_query_id` off → `Degraded` naming the
     setting and pointing at the README, instead of an empty chart. Detect it
     from the absence of any non-null `queryid` across the range combined with
     the instance's settings, and say which of the two evidences drove the
     conclusion.
  4. A permanent, unobtrusive footnote on the page: 1 s statistical sampling,
     sub-second queries under-represented, at most 100 wait keys per 10 s window
     with the remainder folded and the total conserved.
- **Unit tests:**
  `UI-ASH-030 enabled=false renders Disabled naming the config key`;
  `UI-ASH-031 Disabled and the empty state render different text`;
  `UI-ASH-032 the under-sampled warning appears above the chart with the count`;
  `UI-ASH-033 queryid grouping without compute_query_id renders Degraded naming
  the setting`;
  `UI-ASH-034 the sampling footnote is always present`;
  `expectNoA11yViolations` for the disabled, warning and populated states.
- **e2e tests:** `SYS-UI-005` in phase 17 uses the existing
  `agent-container-ash-disabled.yml` compose overlay to assert the `Disabled`
  state end to end. That overlay already exists in `test/compose/`; reuse it
  rather than writing a new one.
- **Done:** gates green; closed in `STATE.md`.

### 11.5 Degraded and error paths (rule T-4)

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/ash/AshPage.tsx`
- **Change:** the remaining T-4 rows: an empty range (no samples at all, distinct
  from disabled and from under-sampled), stale data, 401, 500, and a range wider
  than retention.
- **Unit tests (route kind):**
  `UI-ASH-040 an empty range renders the empty state, distinct from disabled`;
  `UI-ASH-041 stale data renders Stale`;
  `UI-ASH-042 a 401 navigates to login exactly once`;
  `UI-ASH-043 a 500 renders ErrorState with a working retry`;
  `UI-ASH-044 the page polls at the ash interval`.
- **e2e tests:** none.
- **Done:** every applicable T-4 row has a named test; gates green; closed in
  `STATE.md`.

### 11.6 Update README.md

Mandatory closing sub-phase. User guide only — no implementation detail.

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — documentation
- **Files:** `README.md`
- **Change:** extend **Wait-event analysis** with the UI: the stacked chart, the
  drill path from wait type to wait event to query, the `other` bucket being a
  fold rather than an event, the under-sampling warning, and what the page shows
  when ASH is disabled or `compute_query_id` is off. Keep the existing `curl`
  examples. One paragraph.
- **Unit tests:** none (documentation).
- **e2e tests:** none — the page was opened against the L3 stack with a
  workload running.
- **Done:** a user can read the wait analysis and its caveats from the README
  alone; gates green; closed in `STATE.md` with the §11 docs row for phase 11
  set.

---

## Phase gates

- **Fmt / Lint / Typecheck:** `make fmt-check`, `make web-lint`,
  `make web-typecheck`
- **Test subset:** `make web-test`
- **Coverage:** `make web-coverage-gate` — `src/lib/ash.ts` at or above 95
- **Regression guard:** `make test` and `make test-e2e` still green
- **README:** the wait-analysis UI paragraph

## Phase done criterion

The ASH page stacks wait events deterministically with `other` labelled as a
fold and the total conserved, distinguishes a zero-sample group from an unsampled
window, renders `Disabled` distinctly from empty, warns above the chart below 60
samples, explains a missing `compute_query_id` instead of showing nothing, and
`STATE.md` §11 shows phase 11 `DONE` with every sub-phase closed.
