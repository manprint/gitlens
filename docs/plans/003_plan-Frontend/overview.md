# pglens Frontend — Plan Overview

> **Status:** planning | **Authored:** 2026-08-30 by `agent-1:opus`
> **Folder:** `docs/plans/003_plan-Frontend/`
> **Executing this plan? Read [STATE.md](STATE.md) FIRST** — it is the only
> execution-state file: live position, progress board, environment, in-flight
> work, next action. Open a unit in it before touching code, close it after.

## Goal

Ship the pglens web interface: a single-page application, built from `web/` and
served by the existing `pglens-server` binary at `/`, that exposes the nine
product surfaces the backend already supports (fleet, cluster, instance, ASH,
query inspector, locks and activity, advisor, alerts and events, settings).
The plan also lands the minimal backend work a browser client requires — a
published API contract, session authentication for the whole `/api/v1` surface,
and embedded static asset serving — and a frontend test system strong enough
that the UI cannot silently lie about the data it renders. End state: an
operator diagnoses a failover, a wait-event spike, and a bloated index from the
browser, with no `curl`.

```
make build-images
make test-ui-e2e            # boots the L3 primary+standby stack, runs Playwright

# SYS-UI-001 (acceptance):
#  1. browser opens http://127.0.0.1:<port>/  -> login form
#  2. sign in with PGLENS_UI_PASSWORD         -> Fleet Overview, cluster health "ok"
#  3. harness promotes the standby            -> pg_ctl promote
#  4. within two poll intervals the UI shows:
#       - the SAME cluster_id (invariant I-1 visible, not just in the API)
#       - the promoted instance badged "primary"
#       - a failover_detected entry on the Cluster Detail timeline
#       - a firing failover_detected alert on the Alerts page
#  5. zero axe-core serious/critical violations on every page visited
```

## Design decisions

