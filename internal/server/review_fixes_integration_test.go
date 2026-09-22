//go:build integration

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/wire"
)

func tableStatsResult(ts time.Time, partial bool, tables ...string) wire.Result {
	var metrics []wire.Metric
	for _, table := range tables {
		metrics = append(metrics, wire.Metric{
			Name: "pg_table_n_live_tup", Value: 100, Kind: "gauge",
			Labels: map[string]string{"schemaname": "public", "relname": table},
		})
	}
	return wire.Result{Check: "table_stats", TS: ts, Database: "app", Partial: partial, Metrics: metrics}
}

func relationRows(t *testing.T, ctx context.Context, instance uuid.UUID, relname string) int {
	t.Helper()
	var n int
	require.NoError(t, getSharedPool(t).QueryRow(ctx, `SELECT count(*) FROM metrics_tables WHERE instance_id=$1 AND relname=$2`, instance, relname).Scan(&n))
	return n
}

func tablesTruncated(t *testing.T, instance uuid.UUID) bool {
	t.Helper()
	rec := httptest.NewRecorder()
	relationRouter(NewAPI(getSharedPool(t))).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+instance.String()+"/tables", http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp relationAPIResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return resp.Truncated
}

// TestPipeline_RelationHistorySurvivesAndPartialIsFlagged: a complete sample
// prunes only the relations that disappeared, a partial one (the agent's
// top-N) prunes nothing and flags the read API, and the flag clears with the
// next complete sample. The prune used to delete every older row of the
// database, and every multi-table database was reported as truncated on
// every scrape.
func TestPipeline_RelationHistorySurvivesAndPartialIsFlagged(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	p := NewPipeline(pool, clock.NewFake(now))
	instance := uuid.New()
	cluster := pgtype.ClusterID(4242).String()
	push := func(r wire.Result) {
		t.Helper()
		res, err := p.Process(ctx, wire.Envelope{Instances: []wire.Instance{{InstanceID: instance.String(), ClusterID: cluster, Results: []wire.Result{r}}}})
		require.NoError(t, err)
		require.Equal(t, 1, res.Accepted)
	}

	push(tableStatsResult(now.Add(-12*time.Minute), false, "orders", "customers"))
	push(tableStatsResult(now.Add(-8*time.Minute), false, "orders", "customers"))
	require.Equal(t, 2, relationRows(t, ctx, instance, "orders"), "history of a relation that still exists is kept")
	require.False(t, tablesTruncated(t, instance))

	push(tableStatsResult(now.Add(-4*time.Minute), true, "orders"))
	require.Equal(t, 2, relationRows(t, ctx, instance, "customers"), "a partial sample does not prove customers is gone")
	require.True(t, tablesTruncated(t, instance), "the latest sample held only the top relations")

	push(tableStatsResult(now.Add(-1*time.Minute), false, "orders"))
	require.Equal(t, 0, relationRows(t, ctx, instance, "customers"), "customers is gone from a complete sample")
	require.Equal(t, 4, relationRows(t, ctx, instance, "orders"))
	require.False(t, tablesTruncated(t, instance), "a complete sample clears the flag")

	var events int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE instance_id=$1 AND type='cardinality_truncated'`, instance).Scan(&events))
	require.Zero(t, events, "a deliberate top-N is not a budget overflow")
}

// TestPipeline_BloatPruneSparesPgstattuple: the bloat estimate's prune must
// not remove pgstattuple measurements, which the check does not produce.
func TestPipeline_BloatPruneSparesPgstattuple(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	instance := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO metrics_bloat (ts, cluster_id, instance_id, datname, schemaname, relname, object_kind, method, bloat_ratio)
		VALUES ($1, 1, $2, 'app', 'public', 'dropped', 'table', 'estimate', 0.4),
		       ($1, 1, $2, 'app', 'public', 'dropped', 'table', 'pgstattuple', 0.5)`, now.Add(-time.Hour), instance)
	require.NoError(t, err)
	p := NewPipeline(pool, clock.NewFake(now))
	res, err := p.Process(ctx, wire.Envelope{Instances: []wire.Instance{{InstanceID: instance.String(), ClusterID: pgtype.ClusterID(1).String(), Results: []wire.Result{{
		Check: "bloat_estimate", TS: now, Database: "app",
		Metrics: []wire.Metric{{Name: "pg_bloat_ratio", Value: 0.1, Kind: "gauge", Labels: map[string]string{"schemaname": "public", "relname": "kept", "indexrelname": "", "object_kind": "table", "method": "estimate"}}},
	}}}}})
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
	var methods []string
	rows, err := pool.Query(ctx, `SELECT relname || '/' || method FROM metrics_bloat WHERE instance_id=$1 ORDER BY 1`, instance)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var m string
		require.NoError(t, rows.Scan(&m))
		methods = append(methods, m)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"dropped/pgstattuple", "kept/estimate"}, methods)
}

