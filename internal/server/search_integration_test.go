//go:build integration

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// seedSearchInstance seeds an instance and returns it with its cluster. The
// query texts and facts it gets are removed with it.
func seedSearchInstance(t *testing.T, pool *pgxpool.Pool) (uuid.UUID, int64) {
	t.Helper()
	instance := seedWorkloadInstance(t, pool)
	var cluster int64
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT cluster_id FROM instances WHERE instance_id=$1`, instance).Scan(&cluster))
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM query_texts WHERE cluster_id=$1`, cluster)
		_, _ = pool.Exec(ctx, `DELETE FROM object_facts WHERE instance_id=$1`, instance)
	})
	return instance, cluster
}

// seedSearchStatement stores one statement sample of the window and its text.
func seedSearchStatement(t *testing.T, pool *pgxpool.Pool, instance uuid.UUID, cluster, queryid int64, calls float64, text string, ts time.Time) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO metrics_statements (ts, cluster_id, instance_id, datname, queryid, calls_rate, exec_time_rate_ms, sample_seconds)
		VALUES ($1, $2, $3, 'app', $4, $5, $5, 60)`, ts, cluster, instance, queryid, calls)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO query_texts (tenant_id, cluster_id, datname, queryid, pg_major, query_text, first_seen, last_seen)
		VALUES ('default', $1, 'app', $2, 17, $3, now(), now())`, cluster, queryid, text)
	require.NoError(t, err)
}

func searchGet(t *testing.T, pool *pgxpool.Pool, path string) (int, []byte) {
	t.Helper()
	r := chi.NewRouter()
	NewAPI(pool).RegisterRoutes(r)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, http.NoBody))
	return w.Code, w.Body.Bytes()
}

// SRCH-INT-001: q filters the statements before they are ranked and cut to
// limit, so a statement outside the top 5 by calls is found; a '%' in the
// term matches only itself.
func TestSRCHINT001(t *testing.T) {
	pool := getSharedPool(t)
	instance, cluster := seedSearchInstance(t, pool)
	ts := time.Now().UTC().Add(-10 * time.Minute)
	for i := 1; i <= 30; i++ {
		seedSearchStatement(t, pool, instance, cluster, int64(i), float64(i), fmt.Sprintf("SELECT * FROM table_%02d WHERE id = $1", i), ts)
	}
	// The fewest calls of all: last by calls.
	seedSearchStatement(t, pool, instance, cluster, 101, 0.01, "SELECT qty FROM ORDER_ITEMS_X WHERE order_id = $1", ts)
	seedSearchStatement(t, pool, instance, cluster, 102, 0.02, "SELECT * FROM discounts WHERE rate = '100%'", ts)
	seedSearchStatement(t, pool, instance, cluster, 103, 0.03, "SELECT * FROM events LIMIT 1000", ts)
	seedSearchStatement(t, pool, instance, cluster, 104, 0.04, `COPY imports FROM 'C:\temp\rows.csv'`, ts)
	seedSearchStatement(t, pool, instance, cluster, 105, 0.05, "SELECT * FROM temp_rows", ts)

	statements := func(query string) statementsResp {
		t.Helper()
		code, body := searchGet(t, pool, "/api/v1/statements?instance_id="+instance.String()+"&limit=5&order_by=calls"+query)
		require.Equal(t, http.StatusOK, code, string(body))
		var resp statementsResp
		require.NoError(t, json.Unmarshal(body, &resp))
		return resp
	}

	top := statements("")
	require.Len(t, top.Statements, 5)
	for _, s := range top.Statements {
		require.NotContains(t, s.QueryText, "ORDER_ITEMS_X", "not in the top 5 by calls")
	}

	found := statements("&q=" + url.QueryEscape("order_items_x"))
	require.Len(t, found.Statements, 1)
	require.Equal(t, "101", found.Statements[0].QueryID)
	require.Equal(t, top.Truncated, found.Truncated, "truncated keeps the agent's meaning")
	require.Nil(t, top.HasMore, "has_more is sent only with q")
	require.NotNil(t, found.HasMore)
	require.False(t, *found.HasMore)

	percent := statements("&q=" + url.QueryEscape("100%"))
	require.Len(t, percent.Statements, 1, "100% matches the literal only, not 1000")
	require.Equal(t, "102", percent.Statements[0].QueryID)

	// queryid and database match too; ranking and limit still apply after.
	require.Equal(t, "101", statements("&q=101").Statements[0].QueryID)
	byDatabase := statements("&q=APP")
	require.Len(t, byDatabase.Statements, 5)
	require.Equal(t, top.Statements, byDatabase.Statements)
	// table_1 matches table_10..table_19: ranked by calls, cut to 5.
	ranked := statements("&q=table_1")
	require.Len(t, ranked.Statements, 5)
	require.Equal(t, "19", ranked.Statements[0].QueryID)
	require.Equal(t, "15", ranked.Statements[4].QueryID)
	require.True(t, *ranked.HasMore, "10 match, 5 returned")
	code, body := searchGet(t, pool, "/api/v1/statements?instance_id="+instance.String()+"&limit=10&order_by=calls&q=table_1")
	require.Equal(t, http.StatusOK, code, string(body))
	var exact statementsResp
	require.NoError(t, json.Unmarshal(body, &exact))
	require.Len(t, exact.Statements, 10)
	require.False(t, *exact.HasMore, "10 match, 10 returned")
	// A backslash matches itself: \temp is not the escape of 't'.
	backslash := statements("&q=" + url.QueryEscape(`\temp`))
	require.Len(t, backslash.Statements, 1)
	require.Equal(t, "104", backslash.Statements[0].QueryID)
	require.Empty(t, statements("&q=FROM_table").Statements, "'_' matches only itself, not the space")
}

