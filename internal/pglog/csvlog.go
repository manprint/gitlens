package pglog

import (
	"fmt"
	"strconv"
	"time"
)

// csvlog column positions, in the documented order (PostgreSQL 13 to 19; 13 ends at backend_type).
const (
	csvTime = iota
	csvUser
	csvDatabase
	csvPID
	csvConnectionFrom
	csvSessionID
	csvLineNum
	csvCommandTag
	csvSessionStart
	csvVXID
	csvTXID
	csvSeverity
	csvSQLState
	csvMessage
	csvDetail
	csvHint
	csvInternalQuery
	csvInternalQueryPos
	csvContext
	csvQuery
	csvQueryPos
	csvLocation
	csvApplicationName
	csvBackendType
	csvLeaderPID
	csvQueryID
	csvColumns
)

// csvMinColumns is the shortest record accepted: older layouts end at
// application_name; the missing columns read as empty.
const csvMinColumns = csvApplicationName + 1

// ParseCSVRecord reads one csvlog record of 23 to 26 columns.
func ParseCSVRecord(fields []string, loc *time.Location) (Entry, error) {
	if len(fields) < csvMinColumns || len(fields) > csvColumns {
		return Entry{}, fmt.Errorf("%w: %d columns", ErrBadRecord, len(fields))
	}
	col := func(i int) string {
		if i < len(fields) {
			return fields[i]
		}
		return ""
	}
	e := Entry{
		User:            col(csvUser),
		Database:        col(csvDatabase),
		SessionID:       col(csvSessionID),
		Severity:        col(csvSeverity),
		SQLState:        col(csvSQLState),
		Message:         col(csvMessage),
		Detail:          col(csvDetail),
		Hint:            col(csvHint),
		Context:         col(csvContext),
		Statement:       col(csvQuery),
		ApplicationName: col(csvApplicationName),
		BackendType:     col(csvBackendType),
	}
	if v := col(csvPID); v != "" {
		pid, err := strconv.Atoi(v)
		if err != nil {
			return e, fmt.Errorf("%w: pid %q", ErrBadRecord, v)
		}
		e.PID = pid
	}
	if v := col(csvLineNum); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return e, fmt.Errorf("%w: line number %q", ErrBadRecord, v)
		}
		e.LineNum = n
	}
	if v := col(csvQueryID); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return e, fmt.Errorf("%w: query id %q", ErrBadRecord, v)
		}
		e.QueryID = id
	}
	t, err := ParseTime(col(csvTime), loc)
	if err != nil {
		return e, fmt.Errorf("%w: log time %q: %w", ErrBadRecord, col(csvTime), err)
	}
	e.Time = t
	return e, nil
}
