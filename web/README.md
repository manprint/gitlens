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

## TypeScript pin

TypeScript is pinned to `6.0.3` because `typescript-eslint@8.68.0` requires
TypeScript `<6.1.0`; revisit the pin when typed linting supports a newer
TypeScript release.

The frontend scaffold and its workflow are tracked in
`docs/plans/003_plan-Frontend/phase_05.md`.
