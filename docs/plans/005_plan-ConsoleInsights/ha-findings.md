# HA findings (plan 005, phase 7)

Gap register for the HA verification. 7.3 records the static state; 7.4 appends drill
findings; each gap is fixed by the 7.5 unit named in its row.

## Static state (7.3, 2026-11-06, HA-STATIC-001..005)

Topology: leader `odoo-db` (renamed `ha-1`), standbys `pg-ha-2` (`ha-2`) and `pg-ha-3`
(`ha-3`), async streaming, timeline 1. Run: `-g HA-STATIC`, 4 passed, 1 failed (005).
Oracles strengthened after review R-7.3 (fresh Opus review agent, FIX-FIRST twice; every finding applied).

| V | Test | Result |
|---|------|--------|
| V-1 | HA-STATIC-001 | pass: one cluster, `instance_count` 3, addrs {odoo-db, pg-ha-2, pg-ha-3}; names cleared, then set through Settings → Names (rename dialog) each run; names in the instance header and on the cluster topology page |
| V-2 | HA-STATIC-002 | pass: `pg_is_in_recovery()` is `t`/`f` on each node and equals the API role and the instance header Role badge; primary addr equals the hactl leader |
| V-3 | HA-STATIC-003 | pass: API topology 2 edges standby to primary, confidence high; topology edge table shows `ha-2`/`ha-3` to `ha-1`; while the primary emits WAL-only messages (`pg_logical_emit_message`, no table data; idle PostgreSQL reports NULL lag) every standby has a non-null write/flush/replay lag sample in pglens, and the latest is within ±1 s or ±10 % of `pg_stat_replication`; lag section has one article per edge (`ha-N → ha-1 (async)`) with a non-blank canvas |
| V-8 | HA-STATIC-004 | pass: per node, the tables API `n_live_tup` sum equals that node's own `pg_stat_user_tables` (±1 % or 10 rows); the primary's sum exceeds max(10, 1 %) and each standby differs from it by more than that; 3000 read-only commits on one standby raise only its own `pg_xact_commit_total` rate (> 50/s and > 5× the other nodes); each node page shows its own name and a non-blank TPS chart; `max_replay_lag_seconds` is not null and the Fleet card "Max replay lag" equals `formatLag` of it |
| V-10 | HA-STATIC-005 | FAIL: GAP-1, GAP-2; pass after 7.5.1 (2026-11-06 20:17 UTC) |

## Drills (7.4, 2026-11-06, HA-DRILL-001..005)

Fresh VERIFIED backup `03b-pre-ha-20261106T190522Z` first; I-4 pre-review (fresh Opus agent)
FIX-FIRST, its 3 blockers fixed (cleanup in `afterEach`, a clean alert episode before each
drill, test budgets = sum of the poll timeouts). Run one drill at a time with `-g`, harness
`QA-CLEAN-001` green before each. Order D1, D2, D4, D3, D5 (D4 brings back the D2 victim, so
D3 and D5 start from three healthy nodes). Every drill ended with hactl idle, one leader, 3
nodes healthy, 2 standbys streaming and pglens roles equal (hard check).
After the I-4 post-review (R-7.4, FIX-FIRST on B) the oracles were strengthened and every
drill re-run 19:44–20:05 UTC (logs `scratchpad/74r_*`): D1 2.1 min, D2 2.7 min, D4 2.3 min,
D5 9 s green; D3 red only on GAP-4. `rpc_load.py` runs through D1, D2, D4 and D3 (restarted
after an outage: it exits on its first failed RPC; failed runs D1 1, D2 5, D4 0); the new
leader's console Role badge reads `primary`; D4 needs the `split_brain_detected` event and
alert, the alert resolved after the rejoin and a `role_change` of the rejoined node; D2 needs
the `instance_unreachable` Chat delivery; D3 compares the lag of every standby.

