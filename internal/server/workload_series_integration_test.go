//go:build integration

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/store"
)

// seedWorkloadInstance registers an instance (with its cluster and agent) and
// removes it and its workload rows when the test ends.
func seedWorkloadInstance(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	instance, agent := uuid.New(), uuid.New()
	cluster := store.ToDB(pgtype.ClusterID(time.Now().UnixNano()))
	_, err := pool.Exec(ctx, `INSERT INTO agents (agent_id) VALUES ($1)`, agent)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_id, id_source) VALUES ('default',$1,'manual')`, cluster)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen)
		VALUES ($1,'default',$2,$3,'10.0.0.7',5432,180000,'primary','T0',now())`, instance, cluster, agent)
	require.NoError(t, err)
	t.Cleanup(func() {
		for _, table := range []string{"metrics_statements", "metrics_tables", "metrics_indexes", "instances"} {
			_, _ = pool.Exec(ctx, `DELETE FROM `+table+` WHERE instance_id=$1`, instance)
		}
		_, _ = pool.Exec(ctx, `DELETE FROM clusters WHERE cluster_id=$1`, cluster)
		_, _ = pool.Exec(ctx, `DELETE FROM agents WHERE agent_id=$1`, agent)
	})
	return instance
}

type workloadBody struct {
	StepSeconds int64  `json:"step_seconds"`
	Source      string `json:"source"`
	Series      []struct {
		Name   string `json:"name"`
		Unit   string `json:"unit"`
		Kind   string `json:"kind"`
		Points []struct {
			TS    time.Time `json:"ts"`
			Value *float64  `json:"value"`
		} `json:"points"`
	} `json:"series"`
}

func getWorkload(t *testing.T, pool *pgxpool.Pool, instance uuid.UUID, params url.Values) (int, workloadBody) {
	t.Helper()
	r := chi.NewRouter()
	NewAPI(pool).RegisterRoutes(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/v1/instances/"+instance.String()+"/workload/series?"+params.Encode(), http.NoBody))
	var body workloadBody
	if rec.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), rec.Body.String())
	}
	return rec.Code, body
}

// valuesOf maps the bucket starts to the series' values (nil for null).
func valuesOf(body workloadBody, series int) map[time.Time]*float64 {
	out := map[time.Time]*float64{}
	for _, point := range body.Series[series].Points {
		out[point.TS.UTC()] = point.Value
	}
	return out
}

// workloadBase is an hour boundary two hours back: every step of the tests
// divides an hour, so it starts a bucket.
func workloadBase() time.Time { return time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour) }

func window(from, to time.Time, step string, pairs ...string) url.Values {
	q := url.Values{"from": {from.Format(time.RFC3339)}, "to": {to.Format(time.RFC3339)}, "step": {step}}
	for i := 0; i+1 < len(pairs); i += 2 {
		q.Set(pairs[i], pairs[i+1])
	}
	return q
}

// TestWSERINT001_StatementCallsAverageThePerScrapeSums: two queries over three
// scrapes 60 s apart; a 120 s bucket holds the mean of its scrapes' sums.
func TestWSERINT001_StatementCallsAverageThePerScrapeSums(t *testing.T) {
	pool := getSharedPool(t)
	instance := seedWorkloadInstance(t, pool)
	base := workloadBase()
	for scrape, calls := range [][2]float64{{1, 10}, {2, 20}, {3, 30}} {
		for query, rate := range calls {
			_, err := pool.Exec(context.Background(), `INSERT INTO metrics_statements
				(ts, cluster_id, instance_id, datname, queryid, calls_rate, exec_time_rate_ms, shared_blks_hit_rate, shared_blks_read_rate)
				VALUES ($1, 1, $2, 'app', $3, $4, $5, 9, 1)`,
				base.Add(time.Duration(scrape)*time.Minute), instance, int64(query+1), rate, rate*2)
			require.NoError(t, err)
		}
	}

	code, body := getWorkload(t, pool, instance, window(base, base.Add(4*time.Minute), "2m",
		"source", "statements", "series", "calls,exec_time,hit_ratio"))
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, int64(120), body.StepSeconds)
	require.Equal(t, "statements", body.Source)
	require.Len(t, body.Series, 3)
	require.Equal(t, "calls", body.Series[0].Name)
	require.Equal(t, "calls/s", body.Series[0].Unit)
	require.Equal(t, "rate", body.Series[0].Kind)
	require.Len(t, body.Series[0].Points, 2)

	calls := valuesOf(body, 0)
	require.InDelta(t, (11.0+22.0)/2, *calls[base], 1e-9)
	require.InDelta(t, 33.0, *calls[base.Add(2*time.Minute)], 1e-9)
	exec := valuesOf(body, 1)
	require.InDelta(t, 33.0, *exec[base], 1e-9)
	// 2 queries × 9 hits and 1 read per scrape: 18 / 20.
	require.InDelta(t, 0.9, *valuesOf(body, 2)[base], 1e-9)
}

