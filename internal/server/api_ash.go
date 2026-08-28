package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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
	QueryID           *int64    `json:"queryid,omitempty"`
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
	QueryID   int64    `json:"queryid"`
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

	// Query database
	buckets, totalTicks, err := a.queryASHBuckets(ctx, iid, fromPtr, toPtr, database, groupByCols, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed", err.Error())
		return
	}

	resp := ashResponse{
		Buckets:           buckets,
		ResolutionSeconds: 1,
		Statistical:       true,
	}

	// Significance guard
	if totalTicks < 60 {
		msg := "fewer than 60 samples in range; results are not statistically significant"
		resp.Warning = &msg
	}

	writeJSON(w, http.StatusOK, resp)
}

func (a *AshAPI) queryASHBuckets(ctx context.Context, instanceID uuid.UUID, from, to *time.Time, database string, groupByCols []string, limit int) ([]ashBucket, int, error) {
	// Build SELECT clause
	selectCols := []string{"ts", "samples", "window_ticks"}
	if len(groupByCols) == 0 {
		selectCols = append(selectCols, "datname", "wait_event_type", "wait_event", "state", "queryid")
	} else {
		selectCols = append(selectCols, groupByCols...)
	}
	selectStr := strings.Join(selectCols, ", ")

	// Build WHERE clause
	var whereFrags []string
	var args []any
	argIdx := 1

	whereFrags = append(whereFrags, fmt.Sprintf("instance_id = $%d", argIdx))
	args = append(args, instanceID)
	argIdx++

	if from != nil {
		whereFrags = append(whereFrags, fmt.Sprintf("ts >= $%d", argIdx))
		args = append(args, *from)
		argIdx++
	}
	if to != nil {
		whereFrags = append(whereFrags, fmt.Sprintf("ts < $%d", argIdx))
		args = append(args, *to)
		argIdx++
	}
	if database != "" {
		whereFrags = append(whereFrags, fmt.Sprintf("datname = $%d", argIdx))
		args = append(args, database)
	}

	whereStr := strings.Join(whereFrags, " AND ")
	query := fmt.Sprintf(`SELECT %s FROM metrics_ash WHERE %s ORDER BY ts DESC LIMIT %d`, selectStr, whereStr, limit)

	rows, err := a.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var buckets []ashBucket
	totalTicks := 0
	for rows.Next() {
		bucket := ashBucket{}
		var ticks int

		// Scan common columns
		scanDests := []any{&bucket.TS, &bucket.Samples, &ticks}

		if len(groupByCols) == 0 {
			var datname, wet, we, state string
			var qid *int64
			scanDests = append(scanDests, &datname, &wet, &we, &state, &qid)
			if err := rows.Scan(scanDests...); err != nil {
				return nil, 0, err
			}
			bucket.Datname = datname
			bucket.WaitEventType = wet
			bucket.WaitEvent = we
			bucket.State = state
			bucket.QueryID = qid
		} else {
			// Only scan requested columns
			for _, col := range groupByCols {
				switch col {
				case "datname":
					var v string
					scanDests = append(scanDests, &v)
					bucket.Datname = v
				case "wait_event_type":
					var v string
					scanDests = append(scanDests, &v)
					bucket.WaitEventType = v
				case "wait_event":
					var v string
					scanDests = append(scanDests, &v)
					bucket.WaitEvent = v
				case "state":
					var v string
					scanDests = append(scanDests, &v)
					bucket.State = v
				case "queryid":
					var v *int64
					scanDests = append(scanDests, &v)
					bucket.QueryID = v
				}
			}
			if err := rows.Scan(scanDests...); err != nil {
				return nil, 0, err
			}
		}

		// Calculate avg_active_sessions
		bucket.Ticks = ticks
		if ticks > 0 {
			avg := float64(bucket.Samples) / float64(ticks)
			bucket.AvgActiveSessions = &avg
		}

		totalTicks += ticks
		buckets = append(buckets, bucket)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return buckets, totalTicks, nil
}

// ----- /api/v1/ash/top -----

func (a *AshAPI) handleASHTop(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	instanceIDStr := q.Get("instance_id")
	fromStr := q.Get("from")
	toStr := q.Get("to")
	database := q.Get("database")
	limitStr := q.Get("limit")

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

	entries, totalTicks, err := a.queryASHTop(ctx, iid, fromPtr, toPtr, database, limit)
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

func (a *AshAPI) queryASHTop(ctx context.Context, instanceID uuid.UUID, from, to *time.Time, database string, limit int) ([]ashTopEntry, int, error) {
	// Build WHERE clause
	var whereFrags []string
	var args []any
	argIdx := 1

	whereFrags = append(whereFrags, fmt.Sprintf("a.instance_id = $%d", argIdx))
	args = append(args, instanceID)
	argIdx++

	whereFrags = append(whereFrags, "a.queryid IS NOT NULL")

	if from != nil {
		whereFrags = append(whereFrags, fmt.Sprintf("a.ts >= $%d", argIdx))
		args = append(args, *from)
		argIdx++
	}
	if to != nil {
		whereFrags = append(whereFrags, fmt.Sprintf("a.ts < $%d", argIdx))
		args = append(args, *to)
		argIdx++
	}
	if database != "" {
		whereFrags = append(whereFrags, fmt.Sprintf("a.datname = $%d", argIdx))
		args = append(args, database)
		argIdx++
	}

	whereStr := strings.Join(whereFrags, " AND ")

	query := fmt.Sprintf(`
		SELECT a.queryid, COALESCE(q.query_text, ''),
		       SUM(a.samples)::int, SUM(a.window_ticks)::int
		FROM metrics_ash a
		LEFT JOIN query_texts q ON a.queryid = q.queryid
			AND a.datname = q.datname
			AND a.cluster_id = q.cluster_id
		WHERE %s
		GROUP BY a.queryid, q.query_text
		ORDER BY SUM(a.samples) DESC
		LIMIT $%d
	`, whereStr, argIdx)
	args = append(args, limit)

	rows, err := a.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var entries []ashTopEntry
	totalTicks := 0
	for rows.Next() {
		var qid *int64
		var text string
		var samples, ticks int

		if err := rows.Scan(&qid, &text, &samples, &ticks); err != nil {
			return nil, 0, err
		}

		entry := ashTopEntry{
			QueryText: text,
			Samples:   samples,
			Ticks:     ticks,
		}
		if qid != nil {
			entry.QueryID = *qid
		}
		if ticks > 0 {
			avg := float64(samples) / float64(ticks)
			entry.AvgActive = &avg
		}

		entries = append(entries, entry)
		totalTicks += ticks
	}

	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return entries, totalTicks, nil
}
