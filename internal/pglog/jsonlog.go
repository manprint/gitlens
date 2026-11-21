package pglog

import (
	"encoding/json"
	"fmt"
	"time"
)

// jsonRecord holds the jsonlog keys pglog reads; any other key is ignored.
type jsonRecord struct {
	Timestamp       string `json:"timestamp"`
	User            string `json:"user"`
	DBName          string `json:"dbname"`
	PID             int    `json:"pid"`
	SessionID       string `json:"session_id"`
	LineNum         int64  `json:"line_num"`
	ErrorSeverity   string `json:"error_severity"`
	StateCode       string `json:"state_code"`
	Message         string `json:"message"`
	Detail          string `json:"detail"`
	Hint            string `json:"hint"`
	Context         string `json:"context"`
	Statement       string `json:"statement"`
	ApplicationName string `json:"application_name"`
	BackendType     string `json:"backend_type"`
	QueryID         int64  `json:"query_id"`
}

// ParseJSONLine reads one jsonlog line. A line that is not a JSON object (a
// line cut at MaxLineBytes among them) still gives an Entry: its raw text as
// Message and severity LOG, together with ErrBadRecord.
func ParseJSONLine(line []byte, loc *time.Location) (Entry, error) {
	var r jsonRecord
	if err := json.Unmarshal(line, &r); err != nil {
		return Entry{Severity: "LOG", Message: string(line)}, fmt.Errorf("%w: %w", ErrBadRecord, err)
	}
	e := Entry{
		User:            r.User,
		Database:        r.DBName,
		PID:             r.PID,
		SessionID:       r.SessionID,
		LineNum:         r.LineNum,
		Severity:        r.ErrorSeverity,
		SQLState:        r.StateCode,
		Message:         r.Message,
		Detail:          r.Detail,
		Hint:            r.Hint,
		Context:         r.Context,
		Statement:       r.Statement,
		ApplicationName: r.ApplicationName,
		BackendType:     r.BackendType,
		QueryID:         r.QueryID,
	}
	t, err := ParseTime(r.Timestamp, loc)
	if err != nil {
		return e, fmt.Errorf("%w: timestamp %q: %w", ErrBadRecord, r.Timestamp, err)
	}
	e.Time = t
	return e, nil
}