| Drill | V | Result |
|-------|---|--------|
| D1 switchover (HA-DRILL-001) | V-2, V-4, V-5, V-7, V-9, V-12 | pass (1.8 min; run 1 odoo-db→pg-ha-2 red only on a harness oracle, the Odoo probe stopped after 2 calls because the switchover took ~1 s, so the probe now runs ≥ 20 s; run 2 pg-ha-2→pg-ha-3 green). `failover_detected` {old, new} 4 s after the switchover, alert critical, Google Chat `fire` ok 12–16 s later; roles = `pg_is_in_recovery()` and last_seen advances within 2 cycles; one lag series per (standby, primary, metric), new edges to the new leader; Odoo RPC gap ≤ 30 s |
| D2 hard failover (HA-DRILL-002) | V-2, V-4, V-5, V-7, V-9, V-12 pass; V-6 GAP-5 | pass (3.3 min): `docker kill` of leader pg-ha-3; hactl promoted odoo-db (timeline 4) within 15 s; `failover_detected` {old pg-ha-3, new odoo-db}, Chat ok; `agent_down` and `instance_unreachable` for pg-ha-3 ~80 s after the kill, Chat ok; cluster health `degraded` only because an instance is down (no redundancy finding: GAP-5); lag series only from the surviving standby; Odoo RPC gap ≤ 30 s. See O-4 |
| D4 old primary returns (HA-DRILL-004) | V-2, V-6 | pass (29 s): the killed old primary started by hand on purpose (DESIGN §6); hactl marked it `diverged_or_stale` and never routed to it; pglens `split_brain_detected` {primary_count 2} + alert critical within 10 s, resolved after `rejoin.sh` (pg_rewind); `role_change` primary→standby; health `ok`; `counter_reset_detected` events for the rewound node's counters; Odoo RPC gap ≤ 5 s |
| D3 standby loss and return (HA-DRILL-003) | V-3, V-6 (slot), V-7 pass; V-3 topology GAP-4; V-6 single standby GAP-5 | FAIL: GAP-4. Pass: `docker stop` of pg-ha-3; its slot `ha_3` inactive in `/slots` and `slot_inactive` event; `agent_down` + `instance_unreachable` event and alert; health not ok (instance down; GAP-5); `rejoin.sh` back to healthy. Lag under load (closes O-2): with `rpc_load.py` running, `recovery_min_apply_delay = 3s` on the standby and a commit with an xid every 0.2 s on the leader, pglens write/flush/replay lag of the standby is within ±1 s or ±10 % of `pg_stat_replication`, replay lag > 1 s (above the tolerance); after 7.5.3 (20:53 UTC): pass, GAP-4 hard, byte lag > 0 on the delayed standby, edge `high` again after `rejoin.sh` (O-9) |
| D5 commands on a standby (HA-DRILL-005) | V-11 PARTIAL | pass (9 s), refusal path only: `explain` of a SELECT the standby ran → `done` with a plan; `cancel` and `terminate` of a live `pg_sleep` session on the standby → audit `rejected` ("permission tier T2 is required", instance tier T1) and the session still alive; audit has `requested` + outcome rows. Refused or valid, never silently wrong; the refusal comes from the T1 tier, not from the standby role, so cancel/terminate executed on a standby is not observed: GAP-8 |

## Gaps

