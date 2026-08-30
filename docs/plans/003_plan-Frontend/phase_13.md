# Phase 12 — Query Inspector and plan history

> **Intent:** top queries from `pg_stat_statements`, their trend, and the
> on-demand `EXPLAIN` flow with its permission gate and plan history.
> **Shippable alone?** yes.
> **Preconditions:** phase 11 DONE.

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

- `queryid` is comparable **only within one cluster** and not across major
  versions. The API returns `comparable_scope: "cluster"`. Cross-cluster
  comparison must be impossible in the UI, not merely discouraged.
- Queries beyond `pg_stat_statements.max` are evicted: they appear and disappear.
  A query that vanishes from the list is not necessarily gone from the database.
- Only top-N queries per database are historised; the response carries
  `truncated`.
- A command carries `queryid` and options — **never SQL text**.
- `EXPLAIN ANALYZE` requires tier **T1** and `allow_explain_analyze: true` on the
  target; it executes the statement inside a rolled-back transaction but still
  consumes resources and can take locks.
- Commands expire after `PGLENS_COMMAND_TTL` (default 5m) and a late result is
  rejected. Command results are immutable.
- Normalised placeholders can make a plan unrepresentative for
  parameter-sensitive statements.

---

## Sub-phases

### 12.1 Statement derivation library

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/lib/statements.ts` (new)
- **Change:** pure functions:
  - `rankStatements(rows, orderBy)` for `total_exec_time`, `mean_exec_time`,
    `calls`, `rows`, `shared_blks_read`, `temp_blks_written`, each with a stable
    tiebreak on `queryid` so equal values do not reorder between polls.
  - `shareOfTotal(rows)` → each row's share of total execution time, returning
    `null` when the total is zero rather than `0%`.
  - `summariseStatement(row)` → mean, calls, total, rows per call, cache hit
    ratio, temporary bytes per call — every one of them `null`-safe.
  - `normaliseQueryText(text)` → collapses whitespace for the list view while
    keeping the full text for the detail view.
  - `comparableWithin(a, b)` → `true` only when both rows carry the same
    `cluster_id` **and** the same major version; the comparison UI is gated on
    it.
- **Unit tests (pure):**
  `UI-QRY-001 ranking is stable for equal values`;
  `UI-QRY-002 shareOfTotal returns null for a zero total`;
  `UI-QRY-003 rows per call is null when calls is zero`;
  `UI-QRY-004 comparableWithin is false across clusters`;
  `UI-QRY-005 comparableWithin is false across major versions`;
  `UI-QRY-006 queryid is preserved as a string throughout` (rule T-8).
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 12.2 The statement list

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/queries/QueryListPage.tsx` (new)
- **Change:**
  1. A `DataTable` over `getStatements` for the selected instance, database and
     time range, with the sort column reflected in the URL (`sort`) and mapped
     to the API's `order_by` so sorting is server-side, not a client-side sort of
     a truncated page. Sorting a truncated list client-side produces a wrong
     answer that looks right.
  2. Columns: normalised query text (monospace, one line, expandable), calls,
     total time, mean time, share of total, rows, cache hit ratio, temporary
     bytes. Numeric columns use tabular figures.
  3. `truncated: true` renders the `Truncated` primitive naming
     `checks.stat_statements.top_n`.
  4. A permanent note that queries beyond `pg_stat_statements.max` are evicted
     and may appear and disappear, and that `queryid` is comparable only within
     the cluster.
  5. When the `stat_statements` check is disabled or the extension is absent,
     the page renders `Disabled` naming `pg_stat_statements` and pointing at the
     README's extension guidance — not an empty table.
- **Unit tests:**
  `UI-QRY-010 sorting changes order_by and refetches`;
  `UI-QRY-011 truncated renders the top_n notice`;
  `UI-QRY-012 a missing extension renders Disabled, not an empty table`;
  `UI-QRY-013 the eviction note is present`;
  `UI-QRY-014 a row links to the detail route carrying the queryid as a
  string`;
  `expectNoA11yViolations` on the populated table.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 12.3 The command lifecycle client

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation.
  **`agent-1:opus` review gate** — a polling state machine with expiry and
  immutable results; getting the terminal states wrong strands the UI.
- **Files:** `web/src/api/commands.ts`, `web/src/lib/commands.ts` (new)
- **Change:**
  1. `useCommand(commandId)` polls `getCommand` at the `command` policy
     interval (1 s) and **stops polling on any terminal state**
     (`succeeded`, `failed`, `expired`, `rejected`). A poll loop that never stops
     is a resource leak against the server.
  2. `useCreateCommand()` posts the command, then hands the returned
     `command_id` to `useCommand`. The mutation is not retried.
  3. `src/lib/commands.ts::describeCommandState(state, error)` maps every state
     to an operator-facing sentence, exhaustively over the union. `expired` must
     say that the agent did not claim it in time and name the TTL; `rejected`
     must surface the agent's own reason, which is where the capability and
     target-policy gates report themselves.
  4. A client-side timeout equal to the TTL plus a grace period, after which the
     UI stops polling and states that the command expired, even if the server has
     not yet marked it. The UI must never spin forever waiting for an agent that
     is gone.
  5. Every command the UI issues is a normal API call; the command audit trail is
     the server's, not the UI's. Link to it from the result panel.
- **Unit tests:**
  `UI-CMD-001 polls at one second until a terminal state`;
  `UI-CMD-002 stops polling on succeeded, failed, expired and rejected` — table;
  `UI-CMD-003 surfaces the agent's rejection reason verbatim`;
  `UI-CMD-004 expires client-side after the TTL and grace period`;
  `UI-CMD-005 describeCommandState is exhaustive over the state union`;
  `UI-CMD-006 the mutation is not retried on failure`.
