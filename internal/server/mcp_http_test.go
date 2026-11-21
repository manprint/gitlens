package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"
)

const mcpTestToken = "mcp-test-token"

func mcpTestConfig() UIConfig {
	return UIConfig{MCP: MCPConfig{Enabled: true, AllowedOrigins: []string{"https://ai.example"}, Version: "test"}}
}

func mcpRealRouter(cfg UIConfig) http.Handler {
	return NewRouter(cfg, nil, nil, NewAuth(mcpTestToken), NewInventory(nil), NewPipeline(nil, nil),
		NewAPI(nil), NewTopologyAPI(nil), NewAshAPI(nil), NewAlertAPI(nil, nil))
}

func mcpPost(h http.Handler, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "http://pglens.test/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+mcpTestToken)
	for k, v := range headers {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

const mcpPing = `{"jsonrpc":"2.0","id":1,"method":"ping"}`

func TestMCP009RouteChecks(t *testing.T) {
	disabled := mcpRealRouter(UIConfig{})
	if rec := mcpPost(disabled, mcpPing, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("disabled: %d", rec.Code)
	}
	h := mcpRealRouter(mcpTestConfig())
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		req := httptest.NewRequest(method, "http://pglens.test/mcp", nil)
		req.Header.Set("Authorization", "Bearer "+mcpTestToken)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodPost {
			t.Fatalf("%s: %d Allow=%q", method, rec.Code, rec.Header().Get("Allow"))
		}
	}
	if rec := mcpPost(h, mcpPing, map[string]string{"Content-Type": "text/plain"}); rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("content type: %d", rec.Code)
	}
	if rec := mcpPost(h, mcpPing, map[string]string{"Content-Type": "application/json; charset=utf-8"}); rec.Code != http.StatusOK {
		t.Fatalf("content type with charset: %d", rec.Code)
	}
	big := `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"x":"` + strings.Repeat("a", 2<<20) + `"}}`
	if rec := mcpPost(h, big, nil); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("2 MiB: %d", rec.Code)
	}
	if rec := mcpPost(h, mcpPing, map[string]string{"MCP-Protocol-Version": "2024-01-01"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("protocol version: %d", rec.Code)
	}
	rec := mcpPost(h, mcpPing, map[string]string{"MCP-Protocol-Version": "2025-06-18"})
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("ping: %d %v", rec.Code, rec.Header())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"jsonrpc":"2.0","id":1,"result":{}}` {
		t.Fatalf("ping body %s", got)
	}
	// Through the real router: the GET is routed as a GET, not as the outer POST.
	rec = mcpPost(h, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"pglens_list_instances"}}`, nil)
	if !strings.Contains(rec.Body.String(), `"isError":false`) || !strings.Contains(rec.Body.String(), `"text":"[]\n"`) {
		t.Fatalf("list through the router: %s", rec.Body.String())
	}
	rec = mcpPost(h, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, nil)
	if rec.Code != http.StatusAccepted || rec.Body.Len() != 0 {
		t.Fatalf("notification: %d %q", rec.Code, rec.Body.String())
	}
}

