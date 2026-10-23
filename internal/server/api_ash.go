package server

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AshAPI provides ASH query endpoints.
type AshAPI struct {
	pool dbPool
}

// NewAshAPI creates an AshAPI backed by the given pool.
// Pool may be nil in unit tests that exercise only validation logic.
func NewAshAPI(pool *pgxpool.Pool) *AshAPI {
	return &AshAPI{pool: asDBPool(pool)}
}

// RegisterRoutes mounts all ASH API handlers onto r.
func (a *AshAPI) RegisterRoutes(r chi.Router) {
	r.Get("/api/v1/ash", a.handleASH)
	r.Get("/api/v1/ash/top", a.handleASHTop)
}

// ASH response types

type ashBucket struct {
	TS                time.Time `json:"ts"`
	Samples           int       `json:"samples"`
	Ticks             int       `json:"ticks"`
	AvgActiveSessions *float64  `json:"avg_active_sessions"` // null if ticks==0
	Datname           string    `json:"datname,omitempty"`
	WaitEventType     string    `json:"wait_event_type,omitempty"`
	WaitEvent         string    `json:"wait_event,omitempty"`
	State             string    `json:"state,omitempty"`
	// PostgreSQL query IDs are int64 values. Keep them as decimal strings in
	// JSON so browsers cannot round them past Number.MAX_SAFE_INTEGER.
	QueryID *string `json:"queryid,omitempty"`
}

type ashResponse struct {
	Buckets           []ashBucket `json:"buckets"`
	ResolutionSeconds int         `json:"resolution_seconds"`
	Statistical       bool        `json:"statistical"`
	Warning           *string     `json:"warning,omitempty"`
	// Enabled is only set (to false) when the instance's own agent reports
	// ASH switched off — SYS-ASH-002: "no data" and "feature switched off"
	// must not render identically. Omitted (nil) whenever ASH is enabled or
	// the instance has never reported, which is indistinguishable from an
	// idle database and therefore correctly not flagged either way.
	Enabled *bool `json:"enabled,omitempty"`
}

type ashTopEntry struct {
	QueryID   string   `json:"queryid"`
	QueryText string   `json:"query_text"`
	Samples   int      `json:"samples"`
	Ticks     int      `json:"ticks"`
	AvgActive *float64 `json:"avg_active_sessions"`
}

type ashTopResponse struct {
	Entries           []ashTopEntry `json:"entries"`
	ResolutionSeconds int           `json:"resolution_seconds"`
	Statistical       bool          `json:"statistical"`
	Warning           *string       `json:"warning,omitempty"`
}

// ----- /api/v1/ash -----

