//go:build integration

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/store"
	"github.com/manprint/pglens/internal/wire"
)

// renameEnv is the envelope of one agent reporting one instance.
func renameEnv(agentID, instanceID string, cid pgtype.ClusterID, source pgtype.IDSource, addr string) wire.Envelope {
	return wire.Envelope{
		AgentID: agentID,
		Instances: []wire.Instance{
			{InstanceID: instanceID, ClusterID: cid.String(), ClusterIDSource: string(source), Addr: addr, Port: 5432, Role: "primary", PGVersion: 170000, PermTier: "T0"},
		},
	}
}

// register runs an envelope through the inventory and the pipeline, the way a
// push registers an agent's instances.
func register(t *testing.T, pool *pgxpool.Pool, env wire.Envelope) {
	t.Helper()
	res, err := NewInventory(pool).Upsert(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, len(env.Instances), res.Accepted, res.Errors)
	_, err = NewPipeline(pool, clock.NewFake(time.Now())).Process(context.Background(), env)
	require.NoError(t, err)
}

// renameRouter serves the read API, the rename routes included.
func renameRouter(pool *pgxpool.Pool) http.Handler {
	r := chi.NewRouter()
	NewAPI(pool).RegisterRoutes(r)
	return r
}

func patchJSON(t *testing.T, h http.Handler, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func getJSON(t *testing.T, h http.Handler, target string, dst any) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), dst))
}

// clusterNames maps each listed cluster id to its name, "" for none.
func clusterNames(t *testing.T, h http.Handler) map[string]string {
	t.Helper()
	var list []clusterResp
	getJSON(t, h, "/api/v1/clusters", &list)
	names := make(map[string]string, len(list))
	for _, c := range list {
		names[c.ClusterID] = ""
		if c.Name != nil {
			names[c.ClusterID] = *c.Name
		}
	}
	return names
}