| # | Decision | Consequence |
|---|----------|-------------|
| **D1 (user, Q2)** | Frontend is a **Vite + React 19 SPA**, not Next.js. Supersedes the IDEA.md §6/§13 Next.js choice. | No Node runtime in production; the build output is static and embeddable. Server components, SSR, and file-system routing are unavailable; routing is client-side (`react-router-dom`). |
| **D2 (user, Q3)** | The built SPA is **embedded into `pglens-server`** with `go:embed` and served at `/` on the same port as the API. | No CORS anywhere, no second container, no reverse proxy. The server binary gains a build-time dependency on `web/dist`; a committed placeholder keeps `go build` working when `web/dist` is absent. |
| **D3 (user, Q1)** | **Shared-password session authentication** covers the whole `/api/v1` surface plus `/` — one password, an opaque server-side session cookie, no user table and no RBAC. | Every read endpoint, previously anonymous, starts requiring a session. The agent bearer token path (`internal/server/auth.go`) is untouched and remains the agent's only credential. `/healthz`, `/readyz`, `/metrics` and `POST /api/v1/push` stay outside the session gate. |
| **D4 (user, Q4)** | The API contract is a **hand-written OpenAPI 3.1 document** at `api/openapi.yaml`, with a Go test that fails when a registered chi route is missing from it, and `openapi-typescript` generating the frontend's types. | The spec cannot silently drift from the router. The frontend has no hand-written response types. Adding a route without a spec entry breaks `make test`. |
| **D5 (user, Q9)** | Live data comes from **TanStack Query polling**, not SSE or WebSocket. | No backend streaming work. Freshness is bounded by the poll interval; every view states the data's age rather than implying it is live. |
| **D6 (user, Q10)** | Charts use **Apache ECharts 6** (`echarts-for-react`), sparklines use **uPlot**, the replication topology uses **@xyflow/react**. | Three charting dependencies. Chart correctness is tested by asserting the generated ECharts `option` object, never rendered pixels (see D14). |
| **D7 (user, Q11)** | Testing is **two-tier**: Vitest + Testing Library + MSW for unit and component tests, Playwright against the real L3 Docker stack for the acceptance set. | Every component test runs offline and deterministically; only the acceptance set needs Docker. MSW rejects unhandled requests, so an unmocked call is a test failure, not a silent `undefined`. |
| **D8 (user, Q12)** | Frontend lives in `web/`, uses **pnpm**, and all UI strings are **English**. | Matches the repository's documentation language. `web/` sits beside `cmd/`, `internal/`, `test/`; no new top-level convention is invented. |
| **D9 (user, Q7)** | Scope is **nine of the ten IDEA.md §6 pages**. The Pooler page is out of scope because no pgbouncer collector exists. | `docs/LIMITS.md` gains an explicit statement that pglens has no pooler view. No agent or protocol change in this plan. |
| **D10 (user, Q8)** | The Settings page is **read-only inventory** (agents, instances, permission tiers, databases and skip reasons) plus the two mutable surfaces the API already exposes: alert rules and silences. | No user management, no enrollment approval queue, no agent revocation UI. Revocation stays a documented SQL operation. |
| **D11 (user, Q5)** | Plan 002's deferred question Q-B is **closed with the existing contract**: per-state and per-database connection counts, plus the opt-in bounded `top_n` of application names. | No agent, wire-protocol, or storage change. The Locks and Activity page renders exactly what the `activity` check already reports and labels the per-application breakdown as opt-in when it is absent. |
| **D12 (user, Q6)** | The three `OPEN MINOR` findings from plan 002's V001 audit are **corrected in phase 0 of this plan**, and their status is updated in `docs/plans/002_plan-AnalysisBackend/verify/index.md`. | Plan 003 starts on a clean audit register. Phase 0 writes to plan 002's folder; that is intentional and recorded here so a later audit does not read it as scope creep. |
| **D13** | TypeScript is pinned to **6.0.3**, not the latest 7.0.2. | `typescript-eslint@8.68.0` declares `typescript >=4.8.4 <6.1.0` (R1). Pinning 7.x would leave the project without typed linting. Revisit when typescript-eslint ships TS 7 support. |
| **D14** | Chart and graph components are **thin wrappers over pure option-builder functions**. The builders are unit-tested; the wrappers are smoke-tested with the charting library mocked. | jsdom has no canvas, so rendering ECharts in a unit test is neither possible nor meaningful. Chart correctness becomes a pure-function assertion, which is deterministic and fast. Pixel-level rendering is proven once, in Playwright. |
| **D15** | The UI never renders a missing value as `0` or interpolates across a gap. A dedicated set of **state primitives** (`Unknown`, `Stale`, `Degraded`, `Truncated`, `NotPermitted`, `Disabled`) is the only way a component may display absent data. | Implements IDEA.md §6 "Principio di onestà UI" as code, not as a convention. Every page phase has a test that asserts the primitive appears for its own degraded case. |
| **D16** | Server-provided identifiers that exceed IEEE-754 exact integer range (`cluster_id`, `queryid`) are handled as **strings** end to end and never passed through `Number()`. | The API already returns `cluster_id` as a decimal string. A lint rule and a unit test guard the frontend side; `queryid` is treated the same way even where the API returns it as a JSON number, by reading it from the raw response text where necessary. |
| **D17** | The Playwright acceptance suite is **driven by the existing Go E2E harness**, not by its own Docker orchestration. | `test/e2e/ui_test.go` starts `harness.Start`, then shells out to `pnpm exec playwright test` with the harness's server URL. One stack definition, one teardown path, and UI scenarios register in `test/scenario` like every other L3 scenario. |
| **D18** | Frontend coverage is gated by `scripts/coverage_gate_ui.sh`, mirroring the Go gate: a global floor plus per-directory floors for the layers whose correctness the whole UI depends on. | A new page cannot be merged with untested data shaping. The gate runs in `make ci-local-unit` and in CI. |

## Open questions

| # | Question | Assumed default in this plan | Affects |
|---|----------|------------------------------|---------|
| Q-C | Should the session cookie survive a server restart (persisted session table) or is an in-memory store acceptable? | In-memory store; a server restart logs every browser out. Recorded in `docs/LIMITS.md`. | phase 2 § 2.2 |
| Q-D | Is a single shared password sufficient for the first production deployments, or is per-user login needed before general availability? | Sufficient for this plan. Per-user accounts and RBAC are explicitly out of scope and remain in `docs/LIMITS.md`. | phase 2, phase 17 § 17.4 |

Both are recorded in `STATE.md` §9. Neither blocks any sub-phase; each is a
question about the *next* plan, not this one.

## Architecture summary

