package pglog

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// AutoPlan is one plan that auto_explain wrote to the log.
type AutoPlan struct {
	DurationMS float64
	// Plan has the shape EXPLAIN (FORMAT JSON) returns: an array with one
	// object holding "Plan" and the other top-level keys, never "Query Text"
	// or "Query Parameters".
	Plan       json.RawMessage
	QueryText  string
	Parameters string
	Analyzed   bool
}

var (
	// ErrNotAutoExplain: the message has no "duration: … plan:" prefix.
	ErrNotAutoExplain = errors.New("pglog: not an auto_explain message")
	// ErrPlanFormat: the prefix is there but the plan is no JSON object
	// (auto_explain.log_format is text, xml or yaml).
	ErrPlanFormat = errors.New("pglog: auto_explain plan is not JSON")
)

const autoExplainMarker = " ms  plan:"

// ParseAutoExplain reads an auto_explain message: "duration: <ms> ms  plan:\n"
// followed by one JSON object (plan 007 R4).
func ParseAutoExplain(message string) (AutoPlan, error) {
	rest, ok := strings.CutPrefix(message, "duration: ")
	if !ok {
		return AutoPlan{}, ErrNotAutoExplain
	}
	ms, body, ok := strings.Cut(rest, autoExplainMarker)
	if !ok {
		return AutoPlan{}, ErrNotAutoExplain
	}
	duration, err := strconv.ParseFloat(strings.TrimSpace(ms), 64)
	if err != nil {
		return AutoPlan{}, ErrNotAutoExplain
	}
	body = strings.TrimSpace(body)
	if !strings.HasPrefix(body, "{") {
		return AutoPlan{}, ErrPlanFormat
	}
	var top map[string]json.RawMessage
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&top); err != nil || top["Plan"] == nil {
		return AutoPlan{}, ErrPlanFormat
	}
	p := AutoPlan{DurationMS: duration}
	if raw, ok := top["Query Text"]; ok {
		_ = json.Unmarshal(raw, &p.QueryText)
		delete(top, "Query Text")
	}
	if raw, ok := top["Query Parameters"]; ok {
		_ = json.Unmarshal(raw, &p.Parameters)
		delete(top, "Query Parameters")
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(top["Plan"], &root); err != nil {
		return AutoPlan{}, ErrPlanFormat
	}
	_, p.Analyzed = root["Actual Total Time"]
	obj, err := json.Marshal(top)
	if err != nil {
		return AutoPlan{}, ErrPlanFormat
	}
	var buf bytes.Buffer
	buf.Grow(len(obj) + 2)
	buf.WriteByte('[')
	buf.Write(obj)
	buf.WriteByte(']')
	p.Plan = buf.Bytes()
	return p, nil
}