// TestWSERINT002_DeadTuplesIsASumOfGaugesPerScrape: the gauge adds up the
// tables of one scrape and averages the scrapes, never treated as a rate.
func TestWSERINT002_DeadTuplesIsASumOfGaugesPerScrape(t *testing.T) {
	pool := getSharedPool(t)
	instance := seedWorkloadInstance(t, pool)
	base := workloadBase()
	for scrape, dead := range [][2]int64{{100, 50}, {300, 50}} {
		for table, value := range dead {
			_, err := pool.Exec(context.Background(), `INSERT INTO metrics_tables
				(ts, cluster_id, instance_id, datname, schemaname, relname, n_dead_tup, n_live_tup, seq_scan)
				VALUES ($1, 1, $2, 'app', 'public', $3, $4, 1000, 0.5)`,
				base.Add(time.Duration(scrape)*5*time.Minute), instance, fmt.Sprintf("t%d", table), value)
			require.NoError(t, err)
		}
	}

	code, body := getWorkload(t, pool, instance, window(base, base.Add(10*time.Minute), "10m",
		"source", "tables", "series", "dead_tuples,seq_scans,live_tuples"))
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "gauge", body.Series[0].Kind)
	require.Equal(t, "rows", body.Series[0].Unit)
	require.InDelta(t, (150.0+350.0)/2, *valuesOf(body, 0)[base], 1e-9)
	require.InDelta(t, 1.0, *valuesOf(body, 1)[base], 1e-9)
	require.InDelta(t, 2000.0, *valuesOf(body, 2)[base], 1e-9)
}

// TestWSERINT003_ScrapeGapsAreNullBuckets: every bucket of the window is
// returned, null where no scrape landed, so the chart shows the gap.
func TestWSERINT003_ScrapeGapsAreNullBuckets(t *testing.T) {
	pool := getSharedPool(t)
	instance := seedWorkloadInstance(t, pool)
	base := workloadBase()
	for _, offset := range []time.Duration{0, 30 * time.Minute} {
		_, err := pool.Exec(context.Background(), `INSERT INTO metrics_indexes
			(ts, cluster_id, instance_id, datname, schemaname, relname, indexrelname, idx_scan, index_bytes)
			VALUES ($1, 1, $2, 'app', 'public', 'orders', 'orders_pkey', 4, 8192)`, base.Add(offset), instance)
		require.NoError(t, err)
	}

	code, body := getWorkload(t, pool, instance, window(base, base.Add(40*time.Minute), "10m",
		"source", "indexes", "series", "index_bytes,idx_scans"))
	require.Equal(t, http.StatusOK, code)
	require.Len(t, body.Series[0].Points, 4)
	bytes := valuesOf(body, 0)
	require.InDelta(t, 8192.0, *bytes[base], 1e-9)
	require.Nil(t, bytes[base.Add(10*time.Minute)])
	require.Nil(t, bytes[base.Add(20*time.Minute)])
	require.InDelta(t, 8192.0, *bytes[base.Add(30*time.Minute)], 1e-9)
	require.Equal(t, "gauge", body.Series[0].Kind)
	require.Equal(t, "rate", body.Series[1].Kind)
}