The Go server gains three thin layers and no new subsystem: an OpenAPI document
that the router is tested against, a session middleware wrapped around the
existing chi router in `internal/server/http.go:11`, and an embedded static
handler that serves `web/dist` with an SPA fallback. The frontend is a Vite
SPA: a generated type layer over `api/openapi.yaml`, an `openapi-fetch` client
with a single 401 redirect policy, TanStack Query for polling and caching, and
one route per product surface. Data shaping — lag series, ASH stacking, bloat
ratios, topology edges — lives in pure functions under `web/src/lib/`, which is
where the majority of the test suite points. Components render those results
through a fixed set of state primitives so that "unknown" can never be drawn as
zero.

## Interface

| Surface | Name | Type / values | Default | Notes |
|---------|------|---------------|---------|-------|
| env (server) | `PGLENS_UI_PASSWORD` | string | unset | Enables the UI session gate. When unset, the UI is served but every `/api/v1` request is rejected with 503 and the login page states that the server has no UI password configured. |
| env (server) | `PGLENS_UI_PASSWORD_FILE` | path | unset | Reads the password from a file, trailing newline trimmed. Takes precedence over the inline value, matching `PGLENS_BOOTSTRAP_TOKEN_FILE`. |
| env (server) | `PGLENS_UI_SESSION_TTL` | duration | `24h` | Lifetime of a session cookie. Minimum `5m`, maximum `720h`; out-of-range values are a startup error. |
| env (server) | `PGLENS_UI_ENABLED` | bool | `true` | `false` serves no static assets and leaves `/api/v1` gated by nothing but the previous behaviour, for deployments that front pglens with their own proxy. |
| env (server) | `PGLENS_UI_COOKIE_SECURE` | bool | `auto` | `auto` sets the `Secure` attribute when the request arrives over TLS or carries `X-Forwarded-Proto: https`. `true`/`false` force it. |
| HTTP | `POST /api/v1/session` | `{"password":"…"}` | — | 204 with `Set-Cookie: pglens_session=…; HttpOnly; SameSite=Strict; Path=/`. 401 on a wrong password, after a constant-time compare and a fixed 250 ms delay. |
| HTTP | `DELETE /api/v1/session` | — | — | 204, clears the cookie and drops the server-side session. |
| HTTP | `GET /api/v1/session` | — | — | 200 `{"authenticated":true,"expires_at":"…"}` or 401. Used by the SPA on boot to decide between the login route and the app. |
| HTTP | `GET /` and any non-API path | — | — | Serves the embedded SPA (`index.html` fallback for client-side routes). 404 for unknown paths under `/api/`. |
| CLI (make) | `make web-install` `web-lint` `web-typecheck` `web-test` `web-coverage-gate` `web-build` `web-budget` `test-ui-e2e` | — | — | New targets; `ci-local-unit` gains the web chain. |

## Protocol and data-structure changes

| Change | Shape | Backward-compat strategy |
|--------|-------|--------------------------|
| Session cookie | `pglens_session=<32-byte base64url opaque token>`, `HttpOnly`, `SameSite=Strict`, `Path=/` | New surface. No existing client sends or receives it. |
| `/api/v1/*` authentication | Read endpoints and command endpoints require **either** a valid session cookie **or** the existing agent bearer token | The agent keeps working unchanged: `internal/server/auth.go` remains the bearer check, and `POST /api/v1/push` plus the agent command poll/result routes keep using it exclusively. Existing `curl` scripts in the README break by design and are rewritten in phase 17. |
| `api/openapi.yaml` | New file. OpenAPI 3.1, one operation per registered route | Additive. It documents the current contract; it does not change any response shape. |
| Embedded assets | `internal/webui/dist/**` produced by `make web-build`, embedded via `go:embed` | A committed `internal/webui/dist/index.html` placeholder keeps `go build ./...` working in a checkout that never ran `pnpm`. The placeholder states that the UI was not built. |
| Agent wire protocol | **unchanged** | D11 closes Q-B with the existing contract; no protocol version bump. |

## Phases

