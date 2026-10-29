package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

var workloadNow = time.Date(2026, 10, 29, 10, 41, 7, 0, time.UTC)

func workloadParams(pairs ...string) url.Values {
	q := url.Values{}
	for i := 0; i+1 < len(pairs); i += 2 {
		q.Set(pairs[i], pairs[i+1])
	}
	return q
}

// requireWorkloadError asserts the request is refused with msg and returns
// the detail of the refusal.
func requireWorkloadError(t *testing.T, q url.Values, msg string) string {
	t.Helper()
	_, err := parseWorkloadQuery(q, workloadNow)
	var bad *workloadRequestError
	require.ErrorAs(t, err, &bad)
	require.Equal(t, msg, bad.msg)
	return bad.detail
}

func workloadRouter() http.Handler {
	r := chi.NewRouter()
	NewAPI(nil).RegisterRoutes(r)
	return r
}

func serveWorkload(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	workloadRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, http.NoBody))
	return rec
}

// TestWSER001_UnknownSourceIs400: only the three sources exist; the error
// names them.
func TestWSER001_UnknownSourceIs400(t *testing.T) {
	detail := requireWorkloadError(t, workloadParams("source", "locks", "series", "calls"), "unknown source")
	require.Contains(t, detail, "indexes, statements, tables")
	requireWorkloadError(t, workloadParams("series", "calls"), "missing source")

	rec := serveWorkload(t, "/api/v1/instances/"+uuid.NewString()+"/workload/series?source=locks&series=calls")
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "unknown source")
}

// TestWSER002_UnknownSeriesIs400WithTheAllowedNames: a name of another
// source is unknown too; the error lists the source's names.
func TestWSER002_UnknownSeriesIs400WithTheAllowedNames(t *testing.T) {
	detail := requireWorkloadError(t, workloadParams("source", "statements", "series", "calls,dead_tuples"), "unknown series")
	require.Contains(t, detail, `"dead_tuples"`)
	require.Contains(t, detail, "blks_hit, blks_read, calls, exec_time, hit_ratio, rows, temp_blks, wal_bytes")
	requireWorkloadError(t, workloadParams("source", "tables", "series", " , "), "missing series")
	requireWorkloadError(t, workloadParams("source", "tables", "series", "seq_scans;DROP TABLE x"), "unknown series")

	rec := serveWorkload(t, "/api/v1/instances/"+uuid.NewString()+"/workload/series?source=indexes&series=calls")
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "idx_blks_hit")
}

// TestWSER003_NineSeriesIs400: at most eight series; duplicates count once.
func TestWSER003_NineSeriesIs400(t *testing.T) {
	nine := "seq_scans,idx_scans,tup_ins,tup_upd,tup_del,tup_hot_upd,dead_tuples,live_tuples,autovacuums"
	detail := requireWorkloadError(t, workloadParams("source", "tables", "series", nine), "too many series")
	require.Contains(t, detail, "9 series requested, at most 8")

	eight := "calls,exec_time,rows,blks_hit,blks_read,hit_ratio,wal_bytes,temp_blks,calls"
	query, err := parseWorkloadQuery(workloadParams("source", "statements", "series", eight), workloadNow)
	require.NoError(t, err)
	require.Len(t, query.series, 8)

	repeated := url.Values{"source": {"statements"}, "series": {"calls", "rows"}}
	requireWorkloadError(t, repeated, "repeated series parameter")
}

// TestWSER004_StepBelowTheSourceMinimumIsRaised: two samples of the source
// (statements 60 s, tables and indexes 300 s); the default is the minimum.
func TestWSER004_StepBelowTheSourceMinimumIsRaised(t *testing.T) {
	for source, minimum := range map[string]time.Duration{"statements": 2 * time.Minute, "tables": 10 * time.Minute, "indexes": 10 * time.Minute} {
		series := workloadSources[source].seriesNames()[0]
		query, err := parseWorkloadQuery(workloadParams("source", source, "series", series, "step", "30s"), workloadNow)
		require.NoError(t, err)
		require.Equal(t, minimum, query.step, source)
		query, err = parseWorkloadQuery(workloadParams("source", source, "series", series), workloadNow)
		require.NoError(t, err)
		require.Equal(t, minimum, query.step, source)
	}
	query, err := parseWorkloadQuery(workloadParams("source", "statements", "series", "calls", "step", "5m"), workloadNow)
	require.NoError(t, err)
	require.Equal(t, 5*time.Minute, query.step)

	requireWorkloadError(t, workloadParams("source", "statements", "series", "calls", "step", "soon"), "invalid step")
	requireWorkloadError(t, workloadParams("source", "statements", "series", "calls", "step", "-1m"), "invalid step")
}

