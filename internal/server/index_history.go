package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	// indexHistoryMinStep is the sampling interval of metrics_indexes: a bucket
	// narrower than that has no stored sample to average (I-1).
	indexHistoryMinStep = 10 * time.Minute
	// indexHistoryBuckets is how many points a default-step series has, at most.
	indexHistoryBuckets = 120
	// indexHistoryMaxBuckets bounds an explicit step.
	indexHistoryMaxBuckets = 1000
	indexHistoryMaxRange   = 30 * 24 * time.Hour
	indexHistoryDefaultRng = 24 * time.Hour
	indexHistoryBudget     = 10 * time.Second
)

// indexHistoryPoint is one bucket. A bucket without a stored sample has null
// values: nothing is interpolated.
type indexHistoryPoint struct {
	TS             time.Time `json:"ts"`
	IdxScanRate    *float64  `json:"idx_scan_rate"`
	IdxTupReadRate *float64  `json:"idx_tup_read_rate"`
	IndexBytes     *int64    `json:"index_bytes"`
}

type indexHistoryIndex struct {
	Datname      string `json:"datname"`
	Schemaname   string `json:"schemaname"`
	IndexRelname string `json:"indexrelname"`
}

type indexHistoryResponse struct {
	Index    indexHistoryIndex   `json:"index"`
	StepSecs int                 `json:"step_seconds"`
	Points   []indexHistoryPoint `json:"points"`
}

// indexHistoryQuery names one index and the range and bucket width to read.
type indexHistoryQuery struct {
	Instance uuid.UUID
	Index    indexHistoryIndex
	From, To time.Time
	Step     time.Duration
}

// indexHistoryStore reads the series; instanceFound and indexFound are false
// for an unknown instance or index. Unit tests stub it.
type indexHistoryStore interface {
	IndexHistory(ctx context.Context, q indexHistoryQuery) (points []indexHistoryPoint, instanceFound, indexFound bool, err error)
}

func (a *API) indexHistoryStore() indexHistoryStore {
	if a.indexHistory != nil {
		return a.indexHistory
	}
	if a.pool == nil {
		return nil
	}
	return pgIndexHistoryStore{pool: a.pool}
}

func (a *API) registerIndexHistoryRoutes(r chi.Router) {
	r.Get("/api/v1/instances/{id}/indexes/history", a.handleIndexHistory)
}

// handleIndexHistory answers getIndexHistory: the scan rate, the tuple read
// rate and the size of one index over time.
func (a *API) handleIndexHistory(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", err.Error())
		return
	}
	q := r.URL.Query()
	index := indexHistoryIndex{Datname: q.Get("database"), Schemaname: q.Get("schema"), IndexRelname: q.Get("index")}
	for _, p := range []struct{ name, value string }{{"database", index.Datname}, {"schema", index.Schemaname}, {"index", index.IndexRelname}} {
		if p.value == "" {
			writeError(w, http.StatusBadRequest, "missing "+p.name, p.name+" is required")
			return
		}
		if len(p.value) > 255 {
			writeError(w, http.StatusBadRequest, "invalid "+p.name, p.name+" is longer than 255 characters")
			return
		}
	}
	to := time.Now().UTC()
	if raw := q.Get("to"); raw != "" {
		t, perr := parseTimeParam(raw)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "invalid to", perr.Error())
			return
		}
		to = t.UTC()
	}
	from := to.Add(-indexHistoryDefaultRng)
	if raw := q.Get("from"); raw != "" {
		t, perr := parseTimeParam(raw)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "invalid from", perr.Error())
			return
		}
		from = t.UTC()
	}
	if !from.Before(to) {
		writeError(w, http.StatusBadRequest, "invalid range", "from must be before to")
		return
	}
	span := to.Sub(from)
	if span > indexHistoryMaxRange {
		writeError(w, http.StatusBadRequest, "invalid range", "the range is at most 30 days")
		return
	}
	step := span / indexHistoryBuckets
	if raw := q.Get("step"); raw != "" {
		d, perr := parseStep(raw)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "invalid step", perr.Error())
			return
		}
		if d < indexHistoryMinStep {
			writeError(w, http.StatusBadRequest, "invalid step", "the step is at least 10m: the index statistics are sampled every 5 minutes")
			return
		}
		if span/d > indexHistoryMaxBuckets {
			writeError(w, http.StatusBadRequest, "invalid step", fmt.Sprintf("the step gives more than %d points", indexHistoryMaxBuckets))
			return
		}
		step = d
	}
	if step < indexHistoryMinStep {
		step = indexHistoryMinStep
	}
	step = step.Truncate(time.Second)

	st := a.indexHistoryStore()
	if st == nil {
		writeError(w, http.StatusNotFound, "instance not found", fmt.Sprintf("instance %s not found", idStr))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), indexHistoryBudget)
	defer cancel()
	points, instanceFound, indexFound, err := st.IndexHistory(ctx, indexHistoryQuery{Instance: id, Index: index, From: from, To: to, Step: step})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			writeError(w, http.StatusServiceUnavailable, "history timed out", "the history did not load in "+indexHistoryBudget.String())
			return
		}
		writeError(w, http.StatusInternalServerError, "index history query failed", err.Error())
		return
	}
	if !instanceFound {
		writeError(w, http.StatusNotFound, "instance not found", fmt.Sprintf("instance %s not found", idStr))
		return
	}
	if !indexFound {
		writeError(w, http.StatusNotFound, "index not found", fmt.Sprintf("index %s.%s of database %s is not known for instance %s", index.Schemaname, index.IndexRelname, index.Datname, idStr))
		return
	}
	if points == nil {
		points = []indexHistoryPoint{}
	}
	writeJSON(w, http.StatusOK, indexHistoryResponse{Index: index, StepSecs: int(step / time.Second), Points: points})
}

