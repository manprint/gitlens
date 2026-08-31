# Frontend test architecture

These rules are normative for every frontend test.

**T-1 — Four test kinds, no others.**

| Kind           | Location                                                     | What it may touch                                                            | Runner         |
| -------------- | ------------------------------------------------------------ | ---------------------------------------------------------------------------- | -------------- |
| **pure**       | `src/lib/**/*.test.ts`                                       | pure functions only; no React, no DOM                                        | Vitest         |
| **component**  | `src/components/**/*.test.tsx`, `src/features/**/*.test.tsx` | one component tree, MSW, fake timers                                         | Vitest + jsdom |
| **route**      | `src/features/**/<page>.route.test.tsx`                      | a whole page mounted at its route with the real router and real query client | Vitest + jsdom |
| **acceptance** | `web/e2e/**/*.spec.ts`                                       | a real browser against a real stack                                          | Playwright     |

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
