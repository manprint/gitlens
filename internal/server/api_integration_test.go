//go:build integration

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/store"
	"github.com/stretchr/testify/require"
)

func TestAPI_Clusters_WithDB_JSONStringID(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	// insert cluster and instance
	cid := pgtype.ClusterID(1234567890123456789)
	cidDB := store.ToDB(cid)
	_, err := pool.Exec(contextBackground(), `INSERT INTO clusters (tenant_id, cluster_id, id_source) VALUES ('default',$1,'manual')`, cidDB)
	require.NoError(t, err)
	iid := uuid.New()
	agent := uuid.New()
	_, err = pool.Exec(contextBackground(), `INSERT INTO agents (agent_id) VALUES ($1)`, agent)
	require.NoError(t, err)
	_, err = pool.Exec(contextBackground(), `INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen) VALUES ($1,'default',$2,$3,'10.0.0.1',5432,170000,'primary','T0',now())`, iid, cidDB, agent)
	require.NoError(t, err)
	api := NewAPI(pool)
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	// cluster_id must be quoted string
	require.Contains(t, body, `"`+cid.String()+`"`)
	require.NotContains(t, body, fmt.Sprintf(`"cluster_id":%d`, cidDB))
	var resp []clusterResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp, 1)
	require.Equal(t, cid.String(), resp[0].ClusterID)
	require.Equal(t, "ok", resp[0].Health)
	require.Equal(t, 1, resp[0].InstanceCount)
	require.NotNil(t, resp[0].Primary)
	require.Equal(t, iid.String(), *resp[0].Primary)
	// also check instance up flag and standby count
	require.Len(t, resp[0].Instances, 1)
	require.True(t, resp[0].Instances[0].Up)
}

func TestAPI_Clusters_HealthDegradedAndCritical(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	cid := pgtype.ClusterID(111)
	cidDB := store.ToDB(cid)
	_, err := pool.Exec(contextBackground(), `INSERT INTO clusters (tenant_id, cluster_id, id_source) VALUES ('default',$1,'manual')`, cidDB)
	require.NoError(t, err)
	// add primary down (last_seen 5 min ago)
	iid1 := uuid.New()
	agent1 := uuid.New()
	_, err = pool.Exec(contextBackground(), `INSERT INTO agents (agent_id) VALUES ($1)`, agent1)
	require.NoError(t, err)
	_, err = pool.Exec(contextBackground(), `INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen) VALUES ($1,'default',$2,$3,'10.0.0.1',5432,170000,'primary','T0', now() - interval '5 minutes')`, iid1, cidDB, agent1)
	require.NoError(t, err)
	// standby up
	iid2 := uuid.New()
	agent2 := uuid.New()
	_, err = pool.Exec(contextBackground(), `INSERT INTO agents (agent_id) VALUES ($1)`, agent2)
	require.NoError(t, err)
	_, err = pool.Exec(contextBackground(), `INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen) VALUES ($1,'default',$2,$3,'10.0.0.2',5432,170000,'standby','T0', now())`, iid2, cidDB, agent2)
	require.NoError(t, err)
	api := NewAPI(pool)
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp []clusterResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp, 1)
	require.Equal(t, "degraded", resp[0].Health)
	require.Equal(t, 1, resp[0].StandbyCount)

	// now remove primary to get critical (no primary)
	truncateAll(t, pool)
	_, err = pool.Exec(contextBackground(), `INSERT INTO clusters (tenant_id, cluster_id, id_source) VALUES ('default',$1,'manual')`, cidDB)
	require.NoError(t, err)
	// only standby
	iid3 := uuid.New()
	agent3 := uuid.New()
	_, err = pool.Exec(contextBackground(), `INSERT INTO agents (agent_id) VALUES ($1)`, agent3)
	require.NoError(t, err)
	_, err = pool.Exec(contextBackground(), `INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen) VALUES ($1,'default',$2,$3,'10.0.0.3',5432,170000,'standby','T0', now())`, iid3, cidDB, agent3)
	require.NoError(t, err)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/clusters", http.NoBody)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "critical", resp[0].Health)
}