// TestRelationsAPI_LatestRowPerRelation: one item per relation, fractional
// scan rates and NULL columns included. Each relation used to appear once per
// scrape, and a row with seq_scan 0.25 or a NULL column was skipped.
func TestRelationsAPI_LatestRowPerRelation(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	now := time.Now().UTC()
	instance := uuid.New()
	for i, age := range []time.Duration{10 * time.Minute, 5 * time.Minute, time.Minute} {
		_, err := pool.Exec(ctx, `INSERT INTO metrics_tables (ts, cluster_id, instance_id, datname, schemaname, relname, n_live_tup, n_dead_tup, seq_scan)
			VALUES ($1, 1, $2, 'app', 'public', 'orders', NULL, $3, 0.25)`, now.Add(-age), instance, int64(10+i))
		require.NoError(t, err)
	}
	_, err := pool.Exec(ctx, `INSERT INTO metrics_indexes (ts, cluster_id, instance_id, datname, schemaname, relname, indexrelname, idx_scan, index_bytes)
		VALUES ($1, 1, $2, 'app', 'public', 'orders', 'orders_pkey', 0.5, NULL)`, now, instance)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO metrics_bloat (ts, cluster_id, instance_id, datname, schemaname, relname, object_kind, method, bloat_ratio)
		VALUES ($1, 1, $2, 'app', 'public', 'orders', 'table', 'estimate', NULL), ($3, 1, $2, 'app', 'public', 'ancient', 'table', 'estimate', 0.9)`,
		now, instance, now.Add(-20*24*time.Hour))
	require.NoError(t, err)

	get := func(path string) relationAPIResponse {
		t.Helper()
		rec := httptest.NewRecorder()
		relationRouter(NewAPI(pool)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+instance.String()+path, http.NoBody))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var resp relationAPIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		return resp
	}
	tables := get("/tables")
	require.Len(t, tables.Items, 1)
	require.Equal(t, 12.0, tables.Items[0]["n_dead_tup"], "the newest row wins")
	require.Equal(t, 0.25, tables.Items[0]["seq_scan_rate"])
	require.Equal(t, "app", tables.Items[0]["datname"])
	require.NotContains(t, tables.Items[0], "n_live_tup")

	indexes := get("/indexes")
	require.Len(t, indexes.Items, 1)
	require.Equal(t, 0.5, indexes.Items[0]["idx_scan_rate"])

	bloat := get("/bloat")
	require.Len(t, bloat.Items, 1, "a 20-day-old estimate is not current")
	require.Equal(t, "orders", bloat.Items[0]["relname"])
}

// TestStatementsAPI_TotalsFromRatesAndIntervals: totals are rate x interval,
// and one queryid seen under two PostgreSQL majors is not counted twice.
func TestStatementsAPI_TotalsFromRatesAndIntervals(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	instance := uuid.New()
	p := NewPipeline(pool, clock.NewFake(now))
	for i, calls := range []float64{100, 220, 280} {
		res, err := p.Process(ctx, wire.Envelope{Instances: []wire.Instance{{InstanceID: instance.String(), ClusterID: pgtype.ClusterID(7).String(), PGVersion: 170000, Results: []wire.Result{{
			Check: "stat_statements", TS: now.Add(time.Duration(i-3) * time.Minute), Database: "app",
			QueryTexts: map[string]string{"42": "SELECT 42"},
			Metrics: []wire.Metric{
				{Name: "pg_stat_statements_calls_total", Value: calls, Kind: "counter", Labels: map[string]string{"queryid": "42"}},
				{Name: "pg_stat_statements_temp_blks_written_total", Value: calls * 2, Kind: "counter", Labels: map[string]string{"queryid": "42"}},
			},
		}}}}})
		require.NoError(t, err)
		require.Equal(t, 1, res.Accepted)
	}
	var sampleSeconds float64
	require.NoError(t, pool.QueryRow(ctx, `SELECT sample_seconds FROM metrics_statements WHERE instance_id=$1 ORDER BY ts DESC LIMIT 1`, instance).Scan(&sampleSeconds))
	require.Equal(t, 60.0, sampleSeconds)
	_, err := pool.Exec(ctx, `INSERT INTO query_texts (tenant_id, cluster_id, datname, queryid, pg_major, query_text, last_seen)
		VALUES ('default', $1, 'app', 42, 16, 'SELECT 42', $2)`, int64(pgtype.ClusterID(7)), now.Add(-48*time.Hour))
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/statements?instance_id="+instance.String(), http.NoBody)
	relationRouter(NewAPI(pool)).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp statementsResp
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Statements, 1)
	got := resp.Statements[0]
	require.InDelta(t, 180.0, *got.Calls, 0.001, "220-100 plus 280-220 calls, not the sum of two per-second rates")
	require.InDelta(t, 360.0, *got.TempBlksWritten, 0.001)
	require.Equal(t, "SELECT 42", got.QueryText)
}

// TestPipeline_VacuumAndAnalyzeTimesAreStored: the agent reports each
// maintenance timestamp as an age at the scrape. Only the manual vacuum's was
// kept, so last_autovacuum stayed NULL on every row and every large table
// read as never vacuumed to the advisor.
func TestPipeline_VacuumAndAnalyzeTimesAreStored(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	instance := uuid.New()
	labels := map[string]string{"schemaname": "public", "relname": "orders"}
	res, err := NewPipeline(pool, clock.NewFake(now)).Process(ctx, wire.Envelope{Instances: []wire.Instance{{InstanceID: instance.String(), ClusterID: pgtype.ClusterID(11).String(), Results: []wire.Result{{
		Check: "table_stats", TS: now, Database: "app",
		Metrics: []wire.Metric{
			{Name: "pg_table_n_live_tup", Value: 500000, Kind: "gauge", Labels: labels},
			{Name: "pg_table_last_autovacuum_age_seconds", Value: 3600, Kind: "gauge", Labels: labels},
			{Name: "pg_table_last_autoanalyze_age_seconds", Value: 60, Kind: "gauge", Labels: labels},
			{Name: "pg_table_last_analyze_age_seconds", Value: 7200, Kind: "gauge", Labels: labels},
		},
	}}}}})
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)

	var autovacuum, autoanalyze, analyze *time.Time
	var vacuum *time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT last_vacuum, last_autovacuum, last_analyze, last_autoanalyze FROM metrics_tables WHERE instance_id=$1`, instance).Scan(&vacuum, &autovacuum, &analyze, &autoanalyze))
	require.Nil(t, vacuum, "never vacuumed by hand")
	require.NotNil(t, autovacuum)
	require.True(t, autovacuum.Equal(now.Add(-time.Hour)), "%s", autovacuum)
	require.True(t, autoanalyze.Equal(now.Add(-time.Minute)), "%s", autoanalyze)
	require.True(t, analyze.Equal(now.Add(-2*time.Hour)), "%s", analyze)

	rec := httptest.NewRecorder()
	relationRouter(NewAPI(pool)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+instance.String()+"/tables", http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp relationAPIResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Items, 1)
	at := func(key string) time.Time {
		t.Helper()
		raw, ok := resp.Items[0][key].(string)
		require.True(t, ok, key)
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		require.NoError(t, err)
		return parsed
	}
	require.True(t, at("last_autovacuum_at").Equal(now.Add(-time.Hour)))
	require.True(t, at("last_autoanalyze_at").Equal(now.Add(-time.Minute)))
	require.NotContains(t, resp.Items[0], "last_vacuum_at")
}

