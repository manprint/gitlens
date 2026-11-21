package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

const testPagerDutyKey = "R0UT1NGKEY0123456789abcdefABCDEF"

// pointPagerDuty sends the events of the test to a local server.
func pointPagerDuty(t *testing.T) *[]string {
	t.Helper()
	srv, _, bodies := googleChatServer(t)
	previous := pagerDutyEndpoint
	pagerDutyEndpoint = srv.URL + "/v2/enqueue"
	t.Cleanup(func() { pagerDutyEndpoint = previous })
	return bodies
}

// NCH-010: a firing message triggers an event whose dedup_key is the
// message's DedupID.
func TestNCH010_PagerDutyTrigger(t *testing.T) {
	bodies := pointPagerDuty(t)
	m := Message{
		DedupID: "dedup-1", RuleID: "replication_lag", Severity: "critical", Phase: "fire",
		Summary: "replication lag high", Cluster: "7", Instance: "i1", Database: "odoo", Value: 42.5,
		At: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC), Labels: map[string]string{"role": "replica"},
		Detail: &Detail{Title: "Lag", Fields: []Field{{Label: "Server", Value: "i1"}}, Sections: []Section{{Title: "Check", Code: []string{"SELECT 1;"}}}},
	}
	require.NoError(t, NewPagerDuty(testPagerDutyKey, http.DefaultClient).Send(context.Background(), m))
	require.Len(t, *bodies, 1)
	require.JSONEq(t, `{"routing_key":"`+testPagerDutyKey+`","event_action":"trigger","dedup_key":"dedup-1",
	 "payload":{"summary":"replication lag high","source":"i1","severity":"critical","timestamp":"2026-10-29T12:00:00Z",
	  "component":"postgresql","group":"7","class":"replication_lag",
	  "custom_details":{"database":"odoo","value":42.5,"role":"replica","detail":["Lag","Server: i1","Check","SELECT 1;"]}}}`, (*bodies)[0])
	require.Equal(t, "pagerduty", NewPagerDuty("", nil).Name())
}

// NCH-011: a resolved message sends only the key, the action and the dedup key.
func TestNCH011_PagerDutyResolve(t *testing.T) {
	bodies := pointPagerDuty(t)
	m := Message{DedupID: "dedup-1", Severity: "critical", Phase: "resolve", Summary: "s", Cluster: "7"}
	require.NoError(t, NewPagerDuty(testPagerDutyKey, http.DefaultClient).Send(context.Background(), m))
	var event map[string]any
	require.NoError(t, json.Unmarshal([]byte((*bodies)[0]), &event))
	require.Equal(t, map[string]any{"routing_key": testPagerDutyKey, "event_action": "resolve", "dedup_key": "dedup-1"}, event)
}

// NCH-012: critical and warning keep their name, anything else is info; the
// source falls back from the instance to the cluster to pglens.
func TestNCH012_PagerDutySeverityAndSource(t *testing.T) {
	for in, want := range map[string]string{"critical": "critical", "warning": "warning", "info": "info", "other": "info", "": "info"} {
		require.Equal(t, want, pagerDutySeverity(in), in)
	}
	source := func(m Message) any { return pagerDutyEvent("k", m)["payload"].(map[string]any)["source"] }
	require.Equal(t, "c", source(Message{Cluster: "c"}))
	require.Equal(t, "pglens", source(Message{}))
}

// NCH-013: the summary is cut to 1024 characters.
func TestNCH013_PagerDutySummaryIsCut(t *testing.T) {
	summary := pagerDutyEvent("k", Message{Summary: strings.Repeat("é", 2000)})["payload"].(map[string]any)["summary"].(string)
	require.Equal(t, 1024, utf8.RuneCountInString(summary))
}

// NCH-014 (channel side): the routing key is checked without being quoted,
// and a refused event's error does not quote it either.
func TestNCH014_PagerDutyKeyNeverInErrors(t *testing.T) {
	require.NoError(t, ValidatePagerDutyKey(testPagerDutyKey))
	for _, bad := range []string{testPagerDutyKey[:31], testPagerDutyKey + "0", strings.Repeat("-", 32), ""} {
		err := ValidatePagerDutyKey(bad)
		require.ErrorIs(t, err, ErrPagerDutyKey)
		if bad != "" {
			require.NotContains(t, err.Error(), bad)
		}
	}
	srv, calls, _ := googleChatServer(t, http.StatusBadRequest)
	previous := pagerDutyEndpoint
	pagerDutyEndpoint = srv.URL
	t.Cleanup(func() { pagerDutyEndpoint = previous })
	err := NewPagerDuty(testPagerDutyKey, srv.Client()).Send(context.Background(), Message{Severity: "warning", Summary: "s"})
	require.Error(t, err)
	require.Equal(t, int32(1), calls.Load())
	require.NotContains(t, err.Error(), testPagerDutyKey)
}
