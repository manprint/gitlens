package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSlack_PostsExpectedBody(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(b)
		body = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	err := NewSlack(srv.URL, srv.Client()).Send(context.Background(), Message{Severity: "warning", Summary: "bad", At: time.Unix(0, 0)})
	require.NoError(t, err)
	require.Contains(t, body, "WARNING bad")
	require.Contains(t, body, "blocks")
}

func TestWebhook_PostsJSON(t *testing.T) {
	var contentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		w.WriteHeader(204)
	}))
	defer srv.Close()
	require.NoError(t, NewWebhook(srv.URL, srv.Client()).Send(context.Background(), Message{RuleID: "r"}))
	require.Equal(t, "application/json", contentType)
}

func TestChannelsExposeStableNames(t *testing.T) {
	require.Equal(t, "slack", NewSlack("", nil).Name())
	require.Equal(t, "webhook", NewWebhook("", nil).Name())
}

func TestRetry_NoRetryOn4xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(400) }))
	defer srv.Close()
	require.Error(t, NewWebhook(srv.URL, srv.Client()).Send(context.Background(), Message{}))
	require.Equal(t, int32(1), calls.Load())
}

func TestError_DoesNotContainWebhookURL(t *testing.T) {
	url := "http://hooks.slack.com/services/secret"
	err := sendJSON(context.Background(), http.DefaultClient, url, []byte(`{}`))
	if err != nil {
		require.False(t, strings.Contains(err.Error(), url))
	}
}
