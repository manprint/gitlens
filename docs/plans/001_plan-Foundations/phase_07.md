# Phase 6 — Replication and topology engine

> **Intent:** Make replication a first-class citizen — the streaming, receiver
> and slot checks, the central topology fusion that reconstructs the graph, and
> the failover detection that proves the reference scenario.
> **Shippable alone?** yes — adds checks, server-side fusion and API surface.
> **Preconditions:** phase 5 DONE. The `primary-standby` topology and the
> harness exist.

This phase delivers the plan's acceptance criterion. Everything before it has
been building the machinery required to state one thing with confidence: a
`cluster_id` survives a failover.

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

### 6.1 Replication checks

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — role-aware SQL across the version matrix.
- **Files:** `internal/check/replication_streaming.go`, `internal/check/replication_receiver.go`, `internal/check/replication_slots.go`, and `it_*_test.go` beside each
- **Change:** three checks, all `ScopeInstance`, tier `TierReadOnly`, using only
  the shared connection.

  **`replication_streaming`** — `Requires{Roles: [RolePrimary]}`,
  `DefaultInterval` 10s, `Timeout` 2s.
  ```sql
  SELECT application_name,
         COALESCE(client_addr::text, '')                AS client_addr,
         state, COALESCE(sync_state, 'async')           AS sync_state,
         pg_wal_lsn_diff(sent_lsn, write_lsn)::bigint   AS write_lag_bytes,
         pg_wal_lsn_diff(sent_lsn, flush_lsn)::bigint   AS flush_lag_bytes,
         pg_wal_lsn_diff(sent_lsn, replay_lsn)::bigint  AS replay_lag_bytes,
         EXTRACT(epoch FROM write_lag)                  AS write_lag_sec,
         EXTRACT(epoch FROM flush_lag)                  AS flush_lag_sec,
         EXTRACT(epoch FROM replay_lag)                 AS replay_lag_sec
    FROM pg_stat_replication;
  ```
  Emits one `metrics_replication` row per standby. `write_lag`, `flush_lag` and
  `replay_lag` are NULL until the standby has reported at least once — emit NULL,
  **never zero**: "not yet known" and "no lag" are different facts, and
  collapsing them is exactly the dishonesty this project exists to avoid.

  **`replication_receiver`** — `Requires{Roles: [RoleStandby]}`,
  `DefaultInterval` 10s, `Timeout` 2s.
  ```sql
  SELECT COALESCE(w.status, 'disconnected')      AS status,
         COALESCE(w.sender_host, '')             AS sender_host,
         COALESCE(w.sender_port, 0)              AS sender_port,
         COALESCE(w.slot_name, '')               AS slot_name,
         pg_last_wal_receive_lsn()::text         AS receive_lsn,
         pg_last_wal_replay_lsn()::text          AS replay_lsn,
         CASE
           WHEN pg_last_wal_receive_lsn() = pg_last_wal_replay_lsn() THEN 0
           ELSE EXTRACT(epoch FROM now() - pg_last_xact_replay_timestamp())
         END                                     AS replay_lag_sec
    FROM (SELECT 1) dummy
    LEFT JOIN pg_stat_wal_receiver w ON true;
  ```

  > **The `CASE` guard is load-bearing.** On an idle primary,
  > `now() - pg_last_xact_replay_timestamp()` grows without bound even though the
  > standby is perfectly caught up, because no new transaction has arrived to
  > update the timestamp. Reporting that as replication lag is one of the most
  > common wrong-alert bugs in PostgreSQL monitoring, and it pages people at
  > night for nothing. Comparing the receive and replay LSNs first is the correct
  > test for "caught up".

  The `LEFT JOIN` on a dummy row is deliberate: `pg_stat_wal_receiver` is
  **empty** when the standby is disconnected, and a plain `SELECT` would return
  no rows — indistinguishable from a failed scrape. The join guarantees exactly
  one row always, carrying `status = 'disconnected'`, which is the information
  that matters most.

  **`replication_slots`** — `Requires{Roles: nil}` (both roles; a cascading
  standby has slots too), `DefaultInterval` 15s, `Timeout` 2s.
  ```sql
  SELECT slot_name, slot_type, active, COALESCE(active_pid, 0) AS active_pid,
         COALESCE(wal_status, 'unknown')                       AS wal_status,
         COALESCE(safe_wal_size, 0)                            AS safe_wal_size,
         pg_wal_lsn_diff(
           CASE WHEN pg_is_in_recovery()
                THEN pg_last_wal_replay_lsn()
                ELSE pg_current_wal_lsn() END,
           restart_lsn)::bigint                                AS retained_bytes
    FROM pg_replication_slots;
  ```
  The `CASE` on `pg_is_in_recovery()` is required: `pg_current_wal_lsn()` raises
  an error on a standby. Without it the check works in every test with one
  primary and fails the moment someone adds a cascading standby.

  **Security note:** `pg_stat_wal_receiver.conninfo` is not read. PostgreSQL
  obfuscates the password there, but the field still carries host, user and
  arbitrary options, and this project has no use for it — `sender_host` and
  `sender_port` are enough. Not reading a sensitive field is cheaper than
  redacting it correctly.
