package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// googleChatMaxRunes caps the message text; Google Chat accepts more, but an
// alert summary that long is unreadable in a chat space anyway.
const googleChatMaxRunes = 4000

type googleChat struct {
	url    string
	client *http.Client
}

// NewGoogleChat returns a channel that posts to a Google Chat incoming webhook.
func NewGoogleChat(webhookURL string, hc *http.Client) Channel {
	return &googleChat{url: webhookURL, client: hc}
}

func (g *googleChat) Name() string { return "google_chat" }

func (g *googleChat) Send(ctx context.Context, m Message) error {
	b, err := json.Marshal(struct {
		Text string `json:"text"`
	}{Text: googleChatText(m)})
	if err != nil {
		return err
	}
	return sendJSON(ctx, g.client, g.url, b)
}

func googleChatText(m Message) string {
	text := strings.ToUpper(m.Severity) + " " + m.Summary + "\n" +
		fmt.Sprintf("cluster=%s instance=%s database=%s value=%v at=%s", m.Cluster, m.Instance, m.Database, m.Value, m.At.Format(time.RFC3339))
	if r := []rune(text); len(r) > googleChatMaxRunes {
		text = string(r[:googleChatMaxRunes-1]) + "…"
	}
	return text
}
