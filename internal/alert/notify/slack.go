package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type slack struct {
	url    string
	client *http.Client
}

func NewSlack(webhookURL string, hc *http.Client) Channel { return &slack{url: webhookURL, client: hc} }
func (s *slack) Name() string                             { return "slack" }
func (s *slack) Send(ctx context.Context, m Message) error {
	payload := struct {
		Text   string `json:"text"`
		Blocks []any  `json:"blocks"`
	}{
		Text: strings.ToUpper(m.Severity) + " " + m.Summary,
		Blocks: []any{
			map[string]any{"type": "section", "text": map[string]string{"type": "mrkdwn", "text": m.Summary}},
			map[string]any{"type": "context", "elements": []any{map[string]string{"type": "mrkdwn", "text": fmt.Sprintf("cluster=%s instance=%s database=%s value=%v at=%s", m.Cluster, m.Instance, m.Database, m.Value, m.At.Format(time.RFC3339))}}},
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return sendJSON(ctx, s.client, s.url, b)
}
