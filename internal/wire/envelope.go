package wire

import (
	"encoding/json"
	"fmt"
	"time"
)

// Protocol 3 adds Result.partial. The server decodes envelopes strictly, so a
// v2 server has to refuse a v3 agent by version, with a message that says to
// upgrade the server, rather than by an unknown field.
const (
	ProtocolVersionMin     = 1
	ProtocolVersionCurrent = 3
	ProtocolVersion        = ProtocolVersionCurrent
)

// TransmittedAtHeader carries the moment the agent actually sent a push
// request (RFC 3339). SentAt is when the envelope was built and queued, which
// after an outage is hours earlier than its delivery.
const TransmittedAtHeader = "X-Pglens-Transmitted-At"

type Envelope struct {
	ProtocolVersion int        `json:"protocol_version"`
	AgentID         string     `json:"agent_id"`
	AgentVersion    string     `json:"agent_version"`
	SentAt          time.Time  `json:"sent_at"`
	Instances       []Instance `json:"instances"`
	// TransmittedAt is filled by the server from TransmittedAtHeader. It is
	// what the agent's clock skew is measured against: measuring it against
	// SentAt reported every replayed envelope's queueing delay as skew.
	TransmittedAt time.Time `json:"-"`
}

type Instance struct {
	InstanceID      string     `json:"instance_id"`
	TargetName      string     `json:"target_name,omitempty"` // logical agent target name, useful when addr is shared
	ClusterID       string     `json:"cluster_id"`            // decimal string (D18)
	ClusterIDSource string     `json:"cluster_id_source"`     // system_identifier | manual
	Addr            string     `json:"addr"`
	Port            int        `json:"port"`
	PGVersion       int        `json:"pg_version"`
	Role            string     `json:"role"`
	PermTier        string     `json:"perm_tier"`
	Capabilities    []string   `json:"capabilities"`
	Databases       []Database `json:"databases"`
	Results         []Result   `json:"results"`
	TopologyEdges   []Edge     `json:"topology_edges,omitempty"`
	// ASHEnabled reports the agent's own checks.ash.enabled config, so the
	// server can distinguish "ASH is switched off" from "no data yet" —
	// nil means an agent build that predates this field (unknown, treated
	// as enabled by the server). SYS-ASH-002.
	ASHEnabled *bool `json:"ash_enabled,omitempty"`
}

type Database struct {
	Name       string `json:"name"`
	Monitored  bool   `json:"monitored"`
	SkipReason string `json:"skip_reason,omitempty"`
}

type Edge struct {
	From       string `json:"from"`
	To         string `json:"to"`             // the upstream's addr, resolved to an instance_id server-side
	Port       int    `json:"port,omitempty"` // disambiguates targets sharing an address
	Type       string `json:"type"`
	Confidence string `json:"confidence"` // "high" (actively streaming) | "low" (last-known upstream, not currently connected)
}

type Result struct {
	Check      string     `json:"check"`
	TS         time.Time  `json:"ts"`
	Database   string     `json:"database,omitempty"`
	StatsReset *time.Time `json:"stats_reset,omitempty"`
	Truncated  bool       `json:"truncated,omitempty"`
	// Partial marks a deliberate top-N subset: objects absent from this
	// result were not observed, which is not the same as gone. Truncated is
	// reserved for a hard cardinality cap cutting into that subset.
	Partial    bool              `json:"partial,omitempty"`
	Error      string            `json:"error,omitempty"`
	SkipReason string            `json:"skip_reason,omitempty"`
	Metrics    []Metric          `json:"metrics,omitempty"`
	Facts      []Fact            `json:"facts,omitempty"`
	QueryTexts map[string]string `json:"query_texts,omitempty"` // queryid -> text
}

// Fact is an observation that is not a number: an index definition, a GUC
// value, a lock tree, or a query plan. Exactly one value representation is set.
type Fact struct {
	Kind      string            `json:"kind"`
	Key       string            `json:"key"`
	Labels    map[string]string `json:"labels,omitempty"`
	ValueText string            `json:"value_text,omitempty"`
	ValueJSON json.RawMessage   `json:"value_json,omitempty"`
}

func (f Fact) Validate() error {
	if f.Kind == "" {
		return fmt.Errorf("fact kind is required")
	}
	if f.Key == "" {
		return fmt.Errorf("fact key is required")
	}
	switch f.Kind {
	case "index_def", "setting", "lock_tree", "plan", "check_skip":
	default:
		return fmt.Errorf("unknown fact kind %q", f.Kind)
	}
	textSet := f.ValueText != ""
	jsonSet := len(f.ValueJSON) > 0
	if textSet == jsonSet {
		return fmt.Errorf("exactly one fact value is required")
	}
	if jsonSet && !json.Valid(f.ValueJSON) {
		return fmt.Errorf("fact value_json is malformed")
	}
	return nil
}

type Metric struct {
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels,omitempty"`
	Value  float64           `json:"value"`
	Kind   string            `json:"kind"`
}
