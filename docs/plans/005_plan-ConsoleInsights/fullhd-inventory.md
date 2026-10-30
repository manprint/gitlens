# Full HD fit inventory (phase 15)

Plan 005, revision 1. Measured by `FHD-MEASURE-001` (`web/e2e/fullhd_fit.ts`,
`measureRoutes`) on the live Odoo stack, project `desktop-fullhd`. The raw
numbers are in `test/e2e/_artifacts/ui/fullhd-measure.json` (both viewports).
15.3 adds the per-route fit results; 15.4 re-measures the final list.

## 15.1 S1 measured budget (1920 wide, CSS px)

| Part | D26 estimate | Measured before 15.1 | After 15.1 S2/S3 |
|------|--------------|----------------------|------------------|
| Shell header (56 + 1 px border) | 48 | 57 | 57 |
| `main` vertical padding | 32 | 48 (`py-6`) | 32 (`py-4` from 1600 px) |
| Page header (no eyebrow / eyebrow / eyebrow + 2-line subtitle) | 56 | 89 / 112 / 134 | 89 / 112 / 134 |
| Tab bar | 40 | 36 | 36 |
| Toolbar (search only / labelled filters) | 44 | 32 / 52 | 32 / 52 |
| Gaps (`space-y-3`, toolbar→table, table→footer) | — | 12 + 12 | 12 + 12 |
| Table header | 32 | 41 | 33 |
| Row (one line) | 28 | 41–62 (cells wrap) | 29 (28 + 1 px border) when one line; 37–69 where cells still wrap |
| Pager / footer line | 40 | 32 | 32 |

Every page was 1 px taller than the viewport: `main` had `min-h-[calc(100vh-3.5rem)]`
while the header is 3.5rem plus a 1 px border (fixed in 15.1).

Budget at 1920×950 for a `fit` route (one table, one-line rows):
`57 + 32 + page header + tab bar + toolbar + 12 + 33 + 20 × 29 + 12 + 32`.
With page header 112, no tab bar and a one-line toolbar (32): 57 + 32 + 112 + 32
+ 12 + 33 + 580 + 12 + 32 = 902 ≤ 946. With a tab bar (36 + 16 gap) and a
labelled-filter toolbar (52): 974 > 946, so 15.3 applies the D26 compaction
(toolbar on one line from 1600 px, page subtitle on one line, tiles as one
strip or moved to a tab) and per-column widths/truncation so rows stay one line.

## Routes at 1920×950 after 15.1 (scroll height before → after)

| Route | Before | After | Scroll width | Page header | Tab bar | Toolbar | Table header | Row (rows) | Pager | Above table |
|-------|--------|-------|--------------|-------------|---------|---------|--------------|------------|-------|-------------|
| `/` | 951 | 950 | 1920 | 112 | — | — | — | — (0) | — | — |
| `/queries` | 1747 | 1483 | 1920 | 112 | — | 32 | 33 | 37 (20) | 32 | 531 |
| `/indexes?tab=indexes` | 40969 | 40830 | 1920 | 134 | 36 | 52 | 33 | 49 (20) | 32 | 25234 |
| `/indexes?tab=cleanup` | 1832 | 1788 | 1920 | 134 | 36 | 52 | 33 | 42 (20) | 32 | 777 |
| `/indexes?tab=dead-rows` | 2062 | 2184 | 1920 | 134 | 36 | 52 | 33 | 49 (20) | 32 | 775 |
| `/clusters/:clusterId` | 1617 | 1601 | 1920 | 89 | — | — | — | — (0) | — | — |
| `/instances/:instanceId` | 16370 | 15395 | 1920 | 90 | 36 | 52 | 33 | 69 (20) | 32 | 8939 |
| `/instances/:instanceId/ash` | 951 | 950 | 1920 | 89 | — | — | — | — (0) | — | — |
| `/instances/:instanceId/queries` | 1887 | 1623 | 1920 | 112 | — | 32 | 33 | 50 (20) | 32 | 361 |
| `/instances/:instanceId/queries/:queryid` | 1061 | 1045 | 1920 | 89 | — | — | — | — (0) | — | — |
| `/instances/:instanceId/locks` | 2776 | 2760 | 1920 | 112 | — | — | — | — (0) | — | — |
| `/findings` | 147810 | 147446 | 1920 | 112 | — | 32 | 33 | 29 (20) | 32 | 1001 |
| `/alerts` | 1514 | 1556 | 1920 | 112 | — | — | — | — (0) | — | — |
| `/events` | 1639 | 1589 | 1920 | 112 | — | — | — | — (0) | — | — |
| `/alerts/rules` | 1322 | 1058 | 1920 | 89 | — | 52 | 33 | 29 (20) | 32 | 307 |
| `/alerts/silences` | 951 | 950 | 1920 | 89 | — | — | — | — (0) | — | — |
| `/settings` | 4497 | 4429 | 1920 | 112 | — | 52 | 33 | 48 (1) | — | 329 |

