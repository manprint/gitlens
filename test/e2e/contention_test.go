//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/manprint/pglens/test/harness"
	"github.com/manprint/pglens/test/scenario"
)

const contentionProbeTable = "pglens_e2e_contention_probe"

func init() {
	scenario.Register(scenario.Scenario{
		ID:          "SYS-LOCK-001",
		Title:       "SELECT FOR UPDATE blocker is visible and clears",
		Topology:    scenario.TopologyStandalone,
		EstDuration: 2 * time.Minute,
		Covers:      []string{"phase_10.md#9.3", "IDEA.md#5"},
		Expect:      scenario.Expectations{Invariants: []string{"I-1", "I-2", "I-3", "I-4"}},
		Run:         runLockContention,
	})
	scenario.Register(scenario.Scenario{
		ID:          "SYS-DEADLOCK-001",
		Title:       "PostgreSQL deadlock increments the database counter once",
		Topology:    scenario.TopologyStandalone,
		EstDuration: 3 * time.Minute,
		Covers:      []string{"phase_10.md#9.3", "IDEA.md#5"},
		Expect:      scenario.Expectations{Invariants: []string{"I-1", "I-2", "I-3", "I-4"}},
		Run:         runDeadlock,
	})
}

func runLockContention(ctx context.Context, e *scenario.Env) error {
	instanceID, err := contentionInstance(ctx, e)
	if err != nil {
		return err
	}
	pg := e.PG("pg")
	if err := prepareContentionProbe(ctx, pg); err != nil {
		return err
	}

	holderConn, err := pg.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire lock holder: %w", err)
	}
	waiterConn, err := pg.Acquire(ctx)
	if err != nil {
		holderConn.Release()
		return fmt.Errorf("acquire blocked session: %w", err)
	}
	// The lock assertion may legitimately wait for the next scrape. Create
	// this context with enough lifetime for that wait plus transaction cleanup;
	// a short context created before polling can expire before the holder is
	// released.
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cleanupCancel()
	defer holderConn.Release()
	defer waiterConn.Release()

	var holderPID, waiterPID int32
	if err := holderConn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&holderPID); err != nil {
		return fmt.Errorf("read holder pid: %w", err)
	}
	if err := waiterConn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&waiterPID); err != nil {
		return fmt.Errorf("read blocked pid: %w", err)
	}
	holderTx, err := holderConn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin holder transaction: %w", err)
	}
	waiterTx, err := waiterConn.Begin(ctx)
	if err != nil {
		_ = holderTx.Rollback(cleanupCtx)
		return fmt.Errorf("begin blocked transaction: %w", err)
	}

	const holderMarker = "pglens SYS-LOCK-001 holder"
	const waiterMarker = "pglens SYS-LOCK-001 blocked"
	if _, err := holderTx.Exec(ctx, `SELECT value FROM `+contentionProbeTable+` WHERE id=1 FOR UPDATE /* `+holderMarker+` */`); err != nil {
		_ = waiterTx.Rollback(cleanupCtx)
		return fmt.Errorf("take holder row lock: %w", err)
	}
	waiterCtx, waiterCancel := context.WithCancel(ctx)
	defer waiterCancel()
	waiterDone := make(chan error, 1)
	go func() {
		_, queryErr := waiterTx.Exec(waiterCtx, `SELECT value FROM `+contentionProbeTable+` WHERE id=1 FOR UPDATE /* `+waiterMarker+` */`)
		waiterDone <- queryErr
	}()

	lockCtx, cancelLock := context.WithTimeout(ctx, 90*time.Second)
	defer cancelLock()
	if err := waitForBlockedNode(lockCtx, e, instanceID, waiterPID, holderPID, waiterMarker); err != nil {
		_ = holderTx.Rollback(cleanupCtx)
		<-waiterDone
		_ = waiterTx.Rollback(cleanupCtx)
		return err
	}

	if err := holderTx.Rollback(cleanupCtx); err != nil && err != pgx.ErrTxClosed {
		waiterCancel()
		<-waiterDone
		_ = waiterTx.Rollback(cleanupCtx)
		return fmt.Errorf("release holder lock: %w", err)
	}
	select {
	case err := <-waiterDone:
		if err != nil {
			_ = waiterTx.Rollback(cleanupCtx)
			return fmt.Errorf("blocked session did not resume after release: %w", err)
		}
	case <-time.After(10 * time.Second):
		waiterCancel()
		return fmt.Errorf("blocked session pid %d did not resume after holder pid %d released", waiterPID, holderPID)
	}
	if err := waiterTx.Rollback(cleanupCtx); err != nil && err != pgx.ErrTxClosed {
		return fmt.Errorf("cleanup blocked transaction: %w", err)
	}

	clearCtx, cancelClear := context.WithTimeout(ctx, 30*time.Second)
	defer cancelClear()
	if err := waitForNoBlockedNode(clearCtx, e, instanceID, waiterMarker); err != nil {
		return err
	}
	e.AssertInvariants(e.T)
	return nil
}