func TestAPI_Metrics_WithDB_NullAndValue(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	iid := uuid.New()
	cid := store.ToDB(pgtype.ClusterID(123))
	agent := uuid.New()
	_, err := pool.Exec(contextBackground(), `INSERT INTO agents (agent_id) VALUES ($1)`, agent)
	require.NoError(t, err)
	_, err = pool.Exec(contextBackground(), `INSERT INTO clusters (tenant_id, cluster_id, id_source) VALUES ('default',$1,'manual')`, cid)
	require.NoError(t, err)
	_, err = pool.Exec(contextBackground(), `INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen) VALUES ($1,'default',$2,$3,'10.0.0.1',5432,170000,'primary','T0',now())`, iid, cid, agent)
	require.NoError(t, err)
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(3 * time.Minute)
	// insert one point in middle bucket (1 minute bucket)
	ts := from.Add(60 * time.Second)
	sid := int64(123)
	_, err = pool.Exec(contextBackground(), `INSERT INTO metrics (ts, tenant_id, cluster_id, instance_id, datname, metric, labels, series_id, value) VALUES ($1,'default',$2,$3,'','my_metric','{}',$4, 42)`, ts, cid, iid, sid)
	require.NoError(t, err)
	api := NewAPI(pool)
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	url := fmt.Sprintf("/api/v1/metrics/query?metric=my_metric&instance_id=%s&from=%s&to=%s&step=60s", iid.String(), from.Format(time.RFC3339), to.Format(time.RFC3339))
	req := httptest.NewRequest(http.MethodGet, url, http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp metricsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Series, 3)
	require.Nil(t, resp.Series[0].Value)
	require.NotNil(t, resp.Series[1].Value)
	require.Equal(t, 42.0, *resp.Series[1].Value)
	require.Nil(t, resp.Series[2].Value)
	// with database filter
	url = fmt.Sprintf("/api/v1/metrics/query?metric=my_metric&instance_id=%s&database=mydb&from=%s&to=%s&step=60s", iid.String(), from.Format(time.RFC3339), to.Format(time.RFC3339))
	req = httptest.NewRequest(http.MethodGet, url, http.NoBody)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestAPI_Events_WithDB_FilterAndLimit(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	cid := store.ToDB(pgtype.ClusterID(999))
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		_, err := pool.Exec(contextBackground(), `INSERT INTO events (tenant_id, ts, type, cluster_id, payload) VALUES ('default',$1,$2,$3,'{}')`, now.Add(time.Duration(i)*time.Minute), "test_e", cid)
		require.NoError(t, err)
	}
	api := NewAPI(pool)
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	// limit 2
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events?limit=2", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var events []eventResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &events))
	require.Len(t, events, 2)
	// newest first
	require.True(t, events[0].TS.After(events[1].TS) || events[0].TS.Equal(events[1].TS))
	// filter by cluster_id
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/events?cluster_id=%s", pgtype.ClusterID(999).String()), http.NoBody)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &events))
	require.Len(t, events, 5)
	// filter by type
	req = httptest.NewRequest(http.MethodGet, "/api/v1/events?type=test_e", http.NoBody)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &events))
	require.Len(t, events, 5)
	// filter by from/to
	from := now.Add(1 * time.Minute).Format(time.RFC3339)
	to := now.Add(4 * time.Minute).Format(time.RFC3339)
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/events?from=%s&to=%s", from, to), http.NoBody)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &events))
	require.Len(t, events, 3)
	// cluster_id string check
	for _, e := range events {
		require.NotNil(t, e.ClusterID)
		require.Equal(t, pgtype.ClusterID(999).String(), *e.ClusterID)
	}
}

func TestAPI_Instance_WithDB(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	cid := store.ToDB(pgtype.ClusterID(123))
	iid := uuid.New()
	agent := uuid.New()
	_, err := pool.Exec(contextBackground(), `INSERT INTO agents (agent_id) VALUES ($1)`, agent)
	require.NoError(t, err)
	_, err = pool.Exec(contextBackground(), `INSERT INTO clusters (tenant_id, cluster_id, id_source) VALUES ('default',$1,'manual')`, cid)
	require.NoError(t, err)
	_, err = pool.Exec(contextBackground(), `INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen) VALUES ($1,'default',$2,$3,'10.0.0.1',5432,170000,'primary','T0',now())`, iid, cid, agent)
	require.NoError(t, err)
	_, err = pool.Exec(contextBackground(), `INSERT INTO databases (instance_id, datname, monitored, skip_reason) VALUES ($1,'postgres',true,null), ($1,'app',false,'no perms')`, iid)
	require.NoError(t, err)
	api := NewAPI(pool)
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+iid.String(), http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp instanceDetailResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, iid.String(), resp.InstanceID)
	require.Equal(t, pgtype.ClusterID(123).String(), resp.ClusterID)
	require.Equal(t, 1, resp.DatabasesNotMonitored)
	require.Len(t, resp.Databases, 2)
	require.True(t, resp.Up)
	// test fallback URL param without chi (direct handler)
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+iid.String(), http.NoBody)
	w2 := httptest.NewRecorder()
	api.handleInstance(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)
}

