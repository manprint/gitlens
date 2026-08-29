# Phase 4 — Space and maintenance: tables, indexes, vacuum, bloat

> **Intent:** collect the per-relation data that answers index questions,
> autovacuum questions and bloat questions, under a hard cardinality budget.
> **Shippable alone?** yes — four new per-database checks and three read
> endpoints; nothing existing changes.
> **Preconditions:** phase 2 `DONE` (typed hypertables and the `facts`
> transport). Phase 3 is not required but will normally be done first.

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

## The rule that governs this whole phase

Every check here is **database-scoped**: `pg_stat_user_tables`,
`pg_stat_user_indexes` and `pg_statio_*` return only the rows of the database you
are connected to. That means `Requirements.Scope = ScopeDatabase` and a
connection obtained with `t.ConnFor(ctx, t.Database())`. It also means the
cardinality maths is `instances × databases × relations`, which is why sub-phase
4.5 exists and why no check in this phase may emit an unbounded number of series.

The five steps for adding a check are written out in `phase_04.md`, section
"How to add a check". Follow them; they are not repeated here.

---

## Sub-phases

### 4.1 The `table_stats` check

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/check/table_stats.go` (new),
  `internal/check/table_stats_test.go` (new),
  `internal/check/table_stats_integration_test.go` (new)
- **Change:** implement a check named `table_stats`.

  ```go
  Requirements{
      Scope:    ScopeDatabase,
      PermTier: pgtype.TierReadOnly, // T0
  }
  DefaultInterval() = 5 * time.Minute
  Timeout()         = 30 * time.Second
  ```

  One query:

  ```sql
  SELECT s.schemaname,
         s.relname,
         s.seq_scan, s.seq_tup_read, s.idx_scan, s.idx_tup_fetch,
         s.n_tup_ins, s.n_tup_upd, s.n_tup_del, s.n_tup_hot_upd,
         s.n_live_tup, s.n_dead_tup, s.n_mod_since_analyze,
         s.last_vacuum, s.last_autovacuum, s.last_analyze, s.last_autoanalyze,
         s.autovacuum_count, s.autoanalyze_count,
         c.relpages, c.reltuples,
         age(c.relfrozenxid)                              AS relfrozenxid_age,
         pg_total_relation_size(c.oid)                    AS total_bytes,
         pg_table_size(c.oid)                             AS table_bytes,
         COALESCE(pg_total_relation_size(c.reltoastrelid), 0) AS toast_bytes
  FROM pg_stat_user_tables s
  JOIN pg_class c ON c.oid = s.relid
  WHERE c.relkind = 'r'
  ```

  Emit **one metric per column per relation**, with labels
  `{schemaname, relname}`. The metric names are the column names prefixed with
  `pg_table_`, for example `pg_table_n_dead_tup`. The server maps them back onto
  the `metrics_tables` columns in the routing added in sub-phase 2.4, so the
  names must match the columns exactly — a typo here silently drops a column.

  The four `last_*` timestamps are not numbers, so they travel as **ages**:
  emit `pg_table_last_vacuum_age_seconds`,
  `pg_table_last_autovacuum_age_seconds`, `pg_table_last_analyze_age_seconds`
  and `pg_table_last_autoanalyze_age_seconds`, each computed as
  `EXTRACT(epoch FROM now() - s.<column>)`, and emit **nothing** when the
  underlying timestamp is null. A table that has never been autovacuumed must
  stay distinguishable from one autovacuumed a second ago, and a zero would say
  the opposite of the truth.

  The server derives the four `timestamptz` columns of `metrics_tables` from
  those ages at write time — `row.ts - age` — in the routing added in sub-phase
  2.4. Extend that routing here if it does not already do so, and record the
  extension in `STATE.md` §8 as a deviation touching phase 2. When an age metric
  is absent, the corresponding column stays `NULL`, which is the value the
  advisor rule for "never autovacuumed" looks for.

  Counters (`seq_scan`, `seq_tup_read`, `idx_scan`, `idx_tup_fetch`, `n_tup_*`,
  `autovacuum_count`, `autoanalyze_count`) are `pgtype.KindCounter`. Everything
  else is `pgtype.KindGauge`. This classification is asserted by
  `TestProcess_CounterVsGaugeClassification` from sub-phase 2.4.

  Apply the selector from sub-phase 4.5 before emitting. Set
  `Result.Truncated = true` when it truncates.

- **Unit tests:** `TestTableStats_MetricNamesMatchColumns` (a table of the
  expected names, so a rename breaks the build),
  `TestTableStats_NullLastAutovacuumEmitsNothing`,
  `TestTableStats_CounterGaugeClassification`,
  `TestTableStats_RequiresDatabaseScope`,
  `TestTableStats_TruncatedFlagSetWhenSelectorTruncates`.
- **e2e tests:** `INT-TBL-001` — create a table, insert and delete rows,
  `Scrape` reports a positive `n_dead_tup` for it.
  `INT-TBL-002` — `VACUUM` the table, `Scrape` reports
  `pg_table_last_vacuum_age_seconds` present and small.
  `INT-TBL-003` — a freshly created, never-vacuumed table emits no
  `last_autovacuum` age metric at all.
- **Done:** gates green + closed in `STATE.md`.

### 4.2 The `index_stats` check

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/check/index_stats.go` (new),
  `internal/check/index_stats_test.go` (new),
  `internal/check/index_stats_integration_test.go` (new)