// SRCH-INT-002: q reaches a string found only in a resolved finding's
// evidence when the state asked for includes it, and not with the default
// state; active findings still come first.
func TestSRCHINT002(t *testing.T) {
	pool := getSharedPool(t)
	ctx := context.Background()
	rule := fmt.Sprintf("srch.int%d", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM findings WHERE rule_id LIKE $1`, rule+"%") })
	now := time.Now().UTC()
	insert := func(id, state, severity, evidence string, lastSeen time.Time) {
		t.Helper()
		_, err := pool.Exec(ctx, `INSERT INTO findings (finding_id, rule_id, severity, state, scope, title, detail, evidence, first_seen, last_seen)
			VALUES ($1, $2, $3, $4, 'instance', 'Slow statement', 'mean latency above threshold', $5::jsonb, $6, $6)`,
			id, rule, severity, state, evidence, lastSeen)
		require.NoError(t, err)
	}
	states := []string{"open", "degraded", "muted", "resolved"}
	for i := range 120 {
		insert(fmt.Sprintf("%s/%03d", rule, i), states[i%4], "critical", fmt.Sprintf(`{"queryid": %d}`, i), now.Add(-time.Duration(i)*time.Minute))
	}
	insert(rule+"/needle", "resolved", "critical", `{"note": "seen on NEEDLE_QX_42 only"}`, now)
	insert(rule+"/needle-open", "open", "info", `{"note": "needle_qx_42 again"}`, now.Add(-time.Hour))

	findings := func(query string) []findingJSON {
		t.Helper()
		code, body := searchGet(t, pool, "/api/v1/findings?rule_id="+rule+query)
		require.Equal(t, http.StatusOK, code, string(body))
		var out []findingJSON
		require.NoError(t, json.Unmarshal(body, &out))
		return out
	}
	ids := func(list []findingJSON) []string {
		out := make([]string, 0, len(list))
		for _, f := range list {
			out = append(out, f.FindingID)
		}
		return out
	}

	require.Equal(t, []string{rule + "/needle"}, ids(findings("&state=resolved&q=needle_qx_42")))
	require.Equal(t, []string{rule + "/needle-open"}, ids(findings("&q=needle_qx_42")), "default state: open and degraded only")
	// state=all: the open info finding comes before the resolved critical one.
	require.Equal(t, []string{rule + "/needle-open", rule + "/needle"}, ids(findings("&state=all&q=NEEDLE_QX_42")))
	// The search applies before the limit: the default limit of 100 would cut
	// the needle out of 122 findings ordered by recency within their state.
	require.Len(t, findings("&state=all"), 100)
	require.Len(t, findings("&state=all&q=%22queryid%22%3A%201"), 31, "queryid 1, 10..19 and 100..119")
	// The texts match too.
	require.Len(t, findings("&state=all&q=latency%20above"), 100)
}

// seedSearchFact stores one fact of the instance, seen now.
func seedSearchFact(t *testing.T, pool *pgxpool.Pool, instance uuid.UUID, cluster int64, datname, kind, key string, labels map[string]string, text string, value any) {
	t.Helper()
	labelJSON, err := json.Marshal(labels)
	require.NoError(t, err)
	var valueJSON []byte
	if value != nil {
		valueJSON, err = json.Marshal(value)
		require.NoError(t, err)
	}
	_, err = pool.Exec(context.Background(), `INSERT INTO object_facts (tenant_id, cluster_id, instance_id, datname, kind, key, labels, value_text, value_json, first_seen, last_seen, changed_at)
		VALUES ('default', $1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, now() - interval '1 hour', now(), now())`,
		cluster, instance, datname, kind, key, labelJSON, text, valueJSON)
	require.NoError(t, err)
}

func searchInstance(t *testing.T, pool *pgxpool.Pool, instance uuid.UUID, query string) searchResponse {
	t.Helper()
	code, body := searchGet(t, pool, "/api/v1/instances/"+instance.String()+"/search?"+query)
	require.Equal(t, http.StatusOK, code, string(body))
	var resp searchResponse
	require.NoError(t, json.Unmarshal(body, &resp))
	return resp
}

func searchGroupOf(t *testing.T, resp searchResponse, kind string) searchGroup {
	t.Helper()
	for _, g := range resp.Groups {
		if g.Kind == kind {
			return g
		}
	}
	t.Fatalf("no %s group in %+v", kind, resp)
	return searchGroup{}
}

// SRCH-INT-003: searchInstance finds an index by a substring of its
// definition, a table and a column by name and a statement by text, each
// group counting all its matches; over 20 000 query texts it answers within
// its time budget.
func TestSRCHINT003(t *testing.T) {
	pool := getSharedPool(t)
	ctx := context.Background()
	instance, cluster := seedSearchInstance(t, pool)
	ts := time.Now().UTC().Add(-10 * time.Minute)

	seedSearchStatement(t, pool, instance, cluster, 7001, 5, "UPDATE invoice_zq SET paid = true WHERE id = $1", ts)
	seedSearchStatement(t, pool, instance, cluster, 7002, 9, "SELECT * FROM invoice_zq WHERE customer = $1", ts)
	seedSearchFact(t, pool, instance, cluster, "app", "index_def", "billing.invoice_zq_customer_idx",
		map[string]string{"schemaname": "billing", "relname": "invoice_zq", "indexrelname": "invoice_zq_customer_idx"},
		"CREATE INDEX invoice_zq_customer_idx ON billing.invoice_zq USING btree (customer_ref_kw)", nil)
	seedSearchFact(t, pool, instance, cluster, "app", "index_def", "billing.other_pkey",
		map[string]string{"schemaname": "billing", "relname": "other", "indexrelname": "other_pkey"},
		"CREATE UNIQUE INDEX other_pkey ON billing.other USING btree (id)", nil)
	columns := []map[string]any{{"name": "id", "type": "bigint"}, {"name": "customer_ref_kw", "type": "text"}}
	for i := range 60 {
		columns = append(columns, map[string]any{"name": fmt.Sprintf("extra_kw_%02d", i), "type": "integer"})
	}
	seedSearchFact(t, pool, instance, cluster, "app", "table_columns", `billing.invoice_zq`,
		map[string]string{"schemaname": "billing", "relname": "invoice_zq", "relkind": "r"}, "", columns)
	seedSearchFact(t, pool, instance, cluster, "app", "table_columns", `billing.other`,
		map[string]string{"schemaname": "billing", "relname": "other", "relkind": "r"}, "", []map[string]any{{"name": "id", "type": "bigint"}})
	seedSearchFact(t, pool, instance, cluster, "odoo", "table_columns", `public.res_partner`,
		map[string]string{"schemaname": "public", "relname": "res_partner", "relkind": "r"}, "",
		[]map[string]any{{"name": "name", "type": "character varying"}, {"name": "email", "type": "character varying"}})

	// An index by a substring of its definition, which is not its name.
	resp := searchInstance(t, pool, instance, "q=customer_ref&kinds=indexes")
	require.Equal(t, "customer_ref", resp.Q)
	require.Len(t, resp.Groups, 1)
	indexes := resp.Groups[0]
	require.Equal(t, int64(1), indexes.Total)
	require.False(t, indexes.Truncated)
	require.Equal(t, "app.billing.invoice_zq_customer_idx", indexes.Hits[0].Key)
	require.Equal(t, "/indexes?instance="+instance.String()+"&tab=indexes&focus=app.billing.invoice_zq_customer_idx", indexes.Hits[0].Link)

	// Every kind for the table's name, in group order.
	resp = searchInstance(t, pool, instance, "q=invoice_ZQ")
	require.Equal(t, []string{"statements", "indexes", "tables", "columns"}, []string{resp.Groups[0].Kind, resp.Groups[1].Kind, resp.Groups[2].Kind, resp.Groups[3].Kind})
	statements := searchGroupOf(t, resp, "statements")
	require.Equal(t, int64(2), statements.Total)
	require.Equal(t, "7002", statements.Hits[0].Title, "ranked by execution time in the window")
	require.Equal(t, "/queries?instance="+instance.String()+"&q=invoice_ZQ", statements.Hits[0].Link)
	require.Contains(t, statements.Hits[0].Snippet, searchSnippetPart{Text: "invoice_zq", Match: true})
	require.Equal(t, int64(1), searchGroupOf(t, resp, "indexes").Total)
	tables := searchGroupOf(t, resp, "tables")
	require.Equal(t, int64(1), tables.Total)
	require.Equal(t, "billing.invoice_zq", tables.Hits[0].Title)
	require.Equal(t, "/indexes?instance="+instance.String()+"&tab=dead-rows&focus=app.billing.invoice_zq", tables.Hits[0].Link)
	require.Equal(t, int64(62), searchGroupOf(t, resp, "columns").Total, "every column of the table")

	// A table's name finds its columns; the snippet is the qualified name.
	resp = searchInstance(t, pool, instance, "q=res_partner&kinds=columns")
	cols := resp.Groups[0]
	require.Equal(t, int64(2), cols.Total)
	require.Equal(t, []string{"public.res_partner.email", "public.res_partner.name"}, []string{cols.Hits[0].Title, cols.Hits[1].Title})
	require.Equal(t, []searchSnippetPart{{Text: "public.", Match: false}, {Text: "res_partner", Match: true}, {Text: ".name", Match: false}}, cols.Hits[1].Snippet)
	require.Equal(t, "/indexes?instance="+instance.String()+"&tab=dead-rows&focus=odoo.public.res_partner", cols.Hits[1].Link)
	// A type matches too.
	require.Equal(t, int64(2), searchInstance(t, pool, instance, "q=BIGINT&kinds=columns").Groups[0].Total)

	// A range the caller names goes into the statement link as the page reads it.
	from, to := ts.Add(-time.Hour), ts.Add(time.Hour)
	resp = searchInstance(t, pool, instance, "q=invoice_zq&kinds=statements&from="+from.Format(time.RFC3339Nano)+"&to="+to.Format(time.RFC3339Nano))
	require.Equal(t, "/queries?instance="+instance.String()+"&q=invoice_zq&from="+url.QueryEscape(from.Format(time.RFC3339Nano))+
		"&to="+url.QueryEscape(to.Format(time.RFC3339Nano)), resp.Groups[0].Hits[0].Link)

	// A column by name; the total counts past the 50 hits returned.
	resp = searchInstance(t, pool, instance, "q=_kw&kinds=columns")
	cols = resp.Groups[0]
	require.Equal(t, int64(61), cols.Total)
	require.Len(t, cols.Hits, 50)
	require.True(t, cols.Truncated)
	require.Equal(t, "billing.invoice_zq.customer_ref_kw", cols.Hits[0].Title)
	require.Equal(t, "text · app", cols.Hits[0].Subtitle)
	resp = searchInstance(t, pool, instance, "q=_kw&kinds=columns&limit=5")
	require.Len(t, resp.Groups[0].Hits, 5)
	require.Equal(t, int64(61), resp.Groups[0].Total)
	require.Empty(t, searchInstance(t, pool, instance, "q=_kw&kinds=columns&database=other").Groups[0].Hits)

	// Outside the window nothing is found.
	old := time.Now().UTC().Add(-72 * time.Hour)
	resp = searchInstance(t, pool, instance, "q=invoice_zq&from="+old.Format(time.RFC3339)+"&to="+old.Add(time.Hour).Format(time.RFC3339))
	for _, g := range resp.Groups {
		require.Zero(t, g.Total, g.Kind)
	}

	// 20 000 statements with their texts, two samples each, in the window.
	_, err := pool.Exec(ctx, `INSERT INTO metrics_statements (ts, cluster_id, instance_id, datname, queryid, calls_rate, exec_time_rate_ms, sample_seconds)
		SELECT $3::timestamptz - (s * interval '5 minutes'), $1, $2, 'app', 100000 + q, q % 97, q % 13, 60
		FROM generate_series(1, 20000) AS q, generate_series(1, 2) AS s`, cluster, instance, ts)
	require.NoError(t, err)
	tag, err := pool.Exec(ctx, `INSERT INTO query_texts (tenant_id, cluster_id, datname, queryid, pg_major, query_text, first_seen, last_seen)
		SELECT 'default', $1, 'app', 100000 + q, 17,
		       'SELECT o.id, o.total, c.name FROM orders_' || q || ' o JOIN customers c ON c.id = o.customer_id WHERE o.created_at > $1 AND o.state = $2 ' || repeat('AND o.flag_' || q || ' IS NOT NULL ', 8),
		       now(), now()
		FROM generate_series(1, 20000) AS q`, cluster)
	require.NoError(t, err)
	require.Equal(t, int64(20000), tag.RowsAffected())
	for _, table := range []string{"metrics_statements", "query_texts", "object_facts"} {
		_, err = pool.Exec(ctx, `ANALYZE `+table)
		require.NoError(t, err)
	}

	budget := time.Second
	if raceEnabled {
		budget = 3 * time.Second
	}
	start := time.Now()
	resp = searchInstance(t, pool, instance, "q="+url.QueryEscape("orders_1999 "))
	elapsed := time.Since(start)
	statements = searchGroupOf(t, resp, "statements")
	// orders_1999 and orders_19990..19999.
	require.Equal(t, int64(11), statements.Total)
	require.Less(t, elapsed, budget, "search over 20 000 texts took %s", elapsed)
	t.Logf("SRCH-INT-003: 4 kinds over 20 000 statements and texts answered in %s (budget %s, race %v)", elapsed, budget, raceEnabled)

	start = time.Now()
	resp = searchInstance(t, pool, instance, "q=no_such_text_anywhere")
	t.Logf("SRCH-INT-003: a term matching nothing answered in %s", time.Since(start))
	require.Zero(t, searchGroupOf(t, resp, "statements").Total)

	// The plan of the statements query, for the record.
	rows, err := pool.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) "+searchKindQueries[searchKindStatements],
		instance, time.Now().Add(-searchDefaultWindow), time.Now(), "%orders_1999%", searchKindLimit, nil)
	require.NoError(t, err)
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		plan = append(plan, line)
	}
	require.NoError(t, rows.Err())
	t.Log(strings.Join(plan, "\n"))
}
