//go:build e2e

package scenario

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func init() {
	Register(Scenario{
		ID:       "SYS-ALERT-001",
		Title:    "agent-down alert fires and resolves exactly once",
		Topology: TopologyStandalone,
		Covers:   []string{"phase_10.md#9.2", "I-4"},
		Expect:   Expectations{Events: []string{"agent_down"}, Invariants: []string{"I-4"}},
		Run:      runAlertEpisode,
	})
	Register(Scenario{
		ID:       "SYS-ALERT-002",
		Title:    "webhook retries still deliver one notification",
		Topology: TopologyStandalone,
		Covers:   []string{"phase_10.md#9.2", "I-4"},
		Expect:   Expectations{Invariants: []string{"I-4"}},
		Run:      runAlertRetry,
	})
	Register(Scenario{
		ID:       "SYS-ALERT-003",
		Title:    "alert deduplication survives server leader failover",
		Topology: TopologyStandalone,
		Covers:   []string{"phase_10.md#9.2", "I-4"},
		Expect:   Expectations{Invariants: []string{"I-4"}},
		Run:      runAlertLeaderFailover,
	})
	Register(Scenario{
		ID:       "SYS-ALERT-004",
		Title:    "silence suppresses delivery without hiding the firing alert",
		Topology: TopologyStandalone,
		Covers:   []string{"phase_10.md#9.2", "I-4"},
		Expect:   Expectations{Invariants: []string{"I-4"}},
		Run:      runAlertSilence,
	})
}

type receivedNotification struct {
	RuleID   string `json:"rule_id"`
	Phase    string `json:"phase"`
	Instance string `json:"instance"`
}

func receiverCall(ctx context.Context, base, method, path string, body []byte) ([]receivedNotification, error) {
	var r io.Reader
	if len(body) != 0 {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, r)
	if err != nil {
		return nil, err
	}
	if len(body) != 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("receiver %s %s: %d %s", method, path, resp.StatusCode, data)
	}
	if path != "/_received" {
		return nil, nil
	}
	var out []receivedNotification
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func receiverReset(ctx context.Context, base string) error {
	_, err := receiverCall(ctx, base, http.MethodPost, "/_reset", nil)
	return err
}

func receiverFailNext(ctx context.Context, base string, n int) error {
	_, err := receiverCall(ctx, base, http.MethodPost, fmt.Sprintf("/_fail_next?n=%d", n), nil)
	return err
}

func alertInstance(ctx context.Context, e *Env) (string, error) {
	var id string
	err := poll(ctx, 500*time.Millisecond, func(ctx context.Context) (bool, error) {
		err := e.DB.QueryRow(ctx, `SELECT instance_id::text FROM instances ORDER BY last_seen DESC LIMIT 1`).Scan(&id)
		return err == nil && id != "", err
	})
	return id, err
}

func alertRows(ctx context.Context, e *Env, instanceID, state string) ([]map[string]interface{}, error) {
	path := "/api/v1/alerts?rule_id=agent_down&instance_id=" + instanceID + "&state=" + state
	value, err := e.API.Request(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	rows, ok := value.([]interface{})
	if !ok {
		return nil, fmt.Errorf("alerts response is %T, want array", value)
	}
	out := make([]map[string]interface{}, 0, len(rows))
	for _, raw := range rows {
		row, ok := raw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid alert row %T", raw)
		}
		out = append(out, row)
	}
	return out, nil
}

func waitForAlertState(ctx context.Context, e *Env, instanceID, state string) ([]map[string]interface{}, error) {
	var rows []map[string]interface{}
	err := poll(ctx, 500*time.Millisecond, func(ctx context.Context) (bool, error) {
		var err error
		rows, err = alertRows(ctx, e, instanceID, state)
		return err == nil && len(rows) > 0, err
	})
	return rows, err
}

