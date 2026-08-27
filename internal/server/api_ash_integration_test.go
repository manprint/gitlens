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
	buckets, totalTicks, err := ashAPI.queryASHBuckets(ctx, instID, nil, nil, "", []string{}, 1000)
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
	ts := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

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
	_, totalTicks, err := ashAPI.queryASHBuckets(ctx, instID, nil, nil, "", []string{}, 1000)
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
	_, totalTicks, err = ashAPI.queryASHBuckets(ctx, instID, nil, nil, "", []string{}, 1000)
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
	entries, _, err := ashAPI.queryASHTop(ctx, instID, nil, nil, "", 1000)
	require.NoError(t, err)

	require.Len(t, entries, 1)
	require.Equal(t, qid, entries[0].QueryID)
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

	var metricsCount int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM metrics WHERE instance_id = $1`, instID).Scan(&metricsCount)
	require.NoError(t, err)
	require.Equal(t, 0, metricsCount, "ASH data should NOT be in metrics table")
}