func TestAPI_Instance_NotFound_WithDB(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	api := NewAPI(pool)
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/11111111-1111-1111-1111-111111111111", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestAPI_Statements_WithDB(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	iid := uuid.New()
	cid := store.ToDB(pgtype.ClusterID(123))
	agent := uuid.New()
	_, err := pool.Exec(contextBackground(), `INSERT INTO agents (agent_id) VALUES ($1)`, agent)
	require.NoError(t, err)
	_, err = pool.Exec(contextBackground(), `INSERT INTO clusters (tenant_id, cluster_id, id_source) VALUES ('default',$1,'manual')`, cid)
	require.NoError(t, err)
	_, err = pool.Exec(contextBackground(), `INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen) VALUES ($1,'default',$2,$3,'10.0.0.1',5432,170000,'primary','T0',now())`, iid, cid, agent)
	require.NoError(t, err)
	ts := time.Now().UTC()
	_, err = pool.Exec(contextBackground(), `INSERT INTO metrics_statements (ts, tenant_id, cluster_id, instance_id, datname, queryid, calls_rate, exec_time_rate_ms) VALUES ($1,'default',$2,$3,'db',123, 10, 100)`, ts, cid, iid)
	require.NoError(t, err)
	_, err = pool.Exec(contextBackground(), `INSERT INTO query_texts (tenant_id, cluster_id, datname, queryid, pg_major, query_text, first_seen, last_seen) VALUES ('default',$1,'db',123,17,'SELECT 1', now(), now())`, cid)
	require.NoError(t, err)
	api := NewAPI(pool)
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/statements?instance_id=%s", iid.String()), http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp statementsResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Statements, 1)
	require.Equal(t, "123", resp.Statements[0].QueryID)
	require.Equal(t, "SELECT 1", resp.Statements[0].QueryText)
	require.NotNil(t, resp.Statements[0].Calls)
	// with database filter and from/to
	from := ts.Add(-time.Hour).Format(time.RFC3339)
	to := ts.Add(time.Hour).Format(time.RFC3339)
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/statements?instance_id=%s&database=db&from=%s&to=%s&order_by=calls&limit=5", iid.String(), from, to), http.NoBody)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

// INT-API-002: `/metrics/query` over a range containing a reset returns `null` for the affected bucket and numbers either side.
func TestAPI_INT_API_002_MetricsQueryWithResetGap(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := contextBackground()

	iid := uuid.New()
	cid := store.ToDB(pgtype.ClusterID(555))
	agent := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO agents (agent_id) VALUES ($1)`, agent)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_id, id_source) VALUES ('default',$1,'manual')`, cid)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen) VALUES ($1,'default',$2,$3,'10.0.0.1',5432,170000,'primary','T0',now())`, iid, cid, agent)
	require.NoError(t, err)

	// Create a time range with 5 buckets
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(5 * time.Minute) // 5 minutes total

	// Insert metric values: 100 at ts[0], 150 at ts[1], <nothing at ts[2] - reset gap, 175 at ts[3], 200 at ts[4]
	// This simulates: counter goes from 100 to 150 (normal), then resets (gap), then 175, then 200
	seriesID := int64(999)
	ts0 := from.Add(0 * time.Minute)
	ts1 := from.Add(1 * time.Minute)
	// ts2 intentionally skipped to create a gap (reset)
	ts3 := from.Add(3 * time.Minute)
	ts4 := from.Add(4 * time.Minute)

	_, err = pool.Exec(ctx, `INSERT INTO metrics (ts, tenant_id, cluster_id, instance_id, datname, metric, labels, series_id, value)
		VALUES
		($1,'default',$2,$3,'','counter_metric','{}', $4, 100),
		($5,'default',$2,$3,'','counter_metric','{}', $4, 150),
		($6,'default',$2,$3,'','counter_metric','{}', $4, 175),
		($7,'default',$2,$3,'','counter_metric','{}', $4, 200)
	`, ts0, cid, iid, seriesID, ts1, ts3, ts4)
	require.NoError(t, err)

	api := NewAPI(pool)
	r := chi.NewRouter()
	api.RegisterRoutes(r)

	// Query the range with 1-minute buckets
	url := fmt.Sprintf("/api/v1/metrics/query?metric=counter_metric&instance_id=%s&from=%s&to=%s&step=60s",
		iid.String(), from.Format(time.RFC3339), to.Format(time.RFC3339))
	req := httptest.NewRequest(http.MethodGet, url, http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp metricsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	// Should have 5 buckets (0-4 minutes)
	require.Len(t, resp.Series, 5)

	// ts0: has value 100
	require.NotNil(t, resp.Series[0].Value)
	require.Equal(t, 100.0, *resp.Series[0].Value)

	// ts1: has value 150
	require.NotNil(t, resp.Series[1].Value)
	require.Equal(t, 150.0, *resp.Series[1].Value)

	// ts2: should be null (reset gap - no metric written during reset)
	require.Nil(t, resp.Series[2].Value)

	// ts3: has value 175
	require.NotNil(t, resp.Series[3].Value)
	require.Equal(t, 175.0, *resp.Series[3].Value)

	// ts4: has value 200
	require.NotNil(t, resp.Series[4].Value)
	require.Equal(t, 200.0, *resp.Series[4].Value)
}

func contextBackground() context.Context { return context.Background() }
