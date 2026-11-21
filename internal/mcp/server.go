package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// Tool maps one MCP tool to one GET route of the REST API.
type Tool struct {
	Name        string
	Title       string
	Description string
	// InputSchema is the JSON Schema of the arguments (type object).
	InputSchema json.RawMessage
	// Path is the route template, for example /api/v1/instances/{id}/host.
	Path string
	// PathParams are argument names placed into Path.
	PathParams []string
	// QueryParams are argument names copied into the query string when present.
	QueryParams []string
}

// Info names the server in the initialize result.
type Info struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Caller serves one tool call; status and body are those of the REST answer.
type Caller func(ctx context.Context, tool Tool, args map[string]json.RawMessage) (status int, body []byte, err error)

// Server answers MCP messages. It keeps no state between messages.
type Server struct {
	Info         Info
	Instructions string
	Tools        []Tool
}

var pathValue = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)

var emptySchema = json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)

// errInvalidArgs marks argument problems answered with -32602.
var errInvalidArgs = errors.New("invalid arguments")

// Handle answers one JSON-RPC message. isNotification is true for a valid
// message without id: response is then nil. Handle never panics.
func (s *Server) Handle(ctx context.Context, raw []byte, call Caller) (response []byte, isNotification bool) {
	defer func() {
		if recover() != nil {
			response, isNotification = errorResponse(nil, CodeInternalError, "internal error"), false
		}
	}()
	switch firstByte(raw) {
	case '[':
		if !json.Valid(raw) {
			return errorResponse(nil, CodeParseError, "parse error"), false
		}
		return errorResponse(nil, CodeInvalidRequest, "invalid request: batches are not supported"), false
	case '{':
	default:
		if !json.Valid(raw) {
			return errorResponse(nil, CodeParseError, "parse error"), false
		}
		return errorResponse(nil, CodeInvalidRequest, "invalid request: message must be an object"), false
	}
	if !json.Valid(raw) {
		return errorResponse(nil, CodeParseError, "parse error"), false
	}
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		return errorResponse(nil, CodeInvalidRequest, "invalid request"), false
	}
	hasID := req.ID != nil
	if hasID && !validID(req.ID) {
		return errorResponse(nil, CodeInvalidRequest, "invalid request: id must be a string or a number"), false
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		return errorResponse(req.ID, CodeInvalidRequest, "invalid request"), false
	}
	if !hasID {
		return nil, true
	}
	result, rpcErr := s.dispatch(ctx, req, call)
	if rpcErr != nil {
		return errorResponse(req.ID, rpcErr.Code, rpcErr.Message), false
	}
	return resultResponse(req.ID, result), false
}

func (s *Server) dispatch(ctx context.Context, req request, call Caller) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return s.initialize(req.Params)
	case "ping", "notifications/initialized":
		return map[string]any{}, nil
	case "tools/list":
		return s.list(), nil
	case "tools/call":
		return s.call(ctx, req.Params, call)
	}
	return nil, &rpcError{Code: CodeMethodNotFound, Message: "method not found: " + req.Method}
}

func paramsObject(params json.RawMessage) bool {
	b := firstByte(params)
	return b == 0 || b == '{' || string(bytes.TrimSpace(params)) == "null"
}

func (s *Server) initialize(params json.RawMessage) (any, *rpcError) {
	if !paramsObject(params) {
		return nil, &rpcError{Code: CodeInvalidParams, Message: "invalid params"}
	}
	var p struct {
		ProtocolVersion json.RawMessage `json:"protocolVersion"`
	}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &p)
	}
	var version string
	_ = json.Unmarshal(p.ProtocolVersion, &version)
	result := map[string]any{
		"protocolVersion": Negotiate(version),
		"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
		"serverInfo":      s.Info,
	}
	if s.Instructions != "" {
		result["instructions"] = s.Instructions
	}
	return result, nil
}

type toolEntry struct {
	Name        string          `json:"name"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Annotations map[string]bool `json:"annotations"`
}

func (s *Server) list() any {
	tools := make([]toolEntry, 0, len(s.Tools))
	for _, t := range s.Tools {
		schema := t.InputSchema
		if !json.Valid(schema) {
			schema = emptySchema
		}
		tools = append(tools, toolEntry{
			Name: t.Name, Title: t.Title, Description: t.Description, InputSchema: schema,
			Annotations: map[string]bool{"readOnlyHint": true, "openWorldHint": false},
		})
	}
	return map[string]any{"tools": tools}
}

func (s *Server) tool(name string) (Tool, bool) {
	for _, t := range s.Tools {
		if t.Name == name {
			return t, true
		}
	}
	return Tool{}, false
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type callResult struct {
	Content           []textContent   `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError"`
}

