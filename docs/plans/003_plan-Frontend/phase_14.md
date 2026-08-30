# Phase 13 — Locks and Activity

> **Intent:** the contention view: the sampled blocking tree, live-ish session
> activity, and the tier-gated cancel and terminate actions.
> **Shippable alone?** yes.
> **Preconditions:** phase 12 DONE.

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

- The lock view is **sampled every 10 seconds, not live**. A contention episode
  shorter than the interval can be missed entirely. The page must say so
  permanently and must show the sample's own timestamp, not the fetch time.
- An instance with no stored tree still returns `200` with
  `{"sampled_at": null, "stale": true, "nodes": []}`. That is not an error and
  not an empty database — it is "no sample yet".
- Query text in a lock tree is truncated to 2048 bytes.
- Deadlocks are a counter and a rate; the statements involved in a specific
  deadlock are not identifiable without PostgreSQL log analysis, which pglens
  does not do.
- `cancel` and `terminate` require tier **T2** and `allow_signal: true` on the
  target, and only target client backends.
- Per-application connection counts are **opt-in** (`activity.by_application`);
  per-user breakdown does not exist (decision D11).

---

## Sub-phases

### 13.1 Lock-tree derivation library

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/lib/locks.ts` (new)
- **Change:** pure functions:
  - `buildBlockingTree(nodes)` → a forest of blocking relationships from the
    flat node list, with each root being a session that blocks others and is
    itself unblocked. Cycles must be detected and rendered as a cycle rather
    than causing an infinite descent — a deadlock in the sample is exactly the
    case where naive recursion hangs the browser.
  - `treeDepth(tree)` and `blockedCount(tree)` for the summary line.
  - `rankRoots(forest)` → roots ordered by the number of sessions they block,
    then by wait duration. The session blocking eleven others is the one an
    operator needs first.
  - `waitDuration(node, sampledAt)` → derived from the sample's timestamp, not
    from the current clock: the tree is a snapshot and ageing it against `now`
    would overstate every wait.
- **Unit tests (pure):**
  `UI-LOCK-001 builds a forest from a flat blocking list`;
  `UI-LOCK-002 detects a cycle and renders it as a cycle without recursing
  forever` — includes a two-node and a three-node cycle;
  `UI-LOCK-003 ranks roots by blocked count then wait duration`;
  `UI-LOCK-004 waitDuration uses sampled_at, not the current clock`;
  `UI-LOCK-005 an empty node list yields an empty forest without throwing`.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 13.2 The blocking tree view

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/locks/BlockingTree.tsx` (new)
- **Change:**
  1. Render the ranked forest as an expandable tree. Each node shows pid, user,
     application, database, state, wait event type and event, the lock mode being
     waited on, the wait duration, and the truncated query text.
  2. The header states the sample's age using `sampled_at` and a permanent note
     that sampling is every 10 seconds and shorter episodes can be missed.
  3. `sampled_at: null` with `stale: true` renders a specific state: "no lock
     sample has been stored for this instance yet", with the check's interval
     named. It must not read as "there is no contention".
  4. Query text at the 2048-byte boundary renders the `Truncated` primitive.
  5. Accessibility: the tree uses `role="tree"`/`role="treeitem"` with
     `aria-expanded` and `aria-level`, and is keyboard navigable with arrow
     keys.
- **Unit tests:**
  `UI-LOCK-010 renders roots in ranked order`;
  `UI-LOCK-011 sampled_at null renders the no-sample-yet state, not empty`;
  `UI-LOCK-012 the sample age is derived from sampled_at`;
  `UI-LOCK-013 truncated query text renders the Truncated primitive`;
  `UI-LOCK-014 a cycle is rendered with a cycle marker`;
  `UI-LOCK-015 the tree is keyboard navigable and exposes aria-level`;
  `expectNoA11yViolations` on the populated tree.
- **e2e tests:** `SYS-UI-008` in phase 17 creates real contention with the
  existing workload tool and asserts the tree appears.
- **Done:** gates green; closed in `STATE.md`.

### 13.3 Activity view

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/locks/ActivitySection.tsx`,
  `web/src/lib/activity.ts` (new)
- **Change:**
  1. Connection use against the configured maximum, with a saturation
     indication that uses the same threshold vocabulary as the `conn.saturation`
     advisor rule so the two views cannot disagree.
  2. Breakdowns by database and by state, plus age gauges (longest transaction,
     longest idle-in-transaction, oldest state age) and prepared-transaction
     count. An idle-in-transaction session older than a few minutes is a leading
     indicator of the contention shown above it, so it belongs on this page.
  3. Frozen-XID age with the wraparound risk framing used by the
     `table.wraparound_risk` advisor rule.
  4. Per-application counts render only when the agent reports them; otherwise a
     `Disabled` primitive names `checks.activity.by_application` and states it is
     opt-in. **Per-user breakdown does not exist** (decision D11); the UI must
     not offer a control for it, and the section's description says the
     breakdown available is per state and per database.
- **Unit tests:**
  `UI-LOCK-020 saturation uses the advisor's threshold`;
  `UI-LOCK-021 per-application counts render Disabled when not reported`;
  `UI-LOCK-022 no per-user control exists` — asserts the absence explicitly, so
  a future contributor adding one breaks a test and reads D11;
  `UI-LOCK-023 age gauges render Unknown for a null age`;
  `UI-LOCK-024 the deadlock counter is presented as a rate with the
  log-analysis caveat`.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 13.4 Cancel and terminate

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation.
  **`agent-1:opus` review gate** — these are the only destructive actions in the
  product's UI. Review the gate, the confirmation and the audit link.