| Phase | File | Primary assignment | Shippable alone? |
|-------|------|--------------------|------------------|
| 0 — Plan 002 closure and contract prerequisites | [phase_01.md](phase_01.md) | `agent-2:sonnet` | yes |
| 1 — OpenAPI contract and route-coverage gate | [phase_02.md](phase_02.md) | `agent-2:sonnet` | yes |
| 2 — UI session authentication | [phase_03.md](phase_03.md) | `agent-2:sonnet` | yes |
| 3 — Embedded SPA serving and dev proxy | [phase_04.md](phase_04.md) | `agent-2:sonnet` | yes |
| 4 — Frontend workspace scaffold | [phase_05.md](phase_05.md) | `agent-2:sonnet` | yes |
| 5 — Frontend test harness and quality gates | [phase_06.md](phase_06.md) | `agent-2:sonnet` | yes |
| 6 — Typed API client, query layer, state primitives | [phase_07.md](phase_07.md) | `agent-2:sonnet` | yes |
| 7 — App shell, navigation, time range | [phase_08.md](phase_08.md) | `agent-2:sonnet` | yes |
| 8 — Fleet Overview | [phase_09.md](phase_09.md) | `agent-2:sonnet` | yes |
| 9 — Cluster Detail | [phase_10.md](phase_10.md) | `agent-2:sonnet` | yes |
| 10 — Instance Detail | [phase_11.md](phase_11.md) | `agent-2:sonnet` | yes |
| 11 — ASH and wait analysis | [phase_12.md](phase_12.md) | `agent-2:sonnet` | yes |
| 12 — Query Inspector and plan history | [phase_13.md](phase_13.md) | `agent-2:sonnet` | yes |
| 13 — Locks and Activity | [phase_14.md](phase_14.md) | `agent-2:sonnet` | yes |
| 14 — Advisor findings | [phase_15.md](phase_15.md) | `agent-2:sonnet` | yes |
| 15 — Alerts, silences, rules, events | [phase_16.md](phase_16.md) | `agent-2:sonnet` | yes |
| 16 — Settings and fleet inventory | [phase_17.md](phase_17.md) | `agent-2:sonnet` | yes |
| 17 — Packaging, UI acceptance suite, documentation | [phase_18.md](phase_18.md) | `agent-2:sonnet` | yes |

Live status of every phase is in `STATE.md` §11, never duplicated here.

## Reuse map (top candidates)

| Need | Reuse | Location |
|------|-------|----------|
| Single mount point for middleware and new routes | `NewRouter(auth, inv, pipeline, api, topoAPI, ashAPI, alertAPIs...)` | `internal/server/http.go:11` |
| Router call site to rewire | `router := server.NewRouter(...)` | `cmd/pglens-server/main.go:110` |
| Constant-time secret comparison, already reviewed | `(*Auth).Validate` | `internal/server/auth.go:17` |
| Env + `_FILE` configuration loader pattern | `LoadAlertConfig` | `internal/server/config.go:17` |
| Route enumeration for the contract test | `chi.Walk` from `github.com/go-chi/chi/v5 v5.3.2` | `go.mod:7` |
| Read-route registration to enumerate in the spec | `(*API).RegisterRoutes` | `internal/server/api.go:50` |
| JSON error envelope the SPA must parse | `apiError{Error,Detail}` + `writeError` | `internal/server/api.go:89`, `api.go:94` |
| L3 stack lifecycle for the UI acceptance suite | `harness.Start(t, cfg)`, `(*Harness).API()`, `(*Harness).Scenario(t, id)` | `test/harness/harness.go:100`, `:630`, `:917` |
| Scenario registry and coverage traceability | `scenario.Register(scenario.Scenario{ID, Covers, Smoke, Run})` | `test/e2e/smoke_test.go:27` |
| Primary/standby compose topology for acceptance | `topo-primary-standby.yml`, `agent-container-primary-standby.yml` | `test/compose/` |
| Coverage-gate script to mirror for the UI | `scripts/coverage_gate.sh` | `scripts/coverage_gate.sh:1` |
| Existing CI job shape to extend | `ci.yml`, `e2e.yml` | `.github/workflows/` |

## References (external documentation consulted)

