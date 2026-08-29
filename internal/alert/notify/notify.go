package notify

import (
	"context"
	"time"
)

type Message struct {
	DedupID  string            `json:"dedup_id"`
	RuleID   string            `json:"rule_id"`
	Severity string            `json:"severity"`
	Phase    string            `json:"phase"`
	Summary  string            `json:"summary"`
	Cluster  string            `json:"cluster"`
	Instance string            `json:"instance"`
	Database string            `json:"database"`
	Value    float64           `json:"value"`
	At       time.Time         `json:"at"`
	Labels   map[string]string `json:"labels,omitempty"`
}

type Channel interface {
	Name() string
	Send(context.Context, Message) error
}
