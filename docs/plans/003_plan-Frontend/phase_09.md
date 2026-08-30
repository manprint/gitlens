# Phase 8 — Fleet Overview

> **Intent:** the landing page: every cluster's health, its instances, its lag,
> and the state of every agent, on one screen that never hides a problem.
> **Shippable alone?** yes — it is the first page with real data.
> **Preconditions:** phase 7 DONE.

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
| cluster list, health, topology summary, instances | `getClusters` | `cluster_id` is a decimal **string** (I-5); `max_replay_lag_seconds`, `standby_count`, `sync_standby_count` and `topology` are omitted or `null` when unknown |
| firing alerts per cluster | `getAlerts` | used for the alert count badge and for surfacing `agent_down` |
| recent topology events | `getEvents` with `limit` | for the "recent activity" strip |

The IDEA product requirement is explicit that **agent state belongs on this
page**, not buried in settings. That is what sub-phase 8.3 implements.

---

## Sub-phases

### 8.1 Fleet derivation library

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/lib/fleet.ts` (new)
- **Change:** pure functions, per rule T-2:
  - `summariseFleet(clusters)` → `{total, ok, degraded, critical, instancesUp,
    instancesDown, worstHealth}`. `worstHealth` is the maximum severity present,
    which is what the fleet header shows.
  - `rankClusters(clusters, alerts)` → the display order: `critical` first, then
    `degraded`, then clusters with firing alerts, then by name. A monitoring
    landing page that sorts alphabetically buries the outage on page two.
  - `clusterAlertCounts(alerts)` → `Map<clusterId, {critical, warning, info}>`,
    keyed by the **string** cluster id.
  - `instanceStaleness(instance, now)` → `{ageSeconds, isDown}` using
    `last_seen` and the `up` flag. `up === false` is authoritative; a fresh
    `last_seen` with `up === false` is still down.
  - `describeHealth(health, reasons)` → a human sentence explaining *why*, for
    the tooltip: which instance is down, or which standby has lag. "degraded"
    with no explanation is a dead end for an operator.
- **Unit tests (pure):**
  `UI-FLEET-001 summarise counts each health class`;
  `UI-FLEET-002 rank puts critical before degraded before alerting before
  alphabetical`;
  `UI-FLEET-003 alert counts are keyed by the string cluster id, including
  9007199254740993`;
  `UI-FLEET-004 an instance with up=false is down regardless of last_seen`;
  `UI-FLEET-005 instanceStaleness uses the frozen clock`;
  `UI-FLEET-006 describeHealth names the failing instance`;
  `UI-FLEET-007 summarise over an empty fleet returns zeros without throwing`.
- **e2e tests:** none.
- **Done:** gates green; `src/lib/fleet.ts` at or above the 95% floor; closed in
  `STATE.md`.

### 8.2 Cluster cards and the fleet grid

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/fleet/FleetPage.tsx`,
  `ClusterCard.tsx`, `FleetSummaryBar.tsx` (new)
- **Change:**
  1. A summary bar with counts by health, instances up and down, and the number
     of firing alerts. Each count is a filter control.
  2. A responsive grid of cluster cards. Each card shows: the cluster name, the
     `cluster_id` rendered in monospace as the exact string, the health badge
     with the `describeHealth` sentence as its accessible description, the
     primary's address, instance and standby counts, the maximum replay lag, the
     permission tiers present, and the alert count badge.
  3. `max_replay_lag_seconds` is `null` when no standby has reported. Render the
     `Unknown` primitive, not `0 s`. This is the single most likely place for
     invariant I-2 to be violated, because zero lag and unknown lag look the
     same to a careless implementation.
  4. `id_source` is displayed when it is not `system_identifier` — a cluster
     identified by `cluster_name` is a cluster whose identity can break at
     failover, and the operator should know before it happens, with a link to
     the README's grant instructions.
  5. Cards are links to `/clusters/:clusterId`; the whole card is one link, with
     nested interactive controls avoided so the accessible name stays sane.
  6. A text filter over cluster name and instance address, plus health filter
     chips, both reflected in the URL (`q`, `health`).
- **Unit tests (component):**
  `UI-FLEET-010 renders one card per cluster in ranked order`;
  `UI-FLEET-011 renders the cluster_id as an exact string, not a number` — uses
  `9007199254740993` (rule T-8);
  `UI-FLEET-012 renders Unknown for a null max_replay_lag_seconds`;
  `UI-FLEET-013 renders a lag of 0.12 s with two decimals`;
  `UI-FLEET-014 shows the id_source warning when identity falls back to
  cluster_name`;
  `UI-FLEET-015 the health badge carries the reason as its description`;
  `UI-FLEET-016 the filter writes q and health to the URL and narrows the grid`;
  `expectNoA11yViolations` on a populated grid.
- **e2e tests:** covered by `SYS-UI-001` and `SYS-UI-003` in phase 17.
- **Done:** gates green; closed in `STATE.md`.

