# Phase 9 — Cluster Detail

> **Intent:** the replication view: the topology graph, lag over time, slot
> health, and the failover timeline that proves invariant I-1 to a human.
> **Shippable alone?** yes.
> **Preconditions:** phase 8 DONE.

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

## Data sources

| Need | Operation | Notes |
|------|-----------|-------|
| topology edges and failover events | `getClusterTopology` | edges carry `confidence: high\|low`; low-confidence edges carry a `note` |
| lag series per edge | `getClusterReplication` | `from` and `to` are **required**; three metrics per edge: `write_lag_sec`, `flush_lag_sec`, `replay_lag_sec`; missing intervals are `null` |
| instances and health | `getClusters` (cached) or `getInstances` | reuse the fleet cache when present |
| slot state | `getInstance` per member, or the metrics query for slot gauges | slot retention and inactivity |
| configuration drift | `getClusterSettingsDrift` | only differing settings are returned |
| events | `getEvents` filtered by `cluster_id` | full taxonomy, newest first |

---

## Sub-phases

### 9.1 Replication derivation library

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/lib/replication.ts` (new)
- **Change:** pure functions:
  - `buildGraph(instances, topology)` → `{nodes, edges}` for the graph
    component: node per instance with role, address, version, tier and up state;
    edge per topology entry with `type`, `sync_state`, `confidence` and the
    optional `note`. Unresolved endpoints become a distinct `unresolved` node
    rather than being dropped — an edge that goes nowhere is information.
  - `layoutGraph(graph)` → deterministic positions: the primary at the top,
    standbys ordered by address below it, cascading standbys one level lower
    per hop. Determinism matters: a graph that reshuffles on every 15-second
    poll is unreadable. No force-directed layout.
  - `detectAnomalies(graph)` → `{noPrimary, multiplePrimary, orphanStandbys,
    lowConfidenceEdges}`, each a list of node ids.
  - `alignSeries(edges)` → aligns the three metrics of one edge onto a shared
    timestamp axis, preserving `null` as `null`. It must never carry a value
    forward across a gap and never substitute zero. A helper
    `hasGaps(series)` reports whether any bucket is `null`, so the chart can
    label the gap.
  - `summariseSlots(slots)` → `{inactive, retainedBytesTotal, worst}`.
- **Unit tests (pure):**
  `UI-REPL-001 builds a node per instance and an edge per topology entry`;
  `UI-REPL-002 an unresolved endpoint becomes an unresolved node with the note`;
  `UI-REPL-003 layout is deterministic for the same input` — run twice, assert
  identical positions;
  `UI-REPL-004 layout places cascading standbys one level below their upstream`;
  `UI-REPL-005 detects no primary`;
  `UI-REPL-006 detects two primaries as split brain`;
  `UI-REPL-007 alignSeries preserves null and never interpolates`;
  `UI-REPL-008 alignSeries aligns three metrics onto one axis`;
  `UI-REPL-009 hasGaps reports true for a series containing null`;
  `UI-REPL-010 summariseSlots totals retained bytes and names the worst slot`.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 9.2 The topology graph

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation.
  **`agent-1:opus` review gate** — the graph model is the page's core; a wrong
  edge here misleads during an incident.
- **Files:** `web/src/features/cluster/TopologyGraph.tsx`,
  `web/src/components/charts/topology.nodes.tsx` (new)
- **Change:**
  1. Render `buildGraph`/`layoutGraph` output with `@xyflow/react` 12 (R13),
     with panning and zooming enabled, node dragging **disabled** (positions are
     derived, not user state), and a fit-to-view on first render only.
  2. Node content: role badge (`primary`, `standby`, `unresolved`), address and
     port, PostgreSQL version, permission tier, up state, and replay lag for a
     standby. A down instance is visually unmistakable, not merely a different
     shade.
  3. Edge rendering: solid for `confidence: high`, **dashed for
     `confidence: low`** with the server's `note` as its accessible description —
     this is a stated product requirement, not a stylistic choice. Sync state is
     labelled on the edge (`sync`/`async`); an unknown sync state renders
     `Unknown`, never `async`.
  4. Anomalies from `detectAnomalies` render as a banner above the graph: split
     brain and no-primary are `critical`, orphan standbys and low-confidence
     edges are `warning`.
  5. Accessibility: a graph is not readable by a screen reader. Provide an
     equivalent `<table>` of the same edges — from, to, type, sync state,
     confidence — that is visually hidden but present in the accessibility tree,
     and a visible toggle that shows it for anyone who prefers it. Test the table,
     not the canvas.
  6. Selecting a node navigates to that instance; selecting an edge scrolls to
     and highlights that edge's lag chart.
- **Unit tests:**
  `UI-CLUS-001 renders a node per instance and an edge per topology entry`;
  `UI-CLUS-002 a low-confidence edge is dashed and exposes its note`;
  `UI-CLUS-003 an unknown sync_state renders Unknown, never async`;
  `UI-CLUS-004 split brain renders a critical banner`;
  `UI-CLUS-005 the accessible edge table lists every edge`;
  `UI-CLUS-006 selecting a node navigates to the instance route`;
  `expectNoA11yViolations` on the page including the graph region.
- **e2e tests:** `SYS-UI-001` asserts the graph reflects the promoted primary.
- **Done:** gates green; `agent-1:opus` has reviewed the graph model; closed in
  `STATE.md`.

### 9.3 Replication lag charts

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/components/charts/lag.options.ts`,
  `web/src/components/charts/TimeSeriesChart.tsx`,
  `web/src/features/cluster/LagSection.tsx` (new)
