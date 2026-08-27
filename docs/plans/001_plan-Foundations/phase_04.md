# Phase 3 — Server: schema, ingest, storage, API

> **Intent:** Build the server half of the chain — the TimescaleDB schema, the
> ingest endpoint, identity resolution, delta conversion into storage, the read
> API, and the self-monitoring that makes a blind system say so.
> **Shippable alone?** yes — a server that accepts pushes and answers queries,
> even with no agent written yet (integration tests post envelopes directly).
> **Preconditions:** phase 2 DONE. `internal/wire` exists (sub-phase 2.6).

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

## Sub-phases

### 3.1 TimescaleDB schema and migrations

- **Model:** `agent-1:opus`
- **Assignment:** `agent-1:opus` — data-model design. **`agent-1` review gate by construction:** this is the most expensive decision in the plan to change afterwards (decision D10).
- **Files:** `internal/store/migrations/0001_meta.sql`, `internal/store/migrations/0002_hypertables.sql`, `internal/store/migrations/0003_policies.sql`, `internal/store/migrate.go`, `internal/store/clusterid.go`, `deploy/compose/timescaledb.yml`
- **Change:**

  > **`cluster_id` storage — read this before writing the DDL.**
  > `system_identifier` is a `uint64`. PostgreSQL has no unsigned 64-bit type.
  > Storing it as `numeric(20,0)` is exact but slow and bulky; as `text` it is
  > bulky and unindexable as a number. The column is therefore `bigint` holding
  > the **two's-complement bit pattern** of the `uint64` — in Go, `int64(v)` and
  > `uint64(v)` round-trip exactly. `clusterid.go` provides `ToDB(pgtype.ClusterID) int64`
  > and `FromDB(int64) pgtype.ClusterID`, and **nothing else may do the
  > conversion inline.** A value above `2^63` appears negative in `psql`; that is
  > expected and is documented in a comment on every column that holds one.
  > This is decision **D21**, added during phase 3 planning.

  1. `0001_meta.sql` — relational tables. Every one carries `tenant_id` with a
     default (decision D9): single-tenant behavior today, a backfill rather than
     a rewrite tomorrow.
     ```sql
     CREATE TABLE schema_migrations (
       version    int PRIMARY KEY,
       applied_at timestamptz NOT NULL DEFAULT now()
     );

     CREATE TABLE tenants (
       tenant_id text PRIMARY KEY,
       name      text NOT NULL
     );
     INSERT INTO tenants VALUES ('default', 'Default') ON CONFLICT DO NOTHING;

     CREATE TABLE agents (
       agent_id     uuid PRIMARY KEY,
       tenant_id    text NOT NULL DEFAULT 'default' REFERENCES tenants,
       agent_version text,
       first_seen   timestamptz NOT NULL DEFAULT now(),
       last_seen    timestamptz NOT NULL DEFAULT now(),
       revoked_at   timestamptz
     );

     CREATE TABLE clusters (
       tenant_id  text   NOT NULL DEFAULT 'default' REFERENCES tenants,
       cluster_id bigint NOT NULL,   -- uint64 bit pattern; see clusterid.go
       name       text,
       id_source  text   NOT NULL CHECK (id_source IN ('system_identifier','manual')),
       first_seen timestamptz NOT NULL DEFAULT now(),
       PRIMARY KEY (tenant_id, cluster_id)
     );

     CREATE TABLE instances (
       instance_id uuid PRIMARY KEY,
       tenant_id   text   NOT NULL DEFAULT 'default',
       cluster_id  bigint NOT NULL,   -- uint64 bit pattern
       agent_id    uuid   NOT NULL REFERENCES agents,
       addr        text   NOT NULL,
       port        int    NOT NULL,
       pg_version  int    NOT NULL,
       role        text   NOT NULL CHECK (role IN ('primary','standby','unknown')),
       perm_tier   text   NOT NULL,
       first_seen  timestamptz NOT NULL DEFAULT now(),
       last_seen   timestamptz NOT NULL DEFAULT now(),
       FOREIGN KEY (tenant_id, cluster_id) REFERENCES clusters (tenant_id, cluster_id)
     );
     -- Detects the duplicate-instance case of SYS-AGENT-002: an agent that lost
     -- its identity file re-registers the same address under a new UUID.
     CREATE INDEX instances_addr_idx ON instances (tenant_id, cluster_id, addr, port);

     CREATE TABLE databases (
       instance_id uuid NOT NULL REFERENCES instances ON DELETE CASCADE,
       datname     text NOT NULL,
       monitored   bool NOT NULL,
       skip_reason text,
       last_seen   timestamptz NOT NULL DEFAULT now(),
       PRIMARY KEY (instance_id, datname)
     );

     CREATE TABLE query_texts (
       tenant_id  text   NOT NULL DEFAULT 'default',
       cluster_id bigint NOT NULL,
       datname    text   NOT NULL,
       queryid    bigint NOT NULL,
       pg_major   int    NOT NULL,
       query_text text   NOT NULL,
       first_seen timestamptz NOT NULL DEFAULT now(),
       last_seen  timestamptz NOT NULL DEFAULT now(),
       PRIMARY KEY (tenant_id, cluster_id, datname, queryid, pg_major)
     );

     CREATE TABLE events (
       event_id    bigserial PRIMARY KEY,
       tenant_id   text        NOT NULL DEFAULT 'default',
       ts          timestamptz NOT NULL,
       type        text        NOT NULL,
       cluster_id  bigint,
       instance_id uuid,
       payload     jsonb       NOT NULL DEFAULT '{}'::jsonb
     );
     CREATE INDEX events_lookup_idx ON events (tenant_id, cluster_id, ts DESC);

     CREATE TABLE topology_edges (
       tenant_id     text   NOT NULL DEFAULT 'default',
       cluster_id    bigint NOT NULL,
       from_instance uuid   NOT NULL,
       to_instance   uuid   NOT NULL,
       edge_type     text   NOT NULL,
       sync_state    text,
       confidence    text   NOT NULL DEFAULT 'high' CHECK (confidence IN ('high','low')),
       updated_at    timestamptz NOT NULL DEFAULT now(),
       PRIMARY KEY (tenant_id, cluster_id, from_instance, to_instance)
     );
     ```
     `topology_edges` is created here even though phase 6 fills it, so the schema
     is designed once rather than migrated twice.

  2. `0002_hypertables.sql` — the hybrid model of decision D10: three typed
     tables for the domains that dominate cardinality, one generic table for
     everything else.
     ```sql
     -- Generic: any metric a check emits without a dedicated table.
     CREATE TABLE metrics (
       ts          timestamptz      NOT NULL,
       tenant_id   text             NOT NULL DEFAULT 'default',
       cluster_id  bigint           NOT NULL,
       instance_id uuid             NOT NULL,
       datname     text             NOT NULL DEFAULT '',  -- '' = instance scope
       metric      text             NOT NULL,
       labels      jsonb            NOT NULL DEFAULT '{}'::jsonb,
       series_id   bigint           NOT NULL,  -- FNV-1a 64 of the canonical SeriesKey
       value       double precision NOT NULL
     );
     SELECT create_hypertable('metrics', by_range('ts', INTERVAL '6 hours'));

     -- Invariant I-3 enforced by the storage engine rather than by hope:
     -- the same series can never be written twice for the same instant.
     CREATE UNIQUE INDEX metrics_dedup_idx ON metrics (series_id, ts);
     CREATE INDEX metrics_lookup_idx      ON metrics (instance_id, metric, ts DESC);

     CREATE TABLE metrics_statements (
       ts          timestamptz NOT NULL,
       tenant_id   text        NOT NULL DEFAULT 'default',
       cluster_id  bigint      NOT NULL,
       instance_id uuid        NOT NULL,
       datname     text        NOT NULL,
       queryid     bigint      NOT NULL,   -- signed; may legitimately be negative
       calls_rate             double precision,
       exec_time_rate_ms      double precision,
       rows_rate              double precision,
       shared_blks_hit_rate   double precision,
       shared_blks_read_rate  double precision,
       wal_bytes_rate         double precision
     );
     SELECT create_hypertable('metrics_statements', by_range('ts', INTERVAL '6 hours'));
     CREATE UNIQUE INDEX metrics_statements_dedup_idx
       ON metrics_statements (instance_id, datname, queryid, ts);

     CREATE TABLE metrics_ash (
       ts              timestamptz NOT NULL,
       tenant_id       text        NOT NULL DEFAULT 'default',
       cluster_id      bigint      NOT NULL,
       instance_id     uuid        NOT NULL,
       datname         text        NOT NULL DEFAULT '',
       wait_event_type text        NOT NULL,   -- 'CPU' when the backend was not waiting
       wait_event      text        NOT NULL,
       state           text        NOT NULL,
       queryid         bigint,                 -- NULL when compute_query_id is off
       samples         int         NOT NULL,
       window_seconds  int         NOT NULL
     );
     SELECT create_hypertable('metrics_ash', by_range('ts', INTERVAL '6 hours'));
     CREATE UNIQUE INDEX metrics_ash_dedup_idx ON metrics_ash
       (instance_id, datname, wait_event_type, wait_event, state,
        COALESCE(queryid, 0), ts);

     CREATE TABLE metrics_replication (
       ts          timestamptz NOT NULL,
       tenant_id   text        NOT NULL DEFAULT 'default',
       cluster_id  bigint      NOT NULL,
       instance_id uuid        NOT NULL,
       upstream_id uuid,
       slot_name   text        NOT NULL DEFAULT '',
       edge_type   text        NOT NULL DEFAULT 'streaming',
       sync_state  text,
       write_lag_bytes  bigint, flush_lag_bytes bigint, replay_lag_bytes bigint,
       write_lag_sec    double precision,
       flush_lag_sec    double precision,
       replay_lag_sec   double precision,
       slot_active         bool,
       slot_wal_status     text,
       slot_retained_bytes bigint
     );
     SELECT create_hypertable('metrics_replication', by_range('ts', INTERVAL '6 hours'));
     CREATE UNIQUE INDEX metrics_replication_dedup_idx ON metrics_replication
       (instance_id, COALESCE(upstream_id, '00000000-0000-0000-0000-000000000000'::uuid),
        slot_name, ts);
     ```
     `metrics_ash` and `metrics_replication` are created now even though phases 7
     and 6 populate them: decision D10 designs the schema once, and creating
     them later would be a migration the plan can avoid.

     > If `by_range(...)` is not accepted by the pinned TimescaleDB version, fall
     > back to the legacy signature
     > `create_hypertable('metrics','ts', chunk_time_interval => INTERVAL '6 hours')`
     > and record the substitution in `STATE.md` §8. Do not silently mix the two
     > forms across files.

  3. `0003_policies.sql` — compression and retention, ordered per decision D19.
     ```sql
     -- Ordering that must hold: agent buffer max_age (6h)
     --                        < server max_sample_age (12h)
     --                        < compress_after (48h)
     -- so replayed samples always land in an uncompressed chunk. Inserting into
     -- a compressed chunk is possible but slow, and it is avoidable by design.
     ALTER TABLE metrics_statements SET (
       timescaledb.compress,
       timescaledb.compress_segmentby = 'instance_id, datname, queryid',
       timescaledb.compress_orderby   = 'ts DESC'
     );
     SELECT add_compression_policy('metrics_statements', INTERVAL '48 hours');

     ALTER TABLE metrics_ash SET (
       timescaledb.compress,
       timescaledb.compress_segmentby = 'instance_id, wait_event_type, wait_event',
       timescaledb.compress_orderby   = 'ts DESC'
     );
     SELECT add_compression_policy('metrics_ash', INTERVAL '48 hours');

     ALTER TABLE metrics_replication SET (
       timescaledb.compress,
       timescaledb.compress_segmentby = 'instance_id, slot_name',
       timescaledb.compress_orderby   = 'ts DESC'
     );
     SELECT add_compression_policy('metrics_replication', INTERVAL '48 hours');

     ALTER TABLE metrics SET (
       timescaledb.compress,
       timescaledb.compress_segmentby = 'instance_id, metric',
       timescaledb.compress_orderby   = 'ts DESC'
     );
     SELECT add_compression_policy('metrics', INTERVAL '48 hours');

     SELECT add_retention_policy('metrics',             INTERVAL '30 days');
     SELECT add_retention_policy('metrics_statements',  INTERVAL '30 days');
     SELECT add_retention_policy('metrics_ash',         INTERVAL '30 days');
     SELECT add_retention_policy('metrics_replication', INTERVAL '30 days');
     ```
     `tenant_id` is deliberately **not** in any `compress_segmentby`: it is
     constant in single-tenant deployments and a constant column compresses to
     nothing regardless.
     Continuous aggregates and rollups are out of scope for this plan; the
     retention above keeps raw data for 30 days, which is enough for the
     foundations.

  4. `migrate.go` — migrations are embedded with `//go:embed migrations/*.sql`,
     applied in numeric order inside a transaction each, recorded in
     `schema_migrations`, and **forward-only**. Applied automatically at server
     startup. Running the server twice must be a no-op.
