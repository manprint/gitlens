//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/manprint/pglens/test/harness"
	"github.com/manprint/pglens/test/scenario"
)

const advisorWait = 90 * time.Second

func init() {
	scenario.Register(scenario.Scenario{
		ID:          "SYS-ADV-001",
		Title:       "advisor reports the exact relation and memory set",
		Topology:    scenario.TopologyStandalone,
		EstDuration: 2 * time.Minute,
		Covers:      []string{"phase_10.md#9.5", "IDEA.md#7"},
		Expect:      scenario.Expectations{Invariants: []string{"I-1", "I-2", "I-3", "I-4"}},
		Run:         runAdvisorExact,
	})
	scenario.Register(scenario.Scenario{
		ID:          "SYS-ADV-002",
		Title:       "advisor degrades query rules when pg_stat_statements is unavailable",
		Topology:    scenario.TopologyPrimaryStandby,
		EstDuration: 2 * time.Minute,
		Covers:      []string{"phase_10.md#9.5", "IDEA.md#7"},
		Expect:      scenario.Expectations{Invariants: []string{"I-1", "I-2", "I-3", "I-4"}},
		Run:         runAdvisorDegraded,
	})
	scenario.Register(scenario.Scenario{
		ID:          "SYS-ADV-003",
		Title:       "advisor resolves fixed findings without reopening them",
		Topology:    scenario.TopologyStandalone,
		EstDuration: 2 * time.Minute,
		Covers:      []string{"phase_10.md#9.5", "IDEA.md#7"},
		Expect:      scenario.Expectations{Invariants: []string{"I-1", "I-2", "I-3", "I-4"}},
		Run:         runAdvisorFix,
	})
	scenario.Register(scenario.Scenario{
		ID:          "SYS-ADV-004",
		Title:       "advisor reports the exact durability and memory misconfiguration set",
		Topology:    scenario.TopologyStandalone,
		EstDuration: 2 * time.Minute,
		Covers:      []string{"phase_10.md#9.5", "IDEA.md#7"},
		Expect:      scenario.Expectations{Invariants: []string{"I-1", "I-2", "I-3", "I-4"}},
		Run:         runAdvisorMisconfigured,
	})
}

func TestFull_AdvisorExactSet(t *testing.T) {
	runAdvisorScenario(t, "SYS-ADV-001", harness.TopologyStandalone)
}
func TestFull_AdvisorDegraded(t *testing.T) {
	runAdvisorScenario(t, "SYS-ADV-002", harness.TopologyPrimaryStandby)
}
func TestFull_AdvisorFixLifecycle(t *testing.T) {
	runAdvisorScenario(t, "SYS-ADV-003", harness.TopologyStandalone)
}
func TestFull_AdvisorMisconfiguration(t *testing.T) {
	runAdvisorScenario(t, "SYS-ADV-004", harness.TopologyStandalone)
}

func runAdvisorScenario(t *testing.T, id string, topology harness.Topology) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping L3 advisor test in short mode")
	}
	h := harness.Start(t, harness.Config{Topology: topology, AgentMode: harness.AgentModeContainer})
	h.Scenario(t, id)
}

func runAdvisorExact(ctx context.Context, e *scenario.Env) error {
	id, err := advisorInstanceID(ctx, e, "pg")
	if err != nil {
		return err
	}
	if err := e.Compose("stop", "pglens-agent"); err != nil {
		return fmt.Errorf("stop agent before deterministic advisor seed: %w", err)
	}
	if err := prepareAdvisorRelation(ctx, e.PG("pg")); err != nil {
		return err
	}
	if err := seedAdvisorData(ctx, e, id, advisorSettings(), true, true, true, false); err != nil {
		return err
	}
	want := []string{"config.work_mem_oversized", "index.unused", "table.dead_tuples_high"}
	// The autovacuum-off condition is a table reloption, not the global GUC;
	// it intentionally does not add vacuum.disabled to this exact set.
	if err := waitAdvisorExact(ctx, e, id, want); err != nil {
		return err
	}
	e.AssertInvariants(e.T)
	return nil
}

func runAdvisorDegraded(ctx context.Context, e *scenario.Env) error {
	id, err := advisorInstanceID(ctx, e, "pg-standby")
	if err != nil {
		return err
	}
	if err := e.Compose("stop", "pglens-agent"); err != nil {
		return fmt.Errorf("stop agent before deterministic degraded seed: %w", err)
	}
	settings := advisorSettings()
	settings["fsync"] = "off"
	if err := seedAdvisorData(ctx, e, id, settings, false, false, false, true); err != nil {
		return err
	}
	if err := seedAdvisorSkip(ctx, e, id, "stat_statements", "missing extension pg_stat_statements"); err != nil {
		return err
	}
	return waitAdvisorDegraded(ctx, e, id)
}