// mcpFakeREST is the credential gate in front of a REST stand-in that answers
// /api/v1/instances per token and /api/v1/locks with a validation refusal.
func mcpFakeREST(calls *atomic.Int32, seen *http.Header) http.Handler {
	mux := chi.NewRouter()
	mux.Get("/api/v1/instances", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		*seen = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"instances":[{"id":"i-1","token":"` + r.Header.Get("Authorization") + `"}]}`))
	})
	mux.Get("/api/v1/locks", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeError(w, http.StatusBadRequest, "invalid instance_id", "instance_id must be a UUID")
	})
	return RequireCredential(UIConfig{}, NewAuth(mcpTestToken), nil)(mux)
}

func TestMCP010CredentialIsTheRESTOne(t *testing.T) {
	var calls atomic.Int32
	var seen http.Header
	rest := mcpFakeREST(&calls, &seen)
	h := mcpHandler(mcpTestConfig(), NewAuth(mcpTestToken), nil, rest)
	listCall := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"pglens_list_instances","arguments":{}}}`

	if rec := mcpPost(h, listCall, map[string]string{"Authorization": ""}); rec.Code != http.StatusUnauthorized || calls.Load() != 0 {
		t.Fatalf("no credential: %d, calls %d", rec.Code, calls.Load())
	}
	if rec := mcpPost(h, listCall, map[string]string{"Authorization": "Bearer wrong"}); rec.Code != http.StatusUnauthorized || calls.Load() != 0 {
		t.Fatalf("wrong token: %d, calls %d", rec.Code, calls.Load())
	}

	direct := httptest.NewRequest(http.MethodGet, "http://pglens.test/api/v1/instances", nil)
	direct.Header.Set("Authorization", "Bearer "+mcpTestToken)
	want := httptest.NewRecorder()
	rest.ServeHTTP(want, direct)

	var out struct {
		Result struct {
			Content []struct{ Text string } `json:"content"`
			IsError bool                    `json:"isError"`
		} `json:"result"`
	}
	rec := mcpPost(h, listCall, nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	if out.Result.IsError || out.Result.Content[0].Text != want.Body.String() {
		t.Fatalf("tool text %q, REST %q", out.Result.Content[0].Text, want.Body.String())
	}

	rec = mcpPost(h, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"pglens_locks","arguments":{"instance_id":"nope"}}}`, nil)
	out.Result.IsError = false
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Result.IsError || !strings.Contains(out.Result.Content[0].Text, `"error":"invalid instance_id"`) {
		t.Fatalf("refused inner call: %s", rec.Body.String())
	}
}

func TestMCP011Origin(t *testing.T) {
	h := mcpRealRouter(mcpTestConfig())
	cases := map[string]int{
		"":                        http.StatusOK,
		"http://pglens.test":      http.StatusOK,
		"https://ai.example":      http.StatusOK,
		"HTTPS://AI.EXAMPLE":      http.StatusOK,
		"https://evil.example":    http.StatusForbidden,
		"https://ai.example:8443": http.StatusForbidden,
		"null":                    http.StatusForbidden,
	}
	for origin, want := range cases {
		headers := map[string]string{"Origin": origin}
		if origin != "" {
			headers["Sec-Fetch-Site"] = "cross-site"
		}
		if rec := mcpPost(h, mcpPing, headers); rec.Code != want {
			t.Errorf("Origin %q: %d, want %d (%s)", origin, rec.Code, want, rec.Body.String())
		}
	}
	// The cross-origin write guard still covers every other route.
	req := httptest.NewRequest(http.MethodPost, "http://pglens.test/api/v1/push", bytes.NewReader([]byte("{}")))
	req.Header.Set("Origin", "https://ai.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("push from another origin: %d", rec.Code)
	}
}

func TestMCP012Configuration(t *testing.T) {
	env := func(vars map[string]string) func(string) string { return func(k string) string { return vars[k] } }
	cfg, err := LoadMCPConfig(env(nil))
	if err != nil || cfg.Enabled || len(cfg.AllowedOrigins) != 0 {
		t.Fatalf("default: %+v %v", cfg, err)
	}
	cfg, err = LoadMCPConfig(env(map[string]string{"PGLENS_MCP_ENABLED": "true", "PGLENS_MCP_ALLOWED_ORIGINS": " https://a.example , http://B.example:8080,"}))
	if err != nil || !cfg.Enabled || strings.Join(cfg.AllowedOrigins, " ") != "https://a.example http://b.example:8080" {
		t.Fatalf("enabled: %+v %v", cfg, err)
	}
	for _, bad := range []map[string]string{
		{"PGLENS_MCP_ENABLED": "yes"},
		{"PGLENS_MCP_ENABLED": "1"},
		{"PGLENS_MCP_ALLOWED_ORIGINS": "ftp://a.example"},
		{"PGLENS_MCP_ALLOWED_ORIGINS": "https://a.example/path"},
		{"PGLENS_MCP_ALLOWED_ORIGINS": "a.example"},
		{"PGLENS_MCP_ALLOWED_ORIGINS": "https://"},
		{"PGLENS_MCP_ALLOWED_ORIGINS": "https://a.example:99999"},
		{"PGLENS_MCP_ALLOWED_ORIGINS": "https://u@a.example"},
		{"PGLENS_MCP_ALLOWED_ORIGINS": "https://a.example?x=1"},
	} {
		if _, err := LoadMCPConfig(env(bad)); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestMCP013OnlyCredentialHeadersReachREST(t *testing.T) {
	var calls atomic.Int32
	var seen http.Header
	h := mcpHandler(mcpTestConfig(), NewAuth(mcpTestToken), nil, mcpFakeREST(&calls, &seen))
	rec := mcpPost(h, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"pglens_list_instances"}}`, map[string]string{
		"Cookie":          "pglens_session=abc",
		"X-Forwarded-For": "10.0.0.1",
		"X-Pglens-Lang":   "it",
		"Accept-Encoding": "gzip",
	})
	if rec.Code != http.StatusOK || calls.Load() != 1 {
		t.Fatalf("call: %d, calls %d", rec.Code, calls.Load())
	}
	for name := range seen {
		switch name {
		case "Authorization", "Cookie", "Accept":
		default:
			t.Errorf("header %s reached the REST request", name)
		}
	}
	if seen.Get("Cookie") != "pglens_session=abc" || seen.Get("Accept") != "application/json" {
		t.Fatalf("headers %v", seen)
	}
}

func TestMCPMemoryResponseCapsBody(t *testing.T) {
	m := &memoryResponse{header: http.Header{}, limit: 4}
	if n, _ := m.Write([]byte("abc")); n != 3 {
		t.Fatal("short write")
	}
	m.WriteHeader(http.StatusTeapot)
	_, _ = m.Write([]byte("defg"))
	if string(m.body) != "abcd" || m.statusCode() != http.StatusOK {
		t.Fatalf("%q %d", m.body, m.statusCode())
	}
	if (&memoryResponse{}).statusCode() != http.StatusOK {
		t.Fatal("default status")
	}
}
