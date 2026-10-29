package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func googleChatServer(t *testing.T, statuses ...int) (*httptest.Server, *atomic.Int32, *[]string) {
	t.Helper()
	var calls atomic.Int32
	bodies := &[]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(calls.Add(1))
		b, _ := io.ReadAll(r.Body)
		*bodies = append(*bodies, string(b))
		status := http.StatusOK
		if n <= len(statuses) {
			status = statuses[n-1]
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls, bodies
}

func TestNOTIFGC001_PayloadHasOnlyText(t *testing.T) {
	srv, calls, bodies := googleChatServer(t)
	at := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	m := Message{Severity: "critical", Summary: "replication lag high", Cluster: "c1", Instance: "i1", Database: "odoo", Value: 42.5, At: at}
	require.NoError(t, NewGoogleChat(srv.URL, srv.Client()).Send(context.Background(), m))
	require.Equal(t, int32(1), calls.Load())
	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte((*bodies)[0]), &payload))
	require.Len(t, payload, 1)
	text, ok := payload["text"].(string)
	require.True(t, ok)
	for _, want := range []string{"CRITICAL replication lag high", "cluster=c1", "instance=i1", "database=odoo", "value=42.5", "at=2026-10-29T12:00:00Z"} {
		require.Contains(t, text, want)
	}
	require.Equal(t, "google_chat", NewGoogleChat("", nil).Name())
}

func TestNOTIFGC002_LongTextIsTruncated(t *testing.T) {
	text := googleChatText(Message{Severity: "info", Summary: strings.Repeat("é", 5000)})
	require.Equal(t, googleChatMaxRunes, utf8.RuneCountInString(text))
	require.True(t, strings.HasSuffix(text, "…"))
}

func TestNOTIFGC003_RetriesServerErrors(t *testing.T) {
	srv, calls, _ := googleChatServer(t, 500, 500, 200)
	require.NoError(t, NewGoogleChat(srv.URL, srv.Client()).Send(context.Background(), Message{Severity: "warning", Summary: "s"}))
	require.Equal(t, int32(3), calls.Load())
}

func TestNOTIFGC004_ClientErrorIsNotRetriedAndRedacted(t *testing.T) {
	srv, calls, _ := googleChatServer(t, 400)
	url := srv.URL + "/v1/spaces/SPACEID123/messages?key=KEY123456&token=TOKEN123456"
	err := NewGoogleChat(url, srv.Client()).Send(context.Background(), Message{Severity: "warning", Summary: "s"})
	require.Error(t, err)
	require.Equal(t, int32(1), calls.Load())
	require.Contains(t, err.Error(), "HTTP 400")
	require.Contains(t, err.Error(), "[redacted webhook]")
	require.NotContains(t, err.Error(), "key=")
	require.NotContains(t, err.Error(), "token=")
}

func TestNOTIFGC005_RedactsGoogleChatURLs(t *testing.T) {
	for _, raw := range []string{
		`Post "https://chat.googleapis.com/v1/spaces/X/messages?key=K&token=T": dial tcp: lookup failed`,
		`dial https://chat.googleapis.com/v1/spaces/X/messages?key=K&token=T failed (twice: chat.googleapis.com/v1/spaces/X?key=K)`,
	} {
		msg := redactError(errors.New(raw), "").Error()
		require.NotContains(t, msg, "spaces/X")
		require.NotContains(t, msg, "K")
		require.NotContains(t, msg, "T&")
		require.NotContains(t, msg, "token=")
		require.Contains(t, msg, "chat.googleapis.com/[redacted]")
	}
	require.Equal(t, "Post \"hooks.slack.com/[redacted]\": x", redactError(errors.New(`Post "hooks.slack.com/services/A/B": x`), "").Error())
}