`—`: the page has no such part (no data table at the measurement, or a chart
page). Routes with `pendingUnit` in `web/e2e/fullhd_routes.ts` wait for the
unit that restructures them (15.2 instance tabs, 15.3 cluster tabs, 16.3
locks/activity, 11.1 settings tabs).

## 15.3 per-route results (deploy 15.3g, 2026-10-30)

`FHD-FIT-001` passes every `fit` route without `pendingUnit` at 1920×1080 and 1920×950
(no page or horizontal scrollbar, no table box scrolling, pager on screen); the
`pendingUnit` routes are annotated UNVERIFIED and measured only. Columns below are
the 1920×950 measurement (scroll height at both viewports). Compaction from 1600 px:
one-line page header (36), page gaps 16, one-line Section header and toolbar, rows
29 (`FULLHD_CELL` top-aligns inline content), pager per viewport. Restructures (tabs,
views, controls in toolbar lines) are listed in STATE §8. Routes with no rows on the
live stack fit, but their fit at 20 rows is unproven: 15.4 / 7.x re-measure them on
the HA stack (the cluster `events` tab still renders an unpaged card list).

| Route | Scroll height 1080 / 950 | Scroll width | Page header | Tab bar | Toolbar | Table header | Row (rows) | Pager | Above table | Result |
|-------|--------------------------|--------------|-------------|---------|---------|--------------|------------|-------|-------------|--------|
| `/` | 1080 / 950 | 1920 | 36 | — | — | — | — (0) | — | — | fits (no data table) |
| `/queries` | 1080 / 950 | 1920 | 36 | — | 32 | 33 | 29 (20) | 32 | 189 | fits |
| `/indexes?tab=indexes` | 1080 / 950 | 1920 | 36 | 36 | 32 | 33 | 29 (20) | 32 | 183 | fits |
| `/indexes?tab=indexes&view=summary` | 1080 / 950 | 1920 | 36 | 36 | — | — | — (0) | — | — | fits (no data table) |
| `/indexes?tab=indexes&view=findings` | 1080 / 950 | 1920 | 36 | 36 | 32 | 33 | 29 (20) | 32 | 183 | fits |
| `/indexes?tab=indexes&view=foreign-keys` | 1080 / 950 | 1920 | 36 | 36 | 32 | 33 | 29 (20) | 32 | 183 | fits |
| `/indexes?tab=cleanup` | 1080 / 950 | 1920 | 36 | 36 | 32 | 33 | 29 (20) | 32 | 175 | fits |
| `/indexes?tab=dead-rows` | 1080 / 950 | 1920 | 36 | 36 | 32 | 33 | 29 (20) | 32 | 175 | fits |
| `/clusters/:clusterId?section=topology` | 1080 / 950 | 1920 | 36 | 36 | — | — | — (0) | — | — | fits; unproven at 20 rows (no data) |
| `/clusters/:clusterId?section=lag` | 1080 / 950 | 1920 | 36 | 36 | — | — | — (0) | — | — | charts (no horizontal scroll) |
| `/clusters/:clusterId?section=slots` | 1080 / 950 | 1920 | 36 | 36 | — | — | — (0) | — | — | fits; unproven at 20 rows (no data) |
| `/clusters/:clusterId?section=drift` | 1080 / 950 | 1920 | 36 | 36 | — | — | — (0) | — | — | fits; unproven at 20 rows (no data) |
| `/clusters/:clusterId?section=events` | 1080 / 950 | 1920 | 36 | 36 | — | — | — (0) | — | — | fits; unproven at 20 rows (no data) |
| `/instances/:instanceId?section=overview` | 5936 / 5936 | 1920 | 36 | 36 | — | — | — (0) | — | — | charts (no horizontal scroll) |
| `/instances/:instanceId?section=host` | 1808 / 1808 | 1920 | 36 | 36 | — | — | — (0) | — | — | charts (no horizontal scroll) |
| `/instances/:instanceId?section=settings` | 1080 / 950 | 1920 | 36 | 36 | — | — | — (0) | — | — | fits; unproven at 20 rows (no data) |
| `/instances/:instanceId?section=relations` | 1080 / 950 | 1920 | 36 | 36 | 32 | 29 | 29 (20) | 32 | 193 | fits |
| `/instances/:instanceId?section=catalog` | 1080 / 950 | 1920 | 36 | 36 | — | — | — (0) | — | — | fits; unproven at 20 rows (no data) |
| `/instances/:instanceId?section=logical` | 1080 / 950 | 1920 | 36 | 36 | — | — | — (0) | — | — | fits; unproven at 20 rows (no data) |
| `/instances/:instanceId?section=internals` | 1080 / 950 | 1920 | 36 | 36 | 32 | 33 | 29 (2) | — | 195 | fits |
| `/instances/:instanceId?section=relations&view=indexes` | 1080 / 950 | 1920 | 36 | 36 | 32 | 29 | 29 (20) | 32 | 193 | fits |
| `/instances/:instanceId?section=relations&view=table-bloat` | 1080 / 950 | 1920 | 36 | 36 | 32 | 29 | 29 (20) | 32 | 215 | fits |
| `/instances/:instanceId?section=relations&view=index-bloat` | 1080 / 950 | 1920 | 36 | 36 | 32 | 29 | 29 (14) | — | 193 | fits |
| `/instances/:instanceId?section=catalog&view=sequences` | 1080 / 950 | 1920 | 36 | 36 | 32 | 33 | 29 (20) | 32 | 193 | fits |
| `/instances/:instanceId?section=catalog&view=partitions` | 1080 / 950 | 1920 | 36 | 36 | — | — | — (0) | — | — | fits; unproven at 20 rows (no data) |
| `/instances/:instanceId?section=catalog&view=functions` | 1080 / 950 | 1920 | 36 | 36 | — | — | — (0) | — | — | fits; unproven at 20 rows (no data) |
| `/instances/:instanceId?section=catalog&view=security-definer` | 1080 / 950 | 1920 | 36 | 36 | — | — | — (0) | — | — | fits; unproven at 20 rows (no data) |
| `/instances/:instanceId?section=catalog&view=foreign-keys` | 1080 / 950 | 1920 | 36 | 36 | 32 | 33 | 29 (20) | 32 | 193 | fits |
| `/instances/:instanceId?section=catalog&view=extensions` | 1080 / 950 | 1920 | 36 | 36 | 32 | 33 | 29 (4) | — | 193 | fits |
| `/instances/:instanceId?section=catalog&view=postgis` | 1080 / 950 | 1920 | 36 | 36 | — | — | — (0) | — | — | fits; unproven at 20 rows (no data) |
| `/instances/:instanceId?section=internals&view=slru` | 1080 / 950 | 1920 | 36 | 36 | 32 | 33 | 29 (8) | — | 195 | fits |
| `/instances/:instanceId?section=internals&view=temp` | 1080 / 950 | 1920 | 36 | 36 | — | — | — (0) | — | — | fits; unproven at 20 rows (no data) |
| `/instances/:instanceId/ash` | 1080 / 950 | 1920 | 38 | — | — | — | — (0) | — | — | charts (no horizontal scroll) |
| `/instances/:instanceId/queries` | 1080 / 950 | 1920 | 36 | — | 32 | 33 | 30 (20) | 32 | 183 | fits |
| `/instances/:instanceId/queries/:queryid` | 1080 / 950 | 1920 | 36 | — | — | — | — (0) | — | — | fits (no data table) |
| `/instances/:instanceId/locks` | 2517 / 2517 | 1920 | 36 | — | — | — | — (0) | — | — | pending 16.3 |
| `/findings` | 1080 / 950 | 1920 | 36 | 36 | — | 33 | 29 (20) | 32 | 176 | fits |
| `/findings?view=rules` | 1080 / 950 | 1920 | 36 | 36 | 32 | 33 | 29 (20) | 32 | 176 | fits |
| `/alerts` | 1080 / 950 | 1920 | 36 | 36 | — | — | — (0) | — | — | fits; unproven at 20 rows (no data) |
| `/alerts?section=delivery` | 1080 / 950 | 1920 | 36 | 36 | — | — | — (0) | — | — | fits (no data table) |
| `/events` | 1080 / 950 | 1920 | 36 | 36 | — | 33 | 29 (3) | — | 183 | fits |
| `/events?section=alerts` | 1080 / 950 | 1920 | 36 | 36 | — | 33 | 29 (12) | — | 176 | fits |
| `/alerts/rules` | 1080 / 950 | 1920 | 36 | — | 32 | 33 | 29 (20) | 32 | 176 | fits |
| `/alerts/silences` | 1080 / 950 | 1920 | 36 | — | — | — | — (0) | — | — | fits; unproven at 20 rows (no data) |
| `/settings` (Inventory: instances) and `?view=tiers\|agents`, `?tab=preferences\|retention\|advisor\|notifications\|names\|audit`, `?tab=audit&view=server` | 1080 / 950 | 1920 | 36 | 36 | — | — | — | — | — | fits (SET-E2E-001, FHD-FIT-001, 11.1; tiers 903 px content); placeholders until 11.2–11.5 |
| `/settings?view=databases` | 1080 / 950 | 1920 | 36 | 36 | — | — | — | — | — | fits with one instance; unproven with several instances (one table per instance; R-11.1 follow-up, re-measure at 7.x/15.4) |

## Search states (12.2, D20)

`web/e2e/fullhd_routes.ts` lists the search state of Slowest queries
(`/queries?q=res_partner`), Query inspector
(`/instances/:instanceId/queries?q=res_partner`) and Advisor findings
(`/findings?q=index`), pending 12.3: the box sits on the toolbar line and the
matches fill the same table, so these pages must fit like their default state.

Exception: the search-results state of Index analysis (`/indexes?q=…`, any
tab). The grouped results of "Search everything" are a user action shown above
the tab's table, like an expanded row (D26), so that state is not listed and
may scroll the page. From 1600 px each result list is capped at 160 px and
scrolls inside its group, so at 1920×950 the tab's table header stays on
screen (estimated about 640 px down; 12.3 confirms it live). Without `q` the
page is measured as before.
