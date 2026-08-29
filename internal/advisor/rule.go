package advisor

import (
	"strconv"

	"github.com/google/uuid"
	"github.com/manprint/pglens/internal/alert"
	"github.com/manprint/pglens/internal/pgtype"
)

type Severity = alert.Severity

const (
	SeverityCritical = alert.SeverityCritical
	SeverityWarning  = alert.SeverityWarning
	SeverityInfo     = alert.SeverityInfo
)

type Scope string

const (
	ScopeInstance Scope = "instance"
	ScopeCluster  Scope = "cluster"
	ScopeDatabase Scope = "database"
	ScopeRelation Scope = "relation"
)

type State string

const (
	StateOpen     State = "open"
	StateDegraded State = "degraded"
	StateMuted    State = "muted"
	StateResolved State = "resolved"
)

// Finding is one statement about one stable subject.
type Finding struct {
	RuleID         string
	Severity       Severity
	State          State
	Scope          Scope
	ClusterID      *int64
	InstanceID     *uuid.UUID
	Datname        string
	ObjectName     string
	Title          string
	Detail         string
	Remediation    string
	Evidence       map[string]any
	DegradedReason string
}

// ID is stable across evaluations for the same rule and subject.
func (f Finding) ID() string {
	subject := f.ObjectName
	if subject == "" {
		subject = f.Datname
	}
	if subject == "" && f.InstanceID != nil {
		subject = f.InstanceID.String()
	}
	if subject == "" && f.ClusterID != nil {
		subject = "cluster:" + formatInt(*f.ClusterID)
	}
	return f.RuleID + "/" + subject
}

func formatInt(v int64) string {
	if v == 0 {
		return "0"
	}
	return strconv.FormatInt(v, 10)
}

type Rule interface {
	ID() string
	Severity() Severity
	Scope() Scope
	Needs() []string
	MinTier() pgtype.PermTier
	Evaluate(s *Snapshot) []Finding
}