- **Unit tests:** `TestMigrations_Embedded` — every file matches `NNNN_*.sql`, the sequence has no gaps and no duplicates, and the embedded set is non-empty. `TestClusterID_DBRoundTrip` — `FromDB(ToDB(v)) == v` for `0`, `1`, `math.MaxInt64`, `math.MaxInt64+1`, `math.MaxUint64`; and `ToDB(math.MaxUint64) == -1`, asserted explicitly so the sign convention is pinned by a test rather than by a comment.
- **Integration tests:** these need TimescaleDB, so `pgtest` gains a `TimescaleDB()` helper starting `timescale/timescaledb:2.29.0-pg17` (decision D3).
  `INT-STORE-001` — migrating an empty database succeeds; running it a second time changes nothing and adds no `schema_migrations` row.
  `INT-STORE-002` — all four hypertables appear in `timescaledb_information.hypertables`.
  `INT-STORE-003` — inserting the same `(series_id, ts)` twice raises a unique violation, and with `ON CONFLICT DO NOTHING` writes exactly one row. **Invariant I-3 proven at the storage layer.**
  `INT-STORE-004` — compression and retention policies are registered for all four hypertables with the intervals above.
  `INT-STORE-005` — a `cluster_id` of `math.MaxUint64` survives insert and select unchanged through `ToDB`/`FromDB`.
