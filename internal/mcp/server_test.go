package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

var hostTool = Tool{
	Name:        "pglens_instance_host",
	Title:       "Host metrics",
	Description: "Host metrics of one instance.",
	InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"],"additionalProperties":false}`),
	Path:        "/api/v1/instances/{id}/host",
	PathParams:  []string{"id"},
	QueryParams: []string{"from", "to", "kind"},
}

var listTool = Tool{Name: "pglens_list_instances", Description: "Every instance.", Path: "/api/v1/instances"}

func callMessage(name, args string) []byte {
	return []byte(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"` + name + `","arguments":` + args + `}}`)
}

func TestMCP003ToolsList(t *testing.T) {
	s := &Server{Tools: []Tool{hostTool, listTool}}
	resp, _ := s.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`), nil)
	tools := decode(t, resp)["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("tools %v", tools)
	}
	for _, raw := range tools {
		tool := raw.(map[string]any)
		if tool["inputSchema"].(map[string]any)["type"] != "object" {
			t.Fatalf("%v: schema type", tool["name"])
		}
		ann := tool["annotations"].(map[string]any)
		if ann["readOnlyHint"] != true || ann["openWorldHint"] != false {
			t.Fatalf("%v: annotations %v", tool["name"], ann)
		}
		if tool["description"] == "" {
			t.Fatalf("%v: no description", tool["name"])
		}
	}
	if tools[0].(map[string]any)["title"] != "Host metrics" {
		t.Fatalf("title %v", tools[0])
	}
}

func TestMCP004ToolsCall(t *testing.T) {
	s := &Server{Tools: []Tool{hostTool, listTool}}
	var gotTarget string
	reply := func(status int, body string, err error) Caller {
		return func(_ context.Context, tool Tool, args map[string]json.RawMessage) (int, []byte, error) {
			gotTarget, _ = tool.Target(args)
			return status, []byte(body), err
		}
	}
	result := func(resp []byte, _ bool) map[string]any {
		t.Helper()
		r, ok := decode(t, resp)["result"].(map[string]any)
		if !ok {
			t.Fatalf("no result in %s", resp)
		}
		return r
	}
	text := func(r map[string]any) string {
		return r["content"].([]any)[0].(map[string]any)["text"].(string)
	}

	r := result(s.Handle(context.Background(), callMessage("pglens_instance_host", `{"id":"abc-1","from":"2026-11-21T10:00:00Z","kind":["cpu","mem"],"to":null}`), reply(200, ` {"a":1}`, nil)))
	if r["isError"] != false || r["structuredContent"].(map[string]any)["a"] != float64(1) || text(r) != ` {"a":1}` {
		t.Fatalf("object body: %v", r)
	}
	if gotTarget != "/api/v1/instances/abc-1/host?from=2026-11-21T10%3A00%3A00Z&kind=cpu%2Cmem" {
		t.Fatalf("target %s", gotTarget)
	}

	r = result(s.Handle(context.Background(), callMessage("pglens_list_instances", `{}`), reply(200, `[1,2]`, nil)))
	if _, ok := r["structuredContent"]; ok || text(r) != `[1,2]` || r["isError"] != false {
		t.Fatalf("array body: %v", r)
	}

	r = result(s.Handle(context.Background(), callMessage("pglens_instance_host", `{"id":"x"}`), reply(404, `{"error":"not_found"}`, nil)))
	if r["isError"] != true || text(r) != `{"error":"not_found"}` {
		t.Fatalf("404: %v", r)
	}

	r = result(s.Handle(context.Background(), callMessage("pglens_list_instances", `null`), reply(200, strings.Repeat("x", ResultLimit+1), nil)))
	if r["isError"] != true || text(r) != TooLargeText {
		t.Fatalf("oversized: %v", r)
	}

	r = result(s.Handle(context.Background(), callMessage("pglens_list_instances", `{}`), reply(0, "", errors.New("boom"))))
	if r["isError"] != true || text(r) != "call failed: boom" {
		t.Fatalf("caller error: %v", r)
	}

	never := func(context.Context, Tool, map[string]json.RawMessage) (int, []byte, error) {
		t.Fatal("caller invoked for invalid params")
		return 0, nil, nil
	}
	for _, msg := range [][]byte{
		callMessage("pglens_nope", `{}`),
		callMessage("pglens_list_instances", `{"limit":5}`),
		callMessage("pglens_instance_host", `{}`),
		callMessage("pglens_instance_host", `{"id":"../etc"}`),
		callMessage("pglens_instance_host", `{"id":"a","from":{"x":1}}`),
		callMessage("pglens_instance_host", `{"id":"a","kind":[[1]]}`),
		callMessage("pglens_instance_host", `[1]`),
		[]byte(`{"jsonrpc":"2.0","id":3,"method":"tools/call"}`),
		[]byte(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":5}}`),
	} {
		resp, _ := s.Handle(context.Background(), msg, never)
		if got := errorCode(t, resp); got != CodeInvalidParams {
			t.Fatalf("%s: code %v", msg, got)
		}
	}

	resp, _ := s.Handle(context.Background(), callMessage("pglens_list_instances", `{}`), nil)
	if got := errorCode(t, resp); got != CodeInternalError {
		t.Fatalf("nil caller: %v", got)
	}

	target, err := hostTool.Target(map[string]json.RawMessage{"id": json.RawMessage(`7`), "from": json.RawMessage(`true`)})
	if err != nil || target != "/api/v1/instances/7/host?from=true" {
		t.Fatalf("scalar target %q %v", target, err)
	}
}
