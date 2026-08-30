# Phase 10 — Instance Detail

> **Intent:** the per-instance view: throughput and latency, per-database
> metrics with an explicit database selector, host metrics when the agent is
> local, settings with change history, and the relation and space views.
> **Shippable alone?** yes.
> **Preconditions:** phase 9 DONE.

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

## Data sources

| Need | Operation | Notes |
|------|-----------|-------|
| instance, databases, `skip_reason`, `databases_not_monitored` | `getInstance`, `getInstanceDatabases` | the count of unmonitored databases is a required display, not optional |
| time series | `queryMetrics` | `from`, `to`, `step` required; a counter reset yields `null`, never a negative or a spike |
| host metrics | `getInstanceHost` | remote instances return `available: false` with a `reason` |
| settings and change history | `getInstanceSettings` (+ `changed_since`) | includes source, context and pending-restart |
| activity gauges | `getInstanceActivity` | connection counts by database and state |
| relations | `getInstanceTables`, `getInstanceIndexes`, `getInstanceBloat` | each response carries `truncated` |

---

## Sub-phases

### 10.1 Instance header and role banner

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/instance/InstancePage.tsx`,
  `InstanceHeader.tsx` (new)
- **Change:** header shows address and port, role, PostgreSQL version rendered
  from `pg_version` as a human version (`170011` → `17.11`), permission tier,
  last seen, up state, the owning cluster as a link, and a tab bar for the
  instance-scoped routes (overview, ASH, queries, locks). A standby renders a
  persistent banner reading `standby (read-only)` with its replay lag beside it,
  because a query run against the wrong node is a real operational error.
- **Unit tests:**
  `UI-INST-001 formats pg_version 170011 as 17.11` — pure, in
  `src/lib/format/version.ts`, table over 150011, 160002, 180000;
  `UI-INST-002 a standby renders the read-only banner with its lag`;
  `UI-INST-003 a primary renders no read-only banner`;
  `UI-INST-004 an instance with up=false renders the down treatment`;
  `UI-INST-005 the cluster link uses the exact string cluster id`.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 10.2 Database selector and the unmonitored count

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/instance/DatabaseSelector.tsx`,
  `web/src/lib/databases.ts` (new)
- **Change:**
  1. Per-database metrics are per database; the page therefore carries an
     explicit selector whose value lives in the URL (`db`). Default is the
     database with the highest activity, not "all", because summing across
     databases produces numbers that mean nothing.
  2. The selector lists monitored databases and, separately and visibly, the
     count of databases **not** monitored with their `skip_reason` grouped
     (`db_budget`, `excluded`, …). A hidden database is a blind spot and must be
     stated, with a link to the README's `databases.max` and `include`
     configuration.
  3. `src/lib/databases.ts` provides `partitionDatabases(list)` →
     `{monitored, skipped: Map<reason, names[]>}` and
     `defaultDatabase(list)`.
- **Unit tests (pure):**
  `UI-INST-010 partitions monitored from skipped and groups by reason`;
  `UI-INST-011 defaultDatabase picks the most active monitored database`;
  `UI-INST-012 defaultDatabase returns null when none are monitored`.
  **Component:**
  `UI-INST-013 the selector writes db to the URL`;
  `UI-INST-014 the unmonitored count is displayed with its reasons`;
  `UI-INST-015 zero monitored databases renders an explicit state, not an empty
  dropdown`.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 10.3 Metric tiles and time series

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/instance/OverviewSection.tsx`,
  `web/src/components/charts/metric.options.ts` (new)
- **Change:**
  1. A tile row: connections in use against the configured maximum, transactions
     per second, cache hit ratio, WAL generated in the range, checkpoint
     frequency, temporary bytes, deadlocks. Each tile is a `MetricTile` (phase 7
     § 7.4) and therefore renders `Unknown` for a `null`.
  2. A chart grid over the selected range, each driven by `queryMetrics` with the
     derived `step` from phase 7 § 7.3.
  3. **Counter resets are a first-class display.** The API returns `null` for a
     bucket where a counter reset was detected. The chart draws a gap **and**
     marks the point with a reset annotation, because "the number went to zero"
     and "the statistics were reset" look identical in a naive rendering and mean
     entirely different things. Derive the annotation in
     `src/lib/metrics.ts::findResets(series)`.
  4. Cache hit ratio uses `formatPercent`, which returns `null` for a zero
     denominator (phase 6 § 6.5) — an idle database has no hit ratio, not a 0%
     one.
- **Unit tests (pure):**
  `UI-INST-020 findResets marks each null bucket bounded by values`;
  `UI-INST-021 findResets does not mark a leading or trailing null`;
  `UI-INST-022 the metric option preserves nulls and sets connectNulls false`;
  `UI-INST-023 hit ratio with zero reads is Unknown, not 0%`.
  **Component:**
  `UI-INST-024 a reset renders the annotation and a gap`;
  `UI-INST-025 every tile renders Unknown for a null value`;
  `UI-INST-026 changing the time range refetches with a new step`.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 10.4 Host metrics with honest unavailability

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/instance/HostSection.tsx`
- **Change:**
  1. When `available: true`, render CPU, memory, load and filesystem values that
     were actually measured, and **only** those: the API returns only measurable
     fields, so the UI must render the absent ones as `Unknown` rather than
     assuming zero.
  2. Show the `source` (`host`, `cgroup_v1`, `cgroup_v2`) prominently. Inside a
     container the reported total memory is the cgroup limit, not the machine's,
     and an operator reading "8 GiB" needs to know which.
  3. When `available: false`, render the `Degraded` primitive with the server's
     own `reason` string and a link to the README's explanation of `host_local`.
     Never render zeros, never hide the section: a missing host section reads as
     "no problem", and this is a known product limit that should be visible.
  4. State that per-device IOPS, disk latency and network metrics are not
     collected, once, in the section's description rather than as empty tiles.
