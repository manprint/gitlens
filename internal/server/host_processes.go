package server

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/manprint/pglens/internal/store"
	"github.com/manprint/pglens/internal/wire"
)

const (
	defaultProcessLimit = 40
	maxProcessLimit     = 200
	// processSnapshotMaxAge is how far before `at` the newest sample may be.
	processSnapshotMaxAge = 5 * time.Minute
	defaultProcessRange   = time.Hour
)

// processOrders maps order_by of the snapshot to its column, descending.
var processOrders = map[string]string{
	"cpu": "cpu_ratio", "rss": "rss_bytes", "read": "read_bytes_per_s", "write": "write_bytes_per_s",
}

// processGroups maps group_by of the top endpoint to its column.
var processGroups = map[string]string{
	"backend_type": "backend_type", "datname": "datname", "usename": "usename", "application_name": "application_name",
}

type processValue struct {
	CPURatio       *float64 `json:"cpu_ratio"`
	RSSBytes       *float64 `json:"rss_bytes"`
	ReadBytesPerS  *float64 `json:"read_bytes_per_s"`
	WriteBytesPerS *float64 `json:"write_bytes_per_s"`
}

func finiteNonNegative(v *float64) bool {
	return v != nil && *v >= 0 && !math.IsNaN(*v) && !math.IsInf(*v, 0)
}

// processRow turns one host_process fact into a row at the result's time: a
// positive pid, a non-negative CPU ratio and RSS, else the fact is invalid.
func processRow(f wire.Fact, ts time.Time, tenantID string, clusterID int64, instanceID uuid.UUID) (store.ProcessRow, bool) {
	pid, err := strconv.ParseInt(f.Labels["pid"], 10, 32)
	if err != nil || pid <= 0 || strconv.FormatInt(pid, 10) != f.Key {
		return store.ProcessRow{}, false
	}
	var v processValue
	if err := json.Unmarshal(f.ValueJSON, &v); err != nil {
		return store.ProcessRow{}, false
	}
	if !finiteNonNegative(v.CPURatio) || !finiteNonNegative(v.RSSBytes) || *v.RSSBytes >= math.MaxInt64 {
		return store.ProcessRow{}, false
	}
	for _, x := range []*float64{v.ReadBytesPerS, v.WriteBytesPerS} {
		if x != nil && !finiteNonNegative(x) {
			return store.ProcessRow{}, false
		}
	}
	row := store.ProcessRow{
		TS: ts.UTC(), TenantID: tenantID, InstanceID: instanceID, ClusterID: clusterID, PID: int32(pid),
		BackendType: f.Labels["backend_type"], Datname: f.Labels["datname"], Usename: f.Labels["usename"],
		ApplicationName: f.Labels["application_name"], State: f.Labels["state"],
		CPURatio: *v.CPURatio, RSSBytes: int64(*v.RSSBytes),
		ReadBytesPerS: v.ReadBytesPerS, WriteBytesPerS: v.WriteBytesPerS,
	}
	if s := f.Labels["queryid"]; s != "" {
		q, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return store.ProcessRow{}, false
		}
		row.QueryID = &q
	}
	return row, true
}

// processJSON is one process of GET /api/v1/instances/{id}/host/processes.
type processJSON struct {
	PID             int32    `json:"pid"`
	BackendType     string   `json:"backend_type"`
	Datname         string   `json:"datname"`
	Usename         string   `json:"usename"`
	ApplicationName string   `json:"application_name"`
	State           string   `json:"state"`
	QueryID         *string  `json:"queryid"`
	CPURatio        float64  `json:"cpu_ratio"`
	RSSBytes        int64    `json:"rss_bytes"`
	ReadBytesPerS   *float64 `json:"read_bytes_per_s"`
	WriteBytesPerS  *float64 `json:"write_bytes_per_s"`
}

type processSnapshotJSON struct {
	TS        *time.Time    `json:"ts"`
	Processes []processJSON `json:"processes"`
}

// processGroupJSON is one group of GET .../host/processes/top.
type processGroupJSON struct {
	Group             string   `json:"group"`
	AvgCPURatio       float64  `json:"avg_cpu_ratio"`
	MaxRSSBytes       int64    `json:"max_rss_bytes"`
	AvgReadBytesPerS  *float64 `json:"avg_read_bytes_per_s"`
	AvgWriteBytesPerS *float64 `json:"avg_write_bytes_per_s"`
}

type processTopJSON struct {
	From        time.Time          `json:"from"`
	To          time.Time          `json:"to"`
	GroupBy     string             `json:"group_by"`
	SampleTimes int64              `json:"sample_times"`
	Groups      []processGroupJSON `json:"groups"`
}

// processGroupSums is one group's sums over a range, before averaging.
type processGroupSums struct {
	Group                 string
	CPU                   float64
	MaxRSS                int64
	Read, Write           float64
	ReadKnown, WriteKnown int64
}

