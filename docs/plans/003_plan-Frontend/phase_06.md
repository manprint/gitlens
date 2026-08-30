# Phase 5 — Frontend test harness and quality gates

> **Intent:** build the test system every later phase writes into — deterministic
> component tests, fixtures that provably match the published API contract, a
> coverage gate with real floors, an accessibility assertion, and a Playwright
> bootstrap — before a single feature exists.
> **Shippable alone?** yes — it ships test infrastructure and its own meta-tests;
> no user-visible behaviour changes.
> **Preconditions:** phase 4 DONE. `api/openapi.yaml` from phase 1 must exist,
> because the fixture validator reads it.

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

## Why this phase is large

A monitoring UI fails in ways ordinary UIs do not. It can render a stale number
as if it were current, a missing measurement as a zero, a truncated list as a
complete one, or a wrong cluster's data under the right cluster's name — and look
perfectly healthy while doing it. None of those are caught by "the component
renders". This phase therefore builds four specific defences, and every later
phase is required to use them:

1. **Fixtures cannot lie.** Every fixture is validated against the schema in
   `api/openapi.yaml` (§ 5.4). A component test can only be written against a
   response shape the server actually promises.
2. **No request is invisible.** MSW rejects unhandled requests (§ 5.5), so a
   component that quietly calls an endpoint nobody mocked fails the test instead
   of rendering an empty state.
3. **Absence is asserted, not assumed.** Every page phase must include at least
   one degraded-path test using the state primitives (§ 5.2 rule T-4).
4. **Charts are tested as data, not pixels.** Option builders are pure functions
   with exact assertions (§ 5.2 rule T-5, decision D14).

---

## Sub-phases

