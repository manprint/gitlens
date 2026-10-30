package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/manprint/pglens/internal/wire"
)

// Data completeness after an agent start (13.2, D22d). The agent reports, on
// every push, when it started and the interval of each check it schedules
// (wire.Instance.AgentStartedAt and CheckIntervals, stored on the instance),
// and the pipeline keeps the newest result of each check in each database
// (check_results). The coverage endpoint merges the two: a check has reported once a result of the
// current agent run is stored, and until then it is expected within its
// first-run bound (wire.FirstRunBound) of the start.

// coverageGrace is added to a check's first-run bound to say when it is
// expected: the run itself and the next push take some seconds.
const coverageGrace = 30 * time.Second

// Coverage states of one check.
const (
	coverageReported = "reported"
	coveragePending  = "pending"
	coverageFailed   = "failed"
)

// coverageResult is the stored result of one check, its databases merged:
// the first and the newest result of the current run, and whether the newest
// result of any database failed.
type coverageResult struct {
	First, Last time.Time
	Failed      bool
}

// coverageRecord is what the store holds for one instance.
type coverageRecord struct {
	// AgentStartedAt is nil when the agent does not report it.
	AgentStartedAt *time.Time
	// CheckIntervals is nil when the agent does not report it.
	CheckIntervals map[string]int
	Results        map[string]coverageResult
}

// coverageStore reads one instance's coverage. found is false for an unknown
// instance. Unit tests stub it.
type coverageStore interface {
	InstanceCoverage(ctx context.Context, id uuid.UUID) (rec coverageRecord, found bool, err error)
}

// The SQL of the coverage store; the instance id is a bound parameter (I-3).
const (
	coverageInstanceSQL = `SELECT agent_started_at, check_intervals FROM instances WHERE tenant_id = 'default' AND instance_id = $1`
	coverageResultsSQL  = `SELECT check_name, first_result_at, last_result_at, last_failed FROM check_results WHERE instance_id = $1 ORDER BY check_name, datname`
)

// pgCoverageStore is the coverageStore of a database.
type pgCoverageStore struct {
	pool dbPool
}

func (s pgCoverageStore) InstanceCoverage(ctx context.Context, id uuid.UUID) (coverageRecord, bool, error) {
	var (
		rec       coverageRecord
		intervals []byte
	)
	if err := s.pool.QueryRow(ctx, coverageInstanceSQL, id).Scan(&rec.AgentStartedAt, &intervals); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return coverageRecord{}, false, nil
		}
		return coverageRecord{}, false, err
	}
	if len(intervals) > 0 {
		if err := json.Unmarshal(intervals, &rec.CheckIntervals); err != nil {
			return coverageRecord{}, false, fmt.Errorf("read check_intervals: %w", err)
		}
	}
	rows, err := s.pool.Query(ctx, coverageResultsSQL, id)
	if err != nil {
		return coverageRecord{}, false, err
	}
	defer rows.Close()
	byCheck := map[string][]coverageResult{}
	for rows.Next() {
		var (
			name string
			res  coverageResult
		)
		if err := rows.Scan(&name, &res.First, &res.Last, &res.Failed); err != nil {
			return coverageRecord{}, false, err
		}
		byCheck[name] = append(byCheck[name], res)
	}
	if err := rows.Err(); err != nil {
		return coverageRecord{}, false, err
	}
	rec.Results = make(map[string]coverageResult, len(byCheck))
	for name, results := range byCheck {
		rec.Results[name] = mergeCheckResults(results, rec.AgentStartedAt)
	}
	return rec, true, nil
}

// mergeCheckResults merges the rows of one check, one per database (a single
// row for an instance-scoped check): the earliest first result, the newest
// result, and failed when the newest result of any database failed. With the
// agent start known, a database whose newest result predates it has not
// reported in the current run and is left out, so an old failure there does
// not mark the check failed; when no database has reported yet, the newest row
// is kept and reads as pending.
func mergeCheckResults(results []coverageResult, started *time.Time) coverageResult {
	current := results[:0:0]
	for _, r := range results {
		if started == nil || !r.Last.Before(*started) {
			current = append(current, r)
		}
	}
	if len(current) == 0 {
		newest := results[0]
		for _, r := range results[1:] {
			if r.Last.After(newest.Last) {
				newest = r
			}
		}
		return newest
	}
	merged := current[0]
	for _, r := range current[1:] {
		if r.First.Before(merged.First) {
			merged.First = r.First
		}
		if r.Last.After(merged.Last) {
			merged.Last = r.Last
		}
		merged.Failed = merged.Failed || r.Failed
	}
	return merged
}

