//go:build integration

package server

import (
	"context"
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
		_, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS findings (finding_id text NOT NULL, tenant_id text NOT NULL DEFAULT 'default', rule_id text NOT NULL, severity text NOT NULL, state text NOT NULL, scope text NOT NULL, cluster_id bigint, instance_id uuid, datname text NOT NULL DEFAULT '', object_name text NOT NULL DEFAULT '', title text NOT NULL, detail text NOT NULL, remediation text NOT NULL DEFAULT '', evidence jsonb NOT NULL DEFAULT '{}'::jsonb, degraded_reason text, first_seen timestamptz NOT NULL, last_seen timestamptz NOT NULL, resolved_at timestamptz, muted_until timestamptz, mute_reason text, PRIMARY KEY (tenant_id, finding_id))`)
		require.NoError(t, err)
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
