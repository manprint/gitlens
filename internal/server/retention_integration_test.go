//go:build integration

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// retentionJob is one retention policy as TimescaleDB lists it.
type retentionJob struct {
	JobID     int64
	DropAfter string
}

// retentionJobs reads the retention policies of a hypertable by name, never by
// job id: a policy created again is a new job.
func retentionJobs(t *testing.T, pool *pgxpool.Pool, hypertable string) []retentionJob {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT job_id, config->>'drop_after'
		FROM timescaledb_information.jobs
		WHERE proc_name = 'policy_retention' AND hypertable_name = $1 ORDER BY job_id`, hypertable)
	require.NoError(t, err)
	jobs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (retentionJob, error) {
		var j retentionJob
		return j, row.Scan(&j.JobID, &j.DropAfter)
	})
	require.NoError(t, err)
	return jobs
}

// restoreRetention puts a hypertable's migration policy back after a test.
func restoreRetention(t *testing.T, pool *pgxpool.Pool, hypertable, interval string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		_, err := pool.Exec(ctx, `SELECT remove_retention_policy(format('public.%I', $1::text)::regclass, if_exists => true)`, hypertable)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `SELECT add_retention_policy(format('public.%I', $1::text)::regclass, drop_after => $2::interval)`, hypertable, interval)
		require.NoError(t, err)
	})
}

func retentionRouter(api *API) http.Handler {
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	return r
}

func putRetention(t *testing.T, h http.Handler, family, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings/retention/"+family, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func previewRetention(t *testing.T, h http.Handler, family string, days int) retentionPreviewResp {
	t.Helper()
	var out retentionPreviewResp
	getJSON(t, h, "/api/v1/settings/retention/preview?family="+family+"&days="+strconv.Itoa(days), &out)
	return out
}

// seedAshChunks writes one ASH row at each age in days, so each age lands in a
// chunk of its own (6-hour chunks).
func seedAshChunks(t *testing.T, pool *pgxpool.Pool, ages ...int) {
	t.Helper()
	instance := uuid.New()
	for _, age := range ages {
		_, err := pool.Exec(context.Background(), `INSERT INTO metrics_ash
			(ts, cluster_id, instance_id, wait_event_type, wait_event, state, samples, window_seconds)
			VALUES (now() - make_interval(days => $1::int), 1, $2, 'CPU', 'CPU', 'active', 1, 10)`, age, instance)
		require.NoError(t, err)
	}
}

// RET-INT-001: the live listing reads the migrations' policies; a preview
// counts the seeded chunks older than the days; PUT 14 days with confirm leaves
// one retention job with drop_after 14 days, and lengthening back needs no
// confirmation.
func TestRETINT001(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	t.Cleanup(func() { truncateAll(t, pool) })
	restoreRetention(t, pool, "metrics_ash", "30 days")
	h := retentionRouter(NewAPI(pool))

	var rows []retentionRow
	getJSON(t, h, "/api/v1/settings/retention", &rows)
	require.Len(t, rows, 7)
	for _, row := range rows {
		require.NotNil(t, row.Days, row.Family)
		require.Equal(t, "default", row.Source, row.Family)
		if row.Family == "metrics_bloat" {
			require.Equal(t, 90, *row.Days)
			require.Equal(t, 8, row.MinDays, "computed from the live 7-day compression policy")
		} else {
			require.Equal(t, 30, *row.Days, row.Family)
			require.Equal(t, 3, row.MinDays, "computed from the live 48-hour compression policy")
		}
	}

	// Ages 1 and 5 days stay under a 14-day retention; 20 and 25 are dropped.
	seedAshChunks(t, pool, 1, 5, 20, 25)
	// A compressed chunk is counted and sized as well.
	_, err := pool.Exec(context.Background(), `SELECT compress_chunk(c) FROM show_chunks('metrics_ash', older_than => interval '24 days') c`)
	require.NoError(t, err)

	p := previewRetention(t, h, "metrics_ash", 14)
	require.Equal(t, "metrics_ash", p.Family)
	require.Equal(t, 14, p.Days)
	require.Equal(t, int64(2), p.ChunksToDrop)
	require.Positive(t, p.BytesToFree)
	require.NotNil(t, p.OldestChunkAt)
	require.NotNil(t, p.NewestDroppedChunkEnd)
	require.True(t, p.NewestDroppedChunkEnd.After(*p.OldestChunkAt))

	var nativeCount int64
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM show_chunks('metrics_ash', older_than => interval '14 days')`).Scan(&nativeCount))
	require.Equal(t, nativeCount, p.ChunksToDrop, "the preview is what show_chunks returns")
	var nativeBytes int64
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT coalesce(sum(d.total_bytes), 0)::bigint
		FROM chunks_detailed_size('metrics_ash') d
		JOIN show_chunks('metrics_ash', older_than => interval '14 days') s(chunk)
		  ON format('%I.%I', d.chunk_schema, d.chunk_name)::regclass = s.chunk`).Scan(&nativeBytes))
	require.Equal(t, nativeBytes, p.BytesToFree, "the bytes are chunks_detailed_size of those chunks")

	require.Equal(t, int64(3), previewRetention(t, h, "metrics_ash", 4).ChunksToDrop)
	// Not shorter than the live 30 days: nothing more is dropped.
	lengthen := previewRetention(t, h, "metrics_ash", 60)
	require.Zero(t, lengthen.ChunksToDrop)
	require.Zero(t, lengthen.BytesToFree)
	require.Nil(t, lengthen.NewestDroppedChunkEnd)
	require.NotNil(t, lengthen.OldestChunkAt)

	before := retentionJobs(t, pool, "metrics_ash")
	require.Len(t, before, 1)
	require.Equal(t, "30 days", before[0].DropAfter)

	rec := putRetention(t, h, "metrics_ash", `{"days":14}`)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"chunks_to_drop":2`)
	require.Equal(t, before, retentionJobs(t, pool, "metrics_ash"), "a refused shortening writes nothing")

	rec = putRetention(t, h, "metrics_ash", `{"days":14,"confirm":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"family":"metrics_ash","hypertable":"metrics_ash","days":14,"source":"custom","min_days":3,"max_days":365}`, rec.Body.String())
	after := retentionJobs(t, pool, "metrics_ash")
	require.Len(t, after, 1, "one retention policy, never two")
	require.Equal(t, "14 days", after[0].DropAfter)

	// The same days again do not replace the job.
	rec = putRetention(t, h, "metrics_ash", `{"days":14}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, after, retentionJobs(t, pool, "metrics_ash"))

	// Lengthening needs no confirmation and reads back as the default.
	rec = putRetention(t, h, "metrics_ash", `{"days":30}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"source":"default"`)
	final := retentionJobs(t, pool, "metrics_ash")
	require.Len(t, final, 1)
	require.Equal(t, "30 days", final[0].DropAfter)

	// The other families were not touched.
	getJSON(t, h, "/api/v1/settings/retention", &rows)
	for _, row := range rows {
		require.Equal(t, "default", row.Source, row.Family)
	}

	// A family whose policy is gone reads days null and needs confirmation.
	restoreRetention(t, pool, "metrics_replication", "30 days")
	_, err = pool.Exec(context.Background(), `SELECT remove_retention_policy('metrics_replication')`)
	require.NoError(t, err)
	getJSON(t, h, "/api/v1/settings/retention", &rows)
	require.Nil(t, rows[3].Days)
	require.Equal(t, "custom", rows[3].Source)
	rec = putRetention(t, h, "metrics_replication", `{"days":365}`)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	rec = putRetention(t, h, "metrics_replication", `{"days":365,"confirm":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "365 days", retentionJobs(t, pool, "metrics_replication")[0].DropAfter)
}

