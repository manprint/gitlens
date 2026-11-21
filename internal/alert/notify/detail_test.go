package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func sampleDetail() *Detail {
	return &Detail{
		Title:  "Deadlock · pg1",
		Fields: []Field{{"Cluster", "odoo"}, {"Server", "pg1 (10.0.0.1:5432) · primary"}},
		Sections: []Section{
			{Title: "Captured sessions", Lines: []string{"pid 7 waits for ShareLock on public.orders (3,7)", "statement: SELECT 1 WHERE a < b & c > d"}},
			{Title: "What to do", Lines: []string{"See the row:"}, Code: []string{"SELECT * FROM public.orders WHERE ctid='(3,7)';"}},
			{Lines: []string{"untitled"}},
		},
		Footer: "rule db.deadlock_rate",
		Locale: "en",
	}
}

func captureBody(t *testing.T) (*httptest.Server, *string) {
	t.Helper()
	body := new(string)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*body = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, body
}

func TestSlack_RendersDetail(t *testing.T) {
	srv, body := captureBody(t)
	require.NoError(t, NewSlack(srv.URL, srv.Client()).Send(context.Background(), Message{Severity: "critical", Summary: "deadlocks", Detail: sampleDetail()}))
	var p struct {
		Text   string           `json:"text"`
		Blocks []map[string]any `json:"blocks"`
	}
	require.NoError(t, json.Unmarshal([]byte(*body), &p))
	require.Equal(t, "Deadlock · pg1", p.Text)
	require.Equal(t, "header", p.Blocks[0]["type"])
	require.Equal(t, "context", p.Blocks[len(p.Blocks)-1]["type"])
	require.Contains(t, *body, "*Server:* pg1 (10.0.0.1:5432) · primary")
	require.Contains(t, *body, `a \u0026lt; b \u0026amp; c \u0026gt; d`)
	require.Contains(t, *body, "```SELECT * FROM public.orders WHERE ctid='(3,7)';```")
	require.NotContains(t, *body, "cluster=")
}

func TestSlack_DetailBounds(t *testing.T) {
	d := &Detail{Title: strings.Repeat("t", 400)}
	for i := 0; i < 60; i++ {
		d.Sections = append(d.Sections, Section{Title: "s", Lines: []string{strings.Repeat("x", 5000)}})
	}
	blocks := slackBlocks(Message{Detail: d})
	require.LessOrEqual(t, len(blocks), slackBlocksMax)
	hdr := blocks[0].(map[string]any)["text"].(map[string]any)["text"].(string)
	require.Equal(t, slackHeaderMax, utf8.RuneCountInString(hdr))
	sec := blocks[2].(map[string]any)["text"].(map[string]string)["text"]
	require.Equal(t, slackSectionMax, utf8.RuneCountInString(sec))

	d.Footer = "f"
	d.Sections = d.Sections[:30]
	require.Len(t, slackBlocks(Message{Detail: d}), slackBlocksMax)
}

func TestGoogleChat_RendersDetail(t *testing.T) {
	text := googleChatText(Message{Severity: "critical", Summary: "deadlocks", Detail: sampleDetail()})
	require.True(t, strings.HasPrefix(text, "*Deadlock · pg1*\n*Cluster:* odoo\n"))
	require.Contains(t, text, "\n\n*What to do*\nSee the row:\n```SELECT * FROM public.orders WHERE ctid='(3,7)';```")
	require.Contains(t, text, "a < b & c > d")
	require.True(t, strings.HasSuffix(text, "\nuntitled\n\n_rule db.deadlock_rate_"))

	d := sampleDetail()
	d.Sections[1].Code = []string{strings.Repeat("y", 5000)}
	long := googleChatText(Message{Detail: d})
	require.Equal(t, googleChatMaxRunes, utf8.RuneCountInString(long))
	require.True(t, strings.HasSuffix(long, "…\n```"))
	require.Zero(t, strings.Count(long, "```")%2)
}

func TestWebhook_CarriesDetail(t *testing.T) {
	srv, body := captureBody(t)
	require.NoError(t, NewWebhook(srv.URL, srv.Client()).Send(context.Background(), Message{RuleID: "r", Detail: sampleDetail()}))
	require.Contains(t, *body, `"detail":{"title":"Deadlock · pg1","fields":[{"label":"Cluster","value":"odoo"}`)
	srv2, plain := captureBody(t)
	require.NoError(t, NewWebhook(srv2.URL, srv2.Client()).Send(context.Background(), Message{RuleID: "r"}))
	require.NotContains(t, *plain, "detail")
}

func TestClip(t *testing.T) {
	require.Equal(t, "abc", clip("abc", 3))
	require.Equal(t, "ab…", clip("abcd", 3))
	require.Equal(t, "a```bc…\n```", clip("a```bcdefghijklmnop", 11))
}
