# Phase 6 — Typed API client, query layer, state primitives

> **Intent:** generate the frontend's types from `api/openapi.yaml`, build the
> one client every request goes through, define the polling and error policy
> once, and implement the honesty primitives that invariant I-2 depends on.
> **Shippable alone?** yes — it ships a login screen and an authenticated shell
> that proves the whole chain works, with no data pages yet.
> **Preconditions:** phase 5 DONE.

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

Test rules referenced below (T-1 … T-10) are defined in `web/docs/testing.md`,
written in phase 5 § 5.2.

---

## Sub-phases

### 6.1 Type generation from the contract

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/scripts/gen-api-types.ts` (new), `web/src/api/generated.ts`
  (generated, committed), `web/package.json`, `Makefile`,
  `.github/workflows/ci.yml`
- **Change:**
  1. `scripts/gen-api-types.ts` runs `openapi-typescript` 7 (R12)
     programmatically over `../api/openapi.yaml` and writes
     `src/api/generated.ts` with a header comment stating the file is generated,
     naming the source and the regeneration command, and instructing readers not
     to edit it.
  2. `package.json` script `"gen:api": "tsx scripts/gen-api-types.ts"`; add
     `tsx` to the dev dependencies pinned to its current version, recorded in
     `STATE.md` §8 as an addition to phase 4's pin list.
  3. Makefile target `web-gen-api` (added to `.PHONY`) running
     `cd web && pnpm run gen:api`.
  4. **Commit the generated file.** A checkout must type-check without running a
     code generator, and reviewers must be able to see contract changes in the
     diff.
  5. Add a drift check to the `web` CI job and to `make web-lint`:
     `make web-gen-api && git diff --exit-code web/src/api/generated.ts`. A spec
     change that was not regenerated fails CI with a one-line message.
  6. Export the useful aliases from `src/api/types.ts` so features never import
     the raw generated tree:
     ```ts
     export type Schemas = components['schemas']
     export type Cluster = Schemas['Cluster']
     export type Operation<K extends keyof paths> = …
     ```
     One re-export module keeps the generated file's shape an implementation
     detail that can change when `openapi-typescript` does.
- **Unit tests:** `src/api/types.test.ts` — type-level assertions using
  `expectTypeOf` from Vitest: `Cluster['cluster_id']` is `string` (invariant
  I-5), `SeriesPoint['value']` includes `null`, and
  `Cluster['max_replay_lag_seconds']` includes `null`. These are compile-time
  assertions that fail the typecheck if the contract regresses.
- **e2e tests:** none.
- **Done:** `make web-gen-api` produces no diff on a clean tree; the drift check
  is part of `make web-lint`; closed in `STATE.md`.

### 6.2 The typed client

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/api/client.ts` (new)
- **Change:**
  1. Create the single `openapi-fetch` client (R12):
     ```ts
     import createClient from 'openapi-fetch'
     import type { paths } from './generated'

     export const client = createClient<paths>({
       baseUrl: '/',
       credentials: 'same-origin',
       headers: { Accept: 'application/json' },
     })
     ```
     `baseUrl: '/'` is what makes the same bundle work behind the Vite dev proxy
     and inside the Go binary with no configuration. `credentials: 'same-origin'`
     sends the session cookie.
  2. This module is the **only** place in `src/` allowed to construct a request.
     The ESLint `no-restricted-globals` rule on `fetch` from phase 4 § 4.6
     enforces it; add `client.ts` to that rule's allow-list by disabling the rule
     for this one file with an inline comment naming the reason.
  3. Export a normalised error type used everywhere:
     ```ts
     export type ApiFailure =
       | { kind: 'unauthorized' }
       | { kind: 'forbidden' }
       | { kind: 'not_found' }
       | { kind: 'unprocessable'; error: string; detail: string }
       | { kind: 'server'; status: number; error: string; detail: string }
       | { kind: 'network'; message: string }
       | { kind: 'malformed'; message: string }
     ```
     and `toApiFailure(response, body): ApiFailure`. The union is exhaustive and
     the `switch-exhaustiveness-check` lint rule from phase 4 forces every
     consumer to handle a newly added kind. Distinguishing `network` from
     `server` matters: "the server said no" and "the browser could not reach the
     server" are different messages to an operator at 3am.
  4. Add `parseLargeIntStrings(raw: string): unknown` — a JSON parse path used
     only for responses containing `queryid`. `queryid` is a JSON number in the
     API but exceeds safe-integer range; parsing it with `JSON.parse` loses
     precision silently. The function reads the raw response text and rewrites
     `"queryid": <digits>` into `"queryid": "<digits>"` with a bounded regular
     expression before parsing, so the value reaches the UI as a string
     (invariant I-5). Apply it only on the endpoints that carry `queryid`
     (`/api/v1/statements`, `/api/v1/ash`, `/api/v1/ash/top`, `/api/v1/plans`)
     and document the list in the function's comment.