| # | What it settled | Source | Version / date |
|---|-----------------|--------|----------------|
| R1 | `typescript-eslint` declares `typescript >=4.8.4 <6.1.0`, so TypeScript 7.0.2 cannot be linted; TypeScript is pinned to the newest supported stable, `6.0.3` | https://registry.npmjs.org/typescript-eslint/latest · https://registry.npmjs.org/typescript | typescript-eslint 8.68.0 · typescript 6.0.3 / 2026-08-30 |
| R2 | Vite 8 requires Node `^20.19.0 \|\| >=22.12.0`; the CI image and the Dockerfile must satisfy it | https://registry.npmjs.org/vite/latest | vite 8.2.2 / 2026-08-30 |
| R3 | `@vitejs/plugin-react` 6 peers `vite ^8.0.0`; its `oxc-transform-react`, `@rolldown/plugin-babel` and `babel-plugin-react-compiler` peers are optional | https://registry.npmjs.org/@vitejs/plugin-react/latest | 6.1.1 / 2026-08-30 |
| R4 | `@vitest/coverage-v8` pins an exact `vitest` version (`4.1.11`); the two must be bumped together | https://registry.npmjs.org/@vitest/coverage-v8/latest | 4.1.11 / 2026-08-30 |
| R5 | `@testing-library/react` 16 peers React 18 or 19 and requires `@testing-library/dom ^10` as a direct dev dependency | https://registry.npmjs.org/@testing-library/react/latest | 16.3.3 / 2026-08-30 |
| R6 | `echarts-for-react` 3 accepts `echarts ^6`, so ECharts 6 is usable through the React wrapper | https://registry.npmjs.org/echarts-for-react/latest | 3.0.6 / echarts 6.1.0 / 2026-08-30 |
| R7 | `@tanstack/react-table` is at v9 and peers `react >=18` | https://registry.npmjs.org/@tanstack/react-table/latest | 9.2.4 / 2026-08-30 |
| R8 | chi exposes `chi.Walk(routes Routes, walkFn WalkFunc) error` to enumerate every registered method and pattern, which is what makes the OpenAPI coverage test possible | https://pkg.go.dev/github.com/go-chi/chi/v5#Walk | chi v5.3.2 (`go.mod:7`) |
| R9 | Tailwind CSS 4 is configured from CSS (`@import "tailwindcss"`) with the `@tailwindcss/vite` plugin; a `tailwind.config.js` is not required | https://registry.npmjs.org/@tailwindcss/vite/latest · https://tailwindcss.com/docs/installation/using-vite | tailwindcss 4.3.3 / 2026-08-30 |
| R10 | `shadcn` CLI 4.x is the current component installer and reads `components.json` | https://registry.npmjs.org/shadcn/latest | 4.19.0 / 2026-08-30 |
| R11 | MSW 2 `setupServer(...).listen({ onUnhandledRequest: 'error' })` turns an unmocked request into a test failure | https://mswjs.io/docs/api/setup-server/listen | msw 2.15.0 / 2026-08-30 |
| R12 | `openapi-typescript` 7 generates types from an OpenAPI 3.1 document and `openapi-fetch` consumes them for a typed client | https://registry.npmjs.org/openapi-typescript/latest · https://openapi-ts.dev/ | openapi-typescript 7.13.0 · openapi-fetch 0.17.0 / 2026-08-30 |
| R13 | `@xyflow/react` 12 peers `react >=17`; it is the maintained package name for React Flow | https://registry.npmjs.org/@xyflow/react/latest | 12.11.5 / 2026-08-30 |
| R14 | Playwright's current release line is 1.62; `@axe-core/playwright` 4.13 provides the accessibility scan used in the acceptance suite | https://registry.npmjs.org/@playwright/test/latest · https://registry.npmjs.org/@axe-core/playwright/latest | 1.62.1 · 4.13.0 / 2026-08-30 |
| R15 | `ajv` 8 with `ajv-formats` 3 validates JSON against the JSON Schema dialect OpenAPI 3.1 uses, which is how fixtures are proven to match the published contract | https://registry.npmjs.org/ajv/latest · https://ajv.js.org/json-schema.html | ajv 8.20.0 · ajv-formats 3.0.1 / 2026-08-30 |
| R16 | `vitest-axe` is stale (0.1.0); accessibility assertions use `axe-core` 4.13 directly through a local helper instead | https://registry.npmjs.org/vitest-axe/latest · https://registry.npmjs.org/axe-core/latest | vitest-axe 0.1.0 · axe-core 4.13.0 / 2026-08-30 |

No point is `UNVERIFIED`: every version above was read from the npm registry on
2026-08-30, and `chi.Walk` from the pinned module in `go.mod`.

## Invariants

- **I-1 (inherited, now visible in the UI):** a cluster identity never changes,
  even after a promote. The acceptance suite asserts the rendered `cluster_id`
  string is byte-identical before and after failover, not only the API's.
- **I-2 (honesty):** the UI never renders an absent value as `0`, never
  interpolates across a gap, and never hides a truncation. Any absent, stale,
  degraded, truncated, unpermitted or disabled datum is rendered through a state
  primitive from `web/src/components/state/`.
