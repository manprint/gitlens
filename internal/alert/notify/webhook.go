package notify

import (
	"context"
	"encoding/json"
	"net/http"
)

type webhook struct {
	url    string
	client *http.Client
}

func NewWebhook(url string, hc *http.Client) Channel { return &webhook{url: url, client: hc} }
func (w *webhook) Name() string                      { return "webhook" }
func (w *webhook) Send(ctx context.Context, m Message) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return sendJSON(ctx, w.client, w.url, b)
}
