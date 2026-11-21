package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// sqlArgsPool records the SQL and arguments of each query.
type sqlArgsPool struct {
	*mockPool
	sql  []string
	args [][]any
}

func (p *sqlArgsPool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	p.sql = append(p.sql, sql)
	p.args = append(p.args, args)
	return p.mockPool.Query(ctx, sql, args...)
}

func logsRequest(t *testing.T, pool dbPool, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	(&API{pool: pool}).RegisterRoutes(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, http.NoBody))
	return rec
}

// PGL-025
func TestPGL025LogsParameters(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	for name, query := range map[string]string{
		"limit 501":     "limit=501",
		"limit 0":       "limit=0",
		"bad class":     "class=error.statement,nope",
		"bad severity":  "severity=LOUD",
		"bad from":      "from=yesterday",
		"bad before":    "before=soon",
		"empty range":   "from=2026-11-21T10:00:00Z&to=2026-11-21T09:00:00Z",
		"no instance":   "",
		"bad instance":  "instance_id=x",
		"classes range": "classes",
	} {
		path := "/api/v1/logs?instance_id=" + id + "&" + query
		switch name {
		case "no instance":
			path = "/api/v1/logs"
		case "bad instance":
			path = "/api/v1/logs?" + query
		case "classes range":
			path = "/api/v1/logs/classes?instance_id=" + id + "&from=2026-11-21T10:00:00Z&to=2026-11-21T10:00:00Z"
		}
		rec := logsRequest(t, &mockPool{}, path)
		require.Equal(t, http.StatusBadRequest, rec.Code, name)
	}
	rec := logsRequest(t, &mockPool{queryResults: []*mockRows{{}}}, "/api/v1/logs?instance_id="+id+"&limit=500&class=lock.deadlock&severity=ERROR")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `[]`, rec.Body.String())
}

// PGL-026
func TestPGL026TextFilterIsBound(t *testing.T) {
	t.Parallel()
	q := "%' OR 1=1 --"
	pool := &sqlArgsPool{mockPool: &mockPool{queryResults: []*mockRows{{}}}}
	rec := logsRequest(t, pool, "/api/v1/logs?instance_id="+uuid.NewString()+"&q="+url.QueryEscape(q)+"&database=app")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `[]`, rec.Body.String())
	require.Len(t, pool.sql, 1)
	require.NotContains(t, pool.sql[0], "OR 1=1")
	require.Contains(t, pool.sql[0], "position(lower($5) in lower(message)) > 0")
	require.Contains(t, pool.args[0], q)
	require.True(t, strings.HasSuffix(strings.TrimSpace(pool.sql[0]), "LIMIT $6"))
}