- **Unit tests:** row-parsing tests per check, driven by fixture rows: NULL lag columns emit NULL and not 0; an empty `pg_stat_replication` emits no rows and no error; a disconnected receiver emits one row with `status='disconnected'`; a slot with `wal_status='lost'` is parsed.
- **Integration tests:** these need the `primary-standby` topology, so `pgtest` gains a `PrimaryStandby()` helper building a standby with `pg_basebackup -R`.
  `INT-REPL-001` — on the primary, `replication_streaming` returns exactly one row whose `application_name` is `pg-standby`, with `sync_state='async'` and all three lag values non-NULL after the standby has replayed once.
  `INT-REPL-002` — on the standby, `replication_receiver` reports `status='streaming'` and `replay_lag_sec` **equal to 0** while the primary is idle. **This is the regression test for the `CASE` guard**; without it the value drifts upward and the test fails.
  `INT-REPL-003` — stopping the standby makes the receiver check report `status='disconnected'` in exactly one row rather than zero rows.
  `INT-REPL-004` — `replication_slots` on the primary reports the slot as active; after stopping the standby it reports inactive and `retained_bytes` grows.
  `INT-REPL-005` — `replication_slots` runs without error **on the standby**, proving the `pg_is_in_recovery()` guard.
  `INT-REPL-006` — after `pg_ctl promote`, `replication_streaming` becomes applicable on the former standby and `replication_receiver` stops being applicable, driven purely by `Requires().Supports`.
- **e2e tests:** `SYS-REPL-001`, `SYS-REPL-004`, `SYS-SLOT-001` in 6.4.
- **Done:** `make test-integration` green on 15 and 18 with the `primary-standby` fixture; `INT-REPL-002` passes; closed in `STATE.md`.

### 6.2 The topology engine

