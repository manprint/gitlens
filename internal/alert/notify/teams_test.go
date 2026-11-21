package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func teamsMessage() Message {
	return Message{
		DedupID: "d1", RuleID: "replication_lag", Severity: "critical", Phase: "firing",
		Summary: "replication lag high", Cluster: "c1", Instance: "i1", Database: "odoo",
		Value: 42.5, At: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC),
	}
}

// NCH-001: a firing critical message with a detail posts the R2 card: the
// detail's title, the summary, the detail's fields as facts and its sections
// as one monospace block.
func TestNCH001_TeamsBodyWithDetail(t *testing.T) {
	srv, calls, bodies := googleChatServer(t)
	m := teamsMessage()
	m.Detail = &Detail{
		Title:  "Replication lag on i1",
		Fields: []Field{{Label: "Cluster", Value: "c1"}, {Label: "Server", Value: "i1"}, {Label: "Empty", Value: ""}},
		Sections: []Section{
			{Title: "Evidence", Lines: []string{"lag 42.5 s"}},
			{Title: "Check", Code: []string{"SELECT 1;"}},
		},
		Footer: "pglens",
	}
	require.NoError(t, NewTeams(srv.URL, srv.Client()).Send(context.Background(), m))
	require.Equal(t, int32(1), calls.Load())
	require.JSONEq(t, `{"type":"message","attachments":[{"contentType":"application/vnd.microsoft.card.adaptive",
	 "content":{"type":"AdaptiveCard","$schema":"http://adaptivecards.io/schemas/adaptive-card.json","version":"1.4","body":[
	  {"type":"TextBlock","text":"Replication lag on i1","weight":"Bolder","wrap":true},
	  {"type":"TextBlock","text":"replication lag high","wrap":true},
	  {"type":"FactSet","facts":[{"title":"Cluster","value":"c1"},{"title":"Server","value":"i1"}]},
	  {"type":"TextBlock","text":"Evidence\nlag 42.5 s\n\nCheck\nSELECT 1;\n\npglens","wrap":true,"fontType":"Monospace"}]}}]}`, (*bodies)[0])
	require.Equal(t, "teams", NewTeams("", nil).Name())
}

// NCH-002: without a detail the card has three blocks, the facts in English.
func TestNCH002_TeamsBodyWithoutDetail(t *testing.T) {
	b, err := json.Marshal(teamsBody(teamsMessage()))
	require.NoError(t, err)
	var card struct {
		Attachments []struct {
			Content struct {
				Body []map[string]any `json:"body"`
			} `json:"content"`
		} `json:"attachments"`
	}
	require.NoError(t, json.Unmarshal(b, &card))
	body := card.Attachments[0].Content.Body
	require.Len(t, body, 3)
	require.Equal(t, "CRITICAL", body[0]["text"])
	require.Equal(t, "FactSet", body[2]["type"])
	facts, _ := json.Marshal(body[2]["facts"])
	require.JSONEq(t, `[{"title":"Cluster","value":"c1"},{"title":"Instance","value":"i1"},{"title":"Database","value":"odoo"},{"title":"Severity","value":"critical"},{"title":"Time","value":"2026-10-29T12:00:00Z"}]`, string(facts))
}

// NCH-003: a refused post is an error that never quotes the workflow URL.
func TestNCH003_TeamsErrorRedactsTheURL(t *testing.T) {
	srv, calls, _ := googleChatServer(t, http.StatusBadRequest)
	url := srv.URL + "/workflows/WORKFLOWID0301/triggers/manual/paths/invoke?api-version=1&sig=TEAMSSIG0302"
	err := NewTeams(url, srv.Client()).Send(context.Background(), teamsMessage())
	require.Error(t, err)
	require.Equal(t, int32(1), calls.Load(), "a 4xx is not retried")
	for _, secret := range []string{url, "WORKFLOWID0301", "TEAMSSIG0302"} {
		require.NotContains(t, err.Error(), secret)
	}
	require.Contains(t, err.Error(), "HTTP 400")
}
