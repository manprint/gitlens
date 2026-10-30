//go:build integration

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// retentionJob is one retention policy as TimescaleDB lists it.
type retentionJob struct {
	JobID     int64
	DropAfter string
}

// retentionJobs reads the retention policies of a hypertable by name, never by
// job id: a replaced policy is a new job.
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

// faultPool is the shared pool whose transactions send the add of a retention
// policy to a hypertable that does not exist, after the remove of the real
// one ran: the add fails inside the transaction. removes counts the removes
// that ran without an error.
type faultPool struct {
	*pgxpool.Pool
	removes *int
}

func (p faultPool) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return faultTx{Tx: tx, removes: p.removes}, nil
}

type faultTx struct {
	pgx.Tx
	removes *int
}

func (t faultTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tag, err := t.Tx.Exec(ctx, sql, args...)
	if sql == retentionRemoveSQL && err == nil {
		*t.removes++
	}
	return tag, err
}

func (t faultTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if sql == retentionAddSQL {
		args = append([]any{"metrics_no_such_family"}, args[1:]...)
	}
	return t.Tx.QueryRow(ctx, sql, args...)
}

// RET-INT-002: a PUT whose add_retention_policy fails after the remove ran
// rolls back: the hypertable keeps its old policy, the same job, and is never
// left without one.
func TestRETINT002(t *testing.T) {
	pool := getSharedPool(t)
	restoreRetention(t, pool, "metrics_ash", "30 days")
	before := retentionJobs(t, pool, "metrics_ash")
	require.Len(t, before, 1)

	var removes int
	h := retentionRouter(&API{pool: faultPool{Pool: pool, removes: &removes}})
	for i, body := range []string{`{"days":14,"confirm":true}`, `{"days":60}`} {
		rec := putRetention(t, h, "metrics_ash", body)
		require.Equal(t, i+1, removes, "remove_retention_policy ran before the failing add")
		require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
		require.Contains(t, rec.Body.String(), `"error":"retention update failed"`)
		require.Contains(t, rec.Body.String(), "add_retention_policy")
		require.Equal(t, before, retentionJobs(t, pool, "metrics_ash"), "the old policy is kept")
	}

	var rows []retentionRow
	getJSON(t, retentionRouter(NewAPI(pool)), "/api/v1/settings/retention", &rows)
	require.Equal(t, 30, *rows[2].Days)
	require.Equal(t, "default", rows[2].Source)
}