func (a *AshAPI) handleASH(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	instanceIDStr := q.Get("instance_id")
	fromStr := q.Get("from")
	toStr := q.Get("to")
	groupByStr := q.Get("group_by")
	database := q.Get("database")
	limitStr := q.Get("limit")
	filters := ashFilters{waitEventType: q.Get("wait_event_type"), waitEvent: q.Get("wait_event")}

	if instanceIDStr == "" {
		writeError(w, http.StatusBadRequest, "missing instance_id", "query param instance_id is required")
		return
	}

	iid, err := uuid.Parse(instanceIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", err.Error())
		return
	}

	limit, err := parseLimitParam(limitStr, 1000, 10000)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid limit", err.Error())
		return
	}

	var fromPtr, toPtr *time.Time
	if fromStr != "" {
		t, perr := parseTimeParam(fromStr)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "invalid from", perr.Error())
			return
		}
		fromPtr = &t
	}
	if toStr != "" {
		t, perr := parseTimeParam(toStr)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "invalid to", perr.Error())
			return
		}
		toPtr = &t
	}
	if fromPtr != nil && toPtr != nil && !fromPtr.Before(*toPtr) {
		writeError(w, http.StatusBadRequest, "invalid range", "from must be before to")
		return
	}

	// Parse and validate group_by
	groupByCols := []string{}
	if groupByStr != "" {
		parts := strings.Split(groupByStr, ",")
		validCols := map[string]bool{
			"wait_event_type": true,
			"wait_event":      true,
			"state":           true,
			"queryid":         true,
			"datname":         true,
		}
		for _, part := range parts {
			col := strings.TrimSpace(part)
			if col == "" {
				continue
			}
			if !validCols[col] {
				writeError(w, http.StatusBadRequest, "invalid group_by", fmt.Sprintf("unknown column %q", col))
				return
			}
			groupByCols = append(groupByCols, col)
		}
	}

	if a.pool == nil {
		resp := ashResponse{
			Buckets:           []ashBucket{},
			ResolutionSeconds: 1,
			Statistical:       true,
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	ctx := r.Context()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// An instance explicitly reporting ASH disabled short-circuits before
	// ever touching metrics_ash — an empty bucket list there would otherwise
	// be indistinguishable from "no data yet" (SYS-ASH-002).
	var ashEnabled *bool
	if err := a.pool.QueryRow(ctx, `SELECT ash_enabled FROM instances WHERE instance_id=$1`, iid).Scan(&ashEnabled); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "query instance failed", err.Error())
		return
	}
	if ashEnabled != nil && !*ashEnabled {
		disabled := false
		writeJSON(w, http.StatusOK, ashResponse{
			Buckets:           []ashBucket{},
			ResolutionSeconds: 1,
			Statistical:       true,
			Enabled:           &disabled,
		})
		return
	}

	resp := ashResponse{ResolutionSeconds: 1, Statistical: true}
	var totalTicks int
	if len(groupByCols) == 0 {
		resp.Buckets, totalTicks, err = a.queryASHBuckets(ctx, iid, fromPtr, toPtr, database, filters, limit)
	} else {
		to := time.Now().UTC()
		if toPtr != nil {
			to = *toPtr
		}
		from := to.Add(-time.Hour)
		if fromPtr != nil {
			from = *fromPtr
		}
		resp.ResolutionSeconds = ashResolution(from, to)
		resp.Buckets, totalTicks, err = a.queryASHGrouped(ctx, iid, from, to, resp.ResolutionSeconds, database, filters, groupByCols, limit)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed", err.Error())
		return
	}
	if resp.Buckets == nil {
		resp.Buckets = []ashBucket{}
	}

	// Significance guard
	if totalTicks < 60 {
		msg := "fewer than 60 samples in range; results are not statistically significant"
		resp.Warning = &msg
	}

	writeJSON(w, http.StatusOK, resp)
}