### 5.1 Test dependencies and the Vitest configuration

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/package.json`, `web/vitest.config.ts` (new),
  `web/tsconfig.test.json` (new), `web/tsconfig.json`
- **Change:**
  1. Add the test dependencies, pinned exactly:
     ```json
     "@axe-core/playwright": "4.13.0",
     "@playwright/test": "1.62.1",
     "@testing-library/dom": "10.4.1",
     "@testing-library/jest-dom": "7.0.1",
     "@testing-library/react": "16.3.3",
     "@testing-library/user-event": "14.6.6",
     "@vitest/coverage-v8": "4.1.11",
     "@vitest/eslint-plugin": "1.6.27",
     "ajv": "8.20.0",
     "ajv-formats": "3.0.1",
     "axe-core": "4.13.0",
     "eslint-plugin-jest-dom": "5.10.1",
     "eslint-plugin-testing-library": "7.16.2",
     "jsdom": "30.0.1",
     "msw": "2.15.0",
     "vitest": "4.1.11",
     "yaml": "2.9.0"
     ```
     `@vitest/coverage-v8` pins an exact `vitest` (R4) — the two versions must
     always be bumped together. `@testing-library/dom` is a direct dev
     dependency because `@testing-library/react@16` declares it as a peer (R5).
     `vitest-axe` is deliberately **not** used: its latest release is 0.1.0 and
     stale (R16); § 5.7 writes a twenty-line helper over `axe-core` instead.
  2. Scripts:
     ```json
     "test": "vitest run",
     "test:watch": "vitest",
     "test:coverage": "vitest run --coverage",
     "test:e2e": "playwright test",
     "gen:api": "tsx scripts/gen-api-types.ts"
     ```
     (`gen:api` lands in phase 6; add the script now only if `tsx` is already
     present, otherwise add it there.)
  3. `web/vitest.config.ts`, importing the Vite config so aliases are shared:
     ```ts
     export default defineConfig({
       plugins: [react()],
       resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
       test: {
         environment: 'jsdom',
         globals: false,
         setupFiles: ['./src/test/setup.ts'],
         css: false,
         restoreMocks: true,
         unstubEnvs: true,
         unstubGlobals: true,
         clearMocks: true,
         mockReset: true,
         testTimeout: 5000,
         hookTimeout: 10000,
         include: ['src/**/*.test.ts', 'src/**/*.test.tsx'],
         exclude: ['e2e/**'],
         reporters: process.env.CI ? ['default', 'junit'] : ['default'],
         outputFile: { junit: './coverage/junit.xml' },
         coverage: { /* filled in § 5.6 */ },
       },
     })
     ```
     - `globals: false` is deliberate: every test imports `describe`, `it`,
       `expect` and `vi` explicitly. Implicit globals make it impossible to see
       from a file whether it is a test and defeat the ESLint import rules.
     - `css: false` keeps Tailwind out of the unit runner; styling is not
       asserted in jsdom.
     - `restoreMocks`/`clearMocks`/`mockReset` together guarantee that a leaked
       mock from one file cannot make another file pass.
     - `testTimeout: 5000` is intentionally tight. A component test that needs
       more than five seconds is waiting on a real timer or a real network call,
       both of which are bugs under this harness.
  4. `web/tsconfig.test.json` extending `tsconfig.app.json`, adding
     `"types": ["vitest/globals", "@testing-library/jest-dom"]` — the `types`
     entry is for the matcher augmentation, not for globals — and including
     `src/**/*.test.ts(x)`, `src/test/**`, `e2e/**`. Reference it from
     `tsconfig.json` so `pnpm typecheck` type-checks the tests too. **Untyped
     tests are how a test suite rots**; the tests are part of the type gate.
  5. Extend `eslint.config.js` with `eslint-plugin-testing-library` (react
     configuration) and `eslint-plugin-jest-dom`, scoped to
     `src/**/*.test.tsx`, plus `@vitest/eslint-plugin` recommended for the test
     files. Enable `testing-library/no-node-access` and
     `testing-library/prefer-screen-queries` as errors — they are what keep the
     suite from drifting into implementation-detail assertions.
- **Unit tests:** `src/test/harness.selftest.test.ts` — a single trivial test
  asserting `expect(1).toBe(1)` plus `expect(document.body).toBeInTheDocument()`,
  which proves the runner, the jsdom environment and the jest-dom matchers are
  all wired. Delete it only when § 5.10's meta-tests replace it.
- **e2e tests:** none.
- **Done:** `cd web && pnpm test` runs and passes; `pnpm typecheck` covers the
  test files; closed in `STATE.md`.

### 5.2 The test architecture rules

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation.
  **`agent-1:opus` review gate** — these rules are what every later phase is
  measured against; a vague rule here produces a decorative test suite.
- **Files:** `web/docs/testing.md` (new), `TESTING.md` (append a web section)
- **Change:** write `web/docs/testing.md` containing exactly the following
  normative rules. Later phase files reference them by number rather than
  restating them.

  **T-1 — Four test kinds, no others.**
  | Kind | Location | What it may touch | Runner |
  |------|----------|-------------------|--------|
  | **pure** | `src/lib/**/*.test.ts` | pure functions only; no React, no DOM | Vitest |
  | **component** | `src/components/**/*.test.tsx`, `src/features/**/*.test.tsx` | one component tree, MSW, fake timers | Vitest + jsdom |
  | **route** | `src/features/**/<page>.route.test.tsx` | a whole page mounted at its route with the real router and real query client | Vitest + jsdom |
  | **acceptance** | `web/e2e/**/*.spec.ts` | a real browser against a real stack | Playwright |
  A test that does not fit one of these four is a design problem in the code, not
  a missing fifth kind.

  **T-2 — Derivation lives in `src/lib/`.** Any transformation of an API
  response into something a component renders — bucketing, sorting, ratio, lag
  formatting, topology edge building, ASH stacking, health derivation — is a pure
  exported function in `src/lib/`, with a **pure** test. Components read
  already-derived values. A `useMemo` containing arithmetic is a rule violation:
  extract it.

  **T-3 — Query by role, then by label, then by text.** `getByTestId` is
  permitted in exactly three cases, each of which must carry a comment naming
  this rule: a chart container, a virtualised row container, and a canvas. Every
  other `data-testid` is rejected in review.

  **T-4 — Every page tests its degraded paths.** For each page, the phase must
  include component or route tests for all of the following that apply, each
  asserting the specific state primitive from `src/components/state/`:
  - the endpoint returns 200 with an empty collection → empty state, not a
    spinner and not a zero;
  - the endpoint returns 200 with `null` in a measurement → `Unknown`, never
    `0`, and never an interpolated line;
  - the endpoint returns `truncated: true` → `Truncated` with the budget
    explained;
  - the data is older than the freshness threshold → `Stale` with the age;
  - the endpoint returns 401 → the app navigates to the login route once;
  - the endpoint returns 500 → an error state naming the endpoint, with a retry
    control, and **no** rendering of partial data;
  - the feature requires a permission tier the instance lacks → `NotPermitted`
    with the required tier, and the control disabled rather than hidden;
  - the feature is switched off in the agent (`{"enabled": false}`) →
    `Disabled`, distinct from empty.
  A page phase whose tests omit an applicable row is not done.

  **T-5 — Charts are asserted as options.** For every chart, the option builder
  in `src/components/charts/*.options.ts` is a pure function with a **pure**
  test asserting the exact series, axis types, value order, `null` preservation
  and colour tokens. The React wrapper is component-tested with
  `vi.mock('echarts-for-react')` replaced by a stub that records the `option`
  prop, asserting only that the builder's output reaches the library. No test
  asserts pixels in jsdom.

  **T-6 — No sleeping.** `setTimeout`-based waiting, `waitForTimeout` and
  arbitrary `await new Promise(r => setTimeout(r, n))` are banned. Use
  `await screen.findBy…`, `await waitFor(…)` with an assertion inside, or
  `vi.advanceTimersByTimeAsync(n)` for a polling interval whose length is the
  thing under test.

  **T-7 — One assertion subject per test.** A test's name states what it
  asserts, in the form
  `it('renders Unknown when max_replay_lag_seconds is null')`. Names of the form
  `it('works')` or `it('renders correctly')` are rejected.

  **T-8 — Identifiers are compared as strings.** Any test touching `cluster_id`
  or `queryid` asserts the exact string, never a number, and at least one test
  per page that displays them uses a value beyond `Number.MAX_SAFE_INTEGER`
  (`"9007199254740993"`) to prove invariant I-5 end to end.

  **T-9 — Time is frozen.** Every component and route test runs under the fixed
  clock from § 5.8. A test that depends on the real clock is flaky by
  construction.

  **T-10 — Accessibility is asserted per page.** Every route test calls
  `expectNoA11yViolations` from § 5.7. A page with a serious or critical
  violation does not close its sub-phase.

- **Unit tests:** none — this sub-phase is the specification the others
  implement. Its enforcement is the review gate plus the ESLint rules from 5.1.
- **e2e tests:** none.
- **Done:** `web/docs/testing.md` contains T-1 to T-10 verbatim; `TESTING.md`
  links to it and states that the repository's L1-L5 levels map to these four
  kinds (pure and component are L1, route is L2 for the UI, acceptance is L3);
  `agent-1:opus` has reviewed the rules; closed in `STATE.md`.

### 5.3 Render helpers and the provider wrapper

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/test/render.tsx` (new), `web/src/test/setup.ts` (new)
- **Change:**
  1. `setup.ts` performs, in order: `import '@testing-library/jest-dom/vitest'`;
     start the MSW server (§ 5.5) in `beforeAll` with
     `onUnhandledRequest: 'error'`; `server.resetHandlers()` in `afterEach`;
     `server.close()` in `afterAll`; `cleanup()` from Testing Library in
     `afterEach`; install the frozen clock (§ 5.8); and fail the test run on an
     unexpected `console.error` or `console.warn` by replacing them with a spy
     that throws, with an allow-list of known-benign messages kept in one
     exported array. React's key warnings and act warnings are real defects and
     must fail.
  2. `render.tsx` exports `renderWithProviders(ui, options?)` returning
     Testing Library's result plus the `QueryClient` and a `router` handle. It
     wraps the tree in:
     - a fresh `QueryClient` per call, configured with
       `{queries: {retry: false, gcTime: Infinity, staleTime: 0,
       refetchOnWindowFocus: false}, mutations: {retry: false}}`. `retry: false`
       is essential: with retries on, a 500-path test waits for backoff and then
       times out with a misleading message.
     - a `MemoryRouter` seeded from `options.route` (default `/`), so route
       tests control the URL.
     - the application's own providers (theme, time range) once phase 7 defines
       them; until then leave a documented extension point rather than a
       placeholder component.
     It also exports `renderRoute(path)` which mounts the real route tree from
     `src/routes/` at `path` — that is what makes a **route** test different from
     a **component** test.
  3. Export a `user` factory that returns
     `userEvent.setup({advanceTimers: vi.advanceTimersByTime})`, which is
     required for `user-event` to work under fake timers. Every test must use
     this factory rather than calling `userEvent.setup()` directly; add an ESLint
     `no-restricted-imports` rule steering `@testing-library/user-event` imports
     to `@/test/render`.
- **Unit tests:** in `src/test/render.test.tsx`:
  `renders a component with providers`;
  `gives each render a fresh QueryClient` — two renders do not share cache;
  `seeds the router at the requested route`;
  `fails the test when the component logs a console.error`.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 5.4 Fixtures and contract validation

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation.
  **`agent-1:opus` review gate** — this is the mechanism that stops the UI being
  tested against a fiction. Review the schema extraction carefully.
- **Files:** `web/src/test/fixtures/**` (new), `web/src/test/contract.ts` (new),
  `web/src/test/contract.test.ts` (new)
- **Change:**
  1. `src/test/fixtures/` holds one module per endpoint, named after the
     `operationId` from phase 1 (`getClusters.ts`, `getAsh.ts`, …). Each exports:
     - a `base` object — a valid, representative response;
     - named variants for the degraded paths T-4 requires (`empty`,
       `withNullLag`, `truncated`, `stale`, `disabled`, …);
     - a `make…` factory taking a deep partial override, so a test can express
       exactly the one field it cares about and inherit the rest.
     Fixtures are plain data. No randomness, no `Date.now()`, no faker: a fixture
     whose value changes between runs cannot be asserted exactly.
  2. `src/test/contract.ts` exports
     `assertMatchesContract(operationId: string, statusCode: number, body: unknown): void`:
     - loads `../../../api/openapi.yaml` once per process with `yaml.parse`;
     - finds the operation by `operationId`, then its
       `responses[statusCode].content['application/json'].schema`;
     - resolves `$ref`s against the document by registering the whole document
       with Ajv under a base URI and referencing
       `#/components/schemas/...` directly, so no external resolver is needed;
     - validates with Ajv 8 configured `{strict: false, allErrors: true}` plus
       `ajv-formats` for `date-time` and `uuid` (R15). `strict: false` is
       required because OpenAPI 3.1 schemas carry keywords Ajv does not know;
     - throws an error listing every Ajv error with its `instancePath`, so the
       failure message points at the field rather than at the file.
  3. `src/test/contract.test.ts` — a single parameterised test that imports
     **every** fixture module via `import.meta.glob('./fixtures/*.ts', {eager: true})`
     and asserts every exported object validates against the operation named by
     the module's filename. This is the linchpin: a server response shape change
     that is reflected in `api/openapi.yaml` immediately reddens every fixture
     that no longer matches, in one test run, before any page is touched.
  4. Add a negative meta-test `rejects a fixture with a wrong field type` that
     validates a deliberately broken object and asserts `assertMatchesContract`
     throws with the offending path — otherwise a silently passing validator is
     indistinguishable from a working one.
  5. Fixtures must include, from the start, one cluster whose `cluster_id` is
     `"9007199254740993"` (above `Number.MAX_SAFE_INTEGER`) so rule T-8 has a
     value to use.
- **Unit tests:** `contract.test.ts` as described, plus
  `resolves $ref schemas`, `reports the instancePath of the failing field`,
  `throws for an unknown operationId`.
- **e2e tests:** none.
- **Done:** every fixture validates; the negative test proves the validator
  rejects; `agent-1:opus` has reviewed; closed in `STATE.md`.

### 5.5 The MSW server and handler factory

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/test/msw/server.ts` (new),
  `web/src/test/msw/handlers.ts` (new)
- **Change:**
  1. `server.ts` exports `setupServer()` with **no default handlers**. An empty
     default set plus `onUnhandledRequest: 'error'` means every test declares the
     exact endpoints it expects — which is how an accidental extra request
     becomes visible (invariant I-3).
  2. `handlers.ts` exports a small typed factory rather than raw `http.get`
     calls scattered through tests:
     ```ts
     ok(operationId, body, init?)        // 200 with the fixture, contract-checked
     status(operationId, code, body?)    // any status, for 401/404/422/500 paths
     slow(operationId, body, delayMs)    // resolves after N fake-timer ms
     sequence(operationId, ...responses) // first call, second call, … for polling
     never(operationId)                  // a request that never resolves, for loading states
     ```
     Each factory resolves the operation's URL pattern from
     `api/openapi.yaml` by `operationId`, converting `{id}` to MSW's `:id`, so a
     path typo in a test is impossible and a renamed route breaks every affected
     test loudly.
  3. `ok` calls `assertMatchesContract` on the body before returning it. A test
     that mocks an invalid response fails at the mock, not three assertions
     later.
  4. `sequence` is what makes polling testable: a route test advances the fake
     clock by the poll interval and asserts the second response replaced the
     first, including the case where the second is an error and the first must
     therefore be shown as stale rather than erased.
- **Unit tests:** in `src/test/msw/handlers.test.ts`:
  `resolves the URL pattern from the operationId`;
  `rejects a body that violates the contract`;
  `sequence returns each response in order`;
  `an unhandled request fails the test` — implemented by asserting that a fetch
  to an undeclared path rejects.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 5.6 Coverage configuration and the UI coverage gate

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation.
  **`agent-1:opus` review gate** — floors that are too low make the gate
  theatre; floors that are too high get lowered under pressure.
- **Files:** `web/vitest.config.ts` (the `coverage` block),
  `scripts/coverage_gate_ui.sh` (new), `Makefile`, `.github/workflows/ci.yml`
- **Change:**
  1. Coverage configuration:
     ```ts
     coverage: {
       provider: 'v8',
       reporter: ['text-summary', 'json-summary', 'lcov'],
       reportsDirectory: './coverage',
       all: true,
       include: ['src/**/*.{ts,tsx}'],
       exclude: [
         'src/**/*.test.{ts,tsx}',
         'src/test/**',
         'src/components/ui/**',      // shadcn primitives, generated
         'src/api/generated.ts',      // openapi-typescript output
         'src/main.tsx',
         'src/vite-env.d.ts',
       ],
     }
     ```
     `all: true` is not optional: without it an untested file simply does not
     appear in the report and the percentage stays green while coverage falls.
  2. `scripts/coverage_gate_ui.sh`, mirroring `scripts/coverage_gate.sh:1` in
     style (`#!/usr/bin/env bash`, `set -euo pipefail`), reading
     `web/coverage/coverage-summary.json`:
     - global floor: **85%** lines and **80%** branches over the included set;
     - per-directory floors, checked by aggregating the summary's per-file
       entries by directory prefix:
       | Directory | Lines | Why |
       |---|---|---|
       | `src/lib/` | 95 | all derivation; the numbers users act on |
       | `src/api/` | 95 | the 401 policy, the client, identifier handling |
       | `src/components/state/` | 100 | invariant I-2 lives here; a partially tested honesty primitive is worse than none |
       | `src/components/charts/` | 90 | option builders |
       | `src/features/` | 80 | page composition |
     - prints one line per directory with its measured value and its floor, then
       `UI_COVERAGE_GATE=pass|fail`, and exits non-zero on failure. Printing the
       measured value even when passing is what lets a reviewer see erosion.
  3. Makefile targets, added to `.PHONY`:
     ```make
     web-test:
     	cd $(WEB) && $(PNPM) run test

     web-coverage:
     	cd $(WEB) && $(PNPM) run test:coverage

     web-coverage-gate: web-coverage
     	./scripts/coverage_gate_ui.sh
     ```
     Add `web-test` and `web-coverage-gate` to `ci-local-unit` after
     `web-typecheck`.
  4. CI: extend the `web` job from phase 4 § 4.7 with `make web-test` and
     `make web-coverage-gate`, and upload `web/coverage/lcov.info` and
     `web/coverage/junit.xml` as artefacts with `actions/upload-artifact@v4`.
  5. The floors are **raised, never lowered**, by later phases. If a phase cannot
     meet a floor, that is a finding for `STATE.md` §9, not a reason to edit the
     script. Write that sentence as a comment at the top of the script.
