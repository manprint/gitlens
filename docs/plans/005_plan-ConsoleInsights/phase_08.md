# Phase 7 — HA migration of the live PostgreSQL and the pglens drills

Intent: the current Odoo PostgreSQL runs as one primary plus two hot standbys with automatic failover and a read-write/read-only endpoint pair; Odoo uses the read-write endpoint; pglens shows the three nodes as one cluster, follows switchover and hard failover, and every pglens gap found by the drills is implemented and re-drilled.
Prerequisites: 6 (deployed build, backup `03-pre-ha` verified), 10 (names for the nodes).
Phase closure: P7; review by `advisor`.

## State and ownership contract
Read STATE.md §0–1 first. Open before edits; checkpoint at a recovery boundary; close with evidence. Missing design goes to the `advisor`. Destructive steps need the verified backup first (I-4).

## Local design context
Plan revision: 1.
- D9 (revised, see overview): no external pulls are authorised, so the lab uses **only local images**: `postgres:18` (present) for the three nodes and a small controller `hactl` (Go, built from this repository with the existing dependencies, packaged `FROM scratch`) that provides the read-write endpoint, the read-only endpoint, health checking and automatic promotion. It is **lab-grade, not production** (documented). Patroni/etcd/HAProxy remain a recommended production topology and would need the user's explicit network authorisation (final report).
- Topology: nodes `odoo-db` (existing container and volume, becomes node 1 / initial primary), `pg-ha-2`, `pg-ha-3` (new volumes `ha-2-data`, `ha-3-data`, clones by `pg_basebackup -h odoo-db -U replicator -R -X stream -C -S ha_2`/`ha_3` run from one-off `postgres:18` containers); controller `hactl` (container `pglens-odoo-hactl-1`) listens on `:5432` (read-write → current primary), `:5433` (read-only → round-robin standbys, falls back to the primary when none), `:8008` (status JSON). Odoo's `HOST` points to `hactl:5432`; the pglens agent targets are the three nodes **directly** (target names equal the container names `odoo-db`, `pg-ha-2`, `pg-ha-3`; one DSN each, never the proxy) so topology is observed, not hidden. `odoo-db` keeps `host_local: true`; the two standbys are configured with `host_local: false` (their data volumes are not mounted into the agent), so RDY-HOST-01 is not raised for them. `commands.enabled` is already `true` in the stack `agent.yaml` and stays unchanged. The UI display names (`ha-1`, `ha-2`, `ha-3`, set through the rename feature in 7.3) are independent of these target/container names.
- Primary preparation (one restart of `odoo-db`, announced): `wal_level=replica` (default), `max_wal_senders=10`, `max_replication_slots=10`, `hot_standby=on`, `wal_log_hints=on` (needed by `pg_rewind`), `hot_standby_feedback=on` on standbys; role `replicator` (REPLICATION, password from `.env`), `pg_hba.conf` lines for the stack network (`host replication replicator <net> scram-sha-256`); the monitoring role already replicates to the clones.
- `hactl` contract: every 1 s probe each node (`pg_is_in_recovery()`, replay/current LSN, timeline) with a 800 ms timeout; leader = the only node reporting `pg_is_in_recovery() = false` **and** whose timeline equals the highest timeline ever seen (`maxTimeline`); `hactl` persists `{lastLeader, maxTimeline}` in a state file on the named volume `hactl-state`, so a controller restart can never make a stale former primary look like the leader — a writable node whose timeline is lower than `maxTimeline` is `diverged_or_stale` and is never routed; failover when the leader fails 5 consecutive probes and at least one standby is reachable: choose the standby with the highest replay LSN (ties: lowest node name), `SELECT pg_promote(true, 30)`, then `ALTER SYSTEM SET primary_conninfo` on the other standbys + `pg_reload_conf()` (applies on reload since PostgreSQL 13), update the proxies; a recovered old primary is **never** auto-rejoined (split-brain protection): its node status reads `diverged_or_stale` and `ha/rejoin.sh <node>` re-clones it; if two nodes claim primary, `hactl` refuses to route writes (read-only endpoint only) and reports `split_brain` in `/status`. Switchover is a host script (`ha/switchover.sh <target>`) with this order: (1) `POST :8008/block-writes` (the RW endpoint refuses new connections and drops existing ones); (2) read the final LSN with `pg_current_wal_lsn()` on the old primary; (3) fence it with `docker update --restart=no <old>` then `docker stop <old>` (never `pg_ctl stop`: the container restart policy would bring PostgreSQL back as a writable node); (4) wait until the target's `pg_last_wal_replay_lsn()` is ≥ the final LSN; (5) `pg_promote` the target and re-point the other standby; (6) `POST :8008/unblock-writes` (the RW endpoint now routes to the new leader); (7) re-add the old primary as a standby by `pg_rewind` or re-clone, restore `docker update --restart=unless-stopped` and start it. `/status` JSON: `{leader, timeline, max_timeline, writes_blocked, nodes:[{name, role, healthy, lag_bytes, lsn}], split_brain, since}`.
- R3 (local research, 7.0): verify on the `postgres:18` image that `pg_basebackup -R -X stream -C -S` creates a replica, `primary_conninfo` changes on reload, `pg_promote` works, `pg_rewind` prerequisites hold (`wal_log_hints`), timeline following without restart; record command outputs in STATE §7.
- Drill V-checklist (pass criteria in 7.4): V-1 one cluster, three instances; V-2 roles correct within 2 collection cycles after each transition; V-3 topology edges and per-standby write/flush/replay lag equal `pg_stat_replication`; V-4 promotion/timeline change recorded as an event; V-5 lag chart continues per standby across a promotion without mixing nodes; V-6 HA health findings (no standby, lag, inactive slot, diverged old primary, single standby); V-7 alert and notification when the primary is lost; V-8 per-node metrics never mixed (cluster aggregates labelled); V-9 agent reconnects after promotion; V-10 index/table analysis on a standby states that its statistics are the standby's own; V-11 commands (explain/cancel/terminate) on a standby behave as specified (refused or valid, never silently wrong); V-12 failover events reach Google Chat.
- I-2 (agent read-only), I-4, I-8 (no pulls, no push), I-13.

