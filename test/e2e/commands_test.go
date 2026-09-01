//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/manprint/pglens/test/harness"
	"github.com/manprint/pglens/test/scenario"
)

const commandProbeTable = "pglens_e2e_command_probe"

func init() {
	registerCommandScenario("SYS-CMD-001", "command plans are accepted and deduplicated", runCommandPlans)
	registerCommandScenario("SYS-CMD-002", "command restrictions are audited without unsafe side effects", runCommandGates)
	registerCommandScenario("SYS-CMD-003", "claimed commands are at-most-once across restart", runCommandRestart)
	registerCommandScenario("SYS-CMD-004", "expired commands are never executed", runCommandExpiry)
}

func registerCommandScenario(id, title string, run func(context.Context, *scenario.Env) error) {
	scenario.Register(scenario.Scenario{
		ID:          id,
		Title:       title,
		Topology:    scenario.TopologyPrimaryStandby,
		EstDuration: 2 * time.Minute,
		Covers:      []string{"phase_10.md#9.6", "I-5"},
		Run: func(ctx context.Context, e *scenario.Env) error {
			if err := run(ctx, e); err != nil {
				return err
			}
			e.AssertInvariants(e.T)
			return nil
		},
		Expect: scenario.Expectations{Invariants: []string{"command execution is gated, audited, and at-most-once"}},
	})
}

func TestFull_CommandPlans(t *testing.T)   { runCommandScenario(t, "SYS-CMD-001") }
func TestFull_CommandGates(t *testing.T)   { runCommandScenario(t, "SYS-CMD-002") }
func TestFull_CommandRestart(t *testing.T) { runCommandScenario(t, "SYS-CMD-003") }
func TestFull_CommandExpiry(t *testing.T)  { runCommandScenario(t, "SYS-CMD-004") }

func runCommandScenario(t *testing.T, id string) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping L3 command-channel test in short mode")
	}
	h := harness.Start(t, harness.Config{
		Topology:  harness.TopologyPrimaryStandby,
		AgentMode: resolveAgentMode(harness.AgentModeContainer),
	})
	h.Scenario(t, id)
}

func runCommandPlans(ctx context.Context, e *scenario.Env) error {
	instanceID, err := commandInstance(ctx, e, "pg-primary")
	if err != nil {
		return err
	}
	pg := e.PG("pg-primary")
	if _, err := pg.Exec(ctx, "DROP TABLE IF EXISTS "+commandProbeTable); err != nil {
		return fmt.Errorf("drop command probe: %w", err)
	}
	if _, err := pg.Exec(ctx, "CREATE TABLE "+commandProbeTable+" (id bigint NOT NULL, payload text NOT NULL)"); err != nil {
		return fmt.Errorf("create command probe: %w", err)
	}
	if _, err := pg.Exec(ctx, "INSERT INTO "+commandProbeTable+" SELECT i, repeat('x', 128) FROM generate_series(1, 200000) AS s(i)"); err != nil {
		return fmt.Errorf("seed command probe: %w", err)
	}
	if _, err := pg.Exec(ctx, "ANALYZE "+commandProbeTable); err != nil {
		return fmt.Errorf("analyze command probe: %w", err)
	}
	if err := exerciseCommandProbe(ctx, pg); err != nil {
		return err
	}
	queryID, err := waitCommandQueryID(ctx, e, instanceID)
	if err != nil {
		return err
	}

	commandID, err := enqueueCommand(e, instanceID, map[string]any{
		"kind": "explain", "args": map[string]any{"queryid": queryID, "datname": "postgres"},
	})
	if err != nil {
		return err
	}
	if _, err := waitCommandTerminal(ctx, e, commandID, "done"); err != nil {
		return err
	}
	plans, err := waitPlans(ctx, e, instanceID, queryID, 1)
	if err != nil {
		return err
	}
	if got := planRootNodeType(plans[0]["plan"]); got == "" {
		return fmt.Errorf("stored plan has no visible root node type")
	}

	duplicateID, err := enqueueCommand(e, instanceID, map[string]any{
		"kind": "explain", "args": map[string]any{"queryid": queryID, "datname": "postgres"},
	})
	if err != nil {
		return err
	}
	if _, err := waitCommandTerminal(ctx, e, duplicateID, "done"); err != nil {
		return err
	}
	plans, err = waitPlans(ctx, e, instanceID, queryID, 1)
	if err != nil {
		return err
	}

	if _, err := pg.Exec(ctx, "CREATE INDEX "+commandProbeTable+"_id_idx ON "+commandProbeTable+" (id)"); err != nil {
		return fmt.Errorf("create command probe index: %w", err)
	}
	if _, err := pg.Exec(ctx, "ANALYZE "+commandProbeTable); err != nil {
		return fmt.Errorf("reanalyze command probe: %w", err)
	}
	if err := exerciseCommandProbe(ctx, pg); err != nil {
		return err
	}
	changedID, err := enqueueCommand(e, instanceID, map[string]any{
		"kind": "explain", "args": map[string]any{"queryid": queryID, "datname": "postgres"},
	})
	if err != nil {
		return err
	}
	if _, err := waitCommandTerminal(ctx, e, changedID, "done"); err != nil {
		return err
	}
	plans, err = waitPlans(ctx, e, instanceID, queryID, 2)
	if err != nil {
		return err
	}
	if changed, ok := plans[0]["changed"].(bool); !ok || !changed {
		return fmt.Errorf("newer plan row did not report changed=true")
	}
	if planRootNodeType(plans[0]["plan"]) == planRootNodeType(plans[1]["plan"]) {
		return fmt.Errorf("forced plan did not change root node type")
	}
	return nil
}

