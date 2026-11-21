package server

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	defaultAutovacuumLimit = 200
	maxAutovacuumLimit     = 1000
	defaultAutovacuumRange = 7 * 24 * time.Hour
)

// autovacuumOrders maps order_by to its column; every order is descending.
var autovacuumOrders = map[string]string{
	"ts": "ts", "elapsed_seconds": "elapsed_seconds", "tuples_removed": "tuples_removed",
}

// autovacuumRunJSON is one row of GET /api/v1/autovacuum/runs: every column of
// autovacuum_runs except the tenant and cluster; a number the log line did not
// carry is null.
type autovacuumRunJSON struct {
	TS                     time.Time `json:"ts"`
	Datname                string    `json:"datname"`
	Schemaname             string    `json:"schemaname"`
	Relname                string    `json:"relname"`
	Kind                   string    `json:"kind"`
	Aggressive             bool      `json:"aggressive"`
	Wraparound             bool      `json:"wraparound"`
	IndexScans             *int64    `json:"index_scans"`
	PagesRemoved           *int64    `json:"pages_removed"`
	PagesRemain            *int64    `json:"pages_remain"`
	PagesScanned           *int64    `json:"pages_scanned"`
	TuplesRemoved          *int64    `json:"tuples_removed"`
	TuplesRemain           *int64    `json:"tuples_remain"`
	TuplesDeadNotRemovable *int64    `json:"tuples_dead_not_removable"`
	BufferHits             *int64    `json:"buffer_hits"`
	BufferReads            *int64    `json:"buffer_reads"`
	BufferDirtied          *int64    `json:"buffer_dirtied"`
	WALRecords             *int64    `json:"wal_records"`
	WALFPI                 *int64    `json:"wal_fpi"`
	WALBytes               *int64    `json:"wal_bytes"`
	CPUUserSeconds         *float64  `json:"cpu_user_seconds"`
	CPUSystemSeconds       *float64  `json:"cpu_system_seconds"`
	ElapsedSeconds         *float64  `json:"elapsed_seconds"`
}

// autovacuumSummaryJSON is one table of GET /api/v1/autovacuum/summary.
type autovacuumSummaryJSON struct {
	Datname             string    `json:"datname"`
	Schemaname          string    `json:"schemaname"`
	Relname             string    `json:"relname"`
	Runs                int64     `json:"runs"`
	TotalElapsedSeconds float64   `json:"total_elapsed_seconds"`
	MaxElapsedSeconds   float64   `json:"max_elapsed_seconds"`
	TuplesRemoved       int64     `json:"tuples_removed"`
	LastTS              time.Time `json:"last_ts"`
	WraparoundRuns      int64     `json:"wraparound_runs"`
}

func (a *API) registerAutovacuumRoutes(r chi.Router) {
	r.Get("/api/v1/autovacuum/runs", a.handleAutovacuumRuns)
	r.Get("/api/v1/autovacuum/summary", a.handleAutovacuumSummary)
}

// autovacuumWindow reads instance_id, from, to and limit, shared by both
// endpoints: to defaults to now, from to seven days before to.
func autovacuumWindow(w http.ResponseWriter, q url.Values) (instanceID uuid.UUID, from, to time.Time, limit int, ok bool) {
	instanceID, err := uuid.Parse(q.Get("instance_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", "query param instance_id must be a UUID")
		return
	}
	to = time.Now().UTC()
	if s := q.Get("to"); s != "" {
		if to, err = parseTimeParam(s); err != nil {
			writeError(w, http.StatusBadRequest, "invalid to", err.Error())
			return
		}
	}
	from = to.Add(-defaultAutovacuumRange)
	if s := q.Get("from"); s != "" {
		if from, err = parseTimeParam(s); err != nil {
			writeError(w, http.StatusBadRequest, "invalid from", err.Error())
			return
		}
	}
	if !from.Before(to) {
		writeError(w, http.StatusBadRequest, "invalid range", "from must be before to")
		return
	}
	limit = defaultAutovacuumLimit
	if s := q.Get("limit"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil || v < 1 || v > maxAutovacuumLimit {
			writeError(w, http.StatusBadRequest, "invalid limit", fmt.Sprintf("limit must be between 1 and %d, got %q", maxAutovacuumLimit, s))
			return
		}
		limit = v
	}
	return instanceID, from, to, limit, true
}