## Sub-phases

### 7.0 Design freeze and local research (R3)
- **Model:** agent-2:claude-opus-5-5
- **Assignment:** research and write; `advisor` reviews the design **before** 7.1.
- **Files:** READ `/home/agent/pglens-odoo-stack/{compose.yml,agent.yaml,.env,backup.sh,restore.sh}`, `internal/server/topology*.go`, `internal/server/replication*.go`, `docs/` replication sections, `web/src/features/cluster/*`. NEW `/home/agent/pglens-odoo-stack/ha/DESIGN.md`.
- **Change:**
  Preconditions: 10.2 done (the cluster tables this unit reads are migrated); 6.3 for the destructive steps (7.2).
  Contract: run the R3 experiments in throwaway containers (names `ha-research-*`, removed afterwards) on `postgres:18`; write `ha/DESIGN.md` with: topology, ports, network, volumes, parameter list, `hactl` contract, failure matrix (primary crash, standby crash, network partition between primary and controller, controller crash — Odoo loses connections and reconnects; documented), rollback (restore `03-pre-ha` onto the original single-node compose with `restore.sh`), and the pglens expectations per V-item with exact API fields to compare.
  Steps: S1 R3 experiments. S2 `DESIGN.md`. S3 mark R3 CONFIRMED in overview with the recorded outputs.
  Recovery boundary: none.
- **Unit tests:** N/A — research unit; evidence rows in STATE §7.
- **e2e tests:** N/A.
- **Done:** `DESIGN.md` complete, R3 CONFIRMED, `advisor` approval recorded, unit closed.