- **Change:** implement a check named `index_stats`, same scope, tier, interval
  and timeout as `table_stats`.

  ```sql
  SELECT s.schemaname,
         s.relname,
         s.indexrelname,
         s.idx_scan, s.idx_tup_read, s.idx_tup_fetch,
         io.idx_blks_read, io.idx_blks_hit,
         pg_relation_size(s.indexrelid)  AS index_bytes,
         ix.indisunique                  AS is_unique,
         ix.indisprimary                 AS is_primary,
         ix.indisvalid                   AS is_valid,
         pg_get_indexdef(s.indexrelid)   AS indexdef
  FROM pg_stat_user_indexes s
  JOIN pg_index ix ON ix.indexrelid = s.indexrelid
  LEFT JOIN pg_statio_user_indexes io ON io.indexrelid = s.indexrelid
  ```

  Metrics are named `pg_index_<column>` with labels
  `{schemaname, relname, indexrelname}`. `is_unique`, `is_primary` and
  `is_valid` are gauges carrying 0 or 1.

  **The index definition is a fact**, not a metric. For every index that
  survives the selector, emit one fact:

  - `Kind: "index_def"`
  - `Key: schemaname + "." + indexrelname`
  - `Labels: {schemaname, relname, indexrelname}`
  - `ValueText:` the `pg_get_indexdef` output verbatim

  Compute `def_hash` as the lowercase hex SHA-256 of the **normalised**
  definition and carry it in the fact labels under `def_hash`, so the server can
  copy it into the `def_hash` column without re-parsing anything.
  Normalisation is: take the
  substring from the first `(` to the matching final `)`, lowercase it, and
  collapse runs of whitespace to a single space. That strips the index name,
  which differs between two otherwise identical indexes, so two duplicates hash
  the same. The advisor rule in phase 7 groups by `(relname, def_hash)` to find
  duplicates, and by column-list prefix to find redundancy.

  > The `def_hash` column of `metrics_indexes` is filled by the server from the
  > fact label, in the routing already added in sub-phase 2.4. If that routing
  > does not currently copy a label into a column, extend it here and record the
  > extension in `STATE.md` §8 as a deviation touching phase 2.

- **Unit tests:** `TestIndexStats_MetricNamesMatchColumns`,
  `TestIndexStats_EmitsIndexDefFact`,
  `TestIndexStats_DefHashIgnoresIndexName` (two definitions differing only in the
  index name hash identically),
  `TestIndexStats_DefHashDistinguishesColumnOrder` (`(a, b)` and `(b, a)` hash
  differently),
  `TestIndexStats_DefHashIsWhitespaceInsensitive`,
  `TestIndexStats_BooleanColumnsAreZeroOrOne`.
- **e2e tests:** `INT-IDX-001` — create two identical indexes under different
  names on a real container and assert both facts carry the same `def_hash`.
  `INT-IDX-002` — create an index and never use it; `idx_scan` is 0. Run a query
  that uses it; `idx_scan` is positive on the next scrape.
  `INT-IDX-003` — an invalid index (created with a failed `CREATE INDEX
  CONCURRENTLY`, or marked invalid directly) reports `is_valid = 0`.
- **Done:** gates green + closed in `STATE.md`.

