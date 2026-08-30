# Phase 7 — App shell, navigation, time range

> **Intent:** build the frame every page lives in: routes, navigation, header
> with freshness and theme, the shared time-range control, and the URL-as-state
> contract.
> **Shippable alone?** yes — it ships a navigable, empty-but-honest application.
> **Preconditions:** phase 6 DONE.

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

---

## Route table (normative)

Later phases fill these in; none may add a route outside this table without a
`STATE.md` §8 deviation row.

| Path | Page | Phase |
|------|------|-------|
| `/login` | sign in | 6 |
| `/` | Fleet Overview | 8 |
| `/clusters/:clusterId` | Cluster Detail | 9 |
| `/instances/:instanceId` | Instance Detail | 10 |
| `/instances/:instanceId/ash` | ASH and wait analysis | 11 |
| `/instances/:instanceId/queries` | Query Inspector | 12 |
| `/instances/:instanceId/queries/:queryid` | Query detail and plan history | 12 |
| `/instances/:instanceId/locks` | Locks and Activity | 13 |
| `/findings` | Advisor findings | 14 |
| `/alerts` | Alerts and events | 15 |
| `/settings` | Settings and inventory | 16 |
| `*` | not found | 7 |

---

## Sub-phases

### 7.1 Route tree and code splitting

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/routes/index.tsx` (new), `web/src/App.tsx`
- **Change:**
  1. Define the routes above with `createBrowserRouter` from
     `react-router-dom` 7. Every data page is a `React.lazy` import so the
     initial bundle stays small; the shell, the login page and the error boundary
     are eager.
  2. Each lazy route gets a `Suspense` fallback that is the page's own skeleton,
     not a global spinner: a spinner that replaces the whole shell on every
     navigation makes the application feel broken.
  3. A route-level `errorElement` renders `ErrorState` (phase 6 § 6.6) with the
     route path named, and a "reload" action. An uncaught render error must never
     produce a blank page.
  4. The `*` route renders a not-found page linking back to the fleet overview.
  5. Pages that do not exist yet render a small placeholder naming the surface
     and stating it is not implemented in this build. Placeholders are replaced
     by their own phases; a placeholder must never claim data is unavailable, as
     that would be a lie of a different kind.
- **Unit tests:** `UI-SHELL-001 every route in the table resolves to an element`
  — a table test iterating the route table;
  `UI-SHELL-002 an unknown path renders not found`;
  `UI-SHELL-003 a throwing page renders ErrorState rather than a blank screen`.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 7.2 The shell: header, sidebar, content region

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/components/layout/AppShell.tsx`,
  `Sidebar.tsx`, `Header.tsx`, `Breadcrumbs.tsx` (all new)
- **Change:**
  1. A fixed left sidebar with the primary destinations (Fleet, Findings,
     Alerts, Settings) and a collapsed icon-only mode below 1024 px. The
     instance-scoped destinations are not in the sidebar; they are reached from a
     cluster or an instance and appear as tabs inside those pages.
  2. A header containing: breadcrumbs derived from the route, the global time
     range control (§ 7.3), the freshness badge (phase 6 § 6.7), a connection
     indicator, the theme toggle (§ 7.5) and a sign-out control.
  3. The **connection indicator** is a first-class element, not decoration: when
     the last poll failed it shows "not reachable" with the age of the last
     successful response. An operator must be able to tell "the fleet is quiet"
     from "my browser lost the server" at a glance.
  4. Semantic landmarks: `<nav>` for the sidebar with an accessible name,
     `<header>`, `<main id="main">`, and a visually hidden "skip to content"
     link as the first focusable element.
  5. Keyboard: `g f`, `g a`, `g s` navigate to fleet, alerts and settings; `?`
     opens a shortcut sheet; `/` focuses the page's primary filter when one
     exists. Shortcuts must be disabled while focus is in a text input.