// TestWSER005_MoreThan1000BucketsRaisesTheStep: the aligned window, first
// partial bucket included, never holds more than 1000 buckets.
func TestWSER005_MoreThan1000BucketsRaisesTheStep(t *testing.T) {
	from := workloadNow.Add(-30 * 24 * time.Hour).Add(17 * time.Second)
	query, err := parseWorkloadQuery(workloadParams("source", "statements", "series", "calls",
		"from", from.Format(time.RFC3339), "to", workloadNow.Format(time.RFC3339), "step", "2m"), workloadNow)
	require.NoError(t, err)
	require.Greater(t, query.step, 2*time.Minute)
	require.Zero(t, query.step%time.Second)
	buckets := workloadBuckets(query.from, query.to, query.step)
	require.LessOrEqual(t, len(buckets), maxWorkloadBuckets)
	require.Equal(t, float64(len(buckets)), workloadBucketCount(query.from, query.to, query.step))

	// The longest expressible range is refused or capped without listing buckets.
	query, err = parseWorkloadQuery(workloadParams("source", "tables", "series", "dead_tuples",
		"from", "1800-01-01T00:00:00Z", "to", "2026-01-01T00:00:00Z"), workloadNow)
	require.NoError(t, err)
	require.LessOrEqual(t, workloadBucketCount(query.from, query.to, query.step), float64(maxWorkloadBuckets))
}

// TestWSER006_EveryWhitelistEntryHasUnitKindAndAConstantExpression guards I-3:
// the expressions are constants over known components, with no parameter
// marker, statement separator or comment.
func TestWSER006_EveryWhitelistEntryHasUnitKindAndAConstantExpression(t *testing.T) {
	identifier := regexp.MustCompile(`^[a-z_]+$`)
	for name, source := range workloadSources {
		require.True(t, identifier.MatchString(source.table), name)
		require.Positive(t, source.sampleSeconds, name)
		aliases := map[string]bool{}
		for _, component := range source.components {
			require.True(t, identifier.MatchString(component.alias), component.alias)
			require.True(t, identifier.MatchString(component.column), component.column)
			aliases[component.alias] = true
		}
		require.NotEmpty(t, source.series, name)
		for seriesName, def := range source.series {
			label := name + "." + seriesName
			require.True(t, identifier.MatchString(seriesName), label)
			require.NotEmpty(t, def.unit, label)
			require.Contains(t, []string{"rate", "gauge"}, def.kind, label)
			for _, forbidden := range []string{"$", ";", "--", "/*", "'"} {
				require.NotContains(t, def.expr, forbidden, label)
			}
			require.NotEmpty(t, def.components, label)
			for _, component := range def.components {
				require.True(t, aliases[component], label+" uses "+component)
				require.Contains(t, def.expr, component, label)
			}
		}
	}
}

const tablesDeadTuplesSQL = `WITH per_scrape AS (
	SELECT datname, ts, sum(n_dead_tup) AS dead_tuples
	FROM metrics_tables
	WHERE tenant_id = 'default' AND instance_id = $1 AND ts >= $2 AND ts < $3 AND ($5::text = '' OR datname = $5::text)
	GROUP BY datname, ts
), per_database AS (
	SELECT time_bucket(make_interval(secs => $4::bigint), ts, TIMESTAMPTZ '1970-01-01 00:00:00+00') AS bucket, datname, avg(dead_tuples) AS dead_tuples
	FROM per_scrape
	GROUP BY bucket, datname
), per_bucket AS (
	SELECT bucket, sum(dead_tuples) AS dead_tuples
	FROM per_database
	GROUP BY bucket
)
SELECT bucket, (dead_tuples)::double precision
FROM per_bucket
ORDER BY bucket`