### 7.1 HA lab tooling (`hactl`, compose override, scripts)
- **Model:** agent-2:claude-opus-5-5
- **Assignment:** implement; `advisor` reviews the diff (failure semantics, split-brain refusal).
- **Files:** READ `DESIGN.md`, `internal/agent/conn.go` (pgx usage patterns), existing integration test helpers (`testcontainers` usage in `internal/**/_integration_test.go`). NEW `test/ha/hactl/{main.go,node.go,proxy.go,failover.go,state.go,status.go,Dockerfile}`, `*_test.go`, `test/ha/hactl/hactl_integration_test.go`, `/home/agent/pglens-odoo-stack/ha/{compose.ha.yml,build.sh,migrate.sh,rejoin.sh,switchover.sh,rollback.sh}`.
- **Change:**
  Preconditions: 7.0 done.
  Contract: as in the design context; the controller exposes pure, testable pieces: `electLeader(nodes) (leader string, splitBrain bool)`, `chooseCandidate(nodes) string`, `failureTracker` (consecutive-failure counter with injectable clock), `stateStore` (persisted `lastLeader`/`maxTimeline`, atomic write), `proxy` (TCP splice with connect-time routing; existing connections to a demoted primary are closed so clients reconnect); all network and PostgreSQL access behind interfaces for tests.
  Steps: S1 `electLeader` (timeline-aware), `chooseCandidate`, `failureTracker`, `stateStore` + tests. S2 node probing (`pgx`) + status JSON + tests with a fake prober. S3 TCP proxy + tests (loopback). S4 failover orchestration + tests with fakes (promote called once, reconfigure others, no promote when no standby, no promote under split brain). S5 `Dockerfile` (`FROM scratch`, static binary built on the host with `CGO_ENABLED=0 go build`, copied in; no base image is pulled) + `build.sh` (local build only). S6 integration test with 3 real `postgres:18` containers (local images): replicas bootstrap, kill the primary container, `hactl` promotes within 15 s, writes via the RW proxy succeed afterwards, the old primary is not auto-rejoined. S7 compose override and scripts (idempotent; each script prints what it will do and refuses to run unless a verified backup exists: a `VERIFIED` file whose creation time is < 24 h old, test-strategy.md guard rule).
  Recovery boundary: after S4.
  Failure handling: a PostgreSQL behaviour differing from DESIGN.md → `advisor`, update DESIGN.md first.
- **Unit tests:** HACTL-001 one writable node → leader; -002 two writable nodes → `splitBrain`, no leader; -003 candidate = highest replay LSN, tie → lowest name; -004 no reachable standby → no candidate; -005 tracker needs 5 consecutive failures, a success resets; -006 promote not repeated while a promotion is in flight; -007 `/status` JSON golden (incl. `max_timeline`, `writes_blocked`); -008 proxy routes to the leader and drops connections to a demoted node; -009 read-only proxy round-robins standbys and falls back to the primary; -010 status marks a recovered old primary `diverged_or_stale`, never routes to it; -011 `maxTimeline` survives a controller restart: a writable node with a lower timeline is stale even when it is the only writable one reachable; -012 `block-writes` drops RW connections and refuses new ones, `unblock-writes` restores routing to the new leader. Command: `go test -race -shuffle=on -count=1 ./test/ha/hactl`.
- **e2e tests:** HACTL-INT-001 (integration, `-tags integration`, Docker): the S6 scenario with timing assertions (promotion ≤ 15 s after the kill; RW writes succeed; RO endpoint serves the remaining standby; the standby follows the new timeline). Command: `go test -tags=integration -race -count=1 -timeout=10m ./test/ha/hactl -run HACTLINT`.
- **Done:** tests pass, binary builds, scripts shellcheck-clean (`bash -n`), `advisor` review recorded, unit closed.