- **Unit tests:** in `src/api/client.test.ts` (pure and component kinds):
  `UI-API-001 toApiFailure maps 401 to unauthorized`;
  `UI-API-002 toApiFailure maps 422 with the error envelope fields`;
  `UI-API-003 toApiFailure maps a thrown TypeError to network`;
  `UI-API-004 toApiFailure maps invalid JSON to malformed`;
  `UI-API-005 parseLargeIntStrings preserves 9007199254740993 as a string` —
  and asserts `JSON.parse` on the same text would have lost it, so the test
  documents why the function exists;
  `parseLargeIntStrings leaves other numbers untouched`;
  `parseLargeIntStrings does not corrupt a queryid inside a query text string`.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 6.3 Query layer: keys, policies, and the poll clock

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/api/queries.ts` (new), `web/src/api/keys.ts` (new),
  `web/src/api/policy.ts` (new)
- **Change:**
  1. `keys.ts` — a single query-key factory. Every key is an array whose first
     element is the `operationId` and whose remaining elements are the
     parameters in a fixed order:
     `export const qk = { clusters: () => ['getClusters'] as const, cluster: (id: string) => ['getCluster', id] as const, … }`.
     Hand-written key arrays scattered across features are how cache
     invalidation quietly stops working; one factory makes the whole cache
     inspectable.
  2. `policy.ts` — the refresh policy table, one entry per surface, exported as
     data so a test can assert it:
     ```ts
     export const REFRESH = {
       fleet:      { interval: 15_000, staleAfter:  45_000 },
       cluster:    { interval: 15_000, staleAfter:  45_000 },
       instance:   { interval: 15_000, staleAfter:  45_000 },
       locks:      { interval:  5_000, staleAfter:  20_000 },
       activity:   { interval:  5_000, staleAfter:  20_000 },
       ash:        { interval: 30_000, staleAfter:  90_000 },
       statements: { interval: 60_000, staleAfter: 180_000 },
       findings:   { interval: 60_000, staleAfter: 300_000 },
       alerts:     { interval: 15_000, staleAfter:  60_000 },
       command:    { interval:  1_000, staleAfter:      0 },
       static:     { interval:      0, staleAfter:      0 },
     } as const
     ```
     `interval` drives `refetchInterval`; `staleAfter` is the age past which the
     `Stale` primitive is shown. `staleAfter` is deliberately about three
     intervals: one missed poll is not staleness, three is. The agent's default
     push interval is 15 s, so polling faster than that on fleet data returns
     the same numbers and wastes the server's time — the table's values are
     derived from the product's real cadence, not from taste.
  3. `queries.ts` — one exported hook per operation, each returning TanStack
     Query's result plus a `dataAge` in milliseconds computed from
     `dataUpdatedAt` and the query client's clock. Every hook:
     - uses the key factory;
     - sets `refetchInterval` from the policy table;
     - sets `retry: (count, err) => err.kind === 'network' && count < 2` — never
       retry a 4xx, and never retry a 401 at all, which would fight the redirect
       in § 6.4;
     - sets `placeholderData: keepPreviousData` for time-range queries so
       changing a range does not blank the page, and **does not** use it for
       identity-scoped queries where showing the previous cluster's data under a
       new cluster's name would be a lie.
  4. Add `useAutoRefreshPaused()` reading `document.visibilityState`: polling
     stops while the tab is hidden and resumes with an immediate refetch. A
     dashboard left open overnight must not hold a poll loop against a
     production server for eight hours.
- **Unit tests:** in `src/api/queries.test.tsx` (component kind, MSW):
  `UI-API-010 a query polls at its policy interval` — advance the fake clock by
  the interval, assert a second request using the MSW `sequence` factory;
  `UI-API-011 polling stops while the document is hidden and refetches on
  return`;
  `UI-API-012 a 500 is not retried`;
  `UI-API-013 a network error is retried twice then surfaces`;
  `UI-API-014 keepPreviousData is not used for identity-scoped queries` — switch
  cluster id and assert the old data is not rendered;
  `UI-API-015 dataAge grows with the frozen clock`;
  and a pure test `REFRESH staleAfter is at least three intervals for every
  polling surface`, which turns the reasoning above into an executable rule.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 6.4 Authentication state and the single-flight 401

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation.
  **`agent-1:opus` review gate** — concurrent 401s during polling are the
  classic source of redirect loops and lost state.
- **Files:** `web/src/api/auth.ts` (new), `web/src/features/auth/LoginPage.tsx`
  (new), `web/src/features/auth/RequireSession.tsx` (new)
- **Change:**
  1. `auth.ts` exports `useSession()` querying `GET /api/v1/session` with the
     `static` policy, and mutations `signIn(password)` and `signOut()`.
  2. A module-level `onUnauthorized` handler installed once, called from a
     response middleware on the client. It must be **single-flight**: the first
     401 clears the query cache, sets the session state to unauthenticated and
     navigates to `/login?next=<current path>`; subsequent 401s arriving in the
     same tick are swallowed. Implement it with a boolean latch reset when the
     next successful response arrives, not with a debounce timer — a timer makes
     the behaviour clock-dependent and therefore flaky to test.
  3. `RequireSession` wraps the routed application: while the session query is
     loading it renders a neutral full-page skeleton, never a flash of the login
     form; when unauthenticated it renders `<Navigate to="/login" replace>`;
     when the server reports `configured: false` it renders a distinct page
     explaining that the server has no UI password set and naming
     `PGLENS_UI_PASSWORD`. That third state is not an error state: the operator
     did nothing wrong, the deployment is incomplete, and the message must say
     so.
  4. `LoginPage` — a single password field with a visible label, a submit
     button, `autocomplete="current-password"`, an error region with
     `role="alert"` announcing a failed attempt, and a disabled submit while the
     request is in flight. On success it navigates to the `next` parameter,
     defaulting to `/`, after validating that `next` is a same-origin relative
     path beginning with a single `/` — an open redirect through a query
     parameter is a real vulnerability even in a single-tenant tool.
  5. Sign-out clears the entire query cache before navigating, so the next user
     of the browser cannot read the previous session's data from memory.
- **Unit tests:** in `src/api/auth.test.tsx` and
  `src/features/auth/LoginPage.test.tsx`:
  `UI-API-006 N concurrent 401s produce exactly one navigation` — fire five
  parallel failing queries, assert one navigation entry;
  `UI-API-007 a 401 clears the query cache`;
  `UI-AUTH-001 the login form submits the password and navigates to next`;
  `UI-AUTH-002 a wrong password shows an alert and keeps the field focused`;
  `UI-AUTH-003 an absolute next parameter is rejected` — table over
  `https://evil.example`, `//evil.example`, `/valid/path`;
  `UI-AUTH-004 the not-configured state names PGLENS_UI_PASSWORD`;
  `UI-AUTH-005 sign-out clears the cache and navigates to login`;
  `UI-AUTH-006 the loading state never flashes the login form`;
  plus `expectNoA11yViolations` on the login page (rule T-10).