// faultPool is the shared pool whose transactions fail the policy write in
// the database after it ran: the alter of an existing policy is followed by a
// failing statement, and the add of a new one is sent to a hypertable that
// does not exist. alters counts the alters that ran without an error.
type faultPool struct {
	*pgxpool.Pool
	alters *int
}

func (p faultPool) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return faultTx{Tx: tx, alters: p.alters}, nil
}

type faultTx struct {
	pgx.Tx
	alters *int
}

func (t faultTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	switch sql {
	case retentionAlterSQL:
		var altered int64
		if err := t.Tx.QueryRow(ctx, sql, args...).Scan(&altered); err == nil && altered > 0 {
			*t.alters++
		}
		// A division by zero aborts the transaction the alter ran in.
		return t.Tx.QueryRow(ctx, `SELECT 1/0`)
	case retentionAddSQL:
		args = append([]any{"metrics_no_such_family"}, args[1:]...)
	}
	return t.Tx.QueryRow(ctx, sql, args...)
}

// RET-INT-002: a PUT whose policy write fails in the database rolls back. An
// altered policy keeps its old drop_after, its job and its next start; a
// family without a policy is left without one.
func TestRETINT002(t *testing.T) {
	pool := getSharedPool(t)
	restoreRetention(t, pool, "metrics_ash", "30 days")
	before := settledSchedule(t, pool, "metrics_ash")

	var alters int
	h := retentionRouter(&API{pool: faultPool{Pool: pool, alters: &alters}})
	for i, body := range []string{`{"days":14,"confirm":true}`, `{"days":60}`} {
		rec := putRetention(t, h, "metrics_ash", body)
		require.Equal(t, i+1, alters, "alter_job ran before the failure")
		require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
		require.Contains(t, rec.Body.String(), `"error":"retention update failed"`)
		require.Contains(t, rec.Body.String(), "alter_job")
		after := retentionScheduleOf(t, pool, "metrics_ash")
		require.Equal(t, before.JobID, after.JobID)
		require.Equal(t, "30 days", after.DropAfter, "the old policy is kept")
		require.True(t, before.NextStart.Equal(after.NextStart))
	}

	var rows []retentionRow
	getJSON(t, retentionRouter(NewAPI(pool)), "/api/v1/settings/retention", &rows)
	require.Equal(t, 30, *rows[2].Days)
	require.Equal(t, "default", rows[2].Source)

	restoreRetention(t, pool, "metrics_replication", "30 days")
	_, err := pool.Exec(context.Background(), `SELECT remove_retention_policy('metrics_replication')`)
	require.NoError(t, err)
	rec := putRetention(t, h, "metrics_replication", `{"days":60,"confirm":true}`)
	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "add_retention_policy")
	require.Empty(t, retentionJobs(t, pool, "metrics_replication"), "still no policy")
}

