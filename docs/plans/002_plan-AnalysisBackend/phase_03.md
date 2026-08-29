# Phase 2 — Protocol v2: facts and typed relation storage

> **Intent:** give the wire a transport for observations that are not numbers,
> and give relation-level series their own typed hypertables, so phases 3 to 8
> have somewhere to put their data.
> **Shippable alone?** yes — the envelope gains an optional array and the schema
> gains tables. With no check producing facts yet, behavior is unchanged.
> **Preconditions:** phase 1 `DONE`.

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

## Why this phase exists, in one paragraph

`wire.Metric` (`internal/wire/envelope.go:60`) carries a `float64`. Four things
this plan must collect are not numbers: the `CREATE INDEX` definition needed to
spot duplicate indexes, the textual value of a GUC, the blocking tree of a lock
storm, and a query plan. Rather than four bespoke fields, the envelope gains one
generic `facts` array (plan 002 D3). Separately, per-relation series must not go
into the generic `metrics` table, because `relname` would land in the label
string and destroy `compress_segmentby` on the largest tables in the store
(plan 002 D4).

---

## Sub-phases

### 2.1 Envelope version 2 and the `Fact` type

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — protocol change; review gate
- **Files:** `internal/wire/envelope.go` (modified),
  `internal/wire/envelope_test.go` (modified)
- **Change:**
  1. Add the type, below `Metric`:

     ```go
     // Fact is an observation that is not a number: an index definition, a GUC
     // value, a lock tree, a query plan. Exactly one of ValueText and ValueJSON
     // is set.
     type Fact struct {
         Kind      string            `json:"kind"`                 // "index_def" | "setting" | "lock_tree" | "plan"
         Key       string            `json:"key"`                  // stable identity within the kind
         Labels    map[string]string `json:"labels,omitempty"`
         ValueText string            `json:"value_text,omitempty"`
         ValueJSON json.RawMessage   `json:"value_json,omitempty"`
     }
     ```

  2. Add `Facts []Fact \`json:"facts,omitempty"\`` to `Result`
     (`internal/wire/envelope.go:48`), after `Metrics`.
  3. Introduce the version constants and the acceptance range:

     ```go
     const (
         ProtocolVersionMin     = 1
         ProtocolVersionCurrent = 2
     )
     ```

     The agent sets `ProtocolVersion: ProtocolVersionCurrent`. The server accepts
     anything in `[ProtocolVersionMin, ProtocolVersionCurrent]` and rejects
     anything outside with `400` and a body naming the supported range — this is
     plan 002 D2 and it deliberately widens plan 001's rule from "reject anything
     that is not 1" to "reject anything unknown".
  4. Add `func (f Fact) Validate() error` rejecting: an empty `Kind`; an empty
     `Key`; both `ValueText` and `ValueJSON` set; neither set; a `ValueJSON`
     that is not syntactically valid JSON; a `Kind` outside the four known
     values.

  > **Behavior change to mark loudly:** the ingest handler's version check
  > changes from equality to a range test. Find it in `internal/server/ingest.go`
  > and update it together with this sub-phase, or v2 agents will be rejected by
  > the server the moment the agent side lands.

- **Unit tests:** in `internal/wire/envelope_test.go` —
  `TestFact_Validate_RejectsEmptyKind`,
  `TestFact_Validate_RejectsEmptyKey`,
  `TestFact_Validate_RejectsBothValues`,
  `TestFact_Validate_RejectsNeitherValue`,
  `TestFact_Validate_RejectsMalformedJSON`,
  `TestFact_Validate_RejectsUnknownKind`,
  `TestEnvelope_V1RoundTripsWithoutFacts` (a v1 JSON document unmarshals with a
  nil `Facts` and marshals back without the key),
  `TestEnvelope_V2RoundTripsFacts`.
- **e2e tests:** none yet.
- **Done:** gates green + every existing `internal/wire` test passes unchanged +
  closed in `STATE.md`.