- **e2e tests:** `SYS-UI-002` is written in phase 17; the assertions here are its
  unit-level counterpart.
- **Done:** gates green; the single-flight behaviour is proven by
  `UI-API-006`; `agent-1:opus` has reviewed; closed in `STATE.md`.

### 6.5 Formatting library

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/lib/format/*.ts` (new)
- **Change:** pure functions, every one with an explicit `locale = 'en-US'`
  parameter (phase 5 § 5.8 rule):
  - `formatDuration(seconds: number | null): string` — `null` returns the
    sentinel the `Unknown` primitive renders, never `"0s"`.
  - `formatBytes(n: number | null): string` — binary units, one decimal above
    `1024`, matching the agent's own `512MiB` vocabulary so the UI and the config
    file speak the same language.
  - `formatLag(seconds: number | null)` — sub-second values keep two decimals;
    the product's whole value proposition is noticing a 0.12 s lag.
  - `formatTimestamp(iso: string, tz: string)` and
    `formatRelative(iso: string, now: Date)` — built on `date-fns`.
  - `formatCount(n: number)` — grouped thousands, tabular figures.
  - `truncateQuery(text: string, max = 2048)` — matches the server's documented
    2048-byte lock-tree truncation and appends a marker the `Truncated`
    primitive can detect.
  - `formatPercent(numerator, denominator)` — returns `null` when the
    denominator is `0`, so a ratio with no basis is `Unknown` rather than `NaN`
    or `0%`.
- **Unit tests:** one pure test file per function, table-driven, each including
  the `null`, zero, negative and boundary cases. Explicit named cases required:
  `formatDuration(null)` is not `"0s"`; `formatPercent(x, 0)` is `null`;
  `formatBytes(1024)` is `"1.0 KiB"`; `formatLag(0.12)` is `"0.12 s"`;
  `truncateQuery` marks truncation at exactly 2048.
- **e2e tests:** none.
- **Done:** `src/lib/format/` is at or above the 95% floor on its own; closed in
  `STATE.md`.

### 6.6 The state primitives

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation.
  **`agent-1:opus` review gate** — invariant I-2 is implemented here; every page
  depends on these components being unambiguous.
- **Files:** `web/src/components/state/*.tsx` (new),
  `web/src/components/state/index.ts`
- **Change:** implement each primitive as a small, accessible component with a
  fixed visual language. They are the only sanctioned way to render absence.
  | Component | Renders | Required props | Accessibility |
  |---|---|---|---|
  | `Unknown` | an em dash with a tooltip explaining that the value was not measured | `reason?: string` | `aria-label="not measured"` on the dash |
  | `Stale` | the value plus its age and a warning affordance | `age: number`, `threshold: number`, `children` | `role="status"`, age announced |
  | `Degraded` | why a rule or view cannot be evaluated | `reason: string`, `requires?: string` | `role="status"` |
  | `Truncated` | "showing N of M" with the budget named | `shown: number`, `budget: number`, `scope: string` | `role="status"` |
  | `NotPermitted` | the required permission tier and how to grant it | `required: 'T1' \| 'T2'`, `current: PermTier` | `role="status"`, and the associated control gets `aria-disabled` with the reason in its accessible name |
  | `Disabled` | a check switched off in the agent configuration | `feature: string`, `configKey: string` | `role="status"` |
  | `EmptyState` | a genuinely empty result, distinct from all of the above | `title`, `description`, `action?` | heading + description |
  | `ErrorState` | an `ApiFailure`, with the endpoint named and a retry control | `failure: ApiFailure`, `onRetry` | `role="alert"` |
  Hard rules, stated in the module's doc comment and enforced by review:
  - `NotPermitted` **disables** a control, never hides it. A hidden button
    teaches an operator that the feature does not exist; a disabled one with a
    reason teaches them which grant to run.
  - `Disabled` and `EmptyState` must never be visually interchangeable: "the
    check is off" and "the check is on and found nothing" are different facts.
  - No primitive renders the number `0`.
- **Unit tests:** in `src/components/state/*.test.tsx` — this directory is held
  to a **100%** coverage floor (phase 5 § 5.6):
  `UI-STATE-001 Unknown renders an em dash and never a zero`;
  `UI-STATE-002 Stale announces the age and the threshold`;
  `UI-STATE-003 Truncated states shown, budget and scope`;
  `UI-STATE-004 NotPermitted names the required tier and the current tier`;
  `UI-STATE-005 NotPermitted disables rather than hides its control`;
  `UI-STATE-006 Disabled names the configuration key`;
  `UI-STATE-007 Disabled and EmptyState render distinguishable text`;
  `UI-STATE-008 ErrorState names the endpoint and calls onRetry`;
  `UI-STATE-009 Degraded states what is missing`;
  and `expectNoA11yViolations` for every one of them.
- **e2e tests:** none directly; every page's acceptance spec depends on them.
- **Done:** 100% coverage on the directory; `agent-1:opus` has reviewed the
  rules; closed in `STATE.md`.

### 6.7 Freshness plumbing

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/hooks/useFreshness.ts` (new),
  `web/src/components/layout/FreshnessBadge.tsx` (new)
- **Change:** `useFreshness(dataUpdatedAt: number, policy: keyof typeof REFRESH)`
  returns `{age, isStale, label}`. `FreshnessBadge` renders the label and, past
  `staleAfter`, switches to the `Stale` primitive. Every page mounts exactly one
  badge in its header; nested badges are noise. Add the badge to the shell in
  phase 7 § 7.2.
- **Unit tests:** `UI-FRESH-001 reports the age from the frozen clock`;
  `UI-FRESH-002 flips to stale exactly at the threshold, not before`;
  `UI-FRESH-003 an errored refetch keeps the last good age and marks it stale
  rather than resetting it` — the case that matters: a failing poll must age the
  data, not restart its clock.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 6.8 Update README.md

Mandatory closing sub-phase. User guide only — no implementation detail.

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — documentation
- **Files:** `README.md`
- **Change:** this phase makes one thing usable: signing in to the interface.
  Under **Running the server** → **Signing in**, add that opening
  `http://<host>:8080/` in a browser presents a sign-in form, that the password
  is `PGLENS_UI_PASSWORD`, that a session lasts `PGLENS_UI_SESSION_TTL` and does
  not survive a server restart, and that signing out clears it. State that no
  data pages exist yet only if that is still true when the phase closes;
  otherwise say nothing about pages. No component or module names.
- **Unit tests:** none (documentation).
- **e2e tests:** none — the described sign-in was performed in a browser against
  a locally built binary.
- **Done:** a user can sign in from the README alone; gates green; closed in
  `STATE.md` with the §11 docs row for phase 6 set.

---

## Phase gates

- **Fmt:** `make fmt-check` and `cd web && pnpm format:check`
- **Lint:** `make lint` and `make web-lint` (including the generated-file drift
  check)
- **Typecheck:** `make web-typecheck`
- **Test subset:** `make web-test`
- **Coverage:** `make web-coverage-gate` — `src/api/` at or above 95,
  `src/lib/` at or above 95, `src/components/state/` at 100
- **Regression guard:** `make test` and `make test-e2e` still green
- **README:** the sign-in description

## Phase done criterion

`src/api/generated.ts` regenerates with no diff, every request in the
application goes through `client.ts`, five concurrent 401s produce exactly one
navigation, the eight state primitives exist at 100% coverage with accessibility
assertions, signing in through a browser against a locally built binary reaches
an authenticated shell, and `STATE.md` §11 shows phase 6 `DONE` with every
sub-phase closed.