func runCommandGates(ctx context.Context, e *scenario.Env) error {
	instanceID, err := commandInstance(ctx, e, "pg-standby")
	if err != nil {
		return err
	}
	pg := e.PG("pg-standby")
	var before int64
	if err := pg.QueryRow(ctx, "SELECT count(*) FROM pg_stat_statements WHERE query LIKE 'EXPLAIN %'").Scan(&before); err != nil {
		return fmt.Errorf("count explain statements before rejection: %w", err)
	}

	conn, err := pg.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire sleeper connection: %w", err)
	}
	var pid int
	if err := conn.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		conn.Release()
		return fmt.Errorf("get sleeper pid: %w", err)
	}
	sleepCtx, cancelSleep := context.WithTimeout(context.Background(), 30*time.Second)
	sleepDone := make(chan error, 1)
	go func() {
		_, execErr := conn.Exec(sleepCtx, "SELECT pg_sleep(20)")
		sleepDone <- execErr
	}()
	defer func() {
		cancelSleep()
		conn.Release()
	}()

	requests := []struct {
		kind string
		args map[string]any
		gate string
	}{
		{kind: "explain", args: map[string]any{"queryid": int64(1), "analyze": true}, gate: "EXPLAIN ANALYZE"},
		{kind: "cancel", args: map[string]any{"pid": pid}, gate: "permission denied"},
		{kind: "pgstattuple", args: map[string]any{"schema": "public", "relation": commandProbeTable}, gate: "does not exist"},
	}
	for _, request := range requests {
		commandID, enqueueErr := enqueueCommand(e, instanceID, map[string]any{"kind": request.kind, "args": request.args})
		if enqueueErr != nil {
			return enqueueErr
		}
		terminal, waitErr := waitCommandTerminal(ctx, e, commandID, "failed")
		if waitErr != nil {
			return waitErr
		}
		if !strings.Contains(strings.ToLower(commandStringValue(terminal["error"])), strings.ToLower(request.gate)) {
			return fmt.Errorf("%s rejection did not name closed gate: %v", request.kind, terminal["error"])
		}
	}
	var stillRunning bool
	if err := pg.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND query LIKE '%pg_sleep%')", pid).Scan(&stillRunning); err != nil {
		return fmt.Errorf("check rejected cancel target: %w", err)
	}
	if !stillRunning {
		return fmt.Errorf("rejected cancel unexpectedly stopped the target session")
	}
	var after int64
	if err := pg.QueryRow(ctx, "SELECT count(*) FROM pg_stat_statements WHERE query LIKE 'EXPLAIN %'").Scan(&after); err != nil {
		return fmt.Errorf("count explain statements after rejection: %w", err)
	}
	if after != before {
		return fmt.Errorf("rejected explain changed pg_stat_statements: before=%d after=%d", before, after)
	}
	audit, err := commandAudit(ctx, e, instanceID)
	if err != nil {
		return err
	}
	if len(audit) != 3 {
		return fmt.Errorf("restrictive instance audit rows=%d, want 3", len(audit))
	}
	wantOutcomes := map[string]string{
		"explain":     "rejected",
		"cancel":      "error",
		"pgstattuple": "error",
	}
	for _, item := range audit {
		kind := commandStringValue(item["kind"])
		if want, ok := wantOutcomes[kind]; !ok || commandStringValue(item["outcome"]) != want {
			return fmt.Errorf("restrictive audit row=%v, want %s outcome=%s", item, kind, wantOutcomes[kind])
		}
	}
	return nil
}

