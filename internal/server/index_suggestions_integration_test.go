//go:build integration

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func suggestionsGet(t *testing.T, pool *pgxpool.Pool, instance uuid.UUID, query string) (indexSuggestionsJSON, time.Duration) {
	t.Helper()
	r := chi.NewRouter()
	NewAPI(pool).RegisterRoutes(r)
	w := httptest.NewRecorder()
	start := time.Now()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+instance.String()+"/index-suggestions"+query, http.NoBody))
	took := time.Since(start)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp indexSuggestionsJSON
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp, took
}

// seedSuggestionStatement stores one statement of database odoo at each of the
// given scrape times, and its text once.
func seedSuggestionStatement(t *testing.T, pool *pgxpool.Pool, instance uuid.UUID, cluster, queryid int64, execRate, callsRate float64, text string, times ...time.Time) {
	t.Helper()
	ctx := context.Background()
	for _, ts := range times {
		_, err := pool.Exec(ctx, `INSERT INTO metrics_statements (ts, cluster_id, instance_id, datname, queryid, calls_rate, exec_time_rate_ms, sample_seconds)
			VALUES ($1, $2, $3, 'odoo', $4, $5, $6, 60)`, ts, cluster, instance, queryid, callsRate, execRate)
		require.NoError(t, err)
	}
	_, err := pool.Exec(ctx, `INSERT INTO query_texts (tenant_id, cluster_id, datname, queryid, pg_major, query_text, first_seen, last_seen)
		VALUES ('default', $1, 'odoo', $2, 17, $3, now(), now()) ON CONFLICT DO NOTHING`, cluster, queryid, text)
	require.NoError(t, err)
}

func seedPartnerTable(t *testing.T, pool *pgxpool.Pool, instance uuid.UUID, cluster int64, ts time.Time) {
	t.Helper()
	columns := []map[string]any{
		{"name": "id", "attnum": 1, "type": "integer", "typlen": 4, "notnull": true, "n_distinct": -1.0, "avg_width": 4},
		{"name": "name", "attnum": 2, "type": "character varying", "typlen": -1, "n_distinct": -0.7, "avg_width": 20},
		{"name": "ref", "attnum": 3, "type": "character varying", "typlen": -1, "n_distinct": -0.9, "avg_width": 12},
		{"name": "x", "attnum": 4, "type": "integer", "typlen": 4, "n_distinct": 50.0, "avg_width": 4},
	}
	seedSearchFact(t, pool, instance, cluster, "odoo", "table_columns", "public.res_partner",
		map[string]string{"schemaname": "public", "relname": "res_partner", "relkind": "r", "reltuples": "50000", "relpages": "900"}, "", columns)
	seedSearchFact(t, pool, instance, cluster, "odoo", "index_def", "public.res_partner_pkey",
		map[string]string{"schemaname": "public", "relname": "res_partner", "indexrelname": "res_partner_pkey"},
		"CREATE UNIQUE INDEX res_partner_pkey ON public.res_partner USING btree (id)", nil)
	seedSearchFact(t, pool, instance, cluster, "odoo", "index_def", "public.res_partner_name_idx",
		map[string]string{"schemaname": "public", "relname": "res_partner", "indexrelname": "res_partner_name_idx"},
		"CREATE INDEX res_partner_name_idx ON public.res_partner USING btree (name)", nil)
	_, err := pool.Exec(context.Background(), `INSERT INTO metrics_tables (ts, cluster_id, instance_id, datname, schemaname, relname,
			n_live_tup, reltuples, n_tup_ins, n_tup_upd, n_tup_del, n_tup_hot_upd)
		VALUES ($1, $2, $3, 'odoo', 'public', 'res_partner', 50000, 50000, 2, 8, 0.5, 6)`, ts, cluster, instance)
	require.NoError(t, err)
}