- **Unit tests:** `TestUICoverageGateScript_FailsBelowFloor` — added to the Go
  test package created in phase 0 § 0.1: writes a synthetic
  `coverage-summary.json` below a floor into a temporary directory, runs the
  script against it, and asserts a non-zero exit and the `UI_COVERAGE_GATE=fail`
  line; a second case above the floors asserts exit 0. The script must therefore
  accept the summary path as `$1` with a default.
- **e2e tests:** none.
- **Done:** `make web-coverage-gate` passes on the current tree (the harness's
  own meta-tests supply the coverage); the Go test proves the gate fails when it
  should; `agent-1:opus` has approved the floors; closed in `STATE.md`.

### 5.7 The accessibility assertion

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/test/a11y.ts` (new), `web/src/test/a11y.test.tsx` (new)
- **Change:**
  1. `expectNoA11yViolations(container: HTMLElement, options?): Promise<void>` —
     runs `axe.run(container, {resultTypes: ['violations'], rules: {…}})` from
     `axe-core` 4.13 (R16), filters to `impact` in `['serious', 'critical']`, and
     throws with a formatted list: rule id, impact, help URL and the offending
     element's outer HTML truncated to 200 characters. A failure that does not
     name the element is a failure nobody fixes.
  2. Disable the two rules that are meaningless in a detached jsdom container and
     document why in a comment: `region` (there is no page landmark structure
     around a mounted fragment) and `color-contrast` (jsdom does not compute
     styles). Contrast is asserted instead in the Playwright run (§ 5.9,
     `SYS-UI-011`), where a real browser computes it.
  3. Export `A11Y_DISABLED_RULES` so the Playwright configuration re-enables
     exactly those two and nothing else.
- **Unit tests:** `a11y.test.tsx`:
  `passes for a labelled button`;
  `fails for an image without alt text` — asserts the thrown message contains
  `image-alt` and the element;
  `ignores minor and moderate impacts`.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 5.8 Determinism: clock, timezone, locale, randomness

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/test/time.ts` (new), `web/src/test/setup.ts`,
  `web/vitest.config.ts`, `web/package.json`