func runAdvisorFix(ctx context.Context, e *scenario.Env) error {
	id, err := advisorInstanceID(ctx, e, "pg")
	if err != nil {
		return err
	}
	if err := e.Compose("stop", "pglens-agent"); err != nil {
		return fmt.Errorf("stop agent before advisor lifecycle seed: %w", err)
	}
	if err := prepareAdvisorRelation(ctx, e.PG("pg")); err != nil {
		return err
	}
	if err := seedAdvisorData(ctx, e, id, advisorSettings(), true, true, true, false); err != nil {
		return err
	}
	want := []string{"config.work_mem_oversized", "index.unused", "table.dead_tuples_high"}
	if err := waitAdvisorExact(ctx, e, id, want); err != nil {
		return err
	}
	pg := e.PG("pg")
	if _, err := pg.Exec(ctx, "DROP INDEX IF EXISTS pglens_e2e_advisor_unused_idx"); err != nil {
		return fmt.Errorf("drop advisor index: %w", err)
	}
	if _, err := pg.Exec(ctx, "VACUUM (ANALYZE) pglens_e2e_advisor_probe"); err != nil {
		return fmt.Errorf("vacuum advisor table: %w", err)
	}
	if _, err := pg.Exec(ctx, "ALTER SYSTEM SET work_mem='4MB'"); err != nil {
		return fmt.Errorf("lower work_mem: %w", err)
	}
	if _, err := pg.Exec(ctx, "SELECT pg_reload_conf()"); err != nil {
		return fmt.Errorf("reload work_mem: %w", err)
	}
	fixed := advisorSettings()
	fixed["work_mem"] = "4MB"
	fixed["work_mem_bytes"] = "4194304"
	if err := seedAdvisorData(ctx, e, id, fixed, false, false, false, true); err != nil {
		return err
	}
	if err := waitAdvisorResolved(ctx, e, id, want); err != nil {
		return err
	}
	e.AssertInvariants(e.T)
	return nil
}

func runAdvisorMisconfigured(ctx context.Context, e *scenario.Env) error {
	baseID, err := advisorInstanceID(ctx, e, "pg")
	if err != nil {
		return err
	}
	if err := e.Compose("stop", "pglens-agent"); err != nil {
		return fmt.Errorf("stop agent before misconfiguration seed: %w", err)
	}
	var agentID string
	var baseCluster int64
	var pgVersion int
	if err := e.DB.QueryRow(ctx, `SELECT agent_id::text, cluster_id, pg_version FROM instances WHERE instance_id=$1::uuid`, baseID).Scan(&agentID, &baseCluster, &pgVersion); err != nil {
		return fmt.Errorf("read base instance metadata: %w", err)
	}
	instanceID := uuid.New().String()
	clusterID := baseCluster + 900000000000000
	if _, err := e.DB.Exec(ctx, `INSERT INTO clusters (tenant_id,cluster_id,name,id_source) VALUES ('default',$1,'advisor-misconfigured','manual') ON CONFLICT DO NOTHING`, clusterID); err != nil {
		return fmt.Errorf("create misconfigured cluster: %w", err)
	}
	if _, err := e.DB.Exec(ctx, `INSERT INTO instances (instance_id,tenant_id,cluster_id,agent_id,addr,port,pg_version,role,perm_tier,first_seen,last_seen)
VALUES ($1::uuid,'default',$2,$3::uuid,'advisor-misconfigured',5432,$4,'primary','T0',now(),now())`, instanceID, clusterID, agentID, pgVersion); err != nil {
		return fmt.Errorf("create misconfigured instance: %w", err)
	}
	settings := advisorSettings()
	settings["shared_buffers"] = "8MB"
	settings["shared_buffers_bytes"] = "8388608"
	settings["effective_cache_size"] = "128MB"
	settings["effective_cache_size_bytes"] = "134217728"
	settings["work_mem"] = "4MB"
	settings["work_mem_bytes"] = "4194304"
	settings["fsync"] = "off"
	settings["archive_command"] = "false"
	settings["autovacuum_vacuum_cost_delay"] = "100ms"
	if err := seedAdvisorData(ctx, e, instanceID, settings, false, false, false, true); err != nil {
		return err
	}
	if err := insertAdvisorMetric(ctx, e, instanceID, clusterID, time.Now().UTC(), "pg_archiver_failed_ratio", nil, 1); err != nil {
		return fmt.Errorf("seed archive failure: %w", err)
	}
	want := []string{"archive.failing", "config.effective_cache_size_mismatch", "config.fsync_off", "config.shared_buffers_low"}
	if err := waitAdvisorExact(ctx, e, instanceID, want); err != nil {
		return err
	}
	findings, err := advisorFindings(ctx, e, instanceID, "all")
	if err != nil {
		return err
	}
	for _, finding := range findings {
		if strings.TrimSpace(stringValue(finding["remediation"])) == "" {
			return fmt.Errorf("finding %s has empty remediation", stringValue(finding["rule_id"]))
		}
	}
	e.AssertInvariants(e.T)
	return nil
}