func runCommandRestart(ctx context.Context, e *scenario.Env) error {
	instanceID, err := commandInstance(ctx, e, "pg-primary")
	if err != nil {
		return err
	}
	pg := e.PG("pg-primary")
	if _, err := pg.Exec(ctx, "CREATE TABLE IF NOT EXISTS "+commandProbeTable+" (id bigint NOT NULL, payload text NOT NULL)"); err != nil {
		return fmt.Errorf("create restart probe: %w", err)
	}
	if _, err := pg.Exec(ctx, "INSERT INTO "+commandProbeTable+" SELECT i, repeat(md5(i::text), 64) FROM generate_series(1, 500000) AS s(i) ON CONFLICT DO NOTHING"); err != nil {
		return fmt.Errorf("seed restart probe: %w", err)
	}
	lockConn, err := pg.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire restart lock connection: %w", err)
	}
	defer lockConn.Release()
	if _, err := lockConn.Exec(ctx, "BEGIN"); err != nil {
		return fmt.Errorf("begin restart lock: %w", err)
	}
	defer func() { _, _ = lockConn.Exec(context.Background(), "ROLLBACK") }()
	if _, err := lockConn.Exec(ctx, "LOCK TABLE "+commandProbeTable+" IN ACCESS EXCLUSIVE MODE"); err != nil {
		return fmt.Errorf("lock restart probe: %w", err)
	}
	commandID, err := enqueueCommand(e, instanceID, map[string]any{
		"kind": "pgstattuple", "args": map[string]any{"schema": "public", "relation": commandProbeTable},
	})
	if err != nil {
		return err
	}
	claimToken, err := waitClaimToken(ctx, e, commandID)
	if err != nil {
		return err
	}
	if err := e.Compose("kill", "pglens-agent"); err != nil {
		return fmt.Errorf("kill agent after claim: %w", err)
	}
	if _, err := lockConn.Exec(ctx, "ROLLBACK"); err != nil {
		return fmt.Errorf("release restart lock: %w", err)
	}
	time.Sleep(6 * time.Second)
	if err := e.Compose("up", "-d", "pglens-agent"); err != nil {
		return fmt.Errorf("restart agent: %w", err)
	}
	terminal, err := waitCommandTerminal(ctx, e, commandID, "expired")
	if err != nil {
		return err
	}
	if commandStringValue(terminal["state"]) != "expired" {
		return fmt.Errorf("restart replayed command with state=%v", terminal["state"])
	}
	requester, ok := e.API.(interface {
		RequestWithHeaders(string, string, []byte, map[string]string) (interface{}, int, error)
	})
	if !ok {
		return fmt.Errorf("harness API lacks authenticated request support")
	}
	stalePayload, _ := json.Marshal(map[string]any{"claim_token": claimToken, "outcome": "ok", "result": map[string]any{}})
	_, status, staleErr := requester.RequestWithHeaders(http.MethodPost, "/api/v1/commands/"+commandID+"/result", stalePayload, map[string]string{"Authorization": "Bearer dev-token"})
	if staleErr == nil || status != http.StatusConflict {
		return fmt.Errorf("stale claim result status=%d err=%v, want 409", status, staleErr)
	}
	audit, err := commandAudit(ctx, e, instanceID)
	if err != nil {
		return err
	}
	if len(audit) != 1 || commandStringValue(audit[0]["outcome"]) != "rejected" {
		return fmt.Errorf("restart command audit=%v, want one rejected stale-result row", audit)
	}
	return nil
}