- **Model:** `agent-1:opus`
- **Assignment:** `agent-1:opus` — graph fusion and failover semantics. **`agent-1` review gate by construction:** this is where invariant I-1 is either honored or quietly broken.
- **Files:** `internal/topology/engine.go`, `internal/topology/provider.go`, `internal/topology/streaming.go`, `internal/topology/engine_test.go`
- **Change:** the server fuses the per-instance observations into one graph per
  cluster. Coverage gate 90%.
  ```go
  // Provider turns one instance's check results into topology facts. Only
  // `streaming` exists in this plan (decision D6); Aurora, Patroni and Citus
  // become additional providers without touching the engine or the schema.
  type Provider interface {
  	Name() string                                    // "streaming"
  	Applies(inst Observation) bool
  	Edges(inst Observation) []Edge
  }

  type Edge struct {
  	From, To   pgtype.InstanceID
  	Type       string // "streaming"
  	SyncState  string
  	Confidence string // "high" | "low"
  }

  type Engine struct{ /* per-cluster state, clock */ }

  // Apply folds one instance's observation into the cluster graph and returns
  // the events the transition produced.
  func (e *Engine) Apply(obs Observation) []Event
  ```

  **Edge resolution**, in priority order. The primary side is authoritative
  because it is the only side that knows `sync_state` and the three lag values:
  1. From the **primary's** `pg_stat_replication`: match each row to a known
     instance by `application_name` first, then by `client_addr` and port.
     Matched → `confidence: "high"`.
  2. From the **standby's** `pg_stat_wal_receiver`: match `sender_host` and
     `sender_port` to a known instance. Used to resolve the upstream when the
     primary-side match failed.
  3. A row that resolves to no known instance still produces an edge, with a
     synthetic endpoint and `confidence: "low"`. **Never drop it.** A standby the
     system cannot name is a more important fact than a tidy graph, and hiding it
     would be the failure mode this design was written to avoid.

  **Detections:**
  - **`failover_detected`** — an instance transitions `standby -> primary` while
    another instance in the same cluster held `primary` within the last 120
    seconds. Payload carries `old_primary`, `new_primary`, and whether the old
    primary was observed at the time (a clean switchover) or had gone silent (an
    unplanned failover).
  - **`role_change`** — any role transition that does not qualify as a failover,
    including the very first observation of an instance. Emitting `failover` for
    a first sighting would fire on every fresh install; the 120-second window and
    the requirement of a prior primary are what prevent it.
  - **`split_brain_detected`** — two or more instances in one cluster report
    `primary` in the same evaluation window, and this is not a promote already
    accounted for by a `failover_detected` in the last 30 seconds.
  - **`orphan_standby`** — an instance in role `standby` whose upstream cannot be
    resolved for more than 60 seconds.
  - **cascade** — an instance that is both the `To` of one edge and the `From` of
    another is marked `is_cascading` in the API. No event; it is a valid
    topology, not a fault.

  **The invariant, enforced here and asserted in tests:** none of these
  transitions may ever change an instance's `cluster_id`. The engine has no code
  path that writes `cluster_id`; that column is owned exclusively by sub-phase
  3.3 and rejected there when it changes. Stating it twice is deliberate — I-1
  is the plan's acceptance criterion.

  Edges are written to `topology_edges` with `updated_at`; an edge not refreshed
  for 3 intervals is deleted, so a graph never shows a replica that stopped
  existing.
- **Unit tests:** table-driven over fabricated observation sequences with a fake clock; no database.
  `TestEngine_FirstSightingIsRoleChangeNotFailover` — a fresh cluster's first primary emits `role_change`, never `failover_detected`.
  `TestEngine_PromoteEmitsFailover` — standby to primary with a recent prior primary emits exactly one `failover_detected` with correct `old_primary` and `new_primary`.
  `TestEngine_FailoverEmittedOnce` — ten further observations after the promote emit nothing more.
  `TestEngine_PromoteWithoutPriorPrimaryIsRoleChange` — no prior primary within the window gives `role_change`.
  `TestEngine_SplitBrain` — two simultaneous primaries emit `split_brain_detected`; the same shape within 30s of a failover does **not**, because that is the normal transient of a promote.
  `TestEngine_OrphanStandby` — an unresolvable upstream emits `orphan_standby` after 60s and not before.
  `TestEngine_LowConfidenceEdgeKept` — an unmatched `pg_stat_replication` row produces an edge with `confidence: "low"` rather than nothing.
  `TestEngine_EdgePriority` — when both sides report, the primary-side `sync_state` and lag values win.
  `TestEngine_Cascade` — a three-node chain marks the middle node cascading and emits no fault event.
  `TestEngine_StaleEdgeExpires` — an edge unrefreshed for 3 intervals is removed.
  `TestEngine_NeverWritesClusterID` — a structural assertion that no method mutates the field, plus a sequence test where a promote leaves it untouched. **Invariant I-1.**