- **Change:**
  1. Fix the reference instant for the whole suite:
     `export const NOW = new Date('2026-08-27T02:00:00.000Z')`. Every fixture's
     timestamps are expressed relative to it, so "5 minutes ago" is a constant.
     The value matches the timestamps already published in `README.md`'s example
     payloads, which keeps fixtures and documentation consistent.
  2. In `setup.ts`: `beforeEach` calls
     `vi.useFakeTimers({shouldAdvanceTime: false, now: NOW})`;
     `afterEach` calls `vi.useRealTimers()`. `shouldAdvanceTime: false` is
     deliberate — time moves only when a test advances it, so a polling assertion
     is exact rather than approximate.
  3. Pin the timezone and locale so date formatting is reproducible on any
     machine: set `process.env.TZ = 'UTC'` in `vitest.config.ts` via
     `test.env`, and add `"test": "TZ=UTC vitest run"` as a belt-and-braces
     measure in `package.json`. Add an assertion in a meta-test that
     `Intl.DateTimeFormat().resolvedOptions().timeZone === 'UTC'`, so a
     misconfigured machine fails loudly instead of producing off-by-hours
     assertions.
  4. Formatting helpers in `src/lib/format/` must take an explicit locale
     argument defaulting to `'en-US'`; they must never read the ambient locale.
     Add an ESLint `no-restricted-syntax` rule banning
     `toLocaleString`/`toLocaleDateString`/`toLocaleTimeString` outside
     `src/lib/format/`.
  5. Randomness: nothing in `src/` may call `Math.random()` or `crypto.randomUUID()`
     for anything a test observes. Where a stable id is needed, use React's
     `useId`. Add a `no-restricted-properties` rule for `Math.random`.