// coverageCheckJSON is one check as getCoverage lists it.
type coverageCheckJSON struct {
	Check string `json:"check"`
	// IntervalSeconds is null when the agent does not report intervals.
	IntervalSeconds *int   `json:"interval_seconds"`
	State           string `json:"state"`
	// FirstResultAt and LastResultAt are the stored results of the current
	// agent run, null while the check is pending.
	FirstResultAt *time.Time `json:"first_result_at"`
	LastResultAt  *time.Time `json:"last_result_at"`
	// ExpectedBy is set while the check is pending and the agent start is
	// known: the start, plus the check's first-run bound, plus coverageGrace.
	ExpectedBy *time.Time `json:"expected_by"`
}

// coverageJSON is the getCoverage response.
type coverageJSON struct {
	InstanceID     string              `json:"instance_id"`
	AgentStartedAt *time.Time          `json:"agent_started_at"`
	GeneratedAt    time.Time           `json:"generated_at"`
	Checks         []coverageCheckJSON `json:"checks"`
	Reported       int                 `json:"reported"`
	Total          int                 `json:"total"`
}

// buildCoverage merges the agent's checks with their stored results. The
// checks are the ones the agent schedules; an agent that does not report them
// has its stored results listed instead, with no interval and no expectation.
// A result counts for the current run when it is not older than the agent
// start, or always when the start is unknown.
func buildCoverage(id uuid.UUID, rec coverageRecord, now time.Time) coverageJSON {
	names := make([]string, 0, len(rec.CheckIntervals)+len(rec.Results))
	if rec.CheckIntervals != nil {
		for name := range rec.CheckIntervals {
			names = append(names, name)
		}
	} else {
		for name := range rec.Results {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	out := coverageJSON{InstanceID: id.String(), GeneratedAt: now.UTC(), Checks: make([]coverageCheckJSON, 0, len(names)), Total: len(names)}
	var started *time.Time
	if rec.AgentStartedAt != nil {
		t := rec.AgentStartedAt.UTC()
		started = &t
	}
	out.AgentStartedAt = started
	for _, name := range names {
		row := coverageCheckJSON{Check: name, State: coveragePending}
		if seconds, ok := rec.CheckIntervals[name]; ok {
			s := seconds
			row.IntervalSeconds = &s
		}
		res, stored := rec.Results[name]
		current := stored && (started == nil || !res.Last.Before(*started))
		switch {
		case current:
			first, last := res.First.UTC(), res.Last.UTC()
			row.FirstResultAt, row.LastResultAt = &first, &last
			row.State = coverageReported
			if res.Failed {
				row.State = coverageFailed
			}
		case started != nil && row.IntervalSeconds != nil:
			expected := started.Add(wire.FirstRunBound(time.Duration(*row.IntervalSeconds)*time.Second) + coverageGrace)
			row.ExpectedBy = &expected
		}
		if row.State == coverageReported {
			out.Reported++
		}
		out.Checks = append(out.Checks, row)
	}
	return out
}

// coverageStore is the store of the API: the stub of a test, else the
// database, else nil (no store).
func (a *API) coverageStore() coverageStore {
	if a.coverage != nil {
		return a.coverage
	}
	if a.pool == nil {
		return nil
	}
	return pgCoverageStore{pool: a.pool}
}

func (a *API) registerCoverageRoutes(r chi.Router) {
	r.Get("/api/v1/instances/{id}/coverage", a.handleCoverage)
}

// handleCoverage answers how complete the picture of an instance is since its
// agent started: every check, reported, pending or failed.
func (a *API) handleCoverage(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", err.Error())
		return
	}
	st := a.coverageStore()
	if st == nil {
		writeError(w, http.StatusNotFound, "instance not found", fmt.Sprintf("instance %s not found", idStr))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rec, found, err := st.InstanceCoverage(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "coverage query failed", err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "instance not found", fmt.Sprintf("instance %s not found", idStr))
		return
	}
	writeJSON(w, http.StatusOK, buildCoverage(id, rec, time.Now()))
}
