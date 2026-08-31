//go:build e2e

package scenario

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func init() {
	Register(Scenario{
		ID:       "SYS-PERM-001",
		Title:    "whole stack green on tier T0 only",
		Topology: TopologyStandalone,
		Covers:   []string{"phase_06.md#5.7"},
		Smoke:    true,
		Expect:   Expectations{Invariants: []string{"I-1", "I-2", "I-3", "I-4"}},
		Run: func(ctx context.Context, e *Env) error {
			// test/compose's monitoring_user.sql (deploy/sql/monitoring_user.sql,
			// applied verbatim per §10's do-not-repeat rule) grants only
			// pg_monitor plus the two pg_control_system()/pg_control_checkpoint()
			// EXECUTE grants — T1 (pg_read_all_data) and T2 (pg_signal_backend)
			// are commented out. So the standalone topology already is the T0-only
			// fixture this scenario needs; no separate "rds-like" variant exists
			// yet (test/fixtures/sql/02_rds_like.sql, from sub-phase 2.2, is used
			// only by the L2 pgtest harness, not the L3 compose stack).
			clustersCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			var clusters []map[string]interface{}
			if err := poll(clustersCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				var err error
				clusters, err = e.API.Clusters()
				return err == nil && len(clusters) > 0, err
			}); err != nil {
				return fmt.Errorf("resolve clusters from API: %w", err)
			}

			found := false
			for _, c := range clusters {
				instances, _ := c["instances"].([]interface{})
				for _, raw := range instances {
					inst, ok := raw.(map[string]interface{})
					if !ok {
						continue
					}
					tier, _ := inst["perm_tier"].(string)
					if tier != "T0" {
						return fmt.Errorf("instance %v reports perm_tier=%q, want T0", inst["instance_id"], tier)
					}
					found = true
				}
			}
			if !found {
				return fmt.Errorf("API reported no instances to check perm_tier on")
			}

			// Every mandatory check (instance_info, activity, database_stats)
			// must actually be producing fresh samples under T0 — a silent
			// per-check failure would starve one of these series instead of
			// surfacing anywhere else. check_error_total does not exist yet
			// (STATE.md §6/§9 V001-F04, same gap already noted for I-8 in
			// sub-phase 5.6) so this is the honest proxy available today.
			// Polled, not a one-shot count: each check runs on its own
			// hardcoded DefaultInterval() (activity 10s, database_stats
			// 30s — the agent config's per-check `interval:` override is
			// parsed but never actually consulted by the scheduler, a real
			// gap STATE.md §8 records), plus scheduling jitter, plus
			// database_stats needs two raw observations before its first
			// rate exists at all.
			backendsCtx, cancel2 := context.WithTimeout(ctx, 30*time.Second)
			defer cancel2()
			if err := poll(backendsCtx, 3*time.Second, func(ctx context.Context) (bool, error) {
				var n int
				err := e.DB.QueryRow(ctx, `SELECT count(*) FROM metrics WHERE metric='pg_backends'`).Scan(&n)
				return err == nil && n > 0, err
			}); err != nil {
				return fmt.Errorf("no pg_backends samples — activity check is not producing data under T0: %w", err)
			}
			xactCtx, cancel3 := context.WithTimeout(ctx, 90*time.Second)
			defer cancel3()
			if err := poll(xactCtx, 3*time.Second, func(ctx context.Context) (bool, error) {
				var n int
				err := e.DB.QueryRow(ctx, `SELECT count(*) FROM metrics WHERE metric='pg_xact_commit_total'`).Scan(&n)
				return err == nil && n > 0, err
			}); err != nil {
				return fmt.Errorf("no pg_xact_commit_total samples — database_stats check is not producing data under T0: %w", err)
			}

			e.AssertInvariants(e.T)
			return nil
		},
	})

	Register(Scenario{
		ID:       "SYS-PERM-002",
		Title:    "permission tier grants take effect without an agent restart",
		Topology: TopologyStandalone,
		Covers:   []string{"phase_11.md#10.3"},
		Smoke:    true,
		Expect:   Expectations{Invariants: []string{"I-1"}},
		Run:      runPermissionTierGrant,
	})
}