### 4.3 The `vacuum_progress` check

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/check/vacuum_progress.go` (new),
  `internal/check/vacuum_progress_test.go` (new),
  `internal/check/vacuum_progress_integration_test.go` (new)
- **Change:** implement a check named `vacuum_progress`.

  ```go
  Requirements{
      Scope:    ScopeInstance,       // the progress views are cluster-wide
      PermTier: pgtype.TierReadOnly,
  }
  DefaultInterval() = 30 * time.Second
  Timeout()         = 5 * time.Second
  ```

  Two queries, both cheap:

  ```sql
  SELECT p.pid, d.datname, c.relname, p.phase,
         p.heap_blks_total, p.heap_blks_scanned, p.heap_blks_vacuumed,
         p.index_vacuum_count, p.max_dead_tuple_bytes, p.dead_tuple_bytes
  FROM pg_stat_progress_vacuum p
  JOIN pg_database d ON d.oid = p.datid
  LEFT JOIN pg_class c ON c.oid = p.relid;

  SELECT p.pid, d.datname, c.relname, p.phase,
         p.sample_blks_total, p.sample_blks_scanned
  FROM pg_stat_progress_analyze p
  JOIN pg_database d ON d.oid = p.datid
  LEFT JOIN pg_class c ON c.oid = p.relid;
  ```

  > **Version caveat the implementer must handle:** the dead-tuple columns of
  > `pg_stat_progress_vacuum` were renamed across the supported range —
  > `max_dead_tuples`/`num_dead_tuples` in the older versions and
  > `max_dead_tuple_bytes`/`dead_tuple_bytes` in the newer ones. Do **not** guess
  > which boundary applies. Probe once per connection with
  > `SELECT column_name FROM information_schema.columns WHERE table_name =
  > 'pg_stat_progress_vacuum'`, cache the answer on the check for that target,
  > and select the columns that actually exist. Emit both under the single stable
  > metric name `pg_vacuum_dead_tuple_bytes`, converting a tuple count to bytes
  > only if the byte column is absent, in which case emit
  > `pg_vacuum_dead_tuples` instead and leave the byte metric unemitted. A test
  > asserts the probe happens exactly once per connection.

  Metrics carry labels `{datname, relname, phase}`, plus
  `pg_vacuum_jobs_running` and `pg_analyze_jobs_running` as unlabelled gauges.
  Cap the labelled series at 20 concurrent jobs; more than that on one instance
  is itself the finding, and the count gauge still tells the truth.

- **Unit tests:** `TestVacuumProgress_ProbesColumnsOnce`,
  `TestVacuumProgress_UsesByteColumnsWhenPresent`,
  `TestVacuumProgress_FallsBackToTupleColumns`,
  `TestVacuumProgress_JobCountGaugesAlwaysEmitted`,
  `TestVacuumProgress_LabelledSeriesCapped`.
- **e2e tests:** `INT-VAC-001` — on a real container, run `VACUUM` on a large
  enough table in a background session and assert `pg_vacuum_jobs_running` is 1
  while it runs and 0 after. Use a table big enough that the vacuum lasts longer
  than one scrape; generate it with the existing workload helpers in
  `test/workload/` rather than a hand-rolled loop.
  `INT-VAC-002` — the check returns identical metric names on every supported
  version, which is what proves the column probe works.
- **Done:** gates green + `INT-VAC-002` green on all four versions + closed in
  `STATE.md`.

### 4.4 The `bloat_estimate` check

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/check/bloat_estimate.go` (new),
  `internal/check/bloat_estimate_test.go` (new),
  `internal/check/bloat_estimate_integration_test.go` (new),
  `test/fixtures/sql/bloat_estimate.sql` (new — the query, kept as a file so it
  is reviewable and testable on its own)
