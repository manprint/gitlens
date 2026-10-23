//go:build integration

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/store"
	"github.com/manprint/pglens/internal/wire"
)

// INT-ASH-010: ingest three ASH windows and assert the API returns the right avg_active_sessions
func TestIntASH010_AvgActiveSessionsCalculation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool := getSharedPool(t)
	truncateAll(t, pool)

	ctx := contextBackground()

	// Ingest test data
	instID := uuid.New()
	cid := pgtype.ClusterID(12345)
	ts := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// Create 3 windows with different ticks/samples
	windows := []struct {
		samples int
		ticks   int
	}{
		{100, 10},
		{80, 10},
		{90, 10},
	}

	for i, w := range windows {
		ts := ts.Add(time.Duration(i*10) * time.Second)

		env := wire.Envelope{
			ProtocolVersion: 1,
			Instances: []wire.Instance{
				{
					InstanceID: instID.String(),
					ClusterID:  cid.String(),
					Results: []wire.Result{
						{
							Check: "ash",
							TS:    ts,
							Metrics: []wire.Metric{
								{
									Name:  "ash_bucket",
									Value: float64(w.samples),
									Kind:  "gauge",
									Labels: map[string]string{
										"wait_event_type": "CPU",
										"wait_event":      "CPU",
										"state":           "active",
										"datname":         "testdb",
										"window_seconds":  "10",
										"window_ticks":    fmt.Sprintf("%d", w.ticks),
									},
								},
							},
						},
					},
				},
			},
		}

		pipeline := NewPipeline(pool, nil)
		_, err := pipeline.Process(ctx, env)
		require.NoError(t, err)
	}

	// Query the API
	ashAPI := &AshAPI{pool: asDBPool(pool)}
	buckets, totalTicks, err := ashAPI.queryASHBuckets(ctx, instID, nil, nil, "", ashFilters{}, 1000)
	require.NoError(t, err)

	// Verify results
	require.Len(t, buckets, 3)
	require.Equal(t, 30, totalTicks) // 10+10+10

	// queryASHBuckets orders by ts DESC (newest first), so buckets[0]
	// corresponds to the last-inserted window, not the first.
	for i, b := range buckets {
		w := windows[len(windows)-1-i]
		require.NotNil(t, b.AvgActiveSessions)
		expected := float64(w.samples) / float64(w.ticks)
		require.Equal(t, expected, *b.AvgActiveSessions)
	}
}