- **Unit tests:**
  `UI-SHELL-010 the sidebar marks the active destination with aria-current`;
  `UI-SHELL-011 breadcrumbs reflect the route`;
  `UI-SHELL-012 the connection indicator shows the last success age after a
  failed poll`;
  `UI-SHELL-013 the skip link is the first focusable element and moves focus to
  main`;
  `UI-SHELL-014 g-then-f navigates to the fleet overview`;
  `UI-SHELL-015 shortcuts are inert while a text input has focus`;
  `expectNoA11yViolations` on the shell.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 7.3 Time range as URL state

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation.
  **`agent-1:opus` review gate** — the URL contract is what makes a link
  shareable and a bug reproducible; getting it wrong later is expensive.
- **Files:** `web/src/hooks/useTimeRange.ts`, `web/src/lib/timerange.ts`,
  `web/src/components/layout/TimeRangePicker.tsx` (all new)
- **Change:**
  1. The time range is **URL state**, never component state. Query parameters:
     `from` and `to` as RFC 3339 for an absolute range, or `range` as a
     shorthand (`15m`, `1h`, `6h`, `24h`, `7d`) for a relative one. A relative
     range is resolved against the current clock at fetch time, so a link shared
     an hour later shows that person's last hour, which is what "last hour"
     means.
  2. `src/lib/timerange.ts` is pure: `parseRange(params, now)` returns
     `{from: Date, to: Date, kind: 'relative'|'absolute', label: string}` or a
     validation failure; `toParams(range)` is its inverse. Invalid or inverted
     ranges fall back to the default `1h` and surface a `Degraded` notice naming
     the parameter — silently correcting a bad URL hides the mistake.
  3. Default range `1h`. Maximum span `30d`, matching the product's 30-day raw
     retention: a wider request returns nothing useful and the picker says so
     rather than issuing it.
  4. `step` for `/api/v1/metrics/query` is derived, never chosen by the user:
     `chooseStep(from, to, maxPoints = 1000)` returns the smallest step from a
     fixed ladder (`15s, 30s, 1m, 5m, 15m, 1h, 6h, 1d`) that keeps the point
     count under `maxPoints`. The server returns 422 above 10 000 points; the
     ladder keeps the UI an order of magnitude inside that, so the error is
     unreachable by normal use.
  5. The picker offers the relative presets, an absolute range entry, and a
     "zoom out" control. Brushing a chart writes an absolute range to the URL —
     that is how a chart selection becomes shareable.
- **Unit tests (pure, `src/lib/timerange.test.ts`):**
  `UI-TIME-001 parses each relative preset against the frozen clock`;
  `UI-TIME-002 parses an absolute pair`;
  `UI-TIME-003 rejects from > to and falls back to the default`;
  `UI-TIME-004 rejects a span above 30d`;
  `UI-TIME-005 toParams round-trips every kind`;
  `UI-TIME-006 chooseStep keeps the point count under the limit` — table over
  15m, 1h, 24h, 7d, 30d, asserting the exact chosen step and that
  `(to-from)/step <= 1000`;
  `UI-TIME-007 chooseStep never returns a step producing more than 10000
  points` — the property that makes the 422 unreachable.
  **Component:** `UI-TIME-010 selecting a preset writes the range parameter to
  the URL`; `UI-TIME-011 an invalid range parameter renders Degraded and uses
  the default`.
- **e2e tests:** none.
- **Done:** gates green; `agent-1:opus` has reviewed the URL contract; closed in
  `STATE.md`.

