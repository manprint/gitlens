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
| V-10 | HA-STATIC-005 | FAIL: GAP-1, GAP-2 |

## Gaps

| ID | V | Observation | Expected | Evidence | Unit |
|----|---|-------------|----------|----------|------|
| GAP-1 | V-10 | On a standby, the instance "Tables and indexes" section (`/instances/<standby>?section=relations`, `RelationsSection.tsx`) shows no notice about whose statistics these are; the section does not receive the role. | On a standby, the section shows a note (`role="note"`) saying the statistics are this standby's own (scans and counters are per server, not the primary's); none on a primary. | HA-STATIC-005 soft assertion "GAP-1 standby note on Tables and indexes": element not found; `test/e2e/_artifacts/ui/results/ha-HA-STATIC-HA-STATIC-005-*/test-failed-1.png` | 7.5.1 |
| GAP-2 | V-10 | Index analysis (`/indexes?instance=<standby>`, tabs Indexes, Suggestions, Dead rows, Index tax) has no standby note. Cleanup shows "This server is part of a cluster…" (`CleanupPanel.tsx`), but on every cluster member, primary included, and it does not say the statistics are the standby's own. | On a standby, index analysis shows the same standby note on every tab. | HA-STATIC-005 soft assertions "GAP-2 standby note on /indexes <tab>" for `indexes`, `suggestions`, `dead-rows`, `tax`: element not found | 7.5.1 |
| GAP-3 | V-3 | Byte lag is collected (`internal/check/replication_streaming.go`) and interpreted (`internal/server/pipeline.go`) but served by no API (`/replication` gives seconds only; `/metrics/query` returns nulls for `replication_replay_lag_bytes`) and not shown. On an idle cluster PostgreSQL reports NULL time lag, so byte lag is the only non-null lag. | The replication API and the lag section serve per-standby replay byte lag; the phase 8 contract (±1 MiB or ±10 %) can be checked against `pg_wal_lsn_diff`. | Probe 2026-11-06 18:3x UTC: `/metrics/query` nulls; `/replication` edges metrics `write_lag_sec`, `flush_lag_sec`, `replay_lag_sec` only | 7.5.2 |

## Observations (not gaps)

- O-1 The 7.3 contract (`phase_08.md`) says lag "±1 MiB or ±10 %"; DESIGN V-3
  (`ha/DESIGN.md`) fixes seconds ±1 s or ±10 %. HA-STATIC-003 follows DESIGN because the API
  serves no byte lag (GAP-3).
- O-2 The static comparison runs on an idle cluster (time lags near 0, PostgreSQL NULL when
  caught up, counted as 0). Lag under load is asserted by drill D3 (7.4): with `rpc_load.py`
  running, each standby's lag from the API is compared with `pg_stat_replication`, and at least one sample must exceed the tolerance (else the comparison is vacuous).
- O-3 Lab stack only (R-7.2): the live `ha-2-data`/`ha-3-data` volumes were created before
  `compose create`, so compose warns "volume … not created by Docker Compose". Harmless;
  `migrate.sh` now runs `compose create` first, so a fresh migration labels them.