// queryASHBuckets returns the stored rows unaggregated, newest first: every
// dimension, one row per window and key.
func (a *AshAPI) queryASHBuckets(ctx context.Context, instanceID uuid.UUID, from, to *time.Time, database string, filters ashFilters, limit int) ([]ashBucket, int, error) {
	where, args := ashWhere(instanceID, from, to, database, filters)
	query := fmt.Sprintf(`SELECT ts, samples, window_ticks, datname, wait_event_type, wait_event, state, queryid
		FROM metrics_ash WHERE %s ORDER BY ts DESC LIMIT %d`, where, limit)

	rows, err := a.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var buckets []ashBucket
	for rows.Next() {
		var bucket ashBucket
		var qid *int64
		if err := rows.Scan(&bucket.TS, &bucket.Samples, &bucket.Ticks, &bucket.Datname, &bucket.WaitEventType, &bucket.WaitEvent, &bucket.State, &qid); err != nil {
			return nil, 0, err
		}
		bucket.QueryID = formatQueryID(qid)
		bucket.AvgActiveSessions = avgActive(bucket.Samples, bucket.Ticks)
		buckets = append(buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	totalTicks, err := a.queryASHTicks(ctx, instanceID, from, to)
	if err != nil {
		return nil, 0, err
	}
	return buckets, totalTicks, nil
}

// queryASHTicks counts the instance's sampling ticks over the range, every
// window once. The filters narrow the samples, never the ticks, so the
// significance guard reads them here. Summing the rows instead, as the
// buckets and groups did, reported zero ticks, and a false warning, whenever
// a filter or the database matched nothing.
func (a *AshAPI) queryASHTicks(ctx context.Context, instanceID uuid.UUID, from, to *time.Time) (int, error) {
	where, args := ashWhere(instanceID, from, to, "", ashFilters{})
	var ticks int
	err := a.pool.QueryRow(ctx, fmt.Sprintf(`SELECT COALESCE(SUM(t), 0)::int FROM (SELECT ts, MAX(window_ticks) AS t FROM metrics_ash WHERE %s GROUP BY ts) w`, where), args...).Scan(&ticks)
	return ticks, err
}

// ashFilters narrows the rows to one wait event type or wait event: the
// drill-down from a type to its events and from an event to its queries.
type ashFilters struct {
	waitEventType, waitEvent string
}

func ashWhere(instanceID uuid.UUID, from, to *time.Time, database string, filters ashFilters) (string, []any) {
	frags := []string{"instance_id = $1"}
	args := []any{instanceID}
	add := func(frag string, arg any) {
		args = append(args, arg)
		frags = append(frags, fmt.Sprintf(frag, len(args)))
	}
	if from != nil {
		add("ts >= $%d", *from)
	}
	if to != nil {
		add("ts < $%d", *to)
	}
	if database != "" {
		add("datname = $%d", database)
	}
	if filters.waitEventType != "" {
		add("wait_event_type = $%d", filters.waitEventType)
	}
	if filters.waitEvent != "" {
		add("wait_event = $%d", filters.waitEvent)
	}
	return strings.Join(frags, " AND "), args
}

const (
	// ashWindowSeconds is the agent's aggregation window, the finest
	// resolution the stored rows have.
	ashWindowSeconds = 10
	// ashMaxPoints bounds the time buckets of a grouped answer, whatever the
	// range.
	ashMaxPoints = 180
	// ashMaxGroups bounds the distinct groups of a grouped answer; the
	// smaller ones are folded into one "other" group, so the total is kept.
	ashMaxGroups = 24
	ashOther     = "other"
)

// ashResolution is the time bucket, a multiple of the agent's window, that
// fits the range into ashMaxPoints buckets.
func ashResolution(from, to time.Time) int {
	span := int(math.Ceil(to.Sub(from).Seconds() / ashMaxPoints))
	if span <= ashWindowSeconds {
		return ashWindowSeconds
	}
	return (span + ashWindowSeconds - 1) / ashWindowSeconds * ashWindowSeconds
}

// queryASHGrouped aggregates the range into time buckets of resolution
// seconds, one row per bucket and group.
//
// group_by used to aggregate nothing: it selected the requested columns from
// the raw rows, newest first, cut at limit. Each window came back once per
// stored key, the chart kept whichever of them it read last, and the limit
// cut the range to its newest minutes. The requested columns were not even
// filled in: each was copied out of its scan variable before the scan ran,
// so every row read as CPU. And ticks were summed per row, so every key
// counted its window's ticks again, understating the average active sessions
// and hiding the significance warning.
//
// ticks is now the instance's sampling ticks in the bucket: every window
// counted once, from every row of the instance, whatever the filters, so the
// average active sessions of a group is its share of the instance's time.
// Groups beyond the largest ones are folded into "other" so the stacked
// total is conserved; filter values are echoed on every row, so a drill-down
// can tell which type or event it is looking at.
func (a *AshAPI) queryASHGrouped(ctx context.Context, instanceID uuid.UUID, from, to time.Time, resolution int, database string, filters ashFilters, groupByCols []string, limit int) ([]ashBucket, int, error) {
	where, args := ashWhere(instanceID, &from, &to, database, filters)
	res := time.Duration(resolution) * time.Second
	args = append(args, res, from, to)
	resArg, fromArg, toArg := len(args)-2, len(args)-1, len(args)
	cols := strings.Join(groupByCols, ", ")
	gcols := "g." + strings.Join(groupByCols, ", g.")
	query := fmt.Sprintf(`
WITH ticks AS (
  SELECT bucket, SUM(t)::int AS ticks
    FROM (SELECT time_bucket($%[1]d::interval, ts) AS bucket, ts, MAX(window_ticks) AS t
            FROM metrics_ash
           WHERE instance_id = $1 AND ts >= $%[2]d AND ts < $%[3]d
           GROUP BY 1, 2) w
   GROUP BY bucket
)
SELECT g.bucket, g.samples, t.ticks, %[4]s
  FROM (SELECT time_bucket($%[1]d::interval, ts) AS bucket, %[5]s, SUM(samples)::int AS samples
          FROM metrics_ash
         WHERE %[6]s
         GROUP BY 1, %[5]s) g
  JOIN ticks t ON t.bucket = g.bucket
 ORDER BY g.bucket`, resArg, fromArg, toArg, gcols, cols, where)

	rows, err := a.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var raw []ashBucket
	bucketTicks := make(map[time.Time]int)
	for rows.Next() {
		var b ashBucket
		dests := []any{&b.TS, &b.Samples, &b.Ticks}
		var qid *int64
		for _, col := range groupByCols {
			switch col {
			case "datname":
				dests = append(dests, &b.Datname)
			case "wait_event_type":
				dests = append(dests, &b.WaitEventType)
			case "wait_event":
				dests = append(dests, &b.WaitEvent)
			case "state":
				dests = append(dests, &b.State)
			case "queryid":
				dests = append(dests, &qid)
			}
		}
		if err := rows.Scan(dests...); err != nil {
			return nil, 0, err
		}
		b.QueryID = formatQueryID(qid)
		bucketTicks[b.TS] = b.Ticks
		raw = append(raw, b)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	buckets := foldASHGroups(raw, groupByCols, len(bucketTicks), limit)
	for i := range buckets {
		if filters.waitEventType != "" {
			buckets[i].WaitEventType = filters.waitEventType
		}
		if filters.waitEvent != "" {
			buckets[i].WaitEvent = filters.waitEvent
		}
		buckets[i].AvgActiveSessions = avgActive(buckets[i].Samples, buckets[i].Ticks)
	}
	totalTicks, err := a.queryASHTicks(ctx, instanceID, &from, &to)
	if err != nil {
		return nil, 0, err
	}
	return buckets, totalTicks, nil
}

// foldASHGroups keeps the largest groups (by samples over the range) and
// folds the others into one "other" group per bucket. The number kept is
// ashMaxGroups, fewer when limit rows could not hold them for every bucket.
// A group that already reads as "other" (a NULL queryid, a value named
// other) is folded too, so no bucket carries "other" twice.
func foldASHGroups(rows []ashBucket, groupByCols []string, nBuckets, limit int) []ashBucket {
	keyOf := func(b ashBucket) string {
		qid := ""
		if b.QueryID != nil {
			qid = *b.QueryID
		}
		return strings.Join([]string{b.Datname, b.WaitEventType, b.WaitEvent, b.State, qid}, "\x00")
	}
	var template ashBucket
	for _, col := range groupByCols {
		switch col {
		case "datname":
			template.Datname = ashOther
		case "wait_event_type":
			template.WaitEventType = ashOther
		case "wait_event":
			template.WaitEvent = ashOther
		case "state":
			template.State = ashOther
		}
	}
	otherKey := keyOf(template)

	totals := make(map[string]int)
	for _, b := range rows {
		totals[keyOf(b)] += b.Samples
	}
	keep := ashMaxGroups
	if nBuckets > 0 {
		if fit := limit/nBuckets - 1; fit < keep {
			keep = max(fit, 1)
		}
	}
	if len(totals) <= keep+1 {
		return rows
	}
	keys := make([]string, 0, len(totals))
	for k := range totals {
		if k != otherKey {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if totals[keys[i]] != totals[keys[j]] {
			return totals[keys[i]] > totals[keys[j]]
		}
		return keys[i] < keys[j]
	})
	kept := make(map[string]struct{}, keep)
	for _, k := range keys[:min(keep, len(keys))] {
		kept[k] = struct{}{}
	}
	var out []ashBucket
	other := make(map[time.Time]int) // bucket -> index of its "other" row in out
	for _, b := range rows {
		if _, ok := kept[keyOf(b)]; ok {
			out = append(out, b)
			continue
		}
		if i, ok := other[b.TS]; ok {
			out[i].Samples += b.Samples
			continue
		}
		folded := template
		folded.TS, folded.Samples, folded.Ticks = b.TS, b.Samples, b.Ticks
		other[b.TS] = len(out)
		out = append(out, folded)
	}
	return out
}

func avgActive(samples, ticks int) *float64 {
	if ticks <= 0 {
		return nil
	}
	avg := float64(samples) / float64(ticks)
	return &avg
}

// ----- /api/v1/ash/top -----

func (a *AshAPI) handleASHTop(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	instanceIDStr := q.Get("instance_id")
	fromStr := q.Get("from")
	toStr := q.Get("to")
	database := q.Get("database")
	limitStr := q.Get("limit")
	// The drill-down filters, so the queries listed under a wait event are
	// the ones seen in it: the list used to rank every query of the range
	// whatever wait event the chart above had narrowed to.
	filters := ashFilters{waitEventType: q.Get("wait_event_type"), waitEvent: q.Get("wait_event")}

	if instanceIDStr == "" {
		writeError(w, http.StatusBadRequest, "missing instance_id", "query param instance_id is required")
		return
	}

	iid, err := uuid.Parse(instanceIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", err.Error())
		return
	}

	limit, err := parseLimitParam(limitStr, 20, 1000)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid limit", err.Error())
		return
	}

	var fromPtr, toPtr *time.Time
	if fromStr != "" {
		t, perr := parseTimeParam(fromStr)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "invalid from", perr.Error())
			return
		}
		fromPtr = &t
	}
	if toStr != "" {
		t, perr := parseTimeParam(toStr)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "invalid to", perr.Error())
			return
		}
		toPtr = &t
	}
	if fromPtr != nil && toPtr != nil && !fromPtr.Before(*toPtr) {
		writeError(w, http.StatusBadRequest, "invalid range", "from must be before to")
		return
	}

	if a.pool == nil {
		resp := ashTopResponse{
			Entries:           []ashTopEntry{},
			ResolutionSeconds: 1,
			Statistical:       true,
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	ctx := r.Context()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	entries, totalTicks, err := a.queryASHTop(ctx, iid, fromPtr, toPtr, database, filters, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed", err.Error())
		return
	}

	resp := ashTopResponse{
		Entries:           entries,
		ResolutionSeconds: 1,
		Statistical:       true,
	}

	if totalTicks < 60 {
		msg := "fewer than 60 samples in range; results are not statistically significant"
		resp.Warning = &msg
	}

	writeJSON(w, http.StatusOK, resp)
}

// ashTopTextSQL is the newest text of a top entry's queryid in its cluster. The
// primary key leads with datname, which the join never binds, so each entry
// scanned the cluster's whole text table. query_texts_cluster_queryid_idx
// serves the join on the cluster and the queryid.
const ashTopTextSQL = `(SELECT q.query_text FROM query_texts q WHERE q.tenant_id = 'default' AND q.cluster_id = top.cluster_id AND q.queryid = top.queryid ORDER BY q.last_seen DESC LIMIT 1)`

// queryASHTop ranks the queries by samples over the range.
//
// The text used to be joined from query_texts row by row: that table keeps
// one row per PostgreSQL major, so a queryid seen before and after an upgrade
// counted its samples twice. ticks was summed over each query's rows, once
// per wait event and state the query was seen in, which understated its
// average active sessions; it is the instance's sampling ticks over the
// range, every window counted once, the same denominator for every query.
// The filters narrow the samples, not the ticks, as they do for group_by.
func (a *AshAPI) queryASHTop(ctx context.Context, instanceID uuid.UUID, from, to *time.Time, database string, filters ashFilters, limit int) ([]ashTopEntry, int, error) {
	where, args := ashWhere(instanceID, from, to, database, filters)
	tickWhere, _ := ashWhere(instanceID, from, to, "", ashFilters{})
	args = append(args, limit)
	query := fmt.Sprintf(`
WITH ticks AS (
  SELECT COALESCE(SUM(t), 0)::int AS ticks
    FROM (SELECT ts, MAX(window_ticks) AS t FROM metrics_ash WHERE %s GROUP BY ts) w
), top AS (
  SELECT queryid, MIN(cluster_id) AS cluster_id, SUM(samples)::int AS samples
    FROM metrics_ash
   WHERE %s AND queryid IS NOT NULL
   GROUP BY queryid
   ORDER BY SUM(samples) DESC, queryid
   LIMIT $%d
)
SELECT top.queryid,
       COALESCE(%s, ''),
       top.samples, ticks.ticks
  FROM top CROSS JOIN ticks
 ORDER BY top.samples DESC, top.queryid`, tickWhere, where, len(args), ashTopTextSQL)

	rows, err := a.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	entries := []ashTopEntry{}
	for rows.Next() {
		var qid int64
		var entry ashTopEntry
		if err := rows.Scan(&qid, &entry.QueryText, &entry.Samples, &entry.Ticks); err != nil {
			return nil, 0, err
		}
		entry.QueryID = strconv.FormatInt(qid, 10)
		entry.AvgActive = avgActive(entry.Samples, entry.Ticks)
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	totalTicks, err := a.queryASHTicks(ctx, instanceID, from, to)
	if err != nil {
		return nil, 0, err
	}
	return entries, totalTicks, nil
}

func formatQueryID(qid *int64) *string {
	if qid == nil {
		return nil
	}
	value := strconv.FormatInt(*qid, 10)
	return &value
}
