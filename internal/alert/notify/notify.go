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

// isTest reports whether m is the console's test message (Phase "test"). It
// names no cluster, instance or database, so the chat channels leave out the
// context line that would show them empty.
func isTest(m Message) bool { return m.Phase == "test" }