// handleAutovacuumRuns lists the runs of an instance, optionally of one
// database, schema, relation or kind, newest (or longest, or most tuples
// removed) first (plan 007 8.3).
func (a *API) handleAutovacuumRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	instanceID, from, to, limit, ok := autovacuumWindow(w, q)
	if !ok {
		return
	}
	kind := q.Get("kind")
	if kind != "" && kind != "vacuum" && kind != "analyze" {
		writeError(w, http.StatusBadRequest, "invalid kind", "kind must be vacuum or analyze")
		return
	}
	orderBy := q.Get("order_by")
	if orderBy == "" {
		orderBy = "ts"
	}
	column, known := autovacuumOrders[orderBy]
	if !known {
		writeError(w, http.StatusBadRequest, "invalid order_by", "order_by must be ts, elapsed_seconds or tuples_removed")
		return
	}
	out := struct {
		Runs []autovacuumRunJSON `json:"runs"`
	}{Runs: []autovacuumRunJSON{}}
	if a.pool == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	var sb strings.Builder
	sb.WriteString(`SELECT ts, datname, schemaname, relname, kind, aggressive, wraparound,
  index_scans, pages_removed, pages_remain, pages_scanned, tuples_removed, tuples_remain, tuples_dead_not_removable,
  buffer_hits, buffer_reads, buffer_dirtied, wal_records, wal_fpi, wal_bytes, cpu_user_seconds, cpu_system_seconds, elapsed_seconds
FROM autovacuum_runs WHERE tenant_id='default' AND instance_id=$1 AND ts >= $2 AND ts < $3`)
	args := []any{instanceID, from, to}
	for _, f := range []struct{ param, column string }{{"database", "datname"}, {"schema", "schemaname"}, {"relation", "relname"}, {"kind", "kind"}} {
		if s := q.Get(f.param); s != "" {
			args = append(args, s)
			fmt.Fprintf(&sb, " AND %s=$%d", f.column, len(args))
		}
	}
	args = append(args, limit)
	fmt.Fprintf(&sb, " ORDER BY %s DESC NULLS LAST, ts DESC LIMIT $%d", column, len(args))
	rows, err := a.pool.Query(r.Context(), sb.String(), args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "autovacuum runs query failed", err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var x autovacuumRunJSON
		if err := rows.Scan(&x.TS, &x.Datname, &x.Schemaname, &x.Relname, &x.Kind, &x.Aggressive, &x.Wraparound,
			&x.IndexScans, &x.PagesRemoved, &x.PagesRemain, &x.PagesScanned, &x.TuplesRemoved, &x.TuplesRemain, &x.TuplesDeadNotRemovable,
			&x.BufferHits, &x.BufferReads, &x.BufferDirtied, &x.WALRecords, &x.WALFPI, &x.WALBytes,
			&x.CPUUserSeconds, &x.CPUSystemSeconds, &x.ElapsedSeconds); err != nil {
			writeError(w, http.StatusInternalServerError, "autovacuum runs decode failed", err.Error())
			return
		}
		x.TS = x.TS.UTC()
		out.Runs = append(out.Runs, x)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "autovacuum runs read failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAutovacuumSummary sums the runs of each table of an instance over the
// window, the tables with the most autovacuum time first. A run without an
// elapsed time counts as zero seconds, one without tuples as zero tuples.
func (a *API) handleAutovacuumSummary(w http.ResponseWriter, r *http.Request) {
	instanceID, from, to, limit, ok := autovacuumWindow(w, r.URL.Query())
	if !ok {
		return
	}
	out := struct {
		Tables []autovacuumSummaryJSON `json:"tables"`
	}{Tables: []autovacuumSummaryJSON{}}
	if a.pool == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	rows, err := a.pool.Query(r.Context(), `SELECT datname, schemaname, relname, count(*),
  coalesce(sum(elapsed_seconds), 0), coalesce(max(elapsed_seconds), 0), coalesce(sum(tuples_removed), 0)::bigint,
  max(ts), count(*) FILTER (WHERE wraparound)
FROM autovacuum_runs WHERE tenant_id='default' AND instance_id=$1 AND ts >= $2 AND ts < $3
GROUP BY datname, schemaname, relname
ORDER BY 5 DESC, 4 DESC, datname, schemaname, relname LIMIT $4`, instanceID, from, to, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "autovacuum summary query failed", err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var x autovacuumSummaryJSON
		if err := rows.Scan(&x.Datname, &x.Schemaname, &x.Relname, &x.Runs, &x.TotalElapsedSeconds, &x.MaxElapsedSeconds,
			&x.TuplesRemoved, &x.LastTS, &x.WraparoundRuns); err != nil {
			writeError(w, http.StatusInternalServerError, "autovacuum summary decode failed", err.Error())
			return
		}
		x.LastTS = x.LastTS.UTC()
		out.Tables = append(out.Tables, x)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "autovacuum summary read failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}
