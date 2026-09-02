//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/manprint/pglens/test/harness"
	"github.com/manprint/pglens/test/scenario"
)

const (
	maintenanceTable          = "pglens_e2e_maintenance_probe"
	maintenanceLookupIndex    = "pglens_e2e_maintenance_lookup_idx"
	maintenanceUnusedIndex    = "pglens_e2e_maintenance_unused_idx"
	maintenanceDuplicateA     = "pglens_e2e_maintenance_duplicate_a_idx"
	maintenanceDuplicateB     = "pglens_e2e_maintenance_duplicate_b_idx"
	maintenanceRelationBudget = 200
)

func init() {
	registerMaintenanceScenario("SYS-VAC-001", "autovacuum-off dead tuples fall after VACUUM", runVacuumMaintenance)
	registerMaintenanceScenario("SYS-BLOAT-001", "estimated bloat is exposed and exact bloat is optional", runBloatMaintenance)
	registerMaintenanceScenario("SYS-IDX-001", "unused index finding names the exact index", runUnusedIndexMaintenance)
	registerMaintenanceScenario("SYS-IDX-002", "relation cardinality stays within the reporting budget", runRelationCardinalityMaintenance)
	registerMaintenanceScenario("SYS-IDX-003", "duplicate index finding resolves after one index is dropped", runDuplicateIndexMaintenance)
}

func registerMaintenanceScenario(id, title string, run func(context.Context, *scenario.Env) error) {
	scenario.Register(scenario.Scenario{
		ID:          id,
		Title:       title,
		Topology:    scenario.TopologyStandalone,
		EstDuration: 3 * time.Minute,
		Covers:      []string{"phase_10.md#9.4", "IDEA.md#5"},
		Expect:      scenario.Expectations{Invariants: []string{"I-1", "I-2", "I-3", "I-4"}},
		Run:         run,
	})
}

func TestFull_VacuumMaintenance(t *testing.T)         { runMaintenanceScenario(t, "SYS-VAC-001") }
func TestFull_BloatMaintenance(t *testing.T)          { runMaintenanceScenario(t, "SYS-BLOAT-001") }
func TestFull_UnusedIndexMaintenance(t *testing.T)    { runMaintenanceScenario(t, "SYS-IDX-001") }
func TestFull_RelationCardinality(t *testing.T)       { runMaintenanceScenario(t, "SYS-IDX-002") }
func TestFull_DuplicateIndexMaintenance(t *testing.T) { runMaintenanceScenario(t, "SYS-IDX-003") }

func runMaintenanceScenario(t *testing.T, id string) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping L3 maintenance test in short mode")
	}
	h := harness.Start(t, harness.Config{
		Topology:  harness.TopologyStandalone,
		AgentMode: resolveAgentMode(harness.AgentModeContainer),
	})
	h.Scenario(t, id)
}

func runVacuumMaintenance(ctx context.Context, e *scenario.Env) error {
	instanceID, pg := maintenanceSetup(ctx, e)
	if err := prepareMaintenanceTable(ctx, pg, nil); err != nil {
		return err
	}
	if err := deleteMaintenanceRows(ctx, pg); err != nil {
		return err
	}
	if err := waitForTableRatio(ctx, e, instanceID, true); err != nil {
		return fmt.Errorf("dead tuple ratio after delete: %w", err)
	}
	if _, err := pg.Exec(ctx, "VACUUM (ANALYZE) "+maintenanceTable); err != nil {
		return fmt.Errorf("vacuum maintenance table: %w", err)
	}
	if err := waitForTableRatio(ctx, e, instanceID, false); err != nil {
		return fmt.Errorf("dead tuple ratio after vacuum: %w", err)
	}
	e.AssertInvariants(e.T)
	return nil
}

