package server

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The workload series endpoint charts the per-scrape totals of the tracked
// statements, tables and indexes (the agent keeps only its top_n of each).
// Every name a caller may ask for maps to a constant SQL expression below:
// the request picks entries of this whitelist and never reaches SQL text.

const (
	maxWorkloadSeries  = 8
	maxWorkloadBuckets = 1000
	// defaultWorkloadWindow is the range when from and to are both absent.
	defaultWorkloadWindow = time.Hour
	workloadQueryTimeout  = 5 * time.Second
)

// workloadComponent is one per-scrape sum of a stored column.
type workloadComponent struct {
	alias  string
	column string
}

// workloadSeriesDef is one chartable series: an expression over the
// per-bucket component sums, its unit and whether it is a rate or a gauge.
type workloadSeriesDef struct {
	expr       string
	components []string
	unit       string
	kind       string
}

type workloadSource struct {
	table string
	// sampleSeconds is how often the agent scrapes the source; a step is
	// never below two samples (I-1).
	sampleSeconds int
	components    []workloadComponent
	series        map[string]workloadSeriesDef
}

func rateSeries(component, unit string) workloadSeriesDef {
	return workloadSeriesDef{expr: component, components: []string{component}, unit: unit, kind: "rate"}
}

func gaugeSeries(component, unit string) workloadSeriesDef {
	return workloadSeriesDef{expr: component, components: []string{component}, unit: unit, kind: "gauge"}
}

var workloadSources = map[string]workloadSource{
	"statements": {
		table:         "metrics_statements",
		sampleSeconds: 60,
		components: []workloadComponent{
			{"calls", "calls_rate"},
			{"exec_time", "exec_time_rate_ms"},
			{"rows", "rows_rate"},
			{"blks_hit", "shared_blks_hit_rate"},
			{"blks_read", "shared_blks_read_rate"},
			{"wal_bytes", "wal_bytes_rate"},
			{"temp_blks", "temp_blks_written_rate"},
		},
		series: map[string]workloadSeriesDef{
			"calls":     rateSeries("calls", "calls/s"),
			"exec_time": rateSeries("exec_time", "ms/s"),
			"rows":      rateSeries("rows", "rows/s"),
			"blks_hit":  rateSeries("blks_hit", "blocks/s"),
			"blks_read": rateSeries("blks_read", "blocks/s"),
			"hit_ratio": {
				expr:       "blks_hit / NULLIF(blks_hit + blks_read, 0)",
				components: []string{"blks_hit", "blks_read"},
				unit:       "ratio",
				kind:       "rate",
			},
			"wal_bytes": rateSeries("wal_bytes", "bytes/s"),
			"temp_blks": rateSeries("temp_blks", "blocks/s"),
		},
	},
	"tables": {
		table:         "metrics_tables",
		sampleSeconds: 300,
		components: []workloadComponent{
			{"seq_scans", "seq_scan"},
			{"idx_scans", "idx_scan"},
			{"tup_ins", "n_tup_ins"},
			{"tup_upd", "n_tup_upd"},
			{"tup_del", "n_tup_del"},
			{"tup_hot_upd", "n_tup_hot_upd"},
			{"dead_tuples", "n_dead_tup"},
			{"live_tuples", "n_live_tup"},
			{"autovacuums", "autovacuum_count"},
		},
		series: map[string]workloadSeriesDef{
			"seq_scans":   rateSeries("seq_scans", "scans/s"),
			"idx_scans":   rateSeries("idx_scans", "scans/s"),
			"tup_ins":     rateSeries("tup_ins", "rows/s"),
			"tup_upd":     rateSeries("tup_upd", "rows/s"),
			"tup_del":     rateSeries("tup_del", "rows/s"),
			"tup_hot_upd": rateSeries("tup_hot_upd", "rows/s"),
			"dead_tuples": gaugeSeries("dead_tuples", "rows"),
			"live_tuples": gaugeSeries("live_tuples", "rows"),
			"autovacuums": rateSeries("autovacuums", "runs/s"),
		},
	},
	"indexes": {
		table:         "metrics_indexes",
		sampleSeconds: 300,
		components: []workloadComponent{
			{"idx_scans", "idx_scan"},
			{"idx_tup_read", "idx_tup_read"},
			{"idx_tup_fetch", "idx_tup_fetch"},
			{"idx_blks_read", "idx_blks_read"},
			{"idx_blks_hit", "idx_blks_hit"},
			{"index_bytes", "index_bytes"},
		},
		series: map[string]workloadSeriesDef{
			"idx_scans":     rateSeries("idx_scans", "scans/s"),
			"idx_tup_read":  rateSeries("idx_tup_read", "rows/s"),
			"idx_tup_fetch": rateSeries("idx_tup_fetch", "rows/s"),
			"idx_blks_read": rateSeries("idx_blks_read", "blocks/s"),
			"idx_blks_hit":  rateSeries("idx_blks_hit", "blocks/s"),
			"index_bytes":   gaugeSeries("index_bytes", "bytes"),
		},
	},
}