- **Change:** implement a check named `bloat_estimate`.

  ```go
  Requirements{
      Scope:    ScopeDatabase,
      PermTier: pgtype.TierReadOnly,
  }
  DefaultInterval() = 6 * time.Hour
  Timeout()         = 120 * time.Second
  ```

  The estimate is the standard statistical approach: compare the physical size
  of the relation with the size implied by `pg_stats` column widths and null
  fractions, plus per-page and per-tuple overheads. Structure the query in this
  shape, and keep it in `test/fixtures/sql/bloat_estimate.sql` loaded with
  `//go:embed`:

  1. A CTE over `pg_stats` computing, per `(schemaname, tablename)`, the sum of
     `avg_width * (1 - null_frac)` across columns, and the count of nullable
     columns.
  2. A CTE joining that to `pg_class` and `pg_namespace` for `relpages`,
     `reltuples` and `oid`, restricted to `relkind = 'r'` and excluding the
     `pg_catalog` and `information_schema` schemas.
  3. A final projection computing `expected_bytes` from the tuple width plus
     the header overhead of 23 bytes, the null bitmap when there are nullable
     columns, alignment to 8 bytes, and the page overhead of 24 bytes against a
     fill factor read from `pg_class.reloptions` and defaulting to 100.
  4. `bloat_bytes = GREATEST(real_bytes - expected_bytes, 0)` and
     `bloat_ratio = bloat_bytes / NULLIF(real_bytes, 0)`.

  Index bloat uses the same idea against `pg_index` and the indexed column
  widths, emitting rows with `object_kind = 'index'`.

  Emit one metric group per relation with labels
  `{schemaname, relname, indexrelname, object_kind}` and metric names
  `pg_bloat_real_bytes`, `pg_bloat_expected_bytes`, `pg_bloat_bytes`,
  `pg_bloat_ratio`, all gauges. Set `method = "estimate"` — the server writes it
  into the `method` column, which is what lets an exact `pgstattuple` result
  from phase 8 sit beside an estimate without either overwriting the other.

  Hard rules, each with a test:
  - Skip relations smaller than 1 MiB. Bloat on a small table is noise and would
    consume the cardinality budget that a real problem needs.
  - Skip relations with `reltuples < 0` (never analysed) — the estimate is
    meaningless without statistics, and reporting it as zero bloat is worse than
    reporting nothing.
  - Apply the selector of sub-phase 4.5 with its own budget slot, ordered by
    `bloat_bytes` descending, so the biggest problems always survive truncation.
  - `ApplySessionLimits` with the 120 s timeout, so a schema large enough to
    blow the budget cancels server-side instead of holding a connection.

  > **This check is the heaviest thing the agent does.** Invariant I-8 says it
  > must never delay another check. It is database-scoped and therefore runs on
  > a rotating per-database connection, not the shared one, so the constraint is
  > satisfied by construction — but assert it in `INT-BLOAT-002` rather than
  > assuming it.

- **Unit tests:** `TestBloat_SkipsSmallRelations`,
  `TestBloat_SkipsUnanalysedRelations`,
  `TestBloat_RatioIsZeroWhenNoBloat`,
  `TestBloat_MethodIsEstimate`,
  `TestBloat_SelectorOrdersByBloatBytes`,
  `TestBloat_TimeoutIs120s`.
- **e2e tests:** `INT-BLOAT-001` — create a table of at least 2 MiB, delete 50 %
  of the rows without vacuuming, and assert `pg_bloat_ratio` is above 0.2. Then
  `VACUUM FULL` it and assert the ratio drops below 0.1 on the next scrape. The
  exact numbers depend on the estimator, so assert the direction and the
  threshold, never an exact value.
  `INT-BLOAT-002` — while `bloat_estimate` is running against a large schema,
  the `activity` check on the same target still completes within its own
  interval. This is the test of invariant I-8.
  `INT-BLOAT-003` — a 10 000-relation schema truncates rather than emitting
  10 000 series, and `Result.Truncated` is true.
- **Done:** gates green + `make test-integration` green + closed in `STATE.md`.

### 4.5 The shared relation cardinality budget

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — this is the cardinality decision of plan 002
  D5 and a review gate; getting it wrong costs storage in production, not in CI
- **Files:** `internal/check/relselect.go` (new),
  `internal/check/relselect_test.go` (new)
- **Change:** implement one shared selection helper used by `table_stats`,
  `index_stats` and `bloat_estimate`, built on
  `cardinality.Selector` (`internal/cardinality/selector.go:54`) and
  `cardinality.Budget` (`internal/cardinality/budget.go:17`). Do not write a
  second selection algorithm; the hysteresis in `Selector` exists precisely so a
  relation on the boundary does not flap in and out of the series set.

  ```go
  // RelSelector picks which relations are reported for one target, under a
  // per-instance budget shared across databases.
  type RelSelector struct { /* one cardinality.Selector per kind */ }

  type RelKind string
  const (
      RelKindTable RelKind = "table"
      RelKindIndex RelKind = "index"
      RelKindBloat RelKind = "bloat"
  )

  func NewRelSelector(maxTables, maxIndexes int) *RelSelector

  // Select ranks candidates and returns the survivors plus whether it
  // truncated. Ranking is by the candidate's Score, descending, with ties
  // broken by name so the result is deterministic.
  func (s *RelSelector) Select(kind RelKind, cycle uint64, in []cardinality.Candidate) ([]cardinality.Candidate, bool)
  ```

  The score each check supplies:

  | Check | Score | Why |
  |-------|-------|-----|
  | `table_stats` | `n_dead_tup + seq_tup_read + n_mod_since_analyze` | the three signals a maintenance problem shows up in |
  | `index_stats` | `total_bytes - idx_scan_weighted` where an unused large index scores highest | an unused index is a finding; a hot small index is not |
  | `bloat_estimate` | `bloat_bytes` | the biggest waste survives truncation |

  The budget is **per instance, not per database** (D5). Databases are scraped at
  different moments, so the selector must carry the used count across databases
  within one cycle. Implement that by keying the internal state on
  `(kind, cycle)` and resetting at the start of each cycle, in the same way the
  existing statement selector handles `cycle`.

  When truncation happens, the check must also emit
  `pglens_relations_truncated{kind}` as a counter, so the existing Tier 0 rule
  `cardinality_budget_exceeded` fires on it and an operator learns their budget
  is too small instead of quietly seeing half their schema.

