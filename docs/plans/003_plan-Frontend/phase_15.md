# Phase 14 — Advisor findings

> **Intent:** the durable, ranked statements about an instance's state, with the
> four-state lifecycle rendered honestly and muting available without pretending
> a finding was resolved.
> **Shippable alone?** yes.
> **Preconditions:** phase 13 DONE.

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

## Contract facts this page must respect

- A finding's state is one of `open`, `degraded`, `muted`, `resolved`. Each means
  something different and none may be collapsed into another:
  `degraded` means a required metric, check, permission tier or host view is
  unavailable — the rule could not be evaluated, which is **not** the same as the
  rule not firing.
- Muting hides a finding temporarily; the next pass restores its real state when
  the mute expires or is removed. Muting is never resolution.
- `/api/v1/advisor/rules` is the authoritative catalogue: rule id, severity,
  scope, required inputs (`needs`) and minimum permission tier (`min_tier`). The
  UI must read it rather than hard-coding the 39 rules — a hard-coded list goes
  stale the first time the backend adds a rule.
- Findings are based on collected statistics, not query plans; index
  recommendations are candidates, not certainties.
- Rules needing seven days of history stay `degraded` until that history exists.
- Host-memory rules are unavailable for remote instances.

---

## Sub-phases

### 14.1 Findings derivation library

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/lib/findings.ts` (new)
- **Change:** pure functions:
  - `rankFindings(findings)` → severity descending (`critical`, `warning`,
    `info`), then state (`open` before `degraded` before `muted` before
    `resolved`), then scope, then rule id. Deterministic and stable.
  - `groupByScope(findings)` → cluster-scoped and instance-scoped groups, since
    a config-drift finding about a cluster and a bloat finding about one table
    are different kinds of statement.
  - `summariseFindings(findings)` → counts per severity **for open findings
    only**, plus a separate count of degraded rules. Counting a degraded rule as
    "no problem" is the failure mode this function exists to prevent.
  - `joinCatalogue(findings, rules)` → attaches each finding's catalogue entry
    so the UI can explain `needs` and `min_tier`. A finding whose rule is absent
    from the catalogue keeps its own fields and is flagged, never dropped.
  - `muteExpiry(finding, now)` → remaining mute time, or `null`.
- **Unit tests (pure):**
  `UI-FIND-001 ranks by severity then state then scope then rule id`;
  `UI-FIND-002 ranking is stable for identical inputs`;
  `UI-FIND-003 summarise counts open only and reports degraded separately`;
  `UI-FIND-004 joinCatalogue attaches needs and min_tier`;
  `UI-FIND-005 a finding whose rule is missing from the catalogue is flagged,
  not dropped`;
  `UI-FIND-006 muteExpiry returns null for an unmuted finding`;
  `UI-FIND-007 muteExpiry uses the frozen clock`.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 14.2 The findings list

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/findings/FindingsPage.tsx`,
  `FindingCard.tsx` (new)
- **Change:**
  1. A summary bar: open findings by severity, and — separately and visibly —
     the number of rules that could not be evaluated. Both are filters.
  2. Filters for severity, state, scope, cluster and instance, all reflected in
     the URL. The default view shows `open` and `degraded`, not `resolved` or
     `muted`, with the hidden counts stated so nothing is silently omitted.
  3. Each finding card shows the title, rule id, severity, state, scope, the
     affected object, when it was first and last seen, and the evidence values
     the rule fired on. A finding without its numbers is an opinion; with them it
     is a diagnosis.
  4. A `degraded` finding states **what is missing** — the named metric, check,
     tier or host view from the catalogue's `needs` and `min_tier` — using the
     `Degraded` primitive, with the remedy where one exists (a grant, an
     extension, or simply waiting for seven days of history).
  5. A permanent note that findings come from collected statistics rather than
     query plans, so index recommendations are candidates.
- **Unit tests:**
  `UI-FIND-010 the summary counts open by severity and degraded separately`;
  `UI-FIND-011 the default filter hides resolved and muted and states the
  hidden counts`;
  `UI-FIND-012 filters round-trip through the URL`;
  `UI-FIND-013 a degraded finding names the missing input from the catalogue`;
  `UI-FIND-014 a degraded finding needing seven days of history says so`;
  `UI-FIND-015 a tier-gated rule names the required tier and the grant`;
  `UI-FIND-016 evidence values are rendered with the finding`;
  `expectNoA11yViolations` on the populated list.
- **e2e tests:** `SYS-UI-010` in phase 17.
- **Done:** gates green; closed in `STATE.md`.