func runDeadlock(ctx context.Context, e *scenario.Env) error {
	instanceID, err := contentionInstance(ctx, e)
	if err != nil {
		return err
	}
	pg := e.PG("pg")
	if err := prepareContentionProbe(ctx, pg); err != nil {
		return err
	}
	var database string
	var baseline int64
	if err := pg.QueryRow(ctx, `SELECT current_database()`).Scan(&database); err != nil {
		return fmt.Errorf("read database name: %w", err)
	}
	if err := pg.QueryRow(ctx, `SELECT COALESCE(deadlocks, 0) FROM pg_stat_database WHERE datname=current_database()`).Scan(&baseline); err != nil {
		return fmt.Errorf("read deadlock baseline: %w", err)
	}
	// The generic pipeline stores counter deltas as rates. Wait for its first
	// baseline sample so the deliberately-created increment is observable by
	// the alert engine as a positive delta rather than being discarded as the
	// initial counter observation.
	if err := waitForDeadlockMetric(ctx, e, instanceID, database, false); err != nil {
		return fmt.Errorf("wait for deadlock metric baseline: %w", err)
	}

	const ruleID = "e2e.deadlock_database"
	if _, err := e.DB.Exec(ctx, `INSERT INTO alert_rules (rule_id, severity, scope, metric, comparator, threshold, for_seconds, summary)
VALUES ($1, 'warning', 'instance', 'pg_deadlocks_total', 'gt', 0, 0, 'Deadlock observed in database')
ON CONFLICT (tenant_id, rule_id) DO UPDATE SET enabled=true, threshold=0, for_seconds=0, summary=EXCLUDED.summary`, ruleID); err != nil {
		return fmt.Errorf("install deadlock alert rule: %w", err)
	}
	defer func() {
		_, _ = e.DB.Exec(context.Background(), `DELETE FROM alerts WHERE rule_id=$1`, ruleID)
		_, _ = e.DB.Exec(context.Background(), `DELETE FROM alert_rules WHERE rule_id=$1`, ruleID)
	}()

	conn1, err := pg.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire deadlock session 1: %w", err)
	}
	conn2, err := pg.Acquire(ctx)
	if err != nil {
		conn1.Release()
		return fmt.Errorf("acquire deadlock session 2: %w", err)
	}
	defer conn1.Release()
	defer conn2.Release()
	if _, err := conn1.Exec(ctx, `SET deadlock_timeout='100ms'`); err != nil {
		return fmt.Errorf("set deadlock timeout session 1: %w", err)
	}
	if _, err := conn2.Exec(ctx, `SET deadlock_timeout='100ms'`); err != nil {
		return fmt.Errorf("set deadlock timeout session 2: %w", err)
	}
	tx1, err := conn1.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin deadlock transaction 1: %w", err)
	}
	tx2, err := conn2.Begin(ctx)
	if err != nil {
		_ = tx1.Rollback(ctx)
		return fmt.Errorf("begin deadlock transaction 2: %w", err)
	}
	deadlockCtx, deadlockCancel := context.WithTimeout(ctx, 10*time.Second)
	defer deadlockCancel()
	if _, err := tx1.Exec(ctx, `SELECT value FROM `+contentionProbeTable+` WHERE id=1 FOR UPDATE`); err != nil {
		_ = tx2.Rollback(ctx)
		_ = tx1.Rollback(ctx)
		return fmt.Errorf("lock deadlock row 1: %w", err)
	}
	if _, err := tx2.Exec(ctx, `SELECT value FROM `+contentionProbeTable+` WHERE id=2 FOR UPDATE`); err != nil {
		_ = tx2.Rollback(ctx)
		_ = tx1.Rollback(ctx)
		return fmt.Errorf("lock deadlock row 2: %w", err)
	}
	errs := make(chan error, 2)
	go func() {
		_, queryErr := tx1.Exec(deadlockCtx, `SELECT value FROM `+contentionProbeTable+` WHERE id=2 FOR UPDATE /* SYS-DEADLOCK-001 tx1 */`)
		errs <- queryErr
	}()
	go func() {
		_, queryErr := tx2.Exec(deadlockCtx, `SELECT value FROM `+contentionProbeTable+` WHERE id=1 FOR UPDATE /* SYS-DEADLOCK-001 tx2 */`)
		errs <- queryErr
	}()
	err1, err2 := <-errs, <-errs
	_ = tx1.Rollback(context.Background())
	_ = tx2.Rollback(context.Background())
	if (err1 == nil) == (err2 == nil) {
		return fmt.Errorf("deadlock did not choose exactly one victim: tx1=%v tx2=%v", err1, err2)
	}

	counterCtx, cancelCounter := context.WithTimeout(ctx, 120*time.Second)
	defer cancelCounter()
	var after int64
	if err := pollDeadlockCounter(counterCtx, pg, baseline, &after); err != nil {
		return fmt.Errorf("deadlock counter delta: %w", err)
	}
	if after != baseline+1 {
		return fmt.Errorf("deadlock counter changed from %d to %d, want exactly one increment", baseline, after)
	}
	metricCtx, cancelMetric := context.WithTimeout(ctx, 120*time.Second)
	defer cancelMetric()
	if err := waitForDeadlockMetric(metricCtx, e, instanceID, database, true); err != nil {
		return fmt.Errorf("wait for persisted deadlock metric: %w", err)
	}
	alertCtx, cancelAlert := context.WithTimeout(ctx, 120*time.Second)
	defer cancelAlert()
	if err := waitForDatabaseDeadlockAlert(alertCtx, e, instanceID, database, ruleID); err != nil {
		return fmt.Errorf("wait for database deadlock alert: %w", err)
	}
	e.AssertInvariants(e.T)
	return nil
}