### 2.2 Migration `0007_facts.sql`

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — schema design; review gate
- **Files:** `internal/store/migrations/0007_facts.sql` (new)
- **Change:** three typed hypertables plus one relational fact table. Follow the
  column conventions of `0002_hypertables.sql` exactly: `tenant_id` first,
  `ts timestamptz NOT NULL`, `cluster_id bigint`, `instance_id uuid`, then the
  typed columns; hypertable with `by_range('ts', INTERVAL '6 hours')`;
  compression and retention matching plan 001 D19.

  ```sql
  CREATE TABLE IF NOT EXISTS metrics_tables (
    tenant_id     text        NOT NULL DEFAULT 'default',
    ts            timestamptz NOT NULL,
    cluster_id    bigint      NOT NULL,
    instance_id   uuid        NOT NULL,
    datname       text        NOT NULL,
    schemaname    text        NOT NULL,
    relname       text        NOT NULL,
    seq_scan            double precision,
    seq_tup_read        double precision,
    idx_scan            double precision,
    idx_tup_fetch       double precision,
    n_tup_ins           double precision,
    n_tup_upd           double precision,
    n_tup_del           double precision,
    n_tup_hot_upd       double precision,
    n_live_tup          bigint,
    n_dead_tup          bigint,
    n_mod_since_analyze bigint,
    last_vacuum         timestamptz,
    last_autovacuum     timestamptz,
    last_analyze        timestamptz,
    last_autoanalyze    timestamptz,
    autovacuum_count    double precision,
    autoanalyze_count   double precision,
    relpages            bigint,
    reltuples           double precision,
    relfrozenxid_age    bigint,
    total_bytes         bigint,
    table_bytes         bigint,
    toast_bytes         bigint
  );
  SELECT create_hypertable('metrics_tables', by_range('ts', INTERVAL '6 hours'), if_not_exists => TRUE);
  CREATE UNIQUE INDEX IF NOT EXISTS metrics_tables_dedup_idx
    ON metrics_tables (tenant_id, instance_id, datname, schemaname, relname, ts);
  CREATE INDEX IF NOT EXISTS metrics_tables_lookup_idx
    ON metrics_tables (instance_id, datname, ts DESC);

  CREATE TABLE IF NOT EXISTS metrics_indexes (
    tenant_id     text        NOT NULL DEFAULT 'default',
    ts            timestamptz NOT NULL,
    cluster_id    bigint      NOT NULL,
    instance_id   uuid        NOT NULL,
    datname       text        NOT NULL,
    schemaname    text        NOT NULL,
    relname       text        NOT NULL,
    indexrelname  text        NOT NULL,
    idx_scan          double precision,
    idx_tup_read      double precision,
    idx_tup_fetch     double precision,
    idx_blks_read     double precision,
    idx_blks_hit      double precision,
    index_bytes       bigint,
    is_unique         boolean,
    is_primary        boolean,
    is_valid          boolean,
    def_hash          text
  );
  SELECT create_hypertable('metrics_indexes', by_range('ts', INTERVAL '6 hours'), if_not_exists => TRUE);
  CREATE UNIQUE INDEX IF NOT EXISTS metrics_indexes_dedup_idx
    ON metrics_indexes (tenant_id, instance_id, datname, schemaname, indexrelname, ts);
  CREATE INDEX IF NOT EXISTS metrics_indexes_lookup_idx
    ON metrics_indexes (instance_id, datname, ts DESC);

  CREATE TABLE IF NOT EXISTS metrics_bloat (
    tenant_id     text        NOT NULL DEFAULT 'default',
    ts            timestamptz NOT NULL,
    cluster_id    bigint      NOT NULL,
    instance_id   uuid        NOT NULL,
    datname       text        NOT NULL,
    schemaname    text        NOT NULL,
    relname       text        NOT NULL,
    indexrelname  text        NOT NULL DEFAULT '',
    object_kind   text        NOT NULL CHECK (object_kind IN ('table','index')),
    method        text        NOT NULL CHECK (method IN ('estimate','pgstattuple')),
    real_bytes    bigint,
    expected_bytes bigint,
    bloat_bytes   bigint,
    bloat_ratio   double precision
  );
  SELECT create_hypertable('metrics_bloat', by_range('ts', INTERVAL '1 day'), if_not_exists => TRUE);
  CREATE UNIQUE INDEX IF NOT EXISTS metrics_bloat_dedup_idx
    ON metrics_bloat (tenant_id, instance_id, datname, schemaname, relname, indexrelname, method, ts);

  -- object_facts stores the latest textual observation per (kind, key) and the
  -- moment it last changed. It is deliberately NOT a hypertable: a GUC value or
  -- an index definition changes rarely, and what matters is the current value
  -- plus when it changed, not a sample every hour.
  CREATE TABLE IF NOT EXISTS object_facts (
    tenant_id   text        NOT NULL DEFAULT 'default',
    cluster_id  bigint      NOT NULL,
    instance_id uuid        NOT NULL,
    datname     text        NOT NULL DEFAULT '',
    kind        text        NOT NULL,
    key         text        NOT NULL,
    labels      jsonb       NOT NULL DEFAULT '{}'::jsonb,
    value_text  text,
    value_json  jsonb,
    first_seen  timestamptz NOT NULL,
    last_seen   timestamptz NOT NULL,
    changed_at  timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, instance_id, datname, kind, key)
  );
  CREATE INDEX IF NOT EXISTS object_facts_kind_idx
    ON object_facts (tenant_id, kind, instance_id);

  ALTER TABLE metrics_tables SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'instance_id, datname, schemaname, relname',
    timescaledb.compress_orderby   = 'ts DESC'
  );
  SELECT add_compression_policy('metrics_tables', INTERVAL '48 hours');

  ALTER TABLE metrics_indexes SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'instance_id, datname, schemaname, indexrelname',
    timescaledb.compress_orderby   = 'ts DESC'
  );
  SELECT add_compression_policy('metrics_indexes', INTERVAL '48 hours');

  ALTER TABLE metrics_bloat SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'instance_id, datname, relname',
    timescaledb.compress_orderby   = 'ts DESC'
  );
  SELECT add_compression_policy('metrics_bloat', INTERVAL '7 days');

  SELECT add_retention_policy('metrics_tables',  INTERVAL '30 days');
  SELECT add_retention_policy('metrics_indexes', INTERVAL '30 days');
  SELECT add_retention_policy('metrics_bloat',   INTERVAL '90 days');
  ```

  > `add_compression_policy` and `add_retention_policy` are not idempotent in
  > the `IF NOT EXISTS` sense. Look at how `0003_policies.sql` calls them and
  > match that style; if the existing migration relies on the runner never
  > re-applying a numbered file, do the same rather than inventing a guard.
  > Bloat gets a 90-day retention because it is sampled every six hours and its
  > value is the trend, not the point.