- **Unit tests:** `TestRelSelector_RespectsBudget`,
  `TestRelSelector_BudgetIsSharedAcrossDatabases`,
  `TestRelSelector_HighestScoreSurvives`,
  `TestRelSelector_TieBrokenByName`,
  `TestRelSelector_HysteresisPreventsFlapping` (a relation just below the cut
  in one cycle and just above in the next is not admitted and evicted on
  alternating cycles),
  `TestRelSelector_EmitsTruncatedCounter`,
  `TestRelSelector_ResetsPerCycle`.
- **e2e tests:** covered by `INT-BLOAT-003` and by `SYS-IDX-002` in phase 9.
- **Done:** gates green + `internal/check` coverage not below its current value
  + closed in `STATE.md`.

### 4.6 Relation API

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/server/api_relations.go` (new),
  `internal/server/api_relations_test.go` (new),
  `internal/server/api_relations_integration_test.go` (new),
  `internal/server/http.go` (modified)
- **Change:** three endpoints, all read-only, all reading the latest sample per
  relation within a lookback of three intervals.

  | Route | Query parameters | Response |
  |-------|------------------|----------|
  | `GET /api/v1/instances/{id}/tables` | `datname`, `order_by` (`dead_tup` default, `size`, `seq_scan`), `limit` (default 50, max 500) | one object per table with the stat columns, `total_bytes`, `relfrozenxid_age`, and the four maintenance ages |
  | `GET /api/v1/instances/{id}/indexes` | `datname`, `order_by` (`size` default, `scans`), `limit`, `unused_only` (bool) | one object per index with its stats, `index_bytes`, the boolean flags and the definition text read from `object_facts` |
  | `GET /api/v1/instances/{id}/bloat` | `datname`, `object_kind`, `limit` | one object per relation with `real_bytes`, `expected_bytes`, `bloat_bytes`, `bloat_ratio`, `method`, `ts` |

  Every response carries a top-level `"truncated": <bool>` and
  `"relations_not_reported": <int|null>` derived from the truncation counter, so
  a client can render "showing 100 of about 4 000" instead of implying the
  schema has 100 relations. `null` means the count is unknown, which is
  different from zero and must not be collapsed into it.

- **Unit tests:** `TestTablesAPI_DefaultOrderIsDeadTup`,
  `TestTablesAPI_RejectsUnknownOrderBy`,
  `TestTablesAPI_LimitIsCapped`,
  `TestIndexesAPI_UnusedOnlyFilter`,
  `TestIndexesAPI_IncludesDefinition`,
  `TestBloatAPI_FiltersByObjectKind`,
  `TestRelationsAPI_TruncatedIsSurfaced`.
- **e2e tests:** `INT-TBL-004` — ingest a `table_stats` payload and read it back
  through the endpoint with the values intact.
  `INT-IDX-004` — the index endpoint returns the definition text that was
  carried as a fact.
- **Done:** gates green + `make test-integration` green + closed in `STATE.md`.

### 4.7 Integration sweep and permission verification

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — testing
- **Files:** `internal/check/registry_integration_test.go` (modified),
  `deploy/sql/monitoring_user.sql` (modified if and only if a grant is missing)
- **Change:** extend the existing `INT-CHECK-015` table so it covers the four
  new checks: every one of them must run green as a **T0-only** user and, when
  it cannot, must be excluded upstream with a reason rather than failing.

  Run it first **without** touching `deploy/sql/monitoring_user.sql`. If a check
  genuinely needs a grant the script does not give — the likely candidate is
  reading `pg_stats` for a table the monitoring user does not own — add the
  minimal grant to the shipped script and note it in `STATE.md` §8. Do not create
  a test-only copy of the script: the L2 harness applies the shipped file
  precisely so the script users run is the script tested (`STATE.md` §10 of plan
  001 records this as a do-not-repeat).

  > `pg_stats` is a view filtered by ownership. A monitoring user that is not the
  > table owner sees no rows for that table, and the bloat estimate silently
  > becomes empty rather than wrong. Assert this explicitly: `INT-BLOAT-004`
  > runs the estimate as the T0 user against a table owned by someone else and
  > asserts the check reports a skip reason rather than returning zero bloat.
  > `pg_read_all_stats`, which `pg_monitor` includes, is what makes the rows
  > visible; the test is what proves it on every version.

- **Unit tests:** none (this sub-phase is the tests).
- **e2e tests:** `INT-CHECK-016` — the extended tier table covering
  `table_stats`, `index_stats`, `vacuum_progress`, `bloat_estimate`.
  `INT-BLOAT-004` — the ownership case described above.
- **Done:** both green on all four versions + `SYS-PERM-001` still passes +
  closed in `STATE.md`.

### 4.8 Update README.md

Mandatory closing sub-phase of every phase. User guide only — no implementation
detail.

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — documentation
- **Files:** `README.md` (repo root)
- **Change:** update these sections for what this phase actually made usable:
  - **Checks** — add `table_stats`, `index_stats`, `vacuum_progress` and
    `bloat_estimate` to the checks table with interval, scope and tier.
  - **Configuration** — the four intervals, `top_n` for tables and indexes, and
    the note that the top-N budget is **per instance and shared across
    databases**.
  - **Usage** — a `curl` for each of the three new endpoints with its expected
    JSON, including the `truncated` field.
  - **Limitations** — bloat is a **statistical estimate**, not a measurement;
    exact figures require `pgstattuple` and are available on demand only where
    that extension is already installed. Relations under 1 MiB and
    never-analysed relations are not estimated. Only the top-N relations per
    instance are reported; the rest are counted, not listed. Table and index
    statistics are per database and require a connection to each monitored
    database, so they are subject to the database budget.
- **Unit tests:** none (documentation).
- **e2e tests:** none — the `curl` examples were executed and produced the
  documented output.
- **Done:** a new user can list their unused indexes and their most bloated
  tables from the README alone; the estimate-not-measurement caveat is stated
  before any number is shown; gates green; closed in `STATE.md` with the §11 docs
  row for phase 4 set.

---

## Phase gates

- **Fmt:** `make fmt-check`
- **Lint:** `make lint`
- **Build:** `make build`
- **Test subset:** `make test`, `make test-integration`
- **Coverage:** `make coverage-gate`
- **Regression guard:** `INT-CHECK-015`, `INT-STMT-002` (the existing
  cardinality cap), `SYS-PERM-001` and every phase 2 test must still pass.
- **README:** the estimate caveat and the shared budget are documented.

## Phase done criterion

Against a real database containing a 2 MiB table with 50 % dead rows, an unused
duplicate index and a 10 000-relation schema, the agent reports bloat above the
threshold, both duplicate indexes with an identical `def_hash`, and truncates to
the configured budget with `truncated` visible through the API. All four new
checks run green as a T0-only user on every supported version. `INT-TBL-001` to
`INT-TBL-004`, `INT-IDX-001` to `INT-IDX-004`, `INT-VAC-001`/`002`,
`INT-BLOAT-001` to `INT-BLOAT-004` and `INT-CHECK-016` are green. README.md
reflects this phase's shipped behavior, and `STATE.md` §11 shows phase 4 `DONE`
with every sub-phase closed.

## Execution record

- **Assignment:** `agent:gpt5.6-luna`
- **Status:** complete
- **Result:** 4.1–4.8 completed in order. Table, index, vacuum and bloat checks,
  shared relation selection, relation API, T0 registry verification and README
  documentation are present. INT-TBL-001..004, INT-IDX-001..004,
  INT-VAC-001/002, INT-BLOAT-001..004 and INT-CHECK-016 pass in the focused
  acceptance run; the ownership behavior records the actual shipped
  `pg_monitor` visibility in STATE D-020.
- **Final gates:** `make fmt-check`, `make lint`, `make build`, `make test`
  (race), `make coverage-gate` (75.1%, 3968/5287), and
  `make test-integration` all pass.