- **Unit tests:** in `src/test/time.test.ts`:
  `freezes Date.now at NOW`;
  `advances only when the test advances timers`;
  `resolves the UTC timezone`;
  `formats a timestamp identically on repeated runs`.
- **e2e tests:** none.
- **Done:** gates green; running `pnpm test` twice produces identical output;
  closed in `STATE.md`.

### 5.9 Playwright bootstrap and the flake policy

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation.
  **`agent-1:opus` review gate** — the flake policy is the difference between an
  acceptance gate people trust and one they rerun until it passes.
- **Files:** `web/playwright.config.ts` (new), `web/e2e/fixtures.ts` (new),
  `web/e2e/smoke.spec.ts` (new), `web/docs/testing.md` (append the policy)
- **Change:**
  1. `playwright.config.ts`:
     ```ts
     export default defineConfig({
       testDir: './e2e',
       timeout: 30_000,
       expect: { timeout: 10_000 },
       fullyParallel: false,
       forbidOnly: !!process.env.CI,
       retries: process.env.CI ? 1 : 0,
       workers: 1,
       reporter: process.env.CI
         ? [['list'], ['html', { outputFolder: '../test/e2e/_artifacts/ui/report', open: 'never' }]]
         : [['list']],
       outputDir: '../test/e2e/_artifacts/ui/results',
       use: {
         baseURL: process.env.PGLENS_UI_BASE_URL ?? 'http://127.0.0.1:8080',
         trace: 'retain-on-failure',
         screenshot: 'only-on-failure',
         video: 'off',
         actionTimeout: 10_000,
         timezoneId: 'UTC',
         locale: 'en-US',
         colorScheme: 'dark',
       },
       projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
     })
     ```
     - **No `webServer` block.** The stack is started by the Go harness
       (decision D17, phase 17 § 17.1); Playwright must never start or stop it,
       or two orchestrators will fight over the same ports.
     - `workers: 1` and `fullyParallel: false`: the acceptance scenarios mutate
       one shared PostgreSQL cluster (a promote is not parallelisable).
     - Artefacts land under the repository's existing `test/e2e/_artifacts/`
       tree so cleanup and CI upload already work.
  2. `e2e/fixtures.ts` extends Playwright's `test` with:
     - a `signedInPage` fixture that performs the login through the UI once per
       worker using `process.env.PGLENS_UI_PASSWORD` and reuses the storage
       state;
     - an `api` fixture — a thin authenticated `request` context using the agent
       bearer token — so a spec can assert against the API and the UI in the same
       test, which is how "the UI shows what the API returned" becomes provable
       rather than assumed;
     - a `waitForFreshData(page, predicate)` helper built on `expect.poll` with
       an explicit timeout, so **no spec ever calls `waitForTimeout`**.
  3. `e2e/smoke.spec.ts` — one scenario, `SYS-UI-000`: the server is reachable,
     `/` returns the application shell, and the page title is `pglens`. It exists
     so the harness wiring in phase 17 can be proven before any feature spec
     exists.
  4. Append the **flake policy** to `web/docs/testing.md`:
     - `retries: 1` in CI exists to distinguish flake from failure, not to make
       failures pass.
     - Any spec that fails on the first attempt and passes on the retry is
       **quarantined the same day**: annotated `test.fixme`, and logged in the
       plan folder's `bugs.md` with a `B-A<NNN>` id, the trace artefact path and
       the suspected cause. A quarantined spec that is still quarantined after
       two working days is escalated to `STATE.md` §9 as a blocker.
     - A spec may never be made to pass by increasing a timeout without a written
       reason in the same commit stating what the longer wait is waiting for.
     - `waitForTimeout` is banned; the ESLint config bans it in `e2e/**`
       (`no-restricted-syntax` on `CallExpression[callee.property.name='waitForTimeout']`).
  5. Add `e2e/**` to the ESLint project with the Playwright-appropriate rule set,
     and to `tsconfig.test.json`.
