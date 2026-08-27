//go:build e2e

package scenario

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

func init() {
	Register(Scenario{
		ID:       "SYS-DB-001",
		Title:    "database budget: 15 databases, max 10, the most active ones win",
		Topology: TopologyStandalone,
		Covers:   []string{"phase_06.md#5.7"},
		Smoke:    false,
		Expect:   Expectations{Invariants: []string{"I-1", "I-2", "I-3", "I-4"}},
		Run: func(ctx context.Context, e *Env) error {
			var instanceID string
			resolveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := poll(resolveCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				err := e.DB.QueryRow(ctx, `SELECT instance_id::text FROM instances ORDER BY last_seen DESC LIMIT 1`).Scan(&instanceID)
				return err == nil, err
			}); err != nil {
				return fmt.Errorf("resolve monitored instance: %w", err)
			}

			// 14 new databases plus the instance's own pre-existing
			// "postgres" database = 15 total, matching the scenario's own
			// "15 databases, max 10" framing exactly (creating 15 *new*
			// ones would make 16 candidates for a budget of 10, which is a
			// different, off-by-one scenario). Each gets a different
			// amount of committed activity (db14 busiest, db01 quietest):
			// psql -c with ;-separated statements commits each one
			// individually in autocommit mode, bumping
			// pg_stat_database.xact_commit once per statement — NOT once
			// per psql invocation: an earlier version of this loop joined
			// all of a database's SELECTs into one `-c "SELECT 1;SELECT
			// 1;..."` string, but postgres's simple query protocol wraps
			// an entire multi-statement message in a single implicit
			// transaction, so every database ended up with the same ~1
			// commit regardless of the intended count — verified live by
			// dumping the actual monitored/skipped ranking, which showed
			// a clean alphabetical split (db01-09 vs db10-14), the
			// signature of a total tie broken by name, not real activity.
			// Passing N separate -c flags to one psql invocation gives
			// each its own implicit transaction/commit.
			const n = 14
			const perRank = 20
			for i := 1; i <= n; i++ {
				name := fmt.Sprintf("db%02d", i)
				if _, err := e.Exec("pg", "psql", "-U", "postgres", "-c", fmt.Sprintf("CREATE DATABASE %s", name)); err != nil {
					return fmt.Errorf("create database %s: %w", name, err)
				}
				args := []string{"psql", "-U", "postgres", "-d", name}
				for c := 0; c < i*perRank; c++ {
					args = append(args, "-c", "SELECT 1")
				}
				if _, err := e.Exec("pg", args...); err != nil {
					return fmt.Errorf("generate activity in %s: %w", name, err)
				}
			}

			// Poll the real API (not the DB directly) — this is the whole
			// point of the scenario: does db_budget's decision actually
			// reach GET /api/v1/instances/{id}, not just internal state.
			budgetCtx, cancel2 := context.WithTimeout(ctx, 60*time.Second)
			defer cancel2()
			var detail map[string]interface{}
			if err := poll(budgetCtx, 3*time.Second, func(ctx context.Context) (bool, error) {
				var err error
				detail, err = e.API.Instance(instanceID)
				if err != nil {
					return false, err
				}
				dbs, _ := detail["databases"].([]interface{})
				return len(dbs) >= n+1, nil // +1 for "postgres"
			}); err != nil {
				return fmt.Errorf("instance never reported all %d databases: %w", n+1, err)
			}

			dbsRaw, _ := detail["databases"].([]interface{})
			totalMonitored, totalSkipped := 0, 0
			type ranked struct {
				index     int // 0 for "postgres" or anything outside the db* set
				monitored bool
			}
			var mine []ranked
			for _, raw := range dbsRaw {
				db, ok := raw.(map[string]interface{})
				if !ok {
					continue
				}
				name, _ := db["datname"].(string)
				isMonitored, _ := db["monitored"].(bool)
				if isMonitored {
					totalMonitored++
				} else {
					totalSkipped++
					if reason, _ := db["skip_reason"].(string); reason != "db_budget" {
						return fmt.Errorf("database %s skipped for reason %q, want db_budget", name, reason)
					}
				}
				var idx int
				if _, err := fmt.Sscanf(name, "db%02d", &idx); err == nil {
					mine = append(mine, ranked{index: idx, monitored: isMonitored})
				}
			}
			if totalMonitored != 10 {
				return fmt.Errorf("API reports %d monitored databases, want 10 (15 total, max 10)", totalMonitored)
			}
			if totalSkipped != 5 {
				return fmt.Errorf("API reports %d databases skipped with db_budget, want 5", totalSkipped)
			}

			// Among my own 14, whichever are skipped must be exactly the
			// lowest-indexed (least active) ones — no gaps, regardless of
			// whether "postgres" itself (an uncontrolled amount of setup
			// activity) took one of the 10 monitored slots.
			sort.Slice(mine, func(i, j int) bool { return mine[i].index < mine[j].index })
			seenMonitored := false
			for _, r := range mine {
				if r.monitored {
					seenMonitored = true
					continue
				}
				if seenMonitored {
					var summary []string
					for _, m := range mine {
						summary = append(summary, fmt.Sprintf("db%02d=%v", m.index, m.monitored))
					}
					return fmt.Errorf("db%02d is skipped but a less-active database is monitored — db_budget did not pick the most active databases; full ranking: %s", r.index, strings.Join(summary, " "))
				}
			}

			e.AssertInvariants(e.T)
			return nil
		},
	})
}
