package command

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

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

// MaxResultBytes bounds one result as an agent posts it, counting the whole
// JSON body. An EXPLAIN plan of a wide view runs to megabytes, so the limit is
// well above the 1 MiB a command request may take. The agent replaces a larger
// result with an error that says why, and the server refuses one that arrives
// anyway.
const MaxResultBytes = 8 << 20

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
	// BackendStart is the start time of the session the caller saw at PID. It
	// is optional; when it is set, the agent signals only a session that
	// started at this time, and refuses any other session that holds the PID.
	BackendStart *time.Time `json:"backend_start,omitempty"`
}

// UnmarshalJSON accepts both legacy JSON numbers and string query IDs. The
// string form is required by browser clients so int64 identifiers do not lose
// precision when they cross the JavaScript boundary.
func (a *Args) UnmarshalJSON(data []byte) error {
	type wireArgs struct {
		QueryID      json.RawMessage `json:"queryid"`
		Datname      string          `json:"datname,omitempty"`
		Analyze      bool            `json:"analyze,omitempty"`
		PID          *int            `json:"pid,omitempty"`
		Schema       string          `json:"schema,omitempty"`
		Relation     string          `json:"relation,omitempty"`
		BackendStart *time.Time      `json:"backend_start,omitempty"`
	}

	var wire wireArgs
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return err
	}

	*a = Args{
		Datname:      wire.Datname,
		Analyze:      wire.Analyze,
		PID:          wire.PID,
		Schema:       wire.Schema,
		Relation:     wire.Relation,
		BackendStart: wire.BackendStart,
	}
	if len(wire.QueryID) == 0 || bytes.Equal(wire.QueryID, []byte("null")) {
		return nil
	}

	var queryID int64
	if err := json.Unmarshal(wire.QueryID, &queryID); err == nil {
		a.QueryID = &queryID
		return nil
	}
	var queryIDText string
	if err := json.Unmarshal(wire.QueryID, &queryIDText); err != nil {
		return fmt.Errorf("queryid must be an integer or decimal string")
	}
	queryID, err := strconv.ParseInt(queryIDText, 10, 64)
	if err != nil {
		return fmt.Errorf("queryid must be a valid int64: %w", err)
	}
	a.QueryID = &queryID
	return nil
}

// Gates is what the agent knows about one target's permissions.
type Gates struct {
	Tier                pgtype.PermTier
	AllowExplainAnalyze bool
	AllowSignal         bool
}

// Validate checks that Args contains only the arguments supported by kind.
func (a Args) Validate(k Kind) error {
	if !knownKind(k) {
		return fmt.Errorf("unknown command kind %q", k)
	}
	if err := validateDatname(a.Datname); err != nil {
		return err
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
		if a.BackendStart != nil {
			return fmt.Errorf("explain rejects backend_start")
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
		if a.QueryID != nil || a.PID != nil || a.Analyze || a.BackendStart != nil {
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

// maxDatnameBytes is PostgreSQL's limit on an identifier (NAMEDATALEN-1).
const maxDatnameBytes = 63

// validateDatname accepts any name PostgreSQL can hold for a database. The
// name is never spliced into SQL text: it reaches the server as a connection
// or startup parameter, or as a bound argument, so its characters do not make
// a command unsafe. What is checked is what the command store and the
// connection need: a NUL byte, which jsonb cannot store, and a length that
// PostgreSQL cannot give a database.
func validateDatname(datname string) error {
	if len(datname) > maxDatnameBytes {
		return fmt.Errorf("datname is longer than %d bytes", maxDatnameBytes)
	}
	if strings.ContainsRune(datname, 0) {
		return fmt.Errorf("datname contains a NUL byte")
	}
	return nil
}
