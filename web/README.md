# pglens web interface

This directory contains the Vite + React web interface served by
`pglens-server`.

## Local development

Install the pinned dependencies once:

```sh
pnpm install --frozen-lockfile
```

Start the backend on `:8080`, then run the frontend:

```sh
pnpm dev
```

Open the Vite address shown in the terminal, normally `http://localhost:5173`.
Requests to `/api` are proxied to the local server on `:8080`, so the browser
uses the same API and authentication flow as the built interface.

For a production-style server-served build, run the repository commands from
the project root:

```sh
make web-build
make web-lint
make web-typecheck
make web-test
```

The built assets are embedded in the server image or binary. The server serves
them at `/`; the Vite development server is only needed for an iterative local
frontend session.

## Layout

```text
web/
├── src/
│   ├── api/             API client and generated contract types
│   ├── components/      shared UI and feature components
│   ├── features/        domain-oriented screens and interactions
│   ├── hooks/            reusable React hooks
│   ├── lib/             pure derivations and shared utilities
│   └── routes/           route-level composition
├── scripts/              frontend tooling
├── index.html             Vite entry point
└── vite.config.ts         build and development proxy configuration
```

Keep pure derivation in `src/lib/`: functions there should be deterministic,
side-effect-free, and straightforward to test.

## Data tables

Every data table shows 20 rows per page (20, 50 or 100 through the `Rows per
page` select) with a `Showing a–b of n <rows>.` footer that adds
`(filtered from m)` when the search or a filter hides rows. A search box and
filters (a select per categorical column such as state, kind, role or
database; `multi` and `min` filter kinds exist too) narrow the loaded rows
before they reach the table; each sortable header keeps its label
button and gains `Sort <column> ascending/descending` buttons. Missing values
sort last in both directions. Search, filters, sort, page and page size live in
the URL under the table's own prefix (`<tableId>.q`, `.sort`, `.dir`, `.page`,
`.size`, `.f.<filter>`); a value equal to its default is not written (a cleared
default sort is written as `sort=none`), and any change other than the page
returns to page 1.

Use `DataTable` (`src/components/layout/DataTable.tsx`, TanStack columns) with a
`tableId` and a `toolbar` spec, or `SimpleTable` (`SimpleTable.tsx`) for rows
that expand or carry custom cells. Keep toolbar specs and columns module
constants or memoised: the tables memoise on them. The pure parts are in
`src/lib/tableControls.ts`, the URL state in `src/hooks/useTableState.ts`.

## Captions and summary cards

Every card, section, table and chart carries a caption from the catalogue in
`src/lib/captions.ts` (entries live in `src/lib/captionsets/<group>.ts`, one
per route group, merged there). To add one: add an entry to the group's
captionset (id in dot/kebab case such as `index.redundant-prefix`; `what` is one
plain sentence, then `source`, `unit`, `window`, `howToRead`, plus `thresholds`
when a rule applies), then pass `captionId="<id>"` to `MetricTile`, `ChartCard`,
`Section` or `DataTable` (a table also needs `ariaLabel`). A hard column gets
`meta: { help: '<id>' }` with a sentence in `columnHelp`. Terms are defined once
in `src/lib/glossary.ts`; acronyms outside it and the common list fail the
caption tests. `captionCoverage.test.ts` fails when a card has no caption; its
allowlists are empty except the data-driven sites listed there.

A summary card (`MetricTile`) shows one primary figure with a `unit` from the
closed list in `src/lib/units.ts` (`TILE_UNITS`); anything else goes in
`detail`, where each other number is followed by its noun, and a state is a
`status` text. Add a unit to the list only with a test and a reason.
`expectClearCard` (`src/test/clearCard.ts`) checks this in unit tests and the
live specs `VIS-CAP-001..003` (`e2e/console_visual.spec.ts`) check every route.

## How to write a test here

Choose the smallest applicable kind: **pure** for deterministic functions in
`src/lib/`, **component** for one React tree with Testing Library and MSW,
**route** for a complete page with its router and query client, and
**acceptance** for a browser flow in `e2e/` against the real stack. Put reusable
API fixtures in `src/test/fixtures/`, shared fixture values in
`src/test/fixture-helpers.ts`, and HTTP handlers in `src/test/msw/`.

Derivation belongs in `src/lib/`; components render values that those pure
functions have already derived.

## TypeScript pin

TypeScript is pinned to `6.0.3` because `typescript-eslint@8.68.0` requires
TypeScript `<6.1.0`; revisit the pin when typed linting supports a newer
TypeScript release. Keep the lockfile and the pinned version in sync when
updating the frontend toolchain.