const permissionProbeTable = "pglens_e2e_perm_probe"

func runPermissionTierGrant(ctx context.Context, e *Env) error {
	pg := e.PG("pg")
	instanceID, err := permissionInstance(ctx, e)
	if err != nil {
		return err
	}

	// Re-apply the shipped script at T0 after removing any higher grants that
	// could survive a manually reused test volume. This makes the first half
	// of the acceptance test prove the actual deployment SQL, not a fixture.
	if _, err := pg.Exec(ctx, "REVOKE pg_read_all_data, pg_signal_backend FROM pglens"); err != nil {
		return fmt.Errorf("reset monitoring role to T0: %w", err)
	}
	if err := applyMonitoringTier(e, ""); err != nil {
		return err
	}
	if err := waitPermissionTier(ctx, e, instanceID, "T0"); err != nil {
		return err
	}

	if _, err := pg.Exec(ctx, "DROP TABLE IF EXISTS "+permissionProbeTable); err != nil {
		return fmt.Errorf("drop permission probe: %w", err)
	}
	if _, err := pg.Exec(ctx, "CREATE TABLE "+permissionProbeTable+" (id integer NOT NULL)"); err != nil {
		return fmt.Errorf("create permission probe: %w", err)
	}
	defer func() { _, _ = pg.Exec(context.Background(), "DROP TABLE IF EXISTS "+permissionProbeTable) }()
	if _, err := pg.Exec(ctx, "INSERT INTO "+permissionProbeTable+" VALUES (1), (2)"); err != nil {
		return fmt.Errorf("seed permission probe: %w", err)
	}
	if _, err := pg.Exec(ctx, "SELECT count(*) FROM "+permissionProbeTable); err != nil {
		return fmt.Errorf("exercise permission probe: %w", err)
	}

	queryID, err := permissionProbeQueryID(ctx, pg)
	if err != nil {
		return err
	}

	commandID, err := enqueuePermissionCommand(e, instanceID, map[string]any{
		"kind": "explain", "args": map[string]any{"queryid": queryID, "datname": "postgres"},
	})
	if err != nil {
		return err
	}
	if err := waitPermissionCommand(ctx, e, commandID, "permission tier T1 is required"); err != nil {
		return fmt.Errorf("T0 EXPLAIN gate: %w", err)
	}

	// Keep one real target backend alive so the signal command exercises the
	// command permission gate with a valid pid rather than a validation error.
	conn, err := pg.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire signal probe backend: %w", err)
	}
	defer conn.Release()
	var pid int
	if err := conn.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		return fmt.Errorf("read signal probe pid: %w", err)
	}
	commandID, err = enqueuePermissionCommand(e, instanceID, map[string]any{
		"kind": "cancel", "args": map[string]any{"pid": pid},
	})
	if err != nil {
		return err
	}
	if err := waitPermissionCommand(ctx, e, commandID, "permission tier T2 is required"); err != nil {
		return fmt.Errorf("T0 signal gate: %w", err)
	}

	// Apply T1 through the same shipped SQL while the agent is running. The
	// next envelope/command capability refresh must publish and enforce T1.
	if err := applyMonitoringTier(e, "tier1"); err != nil {
		return err
	}
	if err := waitPermissionTier(ctx, e, instanceID, "T1"); err != nil {
		return fmt.Errorf("runtime T1 refresh: %w", err)
	}
	commandID, err = enqueuePermissionCommand(e, instanceID, map[string]any{
		"kind": "explain", "args": map[string]any{"queryid": queryID, "datname": "postgres"},
	})
	if err != nil {
		return err
	}
	if err := waitPermissionCommand(ctx, e, commandID, ""); err != nil {
		return fmt.Errorf("T1 EXPLAIN execution: %w", err)
	}

	// Exercise the highest role tier as well; T2 is intentionally applied only
	// after the T1 command has succeeded, so the no-restart transition is
	// independently observable.
	if err := applyMonitoringTier(e, "tier2"); err != nil {
		return err
	}
	if err := waitPermissionTier(ctx, e, instanceID, "T2"); err != nil {
		return fmt.Errorf("runtime T2 refresh: %w", err)
	}
	e.AssertInvariants(e.T)
	return nil
}