// retentionSchedule is the job of a hypertable's retention policy as the
// scheduler sees it.
type retentionSchedule struct {
	JobID     int64
	DropAfter string
	NextStart time.Time
	TotalRuns int64
	Running   bool
	// Unscheduled is true while the scheduler has not yet given the job a
	// next start (a job just added without initial_start).
	Unscheduled bool
}

// retentionScheduleOf reads the single retention job of a hypertable, with its
// next start and the runs recorded for it (none before its first run).
func retentionScheduleOf(t *testing.T, pool *pgxpool.Pool, hypertable string) retentionSchedule {
	t.Helper()
	var (
		s    retentionSchedule
		next *time.Time
	)
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT j.job_id, j.config->>'drop_after', j.next_start,
			coalesce(st.total_runs, 0), coalesce(st.job_status = 'Running', false)
		FROM timescaledb_information.jobs j
		LEFT JOIN timescaledb_information.job_stats st USING (job_id)
		WHERE j.proc_name = 'policy_retention' AND j.hypertable_name = $1`, hypertable).
		Scan(&s.JobID, &s.DropAfter, &next, &s.TotalRuns, &s.Running))
	if next == nil {
		s.Unscheduled = true
	} else {
		s.NextStart = *next
	}
	return s
}

// settledSchedule waits until the hypertable's retention job is scheduled and
// not running, so its run count and next start are stable before a write is
// measured against them.
func settledSchedule(t *testing.T, pool *pgxpool.Pool, hypertable string) retentionSchedule {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		s := retentionScheduleOf(t, pool, hypertable)
		if !s.Running && !s.Unscheduled {
			return s
		}
		require.True(t, time.Now().Before(deadline), "the retention job of %s kept running", hypertable)
		time.Sleep(200 * time.Millisecond)
	}
}

// retentionRunGrace is how long the test watches for a run the write must not
// start: TimescaleDB started a newly added job within a second.
const retentionRunGrace = 5 * time.Second

// RET-INT-003: a console change keeps the policy's schedule. Changing the days
// of an existing policy alters its job in place: same job id, same next start,
// no run started by the write, drop_after the new value, and that value is the
// one the policy deletes with when it runs. A policy given to a family that has
// none first runs one day later, not at once.
func TestRETINT003(t *testing.T) {
	pool := getSharedPool(t)
	ctx := context.Background()
	truncateAll(t, pool)
	t.Cleanup(func() { truncateAll(t, pool) })
	restoreRetention(t, pool, "metrics_ash", "30 days")
	h := retentionRouter(NewAPI(pool))

	before := settledSchedule(t, pool, "metrics_ash")
	require.Equal(t, "30 days", before.DropAfter)

	// Ages 1 and 5 days stay under 14 days of retention; 20 and 25 are older.
	seedAshChunks(t, pool, 1, 5, 20, 25)
	countChunks := func() int64 {
		var n int64
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM show_chunks('metrics_ash')`).Scan(&n))
		return n
	}
	require.Equal(t, int64(4), countChunks())

	rec := putRetention(t, h, "metrics_ash", `{"days":14,"confirm":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	time.Sleep(retentionRunGrace)
	require.Equal(t, int64(4), countChunks(), "the shortening deleted nothing before the policy's next run")
	after := retentionScheduleOf(t, pool, "metrics_ash")
	require.Equal(t, before.JobID, after.JobID, "the job is altered, not replaced")
	require.True(t, before.NextStart.Equal(after.NextStart), "next_start kept: %s, now %s", before.NextStart, after.NextStart)
	require.Equal(t, before.TotalRuns, after.TotalRuns, "the write started no run")
	require.False(t, after.Running)
	require.Equal(t, "14 days", after.DropAfter, "stored as add_retention_policy stores it")

	// Lengthening keeps the schedule too, and reads back as the default.
	rec = putRetention(t, h, "metrics_ash", `{"days":30}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"source":"default"`)
	lengthened := retentionScheduleOf(t, pool, "metrics_ash")
	require.Equal(t, before.JobID, lengthened.JobID)
	require.True(t, before.NextStart.Equal(lengthened.NextStart))
	require.Equal(t, "30 days", lengthened.DropAfter)

	// The policy deletes with the stored value when it runs: at 14 days the
	// chunks aged 20 and 25 days go, those aged 1 and 5 stay.
	rec = putRetention(t, h, "metrics_ash", `{"days":14,"confirm":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, int64(4), countChunks(), "nothing is deleted before the policy runs")
	_, err := pool.Exec(ctx, `CALL run_job($1)`, before.JobID)
	require.NoError(t, err)
	require.Equal(t, int64(2), countChunks(), "the run dropped the chunks older than the new 14 days")

	// A family without a policy gets one whose first run is a day away.
	restoreRetention(t, pool, "metrics_replication", "30 days")
	_, err = pool.Exec(ctx, `SELECT remove_retention_policy('metrics_replication')`)
	require.NoError(t, err)
	rec = putRetention(t, h, "metrics_replication", `{"days":60,"confirm":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var now time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT now()`).Scan(&now))
	time.Sleep(retentionRunGrace)
	created := retentionScheduleOf(t, pool, "metrics_replication")
	require.Equal(t, "60 days", created.DropAfter)
	require.True(t, !created.NextStart.Before(now.Add(23*time.Hour)), "first run %s, not before %s", created.NextStart, now.Add(23*time.Hour))
	var fixed bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT fixed_schedule FROM timescaledb_information.jobs WHERE job_id = $1`, created.JobID).Scan(&fixed))
	require.False(t, fixed, "a day after each run ends, as the migrations' jobs")
	require.Zero(t, created.TotalRuns, "the new policy has not run")
}
