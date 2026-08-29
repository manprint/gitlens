package alert

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityWarning  Severity = "warning"
	SeverityInfo     Severity = "info"
)

type Scope string

const (
	ScopeInstance Scope = "instance"
	ScopeCluster  Scope = "cluster"
	ScopeDatabase Scope = "database"
)

type State string

const (
	StatePending  State = "pending"
	StateFiring   State = "firing"
	StateResolved State = "resolved"
)

type Comparator string

const (
	GT Comparator = "gt"
	GE Comparator = "ge"
	LT Comparator = "lt"
	LE Comparator = "le"
	EQ Comparator = "eq"
	NE Comparator = "ne"
)

type Tier int

const (
	Tier0 Tier = 0
	Tier1 Tier = 1
)

type Rule struct {
	ID         string
	Tier       Tier
	Enabled    bool
	Severity   Severity
	Scope      Scope
	Metric     string
	Comparator Comparator
	Threshold  float64
	For        time.Duration
	EventType  string
	Summary    string
}

func (r Rule) Validate() error {
	if r.ID == "" {
		return fmt.Errorf("rule id is empty")
	}
	if r.Severity != SeverityCritical && r.Severity != SeverityWarning && r.Severity != SeverityInfo {
		return fmt.Errorf("unknown severity %q", r.Severity)
	}
	if r.Scope != ScopeInstance && r.Scope != ScopeCluster && r.Scope != ScopeDatabase {
		return fmt.Errorf("unknown scope %q", r.Scope)
	}
	if r.Metric == "" && r.EventType == "" {
		return fmt.Errorf("rule has neither metric nor event type")
	}
	if r.Metric != "" && r.EventType != "" {
		return fmt.Errorf("rule has both metric and event type")
	}
	if r.Metric != "" && r.Comparator != GT && r.Comparator != GE && r.Comparator != LT && r.Comparator != LE && r.Comparator != EQ && r.Comparator != NE {
		return fmt.Errorf("unknown comparator %q", r.Comparator)
	}
	if r.For < 0 {
		return fmt.Errorf("rule for duration is negative")
	}
	return nil
}

type Sample struct {
	ClusterID  *int64
	InstanceID *uuid.UUID
	Datname    string
	Labels     map[string]string
	Value      float64
	TS         time.Time
}
type Alert struct {
	Key        string
	RuleID     string
	Severity   Severity
	State      State
	ClusterID  *int64
	InstanceID *uuid.UUID
	Datname    string
	Labels     map[string]string
	Value      float64
	Summary    string
	StartedAt  time.Time
	LastEvalAt time.Time
	ResolvedAt *time.Time
	Suppressed bool
}