- **Change:**
  1. `lag.options.ts` is a **pure** ECharts option builder (rule T-5, decision
     D14) taking the aligned series and returning the option object. It must:
     - use `connectNulls: false` so a gap is drawn as a gap;
     - use a time axis with UTC formatting;
     - plot the three metrics as three named series with the palette tokens;
     - set the y-axis minimum to `0` but **never** coerce a `null` datum to `0`;
     - include a `dataZoom` configured for brushing, whose selection is lifted to
       the URL time range (phase 7 § 7.3).
  2. `TimeSeriesChart` is the thin wrapper: it renders `echarts-for-react` with
     the built option, a fixed height, and a `role="img"` with an
     `aria-label` summarising the series, plus a visually hidden data table for
     the last N points. Charts are otherwise invisible to assistive technology.
  3. `LagSection` renders one chart per edge with the edge named in the heading,
     a `Truncated`/`Unknown` treatment when the range contains no data, and an
     explicit "gaps in this range" note driven by `hasGaps`.
  4. When the selected range exceeds the retention window, render `Degraded`
     naming the 30-day raw retention rather than an empty chart.
- **Unit tests (pure):**
  `UI-CLUS-010 the option contains one series per metric with the exact names`;
  `UI-CLUS-011 connectNulls is false`;
  `UI-CLUS-012 a null datum stays null in the option's data array`;
  `UI-CLUS-013 the axis is a time axis in UTC`;
  `UI-CLUS-014 the option is deterministic for the same input`.
  **Component:**
  `UI-CLUS-015 the wrapper passes the built option to the chart library` (mocked
  per T-5);
  `UI-CLUS-016 the hidden data table lists the plotted points`;
  `UI-CLUS-017 a brush selection writes an absolute range to the URL`;
  `UI-CLUS-018 a range beyond retention renders Degraded naming 30 days`;
  `UI-CLUS-019 an empty series renders the empty state, not a flat zero line`.
- **e2e tests:** `SYS-UI-007` in phase 17 asserts a chart actually paints in a
  real browser.
- **Done:** gates green; closed in `STATE.md`.

### 9.4 Slots, drift and cluster settings

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/cluster/SlotsSection.tsx`,
  `DriftSection.tsx` (new)
- **Change:**
  1. Slots table: slot name, owning instance, active state, WAL status, retained
     bytes, and the age of inactivity. Inactive slots sort first; retained bytes
     above a threshold are flagged with the reason ("a disconnected standby
     retains WAL and can fill the primary's disk").
  2. Drift section: rows from `getClusterSettingsDrift`, each showing the setting
     name and one column per instance with its value, differing values
     highlighted. Only differing settings are returned, so an empty result is the
     good case and must render an explicit "no drift detected" empty state rather
     than nothing.
  3. Both sections use `DataTable` with a required empty state (phase 7 § 7.4).
- **Unit tests:**
  `UI-CLUS-020 inactive slots sort first`;
  `UI-CLUS-021 retained bytes are formatted in binary units`;
  `UI-CLUS-022 no drift renders the explicit no-drift state`;
  `UI-CLUS-023 a differing setting highlights the differing instances`;
  `UI-CLUS-024 a missing slot endpoint renders ErrorState without hiding the
  rest of the page`.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 9.5 The event timeline

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/cluster/EventTimeline.tsx`,
  `web/src/lib/events.ts` (new)