- **Integration tests:** `INT-TOPO-001` — a real promote against the `primary-standby` fixture produces exactly one `failover_detected` and inverted roles in `instances`.
- **e2e tests:** `SYS-REPL-001` (acceptance), `SYS-REPL-002`, `SYS-REPL-003`.
- **Done:** `make test` and `make test-integration` green; `go tool cover -func` reports **>= 90%** for `internal/topology`; closed in `STATE.md`.

### 6.3 Replication API surface

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — extends the phase 3 read API.
- **Files:** `internal/server/api_topology.go`, `internal/server/it_api_topology_test.go`
- **Change:**
  - `GET /api/v1/clusters` gains, per cluster: `primary` (the current primary's
    `instance_id`, or `null`), `topology` (the edge list with `from`, `to`,
    `type`, `sync_state`, `confidence`), `max_replay_lag_seconds`,
    `standby_count`, `sync_standby_count`, and `health` — `ok`, `degraded`
    (a replica lagging or a slot inactive), or `critical` (no primary, or
    split-brain).
  - `GET /api/v1/clusters/{id}/topology` returns the graph plus the failover
    history from `events`.
  - `GET /api/v1/clusters/{id}/replication?from=&to=` returns the lag series per
    edge from `metrics_replication`, with **`null` for gaps** exactly as in
    sub-phase 3.5.
  - Every response carries `cluster_id` as a decimal **string** (D18).
  - An edge with `confidence: "low"` is flagged in the payload with a
    `note` field explaining that the endpoint could not be resolved, so a
    consumer renders uncertainty rather than presenting a guess as fact.
- **Unit tests:** handler tests with a fake store: health computation across the ok/degraded/critical matrix; `null` rendering for gaps; low-confidence note present.
- **Integration tests:** `INT-API-010` — after ingesting a primary-standby pair, `/clusters` reports one primary, one standby and one edge. `INT-API-011` — after a promote, `primary` points at the new instance and `/clusters/{id}/topology` lists the `failover_detected` event. `INT-API-012` — a lagging replica flips `health` to `degraded`. `INT-API-013` — an unresolvable edge is returned with `confidence: "low"` and a note.
- **e2e tests:** `SYS-REPL-001` asserts against these endpoints.
- **Done:** `make test-integration` green; the reference scenario's `curl` in `overview.md` returns the documented shape; closed in `STATE.md`.

### 6.4 Replication scenarios and the acceptance test

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — scenario implementation. **`agent-1:opus` review gate:** the acceptance assertions are what the whole plan is measured by, and a weak assertion here would make every earlier phase unfalsifiable.
- **Files:** `test/scenario/replication.go`, `test/e2e/replication_test.go`
- **Change:**

  | ID | Scenario | Assertion |
  |----|----------|-----------|
  | **`SYS-REPL-001`** | `pg_ctl promote` on `pg-standby` | **The acceptance test.** Capture `cluster_id` before the promote. After it: roles are inverted within 20s; exactly one `failover_detected` exists with the right `old_primary` and `new_primary`; `/clusters` reports `primary = pg-standby`; and **`cluster_id` is byte-identical to the value captured before** (invariant I-1). Additionally `Consistently` proves no `cluster_id_changed` event was emitted and no second `failover_detected` follows |
  | `SYS-REPL-002` | promote the standby **without** stopping the primary | `split_brain_detected` is emitted; `health` is `critical`; both instances report `primary`; the `cluster_id` is still unchanged |
  | `SYS-REPL-003` | stop the primary, leaving the standby unpromoted | `orphan_standby` after 60s; the edge becomes `confidence: "low"` or is expired; `no_primary_in_cluster` from sub-phase 3.6 fires exactly once |
  | `SYS-REPL-004` | `ALTER SYSTEM SET recovery_min_apply_delay = '30s'` on the standby, reload, then write on the primary | `replay_lag_sec` rises monotonically toward 30 and `health` becomes `degraded`. **Then remove the delay and assert it returns to 0** — the recovery half is what proves the `CASE` guard of 6.1 in a live system |
  | `SYS-SLOT-001` | stop the standby, keep the slot | the slot reports inactive; `slot_retained_bytes` grows monotonically; a `slot_inactive` event fires after the configured window (compressed to 30s in tests) |
  | `SYS-REPL-005` | promote, then restart the old primary as a standby of the new one | the graph reverses direction, the old `failover_detected` remains in history, and the `cluster_id` is still unchanged after two role reversals |

  `SYS-REPL-001` joins the **smoke set**; the rest run in `make test-e2e-full`.
  Every scenario ends with `AssertInvariants`.

  > `SYS-REPL-005` exists because a single failover is easy to get right by
  > accident. Two reversals in the same cluster is where an identity scheme that
  > merely appears to work falls apart.