- **e2e tests:** exercised by every phase 5 scenario.
- **Done:** `make test-integration` green; the migration runs clean twice in a row; `INT-STORE-003` proves the dedup index; decision D21 added to `overview.md`; closed in `STATE.md`.

### 3.2 Ingest endpoint and agent authentication

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — the public network surface.
- **Files:** `internal/server/ingest.go`, `internal/server/auth.go`, `internal/server/http.go`, `cmd/pglens-server/main.go`, and `_test.go` beside each
- **Change:** implements decision D13 — bootstrap token, server-assigned
  registration, working revocation. Deliberately **not** implemented: token
  rotation, mTLS, approval queue.

  Authentication, in order, on every request to `/api/v1/push`:
  1. Read the bearer token from `Authorization`. Missing or malformed → `401`.
  2. Compare against the configured bootstrap token with
     `subtle.ConstantTimeCompare`. **Never** use `==` on a secret: the timing
     difference is a real oracle. Mismatch → `401`.
  3. Read `agent_id` from the envelope. Unknown → insert into `agents`
     (auto-approve; there is no approval queue in this plan). Known and
     `revoked_at IS NOT NULL` → `401` with body
     `{"error":"agent revoked","agent_id":"..."}`.
  4. Update `agents.last_seen` and `agent_version`.

  Validation, before any write:
  - `protocol_version != wire.ProtocolVersion` → **`400`** with a body naming the
    supported version. An agent that is too old must fail loudly; a server that
    silently ignores fields it does not understand produces a fleet that looks
    monitored and is not
  - `sent_at` further than `max_sample_age` (12h, D19) in the past → the whole
    envelope is rejected `400` and `pglens_samples_too_old_total` is incremented
  - `sent_at` more than 30s in the future → accept, but compute and record
    `pglens_agent_clock_skew_seconds`; do not reject, since a skewed agent still
    carries useful data and rejecting it would hide the skew rather than surface
    it
  - malformed UUIDs, unparsable `cluster_id`, empty `instance_id` → `400`,
    naming the offending field
  - body larger than 32 MiB → `413`
  - request body is read through `http.MaxBytesReader`, and JSON decoding uses
    `DisallowUnknownFields` so a typo in a field name is an error rather than
    silence

  Handler shape: parse and validate → hand to the pipeline of 3.3/3.4 → return
  `202 Accepted` with `{"accepted":<n>,"rejected":<n>}`. Ingest is
  **idempotent**: reposting the identical envelope must produce no duplicate rows
  (guaranteed by the dedup indexes of 3.1).

  `cmd/pglens-server/main.go` becomes real: read `PGLENS_DSN`, `PGLENS_LISTEN`,
  `PGLENS_BOOTSTRAP_TOKEN` (and `..._FILE`), run migrations, start the HTTP
  server, expose `/healthz` (always 200 once listening) and `/readyz` (200 only
  when migrations are applied and the pool answers `SELECT 1`), and shut down
  gracefully on `SIGTERM` with a 15s drain.