### 14.3 Muting

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/findings/MuteDialog.tsx` (new)
- **Change:**
  1. A mute action opening a dialog requiring a **reason** (free text,
     mandatory — an unexplained mute is indistinguishable from a bug) and an
     expiry chosen from presets (1h, 8h, 24h, 7d) or an explicit timestamp.
  2. `POST /api/v1/findings/{finding-id}/mute` with `{reason, until}`; the
     response's changed state and expiry are rendered immediately.
  3. A muted finding stays visible when the `muted` filter is on, showing its
     reason, who set it (the API records no user identity, so the UI must not
     invent one — display the reason and the expiry only), and the remaining
     time. Unmute is a `DELETE` returning 204.
  4. The dialog states plainly that muting does not resolve the finding and that
     the next evaluation pass restores its real state when the mute expires. The
     word "resolve" must not appear on the mute control.
  5. Optimistic update is **not** used here: show the request in flight and
     render the server's answer. A mute that appears to work and did not is
     exactly the kind of silent lie this product is built against.
- **Unit tests:**
  `UI-FIND-020 the mute dialog requires a reason`;
  `UI-FIND-021 the mute request carries reason and until`;
  `UI-FIND-022 the dialog states that muting is not resolution`;
  `UI-FIND-023 a failed mute leaves the finding unmuted and shows the error`;
  `UI-FIND-024 unmute issues a DELETE and restores the previous state`;
  `UI-FIND-025 a muted finding shows its reason and remaining time`;
  `expectNoA11yViolations` on the dialog.
- **e2e tests:** `SYS-UI-010` mutes and unmutes a real finding.
- **Done:** gates green; closed in `STATE.md`.

### 14.4 The rule catalogue view

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/findings/RuleCatalogue.tsx` (new)
- **Change:** a table from `getAdvisorRules`: rule id, severity, scope, `needs`,
  `min_tier`, and — joined from the current findings — how many instances each
  rule is currently firing on and how many cannot evaluate it. Filterable by
  severity, scope and tier. This is the page that answers "what could pglens
  tell me if I granted T1", which is the question that turns a permission tier
  from an abstraction into a decision.
- **Unit tests:**
  `UI-FIND-030 renders every rule returned by the catalogue`;
  `UI-FIND-031 shows firing and non-evaluable counts per rule`;
  `UI-FIND-032 filtering by min_tier shows what a grant would unlock`;
  `UI-FIND-033 an empty catalogue renders the empty state, not a blank table`.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 14.5 Degraded and error paths (rule T-4)

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/findings/FindingsPage.tsx`
- **Change:** the remaining T-4 rows: no findings at all (which is a good
  outcome and must read as such, while still stating how many rules were
  evaluated and how many were degraded — "no findings" from zero evaluated rules
  is meaningless), stale data, 401, 500, and a catalogue that fails while
  findings load.
- **Unit tests (route kind):**
  `UI-FIND-040 no findings renders a positive state naming the evaluated rule
  count`;
  `UI-FIND-041 no findings with all rules degraded does not read as healthy`;
  `UI-FIND-042 a failing catalogue keeps the findings list and marks the
  explanations unavailable`;
  `UI-FIND-043 stale data renders Stale`;
  `UI-FIND-044 a 401 navigates to login exactly once`;
  `UI-FIND-045 a 500 renders ErrorState with a working retry`;
  `UI-FIND-046 the page polls at the findings interval`.
- **e2e tests:** none.
- **Done:** every applicable T-4 row has a named test; gates green; closed in
  `STATE.md`.

### 14.6 Update README.md

Mandatory closing sub-phase. User guide only — no implementation detail.

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — documentation
- **Files:** `README.md`
- **Change:** extend **Advisor findings** with the UI: the findings page, the
  four states and what `degraded` means, that muting is not resolution and
  requires a reason and an expiry, and the rule catalogue view showing what a
  higher permission tier would unlock. Keep the existing `curl` examples and the
  rule catalogue table. One paragraph.
- **Unit tests:** none (documentation).
- **e2e tests:** none — the page was exercised against the L3 stack.
- **Done:** a user understands the four states and the mute semantics from the
  README alone; gates green; closed in `STATE.md` with the §11 docs row for
  phase 14 set.

---

## Phase gates

- **Fmt / Lint / Typecheck:** `make fmt-check`, `make web-lint`,
  `make web-typecheck`
- **Test subset:** `make web-test`
- **Coverage:** `make web-coverage-gate` — `src/lib/findings.ts` at or above 95
- **Regression guard:** `make test` and `make test-e2e` still green
- **README:** the advisor UI paragraph

## Phase done criterion

The findings page ranks deterministically, counts degraded rules separately from
open findings, explains every degraded finding with its missing input from the
live catalogue, requires a reason to mute and never calls it resolution, renders
"no findings" only alongside the number of rules actually evaluated, and
`STATE.md` §11 shows phase 14 `DONE` with every sub-phase closed.