- **I-3 (no silent network):** in unit and component tests every HTTP request
  goes through MSW, and an unhandled request fails the test.
- **I-4 (contract):** every route registered on the chi router appears in
  `api/openapi.yaml`, and every frontend response type is generated from that
  document. Neither side may hand-write the other's shape.
- **I-5 (large integers):** `cluster_id` and `queryid` are strings in the
  frontend from the moment they are parsed until the moment they are rendered.
- **I-6 (agent path untouched):** the agent's bearer-token authentication,
  ingest route, and command poll/result routes behave exactly as before this
  plan. The L2 and L3 suites that prove it must stay green in every phase.

## Risk register

| Risk | Mitigation |
|------|-----------|
| Gating every read route behind a session breaks the agent or the L3 suite | Phase 2 § 2.3 accepts *either* a session cookie or the existing bearer token, and § 2.6 runs the full L2/L3 suites before the phase closes. `test/harness/api.go` is updated once, in § 2.5, to send the bearer token. |
| The OpenAPI document drifts from the router as later phases add routes | Phase 1 § 1.4 adds `TestOpenAPICoversEveryRoute`, driven by `chi.Walk`, to the default `make test`. A new route without a spec entry fails the unit gate, not a review. |
| Fixtures used by component tests diverge from real API responses, so the UI is tested against a fiction | Phase 5 § 5.4 validates every fixture against the schema extracted from `api/openapi.yaml` with ajv (R15). A fixture that no longer matches the contract fails the unit gate. |
| jsdom cannot render ECharts, so chart tests become meaningless smoke tests | D14: option builders are pure and unit-tested; wrappers are smoke-tested with the library mocked; real rendering is asserted once in Playwright (`SYS-UI-007`). |
| Playwright flake makes the acceptance gate untrustworthy | Phase 5 § 5.9 sets `retries: 0` locally and `1` in CI, with an explicit rule: any test that passes only on retry is quarantined and logged in `bugs.md` the same day. Waiting is always on application state (`expect.poll` against the API), never `waitForTimeout`. |
| `go build ./...` breaks in a checkout that never ran `pnpm` | Phase 3 § 3.1 commits `internal/webui/dist/index.html` as a placeholder and § 3.5 tests both the placeholder and the real build. |
| A 401 during polling produces an infinite redirect loop | Phase 6 § 6.4 centralises 401 handling in one middleware with a single-flight redirect and a test (`UI-API-006`) that asserts exactly one navigation for N concurrent 401s. |
| The plan touches plan 002's folder in phase 0 and a later audit reads it as scope creep | D12 records the intent here, phase 0 § 0.4 updates `002/verify/index.md` explicitly, and `STATE.md` §8 carries the deviation row. |
| TypeScript 7 lands typed-lint support mid-plan and the pin looks arbitrary | D13 states the exact peer range and the revisit condition. The pin is a single line in `web/package.json`. |

## Verification summary

| Gate | Command | Where it runs |
|------|---------|---------------|
| Go fmt | `make fmt-check` | every phase |
| Go lint | `make lint` | every phase |
| Go unit | `make test` | every phase |
| Go coverage floors | `make coverage-gate` | every phase |
| Go integration | `make test-integration` | phases 0-3, and any phase touching Go |
| Web typecheck | `make web-typecheck` | phases 4-17 |
| Web lint | `make web-lint` | phases 4-17 |
| Web unit and component | `make web-test` | phases 4-17 |
| Web coverage floors | `make web-coverage-gate` | phases 5-17 |
| Web production build | `make web-build` | phases 4-17 |
| Web dependency install | `make web-install` | phases 4-17 (once, and on any pin change) |
| Web bundle budget | `make web-budget` | phase 17 |
| L3 smoke (regression guard) | `make test-e2e` | phases 0-3, 17 |
| L3 smoke, binary agent | `AGENT_MODE=binary make test-e2e` | phases 2, 3, 17 |
| L3 full with durable evidence | `make test-e2e-full-evidence` | phase 0 (introduced), phase 17 |
| Container images | `make build-images` | phases 3, 17 |
| UI acceptance | `make test-ui-e2e` | phase 17 (introduced), then as the plan's final gate |