type pgIndexHistoryStore struct{ pool dbPool }

const (
	// The index is known when the agent reported its definition or any
	// statistics row of it lies in the range.
	indexHistoryKnownSQL = `
SELECT EXISTS (SELECT 1 FROM object_facts
                WHERE tenant_id = 'default' AND instance_id = $1 AND kind = 'index_def'
                  AND datname = $2 AND key = $3 || '.' || $4)
    OR EXISTS (SELECT 1 FROM metrics_indexes
                WHERE tenant_id = 'default' AND instance_id = $1 AND datname = $2
                  AND schemaname = $3 AND indexrelname = $4 AND ts >= $5 AND ts < $6)`

	// One row per bucket of the range; a bucket without a stored sample has NULL
	// columns. idx_scan and idx_tup_read are stored as per-second rates, so a
	// bucket's value is their mean (I-1); the size is the last value.
	indexHistorySQL = `
WITH s AS (
  SELECT time_bucket(make_interval(secs => $7), ts) AS bucket,
         avg(idx_scan) AS scan, avg(idx_tup_read) AS tup, last(index_bytes, ts) AS bytes
    FROM metrics_indexes
   WHERE tenant_id = 'default' AND instance_id = $1 AND datname = $2
     AND schemaname = $3 AND indexrelname = $4 AND ts >= $5 AND ts < $6
   GROUP BY 1
)
SELECT b.bucket, s.scan, s.tup, s.bytes
  FROM generate_series(time_bucket(make_interval(secs => $7), $5::timestamptz),
                       $6::timestamptz - interval '1 microsecond',
                       make_interval(secs => $7)) AS b(bucket)
  LEFT JOIN s ON s.bucket = b.bucket
 ORDER BY b.bucket`
)

func (s pgIndexHistoryStore) IndexHistory(ctx context.Context, q indexHistoryQuery) ([]indexHistoryPoint, bool, bool, error) {
	var cluster int64
	if err := s.pool.QueryRow(ctx, indexSuggestionInstanceSQL, q.Instance).Scan(&cluster); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, false, nil
		}
		return nil, false, false, err
	}
	var known bool
	if err := s.pool.QueryRow(ctx, indexHistoryKnownSQL, q.Instance, q.Index.Datname, q.Index.Schemaname, q.Index.IndexRelname, q.From, q.To).Scan(&known); err != nil {
		return nil, true, false, err
	}
	if !known {
		return nil, true, false, nil
	}
	rows, err := s.pool.Query(ctx, indexHistorySQL, q.Instance, q.Index.Datname, q.Index.Schemaname, q.Index.IndexRelname, q.From, q.To, q.Step.Seconds())
	if err != nil {
		return nil, true, true, err
	}
	defer rows.Close()
	points := []indexHistoryPoint{}
	stored := false
	for rows.Next() {
		var p indexHistoryPoint
		if err := rows.Scan(&p.TS, &p.IdxScanRate, &p.IdxTupReadRate, &p.IndexBytes); err != nil {
			return nil, true, true, err
		}
		p.TS = p.TS.UTC()
		if p.IdxScanRate != nil || p.IdxTupReadRate != nil || p.IndexBytes != nil {
			stored = true
		}
		points = append(points, p)
	}
	if err := rows.Err(); err != nil {
		return nil, true, true, err
	}
	if !stored {
		points = []indexHistoryPoint{}
	}
	return points, true, true, nil
}