### 8.3 Agent health on the fleet page

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/fleet/AgentHealthStrip.tsx` (new),
  `web/src/lib/fleet.ts`
- **Change:**
  1. A strip above the grid listing every instance whose agent is not reporting:
     derived from firing `agent_down` and `instance_unreachable` alerts joined to
     the instance list, plus any instance whose `last_seen` is older than three
     times the expected push interval.
  2. It is only rendered when non-empty, and when rendered it is visually
     dominant — this is the case where the rest of the page's numbers are
     stale by definition, and the page must say so before it says anything else.
  3. Each row links to the affected instance and names the likely cause from the
     alert's own labels, plus a link to the README's troubleshooting section.
  4. When an agent is down, every cluster card containing one of its instances
     shows the `Stale` primitive over its lag and health values rather than
     presenting them as current.
- **Unit tests:**
  `UI-FLEET-020 the strip is absent when every agent reports`;
  `UI-FLEET-021 the strip lists an instance with a firing agent_down alert`;
  `UI-FLEET-022 the strip lists an instance whose last_seen exceeds three
  intervals even with no alert yet`;
  `UI-FLEET-023 a cluster containing a down agent shows Stale over its health`;
  `expectNoA11yViolations` with the strip present.
- **e2e tests:** `SYS-UI-003` in phase 17 stops the agent container and asserts
  this strip appears.
- **Done:** gates green; closed in `STATE.md`.

### 8.4 Health semantics, exactly as the server defines them

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation.
  **`agent-1:opus` review gate** — a UI that computes health differently from
  the server produces two contradictory answers to the same question.
- **Files:** `web/src/lib/fleet.ts`, `web/src/features/fleet/ClusterCard.tsx`
- **Change:** the UI **displays** the server's `health` field; it never
  recomputes it. The only derivation permitted is the explanatory sentence, and
  that sentence must be consistent with the server's documented rules:
  `ok` = a primary exists, all instances up, no replication lag;
  `degraded` = a primary exists but an instance is down or a replica has
  measurable replay lag; `critical` = no primary, or more than one (split brain).
  Write those three rules as a comment above `describeHealth` with a pointer to
  the README section that is their source, so a future change to the server's
  rules has an obvious place to land. Add a test that fails if the sentence
  contradicts the field.
- **Unit tests:**
  `UI-FLEET-030 an ok cluster's sentence claims no lag and all instances up`;
  `UI-FLEET-031 a degraded cluster's sentence names the down instance or the
  lagging standby`;
  `UI-FLEET-032 a critical cluster with two primaries says split brain`;
  `UI-FLEET-033 a critical cluster with no primary says no primary`;
  `UI-FLEET-034 the sentence never contradicts the health field` — a property
  test over the fixture matrix.
- **e2e tests:** none.
- **Done:** gates green; `agent-1:opus` has reviewed; closed in `STATE.md`.

### 8.5 Degraded and error paths (rule T-4)

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/fleet/FleetPage.tsx`
- **Change:** implement and test every applicable T-4 row for this page:
  empty fleet, `null` lag, stale data past `staleAfter`, 401, 500, and a partial
  failure where `getClusters` succeeds but `getAlerts` fails. The partial case is
  the interesting one: the page must render the clusters **and** state that alert
  counts are unavailable, rather than failing whole or silently showing zero
  alerts. Showing "0 alerts" when the alert endpoint is down is exactly the class
  of lie invariant I-2 exists to prevent.
- **Unit tests (route kind):**
  `UI-FLEET-040 an empty fleet renders the empty state naming the agent setup
  step`;
  `UI-FLEET-041 stale data past the threshold renders Stale in the header`;
  `UI-FLEET-042 a 401 navigates to login exactly once`;
  `UI-FLEET-043 a 500 renders ErrorState naming the endpoint with a retry that
  refetches`;
  `UI-FLEET-044 a failing alerts endpoint keeps the grid and marks alert counts
  unavailable, never zero`;
  `UI-FLEET-045 the page polls at the fleet interval` — advance the clock, assert
  the second request;
  `expectNoA11yViolations` for the empty, error and populated states.
- **e2e tests:** none.
- **Done:** every applicable T-4 row has a named test; gates green; closed in
  `STATE.md`.

### 8.6 Update README.md

Mandatory closing sub-phase. User guide only — no implementation detail.

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — documentation
- **Files:** `README.md`
- **Change:** extend the **Web interface** section with the Fleet Overview: what
  it shows, that unknown lag is displayed as unknown rather than zero, that a
  down agent appears at the top of this page, and that a cluster identified by
  `cluster_name` rather than `system_identifier` is flagged with a link to the
  grant instructions already documented in **Setting up the monitoring role**.
  One paragraph. No component names, no file paths.
- **Unit tests:** none (documentation).
- **e2e tests:** none — the described page was opened in a browser against the
  L3 stack.
- **Done:** a user can interpret the Fleet Overview from the README alone; gates
  green; closed in `STATE.md` with the §11 docs row for phase 8 set.

---

## Phase gates

- **Fmt / Lint / Typecheck:** `make fmt-check`, `make web-lint`,
  `make web-typecheck`
- **Test subset:** `make web-test`
- **Coverage:** `make web-coverage-gate`
- **Regression guard:** `make test` and `make test-e2e` still green
- **README:** the Fleet Overview paragraph

## Phase done criterion

The Fleet Overview renders ranked cluster cards with exact string cluster ids,
displays unknown lag as `Unknown`, surfaces a down agent above everything else,
keeps rendering clusters when the alerts endpoint fails while marking counts
unavailable, passes the accessibility assertion in every state, and `STATE.md`
§11 shows phase 8 `DONE` with every sub-phase closed.