func runCommandExpiry(ctx context.Context, e *scenario.Env) error {
	instanceID, err := commandInstance(ctx, e, "pg-primary")
	if err != nil {
		return err
	}
	if err := e.Compose("stop", "pglens-agent"); err != nil {
		return fmt.Errorf("stop agent before expiry: %w", err)
	}
	commandID, err := enqueueCommand(e, instanceID, map[string]any{
		"kind": "pgstattuple", "args": map[string]any{"schema": "public", "relation": commandProbeTable},
	})
	if err != nil {
		return err
	}
	time.Sleep(6 * time.Second)
	if err := e.Compose("start", "pglens-agent"); err != nil {
		return fmt.Errorf("start agent after expiry: %w", err)
	}
	terminal, err := waitCommandTerminal(ctx, e, commandID, "expired")
	if err != nil {
		return err
	}
	if commandStringValue(terminal["state"]) != "expired" {
		return fmt.Errorf("expired command state=%v", terminal["state"])
	}
	audit, err := commandAudit(ctx, e, instanceID)
	if err != nil {
		return err
	}
	if len(audit) != 0 {
		return fmt.Errorf("never-delivered expired command created audit rows: %d", len(audit))
	}
	return nil
}

func commandInstance(ctx context.Context, e *scenario.Env, addr string) (string, error) {
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		var id string
		err := e.DB.QueryRow(ctx, "SELECT instance_id::text FROM instances WHERE addr=$1 OR addr LIKE '%' || $1 || '%' ORDER BY last_seen DESC LIMIT 1", addr).Scan(&id)
		if err == nil {
			return id, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return "", fmt.Errorf("find %s instance: timed out waiting for agent snapshot", addr)
}

func exerciseCommandProbe(ctx context.Context, pg *pgxpool.Pool) error {
	for i := 0; i < 5; i++ {
		if _, err := pg.Exec(ctx, "SELECT min(id) FROM "+commandProbeTable); err != nil {
			return fmt.Errorf("exercise command probe: %w", err)
		}
	}
	return nil
}

func enqueueCommand(e *scenario.Env, instanceID string, payload map[string]any) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	raw, err := e.API.Request(http.MethodPost, "/api/v1/instances/"+instanceID+"/commands", body)
	if err != nil {
		return "", fmt.Errorf("enqueue command: %w", err)
	}
	response, ok := raw.(map[string]any)
	if !ok || commandStringValue(response["command_id"]) == "" {
		return "", fmt.Errorf("enqueue response=%v", raw)
	}
	return commandStringValue(response["command_id"]), nil
}

