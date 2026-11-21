// Package pglog reads PostgreSQL structured logs (jsonlog and csvlog) and
// classifies their lines. It holds pure functions and a file reader; it never
// imports a database driver (plan 007 D9).
package pglog

import (
	"errors"
	"strings"
	"time"
)

// Entry is one log record.
type Entry struct {
	Time            time.Time
	User, Database  string
	PID             int
	SessionID       string
	LineNum         int64
	Severity        string // LOG, WARNING, ERROR, FATAL, PANIC, ...
	SQLState        string
	Message         string
	Detail          string
	Hint            string
	Context         string
	Statement       string
	ApplicationName string
	BackendType     string
	QueryID         int64
}

// MaxLineBytes is the longest record kept; a longer one is cut there (I-9).
const MaxLineBytes = 262144

// ErrBadRecord marks a record that could not be parsed.
var ErrBadRecord = errors.New("pglog: bad record")

// timeLayout is the log time without its zone word; fractional seconds are
// accepted when present.
const timeLayout = "2006-01-02 15:04:05"

// ParseTime reads a log time such as "2026-11-21 06:08:11.123 UTC". The zone
// word is not trusted: it is dropped and the rest is read in loc, the location
// of the server setting log_timezone.
func ParseTime(s string, loc *time.Location) (time.Time, error) {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, ' '); i > 0 && strings.Count(s, " ") >= 2 {
		s = s[:i]
	}
	if loc == nil {
		loc = time.UTC
	}
	t, err := time.ParseInLocation(timeLayout, s, loc)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}
