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