- **Unit tests:** `TestMigrations_0007_ContainsHypertables` in
  `internal/store/migrate_test.go`.
- **e2e tests:** `INT-FACT-001` — apply all migrations to a fresh TimescaleDB
  container and assert the three hypertables exist in
  `timescaledb_information.hypertables` and `object_facts` does not.
- **Done:** gates green + `make test-integration` green + closed in `STATE.md`.

### 2.3 Store row types and writers

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/store/write.go` (modified),
  `internal/store/write_test.go` (modified),
  `internal/store/write_integration_test.go` (modified)
- **Change:** add `TableStatRow`, `IndexStatRow`, `BloatRow` and `ObjectFactRow`
  mirroring the columns above, plus the four writers, following the exact shape
  of `WriteStatements` at `internal/store/write.go:219`: a `pgx.Batch` or
  `CopyFrom`, whichever the existing writers use, inside the caller's
  transaction, with `ON CONFLICT DO NOTHING` on the dedup index so a buffer
  replay cannot duplicate a row (plan 001 invariant I-3).

  `WriteObjectFacts` is the odd one out and needs this exact upsert semantic:

  ```sql
  INSERT INTO object_facts (...)
  VALUES (...)
  ON CONFLICT (tenant_id, instance_id, datname, kind, key) DO UPDATE
  SET last_seen  = EXCLUDED.last_seen,
      labels     = EXCLUDED.labels,
      value_text = EXCLUDED.value_text,
      value_json = EXCLUDED.value_json,
      changed_at = CASE
        WHEN object_facts.value_text IS DISTINCT FROM EXCLUDED.value_text
          OR object_facts.value_json IS DISTINCT FROM EXCLUDED.value_json
        THEN EXCLUDED.last_seen
        ELSE object_facts.changed_at
      END
  ```

  `changed_at` only moves when the value actually changed. That is what makes
  "this GUC was changed 20 minutes before the incident" answerable, and it is
  the whole reason the table is not a hypertable.

- **Unit tests:** `TestObjectFactRow_ChangedAtSQLMentionsIsDistinctFrom` is not
  a useful test; instead assert row-to-argument mapping:
  `TestTableStatRow_ArgsOrder`, `TestIndexStatRow_ArgsOrder`,
  `TestBloatRow_ArgsOrder` — each builds a row and asserts the produced argument
  slice matches the column order of the insert statement, which is the mistake a
  weak implementer makes most often here.
- **e2e tests:** `INT-FACT-002` — write the same `TableStatRow` twice and assert
  one row exists (dedup index holds).
  `INT-FACT-003` — write an `ObjectFactRow`, write it again unchanged, assert
  `changed_at` did not move and `last_seen` did; write it with a different
  `value_text` and assert `changed_at` moved.
- **Done:** gates green + `make test-integration` green + `INT-STORE-003` still
  passes + closed in `STATE.md`.

### 2.4 Pipeline routing for facts and relation metrics

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/server/pipeline.go` (modified),
  `internal/server/pipeline_test.go` (modified),
  `internal/server/pipeline_integration_test.go` (modified)