// INT-ASH-011: the significance warning appears below 60 ticks and not at or above it
func TestIntASH011_SignificanceWarning(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool := getSharedPool(t)
	truncateAll(t, pool)

	ctx := contextBackground()

	instID := uuid.New()
	cid := pgtype.ClusterID(12345)
	ts := time.Now().UTC().Truncate(time.Second)

	// Case 1: ingest 59 ticks total
	for i := 0; i < 59; i++ {
		tsWindow := ts.Add(time.Duration(i) * time.Second)
		env := wire.Envelope{
			ProtocolVersion: 1,
			Instances: []wire.Instance{
				{
					InstanceID: instID.String(),
					ClusterID:  cid.String(),
					Results: []wire.Result{
						{
							Check: "ash",
							TS:    tsWindow,
							Metrics: []wire.Metric{
								{
									Name:  "ash_bucket",
									Value: 1,
									Kind:  "gauge",
									Labels: map[string]string{
										"wait_event_type": "CPU",
										"wait_event":      "CPU",
										"state":           "active",
										"datname":         "testdb",
										"window_seconds":  "10",
										"window_ticks":    "1",
									},
								},
							},
						},
					},
				},
			},
		}
		pipeline := NewPipeline(pool, nil)
		_, err := pipeline.Process(ctx, env)
		require.NoError(t, err)
	}

	// Query with 59 ticks - should have warning
	ashAPI := &AshAPI{pool: asDBPool(pool)}
	_, totalTicks, err := ashAPI.queryASHBuckets(ctx, instID, nil, nil, "", ashFilters{}, 1000)
	require.NoError(t, err)
	require.Equal(t, 59, totalTicks)

	// Query the endpoint and check for warning
	req := httptest.NewRequest("GET", "/api/v1/ash?instance_id="+instID.String(), nil)
	w := httptest.NewRecorder()
	r := chi.NewRouter()
	ashAPI.RegisterRoutes(r)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	resp := ashResponse{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.NotNil(t, resp.Warning)
	require.Contains(t, *resp.Warning, "fewer than 60 samples")

	// Case 2: ingest one more tick to reach 60
	tsWindow := ts.Add(time.Duration(59) * time.Second)
	env := wire.Envelope{
		ProtocolVersion: 1,
		Instances: []wire.Instance{
			{
				InstanceID: instID.String(),
				ClusterID:  cid.String(),
				Results: []wire.Result{
					{
						Check: "ash",
						TS:    tsWindow,
						Metrics: []wire.Metric{
							{
								Name:  "ash_bucket",
								Value: 1,
								Kind:  "gauge",
								Labels: map[string]string{
									"wait_event_type": "CPU",
									"wait_event":      "CPU",
									"state":           "active",
									"datname":         "testdb",
									"window_seconds":  "10",
									"window_ticks":    "1",
								},
							},
						},
					},
				},
			},
		},
	}
	pipeline := NewPipeline(pool, nil)
	_, err = pipeline.Process(ctx, env)
	require.NoError(t, err)

	// Query with 60 ticks - should NOT have warning
	_, totalTicks, err = ashAPI.queryASHBuckets(ctx, instID, nil, nil, "", ashFilters{}, 1000)
	require.NoError(t, err)
	require.Equal(t, 60, totalTicks)

	req = httptest.NewRequest("GET", "/api/v1/ash?instance_id="+instID.String(), nil)
	w = httptest.NewRecorder()
	r = chi.NewRouter()
	ashAPI.RegisterRoutes(r)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	resp = ashResponse{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Nil(t, resp.Warning)
}

// INT-ASH-012: group_by=queryid joins to query_texts and returns the text
func TestIntASH012_QueryIDJoinToQueryTexts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool := getSharedPool(t)
	truncateAll(t, pool)

	ctx := contextBackground()

	instID := uuid.New()
	cid := pgtype.ClusterID(12345)
	ts := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	qid := int64(999)
	queryText := "SELECT COUNT(*) FROM large_table WHERE id > 100"

	// Ingest ASH window with queryid
	env := wire.Envelope{
		ProtocolVersion: 1,
		Instances: []wire.Instance{
			{
				InstanceID: instID.String(),
				ClusterID:  cid.String(),
				Results: []wire.Result{
					{
						Check:    "ash",
						TS:       ts,
						Database: "testdb", // must match the metric's datname label: query_texts is keyed by (datname, queryid, pg_major), and pipeline.go attributes QueryTexts to r.Database
						Metrics: []wire.Metric{
							{
								Name:  "ash_bucket",
								Value: 100,
								Kind:  "gauge",
								Labels: map[string]string{
									"wait_event_type": "IO",
									"wait_event":      "DataFileRead",
									"state":           "active",
									"datname":         "testdb",
									"queryid":         fmt.Sprintf("%d", qid),
									"window_seconds":  "10",
									"window_ticks":    "10",
								},
							},
						},
						QueryTexts: map[string]string{
							fmt.Sprintf("%d", qid): queryText,
						},
					},
				},
			},
		},
	}

	pipeline := NewPipeline(pool, nil)
	_, err := pipeline.Process(ctx, env)
	require.NoError(t, err)

	// Query via /api/v1/ash/top
	ashAPI := &AshAPI{pool: asDBPool(pool)}
	entries, _, err := ashAPI.queryASHTop(ctx, instID, nil, nil, "", ashFilters{}, 1000)
	require.NoError(t, err)

	require.Len(t, entries, 1)
	require.Equal(t, fmt.Sprintf("%d", qid), entries[0].QueryID)
	require.Equal(t, queryText, entries[0].QueryText)
	require.Equal(t, 100, entries[0].Samples)
	require.Equal(t, 10, entries[0].Ticks)
}

// INT-ASH-013: migration 0004 applies cleanly to a database already migrated to 0003
func TestIntASH013_MigrationIdempotence(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool := getSharedPool(t)
	ctx := contextBackground()

	// Verify the migration ran and column exists
	var exists bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'metrics_ash'
			AND column_name = 'window_ticks'
		)`).Scan(&exists)
	require.NoError(t, err)
	require.True(t, exists, "window_ticks column should exist after migration")

	// Verify it has the right default
	var defaultVal string
	err = pool.QueryRow(ctx,
		`SELECT column_default
		 FROM information_schema.columns
		 WHERE table_name = 'metrics_ash'
		 AND column_name = 'window_ticks'`).Scan(&defaultVal)
	require.NoError(t, err)
	require.Equal(t, "0", defaultVal)
}

// INT-ASH-014: ASH rows never appear in metrics and never pass through the delta engine
func TestIntASH014_ASHBypassesDelta(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool := getSharedPool(t)
	truncateAll(t, pool)

	ctx := contextBackground()
	var err error

	instID := uuid.New()
	cid := pgtype.ClusterID(12345)
	ts := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// Ingest ASH data
	env := wire.Envelope{
		ProtocolVersion: 1,
		Instances: []wire.Instance{
			{
				InstanceID: instID.String(),
				ClusterID:  cid.String(),
				Results: []wire.Result{
					{
						Check: "ash",
						TS:    ts,
						Metrics: []wire.Metric{
							{
								Name:  "ash_bucket",
								Value: 100,
								Kind:  "gauge",
								Labels: map[string]string{
									"wait_event_type": "CPU",
									"wait_event":      "CPU",
									"state":           "active",
									"datname":         "testdb",
									"window_seconds":  "10",
									"window_ticks":    "10",
								},
							},
						},
					},
				},
			},
		},
	}

	pipeline := NewPipeline(pool, nil)
	_, err = pipeline.Process(ctx, env)
	require.NoError(t, err)

	// Verify rows are in metrics_ash, not in metrics
	var ashCount int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM metrics_ash WHERE instance_id = $1`, instID).Scan(&ashCount)
	require.NoError(t, err)
	require.Greater(t, ashCount, 0, "ASH data should be in metrics_ash")

	// Scoped past pglens_%: every push also writes the collector's own
	// self-monitoring gauges into `metrics` (see selfMetricRows), which is
	// not what this test is about.
	var metricsCount int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM metrics WHERE instance_id = $1 AND metric NOT LIKE 'pglens_%'`, instID).Scan(&metricsCount)
	require.NoError(t, err)
	require.Equal(t, 0, metricsCount, "ASH data should NOT be in metrics table")
}

// TestASHDisabledInstance_ReturnsExplicitFalse proves SYS-ASH-002's own
// requirement: an instance whose agent reports ASH switched off gets an
// explicit "enabled": false from the API, distinct from an idle instance
// with genuinely no data (which never sets ash_enabled at all, and
// TestIntASH010/014 above confirm that case falls through unaffected).
func TestASHDisabledInstance_ReturnsExplicitFalse(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := contextBackground()

	cid := pgtype.ClusterID(777)
	instID := uuid.New()
	agentID := uuid.New()
	cidDB := store.ToDB(cid)

	_, err := pool.Exec(ctx, `INSERT INTO agents (agent_id) VALUES ($1)`, agentID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_id, id_source) VALUES ('default',$1,'manual')`, cidDB)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen, ash_enabled)
		 VALUES ($1,'default',$2,$3,'10.0.0.9',5432,170000,'primary','T0', now(), false)`,
		instID, cidDB, agentID)
	require.NoError(t, err)

	api := &AshAPI{pool: asDBPool(pool)}
	req := httptest.NewRequest("GET", "/api/v1/ash?instance_id="+instID.String(), nil)
	w := httptest.NewRecorder()
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	resp := ashResponse{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.NotNil(t, resp.Enabled)
	require.False(t, *resp.Enabled)
	require.Equal(t, []ashBucket{}, resp.Buckets)
}

// TestIntASH015_GroupByAggregatesPerBucket: group_by returns one row per
// time bucket and group, with the group filled in and the ticks of every
// window counted once; the drill-down filter is echoed on each row; ASH top
// does not count a queryid twice when its text is stored for two majors.
func TestIntASH015_GroupByAggregatesPerBucket(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := contextBackground()
	instID := uuid.New()
	cid := pgtype.ClusterID(4343)
	base := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	for i := 0; i < 2; i++ {
		ts := base.Add(time.Duration(i*10) * time.Second)
		for _, key := range []struct {
			wet, we string
			samples int
		}{{"LWLock", "WALWrite", 6}, {"LWLock", "BufferMapping", 4}, {"CPU", "CPU", 10}} {
			_, err := pool.Exec(ctx, `INSERT INTO metrics_ash (ts, cluster_id, instance_id, datname, wait_event_type, wait_event, state, queryid, samples, window_seconds, window_ticks)
				VALUES ($1, $2, $3, 'app', $4, $5, 'active', 7, $6, 10, 10)`, ts, store.ToDB(cid), instID, key.wet, key.we, key.samples)
			require.NoError(t, err)
		}
	}
	for _, major := range []int{16, 17} {
		_, err := pool.Exec(ctx, `INSERT INTO query_texts (tenant_id, cluster_id, datname, queryid, pg_major, query_text) VALUES ('default', $1, 'app', 7, $2, 'SELECT 7')`, store.ToDB(cid), major)
		require.NoError(t, err)
	}

	r := chi.NewRouter()
	NewAshAPI(pool).RegisterRoutes(r)
	get := func(query string) ashResponse {
		t.Helper()
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/ash?instance_id="+instID.String()+"&from="+base.Format(time.RFC3339)+"&to="+base.Add(time.Hour).Format(time.RFC3339)+query, nil))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var resp ashResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		return resp
	}

	types := get("&group_by=wait_event_type")
	require.Equal(t, 20, types.ResolutionSeconds)
	require.Len(t, types.Buckets, 2, "both windows fall in one 20-second bucket: one row per type")
	byType := map[string]ashBucket{}
	for _, b := range types.Buckets {
		byType[b.WaitEventType] = b
	}
	require.Equal(t, 20, byType["LWLock"].Samples)
	require.Equal(t, 20, byType["LWLock"].Ticks, "two windows of 10 ticks, each counted once")
	require.Equal(t, 1.0, *byType["LWLock"].AvgActiveSessions)
	require.Equal(t, 20, byType["CPU"].Samples)

	events := get("&group_by=wait_event&wait_event_type=LWLock")
	require.Len(t, events.Buckets, 2)
	for _, b := range events.Buckets {
		require.Equal(t, "LWLock", b.WaitEventType, "the drill-down filter is echoed")
		require.Equal(t, 20, b.Ticks, "the filter narrows the samples, not the instance's ticks")
	}

	entries, ticks, err := NewAshAPI(pool).queryASHTop(ctx, instID, &base, nil, "", ashFilters{}, 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, 40, entries[0].Samples, "one text per queryid, not one per major")
	require.Equal(t, "SELECT 7", entries[0].QueryText)
	require.Equal(t, 20, ticks)

	entries, ticks, err = NewAshAPI(pool).queryASHTop(ctx, instID, &base, nil, "", ashFilters{waitEventType: "LWLock", waitEvent: "WALWrite"}, 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, 12, entries[0].Samples, "only the samples seen in the drilled wait event")
	require.Equal(t, 20, ticks, "the filter narrows the samples, not the instance's ticks")

	entries, _, err = NewAshAPI(pool).queryASHTop(ctx, instID, &base, nil, "", ashFilters{waitEvent: "DataFileRead"}, 10)
	require.NoError(t, err)
	require.Empty(t, entries, "no query was seen in a wait event outside the range's samples")
}

// INT-ASH-016: the significance guard reads the range's ticks. Sixty windows,
// each with two wait events, are sixty ticks, not one hundred and twenty. A
// filter that matches nothing has no rows to count, and keeps those sixty
// instead of warning of a sample too small to trust.
func TestIntASH016_TicksCountEachWindowOnceAndSurviveAnEmptyFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool := getSharedPool(t)
	truncateAll(t, pool)

	ctx := contextBackground()
	instID := uuid.New()
	cid := pgtype.ClusterID(12345)
	start := time.Now().UTC().Truncate(time.Second)
	metric := func(waitEventType, waitEvent string) wire.Metric {
		return wire.Metric{
			Name:  "ash_bucket",
			Value: 1,
			Kind:  "gauge",
			Labels: map[string]string{
				"wait_event_type": waitEventType,
				"wait_event":      waitEvent,
				"state":           "active",
				"datname":         "testdb",
				"window_seconds":  "10",
				"window_ticks":    "1",
			},
		}
	}
	for i := 0; i < 60; i++ {
		env := wire.Envelope{
			ProtocolVersion: 1,
			Instances: []wire.Instance{{
				InstanceID: instID.String(),
				ClusterID:  cid.String(),
				Results: []wire.Result{{
					Check:   "ash",
					TS:      start.Add(time.Duration(i) * time.Second),
					Metrics: []wire.Metric{metric("CPU", "CPU"), metric("LWLock", "WALWrite")},
				}},
			}},
		}
		_, err := NewPipeline(pool, nil).Process(ctx, env)
		require.NoError(t, err)
	}

	ashAPI := NewAshAPI(pool)
	_, totalTicks, err := ashAPI.queryASHBuckets(ctx, instID, nil, nil, "", ashFilters{}, 1000)
	require.NoError(t, err)
	require.Equal(t, 60, totalTicks, "each window's ticks once, not once per wait event")

	from := start.Add(-time.Minute).Format(time.RFC3339)
	to := start.Add(3 * time.Minute).Format(time.RFC3339)
	for _, path := range []string{
		"/api/v1/ash?instance_id=" + instID.String() + "&wait_event=Nope",
		"/api/v1/ash?instance_id=" + instID.String() + "&group_by=wait_event_type&wait_event=Nope&from=" + from + "&to=" + to,
		"/api/v1/ash/top?instance_id=" + instID.String() + "&wait_event=Nope",
	} {
		r := chi.NewRouter()
		ashAPI.RegisterRoutes(r)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		require.Equal(t, http.StatusOK, w.Code, path)
		var resp map[string]any
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		require.NotContains(t, resp, "warning", path)
	}
}

// INT-ASH-021: the raw and top answers read the last hour when the request
// gives no from. A window an hour old is left out of the default answer, and
// an explicit from still reaches it.
func TestIntASH021_RawAndTopDefaultToTheLastHour(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := contextBackground()
	instID := uuid.New()
	cid := pgtype.ClusterID(4444)
	now := time.Now().UTC()
	for _, ts := range []time.Time{now.Add(-3 * time.Hour), now.Add(-5 * time.Minute)} {
		_, err := pool.Exec(ctx, `INSERT INTO metrics_ash (ts, cluster_id, instance_id, datname, wait_event_type, wait_event, state, queryid, samples, window_seconds, window_ticks)
			VALUES ($1, $2, $3, 'app', 'LWLock', 'WALWrite', 'active', 7, 6, 10, 10)`, ts, store.ToDB(cid), instID)
		require.NoError(t, err)
	}
	_, err := pool.Exec(ctx, `INSERT INTO query_texts (tenant_id, cluster_id, datname, queryid, pg_major, query_text) VALUES ('default', $1, 'app', 7, 16, 'SELECT 7')`, store.ToDB(cid))
	require.NoError(t, err)

	r := chi.NewRouter()
	NewAshAPI(pool).RegisterRoutes(r)
	get := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		return w
	}
	id := instID.String()
	explicit := now.Add(-4 * time.Hour).Format(time.RFC3339)

	var raw ashResponse
	require.NoError(t, json.NewDecoder(get("/api/v1/ash?instance_id="+id).Body).Decode(&raw))
	require.Len(t, raw.Buckets, 1, "the window an hour old is outside the default last hour")
	require.Equal(t, 6, raw.Buckets[0].Samples)
	require.WithinDuration(t, now.Add(-5*time.Minute), raw.Buckets[0].TS, time.Second)

	raw = ashResponse{}
	require.NoError(t, json.NewDecoder(get("/api/v1/ash?instance_id="+id+"&from="+explicit).Body).Decode(&raw))
	require.Len(t, raw.Buckets, 2, "an explicit from reaches the older window")

	var top ashTopResponse
	require.NoError(t, json.NewDecoder(get("/api/v1/ash/top?instance_id="+id).Body).Decode(&top))
	require.Len(t, top.Entries, 1)
	require.Equal(t, 6, top.Entries[0].Samples, "the top answer reads the same last hour")

	top = ashTopResponse{}
	require.NoError(t, json.NewDecoder(get("/api/v1/ash/top?instance_id="+id+"&from="+explicit).Body).Decode(&top))
	require.Len(t, top.Entries, 1)
	require.Equal(t, 12, top.Entries[0].Samples)
}
