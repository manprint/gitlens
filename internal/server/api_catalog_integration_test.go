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

// INT-CAT-001: the triggers API lists each database's latest batch only: a
// trigger a truncated scrape did not reach again, and a database that
// stopped reporting hours ago, are left out; a truncated database is named.
func TestINTCAT001_TriggersLatestBatch(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	instanceID := uuid.New()
	now := time.Now().UTC().Truncate(time.Microsecond)
	insert := func(datname, kind, key, value, labels string, seen time.Time) {
		t.Helper()
		_, err := pool.Exec(ctx, `INSERT INTO object_facts (tenant_id, cluster_id, instance_id, datname, kind, key, labels, value_text, first_seen, last_seen, changed_at) VALUES ('default',1,$1,$2,$3,$4,$5,$6,$7,$7,$7)`, instanceID, datname, kind, key, labels, value, seen)
		require.NoError(t, err)
	}
	insert("app", "trigger", "public.orders.audit", "disabled", `{"schemaname":"public","relname":"orders","trigger":"audit","kind":"user","function":"public.audit_orders","timing":"AFTER","events":"INSERT OR UPDATE","level":"ROW","constraint":"","constraint_type":""}`, now)
	insert("app", "trigger", "public.orders.RI_ConstraintTrigger_c_1", "disabled", `{"schemaname":"public","relname":"orders","trigger":"RI_ConstraintTrigger_c_1","kind":"internal","constraint":"orders_customer_fkey","constraint_type":"foreign key"}`, now.Add(-20*time.Second))
	insert("app", "trigger", "public.orders.old", "enabled", `{"schemaname":"public","relname":"orders","trigger":"old","kind":"user"}`, now.Add(-10*time.Minute))
	insert("crm", "trigger", "public.leads.stamp", "enabled", `{"schemaname":"public","relname":"leads","trigger":"stamp","kind":"user"}`, now.Add(-5*time.Minute))
	insert("gone", "trigger", "public.x.t", "disabled", `{"schemaname":"public","relname":"x","trigger":"t","kind":"user"}`, now.Add(-4*time.Hour))
	insert("app", "check_partial", "trigger_inventory", "truncated", `{}`, now)

	r := chi.NewRouter()
	NewAPI(pool).RegisterRoutes(r)
	get := func(url string) triggersAPIResponse {
		t.Helper()
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, http.NoBody))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var response triggersAPIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
		return response
	}

	response := get("/api/v1/instances/" + instanceID.String() + "/triggers")
	var names []string
	for _, trigger := range response.Triggers {
		names = append(names, trigger.Datname+"/"+trigger.Name)
	}
	require.Equal(t, []string{"app/RI_ConstraintTrigger_c_1", "app/audit", "crm/stamp"}, names)
	audit := response.Triggers[1]
	require.Equal(t, "disabled", audit.State)
	require.Equal(t, "user", audit.Kind)
	require.Equal(t, "public.audit_orders", audit.Function)
	require.Equal(t, "INSERT OR UPDATE", audit.Events)
	require.Equal(t, "foreign key", response.Triggers[0].ConstraintType)
	require.Equal(t, []string{"app"}, response.TruncatedDatabases)

	response = get("/api/v1/instances/" + instanceID.String() + "/triggers?datname=crm")
	require.Len(t, response.Triggers, 1)
	require.Equal(t, "stamp", response.Triggers[0].Name)
	require.Empty(t, response.TruncatedDatabases)

	response = get("/api/v1/instances/" + uuid.NewString() + "/triggers")
	require.Empty(t, response.Triggers)
	require.NotNil(t, response.Triggers)
}

// INT-CAT-002: the unindexed foreign keys API lists the latest batch with
// the referencing table's size as a number.
func TestINTCAT002_UnindexedForeignKeys(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	instanceID := uuid.New()
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, fact := range []struct {
		key, columns, labels string
		seen                 time.Time
	}{
		{"public.orders.orders_customer_fkey", "customer_id", `{"schemaname":"public","relname":"orders","constraint":"orders_customer_fkey","referenced":"public.customers","columns_sql":"customer_id","table_bytes":"52428800"}`, now},
		{"public.notes.notes_order_fkey", "order_id", `{"schemaname":"public","relname":"notes","constraint":"notes_order_fkey","referenced":"public.orders","table_bytes":"8192"}`, now.Add(-time.Hour)},
	} {
		_, err := pool.Exec(ctx, `INSERT INTO object_facts (tenant_id, cluster_id, instance_id, datname, kind, key, labels, value_text, first_seen, last_seen, changed_at) VALUES ('default',1,$1,'app','fk_unindexed',$2,$3,$4,$5,$5,$5)`, instanceID, fact.key, fact.labels, fact.columns, fact.seen)
		require.NoError(t, err)
	}
	r := chi.NewRouter()
	NewAPI(pool).RegisterRoutes(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+instanceID.String()+"/unindexed-foreign-keys", http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var response unindexedForeignKeysAPIResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Len(t, response.ForeignKeys, 1)
	fk := response.ForeignKeys[0]
	require.Equal(t, "orders_customer_fkey", fk.Constraint)
	require.Equal(t, "customer_id", fk.Columns)
	require.Equal(t, "public.customers", fk.Referenced)
	require.Equal(t, int64(52428800), fk.TableBytes)
	require.Empty(t, response.TruncatedDatabases)
}