- **Unit tests:** none (these are the tests).
- **e2e tests:** the six above.
- **Done:** `make test-e2e` (smoke, now including `SYS-REPL-001`) green for `AGENT_MODE={container,binary}`; `make test-e2e-full` green for all three; the reference scenario in `overview.md` reproduces exactly as written; closed in `STATE.md`.

### 6.5 Update README.md

Mandatory closing sub-phase of every phase. User guide only — no implementation
detail.

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — documentation; `agent-1:opus` reads it on the final phase.
- **Files:** `README.md`
- **Change:** this phase makes replication monitoring usable. Add:
  - **Monitoring a replicated cluster** — what the user must do (nothing beyond
    pointing the agent at each instance), how instances are grouped into a
    cluster automatically, and **why the `pg_control_system()` grant matters
    here specifically**: without it the grouping falls back to the configured
    `cluster_name` and a failover can split one cluster into two
  - **HTTP API** — the new endpoints with realistic `curl` output: the topology
    graph, the replication lag series, the failover history
  - **Events** — the list a user can act on: `failover_detected`,
    `split_brain_detected`, `orphan_standby`, `slot_inactive`,
    `no_primary_in_cluster`, `counter_reset_detected`, `agent_down`, with one
    line each on what it means and what to check
  - **Known limits** — only streaming replication is supported in this release;
    logical replication, Patroni and Aurora topologies are not detected; there is
    no alert delivery yet, events are available through the API only
- **Unit tests:** none (documentation).
- **e2e tests:** none — every `curl` was executed against a live promoted cluster and produced the documented output.
- **Done:** a user can follow the README to monitor a primary-standby pair and interpret a failover event without reading any source; no package or table name appears; all gates green; closed in `STATE.md` with the §11 docs row for phase 6 set.

---

## Phase gates

- **Fmt:** `make fmt-check`
- **Lint:** `make lint`
- **Test subset:** `make test`, `make test-integration`, `make test-e2e`
- **Coverage:** `make coverage-gate` — `internal/topology` now enforced at 90%
- **Regression guard:** every phase 0 to 5 gate still green; the phase 5 smoke set still passes unchanged
- **README:** documents replicated-cluster monitoring and the event list, free of implementation detail

## Phase done criterion

**The plan's acceptance criterion is met.** `SYS-REPL-001` performs a real
`pg_ctl promote`, and afterwards the API reports the roles inverted, exactly one
`failover_detected` event, and a `cluster_id` byte-identical to the one observed
before the promote — under `AGENT_MODE=container` and `AGENT_MODE=binary`.
`SYS-REPL-005` proves it survives two role reversals. `INT-REPL-002` proves
replay lag reads 0 on an idle primary rather than drifting upward. README.md
reflects this phase's shipped behavior, and `STATE.md` §11 shows phase 6 `DONE`
with every sub-phase closed.
