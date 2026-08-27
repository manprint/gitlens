package wire

import "time"

const ProtocolVersion = 1

type Envelope struct {
	ProtocolVersion int        `json:"protocol_version"`
	AgentID         string     `json:"agent_id"`
	AgentVersion    string     `json:"agent_version"`
	SentAt          time.Time  `json:"sent_at"`
	Instances       []Instance `json:"instances"`
}

type Instance struct {
	InstanceID      string     `json:"instance_id"`
	ClusterID       string     `json:"cluster_id"`        // decimal string (D18)
	ClusterIDSource string     `json:"cluster_id_source"` // system_identifier | manual
	Addr            string     `json:"addr"`
	Port            int        `json:"port"`
	PGVersion       int        `json:"pg_version"`
	Role            string     `json:"role"`
	PermTier        string     `json:"perm_tier"`
	Capabilities    []string   `json:"capabilities"`
	Databases       []Database `json:"databases"`
	Results         []Result   `json:"results"`
	TopologyEdges   []Edge     `json:"topology_edges,omitempty"`
}

type Database struct {
	Name       string `json:"name"`
	Monitored  bool   `json:"monitored"`
	SkipReason string `json:"skip_reason,omitempty"`
}

type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
}

type Result struct {
	Check      string            `json:"check"`
	TS         time.Time         `json:"ts"`
	Database   string            `json:"database,omitempty"`
	StatsReset *time.Time        `json:"stats_reset,omitempty"`
	Truncated  bool              `json:"truncated,omitempty"`
	Error      string            `json:"error,omitempty"`
	SkipReason string            `json:"skip_reason,omitempty"`
	Metrics    []Metric          `json:"metrics,omitempty"`
	QueryTexts map[string]string `json:"query_texts,omitempty"` // queryid -> text
}

type Metric struct {
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels,omitempty"`
	Value  float64           `json:"value"`
	Kind   string            `json:"kind"`
}