// TestPipeline_RecordsWhenABaseBackupWasSeen: a running base backup leaves a
// fact whose last_seen is the last time one was seen; an idle scrape leaves
// it alone.
func TestPipeline_RecordsWhenABaseBackupWasSeen(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	instance := uuid.New()
	p := NewPipeline(pool, clock.NewFake(now))
	push := func(ts time.Time, jobs float64) {
		t.Helper()
		res, err := p.Process(ctx, wire.Envelope{Instances: []wire.Instance{{InstanceID: instance.String(), ClusterID: pgtype.ClusterID(12).String(), Results: []wire.Result{{
			Check: "archiver", TS: ts,
			Metrics: []wire.Metric{
				{Name: "pg_archive_mode_enabled", Value: 0, Kind: "gauge"},
				{Name: "pg_basebackup_jobs_running", Value: jobs, Kind: "gauge"},
			},
		}}}}})
		require.NoError(t, err)
		require.Equal(t, 1, res.Accepted)
	}
	push(now.Add(-2*time.Minute), 0)
	var n int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM object_facts WHERE instance_id=$1 AND kind='backup'`, instance).Scan(&n))
	require.Zero(t, n)

	push(now.Add(-time.Minute), 1)
	push(now, 0)
	var lastSeen time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT last_seen FROM object_facts WHERE instance_id=$1 AND kind='backup' AND key='basebackup_running'`, instance).Scan(&lastSeen))
	require.True(t, lastSeen.Equal(now.Add(-time.Minute)), "%s", lastSeen)
}