func (s *Server) call(ctx context.Context, params json.RawMessage, call Caller) (any, *rpcError) {
	if firstByte(params) != '{' {
		return nil, &rpcError{Code: CodeInvalidParams, Message: "invalid params: name is required"}
	}
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: CodeInvalidParams, Message: "invalid params"}
	}
	tool, ok := s.tool(p.Name)
	if !ok {
		return nil, &rpcError{Code: CodeInvalidParams, Message: "unknown tool: " + p.Name}
	}
	args := map[string]json.RawMessage{}
	if b := firstByte(p.Arguments); b != 0 && string(bytes.TrimSpace(p.Arguments)) != "null" {
		if b != '{' || json.Unmarshal(p.Arguments, &args) != nil {
			return nil, &rpcError{Code: CodeInvalidParams, Message: "invalid params: arguments must be an object"}
		}
	}
	if _, err := tool.Target(args); err != nil {
		return nil, &rpcError{Code: CodeInvalidParams, Message: err.Error()}
	}
	if call == nil {
		return nil, &rpcError{Code: CodeInternalError, Message: "internal error"}
	}
	status, body, err := call(ctx, tool, args)
	if err != nil {
		return callResult{Content: []textContent{{Type: "text", Text: "call failed: " + err.Error()}}, IsError: true}, nil
	}
	if len(body) > ResultLimit {
		return callResult{Content: []textContent{{Type: "text", Text: TooLargeText}}, IsError: true}, nil
	}
	result := callResult{Content: []textContent{{Type: "text", Text: string(body)}}, IsError: status >= 400}
	if firstByte(body) == '{' && json.Valid(body) {
		result.StructuredContent = json.RawMessage(bytes.TrimSpace(body))
	}
	return result, nil
}

// Target builds the request URI (path and query) of a call. Every argument
// must be a declared path or query parameter; path values must match
// ^[A-Za-z0-9_.:-]{1,128}$. A null argument counts as absent.
func (t Tool) Target(args map[string]json.RawMessage) (string, error) {
	declared := map[string]bool{}
	for _, n := range t.PathParams {
		declared[n] = true
	}
	for _, n := range t.QueryParams {
		declared[n] = true
	}
	names := make([]string, 0, len(args))
	for n := range args {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if !declared[n] {
			return "", fmt.Errorf("%w: unknown argument %q", errInvalidArgs, n)
		}
	}
	path := t.Path
	for _, n := range t.PathParams {
		v, present, err := scalar(args[n])
		if err != nil || !present {
			return "", fmt.Errorf("%w: %s is required", errInvalidArgs, n)
		}
		if !pathValue.MatchString(v) {
			return "", fmt.Errorf("%w: %s has an invalid value", errInvalidArgs, n)
		}
		path = strings.ReplaceAll(path, "{"+n+"}", url.PathEscape(v))
	}
	query := url.Values{}
	for _, n := range t.QueryParams {
		v, present, err := value(args[n])
		if err != nil {
			return "", fmt.Errorf("%w: %s %s", errInvalidArgs, n, err.Error())
		}
		if present {
			query.Set(n, v)
		}
	}
	if len(query) > 0 {
		return path + "?" + query.Encode(), nil
	}
	return path, nil
}

// scalar reads a string, number or boolean argument.
func scalar(raw json.RawMessage) (string, bool, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return "", false, nil
	}
	switch raw[0] {
	case '"':
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", false, err
		}
		return s, true, nil
	case 't', 'f':
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return "", false, err
		}
		return fmt.Sprint(b), true, nil
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		var n json.Number
		if err := json.Unmarshal(raw, &n); err != nil {
			return "", false, err
		}
		return n.String(), true, nil
	}
	return "", false, errors.New("must be a string, number or boolean")
}

// value reads a scalar or an array of scalars (joined with commas).
func value(raw json.RawMessage) (string, bool, error) {
	if firstByte(raw) != '[' {
		return scalar(raw)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return "", false, err
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		v, present, err := scalar(item)
		if err != nil || !present {
			return "", false, errors.New("must hold only strings, numbers or booleans")
		}
		parts = append(parts, v)
	}
	return strings.Join(parts, ","), true, nil
}