- **Files:** `web/src/features/locks/SignalActions.tsx` (new)
- **Change:**
  1. Each session row offers **cancel query** and **terminate backend**, both
     built on the command lifecycle client from phase 12 § 12.3.
  2. Gating: both require tier **T2** and the target's `allow_signal: true`.
     Below T2 the controls are **disabled** with `NotPermitted` naming T2 and the
     grant (`monitoring_user.sql -v tier1=1 -v tier2=1`). Where the target policy
     is not visible to the UI, the control stays enabled and the agent's
     rejection is surfaced verbatim.
  3. Confirmation is mandatory for both and must name the exact pid, the
     database, the user, the application and the first line of the query text.
     Terminate's dialog states that the client's connection is closed and its
     open transaction is rolled back. The confirm button is not the default
     focus, and terminate's confirm control is visually distinguished as
     destructive.
  4. Only client backends can be signalled. Rows that are not client backends
     have the actions absent with a one-line reason, not merely disabled.
  5. After a terminal command state, the panel shows the outcome and a link to
     the instance's command audit, so the operator's action is traceable by
     someone else.
  6. There is no bulk action. A "terminate all" control on a monitoring tool is
     a foot-gun with no legitimate one-click use.
- **Unit tests:**
  `UI-LOCK-030 both actions are disabled below T2 with the grant named`;
  `UI-LOCK-031 the terminate confirmation names pid, database, user and
  application`;
  `UI-LOCK-032 the terminate confirmation states rollback and connection
  closure`;
  `UI-LOCK-033 the confirm button is not the default focus`;
  `UI-LOCK-034 a non-client backend offers no action and states why`;
  `UI-LOCK-035 an agent rejection is surfaced verbatim`;
  `UI-LOCK-036 a successful command links to the command audit`;
  `UI-LOCK-037 no bulk action exists` — an explicit absence test;
  `expectNoA11yViolations` on both dialogs.
- **e2e tests:** `SYS-UI-009` in phase 17 cancels a real long-running query on a
  T2-provisioned target.
- **Done:** gates green; `agent-1:opus` has reviewed; closed in `STATE.md`.

### 13.5 Degraded and error paths (rule T-4)

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/locks/LocksPage.tsx`
- **Change:** the remaining T-4 rows: no contention in the current sample
  (distinct from no sample at all), stale sample past the threshold, 401, 500,
  and the tier-gated path with the actions disabled.
- **Unit tests (route kind):**
  `UI-LOCK-040 no contention renders an explicit no-blocking state`;
  `UI-LOCK-041 no sample yet is distinguishable from no contention`;
  `UI-LOCK-042 a stale sample renders Stale with the sample age`;
  `UI-LOCK-043 a 401 navigates to login exactly once`;
  `UI-LOCK-044 a 500 renders ErrorState with a working retry`;
  `UI-LOCK-045 the page polls at the locks interval of five seconds`.
- **e2e tests:** none.
- **Done:** every applicable T-4 row has a named test; gates green; closed in
  `STATE.md`.

### 13.6 Update README.md

Mandatory closing sub-phase. User guide only — no implementation detail.

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — documentation
- **Files:** `README.md`
- **Change:** extend the **Web interface** section: the Locks and Activity page,
  that the blocking tree is a 10-second sample and shorter episodes can be
  missed, that "no sample yet" differs from "no contention", that cancel and
  terminate require T2 and the target's `allow_signal`, that both ask for
  confirmation naming the session, and that every action appears in the command
  audit. Reference the existing **Known limits** entries rather than restating
  them.
- **Unit tests:** none (documentation).
- **e2e tests:** none — the page was exercised against a contended L3 stack.
- **Done:** a user understands the sampling limit and the signal gates from the
  README alone; gates green; closed in `STATE.md` with the §11 docs row for
  phase 13 set.

---

## Phase gates

- **Fmt / Lint / Typecheck:** `make fmt-check`, `make web-lint`,
  `make web-typecheck`
- **Test subset:** `make web-test`
- **Coverage:** `make web-coverage-gate` — `src/lib/locks.ts` at or above 95
- **Regression guard:** `make test` and `make test-e2e` still green
- **README:** the Locks and Activity paragraph

## Phase done criterion

The blocking tree renders ranked roots from a sampled snapshot aged by
`sampled_at`, survives a cycle without hanging, distinguishes "no sample" from
"no contention", disables cancel and terminate below T2 with the grant named,
confirms both with the session identified, offers no bulk action, and `STATE.md`
§11 shows phase 13 `DONE` with every sub-phase closed.