- **Unit tests:** `TestAuth_ConstantTime` — asserts the comparison helper is `subtle.ConstantTimeCompare` (a reviewable structural test on the helper, plus a test that a wrong token of the correct length is rejected). `TestIngest_RejectsUnknownProtocolVersion` — version 2 gives 400 and the body names version 1. `TestIngest_RejectsUnknownFields` — an envelope with `"clusterid"` instead of `"cluster_id"` gives 400 naming the field. `TestIngest_RejectsOversizeBody` — 33 MiB gives 413. `TestIngest_RejectsStaleEnvelope` — `sent_at` 13h old gives 400. `TestIngest_AcceptsFutureSkew` — `sent_at` 5m ahead is accepted and the skew is recorded.
- **Integration tests:** `INT-INGEST-001` — a valid envelope returns 202 and the rows appear. `INT-INGEST-002` — the identical envelope posted twice leaves the row count unchanged (idempotency, I-3). `INT-INGEST-003` — an unknown `agent_id` is auto-registered. `INT-INGEST-004` — after `UPDATE agents SET revoked_at = now()`, the next push returns 401 and writes nothing. Backing test for `SYS-AGENT-005`. `INT-INGEST-005` — a wrong bootstrap token returns 401 and does not create an `agents` row.
- **e2e tests:** `SYS-AGENT-005` in phase 5.
- **Done:** `make test-integration` green; `grep -n 'BootstrapToken ==' internal/server/` finds nothing; posting the same envelope twice is provably a no-op; closed in `STATE.md`.