// IDXE-INT-001: a ground-truth workload. Statement A filters on the unindexed
// ref (40 % of the time), B on the indexed name (30 %), C updates by the
// primary key (30 %): exactly (ref) is suggested, sized by the formula.
func TestIDXEINT001_GroundTruthWorkload(t *testing.T) {
	pool := getSharedPool(t)
	instance, cluster := seedSearchInstance(t, pool)
	now := time.Now().UTC()
	times := []time.Time{now.Add(-30 * time.Minute), now.Add(-20 * time.Minute), now.Add(-10 * time.Minute)}
	seedPartnerTable(t, pool, instance, cluster, now.Add(-5*time.Minute))
	seedSuggestionStatement(t, pool, instance, cluster, 1, 40, 12, `SELECT id, name FROM res_partner WHERE ref = $1`, times...)
	seedSuggestionStatement(t, pool, instance, cluster, 2, 30, 5, `SELECT id FROM res_partner WHERE name = $1`, times...)
	seedSuggestionStatement(t, pool, instance, cluster, 3, 30, 4, `UPDATE res_partner SET x = $1 WHERE id = $2`, times...)

	resp, _ := suggestionsGet(t, pool, instance, "?window=1h&database=odoo")
	require.Equal(t, 3, resp.Coverage.StatementsConsidered)
	require.Equal(t, 3, resp.Coverage.StatementsAnalysed)
	require.Len(t, resp.Suggestions, 1, "only (ref): name and id are indexed")
	s := resp.Suggestions[0]
	require.Equal(t, "res_partner", s.Table)
	require.Len(t, s.Columns, 1)
	require.Equal(t, "ref", s.Columns[0].Name)
	require.InDelta(t, 40, s.WeightPct, 0.5)
	require.InDelta(t, 40, s.ReadCostMsPerS, 0.5)
	require.NotNil(t, s.EstSizeBytes)
	want := 50000 * (12.0 + 12) * 1.15
	require.InDelta(t, want, float64(*s.EstSizeBytes), want*0.01)
	require.InDelta(t, 2, s.Write.InsertRate, 1e-9)
	require.InDelta(t, 8, s.Write.UpdateRate, 1e-9)
	require.InDelta(t, 0.75, s.Write.HotUpdateRatio, 1e-9)
	require.Equal(t, "CREATE INDEX CONCURRENTLY idx_res_partner_ref ON public.res_partner (ref);", s.CreateStatement)
	require.NotContains(t, s.Notes, "writes_unknown")

	empty, _ := suggestionsGet(t, pool, instance, "?window=1h&database=other")
	require.Zero(t, empty.Coverage.StatementsConsidered)
	require.Empty(t, empty.Suggestions)
}

// IDXE-INT-002: a window narrower than the data leaves old statements out.
func TestIDXEINT002_WindowExcludesOldStatements(t *testing.T) {
	pool := getSharedPool(t)
	instance, cluster := seedSearchInstance(t, pool)
	now := time.Now().UTC()
	seedPartnerTable(t, pool, instance, cluster, now.Add(-5*time.Minute))
	seedSuggestionStatement(t, pool, instance, cluster, 1, 40, 12, `SELECT id FROM res_partner WHERE ref = $1`, now.Add(-3*time.Hour), now.Add(-170*time.Minute))
	seedSuggestionStatement(t, pool, instance, cluster, 2, 10, 1, `SELECT id FROM res_partner WHERE x = $1`, now.Add(-10*time.Minute))

	narrow, _ := suggestionsGet(t, pool, instance, "?window=1h")
	require.Equal(t, 1, narrow.Coverage.StatementsConsidered)
	for _, s := range narrow.Suggestions {
		require.NotEqual(t, "ref", s.Columns[0].Name, "the 3 h old statement is outside the window")
	}
	wide, _ := suggestionsGet(t, pool, instance, "?window=6h")
	require.Equal(t, 2, wide.Coverage.StatementsConsidered)
	found := false
	for _, s := range wide.Suggestions {
		found = found || s.Columns[0].Name == "ref"
	}
	require.True(t, found)
}

// IDXE-INT-003: 100 000 statement samples answer within the budget.
func TestIDXEINT003_QueryTimeBudget(t *testing.T) {
	pool := getSharedPool(t)
	instance, cluster := seedSearchInstance(t, pool)
	now := time.Now().UTC()
	seedPartnerTable(t, pool, instance, cluster, now.Add(-5*time.Minute))
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO metrics_statements (ts, cluster_id, instance_id, datname, queryid, calls_rate, exec_time_rate_ms, sample_seconds)
		SELECT $1::timestamptz - make_interval(mins => t), $2, $3, 'odoo', q, 1 + (q % 7), 0.5 + (q % 13), 60
		  FROM generate_series(1, 20) t, generate_series(1, 5000) q`, now, cluster, instance)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO query_texts (tenant_id, cluster_id, datname, queryid, pg_major, query_text, first_seen, last_seen)
		SELECT 'default', $1, 'odoo', q, 17, 'SELECT id FROM res_partner WHERE x = $1 AND ref = ' || q, now(), now() FROM generate_series(1, 5000) q`, cluster)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `ANALYZE metrics_statements`)
	require.NoError(t, err)

	budget := time.Second
	if raceEnabled {
		budget = 3 * time.Second
	}
	resp, took := suggestionsGet(t, pool, instance, "?window=1h")
	require.Equal(t, 5000, resp.Coverage.StatementsConsidered)
	require.NotEmpty(t, resp.Suggestions)
	require.Less(t, took, budget, fmt.Sprintf("took %s", took))
	require.False(t, math.IsNaN(resp.Suggestions[0].WeightPct))
}