- **Change:**
  1. `src/lib/events.ts` maps each event type to a severity, a short title and a
     one-line explanation of what to check, taken from the taxonomy already
     published in the README's **Events** table. The mapping is exhaustive over
     the union; the `switch-exhaustiveness-check` rule makes a newly added event
     type a compile error rather than an unlabelled row.
  2. The timeline renders newest first, grouped by day, with the event payload
     expanded for `failover_detected` (old and new primary, both as links) and
     `cluster_id_changed` (which must be rendered as a violation of invariant
     I-1, in the strongest available treatment — it should never happen).
  3. A type filter reflected in the URL. Default shows all types.
  4. An unknown event type renders with a neutral treatment and its raw type
     string, never silently dropped: an event the UI does not recognise is still
     evidence.
- **Unit tests:**
  `UI-CLUS-030 every documented event type has a title and a what-to-check line`
  — table over the full taxonomy;
  `UI-CLUS-031 failover_detected renders old and new primary as instance links`;
  `UI-CLUS-032 cluster_id_changed is rendered as a critical invariant
  violation`;
  `UI-CLUS-033 an unrecognised type renders its raw string rather than being
  dropped`;
  `UI-CLUS-034 the type filter round-trips through the URL`;
  `UI-CLUS-035 events are ordered newest first`.
- **e2e tests:** `SYS-UI-001` asserts the failover entry appears after a
  promote.
- **Done:** gates green; closed in `STATE.md`.

### 9.6 Degraded and error paths (rule T-4)

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/cluster/ClusterPage.tsx`
- **Change:** implement and test the applicable T-4 rows: an unknown cluster id
  (404 → a clear not-found page with a link back to the fleet, not a blank
  screen), a single-instance cluster with no topology (the graph renders one node
  and states that no replication was observed), missing lag data, stale data,
  401, 500, and a partial failure where the topology loads but the replication
  series does not.
- **Unit tests (route kind):**
  `UI-CLUS-040 an unknown cluster id renders not found with a link back`;
  `UI-CLUS-041 a standalone cluster renders one node and no-replication text`;
  `UI-CLUS-042 a failing replication endpoint keeps the graph and marks the
  charts unavailable`;
  `UI-CLUS-043 stale data renders Stale in the header`;
  `UI-CLUS-044 a 401 navigates to login exactly once`;
  `UI-CLUS-045 the page polls at the cluster interval`;
  `expectNoA11yViolations` for the populated, empty and error states.
- **e2e tests:** none.
- **Done:** every applicable T-4 row has a named test; gates green; closed in
  `STATE.md`.

### 9.7 Update README.md

Mandatory closing sub-phase. User guide only — no implementation detail.

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — documentation
- **Files:** `README.md`
- **Change:** extend the **Web interface** section and the **Monitoring a
  replicated cluster** → **Viewing replication state** section: the Cluster
  Detail page shows the topology graph with dashed edges for unresolved
  upstreams, lag over the selected range with gaps drawn as gaps, slot health,
  configuration drift and the event timeline, and the `cluster_id` shown there is
  byte-identical across a failover. Replace nothing; add to the existing `curl`
  examples rather than removing them.
- **Unit tests:** none (documentation).
- **e2e tests:** none — the described page was opened against the L3
  primary-standby stack.
- **Done:** a user can read the replication story from the README alone; gates
  green; closed in `STATE.md` with the §11 docs row for phase 9 set.

---

## Phase gates

- **Fmt / Lint / Typecheck:** `make fmt-check`, `make web-lint`,
  `make web-typecheck`
- **Test subset:** `make web-test`
- **Coverage:** `make web-coverage-gate` — `src/lib/replication.ts` at or above
  95, `src/components/charts/` at or above 90
- **Regression guard:** `make test` and `make test-e2e` still green
- **README:** the Cluster Detail paragraph

## Phase done criterion

The Cluster Detail page renders a deterministic topology graph with dashed
low-confidence edges and an equivalent accessible table, lag charts that draw
gaps as gaps, slot and drift tables with explicit empty states, and an event
timeline covering the full taxonomy including an unrecognised type; every
applicable T-4 row has a test; and `STATE.md` §11 shows phase 9 `DONE` with every
sub-phase closed.