### 3.3 Identity resolution and inventory upsert

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implements invariant I-1.
- **Files:** `internal/server/inventory.go`, `internal/server/it_inventory_test.go`
- **Change:** turn each envelope instance into rows in `clusters`, `instances`,
  `databases`.
  - `cluster_id` comes from the envelope. When `cluster_id_source` is
    `system_identifier`, upsert `clusters` with that source. When it is `manual`
    and a row already exists with source `system_identifier`, **keep the
    identifier row** and emit a `cluster_identity_conflict` event: a real
    identifier always outranks a configured name
  - `instances` is upserted by `instance_id`, updating `role`, `pg_version`,
    `perm_tier`, `addr`, `port`, `last_seen`. **A role change from `primary` to
    `standby` or back emits a `role_change` event** — this is the raw signal
    phase 6 turns into `failover_detected`
  - **`cluster_id` never changes for an existing `instance_id`.** If an envelope
    claims a different one, reject that instance with a `400`-level error in the
    per-instance result and emit a `cluster_id_changed` event. Invariant I-1 is
    enforced here, not assumed
  - duplicate-instance detection: if a new `instance_id` arrives for an
    `(tenant_id, cluster_id, addr, port)` that already has a different live
    `instance_id` seen within the last 5 minutes, emit a
    `duplicate_instance_suspected` event carrying both ids. This is the
    observable consequence of `SYS-AGENT-002`, and the reason the index exists
  - `databases` is upserted from `Instance.Databases`, carrying `monitored` and
    `skip_reason` so the API can report "N databases not monitored" rather than
    implying full coverage