// CLNAME-INT-001: a rename shows in the cluster list; a second cluster cannot
// take the same name in another case; a cleared name is null and frees it.
func TestCLNAMEINT001(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	a, b := pgtype.ClusterID(1<<63+11), pgtype.ClusterID(12)
	register(t, pool, renameEnv(uuid.NewString(), uuid.NewString(), a, pgtype.IDSourceSystemIdentifier, "10.1.0.1"))
	register(t, pool, renameEnv(uuid.NewString(), uuid.NewString(), b, pgtype.IDSourceManual, "10.1.0.2"))
	h := renameRouter(pool)

	rec := patchJSON(t, h, "/api/v1/clusters/"+a.String(), `{"name":"  Prod-EU "}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var renamed clusterResp
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &renamed))
	require.Equal(t, a.String(), renamed.ClusterID)
	require.Equal(t, "Prod-EU", *renamed.Name)
	require.Equal(t, 1, renamed.InstanceCount)
	require.Equal(t, map[string]string{a.String(): "Prod-EU", b.String(): ""}, clusterNames(t, h))

	rec = patchJSON(t, h, "/api/v1/clusters/"+b.String(), `{"name":"prod-eu"}`)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"error":"name already in use"`)

	// Renaming a cluster to its own name in another case is not a conflict.
	rec = patchJSON(t, h, "/api/v1/clusters/"+a.String(), `{"name":"prod-eu"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = patchJSON(t, h, "/api/v1/clusters/"+a.String(), `{"name":null}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"name":null`)
	rec = patchJSON(t, h, "/api/v1/clusters/"+b.String(), `{"name":"PROD-EU"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, map[string]string{a.String(): "", b.String(): "PROD-EU"}, clusterNames(t, h))

	rec = patchJSON(t, h, "/api/v1/clusters/"+a.String(), `{"name":"   "}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var stored *string
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT name FROM clusters WHERE cluster_id=$1`, store.ToDB(a)).Scan(&stored))
	require.Nil(t, stored, "a blank name is stored as NULL")

	rec = patchJSON(t, h, "/api/v1/clusters/"+pgtype.ClusterID(999).String(), `{"name":"x"}`)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

	// A cluster row with no instance behind it is not listed, so it is not
	// renamed either.
	_, err := pool.Exec(context.Background(), `INSERT INTO clusters (tenant_id, cluster_id, id_source) VALUES ('default', 77, 'manual')`)
	require.NoError(t, err)
	rec = patchJSON(t, h, "/api/v1/clusters/77", `{"name":"orphan"}`)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

// CLNAME-INT-002: re-registering the same agent and instance keeps the cluster
// name, the source upgrade from manual to system identifier included.
func TestCLNAMEINT002(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	cid := pgtype.ClusterID(4242)
	agent, instance := uuid.NewString(), uuid.NewString()
	register(t, pool, renameEnv(agent, instance, cid, pgtype.IDSourceManual, "10.2.0.1"))
	h := renameRouter(pool)

	rec := patchJSON(t, h, "/api/v1/clusters/"+cid.String(), `{"name":"odoo-prod"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	register(t, pool, renameEnv(agent, instance, cid, pgtype.IDSourceManual, "10.2.0.1"))
	register(t, pool, renameEnv(agent, instance, cid, pgtype.IDSourceSystemIdentifier, "10.2.0.9"))
	var list []clusterResp
	getJSON(t, h, "/api/v1/clusters", &list)
	require.Len(t, list, 1)
	require.Equal(t, "odoo-prod", *list[0].Name)
	require.Equal(t, string(pgtype.IDSourceSystemIdentifier), list[0].IDSource)
	require.Equal(t, "10.2.0.9", list[0].Instances[0].Addr)
}

// INAME-INT-001: an instance display name shows in the instance list, the
// instance detail and the cluster list; another instance cannot take it in
// another case; re-registration keeps it; a cleared name is null.
func TestINAMEINT001(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	cid := pgtype.ClusterID(5151)
	agent := uuid.NewString()
	first, second := uuid.NewString(), uuid.NewString()
	register(t, pool, renameEnv(agent, first, cid, pgtype.IDSourceManual, "10.3.0.1"))
	register(t, pool, renameEnv(uuid.NewString(), second, cid, pgtype.IDSourceManual, "10.3.0.2"))
	h := renameRouter(pool)

	rec := patchJSON(t, h, "/api/v1/instances/"+first, `{"display_name":" odoo-db-1 "}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var renamed instanceListResp
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &renamed))
	require.Equal(t, first, renamed.InstanceID)
	require.Equal(t, cid.String(), renamed.ClusterID)
	require.Equal(t, "10.3.0.1", renamed.Addr)
	require.Equal(t, "odoo-db-1", *renamed.DisplayName)

	rec = patchJSON(t, h, "/api/v1/instances/"+second, `{"display_name":"ODOO-DB-1"}`)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"error":"name already in use"`)

	register(t, pool, renameEnv(agent, first, cid, pgtype.IDSourceManual, "10.3.0.1"))

	var detail instanceDetailResp
	getJSON(t, h, "/api/v1/instances/"+first, &detail)
	require.Equal(t, "odoo-db-1", *detail.DisplayName)
	var list []instanceListResp
	getJSON(t, h, "/api/v1/instances", &list)
	names := map[string]*string{}
	for _, i := range list {
		names[i.InstanceID] = i.DisplayName
	}
	require.Equal(t, "odoo-db-1", *names[first])
	require.Nil(t, names[second])
	var clusters []clusterResp
	getJSON(t, h, "/api/v1/clusters", &clusters)
	require.Len(t, clusters, 1)
	for _, i := range clusters[0].Instances {
		if i.InstanceID == first {
			require.Equal(t, "odoo-db-1", *i.DisplayName)
		} else {
			require.Nil(t, i.DisplayName)
		}
	}

	rec = patchJSON(t, h, "/api/v1/instances/"+first, `{"display_name":null}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"display_name":null`)
	rec = patchJSON(t, h, "/api/v1/instances/"+second, `{"display_name":"ODOO-DB-1"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = patchJSON(t, h, "/api/v1/instances/"+uuid.NewString(), `{"display_name":"x"}`)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}