- **Unit tests:** none — Playwright specs are not unit tested. The proof is the
  `SYS-UI-000` run in phase 17 § 17.1.
- **e2e tests:** `SYS-UI-000` — written here, first executed in phase 17.
- **Done:** `pnpm exec playwright test --list` enumerates `SYS-UI-000` without
  error; `pnpm typecheck` covers `e2e/**`; the flake policy is written;
  `agent-1:opus` has approved it; closed in `STATE.md`.

### 5.10 Meta-tests: prove the harness catches what it claims to

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/test/harness.meta.test.tsx` (new)
- **Change:** a harness nobody has tried to defeat is a harness nobody should
  trust. Write one test per defence, each of which deliberately does the wrong
  thing and asserts that the harness rejects it:
  1. `an unhandled request fails the test` — render a component that fetches an
     endpoint with no handler registered; assert the resulting rejection mentions
     the URL.
  2. `a fixture that violates the contract is rejected` — call
     `ok('getClusters', {nonsense: true})` and assert it throws with the
     instancePath.
  3. `a console.error fails the test` — render a component that logs one; assert
     the suite's guard throws.
  4. `a leaked timer does not leak into the next test` — schedule an interval in
     one test and assert in the next that the fake clock is at `NOW`.
  5. `two renders do not share query cache` — fetch in one render, assert the
     second render issues its own request.
  6. `a serious a11y violation fails` — as in § 5.7, but through
     `renderWithProviders` so the integration is proven.
  7. `the coverage gate script rejects a low summary` — this one lives on the Go
     side (§ 5.6) and is cross-referenced here in a comment, not duplicated.
  Implement 1-6 with `expect(...).rejects` / `expect(() => …).toThrow()` so the
  meta-tests pass *because* the harness failed correctly.
- **Unit tests:** the six tests above are themselves the unit tests.
- **e2e tests:** none.
- **Done:** all six meta-tests pass; deliberately breaking any one defence (for
  example setting `onUnhandledRequest: 'warn'`) turns its meta-test red — verify
  this once for defence 1 and record it in `STATE.md` §7; closed in `STATE.md`.

### 5.11 Update TESTING.md and README.md

Mandatory closing sub-phase. User guide only — no implementation detail in the
root README.

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — documentation
- **Files:** `TESTING.md`, `README.md`, `web/README.md`
- **Change:**
  - `TESTING.md`: add a **Frontend** section describing the four test kinds, the
    commands (`make web-test`, `make web-coverage-gate`, `make test-ui-e2e` once
    phase 17 adds it), the coverage floors table, and a link to
    `web/docs/testing.md` for the normative rules. Map them onto the existing
    L1-L5 vocabulary so the document keeps one taxonomy.
  - `README.md` **Running the checks**: add `make web-test` and
    `make web-coverage-gate` to the command list, and add a bullet to **Test
    levels** stating that L4 now also covers the frontend's contract validation
    against `api/openapi.yaml`. Two sentences; the detail belongs in
    `TESTING.md`.
  - `web/README.md`: add "how to write a test here" — which of the four kinds to
    pick, where fixtures live, and the one-line rule that derivation goes in
    `src/lib/`.
- **Unit tests:** none (documentation).
- **e2e tests:** none — every documented command was executed.
- **Done:** a contributor can write a correct test from `TESTING.md` and
  `web/docs/testing.md` alone; the root README stays free of implementation
  detail; gates green; closed in `STATE.md` with the §11 docs row for phase 5
  set.

---

## Phase gates

- **Fmt:** `make fmt-check` and `cd web && pnpm format:check`
- **Lint:** `make lint` and `make web-lint`
- **Typecheck:** `make web-typecheck` — including the test files
- **Test subset:** `make test` (Go, now including the coverage-gate script test)
  and `make web-test`
- **Coverage:** `make coverage-gate` and `make web-coverage-gate`
- **Regression guard:** `make test-e2e` still green
- **README:** `TESTING.md`, root README command list, `web/README.md`

## Phase done criterion

`make web-test` runs the meta-tests and passes, `make web-coverage-gate` reports
every floor with its measured value, every fixture validates against
`api/openapi.yaml`, an unhandled request or a contract-violating fixture fails a
test (demonstrated, recorded in `STATE.md` §7),
`pnpm exec playwright test --list` enumerates `SYS-UI-000`, `web/docs/testing.md`
contains rules T-1 to T-10 and the flake policy, and `STATE.md` §11 shows phase 5
`DONE` with every sub-phase closed.