func advisorInstanceID(ctx context.Context, e *scenario.Env, addr string) (string, error) {
	pg := e.PG(addr)
	pgConfig := pg.Config()
	host := pgConfig.ConnConfig.Host
	port := pgConfig.ConnConfig.Port
	deadline := time.NewTimer(advisorWait)
	defer deadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		var id string
		err := e.DB.QueryRow(ctx, `
			SELECT instance_id::text
			  FROM instances
			 WHERE (addr=$1 AND port=$2) OR addr=$3
			 ORDER BY last_seen DESC
			 LIMIT 1`, host, port, addr).Scan(&id)
		if err == nil {
			return id, nil
		}
		lastErr = err
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("resolve instance %s: %w", addr, err)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline.C:
			return "", fmt.Errorf("instance %s has not registered: %w", addr, lastErr)
		case <-ticker.C:
		}
	}
}

func advisorSettings() map[string]string {
	return map[string]string{
		"archive_mode": "on", "archive_command": "true", "archive_timeout_seconds": "0",
		"autovacuum": "on", "autovacuum_max_workers": "3", "autovacuum_vacuum_cost_delay": "0ms",
		"basebackup_seen": "true", "connections_count": "50", "effective_cache_size": "1GB",
		"effective_cache_size_bytes": "1073741824", "fsync": "on", "full_page_writes": "on",
		"max_connections": "100", "pooler_observed": "false", "replication_slot_protects": "true",
		"shared_buffers": "512MB", "shared_buffers_bytes": "536870912", "track_io_timing": "on",
		"wal_keep_size_bytes": "1073741824", "work_mem": "64MB", "work_mem_bytes": "67108864",
	}
}

func advisorSafeMetrics() map[string]float64 {
	return map[string]float64{
		"pg_archiver_failed_ratio": 0, "pg_archiver_last_archived_age_seconds": 0,
		"pg_basebackup_age_days": 1, "pg_checkpoints_requested_ratio": 0,
		"pg_connections_idle_share": .1, "pg_connections_used_ratio": .1,
		"pg_max_datfrozenxid_age": 0, "pg_max_idle_in_txn_seconds": 0,
		"pg_max_xact_age_seconds": 0, "pg_oldest_prepared_xact_seconds": 0,
		"pg_replication_lag_bytes": 0, "pg_vacuum_jobs_running": 0,
		"pg_xact_rollback_ratio": 0,
	}
}

func prepareAdvisorRelation(ctx context.Context, pg *pgxpool.Pool) error {
	if _, err := pg.Exec(ctx, `CREATE TABLE IF NOT EXISTS pglens_e2e_advisor_probe (id bigint PRIMARY KEY, payload text NOT NULL)`); err != nil {
		return fmt.Errorf("create advisor table: %w", err)
	}
	if _, err := pg.Exec(ctx, `ALTER TABLE pglens_e2e_advisor_probe SET (autovacuum_enabled=false, toast.autovacuum_enabled=false)`); err != nil {
		return fmt.Errorf("disable advisor autovacuum: %w", err)
	}
	if _, err := pg.Exec(ctx, `CREATE INDEX IF NOT EXISTS pglens_e2e_advisor_unused_idx ON pglens_e2e_advisor_probe (payload)`); err != nil {
		return fmt.Errorf("create advisor index: %w", err)
	}
	return nil
}

