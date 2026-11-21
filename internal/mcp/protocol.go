// Package mcp answers Model Context Protocol messages (JSON-RPC 2.0 over
// Streamable HTTP, one message per POST, no stream and no session). It maps
// every tool to one read-only GET request and leaves the HTTP layer, the
// credential and the request itself to the caller. Standard library only.
package mcp

import (
	"bytes"
	"encoding/json"
)

// JSON-RPC error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// LatestVersion is the protocol version answered when the client asks for one
// this server does not speak.
const LatestVersion = "2025-11-25"

// ResultLimit caps the REST body a tool call returns.
const ResultLimit = 262144

// TooLargeText is the tool result text when the body exceeds ResultLimit.
const TooLargeText = "result too large: narrow time range or lower limit"

var supportedVersions = []string{LatestVersion, "2025-06-18"}

// SupportedVersions lists the protocol versions this server speaks, newest first.
func SupportedVersions() []string {
	return append([]string(nil), supportedVersions...)
}

// Supported reports whether version is one this server speaks.
func Supported(version string) bool {
	for _, v := range supportedVersions {
		if v == version {
			return true
		}
	}
	return false
}

// Negotiate returns the client's version when supported, else LatestVersion.
func Negotiate(version string) string {
	if Supported(version) {
		return version
	}
	return LatestVersion
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

var nullID = json.RawMessage("null")

// validID accepts a JSON string or number; null and every other type are invalid.
func validID(id json.RawMessage) bool {
	id = bytes.TrimSpace(id)
	if len(id) == 0 {
		return false
	}
	switch id[0] {
	case '"':
		var s string
		return json.Unmarshal(id, &s) == nil
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		var n json.Number
		return json.Unmarshal(id, &n) == nil
	}
	return false
}

func encode(r response) []byte {
	r.JSONRPC = "2.0"
	if len(r.ID) == 0 {
		r.ID = nullID
	}
	out, err := json.Marshal(r)
	if err != nil {
		out, _ = json.Marshal(response{JSONRPC: "2.0", ID: nullID, Error: &rpcError{Code: CodeInternalError, Message: "internal error"}})
	}
	return out
}

func errorResponse(id json.RawMessage, code int, message string) []byte {
	return encode(response{ID: id, Error: &rpcError{Code: code, Message: message}})
}

func resultResponse(id json.RawMessage, result any) []byte {
	return encode(response{ID: id, Result: result})
}

func firstByte(raw []byte) byte {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return 0
	}
	return raw[0]
}
