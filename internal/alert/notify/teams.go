package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// Teams caps: a card's title line and summary stay readable, and the whole
// card stays well under the 28 KB a Teams message may carry.
const (
	teamsTitleMax  = 300
	teamsTextMax   = 3000
	teamsDetailMax = 12000
)

type teams struct {
	url    string
	client *http.Client
}

// NewTeams returns a channel that posts an Adaptive Card to a Microsoft Teams
// workflow ("Post to a channel when a webhook request is received").
func NewTeams(webhookURL string, hc *http.Client) Channel {
	return &teams{url: webhookURL, client: hc}
}

func (t *teams) Name() string { return "teams" }

func (t *teams) Send(ctx context.Context, m Message) error {
	b, err := json.Marshal(teamsBody(m))
	if err != nil {
		return err
	}
	return sendJSON(ctx, t.client, t.url, b)
}

type teamsFact struct {
	Title string `json:"title"`
	Value string `json:"value"`
}

// teamsBody is the R2 message: a title line, the summary, the facts and, with
// a detail, its sections as monospace text. With a detail the title and the
// facts are the detail's, already in the reader's language, as Google Chat
// shows them; without one they are the English fallback the chat channels use.
func teamsBody(m Message) map[string]any {
	title := strings.ToUpper(m.Severity)
	if m.Detail != nil && m.Detail.Title != "" {
		title = m.Detail.Title
	}
	body := []any{
		map[string]any{"type": "TextBlock", "text": clip(title, teamsTitleMax), "weight": "Bolder", "wrap": true},
		map[string]any{"type": "TextBlock", "text": clip(m.Summary, teamsTextMax), "wrap": true},
	}
	if facts := teamsFacts(m); len(facts) > 0 {
		body = append(body, map[string]any{"type": "FactSet", "facts": facts})
	}
	if m.Detail != nil {
		if text := teamsDetailText(m.Detail); text != "" {
			body = append(body, map[string]any{"type": "TextBlock", "text": clip(text, teamsDetailMax), "wrap": true, "fontType": "Monospace"})
		}
	}
	return map[string]any{
		"type": "message",
		"attachments": []any{map[string]any{
			"contentType": "application/vnd.microsoft.card.adaptive",
			"content": map[string]any{
				"type":    "AdaptiveCard",
				"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
				"version": "1.4",
				"body":    body,
			},
		}},
	}
}

func teamsFacts(m Message) []teamsFact {
	var all []teamsFact
	if m.Detail != nil && len(m.Detail.Fields) > 0 {
		for _, f := range m.Detail.Fields {
			all = append(all, teamsFact{Title: f.Label, Value: f.Value})
		}
	} else {
		at := ""
		if !m.At.IsZero() {
			at = m.At.UTC().Format(time.RFC3339)
		}
		all = []teamsFact{
			{"Cluster", m.Cluster}, {"Instance", m.Instance}, {"Database", m.Database},
			{"Severity", m.Severity}, {"Time", at},
		}
	}
	facts := all[:0]
	for _, f := range all {
		if f.Value != "" {
			facts = append(facts, f)
		}
	}
	return facts
}

// teamsDetailText is the detail's sections and footer as plain lines; the
// title and the fields are already in the card.
func teamsDetailText(d *Detail) string {
	var lines []string
	for _, s := range d.Sections {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		if s.Title != "" {
			lines = append(lines, s.Title)
		}
		lines = append(lines, s.Lines...)
		lines = append(lines, s.Code...)
	}
	if d.Footer != "" {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, d.Footer)
	}
	return strings.Join(lines, "\n")
}