func seedAdvisorData(ctx context.Context, e *scenario.Env, instanceID string, settings map[string]string, resetFindings, deadTable, unusedIndex, fresh bool) error {
	var clusterID int64
	if err := e.DB.QueryRow(ctx, `SELECT cluster_id FROM instances WHERE instance_id=$1::uuid`, instanceID).Scan(&clusterID); err != nil {
		return fmt.Errorf("read advisor cluster: %w", err)
	}
	for _, table := range []string{"object_facts", "metrics", "metrics_statements", "metrics_tables", "metrics_indexes", "metrics_bloat"} {
		if _, err := e.DB.Exec(ctx, "DELETE FROM "+table+" WHERE instance_id=$1::uuid", instanceID); err != nil {
			return fmt.Errorf("clear %s: %w", table, err)
		}
	}
	if resetFindings {
		if _, err := e.DB.Exec(ctx, `DELETE FROM findings WHERE instance_id=$1::uuid`, instanceID); err != nil {
			return fmt.Errorf("clear advisor findings: %w", err)
		}
	}
	now := time.Now().UTC()
	if _, err := e.DB.Exec(ctx, `UPDATE instances SET last_seen=$2 WHERE instance_id=$1::uuid`, instanceID, now); err != nil {
		return fmt.Errorf("refresh advisor instance: %w", err)
	}
	for key, value := range settings {
		if _, err := e.DB.Exec(ctx, `INSERT INTO object_facts (tenant_id,cluster_id,instance_id,datname,kind,key,labels,value_text,first_seen,last_seen,changed_at)
VALUES ('default',$1,$2::uuid,'','setting',$3,'{}'::jsonb,$4,$5,$5,$5)
ON CONFLICT (tenant_id,instance_id,datname,kind,key) DO UPDATE SET value_text=EXCLUDED.value_text,last_seen=EXCLUDED.last_seen,changed_at=EXCLUDED.changed_at`, clusterID, instanceID, key, value, now); err != nil {
			return fmt.Errorf("seed setting %s: %w", key, err)
		}
	}
	metrics := advisorSafeMetrics()
	metrics["host_mem_total_bytes"] = 2 * 1024 * 1024 * 1024
	metrics["host_mem_available_bytes"] = 1 * 1024 * 1024 * 1024
	metrics["host_metrics_source"] = 0
	for name, value := range metrics {
		if err := insertAdvisorMetric(ctx, e, instanceID, clusterID, now, name, nil, value); err != nil {
			return err
		}
	}
	if deadTable {
		if _, err := e.DB.Exec(ctx, `INSERT INTO metrics_tables (ts,tenant_id,cluster_id,instance_id,datname,schemaname,relname,n_live_tup,n_dead_tup,n_mod_since_analyze,last_vacuum,last_autovacuum)
VALUES ($1,'default',$2,$3::uuid,'postgres','public','pglens_e2e_advisor_probe',60000,20000,0,NULL,NULL)`, now, clusterID, instanceID); err != nil {
			return fmt.Errorf("seed dead tuples: %w", err)
		}
	}
	if unusedIndex {
		old := now.Add(-8 * 24 * time.Hour)
		for _, ts := range []time.Time{old, now} {
			if _, err := e.DB.Exec(ctx, `INSERT INTO metrics_indexes (ts,tenant_id,cluster_id,instance_id,datname,schemaname,relname,indexrelname,idx_scan,index_bytes,is_unique,is_primary,is_valid,def_hash)
VALUES ($1,'default',$2,$3::uuid,'postgres','public','pglens_e2e_advisor_probe','pglens_e2e_advisor_unused_idx',0,20971520,false,false,true,'advisor-unused')`, ts, clusterID, instanceID); err != nil {
				return fmt.Errorf("seed unused index: %w", err)
			}
		}
	}
	if fresh {
		if _, err := e.DB.Exec(ctx, `INSERT INTO metrics_tables (ts,tenant_id,cluster_id,instance_id,datname,schemaname,relname,n_live_tup,n_dead_tup,n_mod_since_analyze,last_vacuum,last_autovacuum)
VALUES ($1,'default',$2,$3::uuid,'postgres','public','pglens_e2e_advisor_probe',60000,0,0,$1,NULL)`, now, clusterID, instanceID); err != nil {
			return fmt.Errorf("seed fresh table: %w", err)
		}
	}
	return nil
}

func insertAdvisorMetric(ctx context.Context, e *scenario.Env, instanceID string, clusterID int64, ts time.Time, name string, labels map[string]string, value float64) error {
	if labels == nil {
		labels = map[string]string{}
	}
	raw, err := json.Marshal(labels)
	if err != nil {
		return fmt.Errorf("marshal metric labels: %w", err)
	}
	if _, err := e.DB.Exec(ctx, `INSERT INTO metrics (ts,tenant_id,cluster_id,instance_id,datname,metric,labels,series_id,value)
VALUES ($1,'default',$2,$3::uuid,'',$4,$5::jsonb,hashtext($4 || $3::text || $5)::bigint,$6) ON CONFLICT DO NOTHING`, ts, clusterID, instanceID, name, raw, value); err != nil {
		return fmt.Errorf("seed metric %s: %w", name, err)
	}
	return nil
}