- **Unit tests:** pure-function tests for the conflict rules with fabricated inputs: identifier over manual, identifier vs identifier mismatch, manual vs manual mismatch.
- **Integration tests:** `INT-INV-001` — first envelope creates cluster, instance and database rows. `INT-INV-002` — a second envelope with a changed role updates the row and writes exactly one `role_change` event. `INT-INV-003` — an envelope claiming a different `cluster_id` for a known `instance_id` is rejected and emits `cluster_id_changed`; the stored `cluster_id` is unchanged. **Invariant I-1.** `INT-INV-004` — a manual `cluster_id` does not overwrite one sourced from `system_identifier`, and emits `cluster_identity_conflict`. `INT-INV-005` — two `instance_id` values for the same `addr:port` within 5 minutes emit `duplicate_instance_suspected`. `INT-INV-006` — a database marked `monitored=false` with a `skip_reason` round-trips to the API.
- **e2e tests:** `SYS-REPL-001` (phase 6) proves I-1 across a real failover; `SYS-AGENT-002` (phase 5) proves the duplicate detection.
- **Done:** `make test-integration` green; `INT-INV-003` passes; closed in `STATE.md`.

### 3.4 Delta conversion and the metric writer

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — hot path. **`agent-1:opus` review gate:** this is where `internal/delta` meets storage, and where a mistake reintroduces exactly the class of bug phase 1 was built to prevent.
- **Files:** `internal/server/pipeline.go`, `internal/server/writer.go`, `internal/store/write.go`, and `_test.go` beside each
- **Change:**
  - **Gauges are written directly.** They never touch `internal/delta`; enforce
    it with an explicit branch on `Metric.Kind` and a panic-free error on an
    unknown kind
  - **Counters go through `delta.Engine`.** A returned `*Point` is written; a
    returned `*ResetEvent` is written to `events` as `counter_reset_detected`
    with the metric name and reason in the payload; neither means nothing is
    written for that observation, which is the correct behavior and must be
    visible as a gap
  - the engine is keyed by `SeriesKey`, held in memory, and `Evict` runs every
    10 minutes for series unseen for an hour, bounding memory when instances
    disappear
  - **routing to tables** (decision D10): metrics from the `stat_statements`
    check go to `metrics_statements`; from `ash` to `metrics_ash`; from
    `replication_*` to `metrics_replication`; everything else to `metrics` with
    `series_id` computed as FNV-1a 64 over the canonical `SeriesKey`
  - all writes use `pgx.CopyFrom` batched per table per envelope, wrapped in one
    transaction per envelope, with `ON CONFLICT DO NOTHING` semantics. Since
    `CopyFrom` does not support `ON CONFLICT`, write into a `TEMP` staging table
    and `INSERT ... SELECT ... ON CONFLICT DO NOTHING`; alternatively use batched
    `INSERT` when an envelope carries fewer than 500 rows, which is the common
    case. Measure and pick; record the choice in `STATE.md` §8
  - `QueryTexts` are upserted into `query_texts` with `ON CONFLICT (…) DO UPDATE
    SET last_seen = excluded.last_seen`, so text is stored once and never
    duplicated per sample
  - a per-envelope failure never loses the whole envelope: instances are
    processed independently and the response reports counts per instance
- **Unit tests:** `TestPipeline_GaugeBypassesDelta` — a gauge produces a write with the raw value and the engine is never called. `TestPipeline_CounterProducesRate` — two envelopes 60s apart with a counter 100 then 160 produce one row with value 1.0. `TestPipeline_FirstCounterProducesNoRow` — one envelope with a counter produces no metric row and no event. `TestPipeline_ResetProducesEventNotRow` — a reset produces a `counter_reset_detected` event and no metric row. `TestPipeline_RoutingByCheck` — a table mapping check name to destination table, asserted exhaustively so a new check cannot silently land in the wrong one.
- **Integration tests:** `INT-PIPE-001` — two envelopes 60s apart produce exactly one `metrics` row with the expected rate. `INT-PIPE-002` — replaying both envelopes a second time produces no additional rows (I-3). `INT-PIPE-003` — a `stats_reset` change writes a `counter_reset_detected` event and no metric row for that interval; a query over the range shows a gap rather than a zero. `INT-PIPE-004` — statements metrics land in `metrics_statements`, not in `metrics`. `INT-PIPE-005` — query texts are stored once for 10 repeated envelopes and `last_seen` advances. `INT-PIPE-006` — a 5000-row envelope is written in under 2 seconds against a local container (a smoke bound, not a benchmark; adjust the bound once measured and record it in `STATE.md` §8).
- **e2e tests:** `SYS-RESET-001`, `SYS-NET-001` in phase 5.
- **Done:** `make test-integration` green; `INT-PIPE-003` shows a gap, never a zero; no negative value exists in `metrics` after the full suite (asserted by a query in the test teardown); closed in `STATE.md`.