**Acceptance:** the reference scenario is proven by **SYS-UI-001** — after
`pg_ctl promote`, the Fleet Overview and Cluster Detail render the same
`cluster_id` string as before the promote, the new primary's role badge, and a
`failover_detected` timeline entry, all within two poll intervals — together
with **SYS-UI-002** (login required: an unauthenticated browser reaching `/`
gets the login form and no data), **SYS-UI-003** (a stopped agent surfaces as
`agent_down` on the Fleet Overview, not as a healthy cluster), and
**SYS-UI-011** (zero axe-core serious or critical violations across every route).

**Run caveats:** `make build-images` must precede any L3 or UI acceptance run.
`make test-ui-e2e` additionally needs `make web-build` and a one-time
`pnpm exec playwright install --with-deps chromium`. Node `^20.19.0 || >=22.12.0`
is required by Vite 8 (R2). The UI acceptance suite reuses the L3 port
requirements (8080, 5432+, 8474) and runs serially with other L3 suites.
These commands are identical to `STATE.md` §3; they must not drift.

## Model-assignment summary

| Phase | Sub-phases by assignment | Primary | `agent-1` review gates |
|-------|--------------------------|---------|------------------------|
| 0 | 0.1, 0.2, 0.3 → `agent-2:sonnet`; 0.4, 0.5, 0.6 → `agent-3:haiku` | `agent-2:sonnet` | 0.4 (audit register correctness) |
| 1 | 1.1, 1.2, 1.3, 1.4 → `agent-2:sonnet`; 1.5, 1.6 → `agent-3:haiku` | `agent-2:sonnet` | 1.1 (contract shape), 1.4 (coverage test design) |
| 2 | 2.1-2.6 → `agent-2:sonnet`; 2.7 → `agent-3:haiku` | `agent-2:sonnet` | 2.1, 2.2, 2.3 (authentication design and hot path) |
| 3 | 3.1-3.5 → `agent-2:sonnet`; 3.6 → `agent-3:haiku` | `agent-2:sonnet` | 3.2 (SPA fallback route ordering) |
| 4 | 4.1-4.7 → `agent-2:sonnet`; 4.8 → `agent-3:haiku` | `agent-2:sonnet` | 4.1 (dependency pins) |
| 5 | 5.1-5.10 → `agent-2:sonnet`; 5.11 → `agent-3:haiku` | `agent-2:sonnet` | 5.2, 5.4, 5.6, 5.9 (test architecture, fixture validation, coverage gate, flake policy) |
| 6 | 6.1-6.7 → `agent-2:sonnet`; 6.8 → `agent-3:haiku` | `agent-2:sonnet` | 6.4 (401 single-flight), 6.6 (state primitives) |
| 7 | 7.1-7.6 → `agent-2:sonnet`; 7.7 → `agent-3:haiku` | `agent-2:sonnet` | 7.3 (URL state contract) |
| 8 | 8.1-8.5 → `agent-2:sonnet`; 8.6 → `agent-3:haiku` | `agent-2:sonnet` | 8.4 (health derivation) |
| 9 | 9.1-9.6 → `agent-2:sonnet`; 9.7 → `agent-3:haiku` | `agent-2:sonnet` | 9.2 (topology graph model) |
| 10 | 10.1-10.6 → `agent-2:sonnet`; 10.7 → `agent-3:haiku` | `agent-2:sonnet` | — |
| 11 | 11.1-11.5 → `agent-2:sonnet`; 11.6 → `agent-3:haiku` | `agent-2:sonnet` | 11.2 (ASH stacking and `other` bucket) |
| 12 | 12.1-12.6 → `agent-2:sonnet`; 12.7 → `agent-3:haiku` | `agent-2:sonnet` | 12.3 (command lifecycle polling) |
| 13 | 13.1-13.5 → `agent-2:sonnet`; 13.6 → `agent-3:haiku` | `agent-2:sonnet` | 13.4 (cancel/terminate confirmation and tier gate) |
| 14 | 14.1-14.5 → `agent-2:sonnet`; 14.6 → `agent-3:haiku` | `agent-2:sonnet` | — |
| 15 | 15.1-15.6 → `agent-2:sonnet`; 15.7 → `agent-3:haiku` | `agent-2:sonnet` | 15.3 (alert-rule editing) |
| 16 | 16.1-16.4 → `agent-2:sonnet`; 16.5 → `agent-3:haiku` | `agent-2:sonnet` | — |
| 17 | 17.1-17.6 → `agent-2:sonnet`; 17.7, 17.8 → `agent-3:haiku` | `agent-2:sonnet` | 17.2 (acceptance assertions), 17.7 and 17.8 (final documentation read) |