func notificationCount(ctx context.Context, e *Env, instanceID, phase string) (int, error) {
	var count int
	// notifications.alert_key stores the stable delivery id (the alert key
	// plus started_at), not alerts.alert_key itself. Match the stable prefix
	// and keep the phase/channel predicates in SQL so retries cannot inflate
	// the count.
	err := e.DB.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE alert_key LIKE 'agent_down/' || $1 || '/%' AND channel='webhook' AND phase=$2`, instanceID, phase).Scan(&count)
	return count, err
}

func runAlertEpisode(ctx context.Context, e *Env) error {
	if e.MockReceiverURL == "" {
		return fmt.Errorf("alerting scenario started without a mock receiver")
	}
	if err := receiverReset(ctx, e.MockReceiverURL); err != nil {
		return err
	}
	instanceID, err := alertInstance(ctx, e)
	if err != nil {
		return fmt.Errorf("resolve monitored instance: %w", err)
	}
	if err := e.Compose("stop", "pglens-agent"); err != nil {
		return fmt.Errorf("stop agent: %w", err)
	}
	e.T.Cleanup(func() { _ = e.Compose("start", "pglens-agent") })
	firingCtx, cancel := context.WithTimeout(ctx, 150*time.Second)
	defer cancel()
	if _, err := waitForAlertState(firingCtx, e, instanceID, "firing"); err != nil {
		return fmt.Errorf("agent_down did not fire: %w", err)
	}
	var fire int
	if err := poll(ctx, 500*time.Millisecond, func(ctx context.Context) (bool, error) {
		var err error
		fire, err = notificationCount(ctx, e, instanceID, "fire")
		return err == nil && fire == 1, err
	}); err != nil {
		return fmt.Errorf("fire notification count: %w", err)
	}
	if err := e.Compose("start", "pglens-agent"); err != nil {
		return fmt.Errorf("restart agent: %w", err)
	}
	if err := poll(ctx, 500*time.Millisecond, func(ctx context.Context) (bool, error) {
		var state string
		err := e.DB.QueryRow(ctx, `SELECT state FROM alerts WHERE rule_id='agent_down' AND instance_id=$1::uuid ORDER BY started_at DESC LIMIT 1`, instanceID).Scan(&state)
		return err == nil && state == "resolved", err
	}); err != nil {
		return fmt.Errorf("agent_down did not resolve: %w", err)
	}
	var resolve int
	if err := poll(ctx, 500*time.Millisecond, func(ctx context.Context) (bool, error) {
		var err error
		resolve, err = notificationCount(ctx, e, instanceID, "resolve")
		return err == nil && resolve == 1, err
	}); err != nil {
		return fmt.Errorf("resolve notification count: %w", err)
	}
	bodies, err := receiverCall(ctx, e.MockReceiverURL, http.MethodGet, "/_received", nil)
	if err != nil {
		return err
	}
	if len(bodies) != 2 || bodies[0].RuleID != "agent_down" || bodies[0].Phase != "fire" || bodies[1].Phase != "resolve" {
		return fmt.Errorf("want exactly fire+resolve notifications, got %#v", bodies)
	}
	e.AssertInvariants(e.T)
	return nil
}

func runAlertRetry(ctx context.Context, e *Env) error {
	if err := receiverReset(ctx, e.MockReceiverURL); err != nil {
		return err
	}
	if err := receiverFailNext(ctx, e.MockReceiverURL, 2); err != nil {
		return err
	}
	instanceID, err := alertInstance(ctx, e)
	if err != nil {
		return err
	}
	if err := e.Compose("stop", "pglens-agent"); err != nil {
		return err
	}
	e.T.Cleanup(func() { _ = e.Compose("start", "pglens-agent") })
	firingCtx, cancel := context.WithTimeout(ctx, 150*time.Second)
	defer cancel()
	if _, err := waitForAlertState(firingCtx, e, instanceID, "firing"); err != nil {
		return err
	}
	if err := poll(ctx, 500*time.Millisecond, func(ctx context.Context) (bool, error) {
		n, err := notificationCount(ctx, e, instanceID, "fire")
		return err == nil && n == 1, err
	}); err != nil {
		return fmt.Errorf("retry notification not sent: %w", err)
	}
	var ok bool
	var attempts int
	if err := poll(ctx, 500*time.Millisecond, func(ctx context.Context) (bool, error) {
		err := e.DB.QueryRow(ctx, `SELECT ok,attempts FROM notifications WHERE alert_key LIKE 'agent_down/' || $1 || '/%' AND channel='webhook' AND phase='fire'`, instanceID).Scan(&ok, &attempts)
		return err == nil && ok && attempts == 1, err
	}); err != nil {
		return fmt.Errorf("notification row did not settle after transport retries: %w (ok:%v attempts:%d)", err, ok, attempts)
	}
	bodies, err := receiverCall(ctx, e.MockReceiverURL, http.MethodGet, "/_received", nil)
	if err != nil {
		return err
	}
	if len(bodies) != 1 {
		return fmt.Errorf("want one delivered body after two failures, got %d", len(bodies))
	}
	e.AssertInvariants(e.T)
	return nil
}

func runAlertLeaderFailover(ctx context.Context, e *Env) error {
	if err := receiverReset(ctx, e.MockReceiverURL); err != nil {
		return err
	}
	instanceID, err := alertInstance(ctx, e)
	if err != nil {
		return err
	}
	if err := e.Compose("stop", "pglens-agent"); err != nil {
		return err
	}
	e.T.Cleanup(func() { _ = e.Compose("start", "pglens-agent") })
	firingCtx, cancel := context.WithTimeout(ctx, 150*time.Second)
	defer cancel()
	if _, err := waitForAlertState(firingCtx, e, instanceID, "firing"); err != nil {
		return err
	}
	if err := poll(ctx, 500*time.Millisecond, func(ctx context.Context) (bool, error) {
		n, err := notificationCount(ctx, e, instanceID, "fire")
		return err == nil && n == 1, err
	}); err != nil {
		return err
	}
	if e.KillServerReplica == nil {
		return fmt.Errorf("alerting harness has no replica kill operation")
	}
	if err := e.KillServerReplica(); err != nil {
		return fmt.Errorf("kill one server replica: %w", err)
	}
	if err := poll(ctx, 500*time.Millisecond, func(ctx context.Context) (bool, error) {
		bodies, err := receiverCall(ctx, e.MockReceiverURL, http.MethodGet, "/_received", nil)
		return err == nil && len(bodies) == 1, err
	}); err != nil {
		return fmt.Errorf("leader failover caused duplicate delivery: %w", err)
	}
	if err := e.Compose("start", "pglens-server"); err != nil {
		return fmt.Errorf("restart killed server replica: %w", err)
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		bodies, err := receiverCall(ctx, e.MockReceiverURL, http.MethodGet, "/_received", nil)
		if err == nil && len(bodies) != 1 {
			return fmt.Errorf("restart caused duplicate delivery: %d bodies", len(bodies))
		}
		time.Sleep(500 * time.Millisecond)
	}
	e.AssertInvariants(e.T)
	return nil
}

func runAlertSilence(ctx context.Context, e *Env) error {
	if err := receiverReset(ctx, e.MockReceiverURL); err != nil {
		return err
	}
	instanceID, err := alertInstance(ctx, e)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	payload, _ := json.Marshal(map[string]interface{}{
		"matchers":  []map[string]string{{"name": "rule_id", "value": "agent_down"}, {"name": "instance_id", "value": instanceID}},
		"reason":    "maintenance",
		"starts_at": now.Add(-time.Minute),
		"ends_at":   now.Add(5 * time.Minute),
	})
	if _, err := e.API.Request(http.MethodPost, "/api/v1/silences", payload); err != nil {
		return fmt.Errorf("create silence: %w", err)
	}
	if err := e.Compose("stop", "pglens-agent"); err != nil {
		return err
	}
	e.T.Cleanup(func() { _ = e.Compose("start", "pglens-agent") })
	firingCtx, cancel := context.WithTimeout(ctx, 150*time.Second)
	defer cancel()
	rows, err := waitForAlertState(firingCtx, e, instanceID, "firing")
	if err != nil {
		return err
	}
	if suppressed, _ := rows[0]["suppressed"].(bool); !suppressed {
		return fmt.Errorf("firing alert was not marked suppressed: %v", rows[0])
	}
	bodies, err := receiverCall(ctx, e.MockReceiverURL, http.MethodGet, "/_received", nil)
	if err != nil {
		return err
	}
	if len(bodies) != 0 {
		return fmt.Errorf("silenced firing alert delivered %d notifications", len(bodies))
	}
	e.AssertInvariants(e.T)
	return nil
}