### 7.2 In-place migration of the live stack
- **Model:** agent-2:claude-opus-5-5
- **Assignment:** execute; `advisor` reviews the evidence (I-4) before the migration step and after.
- **Files:** READ `DESIGN.md`, `ha/migrate.sh`, backup evidence from 6.3. WRITE the live stack (`compose.yml`/override, `agent.yaml`), `/home/agent/pglens-odoo-stack/README.md`, `web/e2e/qa/{stack.ts,pg.ts}` (HA-aware, see S4b). Also WRITE `web/e2e/ha.spec.ts` (`LIVE-HA-001`, the unit's live spec; titles start with the test ID).
- **Change:**
  Preconditions: 7.1 done; a verified backup younger than 24 h exists (`VERIFIED` file; when `03-pre-ha` is older than 24 h take a fresh one first with `backup.sh 03b-pre-ha` and verify it — re-running `verify_backup.sh` on an old dump does not make it newer); Odoo idle.
  Contract: execute `ha/migrate.sh` stepwise: prepare the primary (parameters, role, `pg_hba`, one restart of `odoo-db`), clone the two standbys, start `hactl`, switch Odoo to `hactl:5432` (recreate only the `odoo` container), switch the pglens agent to three direct targets (restart `pglens-agent`), verify counts (`res_partner`, `product_template`, `sale_order`, `mail_message` equal on all three nodes after replay catches up), run an Odoo RPC smoke (login, create+delete a partner through the RW endpoint), confirm `pg_stat_replication` shows two streaming standbys.
  Steps: S1 pre-checks. S2 prepare primary. S3 clone standbys. S4 start `hactl` + repoint Odoo. S4b make the QA harness HA-aware: `stack.ts` passes both compose files (`-f compose.yml -f ha/compose.ha.yml`) and `stackStatus()`/G-STACK also cover `pg-ha-2`, `pg-ha-3` and `hactl`; `pg.ts` resolves the current leader from `hactl` `GET :8008/status` (port published by the override) and runs its writes with `docker exec <leader-container> psql`, so stimulus and `QA-CLEAN-001` keep working after a failover. S5 repoint agent (three direct targets, standbys `host_local: false`). S6 verification checks. S7 record evidence (commands, outputs, durations) in STATE §7.
  Recovery boundary: after S2 and after S4 (record node states; `rollback.sh` restores the single-node compose and the volume from the newest verified pre-HA backup (`03-pre-ha` or `03b-pre-ha`) if a step cannot be completed).
  Failure handling: any failed verification → stop, record, decide with the `advisor` between fix-forward and rollback; never continue with unequal counts.
- **Unit tests:** N/A — operational unit (assertions are the script checks and S6 counts).
- **e2e tests:** `LIVE-HA-001` (`web/e2e/ha.spec.ts`): UI shows three instances, one cluster; `GET /api/v1/instances` roles `primary`/`standby`; Odoo page loads through the new endpoint. `LIVE-HA-001` calls `test.setTimeout(liveTimeout(0, 60_000))`.
- **Done:** topology verified, Odoo works, row counts equal, evidence recorded, unit closed.

### 7.3 pglens topology verification (static state)
- **Model:** agent-2:claude-opus-5-5
- **Assignment:** verify; record gaps; `advisor` reviews the gap list.
- **Files:** READ cluster/topology UI and API files, `internal/server/topology*.go`, `internal/advisor` replication rules, `web/e2e/ha.spec.ts`. WRITE `web/e2e/ha.spec.ts` (`HA-STATIC-001..`), `docs/plans/005_plan-ConsoleInsights/ha-findings.md` (gap register).
- **Change:**
  Preconditions: 7.2 done; two collection cycles elapsed.
  Contract: evaluate V-1, V-3, V-8, V-10 (and V-2 for the initial roles) on the live topology: node names set through the rename feature (`ha-1`, `ha-2`, `ha-3`), topology edges, lag values vs `pg_stat_replication`, per-node charts, the standby analysis notice. Each failing V-item becomes a row in `ha-findings.md` (id, observation, expected, evidence, proposed unit 7.5.<n>).
  Steps: S1 write `HA-STATIC-*` assertions (red first where behaviour is missing). S2 run. S3 record gaps.
  Recovery boundary: none.
- **Unit tests:** N/A — live verification (the assertions are the tests).
- **e2e tests:** HA-STATIC-001 V-1 (one cluster, three instances, names), -002 V-2 roles, -003 V-3 edges + lag vs PostgreSQL (±1 MiB or ±10 %) and the cluster lag section (`/clusters/<id>?section=lag`, the tab added by 15.3) draws a non-blank canvas (the live spec of 1.4 branches on the instance count and covers the chart here), -004 V-8 charts per node and labelled aggregate, -005 V-10 standby notice text; `-g HA-STATIC`; each HA-STATIC test calls `test.setTimeout(liveTimeout(0, 90_000))`.
- **Done:** every assertion recorded pass or turned into a gap row; unit closed.

### 7.4 Drills: switchover, hard failover, standby loss, rejoin
- **Model:** agent-2:claude-opus-5-5
- **Assignment:** run; `advisor` reviews the observations against V-1…V-12.
- **Files:** READ `ha-findings.md`, `ha/*.sh`, `web/e2e/{ha.spec.ts,qa/stack.ts,qa/pg.ts}`. WRITE `web/e2e/ha.spec.ts` (`HA-DRILL-001..`), `ha-findings.md`.
- **Change:**
  Preconditions: 7.3 done; a `VERIFIED` backup younger than 24 h exists; Odoo load script running (`rpc_load.py`) to prove continuity; `PGLENS_QA_ALLOW_LIFECYCLE=1` and `PGLENS_QA_ALLOW_STIMULUS=1` set (the drills stop containers).
  Contract: each drill is a Playwright test driving `ha/*.sh` or `docker kill`, observing the UI, the pglens API and PostgreSQL (three-way): D1 switchover to `pg-ha-2` (V-2, V-4, V-5, V-9, V-12); D2 hard failover (`docker kill` the primary; V-2, V-4, V-5, V-6, V-7, V-9, V-12; Odoo RPC errors bounded to ≤ 30 s then recover); D3 standby loss and return (V-3, V-6; with `rpc_load.py` running, each standby's write/flush/replay lag from `/replication` is compared with `pg_stat_replication` within ±1 s or ±10 %, at least one sample above the tolerance, closing ha-findings O-2); D4 rejoin the old primary with `rejoin.sh` (V-6 diverged finding clears, V-2); D5 commands on a standby (V-11). Between drills `hactl /status` must be healthy and `QA-CLEAN-001` pass.
  Steps: S1 D1. S2 D2. S3 D3. S4 D4. S5 D5. S6 consolidate findings into `ha-findings.md`; classify each as OK or GAP.
  Recovery boundary: before D2 (record node roles; recovery = `rejoin.sh` + `hactl` status).
  Failure handling: a drill leaves the topology unhealthy → restore it (rejoin/re-clone) before continuing; unrecoverable → `rollback.sh` from `03-pre-ha`.
- **Unit tests:** N/A — drills.
- **e2e tests:** HA-DRILL-001..005 as above (`-g HA-DRILL`, discovery 5, serial). Timeouts: each drill `test.setTimeout(liveTimeout(30_000, 15_000 + 30_000 + 2 * 90_000))` (promotion ≤ 15 s, Odoo RPC recovery ≤ 30 s, two collection cycles, plus the stimulus); the rejoin drill (the demoted old primary rejoining as standby) `liveTimeout(0, 600_000)`.
- **Done:** all five drills executed with recorded observations; the gap register is final; unit closed.

### 7.5 Close every gap (correction units 7.5.<n>)
- **Model:** agent-2:claude-opus-5-5
- **Assignment:** implement one unit per gap in `ha-findings.md`; `advisor` reviews each design before code (new behaviour) and the diff (agent/server contract).
- **Files:** per gap (named in the row): typical sites `internal/advisor` rules (HA health findings V-6), `internal/server` events/topology (V-4, V-5), `internal/agent` reconnect (V-9), `web/src/features/cluster` and index analysis (V-10), `internal/agent` command refusal on standby (V-11), `internal/alert` events (V-12).
- **Change:**
  Preconditions: 7.4 done; the gap row exists.
  Contract: each gap unit is written into this file by the `advisor`'s design (a new sub-section `7.5.<n>` with the seven fields) **before** implementation — the register defines what is missing, the unit defines the design and the tests; none is skipped: a gap closed only by a re-drill that passes the V-item.
  Steps (per unit): S1 failing test at the lowest layer (Go/vitest). S2 implementation. S3 docs/README. S4 re-drill the relevant V-item live and record.
  Recovery boundary: per unit.
  Failure handling: a gap that needs a product decision (e.g. whether pglens should offer promote) → ask the user at the end; record and mark the V-item `DEFERRED-USER` with the reason.
- **Unit tests:** per gap (IDs `HA-<V-id>-n`).
- **e2e tests:** the corresponding `HA-DRILL-*` is rerun.
- **Done:** every gap row FIXED with evidence (or `DEFERRED-USER` with the question recorded), all V-items pass in a final full re-drill, unit closed.

## Phase gates and closure
- Required gates: G-GO-FOCUS (`./test/ha/hactl` + packages touched by 7.5), G-GO-INT (`HACTLINT` + touched), G-RO (if the agent changed), G-API (if the contract changed), G-WEB-STATIC/UNIT (if the UI changed), G-DEPLOY, G-HA (the STATE §3 command: `--project=desktop-fullhd e2e/ha.spec.ts -g 'LIVE-HA|HA-STATIC|HA-DRILL'` with `PGLENS_QA_ALLOW_STIMULUS=1 PGLENS_QA_ALLOW_LIFECYCLE=1` and the live environment), G-QA (`QA-CLEAN-001`).
- Re-proof (9.4, STATE §8): with the HA stack up, run `-g 'TBL-E2E-005'`; its `/clusters/<id>` view must hold at least one data table (edges, slots or drift) and carry no `UNVERIFIED` annotation.
- README obligation: `README.md` (replication/topology section: what pglens shows for HA, the lab topology and its limits), `docs/` HA notes, stack README (`ha/`).
- Every sub-phase DONE (7.5 units each DONE). `advisor` reviews drill evidence against the V-checklist and the rollback readiness. Open P7, record, mark DONE. Commit per STATE §3.
