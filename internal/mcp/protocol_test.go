package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func decode(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("response is not a JSON object: %v (%s)", err, raw)
	}
	return out
}

func errorCode(t *testing.T, raw []byte) float64 {
	t.Helper()
	e, ok := decode(t, raw)["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error in %s", raw)
	}
	return e["code"].(float64)
}

func TestMCP001InitializeNegotiatesVersion(t *testing.T) {
	s := &Server{Info: Info{Name: "pglens", Version: "dev"}, Instructions: "read-only"}
	cases := map[string]string{
		`"2025-06-18"`: "2025-06-18",
		`"2025-11-25"`: "2025-11-25",
		`"2099-01-01"`: LatestVersion,
		`42`:           LatestVersion,
	}
	for version, want := range cases {
		raw := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":` + version + `,"capabilities":{},"clientInfo":{"name":"x"}}}`
		resp, note := s.Handle(context.Background(), []byte(raw), nil)
		if note {
			t.Fatalf("%s: initialize treated as notification", version)
		}
		result := decode(t, resp)["result"].(map[string]any)
		if result["protocolVersion"] != want {
			t.Fatalf("%s: version %v, want %s", version, result["protocolVersion"], want)
		}
		tools := result["capabilities"].(map[string]any)["tools"].(map[string]any)
		if tools["listChanged"] != false {
			t.Fatalf("tools capability %v", tools)
		}
		if result["serverInfo"].(map[string]any)["name"] != "pglens" || result["instructions"] != "read-only" {
			t.Fatalf("result %v", result)
		}
	}
	if Negotiate("") != LatestVersion || !Supported("2025-06-18") || Supported("2024-11-05") {
		t.Fatal("negotiation table")
	}
	if got := SupportedVersions(); len(got) != 2 || got[0] != LatestVersion {
		t.Fatalf("versions %v", got)
	}
}

func TestMCP002NotificationAndPing(t *testing.T) {
	s := &Server{}
	resp, note := s.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`), nil)
	if !note || resp != nil {
		t.Fatalf("notification answered %q, %v", resp, note)
	}
	resp, note = s.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","method":"no/such","params":[1]}`), nil)
	if !note || resp != nil {
		t.Fatalf("unknown notification answered %q", resp)
	}
	resp, note = s.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":"p","method":"ping"}`), nil)
	if note {
		t.Fatal("ping treated as notification")
	}
	if got := strings.TrimSpace(string(resp)); got != `{"jsonrpc":"2.0","id":"p","result":{}}` {
		t.Fatalf("ping answered %s", got)
	}
}

func TestMCP005MalformedMessages(t *testing.T) {
	s := &Server{}
	cases := map[string]float64{
		`{"jsonrpc":"2.0","id":1,`: CodeParseError,
		``:                         CodeParseError,
		`[{"jsonrpc":"2.0","id":1,"method":"ping"}]`: CodeInvalidRequest,
		`"ping"`: CodeInvalidRequest,
		`{"jsonrpc":"2.0","id":null,"method":"ping"}`:                 CodeInvalidRequest,
		`{"jsonrpc":"2.0","id":{},"method":"ping"}`:                   CodeInvalidRequest,
		`{"jsonrpc":"1.0","id":1,"method":"ping"}`:                    CodeInvalidRequest,
		`{"jsonrpc":"2.0","id":1}`:                                    CodeInvalidRequest,
		`{"jsonrpc":"2.0","id":1,"method":"nope"}`:                    CodeMethodNotFound,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":[1]}`: CodeInvalidParams,
	}
	for raw, want := range cases {
		resp, note := s.Handle(context.Background(), []byte(raw), nil)
		if note {
			t.Fatalf("%s: treated as notification", raw)
		}
		if got := errorCode(t, resp); got != want {
			t.Fatalf("%s: code %v, want %v", raw, got, want)
		}
	}
	for raw, wantID := range map[string]string{
		`{"jsonrpc":"2.0","id":7,"method":"nope"}`:          `7`,
		`{"jsonrpc":"2.0","id":"7","method":"ping"}`:        `"7"`,
		`{"jsonrpc":"2.0","id":-1.5,"method":"ping","x":1}`: `-1.5`,
		`{"jsonrpc":"2.0","id":[1],"method":"ping"}`:        `null`,
	} {
		resp, _ := s.Handle(context.Background(), []byte(raw), nil)
		var msg struct {
			ID json.RawMessage `json:"id"`
		}
		if err := json.Unmarshal(resp, &msg); err != nil || string(msg.ID) != wantID {
			t.Fatalf("%s: id %s, want %s", raw, msg.ID, wantID)
		}
	}
}

func FuzzHandle(f *testing.F) {
	for _, seed := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"pglens_get","arguments":{"id":"a","from":["x",1,true]}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`[1,2]`, `{`, `null`, `{"jsonrpc":"2.0","id":"x","method":"tools/list"}`,
	} {
		f.Add([]byte(seed))
	}
	s := &Server{Info: Info{Name: "pglens"}, Tools: []Tool{fuzzTool}}
	call := func(context.Context, Tool, map[string]json.RawMessage) (int, []byte, error) {
		return 200, []byte(`{"ok":true}`), nil
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		resp, note := s.Handle(context.Background(), raw, call)
		if note {
			if resp != nil {
				t.Fatal("notification with a body")
			}
			return
		}
		if !json.Valid(resp) {
			t.Fatalf("invalid JSON answer %q", resp)
		}
	})
}

var fuzzTool = Tool{
	Name: "pglens_get", Description: "fuzz", Path: "/api/v1/x/{id}",
	PathParams: []string{"id"}, QueryParams: []string{"from"},
}