// TestWSER007_SQLHoldsOnlyWhitelistedExpressions: a golden statement, and for
// every source and series the statement reads only whitelisted columns, in
// the requested order, with the five bind parameters.
func TestWSER007_SQLHoldsOnlyWhitelistedExpressions(t *testing.T) {
	sql, err := workloadSQL("tables", []string{"dead_tuples"})
	require.NoError(t, err)
	require.Equal(t, tablesDeadTuplesSQL, sql)

	sql, err = workloadSQL("statements", []string{"calls", "hit_ratio"})
	require.NoError(t, err)
	require.Contains(t, sql, "sum(calls_rate) AS calls, sum(shared_blks_hit_rate) AS blks_hit, sum(shared_blks_read_rate) AS blks_read")
	require.Contains(t, sql, "SELECT bucket, (calls)::double precision, (blks_hit / NULLIF(blks_hit + blks_read, 0))::double precision")

	sumOf := regexp.MustCompile(`sum\(([a-z_]+)\) AS`)
	param := regexp.MustCompile(`\$\d+`)
	for name, source := range workloadSources {
		columns := map[string]bool{}
		for _, component := range source.components {
			columns[component.column] = true
			columns[component.alias] = true
		}
		for _, seriesName := range source.seriesNames() {
			sql, err := workloadSQL(name, []string{seriesName})
			require.NoError(t, err)
			require.Contains(t, sql, "FROM "+source.table+"\n")
			require.Contains(t, sql, "("+source.series[seriesName].expr+")::double precision")
			for _, match := range sumOf.FindAllStringSubmatch(sql, -1) {
				require.True(t, columns[match[1]], name+"."+seriesName+" sums "+match[1])
			}
			used := map[string]bool{}
			for _, marker := range param.FindAllString(sql, -1) {
				used[marker] = true
			}
			require.Equal(t, map[string]bool{"$1": true, "$2": true, "$3": true, "$4": true, "$5": true}, used)
			require.NotContains(t, sql, ";")
		}
	}

	_, err = workloadSQL("tables", []string{"calls"})
	require.Error(t, err)
	_, err = workloadSQL("bloat", []string{"calls"})
	require.Error(t, err)
}

// TestWSER008_BucketsCoverTheWindowOnTheEpochGrid: the first bucket contains
// from, the last one starts before to, all on multiples of the step.
func TestWSER008_BucketsCoverTheWindowOnTheEpochGrid(t *testing.T) {
	from := time.Date(2026, 10, 29, 10, 1, 30, 844_000_000, time.UTC)
	to := from.Add(10 * time.Minute)
	buckets := workloadBuckets(from, to, 2*time.Minute)
	require.Len(t, buckets, 6)
	require.Equal(t, time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC), buckets[0])
	require.Equal(t, time.Date(2026, 10, 29, 10, 10, 0, 0, time.UTC), buckets[5])
	for _, bucket := range buckets {
		require.Zero(t, bucket.Unix()%120)
	}
	// Before the epoch the bucket still contains from.
	early := time.Date(1969, 12, 31, 23, 59, 0, 0, time.UTC)
	require.Equal(t, time.Date(1969, 12, 31, 23, 58, 0, 0, time.UTC), workloadBuckets(early, early.Add(time.Second), 2*time.Minute)[0])
}

// TestWSER009_WindowDefaultsAndValidation: the window defaults to the last
// hour; a reversed or malformed window is a 400.
func TestWSER009_WindowDefaultsAndValidation(t *testing.T) {
	query, err := parseWorkloadQuery(workloadParams("source", "statements", "series", "calls", "database", "app"), workloadNow)
	require.NoError(t, err)
	require.Equal(t, workloadNow, query.to)
	require.Equal(t, workloadNow.Add(-time.Hour), query.from)
	require.Equal(t, "app", query.database)

	requireWorkloadError(t, workloadParams("source", "statements", "series", "calls", "from", "yesterday"), "invalid from")
	requireWorkloadError(t, workloadParams("source", "statements", "series", "calls", "to", "later"), "invalid to")
	requireWorkloadError(t, workloadParams("source", "statements", "series", "calls",
		"from", "2026-10-29T11:00:00Z", "to", "2026-10-29T10:00:00Z"), "invalid range")
}

// TestWSER010_HandlerStatusCodes: a bad instance id is a 400, a valid request
// without a database pool a 503 with a JSON error.
func TestWSER010_HandlerStatusCodes(t *testing.T) {
	rec := serveWorkload(t, "/api/v1/instances/not-a-uuid/workload/series?source=statements&series=calls")
	require.Equal(t, http.StatusBadRequest, rec.Code)

	rec = serveWorkload(t, "/api/v1/instances/"+uuid.NewString()+"/workload/series?source=statements&series=calls")
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	var body apiError
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "database unavailable", body.Error)
	require.True(t, strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json"))
}
