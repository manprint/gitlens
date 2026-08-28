//go:build e2e

package scenario

import (
	"context"
	"fmt"
	"time"
)

func init() {
	Register(Scenario{
		ID:       "SYS-REPL-001",
		Title:    "pg_ctl promote: the plan's acceptance test",
		Topology: TopologyPrimaryStandby,
		Covers:   []string{"phase_07.md#6.4", "I-1"},
		Smoke:    true,
		Expect: Expectations{
			Events:     []string{"failover_detected"},
			Invariants: []string{"I-1", "I-2", "I-3", "I-4"},
		},
		Run: func(ctx context.Context, e *Env) error {
			var primaryID, standbyID, clusterID string
			resolveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := poll(resolveCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				rows, err := e.DB.Query(ctx, `SELECT instance_id::text, role, cluster_id::text FROM instances`)
				if err != nil {
					return false, err
				}
				defer rows.Close()
				primaryID, standbyID, clusterID = "", "", ""
				for rows.Next() {
					var id, role, cid string
					if err := rows.Scan(&id, &role, &cid); err != nil {
						return false, err
					}
					clusterID = cid
					switch role {
					case "primary":
						primaryID = id
					case "standby":
						standbyID = id
					}
				}
				return primaryID != "" && standbyID != "", nil
			}); err != nil {
				return fmt.Errorf("resolve primary+standby instances: %w", err)
			}
			if clusterID == "" {
				return fmt.Errorf("resolved instances but no cluster_id")
			}

			// A real failover starts with the primary going away — SYS-REPL-002
			// is specifically the negative case of promoting *without* this
			// step, which is what produces split-brain instead of a clean
			// failover; leaving the old primary running here would make
			// "/clusters reports primary = pg-standby" ambiguous (both
			// instances legitimately claiming to be primary at once).
			if err := e.Compose("kill", "-s", "SIGKILL", "pg-primary"); err != nil {
				return fmt.Errorf("kill pg-primary: %w", err)
			}

			// `pg_ctl promote` refuses to run as root (the container's
			// default exec user) — must run as the postgres user that
			// actually owns the running server process.
			if _, err := e.ExecAs("pg-standby", "postgres", "pg_ctl", "promote", "-D", "/var/lib/postgresql/data"); err != nil {
				return fmt.Errorf("pg_ctl promote: %w", err)
			}
			promoteTS := time.Now().UTC()

			// Roles inverted within 20s.
			invertCtx, cancel2 := context.WithTimeout(ctx, 20*time.Second)
			defer cancel2()
			if err := poll(invertCtx, 1*time.Second, func(ctx context.Context) (bool, error) {
				var role string
				err := e.DB.QueryRow(ctx, `SELECT role FROM instances WHERE instance_id=$1::uuid`, standbyID).Scan(&role)
				return err == nil && role == "primary", err
			}); err != nil {
				return fmt.Errorf("roles never inverted within 20s: %w", err)
			}

			// Exactly one failover_detected, with the right old/new primary.
			var failoverCount int
			var payload map[string]interface{}
			failoverCtx, cancel3 := context.WithTimeout(ctx, 30*time.Second)
			defer cancel3()
			if err := poll(failoverCtx, 1*time.Second, func(ctx context.Context) (bool, error) {
				var n int
				if err := e.DB.QueryRow(ctx,
					`SELECT count(*) FROM events WHERE type='failover_detected' AND ts >= $1`,
					promoteTS.Add(-time.Minute)).Scan(&n); err != nil {
					return false, err
				}
				failoverCount = n
				return n > 0, nil
			}); err != nil {
				return fmt.Errorf("failover_detected event never appeared: %w", err)
			}
			if failoverCount != 1 {
				return fmt.Errorf("expected exactly 1 failover_detected event, got %d", failoverCount)
			}
			if err := e.DB.QueryRow(ctx,
				`SELECT payload FROM events WHERE type='failover_detected' AND ts >= $1`,
				promoteTS.Add(-time.Minute)).Scan(&payload); err != nil {
				return fmt.Errorf("query failover_detected payload: %w", err)
			}
			if op, _ := payload["old_primary"].(string); op != primaryID {
				return fmt.Errorf("failover_detected old_primary=%q, want %q", op, primaryID)
			}
			if np, _ := payload["new_primary"].(string); np != standbyID {
				return fmt.Errorf("failover_detected new_primary=%q, want %q", np, standbyID)
			}

			// /api/v1/clusters reports primary = the (formerly standby) instance.
			clustersCtx, cancel4 := context.WithTimeout(ctx, 30*time.Second)
			defer cancel4()
			if err := poll(clustersCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				clusters, err := e.API.Clusters()
				if err != nil {
					return false, err
				}
				for _, c := range clusters {
					if cid, _ := c["cluster_id"].(string); cid != clusterID {
						continue
					}
					prim, _ := c["primary"].(string)
					return prim == standbyID, nil
				}
				return false, nil
			}); err != nil {
				return fmt.Errorf("GET /api/v1/clusters never reported the new primary: %w", err)
			}

			// cluster_id byte-identical to the value captured before the promote (I-1).
			var clusterIDAfter string
			if err := e.DB.QueryRow(ctx, `SELECT cluster_id::text FROM instances WHERE instance_id=$1::uuid`, standbyID).Scan(&clusterIDAfter); err != nil {
				return fmt.Errorf("query post-promote cluster_id: %w", err)
			}
			if clusterIDAfter != clusterID {
				return fmt.Errorf("cluster_id changed across the promote: before=%s after=%s", clusterID, clusterIDAfter)
			}

			// Consistently for 10s: no cluster_id_changed, no second failover_detected.
			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) {
				var ccChanged, failovers int
				if err := e.DB.QueryRow(ctx, `SELECT count(*) FROM events WHERE type='cluster_id_changed'`).Scan(&ccChanged); err != nil {
					return fmt.Errorf("query cluster_id_changed events: %w", err)
				}
				if ccChanged > 0 {
					return fmt.Errorf("unexpected cluster_id_changed event after the promote")
				}
				if err := e.DB.QueryRow(ctx,
					`SELECT count(*) FROM events WHERE type='failover_detected' AND ts >= $1`,
					promoteTS.Add(-time.Minute)).Scan(&failovers); err != nil {
					return fmt.Errorf("query failover_detected events: %w", err)
				}
				if failovers != 1 {
					return fmt.Errorf("failover_detected count changed to %d after the initial one", failovers)
				}
				time.Sleep(2 * time.Second)
			}

			e.AssertInvariants(e.T)
			return nil
		},
	})

	Register(Scenario{
		ID:       "SYS-REPL-002",
		Title:    "pg_ctl promote without stopping the primary: split-brain",
		Topology: TopologyPrimaryStandby,
		Covers:   []string{"phase_07.md#6.4", "I-1"},
		Expect: Expectations{
			Events:     []string{"split_brain_detected"},
			Invariants: []string{"I-1", "I-2", "I-3", "I-4"},
		},
		Run: func(ctx context.Context, e *Env) error {
			var primaryID, standbyID, clusterID string
			resolveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := poll(resolveCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				rows, err := e.DB.Query(ctx, `SELECT instance_id::text, role, cluster_id::text FROM instances`)
				if err != nil {
					return false, err
				}
				defer rows.Close()
				primaryID, standbyID, clusterID = "", "", ""
				for rows.Next() {
					var id, role, cid string
					if err := rows.Scan(&id, &role, &cid); err != nil {
						return false, err
					}
					clusterID = cid
					switch role {
					case "primary":
						primaryID = id
					case "standby":
						standbyID = id
					}
				}
				return primaryID != "" && standbyID != "", nil
			}); err != nil {
				return fmt.Errorf("resolve primary+standby instances: %w", err)
			}
			if clusterID == "" {
				return fmt.Errorf("resolved instances but no cluster_id")
			}

			// The defining difference from SYS-REPL-001: the primary is left
			// running. Promoting the standby now gives the cluster two
			// simultaneous primaries instead of a clean failover.
			if _, err := e.ExecAs("pg-standby", "postgres", "pg_ctl", "promote", "-D", "/var/lib/postgresql/data"); err != nil {
				return fmt.Errorf("pg_ctl promote: %w", err)
			}
			promoteTS := time.Now().UTC()

			// The (formerly) standby's role flips to primary within 20s.
			invertCtx, cancel2 := context.WithTimeout(ctx, 20*time.Second)
			defer cancel2()
			if err := poll(invertCtx, 1*time.Second, func(ctx context.Context) (bool, error) {
				var role string
				err := e.DB.QueryRow(ctx, `SELECT role FROM instances WHERE instance_id=$1::uuid`, standbyID).Scan(&role)
				return err == nil && role == "primary", err
			}); err != nil {
				return fmt.Errorf("standby never reported primary within 20s: %w", err)
			}

			// Both instances now report role=primary in the same cluster.
			var primaryCount int
			if err := e.DB.QueryRow(ctx,
				`SELECT count(*) FROM instances WHERE cluster_id::text=$1 AND role='primary'`,
				clusterID).Scan(&primaryCount); err != nil {
				return fmt.Errorf("count primary instances: %w", err)
			}
			if primaryCount != 2 {
				return fmt.Errorf("expected 2 instances reporting primary (split-brain), got %d", primaryCount)
			}

			// split_brain_detected fires — but not immediately: the engine
			// treats any standby->primary transition with a recently-seen
			// prior primary as a *failover* first (topology/engine.go's
			// detectSplitBrain suppresses split-brain for 30s after any such
			// transition, to avoid a false positive during a real failover's
			// brief double-primary window). Since the primary was never
			// stopped here, it keeps reporting itself as primary every push
			// cycle, so split-brain fires once that 30s suppression window
			// has actually elapsed and the engine observes both instances
			// still primary.
			splitCtx, cancel3 := context.WithTimeout(ctx, 60*time.Second)
			defer cancel3()
			if err := poll(splitCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				var n int
				if err := e.DB.QueryRow(ctx,
					`SELECT count(*) FROM events WHERE type='split_brain_detected' AND ts >= $1`,
					promoteTS.Add(-time.Minute)).Scan(&n); err != nil {
					return false, err
				}
				return n > 0, nil
			}); err != nil {
				return fmt.Errorf("split_brain_detected event never appeared: %w", err)
			}

			// /api/v1/clusters reports health=critical for this cluster.
			healthCtx, cancel4 := context.WithTimeout(ctx, 30*time.Second)
			defer cancel4()
			if err := poll(healthCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				clusters, err := e.API.Clusters()
				if err != nil {
					return false, err
				}
				for _, c := range clusters {
					if cid, _ := c["cluster_id"].(string); cid != clusterID {
						continue
					}
					health, _ := c["health"].(string)
					return health == "critical", nil
				}
				return false, nil
			}); err != nil {
				return fmt.Errorf("GET /api/v1/clusters never reported health=critical: %w", err)
			}

			// cluster_id is unchanged across the split-brain (I-1).
			var clusterIDAfter string
			if err := e.DB.QueryRow(ctx, `SELECT cluster_id::text FROM instances WHERE instance_id=$1::uuid`, standbyID).Scan(&clusterIDAfter); err != nil {
				return fmt.Errorf("query post-promote cluster_id: %w", err)
			}
			if clusterIDAfter != clusterID {
				return fmt.Errorf("cluster_id changed across the split-brain: before=%s after=%s", clusterID, clusterIDAfter)
			}

			// Clean up: kill the original primary so later scenarios (and this
			// scenario's own AssertInvariants) don't run against a split-brain
			// cluster that lingers for the rest of the process's containers.
			if err := e.Compose("kill", "-s", "SIGKILL", "pg-primary"); err != nil {
				return fmt.Errorf("kill pg-primary (cleanup): %w", err)
			}

			e.AssertInvariants(e.T)
			return nil
		},
	})

	Register(Scenario{
		ID:       "SYS-REPL-003",
		Title:    "primary stopped, standby left unpromoted: orphan standby",
		Topology: TopologyPrimaryStandby,
		Covers:   []string{"phase_07.md#6.4", "I-1"},
		Expect: Expectations{
			Events:     []string{"orphan_standby", "no_primary_in_cluster"},
			Invariants: []string{"I-1", "I-2", "I-3", "I-4"},
		},
		Run: func(ctx context.Context, e *Env) error {
			var standbyID, clusterID string
			resolveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := poll(resolveCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				rows, err := e.DB.Query(ctx, `SELECT instance_id::text, role, cluster_id::text FROM instances`)
				if err != nil {
					return false, err
				}
				defer rows.Close()
				standbyID, clusterID = "", ""
				var sawPrimary bool
				for rows.Next() {
					var id, role, cid string
					if err := rows.Scan(&id, &role, &cid); err != nil {
						return false, err
					}
					clusterID = cid
					switch role {
					case "primary":
						sawPrimary = true
					case "standby":
						standbyID = id
					}
				}
				return sawPrimary && standbyID != "", nil
			}); err != nil {
				return fmt.Errorf("resolve primary+standby instances: %w", err)
			}
			if clusterID == "" {
				return fmt.Errorf("resolved instances but no cluster_id")
			}

			// Wait for the standby to actually be streaming from the primary
			// (checked directly against PostgreSQL, not through the agent
			// pipeline — internal/server/pipeline.go's metrics_replication
			// ingestion always writes edge_type="streaming" regardless of
			// real status and silently drops the status metric itself into
			// no field at all, so it cannot answer "is this connected right
			// now") before killing the primary. internal/topology.Engine
			// only starts its orphan clock once it has seen an edge object
			// at all (see engine.go's own TestEngine_OrphanStandby) — killing
			// the primary before the standby's walreceiver ever connected
			// would mean the standby's edge was never established in the
			// first place, so nothing could ever be "orphaned" from it.
			edgeCtx, cancelEdge := context.WithTimeout(ctx, 30*time.Second)
			defer cancelEdge()
			standbyPG := e.PG("pg-standby")
			if err := poll(edgeCtx, 1*time.Second, func(ctx context.Context) (bool, error) {
				var status string
				err := standbyPG.QueryRow(ctx, `SELECT status FROM pg_stat_wal_receiver`).Scan(&status)
				return err == nil && status == "streaming", nil
			}); err != nil {
				return fmt.Errorf("standby's wal receiver never reported streaming before the kill: %w", err)
			}

			// Give the agent time to actually scrape replication_receiver
			// (10s interval) and push at least one resulting high-confidence
			// edge (5s push_interval) now that PostgreSQL itself confirms
			// streaming — there is no server-visible signal for "the agent's
			// own cache already has this" to poll on instead (see the
			// comment above on metrics_replication's limits).
			select {
			case <-time.After(15 * time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}

			// Stop the primary and leave the standby unpromoted — the
			// standby never becomes a leader, so nothing ever resolves its
			// replication edge again, and the cluster is left with no
			// primary at all.
			if err := e.Compose("kill", "-s", "SIGKILL", "pg-primary"); err != nil {
				return fmt.Errorf("kill pg-primary: %w", err)
			}
			afterKill := time.Now().UTC()

			// The standby must never be promoted by anything in this
			// scenario: assert its role stays "standby" for the whole wait,
			// which is what distinguishes this from SYS-REPL-001/002.
			//
			// Timing budget: PostgreSQL's own wal_receiver_timeout (default
			// 60s) gates how long the standby takes to even notice its
			// sender is gone before replication_receiver reports it
			// disconnected; only then does the agent stop emitting a
			// high-confidence edge, and only then does
			// internal/topology.Engine's own 60s orphan window start
			// ticking (see TestEngine_OrphanStandby). no_primary_in_cluster
			// (internal/server/staleness.go) has its own independent 60s
			// threshold, measured from the primary's last successful push.
			// None of these windows share a clock, so budget generously.
			waitCtx, cancel2 := context.WithTimeout(ctx, 4*time.Minute)
			defer cancel2()
			var sawOrphan, sawNoPrimary bool
			if err := poll(waitCtx, 3*time.Second, func(ctx context.Context) (bool, error) {
				var role string
				if err := e.DB.QueryRow(ctx, `SELECT role FROM instances WHERE instance_id=$1::uuid`, standbyID).Scan(&role); err != nil {
					return false, err
				}
				if role != "standby" {
					return false, fmt.Errorf("standby was promoted (role=%q) — this scenario never promotes it", role)
				}

				if !sawOrphan {
					var n int
					if err := e.DB.QueryRow(ctx,
						`SELECT count(*) FROM events WHERE type='orphan_standby' AND instance_id=$1::uuid AND ts >= $2`,
						standbyID, afterKill.Add(-time.Minute)).Scan(&n); err != nil {
						return false, err
					}
					sawOrphan = n > 0
				}
				if !sawNoPrimary {
					var n int
					if err := e.DB.QueryRow(ctx,
						`SELECT count(*) FROM events WHERE type='no_primary_in_cluster' AND cluster_id::text=$1 AND ts >= $2`,
						clusterID, afterKill.Add(-time.Minute)).Scan(&n); err != nil {
						return false, err
					}
					sawNoPrimary = n > 0
				}
				return sawOrphan && sawNoPrimary, nil
			}); err != nil {
				return fmt.Errorf("orphan_standby=%v no_primary_in_cluster=%v never both appeared: %w", sawOrphan, sawNoPrimary, err)
			}

			// no_primary_in_cluster fires exactly once (phase_07.md's own
			// wording for this scenario).
			var noPrimaryCount int
			if err := e.DB.QueryRow(ctx,
				`SELECT count(*) FROM events WHERE type='no_primary_in_cluster' AND cluster_id::text=$1`,
				clusterID).Scan(&noPrimaryCount); err != nil {
				return fmt.Errorf("count no_primary_in_cluster events: %w", err)
			}
			if noPrimaryCount != 1 {
				return fmt.Errorf("expected exactly 1 no_primary_in_cluster event, got %d", noPrimaryCount)
			}

			// cluster_id is unchanged (I-1).
			var clusterIDAfter string
			if err := e.DB.QueryRow(ctx, `SELECT cluster_id::text FROM instances WHERE instance_id=$1::uuid`, standbyID).Scan(&clusterIDAfter); err != nil {
				return fmt.Errorf("query post-kill cluster_id: %w", err)
			}
			if clusterIDAfter != clusterID {
				return fmt.Errorf("cluster_id changed: before=%s after=%s", clusterID, clusterIDAfter)
			}

			e.AssertInvariants(e.T)
			return nil
		},
	})

	Register(Scenario{
		ID:       "SYS-REPL-004",
		Title:    "recovery_min_apply_delay: replay lag rises then returns to 0",
		Topology: TopologyPrimaryStandby,
		Covers:   []string{"phase_07.md#6.1", "phase_07.md#6.4"},
		Expect: Expectations{
			Invariants: []string{"I-2", "I-3", "I-4"},
		},
		Run: func(ctx context.Context, e *Env) error {
			var clusterID string
			resolveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := poll(resolveCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				err := e.DB.QueryRow(ctx, `SELECT cluster_id::text FROM instances WHERE role='primary'`).Scan(&clusterID)
				return err == nil, nil
			}); err != nil {
				return fmt.Errorf("resolve cluster_id: %w", err)
			}

			// Delay recovery on the standby by 30s, then write on the
			// primary: the write streams to the standby immediately (visible
			// in receive_lsn) but its application is deliberately held back,
			// which is exactly what internal/check/replication_receiver.go's
			// CASE guard (6.1) is computing a lag for.
			// ALTER SYSTEM cannot run inside a transaction block, and psql's
			// simple query protocol wraps multiple `;`-separated statements
			// in ONE implicit transaction (the exact gotcha row 111 hit
			// writing SYS-DB-001) — two separate -c flags, not one.
			if _, err := e.Exec("pg-standby", "psql", "-U", "postgres",
				"-c", "ALTER SYSTEM SET recovery_min_apply_delay = '30s'",
				"-c", "SELECT pg_reload_conf()"); err != nil {
				return fmt.Errorf("set recovery_min_apply_delay: %w", err)
			}
			if _, err := e.Exec("pg-primary", "psql", "-U", "postgres", "-c",
				"CREATE TABLE IF NOT EXISTS repl004_probe(i serial); INSERT INTO repl004_probe DEFAULT VALUES;"); err != nil {
				return fmt.Errorf("write on primary: %w", err)
			}

			// replay_lag_sec rises — sample it a few times over ~25s and
			// require it to reach a meaningfully large value (not just
			// nonzero-once), and that samples are non-decreasing while the
			// delayed transaction is still pending.
			riseCtx, cancelRise := context.WithTimeout(ctx, 28*time.Second)
			defer cancelRise()
			var lastLag float64
			var sawMeaningfulLag bool
			if err := poll(riseCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				var lag *float64
				if err := e.DB.QueryRow(ctx,
					`SELECT MAX(replay_lag_sec) FROM metrics_replication WHERE cluster_id::text=$1 AND replay_lag_sec IS NOT NULL`,
					clusterID).Scan(&lag); err != nil {
					return false, err
				}
				if lag == nil {
					return false, nil
				}
				if *lag < lastLag-0.5 { // small tolerance for sampling jitter, not a real decrease
					return false, fmt.Errorf("replay_lag_sec decreased before the delayed write applied: %v -> %v", lastLag, *lag)
				}
				lastLag = *lag
				if *lag >= 5 {
					sawMeaningfulLag = true
				}
				return sawMeaningfulLag, nil
			}); err != nil {
				return fmt.Errorf("replay_lag_sec never rose to a meaningful value (last=%v): %w", lastLag, err)
			}

			// health degrades while the replica is lagging.
			healthCtx, cancelHealth := context.WithTimeout(ctx, 15*time.Second)
			defer cancelHealth()
			if err := poll(healthCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				clusters, err := e.API.Clusters()
				if err != nil {
					return false, err
				}
				for _, c := range clusters {
					if cid, _ := c["cluster_id"].(string); cid != clusterID {
						continue
					}
					health, _ := c["health"].(string)
					return health == "degraded", nil
				}
				return false, nil
			}); err != nil {
				return fmt.Errorf("health never became degraded while lagging: %w", err)
			}

			// Remove the delay, write again, and confirm lag returns to 0 —
			// this is the CASE guard's true negative, not just the earlier
			// write's natural post-apply settle.
			if _, err := e.Exec("pg-standby", "psql", "-U", "postgres",
				"-c", "ALTER SYSTEM SET recovery_min_apply_delay = '0'",
				"-c", "SELECT pg_reload_conf()"); err != nil {
				return fmt.Errorf("clear recovery_min_apply_delay: %w", err)
			}
			// Give the still-pending delayed transaction (up to the
			// remainder of its original 30s) a chance to apply before the
			// next write, so the "returns to 0" check reflects the delay
			// actually being off, not a coincidental gap between samples.
			select {
			case <-time.After(30 * time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}
			if _, err := e.Exec("pg-primary", "psql", "-U", "postgres", "-c",
				"INSERT INTO repl004_probe DEFAULT VALUES;"); err != nil {
				return fmt.Errorf("write on primary after clearing delay: %w", err)
			}

			zeroCtx, cancelZero := context.WithTimeout(ctx, 20*time.Second)
			defer cancelZero()
			if err := poll(zeroCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				var lag *float64
				if err := e.DB.QueryRow(ctx,
					`SELECT replay_lag_sec FROM metrics_replication WHERE cluster_id::text=$1 AND replay_lag_sec IS NOT NULL ORDER BY ts DESC LIMIT 1`,
					clusterID).Scan(&lag); err != nil {
					return false, err
				}
				return lag != nil && *lag == 0, nil
			}); err != nil {
				return fmt.Errorf("replay_lag_sec never returned to 0 after clearing the delay: %w", err)
			}

			e.AssertInvariants(e.T)
			return nil
		},
	})

	Register(Scenario{
		ID:       "SYS-SLOT-001",
		Title:    "inactive slot retains WAL, event fires",
		Topology: TopologyPrimaryStandby,
		Covers:   []string{"phase_07.md#6.4"},
		Expect: Expectations{
			Events:     []string{"slot_inactive"},
			Invariants: []string{"I-2", "I-3", "I-4"},
		},
		Run: func(ctx context.Context, e *Env) error {
			// The slot ("standby1") is created on the primary — physical
			// replication slots live on the upstream side, not the standby
			// (test/fixtures/sql/replica_setup.sh).
			var primaryID string
			resolveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := poll(resolveCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				err := e.DB.QueryRow(ctx, `SELECT instance_id::text FROM instances WHERE role='primary'`).Scan(&primaryID)
				return err == nil, nil
			}); err != nil {
				return fmt.Errorf("resolve primary instance: %w", err)
			}

			// Stop (not kill) the standby: the slot survives a clean stop,
			// which is the whole point — a dead consumer with a slot never
			// gets automatically dropped, so WAL keeps accumulating for it
			// forever unless someone notices.
			if err := e.Compose("stop", "pg-standby"); err != nil {
				return fmt.Errorf("stop pg-standby: %w", err)
			}

			// The slot reports inactive.
			inactiveCtx, cancelInactive := context.WithTimeout(ctx, 30*time.Second)
			defer cancelInactive()
			if err := poll(inactiveCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				var active bool
				err := e.DB.QueryRow(ctx,
					`SELECT slot_active FROM metrics_replication WHERE instance_id=$1::uuid AND slot_name='standby1' ORDER BY ts DESC LIMIT 1`,
					primaryID).Scan(&active)
				return err == nil && !active, nil
			}); err != nil {
				return fmt.Errorf("slot never reported inactive: %w", err)
			}

			// slot_retained_bytes grows monotonically while writes continue
			// on the primary with nothing consuming the slot.
			var lastRetained int64 = -1
			growCtx, cancelGrow := context.WithTimeout(ctx, 20*time.Second)
			defer cancelGrow()
			sawGrowth := false
			if err := poll(growCtx, 3*time.Second, func(ctx context.Context) (bool, error) {
				if _, err := e.Exec("pg-primary", "psql", "-U", "postgres", "-c",
					"CREATE TABLE IF NOT EXISTS slot001_probe(i serial); INSERT INTO slot001_probe DEFAULT VALUES;"); err != nil {
					return false, fmt.Errorf("write on primary: %w", err)
				}
				var retained int64
				if err := e.DB.QueryRow(ctx,
					`SELECT slot_retained_bytes FROM metrics_replication WHERE instance_id=$1::uuid AND slot_name='standby1' AND slot_retained_bytes IS NOT NULL ORDER BY ts DESC LIMIT 1`,
					primaryID).Scan(&retained); err != nil {
					return false, err
				}
				if lastRetained >= 0 && retained < lastRetained {
					return false, fmt.Errorf("slot_retained_bytes decreased: %d -> %d", lastRetained, retained)
				}
				if lastRetained >= 0 && retained > lastRetained {
					sawGrowth = true
				}
				lastRetained = retained
				return sawGrowth, nil
			}); err != nil {
				return fmt.Errorf("slot_retained_bytes never grew (last=%d): %w", lastRetained, err)
			}

			// slot_inactive fires after the configured window
			// (internal/server/staleness.go: slotInactiveThreshold, 30s).
			eventCtx, cancelEvent := context.WithTimeout(ctx, 45*time.Second)
			defer cancelEvent()
			if err := poll(eventCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				var n int
				if err := e.DB.QueryRow(ctx,
					`SELECT count(*) FROM events WHERE type='slot_inactive' AND instance_id=$1::uuid`,
					primaryID).Scan(&n); err != nil {
					return false, err
				}
				return n > 0, nil
			}); err != nil {
				return fmt.Errorf("slot_inactive event never appeared: %w", err)
			}

			e.AssertInvariants(e.T)
			return nil
		},
	})

	Register(Scenario{
		ID:       "SYS-REPL-005",
		Title:    "double role reversal: identity survives promote then rejoin",
		Topology: TopologyPrimaryStandby,
		Covers:   []string{"phase_07.md#6.4", "I-1"},
		Expect: Expectations{
			Events:     []string{"failover_detected"},
			Invariants: []string{"I-1", "I-2", "I-3", "I-4"},
		},
		Run: func(ctx context.Context, e *Env) error {
			var oldPrimaryID, standbyID, clusterID string
			resolveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := poll(resolveCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				rows, err := e.DB.Query(ctx, `SELECT instance_id::text, role, cluster_id::text FROM instances`)
				if err != nil {
					return false, err
				}
				defer rows.Close()
				oldPrimaryID, standbyID, clusterID = "", "", ""
				for rows.Next() {
					var id, role, cid string
					if err := rows.Scan(&id, &role, &cid); err != nil {
						return false, err
					}
					clusterID = cid
					switch role {
					case "primary":
						oldPrimaryID = id
					case "standby":
						standbyID = id
					}
				}
				return oldPrimaryID != "" && standbyID != "", nil
			}); err != nil {
				return fmt.Errorf("resolve primary+standby instances: %w", err)
			}
			if clusterID == "" {
				return fmt.Errorf("resolved instances but no cluster_id")
			}

			// Reversal 1: a real failover, exactly like SYS-REPL-001.
			if err := e.Compose("kill", "-s", "SIGKILL", "pg-primary"); err != nil {
				return fmt.Errorf("kill pg-primary: %w", err)
			}
			if _, err := e.ExecAs("pg-standby", "postgres", "pg_ctl", "promote", "-D", "/var/lib/postgresql/data"); err != nil {
				return fmt.Errorf("pg_ctl promote: %w", err)
			}
			invertCtx, cancelInvert := context.WithTimeout(ctx, 20*time.Second)
			defer cancelInvert()
			if err := poll(invertCtx, 1*time.Second, func(ctx context.Context) (bool, error) {
				var role string
				err := e.DB.QueryRow(ctx, `SELECT role FROM instances WHERE instance_id=$1::uuid`, standbyID).Scan(&role)
				return err == nil && role == "primary", err
			}); err != nil {
				return fmt.Errorf("first role inversion never happened: %w", err)
			}
			failoverCtx, cancelFailover := context.WithTimeout(ctx, 30*time.Second)
			defer cancelFailover()
			if err := poll(failoverCtx, 1*time.Second, func(ctx context.Context) (bool, error) {
				var n int
				if err := e.DB.QueryRow(ctx, `SELECT count(*) FROM events WHERE type='failover_detected'`).Scan(&n); err != nil {
					return false, err
				}
				return n > 0, nil
			}); err != nil {
				return fmt.Errorf("first failover_detected never appeared: %w", err)
			}

			// Reversal 2: rejoin the old primary as a standby of the new
			// one. There is no in-place way to do this through the compose
			// service itself (its entrypoint is the stock postgres image,
			// baked to boot whatever is already in PGDATA — unlike
			// pg-standby's own custom standby_init.sh, there is no script
			// here to swap roles), so a one-off container is run against the
			// same named volume to wipe it and pg_basebackup fresh from the
			// new primary (pg-standby) — the exact same recipe
			// test/fixtures/sql/standby_init.sh already uses for the
			// original bootstrap, just invoked directly instead of baked
			// into an entrypoint. pg_basebackup preserves the source's
			// system_identifier, so the rebuilt instance keeps the same
			// cluster_id; the pglens agent's own identity.json (a separate
			// file, in the agent container's own volume, never touched
			// here) is what keeps instance_id stable across this rebuild.
			if err := e.Compose(
				"run", "--rm", "--user", "root", "--entrypoint", "/bin/bash", "pg-primary", "-c",
				"until pg_isready -h pg-standby -U postgres -d postgres; do sleep 1; done && "+
					"rm -rf /var/lib/postgresql/data/* && chown postgres:postgres /var/lib/postgresql/data && "+
					"PGPASSWORD=postgres gosu postgres psql -h pg-standby -U postgres -d postgres -c \"SELECT pg_create_physical_replication_slot('standby1') WHERE NOT EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name='standby1')\" && "+
					"PGPASSWORD=postgres gosu postgres pg_basebackup -h pg-standby -U postgres -S standby1 -D /var/lib/postgresql/data -Fp -Xs -R",
			); err != nil {
				return fmt.Errorf("rebuild old primary as a standby: %w", err)
			}
			if err := e.Compose("up", "-d", "pg-primary"); err != nil {
				return fmt.Errorf("restart pg-primary: %w", err)
			}

			// The rebuilt instance actually streams from the new primary.
			streamCtx, cancelStream := context.WithTimeout(ctx, 180*time.Second)
			defer cancelStream()
			oldPrimaryPG := e.PG("pg-primary")
			if err := poll(streamCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				var status string
				err := oldPrimaryPG.QueryRow(ctx, `SELECT status FROM pg_stat_wal_receiver`).Scan(&status)
				return err == nil && status == "streaming", nil
			}); err != nil {
				return fmt.Errorf("rebuilt instance never streamed from the new primary: %w", err)
			}

			// The graph has reversed: same instance_id, now reporting
			// standby, of the same cluster.
			reverseCtx, cancelReverse := context.WithTimeout(ctx, 30*time.Second)
			defer cancelReverse()
			if err := poll(reverseCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				var role, cid string
				err := e.DB.QueryRow(ctx, `SELECT role, cluster_id::text FROM instances WHERE instance_id=$1::uuid`, oldPrimaryID).Scan(&role, &cid)
				return err == nil && role == "standby" && cid == clusterID, nil
			}); err != nil {
				return fmt.Errorf("old primary never reported standby of the same cluster: %w", err)
			}

			// The new primary never wavered, and no second failover_detected
			// was produced by this rejoin — a plain role reversal, not
			// another failover, per phase_07.md's own wording ("the old
			// failover_detected remains in history").
			var newPrimaryRole string
			if err := e.DB.QueryRow(ctx, `SELECT role FROM instances WHERE instance_id=$1::uuid`, standbyID).Scan(&newPrimaryRole); err != nil {
				return fmt.Errorf("query new primary role: %w", err)
			}
			if newPrimaryRole != "primary" {
				return fmt.Errorf("new primary role changed unexpectedly to %q", newPrimaryRole)
			}
			var failoverCount int
			if err := e.DB.QueryRow(ctx, `SELECT count(*) FROM events WHERE type='failover_detected'`).Scan(&failoverCount); err != nil {
				return fmt.Errorf("count failover_detected events: %w", err)
			}
			if failoverCount != 1 {
				return fmt.Errorf("expected exactly 1 failover_detected across both reversals, got %d", failoverCount)
			}

			// cluster_id is unchanged after both reversals (I-1).
			var clusterIDAfter string
			if err := e.DB.QueryRow(ctx, `SELECT cluster_id::text FROM instances WHERE instance_id=$1::uuid`, standbyID).Scan(&clusterIDAfter); err != nil {
				return fmt.Errorf("query post-reversal cluster_id: %w", err)
			}
			if clusterIDAfter != clusterID {
				return fmt.Errorf("cluster_id changed across the two reversals: before=%s after=%s", clusterID, clusterIDAfter)
			}

			e.AssertInvariants(e.T)
			return nil
		},
	})
}