- **Change:**
  1. Extend `destinationTable` (`internal/server/pipeline.go:66`) with three
     cases, keeping the existing ones untouched:

     ```go
     case check == "table_stats":
         return "metrics_tables"
     case check == "index_stats":
         return "metrics_indexes"
     case check == "bloat_estimate":
         return "metrics_bloat"
     ```

  2. In `Process` (`internal/server/pipeline.go:83`), add the corresponding
     `case` branches next to the existing `"metrics_statements"` and
     `"metrics_replication"` branches. Follow `applyStatementMetric`
     (`internal/server/pipeline.go:568`) precisely: group the metrics of one
     result by their identity labels into one row per relation, then apply each
     metric to the right column by name with a `switch`. The grouping key is
     `(datname, schemaname, relname)` for tables and
     `(datname, schemaname, indexrelname)` for indexes.
  3. Counters still go through the delta engine
     (`internal/delta/engine.go:74`); gauges do not. `seq_scan`, `idx_scan`,
     `n_tup_*`, `autovacuum_count`, `idx_blks_*` are counters. `n_live_tup`,
     `n_dead_tup`, `relpages`, `*_bytes`, `relfrozenxid_age` are gauges. Getting
     this wrong produces rates where the advisor expects absolutes, so the test
     below asserts it explicitly.
  4. Add fact handling: for every `Result.Facts` entry, validate it, then map it
     to an `ObjectFactRow` and append to the batch written in the same
     transaction. An invalid fact increments the existing rejected counter and
     is skipped; it never aborts the envelope, matching how a bad metric is
     handled today.
  5. `lock_tree` and `plan` facts are **not** written to `object_facts`; they
     are routed to their own tables by phases 3 and 8. For now, a fact of those
     kinds is accepted and dropped with a debug log. Leave a `// phase 3` and
     `// phase 8` comment at the branch so the later implementer finds it.

- **Unit tests:** in `internal/server/pipeline_test.go` —
  `TestDestinationTable_RelationChecks`,
  `TestProcess_GroupsTableMetricsPerRelation`,
  `TestProcess_CounterVsGaugeClassification` (table-driven over every column,
  asserting which ones went through the delta engine),
  `TestProcess_InvalidFactIsSkippedNotFatal`,
  `TestProcess_LockTreeFactIsAcceptedAndDropped`.
- **e2e tests:** `INT-FACT-004` — post a **v1** envelope to the ingest endpoint
  of a v2 server and assert `202` and that the metrics landed; this is the
  rolling-upgrade guarantee of D2.
  `INT-FACT-005` — post a v2 envelope carrying table metrics and a `setting`
  fact, and assert one `metrics_tables` row and one `object_facts` row.
  `INT-FACT-006` — post `protocol_version: 3` and assert `400` with the
  supported range in the body.
- **Done:** gates green + `make test-integration` green + `INT-INGEST-002` and
  `INT-PIPE-003` still pass + closed in `STATE.md`.

### 2.5 Golden payload for protocol v2

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/wire/golden.go` (modified),
  `internal/wire/testdata/` (new files)
- **Change:** extend the existing golden mechanism so the v2 envelope shape is
  frozen. Read `internal/wire/golden.go` first and follow whatever regeneration
  entry point it already provides — `make golden` exists in the Makefile, so the
  regeneration path is already wired and must be reused, not replaced.

  Add one golden document per supported PostgreSQL version that includes at
  least one `facts` entry, so a change to the `Fact` JSON shape breaks a test in
  a pull request rather than in production.

  Keep the existing v1 goldens **unchanged and still asserted**. They are the
  regression that proves D2's backward compatibility is real.

- **Unit tests:** `TestGolden_V2ContainsFacts`,
  `TestGolden_V1GoldensStillMatch`.
- **e2e tests:** `INT-GOLDEN-002` — the existing `INT-GOLDEN-001` harness runs
  against the v2 payload for every version in the matrix.
- **Done:** gates green + `INT-GOLDEN-001` still passes unchanged + closed in
  `STATE.md`.

### 2.6 Agent-side fact emission plumbing

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/check/check.go` (modified),
  `internal/agent/pusher.go` (modified),
  `internal/check/check_test.go` (modified)