func seedAdvisorSkip(ctx context.Context, e *scenario.Env, instanceID, check, reason string) error {
	var clusterID int64
	if err := e.DB.QueryRow(ctx, `SELECT cluster_id FROM instances WHERE instance_id=$1::uuid`, instanceID).Scan(&clusterID); err != nil {
		return fmt.Errorf("read skip cluster: %w", err)
	}
	now := time.Now().UTC()
	_, err := e.DB.Exec(ctx, `INSERT INTO object_facts (tenant_id,cluster_id,instance_id,datname,kind,key,labels,value_text,first_seen,last_seen,changed_at)
VALUES ('default',$1,$2::uuid,'','check_skip',$3,'{}'::jsonb,$4,$5,$5,$5)
ON CONFLICT (tenant_id,instance_id,datname,kind,key) DO UPDATE SET value_text=EXCLUDED.value_text,last_seen=EXCLUDED.last_seen,changed_at=EXCLUDED.changed_at`, clusterID, instanceID, check, reason, now)
	if err != nil {
		return fmt.Errorf("seed %s skip: %w", check, err)
	}
	return nil
}

func advisorFindings(ctx context.Context, e *scenario.Env, instanceID, state string) ([]map[string]interface{}, error) {
	value, err := e.API.Request(http.MethodGet, "/api/v1/findings?instance_id="+instanceID+"&state="+state+"&limit=500", nil)
	if err != nil {
		return nil, err
	}
	rows, ok := value.([]interface{})
	if !ok {
		return nil, fmt.Errorf("findings returned %T", value)
	}
	out := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		finding, ok := row.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("finding returned %T", row)
		}
		out = append(out, finding)
	}
	return out, nil
}

func waitAdvisorExact(ctx context.Context, e *scenario.Env, instanceID string, want []string) error {
	want = append([]string(nil), want...)
	sort.Strings(want)
	deadline := time.NewTimer(advisorWait)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var last []string
	for {
		findings, err := advisorFindings(ctx, e, instanceID, "")
		if err == nil {
			last = advisorIDs(findings)
			open := true
			for _, finding := range findings {
				if stringValue(finding["state"]) != "open" {
					open = false
					break
				}
			}
			if open && equalStrings(last, want) {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("advisor exact set mismatch: got %v want %v (last error: %v)", last, want, err)
		case <-ticker.C:
		}
	}
}

func waitAdvisorDegraded(ctx context.Context, e *scenario.Env, instanceID string) error {
	wantQueries := map[string]bool{"query.slow_mean": true, "query.total_time_share": true, "query.regression": true, "query.temp_bytes_high": true, "query.cache_miss_high": true}
	deadline := time.NewTimer(advisorWait)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	lastSeen := map[string]bool{}
	var lastErr error
	for {
		findings, err := advisorFindings(ctx, e, instanceID, "")
		lastErr = err
		if err == nil {
			configOpen := false
			seen := map[string]bool{}
			lastSeen = seen
			openQuery := false
			for _, finding := range findings {
				id := stringValue(finding["rule_id"])
				if id == "config.fsync_off" && stringValue(finding["state"]) == "open" {
					configOpen = true
				}
				if wantQueries[id] {
					if stringValue(finding["state"]) == "open" {
						openQuery = true
					}
					if stringValue(finding["state"]) == "degraded" && strings.Contains(stringValue(finding["degraded_reason"]), "missing extension pg_stat_statements") {
						seen[id] = true
					}
				}
			}
			if configOpen && len(seen) == len(wantQueries) && !openQuery {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("advisor degraded query set incomplete: seen=%v error=%v", lastSeen, lastErr)
		case <-ticker.C:
		}
	}
}

func waitAdvisorResolved(ctx context.Context, e *scenario.Env, instanceID string, ids []string) error {
	deadline := time.NewTimer(advisorWait)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		findings, err := advisorFindings(ctx, e, instanceID, "all")
		if err == nil {
			states := map[string]string{}
			for _, finding := range findings {
				states[stringValue(finding["rule_id"])] = stringValue(finding["state"])
			}
			resolved := true
			for _, id := range ids {
				if states[id] != "resolved" {
					resolved = false
				}
			}
			if resolved {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("advisor findings did not resolve: %v", err)
		case <-ticker.C:
		}
	}
}

func advisorIDs(findings []map[string]interface{}) []string {
	ids := make([]string, 0, len(findings))
	for _, finding := range findings {
		ids = append(ids, stringValue(finding["rule_id"]))
	}
	sort.Strings(ids)
	return ids
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func stringValue(value interface{}) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
