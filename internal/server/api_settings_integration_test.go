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
)

// INT-SET-003: a primary and standby with different hot_standby_feedback
// settings produce exactly one actionable drift entry.
func TestINTSET003_SettingsDriftPrimaryStandby(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	const clusterID int64 = 5003
	primaryID, standbyID := uuid.New(), uuid.New()
	primaryAgent, standbyAgent := uuid.New(), uuid.New()

	_, err := pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_id, id_source) VALUES ('default',$1,'manual')`, clusterID)
	require.NoError(t, err)
	for _, agentID := range []uuid.UUID{primaryAgent, standbyAgent} {
		_, err = pool.Exec(ctx, `INSERT INTO agents (agent_id) VALUES ($1)`, agentID)
		require.NoError(t, err)
	}
	for _, instance := range []struct {
		id, agent uuid.UUID
		role      string
		addr      string
	}{{primaryID, primaryAgent, "primary", "10.0.0.1"}, {standbyID, standbyAgent, "standby", "10.0.0.2"}} {
		_, err = pool.Exec(ctx, `INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen) VALUES ($1,'default',$2,$3,$4,5432,180000,$5,'T0',now())`, instance.id, clusterID, instance.agent, instance.addr, instance.role)
		require.NoError(t, err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, fact := range []struct {
		instance uuid.UUID
		value    string
	}{{primaryID, "off"}, {standbyID, "on"}} {
		_, err = pool.Exec(ctx, `INSERT INTO object_facts (tenant_id, cluster_id, instance_id, datname, kind, key, labels, value_text, first_seen, last_seen, changed_at) VALUES ('default',$1,$2,'','setting','hot_standby_feedback',$3,$4,$5,$5,$5)`, clusterID, fact.instance, `{"context":"user","source":"configuration file"}`, fact.value, now)
		require.NoError(t, err)
	}
	// An equal setting must not appear as drift, proving the endpoint compares
	// values rather than merely listing every setting observed in the cluster.
	for _, instance := range []uuid.UUID{primaryID, standbyID} {
		_, err = pool.Exec(ctx, `INSERT INTO object_facts (tenant_id, cluster_id, instance_id, datname, kind, key, labels, value_text, first_seen, last_seen, changed_at) VALUES ('default',$1,$2,'','setting','fsync',$3,'on',$4,$4,$4)`, clusterID, instance, `{"context":"user"}`, now)
		require.NoError(t, err)
	}

	r := chi.NewRouter()
	NewAPI(pool).RegisterRoutes(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/clusters/5003/settings-drift", http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var response []driftEntry
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Len(t, response, 1)
	require.Equal(t, "hot_standby_feedback", response[0].Name)
	require.Len(t, response[0].Values, 2)
	byRole := map[string]driftValue{}
	for _, value := range response[0].Values {
		byRole[value.Role] = value
	}
	require.Equal(t, driftValue{InstanceID: primaryID.String(), Role: "primary", Value: "off"}, byRole["primary"])
	require.Equal(t, driftValue{InstanceID: standbyID.String(), Role: "standby", Value: "on"}, byRole["standby"])
}
