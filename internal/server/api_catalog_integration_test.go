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

// INT-CAT-003: the extensions and spatial columns APIs type their facts: a
// column declared without an SRID has a null srid, and an unreadable
// spatial_ref_sys a null srid_known.
func TestINTCAT003_ExtensionsAndSpatialColumns(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	instanceID := uuid.New()
	now := time.Now().UTC().Truncate(time.Microsecond)
	insert := func(kind, key, value, labels string) {
		t.Helper()
		_, err := pool.Exec(ctx, `INSERT INTO object_facts (tenant_id, cluster_id, instance_id, datname, kind, key, labels, value_text, first_seen, last_seen, changed_at) VALUES ('default',1,$1,'gis',$2,$3,$4,$5,$6,$6,$6)`, instanceID, kind, key, labels, value, now)
		require.NoError(t, err)
	}
	insert("extension", "postgis", "3.5.2", `{"schema":"public","default_version":"3.6.4","upgrade":"available"}`)
	insert("spatial_column", "public.parcels.geom", "geometry", `{"schemaname":"public","relname":"parcels","column":"geom","subtype":"MultiPolygon","srid":"3857","srid_known":"true","index_method":"gist","rows":"250000"}`)
	insert("spatial_column", "public.notes.g", "geometry", `{"schemaname":"public","relname":"notes","column":"g","subtype":"Geometry","srid":"","srid_known":"unknown","index_method":"","rows":"0"}`)

	r := chi.NewRouter()
	NewAPI(pool).RegisterRoutes(r)
	get := func(path string, out any) {
		t.Helper()
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+instanceID.String()+path, http.NoBody))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), out))
	}

	var extensions extensionsAPIResponse
	get("/extensions", &extensions)
	require.Len(t, extensions.Extensions, 1)
	require.Equal(t, extensionAPIEntry{
		Datname: "gis", Name: "postgis", Schema: "public", Version: "3.5.2", DefaultVersion: "3.6.4", Upgrade: "available",
		FirstSeen: now, LastSeen: now, ChangedAt: now,
	}, normalizeExtensionTimes(extensions.Extensions[0]))

	var raw map[string]any
	get("/spatial-columns", &raw)
	columns := raw["columns"].([]any)
	require.Len(t, columns, 2)
	notes, parcels := columns[0].(map[string]any), columns[1].(map[string]any)
	require.Nil(t, notes["srid"])
	require.Nil(t, notes["srid_known"])
	require.Equal(t, "", notes["index_method"])
	require.Equal(t, float64(3857), parcels["srid"])
	require.Equal(t, true, parcels["srid_known"])
	require.Equal(t, float64(250000), parcels["rows"])
}

func normalizeExtensionTimes(e extensionAPIEntry) extensionAPIEntry {
	e.FirstSeen, e.LastSeen, e.ChangedAt = e.FirstSeen.UTC(), e.LastSeen.UTC(), e.ChangedAt.UTC()
	return e
}