func runBloatMaintenance(ctx context.Context, e *scenario.Env) error {
	instanceID, pg := maintenanceSetup(ctx, e)
	if err := prepareMaintenanceTable(ctx, pg, nil); err != nil {
		return err
	}
	if err := deleteMaintenanceRows(ctx, pg); err != nil {
		return err
	}
	if err := waitForBloat(ctx, e, instanceID); err != nil {
		return fmt.Errorf("estimated bloat: %w", err)
	}

	var hasPgstattuple bool
	if err := pg.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname='pgstattuple')`).Scan(&hasPgstattuple); err != nil {
		return fmt.Errorf("detect pgstattuple extension: %w", err)
	}
	if hasPgstattuple {
		if err := runExactBloatCommand(ctx, e, instanceID); err != nil {
			return err
		}
	}
	e.AssertInvariants(e.T)
	return nil
}

func runUnusedIndexMaintenance(ctx context.Context, e *scenario.Env) error {
	instanceID, pg := maintenanceSetup(ctx, e)
	if err := prepareMaintenanceTable(ctx, pg, []string{maintenanceUnusedIndex}); err != nil {
		return err
	}
	if err := waitForIndex(ctx, e, instanceID, maintenanceUnusedIndex); err != nil {
		return err
	}
	if err := seedIndexHistory(ctx, e.DB, instanceID, maintenanceUnusedIndex); err != nil {
		return err
	}
	if err := waitForFinding(ctx, e, instanceID, "index.unused", "public."+maintenanceUnusedIndex, "open"); err != nil {
		return fmt.Errorf("unused index finding: %w", err)
	}
	e.AssertInvariants(e.T)
	return nil
}

func runRelationCardinalityMaintenance(ctx context.Context, e *scenario.Env) error {
	instanceID, pg := maintenanceSetup(ctx, e)
	started := time.Now()
	if _, err := pg.Exec(ctx, `DO $$
DECLARE i integer;
BEGIN
  FOR i IN 1..5000 LOOP
    EXECUTE format('CREATE TABLE IF NOT EXISTS public.pglens_e2e_rel_%s (id integer)', lpad(i::text, 5, '0'));
    EXECUTE format('CREATE INDEX IF NOT EXISTS pglens_e2e_rel_%s_idx ON public.pglens_e2e_rel_%s (id)', lpad(i::text, 5, '0'), lpad(i::text, 5, '0'));
  END LOOP;
END $$`); err != nil {
		return fmt.Errorf("seed 5000 indexed tables: %w", err)
	}
	if err := waitForRelationBudget(ctx, e, instanceID, started); err != nil {
		return err
	}
	e.AssertInvariants(e.T)
	return nil
}

func runDuplicateIndexMaintenance(ctx context.Context, e *scenario.Env) error {
	instanceID, pg := maintenanceSetup(ctx, e)
	if err := prepareMaintenanceTable(ctx, pg, []string{maintenanceDuplicateA, maintenanceDuplicateB}); err != nil {
		return err
	}
	if err := waitForIndex(ctx, e, instanceID, maintenanceDuplicateA, maintenanceDuplicateB); err != nil {
		return err
	}
	if err := waitForFinding(ctx, e, instanceID, "index.duplicate", "public."+maintenanceTable, "open"); err != nil {
		return fmt.Errorf("duplicate index finding: %w", err)
	}
	if _, err := pg.Exec(ctx, "DROP INDEX "+maintenanceDuplicateB); err != nil {
		return fmt.Errorf("drop duplicate index: %w", err)
	}
	if err := waitForFinding(ctx, e, instanceID, "index.duplicate", "public."+maintenanceTable, "resolved"); err != nil {
		return fmt.Errorf("duplicate index finding resolution: %w", err)
	}
	e.AssertInvariants(e.T)
	return nil
}

func maintenanceSetup(ctx context.Context, e *scenario.Env) (string, *pgxpool.Pool) {
	e.T.Helper()
	var id string
	if err := pollMaintenance(ctx, 500*time.Millisecond, func(ctx context.Context) (bool, error) {
		err := e.DB.QueryRow(ctx, `SELECT instance_id::text FROM instances ORDER BY last_seen DESC LIMIT 1`).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return err == nil && id != "", err
	}); err != nil {
		e.T.Fatalf("resolve maintenance instance: %v", err)
	}
	return id, e.PG("pg")
}

func prepareMaintenanceTable(ctx context.Context, pg *pgxpool.Pool, indexes []string) error {
	if _, err := pg.Exec(ctx, `CREATE TABLE IF NOT EXISTS `+maintenanceTable+` (id bigint PRIMARY KEY, payload text NOT NULL)`); err != nil {
		return fmt.Errorf("create maintenance table: %w", err)
	}
	if _, err := pg.Exec(ctx, `ALTER TABLE `+maintenanceTable+` SET (autovacuum_enabled=false, toast.autovacuum_enabled=false)`); err != nil {
		return fmt.Errorf("disable maintenance autovacuum: %w", err)
	}
	if _, err := pg.Exec(ctx, `TRUNCATE TABLE `+maintenanceTable); err != nil {
		return fmt.Errorf("truncate maintenance table: %w", err)
	}
	if _, err := pg.Exec(ctx, `INSERT INTO `+maintenanceTable+` (id, payload)