- **Change:** give checks a way to return facts.
  1. Add `Facts []Fact` to `check.Result`
     (`internal/check/check.go:68`), where `check.Fact` mirrors `wire.Fact` but
     uses `map[string]string` and `[]byte`; `internal/check` must not import
     `internal/wire`, so keep the two types separate and convert at the boundary,
     exactly as `Metrics` is converted today.
  2. In the conversion point in `internal/agent/pusher.go`, map
     `check.Result.Facts` onto `wire.Result.Facts`, validating each and dropping
     invalid ones with a warning that names the check and the fact key.
  3. Set `ProtocolVersion: wire.ProtocolVersionCurrent` where the envelope is
     built.

  > **This does not change the `Check` interface** (plan 002 D22). `Result` is a
  > struct, and adding a field to it is source-compatible with all eight
  > existing checks; none of them sets `Facts`, so all of them keep compiling
  > and behaving identically.

- **Unit tests:** `TestPusher_ConvertsFacts`,
  `TestPusher_DropsInvalidFactWithWarning`,
  `TestPusher_SetsProtocolVersion2`,
  plus a compile-level assertion that the eight existing checks still satisfy
  `check.Check` — the existing `check_test.go` already has such a table; extend
  it rather than adding a new one.
- **e2e tests:** none (phase 3 is the first producer).
- **Done:** gates green + every existing check test passes unchanged + closed in
  `STATE.md`.

### 2.7 Update README.md

Mandatory closing sub-phase of every phase. User guide only — no implementation
detail.

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — documentation
- **Files:** `README.md` (repo root)
- **Change:** the only user-visible change in this phase is compatibility
  behavior. Update:
  - **Troubleshooting** — what a `400` from the ingest endpoint means, with the
    exact message shape, and that it indicates an agent newer than the server.
  - **Limitations** — the server accepts agents speaking protocol version 1 or
    2; an agent newer than the server is rejected, so upgrade the server first.
  Do not document the `facts` array itself: it is an internal transport detail
  with no user-facing surface.
- **Unit tests:** none (documentation).
- **e2e tests:** none — the documented `400` body was produced by
  `INT-FACT-006`.
- **Done:** the upgrade ordering rule is stated in the README; no implementation
  detail present; gates green; closed in `STATE.md` with the §11 docs row for
  phase 2 set.

---

## Phase gates

- **Fmt:** `make fmt-check`
- **Lint:** `make lint`
- **Build:** `make build`
- **Test subset:** `make test`, `make test-integration`
- **Coverage:** `make coverage-gate`
- **Regression guard:** `INT-GOLDEN-001`, `INT-INGEST-002`, `INT-PIPE-003`,
  `INT-STORE-003`, `INT-CHECK-015` must all still pass unchanged.
- **README:** upgrade ordering documented.

## Execution record

- **Status:** complete, pending phase-close commit
- **Model:** `agent:gpt5.6-luna`
- **Sub-phases:** 2.1–2.7 implemented and verified in order
- **Acceptance tests:** `INT-FACT-001..006` and `INT-GOLDEN-002` pass; legacy `INT-GOLDEN-001` remains green against unchanged v1 fixtures
- **Final technical gates:** fmt-check, lint, build, unit/race, L2 integration, and coverage 75.1% pass

## Phase done criterion

A v2 agent and a v1 agent both push successfully to the same server;
`protocol_version: 3` is rejected with the supported range; a `setting` fact
lands in `object_facts` with a `changed_at` that only moves when the value moves;
and `metrics_tables`, `metrics_indexes`, `metrics_bloat` exist as compressed
hypertables with dedup indexes. `INT-FACT-001` through `INT-FACT-006` and
`INT-GOLDEN-002` are green. README.md reflects this phase's shipped behavior,
and `STATE.md` §11 shows phase 2 `DONE` with every sub-phase closed.