// INT-CAT-004: the sequences API keeps bigint bounds exact as strings and
// leaves the value fields null for a sequence whose value is hidden.
func TestINTCAT004_Sequences(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	instanceID := uuid.New()
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, fact := range []struct{ key, state, labels string }{
		{"public.wide", "used", `{"schemaname":"public","sequence":"wide","data_type":"bigint","start":"1","increment":"1","min":"1","max":"9223372036854775807","cycle":"false","table_schema":"public","table":"narrow","column":"id","column_type":"integer","link":"default","columns":"1","limit":"2147483647","limit_source":"column","exhausts":"true","last_value":"2000000000","used_ratio":"0.931323","remaining":"147483647"}`},
		{"public.secret", "hidden", `{"schemaname":"public","sequence":"secret","data_type":"bigint","start":"1","increment":"1","min":"1","max":"9223372036854775807","cycle":"false","table_schema":"","table":"","column":"","column_type":"","link":"","columns":"0","limit":"9223372036854775807","limit_source":"sequence","exhausts":"true","last_value":"","used_ratio":"","remaining":""}`},
	} {
		_, err := pool.Exec(ctx, `INSERT INTO object_facts (tenant_id, cluster_id, instance_id, datname, kind, key, labels, value_text, first_seen, last_seen, changed_at) VALUES ('default',1,$1,'app','sequence',$2,$3,$4,$5,$5,$5)`, instanceID, fact.key, fact.labels, fact.state, now)
		require.NoError(t, err)
	}
	r := chi.NewRouter()
	NewAPI(pool).RegisterRoutes(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+instanceID.String()+"/sequences", http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var raw map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
	sequences := raw["sequences"].([]any)
	require.Len(t, sequences, 2)
	secret, wide := sequences[0].(map[string]any), sequences[1].(map[string]any)
	require.Equal(t, "hidden", secret["state"])
	require.Nil(t, secret["last_value"])
	require.Nil(t, secret["used_ratio"])
	require.Nil(t, secret["remaining"])
	require.Equal(t, "9223372036854775807", secret["max"])
	require.Equal(t, "2000000000", wide["last_value"])
	require.Equal(t, 0.931323, wide["used_ratio"])
	require.Equal(t, "column", wide["limit_source"])
	require.Equal(t, true, wide["exhausts"])
	require.Equal(t, float64(1), wide["columns"])
	require.Empty(t, raw["truncated_databases"])
}

// INT-CAT-005: the partitioned tables API types its facts: no default
// partition and a table never analyzed are null, invalid indexes a list.
func TestINTCAT005_PartitionedTables(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	instanceID := uuid.New()
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, fact := range []struct{ key, strategy, labels string }{
		{"public.events", "range", `{"schemaname":"public","relname":"events","strategy":"range","partition_key":"RANGE (at)","partitions":"3","direct_partitions":"3","depth":"1","bytes":"819200","rows":"3001","default_partition":"public.events_def","default_schema":"public","default_relname":"events_def","default_rows":"1","invalid_indexes":"ev_uq,ev_b","invalid_unique_indexes":"1","analyzed_at":"2026-10-01T00:00:00Z","covered_until":"2027-07-01T00:00:00Z"}`},
		{"public.logs", "list", `{"schemaname":"public","relname":"logs","strategy":"list","partition_key":"LIST (kind)","partitions":"2","direct_partitions":"2","depth":"2","bytes":"0","rows":"0","default_partition":"","default_schema":"","default_relname":"","default_rows":"","invalid_indexes":"","invalid_unique_indexes":"0","analyzed_at":"","covered_until":""}`},
	} {
		_, err := pool.Exec(ctx, `INSERT INTO object_facts (tenant_id, cluster_id, instance_id, datname, kind, key, labels, value_text, first_seen, last_seen, changed_at) VALUES ('default',1,$1,'app','partitioned_table',$2,$3,$4,$5,$5,$5)`, instanceID, fact.key, fact.labels, fact.strategy, now)
		require.NoError(t, err)
	}
	r := chi.NewRouter()
	NewAPI(pool).RegisterRoutes(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+instanceID.String()+"/partitioned-tables", http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var raw map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
	tables := raw["tables"].([]any)
	require.Len(t, tables, 2)
	events, logs := tables[0].(map[string]any), tables[1].(map[string]any)
	require.Equal(t, "public.events_def", events["default_partition"])
	require.Equal(t, float64(1), events["default_rows"])
	require.Equal(t, []any{"ev_uq", "ev_b"}, events["invalid_indexes"])
	require.Equal(t, "2026-10-01T00:00:00Z", events["analyzed_at"])
	require.Equal(t, "2027-07-01T00:00:00Z", events["covered_until"])
	require.Equal(t, float64(819200), events["bytes"])
	require.Nil(t, logs["default_partition"])
	require.Nil(t, logs["default_rows"])
	require.Nil(t, logs["analyzed_at"])
	require.Nil(t, logs["covered_until"])
	require.Equal(t, []any{}, logs["invalid_indexes"])
	require.Equal(t, float64(2), logs["depth"])
}
