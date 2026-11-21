//go:build integration

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

// ensureFindingTables creates the findings table and the query_texts table its
// list joins, as the migrations shape them, in a database the tests share.
func ensureFindingTables(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `CREATE TABLE IF NOT EXISTS findings (finding_id text NOT NULL, tenant_id text NOT NULL DEFAULT 'default', rule_id text NOT NULL, severity text NOT NULL, state text NOT NULL, scope text NOT NULL, cluster_id bigint, instance_id uuid, datname text NOT NULL DEFAULT '', object_name text NOT NULL DEFAULT '', title text NOT NULL, detail text NOT NULL, remediation text NOT NULL DEFAULT '', evidence jsonb NOT NULL DEFAULT '{}'::jsonb, degraded_reason text, first_seen timestamptz NOT NULL, last_seen timestamptz NOT NULL, resolved_at timestamptz, muted_until timestamptz, mute_reason text, PRIMARY KEY (tenant_id, finding_id));
		CREATE TABLE IF NOT EXISTS query_texts (tenant_id text NOT NULL DEFAULT 'default', cluster_id bigint NOT NULL, datname text NOT NULL, queryid bigint NOT NULL, pg_major int NOT NULL, query_text text NOT NULL, first_seen timestamptz NOT NULL DEFAULT now(), last_seen timestamptz NOT NULL DEFAULT now(), PRIMARY KEY (tenant_id, cluster_id, datname, queryid, pg_major))`)
	require.NoError(t, err)
}

func chiRouterForFindings(pool *pgxpool.Pool) *chi.Mux {
	r := chi.NewRouter()
	NewAPI(pool).RegisterRoutes(r)
	return r
}

func TestINTADV006_FindingsMuteLifecycle(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		pool := pg.Pool(t, "app", pgtest.RoleSuperuser)
		ensureFindingTables(t, pool)
		var err error
		id := fmt.Sprintf("integration.rule/%d", time.Now().UnixNano())
		now := time.Now().UTC()
		_, err = pool.Exec(ctx, `INSERT INTO findings(finding_id,rule_id,severity,state,scope,title,detail,evidence,first_seen,last_seen) VALUES ($1,'integration.rule','critical','open','instance','test finding','test detail','{"source":"integration"}',$2,$2)`, id, now)
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM findings WHERE finding_id=$1`, id) })
		r := chiRouterForFindings(pool)

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/findings", nil))
		require.Equal(t, http.StatusOK, rec.Code)
		require.Contains(t, rec.Body.String(), id)

		until := now.Add(time.Hour).Format(time.RFC3339)
		rec = httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/findings/"+id+"/mute", strings.NewReader(`{"reason":"accepted","until":"`+until+`"}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/findings", nil))
		require.NotContains(t, rec.Body.String(), id)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/findings?state=muted", nil))
		require.Equal(t, http.StatusOK, rec.Code)
		require.Contains(t, rec.Body.String(), id)

		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/findings/"+id+"/mute", nil))
		require.Equal(t, http.StatusNoContent, rec.Code)
		var state string
		require.NoError(t, pool.QueryRow(ctx, `SELECT state FROM findings WHERE finding_id=$1`, id).Scan(&state))
		require.Equal(t, "open", state)
	})
}

// TestINTADV007_FindingsListActiveBeforeInactive: a limit cuts muted and
// resolved findings before an open one, whatever their severity.
func TestINTADV007_FindingsListActiveBeforeInactive(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		pool := pg.Pool(t, "app", pgtest.RoleSuperuser)
		ensureFindingTables(t, pool)
		var err error
		rule := fmt.Sprintf("integration.order%d", time.Now().UnixNano())
		now := time.Now().UTC()
		for _, f := range []struct{ id, severity, state string }{
			{rule + "/resolved", "critical", "resolved"},
			{rule + "/muted", "critical", "muted"},
			{rule + "/open", "info", "open"},
		} {
			_, err = pool.Exec(ctx, `INSERT INTO findings(finding_id,rule_id,severity,state,scope,title,detail,first_seen,last_seen) VALUES ($1,$2,$3,$4,'instance','t','d',$5,$5)`, f.id, rule, f.severity, f.state, now)
			require.NoError(t, err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM findings WHERE rule_id=$1`, rule) })

		rec := httptest.NewRecorder()
		chiRouterForFindings(pool).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/findings?state=all&limit=1&rule_id="+rule, nil))
		require.Equal(t, http.StatusOK, rec.Code)
		require.Contains(t, rec.Body.String(), rule+"/open")
		require.NotContains(t, rec.Body.String(), rule+"/resolved")
	})
}

// TestINTADV008_QueryFindingCarriesItsStatementText: a query finding lists the
// latest text of its statement; other findings, a statement without text and a
// malformed queryid carry none and do not fail the list.
func TestINTADV008_QueryFindingCarriesItsStatementText(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		pool := pg.Pool(t, "app", pgtest.RoleSuperuser)
		ensureFindingTables(t, pool)
		rule := fmt.Sprintf("integration.text%d", time.Now().UnixNano())
		cluster := time.Now().UnixNano()
		now := time.Now().UTC()
		_, err := pool.Exec(ctx, `INSERT INTO query_texts (cluster_id, datname, queryid, pg_major, query_text, last_seen) VALUES
			($1, 'shop', 9223372036854775807, 16, 'SELECT old', $2::timestamptz - interval '1 day'),
			($1, 'shop', 9223372036854775807, 17, 'SELECT * FROM orders WHERE id = $1', $2),
			($1, 'other', 9223372036854775807, 17, 'SELECT wrong database', $2)`, cluster, now)
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM query_texts WHERE cluster_id=$1`, cluster) })
		for _, f := range []struct{ id, object string }{
			{rule + "/q", "queryid:9223372036854775807"},
			{rule + "/missing", "queryid:-5"},
			{rule + "/overflow", "queryid:99999999999999999999"},
			{rule + "/table", "public.orders"},
		} {
			_, err = pool.Exec(ctx, `INSERT INTO findings(finding_id,rule_id,severity,state,scope,cluster_id,datname,object_name,title,detail,first_seen,last_seen) VALUES ($1,$2,'critical','open','instance',$3,'shop',$4,'t','d',$5,$5)`, f.id, rule, cluster, f.object, now)
			require.NoError(t, err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM findings WHERE rule_id=$1`, rule) })

		rec := httptest.NewRecorder()
		chiRouterForFindings(pool).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/findings?rule_id="+rule, nil))
		require.Equal(t, http.StatusOK, rec.Code)
		var body []findingJSON
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		texts := map[string]*string{}
		for _, f := range body {
			texts[f.FindingID] = f.QueryText
		}
		require.Len(t, texts, 4)
		require.NotNil(t, texts[rule+"/q"])
		require.Equal(t, "SELECT * FROM orders WHERE id = $1", *texts[rule+"/q"])
		require.Nil(t, texts[rule+"/missing"])
		require.Nil(t, texts[rule+"/overflow"])
		require.Nil(t, texts[rule+"/table"])
	})
}