func workloadSourceNames() []string {
	names := make([]string, 0, len(workloadSources))
	for name := range workloadSources {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (s workloadSource) seriesNames() []string {
	names := make([]string, 0, len(s.series))
	for name := range s.series {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// workloadQuery is a validated request.
type workloadQuery struct {
	source   string
	series   []string
	database string
	from, to time.Time
	step     time.Duration
}

type workloadRequestError struct{ msg, detail string }

func (e *workloadRequestError) Error() string { return e.msg + ": " + e.detail }

func badWorkload(msg, detail string) error { return &workloadRequestError{msg: msg, detail: detail} }

// parseWorkloadQuery validates the query parameters. The step defaults to
// the source's minimum and is raised to two samples of the source and to at
// most maxWorkloadBuckets buckets; the response reports the effective step.
func parseWorkloadQuery(q url.Values, now time.Time) (workloadQuery, error) {
	sourceName := q.Get("source")
	if sourceName == "" {
		return workloadQuery{}, badWorkload("missing source", "allowed sources: "+strings.Join(workloadSourceNames(), ", "))
	}
	source, ok := workloadSources[sourceName]
	if !ok {
		return workloadQuery{}, badWorkload("unknown source", fmt.Sprintf("source %q; allowed sources: %s", sourceName, strings.Join(workloadSourceNames(), ", ")))
	}
	if len(q["series"]) > 1 {
		return workloadQuery{}, badWorkload("repeated series parameter", "pass the series as one comma-separated list")
	}
	var names []string
	for _, name := range strings.Split(q.Get("series"), ",") {
		if name = strings.TrimSpace(name); name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return workloadQuery{}, badWorkload("missing series", "allowed series for "+sourceName+": "+strings.Join(source.seriesNames(), ", "))
	}
	if len(names) > maxWorkloadSeries {
		return workloadQuery{}, badWorkload("too many series", fmt.Sprintf("%d series requested, at most %d", len(names), maxWorkloadSeries))
	}
	for _, name := range names {
		if _, ok := source.series[name]; !ok {
			return workloadQuery{}, badWorkload("unknown series", fmt.Sprintf("series %q; allowed series for %s: %s", name, sourceName, strings.Join(source.seriesNames(), ", ")))
		}
	}

	to := now.UTC()
	if raw := q.Get("to"); raw != "" {
		t, err := parseTimeParam(raw)
		if err != nil {
			return workloadQuery{}, badWorkload("invalid to", err.Error())
		}
		to = t
	}
	from := to.Add(-defaultWorkloadWindow)
	if raw := q.Get("from"); raw != "" {
		t, err := parseTimeParam(raw)
		if err != nil {
			return workloadQuery{}, badWorkload("invalid from", err.Error())
		}
		from = t
	}
	if !from.Before(to) {
		return workloadQuery{}, badWorkload("invalid range", "from must be before to")
	}
	span := to.Sub(from)
	if !from.Add(span).Equal(to) {
		return workloadQuery{}, badWorkload("invalid range", "a range over 292 years cannot be expressed")
	}

	minStep := time.Duration(2*source.sampleSeconds) * time.Second
	step := minStep
	if raw := q.Get("step"); raw != "" {
		parsed, err := parseStep(raw)
		if err != nil {
			return workloadQuery{}, badWorkload("invalid step", err.Error())
		}
		step = max(parsed, minStep)
	}
	// Whole seconds keep the buckets on the same grid as time_bucket.
	step = step.Round(time.Second)
	// The first bucket starts at or before from, so the aligned window holds
	// at most one bucket more than span/step.
	if workloadBucketCount(from, to, step) > maxWorkloadBuckets {
		step = time.Duration(math.Ceil(float64(span)/(maxWorkloadBuckets-1)/float64(time.Second))) * time.Second
	}
	return workloadQuery{source: sourceName, series: names, database: q.Get("database"), from: from, to: to, step: step}, nil
}

// workloadEpoch is the bucket origin, passed to time_bucket explicitly so the
// Go side and the SQL side cut the same buckets.
var workloadEpoch = time.Unix(0, 0).UTC()

// workloadBucketStart is the start of the bucket that contains t.
func workloadBucketStart(t time.Time, step time.Duration) time.Time {
	start := workloadEpoch.Add(t.Sub(workloadEpoch) / step * step)
	if start.After(t) { // t before the epoch: integer division rounds up
		start = start.Add(-step)
	}
	return start
}

// workloadBucketCount is len(workloadBuckets(from, to, step)), computed
// without listing them, so a huge range is refused before any allocation.
func workloadBucketCount(from, to time.Time, step time.Duration) float64 {
	return math.Ceil(float64(to.Sub(workloadBucketStart(from, step))) / float64(step))
}

// workloadBuckets lists the bucket starts covering [from, to): the first one
// is the bucket that contains from, aligned to workloadEpoch.
func workloadBuckets(from, to time.Time, step time.Duration) []time.Time {
	var buckets []time.Time
	for ts := workloadBucketStart(from, step); ts.Before(to); ts = ts.Add(step) {
		buckets = append(buckets, ts)
	}
	return buckets
}

// workloadSQL builds the statement for the requested series of one source.
// Every identifier and expression comes from workloadSources; the instance,
// window, step and database are bind parameters ($1..$5).
//
// Rows of one scrape are summed per database, those sums are averaged over
// the scrapes in a bucket, and the databases are then added up: a database
// scraped at a different instant than another still counts once per bucket.
//
// Known bias: statements keep only the top queries of each scrape, so a quiet
// database can miss some scrapes of a bucket; its average then covers only the
// scrapes it appeared in instead of counting the others as zero, which
// overstates it slightly. Lookups use the (instance_id, datname, ts DESC)
// index; without a database filter, long windows on a store with many
// instances may need an (instance_id, ts DESC) index.
func workloadSQL(sourceName string, names []string) (string, error) {
	source, ok := workloadSources[sourceName]
	if !ok {
		return "", fmt.Errorf("unknown source %q", sourceName)
	}
	var needed []string
	for _, name := range names {
		def, ok := source.series[name]
		if !ok {
			return "", fmt.Errorf("unknown series %q", name)
		}
		for _, component := range def.components {
			if !slices.Contains(needed, component) {
				needed = append(needed, component)
			}
		}
	}
	columns := make(map[string]string, len(source.components))
	for _, component := range source.components {
		columns[component.alias] = component.column
	}
	var perScrape, perDatabase, perBucket, outputs []string
	for _, alias := range needed {
		perScrape = append(perScrape, fmt.Sprintf("sum(%s) AS %s", columns[alias], alias))
		perDatabase = append(perDatabase, fmt.Sprintf("avg(%s) AS %s", alias, alias))
		perBucket = append(perBucket, fmt.Sprintf("sum(%s) AS %s", alias, alias))
	}
	for _, name := range names {
		outputs = append(outputs, fmt.Sprintf("(%s)::double precision", source.series[name].expr))
	}
	return `WITH per_scrape AS (
	SELECT datname, ts, ` + strings.Join(perScrape, ", ") + `
	FROM ` + source.table + `
	WHERE tenant_id = 'default' AND instance_id = $1 AND ts >= $2 AND ts < $3 AND ($5::text = '' OR datname = $5::text)
	GROUP BY datname, ts
), per_database AS (
	SELECT time_bucket(make_interval(secs => $4::bigint), ts, TIMESTAMPTZ '1970-01-01 00:00:00+00') AS bucket, datname, ` + strings.Join(perDatabase, ", ") + `
	FROM per_scrape
	GROUP BY bucket, datname
), per_bucket AS (
	SELECT bucket, ` + strings.Join(perBucket, ", ") + `
	FROM per_database
	GROUP BY bucket
)
SELECT bucket, ` + strings.Join(outputs, ", ") + `
FROM per_bucket
ORDER BY bucket`, nil
}

type workloadPoint struct {
	TS    time.Time `json:"ts"`
	Value *float64  `json:"value"`
}

type workloadSeries struct {
	Name   string          `json:"name"`
	Unit   string          `json:"unit"`
	Kind   string          `json:"kind"`
	Points []workloadPoint `json:"points"`
}

type workloadSeriesResponse struct {
	StepSeconds int64            `json:"step_seconds"`
	Source      string           `json:"source"`
	Series      []workloadSeries `json:"series"`
}

func (a *API) registerWorkloadRoutes(r chi.Router) {
	r.Get("/api/v1/instances/{id}/workload/series", a.handleWorkloadSeries)
}

func (a *API) handleWorkloadSeries(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", err.Error())
		return
	}
	query, err := parseWorkloadQuery(r.URL.Query(), time.Now())
	if err != nil {
		var bad *workloadRequestError
		if errors.As(err, &bad) {
			writeError(w, http.StatusBadRequest, bad.msg, bad.detail)
			return
		}
		writeError(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}
	if a.pool == nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable", "the server has no database pool")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), workloadQueryTimeout)
	defer cancel()

	var exists bool
	if err := a.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM instances WHERE tenant_id = 'default' AND instance_id = $1)`, id).Scan(&exists); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed", err.Error())
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "instance not found", fmt.Sprintf("instance %s not found", id))
		return
	}

	resp, err := a.workloadSeries(ctx, id, query)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// workloadSeries reads the requested series and fills every bucket of the
// window, with null where no scrape landed.
func (a *API) workloadSeries(ctx context.Context, id uuid.UUID, query workloadQuery) (workloadSeriesResponse, error) {
	sql, err := workloadSQL(query.source, query.series)
	if err != nil {
		return workloadSeriesResponse{}, err
	}
	rows, err := a.pool.Query(ctx, sql, id, query.from, query.to, int64(query.step/time.Second), query.database)
	if err != nil {
		return workloadSeriesResponse{}, err
	}
	values, err := scanWorkloadRows(rows, len(query.series))
	if err != nil {
		return workloadSeriesResponse{}, err
	}
	buckets := workloadBuckets(query.from, query.to, query.step)
	source := workloadSources[query.source]
	resp := workloadSeriesResponse{StepSeconds: int64(query.step / time.Second), Source: query.source, Series: make([]workloadSeries, 0, len(query.series))}
	for index, name := range query.series {
		def := source.series[name]
		points := make([]workloadPoint, 0, len(buckets))
		for _, bucket := range buckets {
			var value *float64
			if row, ok := values[bucket.Unix()]; ok {
				value = row[index]
			}
			points = append(points, workloadPoint{TS: bucket, Value: value})
		}
		resp.Series = append(resp.Series, workloadSeries{Name: name, Unit: def.unit, Kind: def.kind, Points: points})
	}
	return resp, nil
}

// scanWorkloadRows maps each bucket start (Unix seconds) to its series values.
func scanWorkloadRows(rows pgx.Rows, width int) (map[int64][]*float64, error) {
	defer rows.Close()
	values := make(map[int64][]*float64)
	for rows.Next() {
		var bucket time.Time
		row := make([]*float64, width)
		dest := make([]any, 0, width+1)
		dest = append(dest, &bucket)
		for index := range row {
			dest = append(dest, &row[index])
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		for index, value := range row {
			if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0)) {
				row[index] = nil
			}
		}
		values[bucket.Unix()] = row
	}
	return values, rows.Err()
}
