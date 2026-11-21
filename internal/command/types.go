package command

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

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
	// KindIndexCheck plans a statement with and without a hypothetical index
	// (hypopg) inside a read-only transaction; nothing is created.
	KindIndexCheck Kind = "index_check"
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
	// Params are the values of an explained statement's $1, $2, ... in
	// order, as text the server reads with each parameter's type; null is SQL
	// NULL. They are bound, never spliced into the statement.
	Params []*string `json:"params,omitempty"`
	// IndexSchema, IndexRelation, IndexColumns and IndexInclude describe the
	// hypothetical btree index of an index_check. They are identifiers, never
	// expressions: the executor quotes each one.
	IndexSchema   string   `json:"index_schema,omitempty"`
	IndexRelation string   `json:"index_relation,omitempty"`
	IndexColumns  []string `json:"index_columns,omitempty"`
	IndexInclude  []string `json:"index_include,omitempty"`
}

// Limits on Params: PostgreSQL statements rarely take more, and the values
// travel in the command row.
const (
	MaxParams          = 100
	MaxParamValueBytes = 8 << 10
)

// UnmarshalJSON accepts both legacy JSON numbers and string query IDs. The
// string form is required by browser clients so int64 identifiers do not lose
// precision when they cross the JavaScript boundary.
func (a *Args) UnmarshalJSON(data []byte) error {
	type wireArgs struct {
		QueryID       json.RawMessage `json:"queryid"`
		Datname       string          `json:"datname,omitempty"`
		Analyze       bool            `json:"analyze,omitempty"`
		PID           *int            `json:"pid,omitempty"`
		Schema        string          `json:"schema,omitempty"`
		Relation      string          `json:"relation,omitempty"`
		BackendStart  *time.Time      `json:"backend_start,omitempty"`
		Params        []*string       `json:"params,omitempty"`
		IndexSchema   string          `json:"index_schema,omitempty"`
		IndexRelation string          `json:"index_relation,omitempty"`
		IndexColumns  []string        `json:"index_columns,omitempty"`
		IndexInclude  []string        `json:"index_include,omitempty"`
	}

	var wire wireArgs
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return err
	}

	*a = Args{
		Datname:       wire.Datname,
		Analyze:       wire.Analyze,
		PID:           wire.PID,
		Schema:        wire.Schema,
		Relation:      wire.Relation,
		BackendStart:  wire.BackendStart,
		Params:        wire.Params,
		IndexSchema:   wire.IndexSchema,
		IndexRelation: wire.IndexRelation,
		IndexColumns:  wire.IndexColumns,
		IndexInclude:  wire.IndexInclude,
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
	if k != KindIndexCheck && a.hasIndexFields() {
		return fmt.Errorf("%s rejects index_schema, index_relation, index_columns and index_include", k)
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
		if err := validateParams(a.Params); err != nil {
			return err
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
		if a.Datname != "" || a.Schema != "" || a.Relation != "" || a.Params != nil {
			return fmt.Errorf("%s rejects unrelated arguments", k)
		}
	case KindPgstattuple:
		if a.Schema == "" || a.Relation == "" {
			return fmt.Errorf("pgstattuple requires schema and relation")
		}
		if a.QueryID != nil || a.PID != nil || a.Analyze || a.BackendStart != nil || a.Params != nil {
			return fmt.Errorf("pgstattuple rejects unrelated arguments")
		}
	case KindIndexCheck:
		if a.QueryID == nil {
			return fmt.Errorf("index_check requires queryid")
		}
		if a.Datname == "" {
			return fmt.Errorf("index_check requires datname")
		}
		if a.PID != nil || a.Analyze || a.BackendStart != nil || a.Params != nil || a.Schema != "" || a.Relation != "" {
			return fmt.Errorf("index_check rejects unrelated arguments")
		}
		return a.validateIndexFields()
	}
	return nil
}

// Limits on the hypothetical index of an index_check.
const (
	maxIndexColumns  = 32
	maxIdentifierLen = 63
)

func (a Args) hasIndexFields() bool {
	return a.IndexSchema != "" || a.IndexRelation != "" || a.IndexColumns != nil || a.IndexInclude != nil
}

func (a Args) validateIndexFields() error {
	if err := validateIdentifier("index_schema", a.IndexSchema); err != nil {
		return err
	}
	if err := validateIdentifier("index_relation", a.IndexRelation); err != nil {
		return err
	}
	if len(a.IndexColumns) == 0 || len(a.IndexColumns) > maxIndexColumns {
		return fmt.Errorf("index_columns takes 1 to %d columns", maxIndexColumns)
	}
	if len(a.IndexInclude) > maxIndexColumns {
		return fmt.Errorf("index_include takes at most %d columns", maxIndexColumns)
	}
	seen := map[string]bool{}
	for _, name := range a.IndexColumns {
		if err := validateIdentifier("index_columns", name); err != nil {
			return err
		}
	}
	for _, name := range a.IndexInclude {
		if err := validateIdentifier("index_include", name); err != nil {
			return err
		}
	}
	for _, name := range append(append([]string{}, a.IndexColumns...), a.IndexInclude...) {
		if seen[name] {
			return fmt.Errorf("index_columns and index_include list column %q twice", name)
		}
		seen[name] = true
	}
	return nil
}

// validateIdentifier accepts 1 to 63 bytes without NUL or control
// characters; the executor quotes the name, so other characters are safe.
func validateIdentifier(field, name string) error {
	if name == "" || len(name) > maxIdentifierLen {
		return fmt.Errorf("%s must be 1 to %d bytes", field, maxIdentifierLen)
	}
	for _, r := range name {
		if r == 0 || unicode.IsControl(r) {
			return fmt.Errorf("%s contains a NUL byte or a control character", field)
		}
	}
	return nil
}

func validateParams(params []*string) error {
	if len(params) > MaxParams {
		return fmt.Errorf("explain takes at most %d parameter values", MaxParams)
	}
	for i, value := range params {
		if value == nil {
			continue
		}
		if len(*value) > MaxParamValueBytes {
			return fmt.Errorf("parameter $%d is longer than %d bytes", i+1, MaxParamValueBytes)
		}
		if strings.ContainsRune(*value, 0) {
			return fmt.Errorf("parameter $%d contains a NUL byte", i+1)
		}
	}
	return nil
}

func knownKind(k Kind) bool {
	switch k {
	case KindExplain, KindCancel, KindTerminate, KindPgstattuple, KindIndexCheck:
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
