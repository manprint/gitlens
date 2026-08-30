# Phase 16 — Settings and fleet inventory

> **Intent:** the read-only inventory an operator needs to answer "what is pglens
> actually watching, with what permissions, and what is it not watching" — plus
> the two mutable surfaces the API supports, reached from here.
> **Shippable alone?** yes.
> **Preconditions:** phase 15 DONE.

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

> **Scope, from decision D10.** This page is read-only inventory plus links to
> the alert-rule and silence editors from phase 15. There is **no** user
> management, **no** RBAC, **no** enrollment approval queue and **no** agent
> revocation control, because the backend has none of them. The page must say so
> where an operator would reasonably look for them, rather than leaving an
> unexplained gap.

---

## Sub-phases

### 16.1 Inventory tables

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/settings/SettingsPage.tsx`,
  `InventoryTables.tsx` (new)
- **Change:**
  1. **Instances** table from `getInstances`: address and port, cluster (link),
     role, PostgreSQL version, permission tier, last seen, up state, and whether
     host metrics are available. Sortable and filterable; the filter is in the
     URL.
  2. **Databases** table, per instance, from `getInstanceDatabases`: name,
     monitored flag, `skip_reason`, and the `databases_not_monitored` count.
     Unmonitored databases sort first — they are the interesting rows.
  3. **Permission tiers** summary: how many instances are at T0, T1 and T2, and
     which advisor rules and actions each tier unlocks, cross-referenced to the
     rule catalogue from phase 14 § 14.4. This turns "should I grant T1" into a
     question with a visible answer.
  4. **Agents**: derived from the instances' `last_seen` and the firing
     `agent_down` alerts, since the API has no agent listing endpoint. Label the
     section accordingly and do not imply a richer agent registry than exists.
- **Unit tests:**
  `UI-SET-001 unmonitored databases sort first with their skip reason`;
  `UI-SET-002 the tier summary counts instances per tier`;
  `UI-SET-003 the tier summary lists what each tier unlocks`;
  `UI-SET-004 the agent view is derived from last_seen and alerts`;
  `UI-SET-005 filters round-trip through the URL`;
  `expectNoA11yViolations` on the populated page.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 16.2 Server information and product limits

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/settings/ServerInfo.tsx` (new)
- **Change:**
  1. Build identifier of the interface from `__PGLENS_BUILD__`, the API
     contract version from the OpenAPI document, and the session's expiry from
     `getSession`.
  2. A **Limits** panel summarising the product boundaries an operator will hit,
     each one linking to `docs/LIMITS.md`: 30-day raw retention with no rollups,
     PostgreSQL 15 to 18 only, streaming replication only, at most 10 databases
     per instance by default, top-N relation budgets, bloat as an estimate, ASH
     as statistical sampling, and no pooler view (decision D9).
  3. An **Authentication** panel stating exactly what is in force: a single
     shared password, no user accounts, no roles, no per-user audit, sessions
     that do not survive a server restart, and the agent's separate bearer token.
     This is where an operator will look for user management, and finding a clear
     "this does not exist" is better than finding nothing.
  4. A link to the rendered API reference (`docs/api.md`) and to the alert rules
     and silences pages.
- **Unit tests:**
  `UI-SET-010 renders the build identifier`;
  `UI-SET-011 the limits panel names the retention window and the version
  range`;
  `UI-SET-012 the limits panel states that no pooler view exists`;
  `UI-SET-013 the authentication panel states the absence of user accounts and
  roles`;
  `UI-SET-014 the session expiry is rendered from the session endpoint`.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 16.3 Command audit

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/settings/CommandAudit.tsx` (new)
- **Change:** a per-instance audit list from `getInstanceCommandAudit`: every
  request, claim, result, expiry, rejection and error, newest first, with the
  command kind, its arguments, its state and its timing. This is the record that
  makes the `EXPLAIN`, `cancel` and `terminate` actions accountable; it is
  reachable from here and from each command's result panel (phase 12 § 12.3,
  phase 13 § 13.4). State that the audit records the action, not the person, and
  why (no user identity exists).
- **Unit tests:**
  `UI-SET-020 the audit lists entries newest first`;
  `UI-SET-021 a rejected command shows the rejection reason`;
  `UI-SET-022 an expired command shows the expiry`;
  `UI-SET-023 the audit states that it records actions, not identities`;
  `UI-SET-024 an empty audit renders the empty state`.
- **e2e tests:** `SYS-UI-006` asserts an audit entry appears after a real
  `explain`.
- **Done:** gates green; closed in `STATE.md`.

### 16.4 Degraded and error paths (rule T-4)

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/settings/SettingsPage.tsx`
- **Change:** the applicable T-4 rows: an empty fleet, a per-instance endpoint
  failing while the instance list succeeds, stale data, 401, and 500.
- **Unit tests (route kind):**
  `UI-SET-030 an empty fleet renders the setup guidance`;
  `UI-SET-031 a failing per-instance databases endpoint marks that row
  unavailable and keeps the rest`;
  `UI-SET-032 stale data renders Stale`;
  `UI-SET-033 a 401 navigates to login exactly once`;
  `UI-SET-034 a 500 renders ErrorState with a working retry`.
- **e2e tests:** none.
- **Done:** every applicable T-4 row has a named test; gates green; closed in
  `STATE.md`.

### 16.5 Update README.md

Mandatory closing sub-phase. User guide only — no implementation detail.

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — documentation
- **Files:** `README.md`
- **Change:** extend the **Web interface** section with the Settings page: the
  inventory of instances and databases including what is not monitored and why,
  the permission-tier summary showing what each tier unlocks, the command audit,
  and the explicit statement that pglens has no user accounts, roles, enrollment
  queue or agent revocation control in the interface — revocation remains the
  documented SQL operation already described under **Configuration**. One
  paragraph.
- **Unit tests:** none (documentation).
- **e2e tests:** none — the page was exercised against the L3 stack.
- **Done:** an operator can find what is watched, what is not, and what the
  interface cannot do, from the README alone; gates green; closed in `STATE.md`
  with the §11 docs row for phase 16 set.

---

## Phase gates

- **Fmt / Lint / Typecheck:** `make fmt-check`, `make web-lint`,
  `make web-typecheck`
- **Test subset:** `make web-test`
- **Coverage:** `make web-coverage-gate`
- **Regression guard:** `make test` and `make test-e2e` still green
- **README:** the Settings paragraph

## Phase done criterion

The Settings page lists every instance and database including the unmonitored
ones with their reasons, summarises permission tiers against what they unlock,
exposes the command audit, and states plainly that user accounts, roles, an
enrollment queue and revocation controls do not exist in the interface;
`STATE.md` §11 shows phase 16 `DONE` with every sub-phase closed.