// TestWSERINT004_DatabaseFilter: a database parameter keeps that database's
// rows only; without it the databases add up.
func TestWSERINT004_DatabaseFilter(t *testing.T) {
	pool := getSharedPool(t)
	instance := seedWorkloadInstance(t, pool)
	base := workloadBase()
	for database, calls := range map[string]float64{"app": 5, "erp": 7} {
		_, err := pool.Exec(context.Background(), `INSERT INTO metrics_statements
			(ts, cluster_id, instance_id, datname, queryid, calls_rate) VALUES ($1, 1, $2, $3, 1, $4)`,
			base, instance, database, calls)
		require.NoError(t, err)
	}
	// erp is scraped 30 s later than app: still one value per database per bucket.
	_, err := pool.Exec(context.Background(), `INSERT INTO metrics_statements
		(ts, cluster_id, instance_id, datname, queryid, calls_rate) VALUES ($1, 1, $2, 'erp', 1, 9)`,
		base.Add(30*time.Second), instance)
	require.NoError(t, err)

	q := window(base, base.Add(2*time.Minute), "2m", "source", "statements", "series", "calls")
	code, body := getWorkload(t, pool, instance, q)
	require.Equal(t, http.StatusOK, code)
	require.InDelta(t, 5.0+(7.0+9.0)/2, *valuesOf(body, 0)[base], 1e-9)

	q.Set("database", "app")
	code, body = getWorkload(t, pool, instance, q)
	require.Equal(t, http.StatusOK, code)
	require.InDelta(t, 5.0, *valuesOf(body, 0)[base], 1e-9)

	q.Set("database", "missing")
	code, body = getWorkload(t, pool, instance, q)
	require.Equal(t, http.StatusOK, code)
	require.Nil(t, valuesOf(body, 0)[base])
}

// TestWSERINT005_DayWindowOver100kRowsIsFast: 24 h of statements scraped every
// minute for 70 queries (100 800 rows) answer within the budget: 1 s, 3 s
// under the race detector.
func TestWSERINT005_DayWindowOver100kRowsIsFast(t *testing.T) {
	pool := getSharedPool(t)
	instance := seedWorkloadInstance(t, pool)
	ctx := context.Background()
	// On the 5 m grid so the window holds exactly 288 buckets.
	to := time.Now().UTC().Truncate(5 * time.Minute)
	from := to.Add(-24 * time.Hour)
	tag, err := pool.Exec(ctx, `INSERT INTO metrics_statements
		(ts, cluster_id, instance_id, datname, queryid, calls_rate, exec_time_rate_ms, rows_rate, shared_blks_hit_rate, shared_blks_read_rate, wal_bytes_rate, temp_blks_written_rate)
		SELECT ts, 1, $1, 'app', q, q, q * 0.5, q * 2, 90, 10, 100, 0
		FROM generate_series($2::timestamptz, $3::timestamptz - interval '1 minute', interval '1 minute') AS ts,
		     generate_series(1, 70) AS q`, instance, from, to)
	require.NoError(t, err)
	require.Equal(t, int64(100_800), tag.RowsAffected())
	_, err = pool.Exec(ctx, `ANALYZE metrics_statements`)
	require.NoError(t, err)

	budget := time.Second
	if raceEnabled {
		budget = 3 * time.Second
	}
	q := window(from, to, "5m", "source", "statements", "series", "calls,exec_time,rows,hit_ratio,wal_bytes,temp_blks,blks_read,blks_hit")
	start := time.Now()
	code, body := getWorkload(t, pool, instance, q)
	elapsed := time.Since(start)
	require.Equal(t, http.StatusOK, code)
	require.Len(t, body.Series, 8)
	require.Len(t, body.Series[0].Points, 288)
	// Sum of q over 1..70 = 2485 calls/s in every bucket.
	require.InDelta(t, 2485.0, *body.Series[0].Points[100].Value, 1e-6)
	require.Less(t, elapsed, budget, "24 h over 100 800 rows took %s", elapsed)
	t.Logf("WSER-INT-005: 24 h, 100 800 rows, 8 series answered in %s (budget %s, race %v)", elapsed, budget, raceEnabled)

	// S6: the plan, for the record.
	sql, err := workloadSQL("statements", []string{"calls", "hit_ratio"})
	require.NoError(t, err)
	rows, err := pool.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) "+sql, instance, from, to, 300.0, "")
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		t.Log(line)
	}
	require.NoError(t, rows.Err())
}

// TestWSERINT006_UnknownInstanceIs404: a valid request for an instance the
// server has never seen.
func TestWSERINT006_UnknownInstanceIs404(t *testing.T) {
	pool := getSharedPool(t)
	code, _ := getWorkload(t, pool, uuid.New(), url.Values{"source": {"statements"}, "series": {"calls"}})
	require.Equal(t, http.StatusNotFound, code)
}