func waitCommandQueryID(ctx context.Context, e *scenario.Env, instanceID string) (int64, error) {
	exactGetter, ok := e.API.(interface {
		GetExact(string) (interface{}, error)
	})
	if !ok {
		return 0, fmt.Errorf("harness API lacks lossless JSON number support")
	}
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		raw, err := exactGetter.GetExact("/api/v1/statements?instance_id=" + instanceID + "&limit=100")
		if err == nil {
			response, responseOK := raw.(map[string]interface{})
			if !responseOK {
				time.Sleep(500 * time.Millisecond)
				continue
			}
			if statements, ok := response["statements"].([]interface{}); ok {
				for _, raw := range statements {
					item, ok := raw.(map[string]any)
					if !ok {
						continue
					}
					queryText := strings.TrimSpace(commandStringValue(item["query_text"]))
					if strings.HasPrefix(strings.ToUpper(queryText), "SELECT") &&
						strings.Contains(queryText, commandProbeTable) &&
						!strings.Contains(queryText, "generate_series") {
						if rawQueryID := strings.TrimSpace(commandStringValue(item["queryid"])); rawQueryID != "" {
							if parsed, parseErr := strconv.ParseInt(rawQueryID, 10, 64); parseErr == nil {
								return parsed, nil
							}
						}
					}
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return 0, fmt.Errorf("timed out waiting for command probe queryid")
}

func waitCommandTerminal(ctx context.Context, e *scenario.Env, commandID, want string) (map[string]any, error) {
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		response, err := e.API.Get("/api/v1/commands/" + commandID)
		if err == nil {
			state := commandStringValue(response["state"])
			if state == want {
				return response, nil
			}
			if state == "done" || state == "failed" || state == "expired" {
				return nil, fmt.Errorf("command %s reached state=%s, want %s: %v", commandID, state, want, response["error"])
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return nil, fmt.Errorf("timed out waiting for command %s to reach %s", commandID, want)
}

func waitPlans(ctx context.Context, e *scenario.Env, instanceID string, queryID int64, want int) ([]map[string]any, error) {
	deadline := time.Now().Add(2 * time.Minute)
	path := "/api/v1/plans?queryid=" + strconv.FormatInt(queryID, 10) + "&instance_id=" + instanceID + "&limit=10"
	for time.Now().Before(deadline) {
		response, err := e.API.Get(path)
		if err == nil {
			if total, ok := response["total_shapes"].(float64); ok && int(total) >= want {
				items, ok := response["plans"].([]any)
				if !ok {
					return nil, fmt.Errorf("plans response has invalid plans=%T", response["plans"])
				}
				out := make([]map[string]any, 0, len(items))
				for _, item := range items {
					if plan, ok := item.(map[string]any); ok {
						out = append(out, plan)
					}
				}
				if len(out) >= want {
					return out, nil
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil, fmt.Errorf("timed out waiting for %d plan shapes", want)
}

func waitClaimToken(ctx context.Context, e *scenario.Env, commandID string) (string, error) {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var token *string
		var state, commandError string
		err := e.DB.QueryRow(ctx, "SELECT claim_token::text, state, COALESCE(error, '') FROM commands WHERE command_id=$1", commandID).Scan(&token, &state, &commandError)
		if err == nil && token != nil && *token != "" && state == "claimed" {
			return *token, nil
		}
		if state == "done" || state == "failed" || state == "expired" {
			return "", fmt.Errorf("command reached state=%s before claim capture: %s", state, commandError)
		}
		time.Sleep(200 * time.Millisecond)
	}
	return "", fmt.Errorf("timed out waiting for command claim")
}

func commandAudit(ctx context.Context, e *scenario.Env, instanceID string) ([]map[string]any, error) {
	raw, err := e.API.Request(http.MethodGet, "/api/v1/instances/"+instanceID+"/command-audit?limit=100", nil)
	if err != nil {
		return nil, fmt.Errorf("get command audit: %w", err)
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("command audit response=%T", raw)
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if row, ok := item.(map[string]any); ok {
			out = append(out, row)
		}
	}
	return out, nil
}

func planRootNodeType(plan any) string {
	entries, ok := plan.([]any)
	if !ok || len(entries) == 0 {
		return ""
	}
	entry, ok := entries[0].(map[string]any)
	if !ok {
		return ""
	}
	root, ok := entry["Plan"].(map[string]any)
	if !ok {
		return ""
	}
	return commandStringValue(root["Node Type"])
}

func commandStringValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