SELECT i, repeat(md5(i::text), 8) FROM generate_series(1, 200000) AS s(i)`); err != nil {
		return fmt.Errorf("seed maintenance table: %w", err)
	}
	if _, err := pg.Exec(ctx, `CREATE INDEX IF NOT EXISTS `+maintenanceLookupIndex+` ON `+maintenanceTable+` (payload)`); err != nil {
		return fmt.Errorf("create lookup index: %w", err)
	}
	for _, index := range indexes {
		if _, err := pg.Exec(ctx, "CREATE INDEX IF NOT EXISTS "+index+" ON "+maintenanceTable+" (id, payload)"); err != nil {
			return fmt.Errorf("create index %s: %w", index, err)
		}
	}
	if _, err := pg.Exec(ctx, "ANALYZE "+maintenanceTable); err != nil {
		return fmt.Errorf("analyze maintenance table: %w", err)
	}
	return nil
}

func deleteMaintenanceRows(ctx context.Context, pg *pgxpool.Pool) error {
	if _, err := pg.Exec(ctx, `DELETE FROM `+maintenanceTable+` WHERE id <= 120000`); err != nil {
		return fmt.Errorf("delete maintenance rows: %w", err)
	}
	// Leave the dead tuples visible to pg_stat_user_tables. Running ANALYZE
	// immediately after the DELETE can race with the stats collector and make
	// n_dead_tup read as zero before table_stats has observed the maintenance
	// event, turning the high-ratio phase into a timeout.
	return nil
}

func waitForTableRatio(ctx context.Context, e *scenario.Env, instanceID string, wantHigh bool) error {
	attempt := 0
	return pollMaintenance(ctx, time.Second, func(ctx context.Context) (bool, error) {
		attempt++
		var ratio float64
		var vacuumAge *float64
		err := e.DB.QueryRow(ctx, `SELECT dead_tuple_ratio,last_vacuum_age_seconds
			FROM metrics_tables WHERE instance_id=$1 AND schemaname='public' AND relname=$2
			ORDER BY ts DESC LIMIT 1`, instanceID, maintenanceTable).Scan(&ratio, &vacuumAge)
		if errors.Is(err, pgx.ErrNoRows) {
			if attempt%30 == 0 {
				e.T.Logf("table ratio poll: phase=%s db row not observed after %d attempts", tableRatioPhase(wantHigh), attempt)
			}
			return false, nil
		}
		if err != nil {
			return false, err
		}
		payload, err := e.API.Get("/api/v1/instances/" + instanceID + "/tables?order=dead_ratio&limit=5")
		if err != nil {
			return false, err
		}
		item, ok := relationItem(payload, maintenanceTable)
		if !ok {
			return false, nil
		}
		apiRatio, ok := item["dead_ratio"].(float64)
		if !ok {
			return false, nil
		}
		if attempt%30 == 0 {
			age := "nil"
			if vacuumAge != nil {
				age = fmt.Sprintf("%.1fs", *vacuumAge)
			}
			e.T.Logf("table ratio poll: phase=%s db=%.3f api=%.3f vacuum_age=%s attempt=%d", tableRatioPhase(wantHigh), ratio, apiRatio, age, attempt)
		}
		if wantHigh {
			return ratio > .2 && apiRatio > .2, nil
		}
		// The preceding phase already observed this table above the threshold;
		// requiring both stores to observe it below the threshold proves that a
		// fresh post-VACUUM sample arrived. Do not add a wall-clock age gate:
		// under the full suite, a delayed table_stats scrape can legitimately
		// publish a correct zero ratio after the last_vacuum age has crossed 60s.
		return ratio < .2 && apiRatio < .2, nil
	})
}

func tableRatioPhase(wantHigh bool) string {
	if wantHigh {
		return "after-delete"
	}
	return "after-vacuum"
}

func waitForBloat(ctx context.Context, e *scenario.Env, instanceID string) error {
	return pollMaintenance(ctx, time.Second, func(ctx context.Context) (bool, error) {
		var ratio float64
		err := e.DB.QueryRow(ctx, `SELECT bloat_ratio FROM metrics_bloat
			WHERE instance_id=$1 AND schemaname='public' AND relname=$2 AND method='estimate'
			ORDER BY ts DESC LIMIT 1`, instanceID, maintenanceTable).Scan(&ratio)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		payload, err := e.API.Get("/api/v1/instances/" + instanceID + "/bloat?method=estimate&limit=50")
		if err != nil {
			return false, err
		}
		item, ok := relationItem(payload, maintenanceTable)
		apiRatio, apiOK := item["bloat_ratio"].(float64)
		return ok && ratio > .2 && apiOK && apiRatio > .2, nil
	})
}

func runExactBloatCommand(ctx context.Context, e *scenario.Env, instanceID string) error {
	payload, err := json.Marshal(map[string]any{
		"kind": "pgstattuple",
		"args": map[string]string{"schema": "public", "relation": maintenanceTable},
	})
	if err != nil {
		return err
	}
	value, err := e.API.Request(http.MethodPost, "/api/v1/instances/"+instanceID+"/commands", payload)
	if err != nil {
		return fmt.Errorf("enqueue pgstattuple: %w", err)
	}
	accepted, ok := value.(map[string]interface{})
	if !ok {
		return fmt.Errorf("enqueue pgstattuple returned %T", value)
	}
	commandID, ok := accepted["command_id"].(string)
	if !ok || commandID == "" {
		return fmt.Errorf("enqueue pgstattuple returned no command_id")
	}
	if err := pollMaintenance(ctx, time.Second, func(ctx context.Context) (bool, error) {
		status, err := e.API.Get("/api/v1/commands/" + commandID)
		if err != nil {
			return false, err
		}
		state, _ := status["state"].(string)
		if state == "failed" || state == "expired" {
			return false, fmt.Errorf("pgstattuple command %s: %v", state, status["error"])
		}
		return state == "done", nil
	}); err != nil {
		return fmt.Errorf("wait pgstattuple command: %w", err)
	}
	return pollMaintenance(ctx, time.Second, func(ctx context.Context) (bool, error) {
		result, err := e.API.Get("/api/v1/instances/" + instanceID + "/bloat?method=pgstattuple&limit=50")
		if err != nil {
			return false, err
		}
		item, ok := relationItem(result, maintenanceTable)
		method, methodOK := item["method"].(string)
		return ok && methodOK && method == "pgstattuple", nil
	})
}

func waitForIndex(ctx context.Context, e *scenario.Env, instanceID string, names ...string) error {
	return pollMaintenance(ctx, time.Second, func(ctx context.Context) (bool, error) {
		for _, name := range names {
			var bytes int64
			err := e.DB.QueryRow(ctx, `SELECT index_bytes FROM metrics_indexes
				WHERE instance_id=$1 AND schemaname='public' AND indexrelname=$2
				ORDER BY ts DESC LIMIT 1`, instanceID, name).Scan(&bytes)
			if errors.Is(err, pgx.ErrNoRows) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			if bytes <= 0 {
				return false, nil
			}
		}
		return true, nil
	})
}

func seedIndexHistory(ctx context.Context, db *pgxpool.Pool, instanceID, indexName string) error {
	id, err := uuid.Parse(instanceID)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `INSERT INTO metrics_indexes
		(ts,tenant_id,cluster_id,instance_id,datname,schemaname,relname,indexrelname,
		 idx_scan,idx_tup_read,idx_tup_fetch,idx_blks_read,idx_blks_hit,index_bytes,
		 is_unique,is_primary,is_valid,def_hash)
	SELECT now()-interval '8 days',tenant_id,cluster_id,instance_id,datname,schemaname,relname,indexrelname,
		 0,0,0,0,0,COALESCE(index_bytes,0),COALESCE(is_unique,false),COALESCE(is_primary,false),COALESCE(is_valid,true),def_hash
	FROM metrics_indexes
	WHERE instance_id=$1 AND schemaname='public' AND indexrelname=$2
	ORDER BY ts DESC LIMIT 1`, id, indexName)
	if err != nil {
		return fmt.Errorf("seed index history: %w", err)
	}
	return nil
}

func waitForFinding(ctx context.Context, e *scenario.Env, instanceID, ruleID, objectName, wantedState string) error {
	return pollMaintenance(ctx, time.Second, func(ctx context.Context) (bool, error) {
		value, err := e.API.Request(http.MethodGet, "/api/v1/findings?instance_id="+instanceID+"&rule_id="+ruleID+"&state=all&limit=100", nil)
		if err != nil {
			return false, err
		}
		findings, ok := value.([]interface{})
		if !ok {
			return false, fmt.Errorf("findings returned %T", value)
		}
		for _, raw := range findings {
			finding, ok := raw.(map[string]interface{})
			if !ok || finding["object_name"] != objectName {
				continue
			}
			if finding["state"] == wantedState {
				if ruleID == "index.duplicate" && wantedState == "open" {
					if !duplicateEvidenceHas(finding, maintenanceDuplicateA, maintenanceDuplicateB) {
						return false, nil
					}
				}
				return true, nil
			}
		}
		return false, nil
	})
}

func duplicateEvidenceHas(finding map[string]interface{}, first, second string) bool {
	evidence, ok := finding["evidence"].(map[string]interface{})
	if !ok {
		return false
	}
	indexes, ok := evidence["indexes"].([]interface{})
	if !ok {
		return false
	}
	foundFirst, foundSecond := false, false
	for _, raw := range indexes {
		name, _ := raw.(string)
		foundFirst = foundFirst || name == "public."+first
		foundSecond = foundSecond || name == "public."+second
	}
	return foundFirst && foundSecond
}

func waitForRelationBudget(ctx context.Context, e *scenario.Env, instanceID string, started time.Time) error {
	if err := pollMaintenance(ctx, time.Second, func(ctx context.Context) (bool, error) {
		var intervals, maxRows, minRows int64
		err := e.DB.QueryRow(ctx, `SELECT count(*),COALESCE(max(n),0),COALESCE(min(n),0)
			FROM (SELECT ts,count(*) AS n FROM metrics_indexes
			WHERE instance_id=$1 AND ts >= now()-interval '2 minutes' GROUP BY ts) samples`, instanceID).Scan(&intervals, &maxRows, &minRows)
		if err != nil {
			return false, err
		}
		if intervals < 2 || maxRows > maintenanceRelationBudget || minRows > maintenanceRelationBudget {
			return false, nil
		}
		payload, err := e.API.Get("/api/v1/instances/" + instanceID + "/indexes?limit=50")
		if err != nil {
			return false, err
		}
		truncated, _ := payload["truncated"].(bool)
		notReported, _ := payload["relations_not_reported"].(float64)
		return truncated && notReported > 0, nil
	}); err != nil {
		return err
	}
	if elapsed := time.Since(started); elapsed > 120*time.Second {
		return fmt.Errorf("5000-table relation scrape took %s, want <= 120s", elapsed)
	}
	return nil
}

func relationItem(payload map[string]interface{}, relation string) (map[string]interface{}, bool) {
	items, ok := payload["items"].([]interface{})
	if !ok {
		return nil, false
	}
	for _, raw := range items {
		item, ok := raw.(map[string]interface{})
		if ok && item["relname"] == relation {
			return item, true
		}
	}
	return nil, false
}

func pollMaintenance(ctx context.Context, interval time.Duration, check func(context.Context) (bool, error)) error {
	for {
		ok, err := check(ctx)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