### 3.5 Read API

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — the contract every later consumer builds on (decision D14 makes it the only consumer surface in this plan).
- **Files:** `internal/server/api.go`, `internal/server/api_test.go`, `internal/server/it_api_test.go`
- **Change:** implement the endpoints listed in `overview.md`.
  - `GET /api/v1/clusters` — one object per cluster: `cluster_id` as a **decimal
    string** (D18), `name`, `id_source`, `primary` (the `instance_id` currently
    in role `primary`, or `null`), `instance_count`, `health`, and the instances
    with `instance_id`, `addr`, `port`, `role`, `pg_version`, `perm_tier`,
    `last_seen`, `up`
  - `GET /api/v1/instances/{id}` — the instance plus its databases, with
    `monitored`, `skip_reason`, and a `databases_not_monitored` count so the
    caller can render the honest number
  - `GET /api/v1/metrics/query?metric=&instance_id=&database=&from=&to=&step=` —
    time series. **A missing interval is returned as an explicit `null`, never
    as `0` and never interpolated.** This is `IDEA.md` §6's honesty rule
    expressed in the API, and every consumer inherits it
  - `GET /api/v1/events?cluster_id=&type=&from=&to=&limit=` — newest first,
    `limit` capped at 1000
  - `GET /api/v1/statements?instance_id=&database=&from=&to=&order_by=&limit=` —
    top queries joined to `query_texts`, with a `truncated` flag propagated from
    ingest and a `comparable_scope` field reading `"cluster"` to state in the
    payload that `queryid` must not be compared across clusters (`IDEA.md` §4.6)
  - every list endpoint is bounded by an explicit `limit` with a documented
    maximum; no endpoint can be made to return an unbounded result set
  - errors are a consistent shape `{"error":"...","detail":"..."}` with the right
    status; `404` for a missing instance, `400` for a bad range, `422` for a
    `step` that would produce more than 10000 points
- **Unit tests:** handler tests with a fake store: parameter validation, the `null`-for-gap rendering, the point-count cap, the error shape, and `cluster_id` rendered as a quoted string.
- **Integration tests:** `INT-API-001` — after ingesting two envelopes, `/clusters` reports one cluster with the right primary. `INT-API-002` — `/metrics/query` over a range containing a reset returns `null` for the affected bucket and numbers either side. `INT-API-003` — `/events` returns the `counter_reset_detected` written by `INT-PIPE-003`. `INT-API-004` — `/statements` returns the text once and `comparable_scope == "cluster"`. `INT-API-005` — a `step` producing 20000 points returns 422. `INT-API-006` — `cluster_id` in every response body is a JSON string, asserted by scanning the raw bytes for `"cluster_id":"`.
- **e2e tests:** the reference scenario in phase 6 asserts against `/clusters`.
- **Done:** `make test-integration` green; every endpoint in the `overview.md` interface table exists and is covered; closed in `STATE.md`.

### 3.6 Self-monitoring and staleness

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — the tier-0 alerting substrate of `IDEA.md` §5.4.
- **Files:** `internal/server/staleness.go`, `internal/server/it_staleness_test.go`
- **Change:** `IDEA.md` v0.1 had no alerting on *absence* of data, which is the
  most important kind: a monitor that does not notice it has gone blind fails
  exactly during an incident. This sub-phase makes the system able to say "I do
  not know".
  - an evaluator goroutine runs every 15s on the injectable clock and, for every
    instance in `instances`, writes the gauge `pglens_up{instance_id}` — `1` when
    `last_seen` is within `3 x expected_interval` (default 90s), `0` otherwise —
    plus `pglens_agent_last_seen_seconds`
  - transitions emit events, **once per transition, never repeated per tick**:
    `agent_down` / `agent_up`, `instance_unreachable` / `instance_reachable`
  - `no_primary_in_cluster` when a cluster has had no `primary` instance for more
    than 60s
  - counters exposed by the server about itself:
    `pglens_ingest_envelopes_total{result}`, `pglens_ingest_rejected_total{reason}`,
    `pglens_samples_too_old_total`, `pglens_agent_clock_skew_seconds`,
    `pglens_series_total{instance_id}`, `pglens_cardinality_truncated_total`
  - the evaluator is **leader-elected** with
    `SELECT pg_try_advisory_lock(hashtext('pglens:staleness'))` on a dedicated
    session-scoped connection. Two server replicas must not both emit
    `agent_down`. This is the correction to the "stateless server" claim in
    `IDEA.md` v0.1 §5.5; the session lock releases automatically if the process
    dies, so no lease renewal machinery is needed
