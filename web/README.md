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