| ID | V | Observation | Expected | Evidence | Unit |
|----|---|-------------|----------|----------|------|
| GAP-1 | V-10 | On a standby, the instance "Tables and indexes" section (`/instances/<standby>?section=relations`, `RelationsSection.tsx`) shows no notice about whose statistics these are; the section does not receive the role. | On a standby, the section shows a note (`role="note"`) saying the statistics are this standby's own (scans and counters are per server, not the primary's); none on a primary. | HA-STATIC-005 soft assertion "GAP-1 standby note on Tables and indexes": element not found; `test/e2e/_artifacts/ui/results/ha-HA-STATIC-HA-STATIC-005-*/test-failed-1.png` | 7.5.1 FIXED: HA-STATIC-005 hard assertions pass live 20:17 UTC |
| GAP-2 | V-10 | Index analysis (`/indexes?instance=<standby>`, tabs Indexes, Suggestions, Dead rows, Index tax) has no standby note. Cleanup shows "This server is part of a cluster…" (`CleanupPanel.tsx`), but on every cluster member, primary included, and it does not say the statistics are the standby's own. | On a standby, index analysis shows the same standby note on every tab. | HA-STATIC-005 soft assertions "GAP-2 standby note on /indexes <tab>" for `indexes`, `suggestions`, `dead-rows`, `tax`: element not found | 7.5.1 FIXED: one note on all five tabs, none on the primary, HA-STATIC-005 pass 20:17 UTC |
| GAP-3 | V-3 | Byte lag is collected (`internal/check/replication_streaming.go`) and interpreted (`internal/server/pipeline.go`) but served by no API (`/replication` gives seconds only; `/metrics/query` returns nulls for `replication_replay_lag_bytes`) and not shown. On an idle cluster PostgreSQL reports NULL time lag, so byte lag is the only non-null lag. | The replication API and the lag section serve per-standby replay byte lag; the phase 8 contract (±1 MiB or ±10 %) can be checked against `pg_wal_lsn_diff`. | Probe 2026-11-06 18:3x UTC: `/metrics/query` nulls; `/replication` edges metrics `write_lag_sec`, `flush_lag_sec`, `replay_lag_sec` only | 7.5.2 FIXED: fourth series `replay_lag_bytes`, Lag section "Replay lag (bytes)"; HA-STATIC-003 byte oracle pass 20:30 UTC |
| GAP-4 | V-3, V-6 | A standby that stopped keeps its topology edge at confidence `high` in `/clusters/{id}/topology` (and the topology graph) while the instance already reads `up=false`: `purgeTopologyEdges` drops an instance's edges only after `topologyEdgeLiveness` = 1 h (`internal/server/staleness.go`). | Edges from an instance that is not up (silent past `InstanceStaleAfter`) are not served as live `high` edges: omitted or marked stale, and the graph shows the standby detached. | HA-DRILL-003 soft assertion "D3 V-3 the lost standby has no high-confidence edge": `true` ~100 s after `docker stop` | 7.5.3 FIXED: silent reporter served `low` + note in `/topology` and `/clusters`; HA-DRILL-003 hard assertion pass 20:53 UTC |
| GAP-5 | V-6 | With one standby left (D2) pglens raises no redundancy finding: health reads `degraded` only because an instance is down, and nothing says the cluster has a single standby (expected gap, DESIGN §8). | An advisor finding or alert when a cluster that had ≥ 2 standbys streams to fewer (single standby, no standby), cleared when they return. | HA-DRILL-002 and -003: only `agent_down`, `instance_unreachable` (and `failover_detected` in D2); `health` comes from `healthForCluster` (`internal/server/api.go`: down instance → degraded); no redundancy rule in `internal/alert/builtin.go` or `internal/advisor`; DESIGN §8 V-6 names `replica.lag_high`, which is no builtin | 7.5.4 |
| GAP-6 | V-6 (lab) | After a switchover the demoted node keeps the physical slots it held as primary (`ha_1`, `ha_3` on pg-ha-2 after D1), inactive, retaining WAL on a standby; pglens rightly fires `slot_inactive` until they are dropped. `switchover.sh`/hactl drop no slot on demotion; `EnsureSlot` (`test/ha/hactl/node.go`) only creates slots and drops `lost` ones. pg-ha-2 (clean switchover, then streaming) kept them; pg-ha-3 (crash, `rejoin.sh` with pg_rewind) did not. | Lab: `rejoin.sh` and the switchover path drop the node's inactive slots once it streams from the new leader (hactl `EnsureSlot` recreates them on a later promotion). pglens behaviour is correct. | `pg_replication_slots` on pg-ha-2 after D1: `ha_3:false`, `ha_1:false`; alert `slot_inactive` on pg-ha-2 firing from 19:16:39; dropped by hand after the drills | 7.5.5 |
| GAP-7 | V-7, V-12 | A second outage of the same instance (or a second failover in the cluster) while the alert episode of the first is still open (2 min lookback, `internal/alert/engine.go`) is merged into that episode: no new `started_at`, no new notification. Observed: pg-ha-3 down again at 19:22:40 (D3 run 1), merged into the D2 episode 19:19:25→19:26:25 although the node was back at ~19:20:30. | A new outage after the instance came back (or a new failover) opens a new episode and notifies, or the merge is a documented product choice. | `notifications` rows: one `instance_unreachable` fire for pg-ha-3 between 19:19 and 19:27; events 19:19:24 and 19:22:39 | 7.5.6 (may be `DEFERRED-USER`: episode semantics) |
| GAP-8 | V-11 | Commands on a standby observed only on the refusal path: the instances are tier T1, so `cancel`/`terminate` are refused for the tier ("permission tier T2 is required") before any standby behaviour; `explain` ran and returned a plan. | With the monitoring role granted `pg_signal_backend` (T2), `cancel`/`terminate` of a standby session either work on the standby (session gone) or are refused with a standby reason; never accepted and silently ineffective. | HA-DRILL-005 audit rows `rejected` with the tier detail | 7.5.7 (re-drill D5 at T2) |
| GAP-9 | V-12 | Full HD fit under HA data: FHD-FIT-001 (live, 20:30 and 20:33 UTC, after the 7.5.2 deploy) reports a page scrollbar on `/indexes?tab=cleanup`, `/clusters/:id?section=events`, `/events`, `/settings?view=databases` at 1920×950 and `/settings?tab=audit` at 1920×1080. Three instances stack three database cards; the drills filled the event pages (20 rows of 100) and the audit. Lag route passes. | Every fit route fits at 1920×950 and 1920×1080 with three instances and full event pages. | `scratchpad/752_fhdfit.log`, `752_fhdfit2.log`; `test/e2e/_artifacts/ui/fullhd/events-950.png`, `settings-view-databases-950.png` | 7.5.8 |