// TestSettingsAPI_OnlyTheLatestScrape: a setting the latest scrape did not
// ship (it went back to its default) is neither listed nor drift, and an
// instance that stopped reporting is not a cluster member for drift.
func TestSettingsAPI_OnlyTheLatestScrape(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	const clusterID int64 = 5013
	now := time.Now().UTC().Truncate(time.Microsecond)
	_, err := pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_id, id_source) VALUES ('default',$1,'manual')`, clusterID)
	require.NoError(t, err)
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	for i, id := range ids {
		agent := uuid.New()
		_, err = pool.Exec(ctx, `INSERT INTO agents (agent_id) VALUES ($1)`, agent)
		require.NoError(t, err)
		lastSeen := now
		if i == 2 {
			lastSeen = now.Add(-3 * time.Hour)
		}
		_, err = pool.Exec(ctx, `INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen) VALUES ($1,'default',$2,$3,$4,5432,180000,'standby','T0',$5)`, id, clusterID, agent, "10.0.1."+strconv.Itoa(i), lastSeen)
		require.NoError(t, err)
	}
	fact := func(id uuid.UUID, key, value string, seen time.Time) {
		t.Helper()
		_, err := pool.Exec(ctx, `INSERT INTO object_facts (tenant_id, cluster_id, instance_id, datname, kind, key, labels, value_text, first_seen, last_seen, changed_at) VALUES ('default',$1,$2,'','setting',$3,'{"context":"user"}',$4,$5,$5,$5)`, clusterID, id, key, value, seen)
		require.NoError(t, err)
	}
	fact(ids[0], "work_mem", "4096", now)
	fact(ids[0], "random_page_cost", "1.1", now.Add(-2*time.Hour))
	fact(ids[1], "work_mem", "4096", now)
	fact(ids[1], "random_page_cost", "4", now)
	fact(ids[2], "work_mem", "65536", now.Add(-3*time.Hour))

	r := chi.NewRouter()
	NewAPI(pool).RegisterRoutes(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+ids[0].String()+"/settings", http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var settings settingsAPIResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &settings))
	require.Len(t, settings.Settings, 1)
	require.Equal(t, "work_mem", settings.Settings[0].Name)

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/clusters/5013/settings-drift", http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var drift []driftEntry
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &drift))
	require.Empty(t, drift, "random_page_cost is stale on the first member and work_mem differs only on an instance gone for hours")
}