- **Unit tests:** `TestStaleness_TransitionsOnce` — with a fake clock, ten ticks past the threshold emit exactly one `agent_down`; recovery emits exactly one `agent_up`. `TestStaleness_Threshold` — at exactly the threshold `up` is still 1; one second past it is 0.
- **Integration tests:** `INT-STALE-001` — ingest, then advance past the threshold: `up` becomes 0 and an `agent_down` event exists. `INT-STALE-002` — resuming ingest flips `up` to 1 and emits exactly one `agent_up`. `INT-STALE-003` — two evaluator instances against the same database produce exactly one `agent_down` event, proving the advisory lock. `INT-STALE-004` — a cluster whose only primary disappears emits `no_primary_in_cluster` once.
- **e2e tests:** `SYS-NET-003` (black hole) and `SYS-AGENT-005` (revocation) in phase 5.
- **Done:** `make test-integration` green; `INT-STALE-003` proves single-leader behavior; closed in `STATE.md`.

### 3.7 Update README.md

Mandatory closing sub-phase of every phase. User guide only — no implementation
detail.

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — documentation; `agent-1:opus` reads it on the final phase.
- **Files:** `README.md`
- **Change:** this phase makes the **server** runnable for the first time. Add:
  - **Running the server** — the `docker compose` invocation from
    `deploy/compose/`, and the equivalent for the binary; what it does at
    startup (applies migrations, listens on `:8080`)
  - **Configuration** — a table of `PGLENS_DSN`, `PGLENS_LISTEN`,
    `PGLENS_BOOTSTRAP_TOKEN`, `PGLENS_BOOTSTRAP_TOKEN_FILE`, each with type,
    default and meaning. State plainly that the bootstrap token is a shared
    secret and that this release has no token rotation
  - **HTTP API** — one subsection per endpoint with a realistic `curl` and a
    trimmed real response. Include the two rules a consumer must know:
    `cluster_id` is a **string** because it does not fit a JSON number, and a
    missing interval is `null`, never `0`
  - **Requirements** — add PostgreSQL 17 with TimescaleDB 2.29 for the server's
    own storage
  - **Known limits** — no token rotation, no mTLS, no approval queue; raw
    retention is 30 days with no rollups; single-tenant
  Do not describe migrations, table names, the pipeline, or package layout.
- **Unit tests:** none (documentation).
- **e2e tests:** none — every `curl` shown was executed against a running server and produced the documented output.
- **Done:** a user can start the server from the README alone and successfully call every documented endpoint; no table name, package name or internal file path appears in the file; all gates green; closed in `STATE.md` with the §11 docs row for phase 3 set.

---

## Phase gates

- **Fmt:** `make fmt-check`
- **Lint:** `make lint`
- **Test subset:** `make test` and `make test-integration`
- **Coverage:** `make coverage-gate`
- **Regression guard:** phases 0 to 2 gates still green; `INT-CHECK-015` (every check runs as T0) still passes
- **README:** updated with the server, its configuration and its API, free of implementation detail

## Phase done criterion

A server started from `deploy/compose/` applies its migrations, accepts a valid
envelope on `POST /api/v1/push`, rejects an unknown protocol version, a stale
envelope and a revoked agent, converts counters to rates with reset detection,
never writes the same `(series, ts)` twice (`INT-INGEST-002`, `INT-PIPE-002`),
never changes a `cluster_id` for an existing instance (`INT-INV-003`), returns
`null` for gaps rather than zero (`INT-API-002`), and reports `pglens_up = 0`
with a single `agent_down` event when an agent stops (`INT-STALE-001`).
README.md reflects this phase's shipped behavior, and `STATE.md` §11 shows
phase 3 `DONE` with every sub-phase closed.