- **Unit tests:**
  `UI-INST-030 an available host renders only the measured fields`;
  `UI-INST-031 an unmeasured field renders Unknown`;
  `UI-INST-032 an unavailable host renders Degraded with the server reason`;
  `UI-INST-033 the cgroup source is displayed when memory comes from a limit`;
  `UI-INST-034 the section is never hidden`.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 10.5 Settings, change history and durability

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/instance/SettingsSection.tsx`,
  `web/src/lib/settings.ts` (new)
- **Change:**
  1. A searchable table of settings: name, value, unit, source, context,
     pending-restart flag, and when it was last observed to change. A
     `pending_restart` setting is flagged prominently — it is the difference
     between a change that is in force and one that is not.
  2. A "changed since" control using the `changed_since` parameter, defaulting
     to the selected time range, so an operator can answer "what changed today".
  3. `archive_command` values arrive with only the first token retained and the
     rest as `[redacted]`. Render the redaction explicitly with a tooltip
     explaining that arguments are withheld because they may contain
     credentials — an unexplained truncated command looks like a bug.
  4. A durability summary derived from the settings the product already
     collects: `fsync`, `full_page_writes`, `archive_mode`, `wal_level`,
     `synchronous_commit`. Where a value is dangerous the row carries the same
     severity vocabulary the advisor uses, so the two views agree.
- **Unit tests (pure, `src/lib/settings.ts`):**
  `UI-INST-040 normalises byte and time units for display`;
  `UI-INST-041 flags pending_restart`;
  `UI-INST-042 detects a redacted archive_command`;
  `UI-INST-043 classifies fsync=off as critical`.
  **Component:**
  `UI-INST-044 the redaction is explained rather than shown as truncation`;
  `UI-INST-045 changed_since narrows the table and is reflected in the URL`;
  `UI-INST-046 an empty changed-since result renders an explicit no-changes
  state`.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 10.6 Relations, bloat and truncation

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/instance/RelationsSection.tsx`
- **Change:**
  1. Three tables — tables, indexes, bloat estimates — each sortable, each
     paginated through the API's `limit`.
  2. Every response carries `truncated`. When it is true, the `Truncated`
     primitive states the top-N budget, that it is shared per instance across
     databases, and where to change it (`checks.table_stats.top_n`). A truncated
     list presented as complete is how an operator concludes a table does not
     exist.
  3. Bloat rows must state that the figure is a statistical estimate, not a
     measurement, and that relations under 1 MiB and never-analysed relations are
     not estimated. Put it in the section description, once, not per row.
  4. Where `pgstattuple` is available, offer the exact-bloat command as a link to
     the Query Inspector's command flow (phase 12); where the extension is
     absent, render `NotPermitted`-style guidance naming the extension and
     stating that pglens never installs it.
- **Unit tests:**
  `UI-INST-050 a truncated response renders Truncated naming the budget and the
  config key`;
  `UI-INST-051 an untruncated response renders no truncation notice`;
  `UI-INST-052 the bloat section states the estimate caveat once`;
  `UI-INST-053 a never-analysed relation is absent rather than shown as zero
  bloat`;
  `UI-INST-054 the exact-bloat action is disabled with a reason when
  pgstattuple is absent`;
  `expectNoA11yViolations` on the populated section.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 10.7 Update README.md

Mandatory closing sub-phase. User guide only — no implementation detail.

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — documentation
- **Files:** `README.md`
- **Change:** extend the **Web interface** section: the Instance Detail page,
  the database selector and the visible count of unmonitored databases, the
  standby read-only banner, counter resets drawn as gaps with an annotation,
  host metrics that state their source and say so when unavailable, the settings
  view with pending-restart and the `archive_command` redaction, and the relation
  tables with their top-N truncation notice. One paragraph plus a short list. No
  file paths, no component names.
- **Unit tests:** none (documentation).
- **e2e tests:** none — the page was opened against the L3 stack.
- **Done:** a user can interpret every panel from the README alone; gates green;
  closed in `STATE.md` with the §11 docs row for phase 10 set.

---

## Phase gates

- **Fmt / Lint / Typecheck:** `make fmt-check`, `make web-lint`,
  `make web-typecheck`
- **Test subset:** `make web-test`
- **Coverage:** `make web-coverage-gate`
- **Regression guard:** `make test` and `make test-e2e` still green
- **README:** the Instance Detail paragraph

## Phase done criterion

The Instance Detail page renders per-database metrics behind an explicit
selector that names the unmonitored count, draws counter resets as annotated
gaps, renders host unavailability with the server's own reason instead of zeros,
explains the `archive_command` redaction, and flags every truncated relation
list; every applicable T-4 row has a test; and `STATE.md` §11 shows phase 10
`DONE` with every sub-phase closed.