func applyMonitoringTier(e *Env, tier string) error {
	args := []string{
		"psql", "--username", "postgres", "--dbname", "postgres",
		"-v", "ON_ERROR_STOP=1",
		"-v", "pglens_password=pglens-monitoring-test",
		"-v", "dbname=postgres",
	}
	if tier == "tier1" || tier == "tier2" {
		args = append(args, "-v", "tier1=1")
	}
	if tier == "tier2" {
		args = append(args, "-v", "tier2=1")
	}
	args = append(args, "-f", "/opt/pglens/monitoring_user.sql")
	if output, err := e.ExecAs("pg", "postgres", args...); err != nil {
		return fmt.Errorf("apply monitoring SQL %s: %w (%s)", tierOrT0(tier), err, strings.TrimSpace(output))
	}
	return nil
}

func tierOrT0(tier string) string {
	if tier == "" {
		return "T0"
	}
	return strings.ToUpper(tier)
}

func permissionInstance(ctx context.Context, e *Env) (string, error) {
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		var id string
		// The standalone stack has one monitored instance. Container agents
		// report the Docker target name (pg), while the host binary reports
		// localhost because it reaches the published PostgreSQL port; resolve
		// by freshness instead of coupling this scenario to either transport.
		if err := e.DB.QueryRow(ctx, `SELECT instance_id::text FROM instances ORDER BY last_seen DESC LIMIT 1`).Scan(&id); err == nil {
			return id, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return "", fmt.Errorf("timed out resolving standalone permission-test instance")
}

func waitPermissionTier(ctx context.Context, e *Env, instanceID, want string) error {
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		var got string
		if err := e.DB.QueryRow(ctx, `SELECT perm_tier FROM instances WHERE instance_id=$1`, instanceID).Scan(&got); err == nil && got == want {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("timed out waiting for instance %s perm_tier=%s", instanceID, want)
}

func permissionProbeQueryID(ctx context.Context, pg *pgxpool.Pool) (int64, error) {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var queryID int64
		err := pg.QueryRow(ctx, `SELECT queryid FROM pg_stat_statements WHERE query LIKE '%pglens_e2e_perm_probe%' AND query ~* '^[[:space:]]*(SELECT|WITH)' ORDER BY calls DESC LIMIT 1`).Scan(&queryID)
		if err == nil {
			return queryID, nil
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return 0, fmt.Errorf("timed out resolving permission probe queryid")
}

func enqueuePermissionCommand(e *Env, instanceID string, payload map[string]any) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal permission command: %w", err)
	}
	raw, err := e.API.Request(http.MethodPost, "/api/v1/instances/"+instanceID+"/commands", body)
	if err != nil {
		return "", fmt.Errorf("enqueue permission command: %w", err)
	}
	response, ok := raw.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("enqueue permission command response=%v", raw)
	}
	commandID, _ := response["command_id"].(string)
	if commandID == "" {
		return "", fmt.Errorf("enqueue permission command missing command_id: %v", raw)
	}
	return commandID, nil
}

func waitPermissionCommand(ctx context.Context, e *Env, commandID, wantError string) error {
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		response, err := e.API.Get("/api/v1/commands/" + commandID)
		if err == nil {
			state, _ := response["state"].(string)
			if state == "done" {
				gotError, _ := response["error"].(string)
				if wantError != "" && !strings.Contains(gotError, wantError) {
					return fmt.Errorf("command error=%q, want substring %q", gotError, wantError)
				}
				if wantError == "" && strings.TrimSpace(gotError) != "" {
					return fmt.Errorf("successful command returned error=%q", gotError)
				}
				return nil
			}
			if state == "failed" && wantError != "" {
				gotError, _ := response["error"].(string)
				if strings.Contains(gotError, wantError) {
					return nil
				}
				return fmt.Errorf("command error=%q, want substring %q", gotError, wantError)
			}
			if state == "failed" || state == "expired" {
				return fmt.Errorf("command reached terminal state=%s: %v", state, response["error"])
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("timed out waiting for command %s", commandID)
}