### 7.4 Page scaffolding primitives

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/components/layout/PageHeader.tsx`,
  `Section.tsx`, `MetricTile.tsx`, `DataTable.tsx` (all new)
- **Change:**
  1. `PageHeader` — title, subtitle, freshness badge slot, action slot. One per
     page.
  2. `Section` — a titled region with an optional description and a consistent
     spacing scale.
  3. `MetricTile` — a single number with a label, a unit and an optional
     sparkline. It accepts `value: number | null` and renders `Unknown` for
     `null`; it has no code path that renders `0` for absence. This is where I-2
     is most likely to be violated, so the type makes the violation impossible.
  4. `DataTable` — a thin wrapper over TanStack Table 9 (R7) providing sorting,
     a column visibility menu, a "showing N of M" footer wired to the
     `Truncated` primitive, sticky headers, tabular figures, and virtualisation
     via `@tanstack/react-virtual` above 200 rows. It exposes an
     `emptyState` prop that is **required**, so no table can render as a blank
     rectangle.
- **Unit tests:**
  `UI-LAYOUT-001 MetricTile renders Unknown for null and never 0`;
  `UI-LAYOUT-002 MetricTile renders the unit next to the value`;
  `UI-LAYOUT-003 DataTable sorts by a column and reflects it in aria-sort`;
  `UI-LAYOUT-004 DataTable renders the required empty state`;
  `UI-LAYOUT-005 DataTable shows Truncated when the budget is reported`;
  `UI-LAYOUT-006 DataTable virtualises above 200 rows` — assert the DOM row
  count is far below the data length (this test may use a `data-testid` on the
  row container under rule T-3);
  `expectNoA11yViolations` on a populated table.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 7.5 Theme, density and preferences

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/hooks/useTheme.ts`, `web/src/lib/preferences.ts` (new)
- **Change:** dark by default (phase 4 § 4.4), with light and system options
  persisted in `localStorage` under a single namespaced key
  `pglens.preferences`. The same store holds table density and the sidebar
  collapsed flag. Reading a corrupt or absent value must fall back to defaults
  without throwing — `localStorage` can be unavailable or full. Apply the theme
  before first paint with a tiny inline script in `index.html` to avoid a
  light-to-dark flash.
- **Unit tests:** `UI-PREF-001 defaults when storage is empty`;
  `UI-PREF-002 defaults when storage holds invalid JSON`;
  `UI-PREF-003 defaults when localStorage throws`;
  `UI-PREF-004 a theme change persists and applies the class`.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 7.6 Global error and offline handling

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/components/layout/ErrorBoundary.tsx`,
  `web/src/hooks/useOnline.ts` (new)
- **Change:** a top-level error boundary rendering `ErrorState` with a reload
  action and the build identifier from `__PGLENS_BUILD__` (phase 4 § 4.3), so a
  screenshot of a broken page identifies the build. `useOnline` listens to the
  browser's online and offline events and drives the connection indicator's
  "browser is offline" case, which must read differently from "server is not
  reachable".
- **Unit tests:** `UI-SHELL-020 the boundary renders ErrorState and the build
  id`; `UI-SHELL-021 offline and unreachable render distinguishable messages`.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 7.7 Update README.md

Mandatory closing sub-phase. User guide only — no implementation detail.

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — documentation
- **Files:** `README.md`
- **Change:** add a short **Web interface** section after **Running the server**
  describing what is usable now: signing in, the navigation destinations that
  exist, the time-range control and the fact that it is reflected in the URL so
  a view can be shared as a link, the freshness indicator, and the keyboard
  shortcuts. Do not describe pages that are still placeholders. Two short
  paragraphs and a shortcut list.
- **Unit tests:** none (documentation).
- **e2e tests:** none — every described behaviour was exercised in a browser.
- **Done:** a user can navigate and share a link from the README alone; gates
  green; closed in `STATE.md` with the §11 docs row for phase 7 set.

---

## Phase gates

- **Fmt / Lint / Typecheck:** `make fmt-check`, `make web-lint`,
  `make web-typecheck`
- **Test subset:** `make web-test`
- **Coverage:** `make web-coverage-gate`
- **Regression guard:** `make test` and `make test-e2e` still green
- **README:** the web interface section

## Phase done criterion

Every route in the table resolves, the time range round-trips through the URL
including a brushed absolute range, `chooseStep` provably cannot produce a 422,
a failed poll is visibly distinct from an offline browser, the shell passes the
accessibility assertion, and `STATE.md` §11 shows phase 7 `DONE` with every
sub-phase closed.