// foldProcessGroups averages the group sums over the sample times in range:
// a group absent at a sample time counts as zero there. An I/O average is
// null when no process of the group had readable counters. Groups are
// ordered by average CPU, then name, and cut to limit.
func foldProcessGroups(sums []processGroupSums, sampleTimes int64, limit int) []processGroupJSON {
	out := make([]processGroupJSON, 0, len(sums))
	if sampleTimes <= 0 {
		return out
	}
	n := float64(sampleTimes)
	for _, s := range sums {
		g := processGroupJSON{Group: s.Group, AvgCPURatio: s.CPU / n, MaxRSSBytes: s.MaxRSS}
		if s.ReadKnown > 0 {
			v := s.Read / n
			g.AvgReadBytesPerS = &v
		}
		if s.WriteKnown > 0 {
			v := s.Write / n
			g.AvgWriteBytesPerS = &v
		}
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].AvgCPURatio != out[j].AvgCPURatio {
			return out[i].AvgCPURatio > out[j].AvgCPURatio
		}
		return out[i].Group < out[j].Group
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func processLimit(w http.ResponseWriter, s string) (int, bool) {
	if s == "" {
		return defaultProcessLimit, true
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > maxProcessLimit {
		writeError(w, http.StatusBadRequest, "invalid limit", "limit must be an integer from 1 to 200")
		return 0, false
	}
	return n, true
}

func (a *API) handleProcesses(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", err.Error())
		return
	}
	q := r.URL.Query()
	at := time.Now().UTC()
	if s := q.Get("at"); s != "" {
		if at, err = parseTimeParam(s); err != nil {
			writeError(w, http.StatusBadRequest, "invalid at", err.Error())
			return
		}
	}
	order := q.Get("order_by")
	if order == "" {
		order = "cpu"
	}
	column, ok := processOrders[order]
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid order_by", "order_by must be cpu, rss, read or write")
		return
	}
	limit, ok := processLimit(w, q.Get("limit"))
	if !ok {
		return
	}
	resp := processSnapshotJSON{Processes: []processJSON{}}
	if a.pool == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	ctx := r.Context()
	var ts time.Time
	err = a.pool.QueryRow(ctx, `SELECT ts FROM metrics_processes
		WHERE tenant_id = 'default' AND instance_id = $1 AND ts <= $2 AND ts >= $3
		ORDER BY ts DESC LIMIT 1`, id, at, at.Add(-processSnapshotMaxAge)).Scan(&ts)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed", err.Error())
		return
	}
	// column comes from the processOrders whitelist.
	rows, err := a.pool.Query(ctx, `SELECT pid, backend_type, datname, usename, application_name, state, queryid,
		cpu_ratio, rss_bytes, read_bytes_per_s, write_bytes_per_s
		FROM metrics_processes WHERE tenant_id = 'default' AND instance_id = $1 AND ts = $2
		ORDER BY `+column+` DESC NULLS LAST, pid LIMIT $3`, id, ts, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed", err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var (
			p   processJSON
			qid *int64
		)
		if err := rows.Scan(&p.PID, &p.BackendType, &p.Datname, &p.Usename, &p.ApplicationName, &p.State, &qid,
			&p.CPURatio, &p.RSSBytes, &p.ReadBytesPerS, &p.WriteBytesPerS); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed", err.Error())
			return
		}
		if qid != nil {
			s := strconv.FormatInt(*qid, 10)
			p.QueryID = &s
		}
		resp.Processes = append(resp.Processes, p)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "rows error", err.Error())
		return
	}
	ts = ts.UTC()
	resp.TS = &ts
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) handleProcessesTop(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", err.Error())
		return
	}
	q := r.URL.Query()
	to := time.Now().UTC()
	if s := q.Get("to"); s != "" {
		if to, err = parseTimeParam(s); err != nil {
			writeError(w, http.StatusBadRequest, "invalid to", err.Error())
			return
		}
	}
	from := to.Add(-defaultProcessRange)
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
	groupBy := q.Get("group_by")
	if groupBy == "" {
		groupBy = "backend_type"
	}
	column, ok := processGroups[groupBy]
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid group_by", "group_by must be backend_type, datname, usename or application_name")
		return
	}
	limit, ok := processLimit(w, q.Get("limit"))
	if !ok {
		return
	}
	resp := processTopJSON{From: from.UTC(), To: to.UTC(), GroupBy: groupBy, Groups: []processGroupJSON{}}
	if a.pool == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	ctx := r.Context()
	const where = `FROM metrics_processes WHERE tenant_id = 'default' AND instance_id = $1 AND ts >= $2 AND ts < $3`
	if err := a.pool.QueryRow(ctx, `SELECT count(DISTINCT ts) `+where, id, from, to).Scan(&resp.SampleTimes); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed", err.Error())
		return
	}
	// column comes from the processGroups whitelist.
	rows, err := a.pool.Query(ctx, `SELECT `+column+`, sum(cpu_ratio), max(rss_bytes),
		COALESCE(sum(read_bytes_per_s), 0), COALESCE(sum(write_bytes_per_s), 0),
		count(read_bytes_per_s), count(write_bytes_per_s) `+where+` GROUP BY 1`, id, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed", err.Error())
		return
	}
	defer rows.Close()
	var sums []processGroupSums
	for rows.Next() {
		var s processGroupSums
		if err := rows.Scan(&s.Group, &s.CPU, &s.MaxRSS, &s.Read, &s.Write, &s.ReadKnown, &s.WriteKnown); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed", err.Error())
			return
		}
		sums = append(sums, s)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "rows error", err.Error())
		return
	}
	resp.Groups = foldProcessGroups(sums, resp.SampleTimes, limit)
	writeJSON(w, http.StatusOK, resp)
}
