package command

import (
	"fmt"

	"github.com/manprint/pglens/internal/pgtype"
)

// Kind identifies an operation that an agent may execute on a monitored
// PostgreSQL instance.
type Kind string

const (
	KindExplain     Kind = "explain"
	KindCancel      Kind = "cancel"
	KindTerminate   Kind = "terminate"
	KindPgstattuple Kind = "pgstattuple"
)

// Args is the union of every command's arguments. Only the fields relevant to
// the kind may be set; Validate rejects the rest, so a caller cannot smuggle a
// field past the executor.
type Args struct {
	QueryID  *int64 `json:"queryid,omitempty"`
	Datname  string `json:"datname,omitempty"`
	Analyze  bool   `json:"analyze,omitempty"`
	PID      *int   `json:"pid,omitempty"`
	Schema   string `json:"schema,omitempty"`
	Relation string `json:"relation,omitempty"`
}

// Gates is what the agent knows about one target's permissions.
type Gates struct {
	Tier                pgtype.PermTier
	AllowExplainAnalyze bool
	AllowSignal         bool
	HasPgstattuple      bool
}

// Validate checks that Args contains only the arguments supported by kind.
func (a Args) Validate(k Kind) error {
	if !knownKind(k) {
		return fmt.Errorf("unknown command kind %q", k)
	}
	if !validDatname(a.Datname) {
		return fmt.Errorf("datname contains an unsafe identifier character")
	}

	switch k {
	case KindExplain:
		if a.QueryID == nil {
			return fmt.Errorf("explain requires queryid")
		}
		if a.PID != nil {
			return fmt.Errorf("explain rejects pid")
		}
		if a.Schema != "" || a.Relation != "" {
			return fmt.Errorf("explain rejects schema and relation")
		}
	case KindCancel, KindTerminate:
		if a.PID == nil {
			return fmt.Errorf("%s requires pid", k)
		}
		if a.QueryID != nil {
			return fmt.Errorf("%s rejects queryid", k)
		}
		if a.Analyze {
			return fmt.Errorf("%s rejects analyze", k)
		}
		if a.Datname != "" || a.Schema != "" || a.Relation != "" {
			return fmt.Errorf("%s rejects unrelated arguments", k)
		}
	case KindPgstattuple:
		if a.Schema == "" || a.Relation == "" {
			return fmt.Errorf("pgstattuple requires schema and relation")
		}
		if a.QueryID != nil || a.PID != nil || a.Analyze {
			return fmt.Errorf("pgstattuple rejects unrelated arguments")
		}
	}
	return nil
}

func knownKind(k Kind) bool {
	switch k {
	case KindExplain, KindCancel, KindTerminate, KindPgstattuple:
		return true
	default:
		return false
	}
}

func validDatname(datname string) bool {
	for _, r := range datname {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') &&
			(r < '0' || r > '9') && r != '_' && r != '$' {
			return false
		}
	}
	return true
}