## Observations (not gaps)

- O-1 The 7.3 contract (`phase_08.md`) says lag "±1 MiB or ±10 %"; DESIGN V-3
  (`ha/DESIGN.md`) fixes seconds ±1 s or ±10 %. HA-STATIC-003 follows DESIGN for seconds and,
  since 7.5.2 (GAP-3 FIXED), also checks replay byte lag ±1 MiB or ±10 %.
- O-2 The static comparison runs on an idle cluster (time lags near 0, PostgreSQL NULL when
  caught up, counted as 0). Lag under load is asserted by drill D3 (7.4): with `rpc_load.py`
  running, each standby's lag from the API is compared with `pg_stat_replication`, and at least one sample must exceed the tolerance (else the comparison is vacuous).
- O-3 Lab stack only (R-7.2): the live `ha-2-data`/`ha-3-data` volumes were created before
  `compose create`, so compose warns "volume … not created by Docker Compose". Harmless;
  `migrate.sh` now runs `compose create` first, so a fresh migration labels them.
- O-4 A stopped instance keeps its last role in `/clusters` (after D2 the killed pg-ha-3 reads
  `role: primary, up: false` next to the new primary). The cluster's `primary` field and
  health are right; the console shows the instance as down. Kept as last known role.
- O-5 Idle PostgreSQL drops replay lag to ~0 even with `recovery_min_apply_delay`: the delay
  applies to commit records only, and later non-commit WAL replays at once. A drill that
  needs a steady lag must commit (with an xid) faster than the delay.
- O-6 Moved to GAP-7 (review R-7.4): a second outage inside an open alert episode is
  merged into it. The drills wait for a clean episode (`expectNoFiring`) so each one
  observes its own `started_at` and notification; GAP-7 covers the merged case.
- O-7 DESIGN §8 V-7 expects `instance_unreachable` "~20 s" after the loss; pglens raises it
  after `InstanceStaleAfter` = 3 push cycles (90 s) plus evaluation, observed 80–100 s
  (`internal/store/liveness.go`). The drills wait `STALE_WAIT` (150 s). DESIGN wording, not
  a defect.
- O-8 Not drilled live in 7.4 (outside the D1–D5 contract): no-candidate failover
  (`no_primary_in_cluster`), network partition (`/fault/isolate`), hactl crash and restart.
  hactl unit and integration tests (HACTL-*) cover the controller side; the 7.5 final
  re-drill adds them if a gap unit touches those paths.
- O-9 7.5.3 HA-DRILL-003 (20:41–20:46 UTC): after `rejoin.sh` the agent reconnected to pg-ha-3 at
  ~20:43:45 and the instance read `up`, but its `replication_receiver` rows resumed only at
  20:45:46 (gap 20:41:35–20:45:46); until then the standby reported no edge, so `/topology`
  listed none for it (the edge came back `high` at 20:46:31). The topology is briefly
  incomplete after a return, never wrong. The post-rejoin "edge high again" poll waits
  180 s. Candidate for the final re-drill if a later unit touches the agent's check
  scheduling after a reconnect.
