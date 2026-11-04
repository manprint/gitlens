//go:build integration

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/pgtype"
)

// SESSAPI-INT-001: a stored lock tree, the monitoring_role and agent_flags
// facts, query_texts and the activity metrics become the session list with
// origin and text, and the summary, through the real store.
func TestSESSAPIINT001_SessionsOriginTextAndSummary(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	id := uuid.New()
	other := uuid.New()
	cid := int64(pgtype.ClusterID(90101))
	otherCID := int64(pgtype.ClusterID(90102))
	now := time.Now().UTC()

	tree := `{"nodes":[],"sessions_truncated":true,"sessions":[
	  {"pid":11,"application_name":"billing","usename":"app","datname":"shop","client_addr":"10.0.0.5","state":"active","query_id":"-4611686018427387904","query_age":1.5,"xact_age":2},
	  {"pid":12,"application_name":"psql","usename":"pglens_monitor","datname":"shop","client_addr":"","state":"idle"},
	  {"pid":13,"application_name":"pglens/check","usename":"app","datname":"shop","client_addr":"10.0.0.9","state":"active","query_id":"77"},
	  {"pid":14,"application_name":"billing","usename":"app","datname":"shop","state":"active","query_id":"5","query":"SELECT 1 /* raw */"},
	  {"pid":15,"application_name":"billing","usename":"app","datname":"shop","state":"idle in transaction"}
	]}`
	_, err := pool.Exec(ctx, `INSERT INTO lock_snapshots (tenant_id,instance_id,cluster_id,ts,tree) VALUES ('default',$1,$2,now(),$3::jsonb)`, id, cid, tree)
	require.NoError(t, err)

	// The newest last_seen wins between two majors; another cluster's text of
	// the same id is never used.
	for _, q := range []struct {
		cluster int64
		db      string
		qid     int64
		major   int
		text    string
		seen    time.Time
	}{
		{cid, "shop", -4611686018427387904, 15, "SELECT old FROM t WHERE id = $1", now.Add(-time.Hour)},
		{cid, "shop", -4611686018427387904, 18, "SELECT new FROM t WHERE id = $1", now},
		{cid, "shop", 5, 18, "SELECT normalised FROM t", now},
		{cid, "elsewhere", 77, 18, "SELECT other database", now},
		{otherCID, "shop", 77, 18, "SELECT other cluster", now},
	} {
		_, err = pool.Exec(ctx, `INSERT INTO query_texts (tenant_id,cluster_id,datname,queryid,pg_major,query_text,first_seen,last_seen) VALUES ('default',$1,$2,$3,$4,$5,$6,$6)`,
			q.cluster, q.db, q.qid, q.major, q.text, q.seen)
		require.NoError(t, err)
	}

	_, err = pool.Exec(ctx, `INSERT INTO object_facts (tenant_id,cluster_id,instance_id,datname,kind,key,labels,value_text,value_json,first_seen,last_seen,changed_at)
		VALUES ('default',$1,$2,'','monitoring_role','self',$3::jsonb,'read-only',NULL,$4,$4,$4)`, cid, id,
		`{"role":"pglens_monitor","read_only_default":"on","has_pg_monitor":"true","has_read_all_stats":"true"}`, now)
	require.NoError(t, err)
	// The monitoring role of another instance does not leak into this one.
	_, err = pool.Exec(ctx, `INSERT INTO object_facts (tenant_id,cluster_id,instance_id,datname,kind,key,labels,value_text,value_json,first_seen,last_seen,changed_at)
		VALUES ('default',$1,$2,'','monitoring_role','self',$3::jsonb,'read-only',NULL,$4,$4,$4)`, otherCID, other,
		`{"role":"app"}`, now)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO object_facts (tenant_id,cluster_id,instance_id,datname,kind,key,labels,value_text,value_json,first_seen,last_seen,changed_at)
		VALUES ('default',$1,$2,'','agent_flags','agent','{}',NULL,$3::jsonb,$4,$4,$4)`, cid, id, `{"by_application":false,"commands_enabled":true}`, now)
	require.NoError(t, err)

	series := int64(94000)
	metric := func(name, labels string, value float64) {
		series++
		_, err := pool.Exec(ctx, `INSERT INTO metrics (ts,tenant_id,cluster_id,instance_id,datname,metric,labels,series_id,value) VALUES ($1,'default',$2,$3,'',$4,$5::jsonb,$6,$7)`, now, cid, id, name, labels, series, value)
		require.NoError(t, err)
	}
	metric("pg_connections_by_origin", `{"origin":"application"}`, 300)
	metric("pg_connections_by_origin", `{"origin":"pglens"}`, 6)
	metric("pg_connections_by_user", `{"usename":"app"}`, 290)
	metric("pg_connections_by_client", `{"client_addr":"local"}`, 12)
	metric("pg_connections_by_database", `{"datname":"shop"}`, 306)
	metric("pg_backends", `{"state":"active","wait_event_type":"CPU"}`, 20)
	metric("pg_backends", `{"state":"idle","wait_event_type":"Client"}`, 286)
	metric("pg_connections_by_application", `{"application_name":"billing"}`, 100)

	r := chi.NewRouter()
	NewAPI(pool).RegisterRoutes(r)
	get := func(instance uuid.UUID) sessapiResponse {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/locks?instance_id="+instance.String(), http.NoBody))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var got sessapiResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		return got
	}
	got := get(id)
	require.Len(t, got.Sessions, 5)
	byPID := map[int]map[string]any{}
	for _, s := range got.Sessions {
		byPID[int(s["pid"].(float64))] = s
	}
	require.Equal(t, "application", byPID[11]["origin"])
	require.Equal(t, "pglens", byPID[12]["origin"], "the monitoring role's session")
	require.Equal(t, "pglens", byPID[13]["origin"])
	require.Equal(t, "SELECT new FROM t WHERE id = $1", byPID[11]["query_text"])
	require.Equal(t, "normalised", byPID[11]["query_text_source"])
	require.Equal(t, 1.5, byPID[11]["query_age"], "existing and new fields pass through")
	require.NotContains(t, byPID[13], "query_text", "an id seen only in another database or cluster has no text")
	require.Equal(t, "SELECT 1 /* raw */", byPID[14]["query_text"])
	require.Equal(t, "raw", byPID[14]["query_text_source"])
	require.NotContains(t, byPID[15], "query_text")

	c := got.Connections
	require.NotNil(t, c)
	require.False(t, c.ByApplicationEnabled, "agent_flags.by_application=false wins over the metric")
	require.True(t, c.Truncated)
	require.Equal(t, map[string]int64{"application": 3, "pglens": 2}, counts(c.ByOrigin))
	require.Equal(t, map[string]int64{"billing": 3, "psql": 1, "pglens/check": 1}, counts(c.ByApplication))
	require.Equal(t, map[string]int64{"local": 3, "10.0.0.5": 1, "10.0.0.9": 1}, counts(c.ByClient))
	require.Equal(t, map[string]int64{"active": 3, "idle": 1, "idle in transaction": 1}, counts(c.ByState))
	require.NotNil(t, c.Totals)
	require.Equal(t, map[string]int64{"application": 300, "pglens": 6}, counts(c.Totals.ByOrigin))
	require.Equal(t, map[string]int64{"active": 20, "idle": 286}, counts(c.Totals.ByState))
	require.Equal(t, map[string]int64{"billing": 100}, counts(c.Totals.ByApplication))

	// Without the role fact the monitoring user is an application session; with
	// no agent_flags fact the metric decides by_application_enabled.
	_, err = pool.Exec(ctx, `DELETE FROM object_facts WHERE instance_id=$1`, id)
	require.NoError(t, err)
	got = get(id)
	require.Equal(t, "application", got.Sessions[1]["origin"])
	require.True(t, got.Connections.ByApplicationEnabled)
	_, err = pool.Exec(ctx, `DELETE FROM metrics WHERE instance_id=$1 AND metric='pg_connections_by_application'`, id)
	require.NoError(t, err)
	require.False(t, get(id).Connections.ByApplicationEnabled)
}