func contentionInstance(ctx context.Context, e *scenario.Env) (string, error) {
	var id string
	err := pollContention(ctx, 500*time.Millisecond, func(ctx context.Context) (bool, error) {
		err := e.DB.QueryRow(ctx, `SELECT instance_id::text FROM instances ORDER BY last_seen DESC LIMIT 1`).Scan(&id)
		return err == nil && id != "", err
	})
	if err != nil {
		return "", fmt.Errorf("resolve monitored instance: %w", err)
	}
	return id, nil
}

func prepareContentionProbe(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS `+contentionProbeTable+` (id integer PRIMARY KEY, value integer NOT NULL)`); err != nil {
		return fmt.Errorf("create contention probe: %w", err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE TABLE `+contentionProbeTable); err != nil {
		return fmt.Errorf("truncate contention probe: %w", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO `+contentionProbeTable+` (id, value) VALUES (1, 10), (2, 20)`); err != nil {
		return fmt.Errorf("seed contention probe: %w", err)
	}
	return nil
}

func waitForBlockedNode(ctx context.Context, e *scenario.Env, instanceID string, waiterPID, holderPID int32, marker string) error {
	var observed map[string]interface{}
	err := pollContention(ctx, time.Second, func(ctx context.Context) (bool, error) {
		payload, err := e.API.Get("/api/v1/locks?instance_id=" + instanceID)
		if err != nil {
			return false, err
		}
		if stale, _ := payload["stale"].(bool); stale {
			return false, nil
		}
		for _, node := range lockNodes(payload) {
			if intValue(node["pid"]) != waiterPID || node["wait_event_type"] != "Lock" || !hasPID(node["blocked_by"], holderPID) {
				continue
			}
			query, _ := node["query"].(string)
			if !strings.Contains(query, marker) {
				continue
			}
			if query == "" || len(query) > 2048 {
				return false, fmt.Errorf("blocked query length is %d, want 1..2048", len(query))
			}
			observed = node
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("wait for blocked lock in /api/v1/locks: %w", err)
	}
	if observed == nil {
		return fmt.Errorf("lock API returned no observed blocked node")
	}
	return nil
}

func waitForNoBlockedNode(ctx context.Context, e *scenario.Env, instanceID, marker string) error {
	err := pollContention(ctx, time.Second, func(ctx context.Context) (bool, error) {
		payload, err := e.API.Get("/api/v1/locks?instance_id=" + instanceID)
		if err != nil {
			return false, err
		}
		if stale, _ := payload["stale"].(bool); stale {
			return false, nil
		}
		for _, node := range lockNodes(payload) {
			if query, _ := node["query"].(string); strings.Contains(query, marker) {
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("wait for released lock to disappear: %w", err)
	}
	return nil
}

func lockNodes(payload map[string]interface{}) []map[string]interface{} {
	raw, ok := payload["nodes"].([]interface{})
	if !ok {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(raw))
	for _, value := range raw {
		if node, ok := value.(map[string]interface{}); ok {
			out = append(out, node)
		}
	}
	return out
}

func intValue(value interface{}) int32 {
	switch value := value.(type) {
	case float64:
		return int32(value)
	case int:
		return int32(value)
	case int32:
		return value
	default:
		return 0
	}
}

func hasPID(value interface{}, wanted int32) bool {
	raw, ok := value.([]interface{})
	if !ok {
		return false
	}
	for _, pid := range raw {
		if intValue(pid) == wanted {
			return true
		}
	}
	return false
}

func waitForDeadlockMetric(ctx context.Context, e *scenario.Env, instanceID, database string, positive bool) error {
	return pollContention(ctx, 2*time.Second, func(ctx context.Context) (bool, error) {
		var count int
		query := `SELECT count(*) FROM metrics WHERE instance_id=$1::uuid AND datname=$2 AND metric='pg_deadlocks_total'`
		if positive {
			query += ` AND value > 0`
		}
		if err := e.DB.QueryRow(ctx, query, instanceID, database).Scan(&count); err != nil {
			return false, err
		}
		return count > 0, nil
	})
}

func pollDeadlockCounter(ctx context.Context, pool *pgxpool.Pool, baseline int64, after *int64) error {
	return pollContention(ctx, 500*time.Millisecond, func(ctx context.Context) (bool, error) {
		if err := pool.QueryRow(ctx, `SELECT COALESCE(deadlocks, 0) FROM pg_stat_database WHERE datname=current_database()`).Scan(after); err != nil {
			return false, err
		}
		if *after > baseline+1 {
			return false, fmt.Errorf("counter jumped from %d to %d", baseline, *after)
		}
		return *after == baseline+1, nil
	})
}

func waitForDatabaseDeadlockAlert(ctx context.Context, e *scenario.Env, instanceID, database, ruleID string) error {
	seenDatnames := map[string]struct{}{}
	err := pollContention(ctx, time.Second, func(ctx context.Context) (bool, error) {
		value, err := e.API.Request(http.MethodGet, "/api/v1/alerts?rule_id="+ruleID+"&instance_id="+instanceID+"&state=all", nil)
		if err != nil {
			return false, err
		}
		rows, ok := value.([]interface{})
		if !ok {
			return false, fmt.Errorf("alerts response is %T, want array", value)
		}
		for _, raw := range rows {
			row, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			if datname, ok := row["datname"].(string); ok {
				seenDatnames[datname] = struct{}{}
			}
			if row["datname"] == database {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		debugCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var ruleCount, metricCount, alertCount int
		var metricDatnames, alertDatnames string
		_ = e.DB.QueryRow(debugCtx, `SELECT count(*) FROM alert_rules WHERE rule_id=$1 AND enabled`, ruleID).Scan(&ruleCount)
		_ = e.DB.QueryRow(debugCtx, `SELECT count(*), COALESCE(string_agg(DISTINCT datname, ','), '') FROM metrics WHERE instance_id=$1::uuid AND metric='pg_deadlocks_total' AND value > 0`, instanceID).Scan(&metricCount, &metricDatnames)
		_ = e.DB.QueryRow(debugCtx, `SELECT count(*), COALESCE(string_agg(DISTINCT datname, ','), '') FROM alerts WHERE rule_id=$1 AND instance_id=$2::uuid`, ruleID, instanceID).Scan(&alertCount, &alertDatnames)
		return fmt.Errorf("alert for database %q not observed for rule %s (seen datnames: %v; enabled rules=%d, positive metrics=%d [%s], alerts=%d [%s]): %w", database, ruleID, sortedStrings(seenDatnames), ruleCount, metricCount, metricDatnames, alertCount, alertDatnames, err)
	}
	return nil
}

func sortedStrings(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func pollContention(ctx context.Context, interval time.Duration, check func(context.Context) (bool, error)) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var lastErr error
	for {
		ok, err := check(ctx)
		if err == nil && ok {
			return nil
		}
		if err != nil {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("poll timed out: %w (last error: %v)", ctx.Err(), lastErr)
			}
			return fmt.Errorf("poll timed out: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func TestFull_LockContention(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping L3 lock contention test in short mode")
	}
	h := harness.Start(t, harness.Config{Topology: harness.TopologyStandalone, AgentMode: resolveAgentMode(harness.AgentModeContainer)})
	h.Scenario(t, "SYS-LOCK-001")
}

func TestFull_Deadlock(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping L3 deadlock test in short mode")
	}
	h := harness.Start(t, harness.Config{Topology: harness.TopologyStandalone, AgentMode: resolveAgentMode(harness.AgentModeContainer)})
	h.Scenario(t, "SYS-DEADLOCK-001")
}
