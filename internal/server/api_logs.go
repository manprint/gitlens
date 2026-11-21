package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/manprint/pglens/internal/pglog"
)

const (
	defaultLogsLimit = 100
	maxLogsLimit     = 500
	defaultLogsRange = time.Hour
)

// logSeverities are the error_severity values PostgreSQL writes.
var logSeverities = []string{"DEBUG1", "DEBUG2", "DEBUG3", "DEBUG4", "DEBUG5", "LOG", "INFO", "NOTICE", "WARNING", "ERROR", "FATAL", "PANIC"}

type logEventResponse struct {
	TS              time.Time       `json:"ts"`
	Class           string          `json:"class"`
	Severity        string          `json:"severity"`
	SQLState        string          `json:"sqlstate"`
	Datname         string          `json:"datname"`
	Usename         string          `json:"usename"`
	ApplicationName string          `json:"application_name"`
	PID             *int32          `json:"pid"`
	QueryID         *string         `json:"queryid"`
	Message         string          `json:"message"`
	Extra           json.RawMessage `json:"extra"`
	TraceID         *string         `json:"trace_id"`
}

type logClassResponse struct {
	Class  string    `json:"class"`
	Count  int64     `json:"count"`
	LastTS time.Time `json:"last_ts"`
}

func (a *API) registerLogRoutes(r chi.Router) {
	r.Get("/api/v1/logs", a.handleLogs)
	r.Get("/api/v1/logs/classes", a.handleLogClasses)
}

// logWindow reads instance_id, from and to; to defaults to now and from to
// an hour before to.
func logWindow(w http.ResponseWriter, r *http.Request) (uuid.UUID, time.Time, time.Time, bool) {
	q := r.URL.Query()
	if q.Get("instance_id") == "" {
		writeError(w, http.StatusBadRequest, "missing instance_id", "query param instance_id is required")
		return uuid.UUID{}, time.Time{}, time.Time{}, false
	}
	id, err := uuid.Parse(q.Get("instance_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", err.Error())
		return uuid.UUID{}, time.Time{}, time.Time{}, false
	}
	to := time.Now().UTC()
	if s := q.Get("to"); s != "" {
		if to, err = parseTimeParam(s); err != nil {
			writeError(w, http.StatusBadRequest, "invalid to", err.Error())
			return uuid.UUID{}, time.Time{}, time.Time{}, false
		}
	}
	from := to.Add(-defaultLogsRange)
	if s := q.Get("from"); s != "" {
		if from, err = parseTimeParam(s); err != nil {
			writeError(w, http.StatusBadRequest, "invalid from", err.Error())
			return uuid.UUID{}, time.Time{}, time.Time{}, false
		}
	}
	if !from.Before(to) {
		writeError(w, http.StatusBadRequest, "invalid range", "from must be before to")
		return uuid.UUID{}, time.Time{}, time.Time{}, false
	}
	return id, from, to, true
}

func (a *API) handleLogs(w http.ResponseWriter, r *http.Request) {
	id, from, to, ok := logWindow(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	limit := defaultLogsLimit
	if s := q.Get("limit"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil || v < 1 || v > maxLogsLimit {
			writeError(w, http.StatusBadRequest, "invalid limit", fmt.Sprintf("limit must be between 1 and %d, got %q", maxLogsLimit, s))
			return
		}
		limit = v
	}
	var sb strings.Builder
	sb.WriteString(`SELECT ts, class, severity, sqlstate, datname, usename, application_name, pid, queryid, message, extra, trace_id
FROM log_events WHERE tenant_id='default' AND instance_id=$1 AND ts >= $2 AND ts < $3`)
	args := []any{id, from, to}
	add := func(cond string, v any) {
		args = append(args, v)
		fmt.Fprintf(&sb, cond, len(args))
	}
	if s := q.Get("class"); s != "" {
		classes := eventTypeList(s)
		for _, c := range classes {
			if !slices.Contains(pglog.Classes(), c) {
				writeError(w, http.StatusBadRequest, "invalid class", fmt.Sprintf("unknown log class %q", c))
				return
			}
		}
		add(" AND class = ANY($%d)", classes)
	}
	if s := q.Get("severity"); s != "" {
		if !slices.Contains(logSeverities, s) {
			writeError(w, http.StatusBadRequest, "invalid severity", fmt.Sprintf("unknown severity %q", s))
			return
		}
		add(" AND severity = $%d", s)
	}
	if s := q.Get("database"); s != "" {
		add(" AND datname = $%d", s)
	}
	if s := q.Get("q"); s != "" {
		add(" AND position(lower($%d) in lower(message)) > 0", s)
	}
	if s := q.Get("before"); s != "" {
		before, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid before", err.Error())
			return
		}
		add(" AND ts < $%d", before)
	}
	add(" ORDER BY ts DESC LIMIT $%d", limit)

	out := []logEventResponse{}
	if a.pool == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := a.pool.Query(ctx, sb.String(), args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed", err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var e logEventResponse
		var queryID *int64
		var extra []byte
		if err := rows.Scan(&e.TS, &e.Class, &e.Severity, &e.SQLState, &e.Datname, &e.Usename, &e.ApplicationName, &e.PID, &queryID, &e.Message, &extra, &e.TraceID); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed", err.Error())
			return
		}
		if queryID != nil {
			s := strconv.FormatInt(*queryID, 10)
			e.QueryID = &s
		}
		if len(extra) > 0 {
			e.Extra = extra
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) handleLogClasses(w http.ResponseWriter, r *http.Request) {
	id, from, to, ok := logWindow(w, r)
	if !ok {
		return
	}
	out := []logClassResponse{}
	if a.pool == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := a.pool.Query(ctx, `SELECT class, count(*), max(ts) FROM log_events
WHERE tenant_id='default' AND instance_id=$1 AND ts >= $2 AND ts < $3
GROUP BY class ORDER BY count(*) DESC, class`, id, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed", err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var c logClassResponse
		if err := rows.Scan(&c.Class, &c.Count, &c.LastTS); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed", err.Error())
			return
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}