- **e2e tests:** `SYS-UI-006` in phase 17 runs a real `explain`.
- **Done:** gates green; `agent-1:opus` has reviewed; closed in `STATE.md`.

### 12.4 The EXPLAIN flow and its gates

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/queries/ExplainPanel.tsx` (new)
- **Change:**
  1. Two actions on a query: **plan only** (`analyze: false`) and **plan with
     ANALYZE** (`analyze: true`).
  2. Gating, both of which must be reflected in the UI rather than discovered
     from a rejection:
     - plan-only requires tier **T1**; below it the control is **disabled** with
       `NotPermitted` naming T1 and pointing at `monitoring_user.sql -v tier1=1`;
     - ANALYZE additionally requires `allow_explain_analyze: true` on the target;
       when that is not visible to the UI, the control stays enabled and the
       agent's rejection is surfaced verbatim — guessing a policy the UI cannot
       see would be worse than reporting the server's answer.
  3. ANALYZE requires an explicit confirmation dialog stating, in plain words,
     that the statement is actually executed inside a transaction that is rolled
     back, that it consumes resources, and that it can take locks. The dialog's
     confirm button is not the default focus.
  4. The request carries `queryid`, `datname` and the options. Assert in a test
     that the request body contains **no** query text — this is a privacy
     property of the product and must not regress.
  5. The result renders the JSON plan as a collapsible tree with node type,
     estimated and actual rows where present, and total cost; plus a raw JSON
     view. A plan produced from a normalised statement carries a note that
     placeholders may make it unrepresentative for parameter-sensitive queries.
- **Unit tests:**
  `UI-QRY-020 plan-only is disabled below T1 with the required tier named`;
  `UI-QRY-021 the ANALYZE confirmation states execution, resources and locks`;
  `UI-QRY-022 the confirm button is not the default focus`;
  `UI-QRY-023 the request body contains queryid and datname and no query text`;
  `UI-QRY-024 an agent rejection is surfaced verbatim`;
  `UI-QRY-025 the plan tree renders node types and costs`;
  `UI-QRY-026 the placeholder caveat is shown with every plan`;
  `expectNoA11yViolations` on the dialog and the plan tree.
- **e2e tests:** `SYS-UI-006`.
- **Done:** gates green; closed in `STATE.md`.

### 12.5 Plan history

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/queries/PlanHistory.tsx` (new)
- **Change:** a list from `getPlans` for the query, newest first, each entry
  showing when it was captured, whether it was an ANALYZE, and the plan hash.
  Selecting two entries shows them side by side with differing node types and
  costs highlighted. State once that plan history exists only for plans that were
  explicitly requested — pglens is not an automatic plan sampler — so an empty
  history is expected rather than a fault.
- **Unit tests:**
  `UI-QRY-030 an empty history renders the on-request explanation, not an
  error`;
  `UI-QRY-031 entries are ordered newest first`;
  `UI-QRY-032 comparing two plans highlights differing nodes`;
  `UI-QRY-033 comparison is unavailable with fewer than two entries`.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 12.6 Degraded and error paths (rule T-4)

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/queries/QueryListPage.tsx`,
  `QueryDetailPage.tsx`
- **Change:** the remaining T-4 rows: an empty statement list, a `queryid` that
  no longer exists (evicted — the detail page must explain eviction rather than
  showing "not found"), stale data, 401, 500, a 422 from an out-of-range
  parameter, and the tier-gated path.
- **Unit tests (route kind):**
  `UI-QRY-040 an evicted queryid renders the eviction explanation`;
  `UI-QRY-041 an empty list renders the empty state`;
  `UI-QRY-042 a 422 renders the server's detail message`;
  `UI-QRY-043 a 401 navigates to login exactly once`;
  `UI-QRY-044 a 500 renders ErrorState with a working retry`;
  `UI-QRY-045 the page polls at the statements interval`.
- **e2e tests:** none.
- **Done:** every applicable T-4 row has a named test; gates green; closed in
  `STATE.md`.

### 12.7 Update README.md

Mandatory closing sub-phase. User guide only — no implementation detail.

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — documentation
- **Files:** `README.md`
- **Change:** extend **On-demand operations** and the **Web interface** section:
  the Query Inspector, that comparison is restricted to one cluster, that
  `EXPLAIN` requires T1 and `EXPLAIN ANALYZE` additionally requires the target's
  `allow_explain_analyze`, that ANALYZE executes the statement inside a
  rolled-back transaction and asks for confirmation, that query text is never
  sent in a command, and that plan history exists only for explicitly requested
  plans. Keep the existing `curl` examples.
- **Unit tests:** none (documentation).
- **e2e tests:** none — the flow was exercised against the L3 stack.
- **Done:** a user understands the permission gates and the ANALYZE trade-off
  from the README alone; gates green; closed in `STATE.md` with the §11 docs row
  for phase 12 set.

---

## Phase gates

- **Fmt / Lint / Typecheck:** `make fmt-check`, `make web-lint`,
  `make web-typecheck`
- **Test subset:** `make web-test`
- **Coverage:** `make web-coverage-gate` — `src/lib/statements.ts` and
  `src/lib/commands.ts` at or above 95
- **Regression guard:** `make test` and `make test-e2e` still green
- **README:** the Query Inspector and permission-gate paragraph

## Phase done criterion

The Query Inspector sorts server-side, marks truncation, refuses cross-cluster
comparison, disables `EXPLAIN` below T1 with the required grant named, confirms
ANALYZE with an accurate description of what it does, provably sends no query
text in a command, stops polling a command at every terminal state, and
`STATE.md` §11 shows phase 12 `DONE` with every sub-phase closed.
