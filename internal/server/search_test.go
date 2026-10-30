package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// SRCH-001: the LIKE wildcards and the escape character are escaped, and the
// term is wrapped for a substring match.
func TestSRCH001(t *testing.T) {
	t.Parallel()
	got, err := searchPattern(`a%b_c\d`)
	require.NoError(t, err)
	require.Equal(t, `%a\%b\_c\\d%`, got)
}

// SRCH-002: the term is trimmed before it is measured and matched.
func TestSRCH002(t *testing.T) {
	t.Parallel()
	got, err := searchPattern(" ab ")
	require.NoError(t, err)
	require.Equal(t, "%ab%", got)
}

// SRCH-003: a term of one character is too short, one of 201 too long; the
// bounds count characters, not bytes.
func TestSRCH003(t *testing.T) {
	t.Parallel()
	_, err := searchPattern("a")
	require.ErrorIs(t, err, ErrSearchTooShort)
	_, err = searchPattern("   a  ")
	require.ErrorIs(t, err, ErrSearchTooShort)
	_, err = searchPattern(strings.Repeat("x", 201))
	require.ErrorIs(t, err, ErrSearchTooLong)
	_, err = searchPattern(strings.Repeat("x", 200))
	require.NoError(t, err)
	_, err = searchPattern(strings.Repeat("é", 200))
	require.NoError(t, err, "200 two-byte characters are 200 characters")
}

// SRCH-004: each endpoint that takes q answers a term out of bounds with 400
// and the same body.
func TestSRCH004(t *testing.T) {
	t.Parallel()
	instance := uuid.NewString()
	for _, path := range []string{
		"/api/v1/statements?instance_id=" + instance + "&q=",
		"/api/v1/findings?q=",
		"/api/v1/instances/" + instance + "/search?q=",
	} {
		for _, q := range []string{"a", " a ", strings.Repeat("y", 201)} {
			r := chi.NewRouter()
			(&API{pool: &mockPool{}}).RegisterRoutes(r)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path+url.QueryEscape(q), http.NoBody))
			require.Equal(t, http.StatusBadRequest, w.Code, "%s%q", path, q)
			var body apiError
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			require.Equal(t, apiError{Error: "invalid q", Detail: "q must be 2–200 characters"}, body, path)
		}
	}
}

// SRCH-005: a snippet is cut around the first match with '…' on each cut side,
// keeps the text's case and marks every match.
func TestSRCH005(t *testing.T) {
	t.Parallel()
	join := func(parts []searchSnippetPart) string {
		var sb strings.Builder
		for _, p := range parts {
			sb.WriteString(p.Text)
		}
		return sb.String()
	}
	length := func(parts []searchSnippetPart) int { return len([]rune(join(parts))) }

	t.Run("match in the middle", func(t *testing.T) {
		text := strings.Repeat("a", 200) + "Orders" + strings.Repeat("b", 200)
		parts := buildSnippet(text, "ORDERS")
		require.True(t, strings.HasPrefix(join(parts), "…a"), join(parts))
		require.True(t, strings.HasSuffix(join(parts), "b…"), join(parts))
		require.LessOrEqual(t, length(parts), searchSnippetLength)
		var marked []string
		for _, p := range parts {
			if p.Match {
				marked = append(marked, p.Text)
			}
		}
		require.Equal(t, []string{"Orders"}, marked, "the text's case is kept")
	})

	t.Run("match at the start", func(t *testing.T) {
		text := "SELECT * FROM orders WHERE " + strings.Repeat("x = 1 AND ", 30)
		parts := buildSnippet(text, "select")
		require.Equal(t, searchSnippetPart{Text: "SELECT", Match: true}, parts[0], "no leading cut mark")
		require.True(t, strings.HasSuffix(join(parts), "…"), join(parts))
		require.LessOrEqual(t, length(parts), searchSnippetLength)
	})

	t.Run("several matches", func(t *testing.T) {
		parts := buildSnippet("ab xAB ab", "ab")
		require.Equal(t, []searchSnippetPart{
			{Text: "ab", Match: true}, {Text: " x", Match: false}, {Text: "AB", Match: true},
			{Text: " ", Match: false}, {Text: "ab", Match: true},
		}, parts)
	})

	t.Run("short text and white space", func(t *testing.T) {
		parts := buildSnippet("SELECT 1\n\t FROM t", "1 from")
		require.Equal(t, []searchSnippetPart{
			{Text: "SELECT ", Match: false}, {Text: "1 FROM", Match: true}, {Text: " t", Match: false},
		}, parts)
	})

	t.Run("no match", func(t *testing.T) {
		parts := buildSnippet(strings.Repeat("z", 300), "queryid")
		require.Len(t, parts, 1)
		require.False(t, parts[0].Match)
		require.True(t, strings.HasSuffix(parts[0].Text, "…"))
		require.Equal(t, searchSnippetLength, length(parts))
	})

	t.Run("multibyte text", func(t *testing.T) {
		text := strings.Repeat("é", 100) + "ÀÉÎõü" + strings.Repeat("ß", 300)
		parts := buildSnippet(text, "àéîÕÜ")
		require.LessOrEqual(t, length(parts), searchSnippetLength)
		require.True(t, strings.HasPrefix(join(parts), "…é"), join(parts))
		require.True(t, strings.HasSuffix(join(parts), "ß…"), join(parts))
		var marked []string
		for _, p := range parts {
			require.True(t, utf8.ValidString(p.Text), "a cut never splits a character")
			if p.Match {
				marked = append(marked, p.Text)
			}
		}
		require.Equal(t, []string{"ÀÉÎõü"}, marked)
	})

	t.Run("match at the end", func(t *testing.T) {
		parts := buildSnippet(strings.Repeat("q", 300)+"tail", "tail")
		require.True(t, strings.HasPrefix(join(parts), "…q"), join(parts))
		require.Equal(t, searchSnippetPart{Text: "tail", Match: true}, parts[len(parts)-1], "no trailing cut mark")
		require.LessOrEqual(t, length(parts), searchSnippetLength)
	})
}

// SRCH-006: kinds defaults to all four in group order, drops repeats, keeps
// group order whatever the request's order, and rejects an unknown kind.
func TestSRCH006(t *testing.T) {
	t.Parallel()
	all := []string{"statements", "indexes", "tables", "columns"}
	got, err := parseSearchKinds("")
	require.NoError(t, err)
	require.Equal(t, all, got)
	got, err = parseSearchKinds("columns, statements,columns")
	require.NoError(t, err)
	require.Equal(t, []string{"statements", "columns"}, got)
	for _, bad := range []string{"views", "statements,", "indexes,,tables", "Statements"} {
		_, err = parseSearchKinds(bad)
		require.Error(t, err, bad)
	}

	r := chi.NewRouter()
	(&API{pool: &mockPool{}}).RegisterRoutes(r)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+uuid.NewString()+"/search?q=ab&kinds=views", http.NoBody))
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "invalid kinds")
}

// SRCH-007: an unknown instance is 404, a bad window or instance id 400.
func TestSRCH007(t *testing.T) {
	t.Parallel()
	pool := &mockPool{queryRowVals: []*mockRow{{vals: []any{false}}}}
	r := chi.NewRouter()
	(&API{pool: pool}).RegisterRoutes(r)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+uuid.NewString()+"/search?q=orders", http.NoBody))
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	require.Zero(t, pool.queryCalls, "no kind is searched for an unknown instance")

	for _, query := range []string{
		"from=2026-10-02T00:00:00Z&to=2026-10-01T00:00:00Z",
		"from=yesterday",
		"to=later",
		"limit=0",
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+uuid.NewString()+"/search?q=orders&"+query, http.NoBody))
		require.Equal(t, http.StatusBadRequest, w.Code, query)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/instances/not-a-uuid/search?q=orders", http.NoBody))
	require.Equal(t, http.StatusBadRequest, w.Code)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+uuid.NewString()+"/search", http.NoBody))
	require.Equal(t, http.StatusBadRequest, w.Code, "searchInstance needs a term")
}

// searchCapturePool records the SQL text and arguments of every query.
type searchCapturePool struct {
	*mockPool
	sqls []string
	args [][]any
}

func (p *searchCapturePool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	p.sqls, p.args = append(p.sqls, sql), append(p.args, args)
	return p.mockPool.Query(ctx, sql, args...)
}

func (p *searchCapturePool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	p.sqls, p.args = append(p.sqls, sql), append(p.args, args)
	return p.mockPool.QueryRow(ctx, sql, args...)
}

// The term reaches the database only as a bound parameter (I-3): a term made
// of SQL never appears in the query text, and its pattern is an argument.
func TestSearchTermIsABoundParameter(t *testing.T) {
	t.Parallel()
	const term = `x'); DROP TABLE findings; --`
	pattern, err := searchPattern(term)
	require.NoError(t, err)
	instance := uuid.NewString()
	for _, path := range []string{
		"/api/v1/statements?instance_id=" + instance + "&limit=5&q=",
		"/api/v1/findings?state=all&q=",
		"/api/v1/instances/" + instance + "/search?q=",
	} {
		pool := &searchCapturePool{mockPool: &mockPool{queryRowVals: []*mockRow{{vals: []any{true}}}}}
		r := chi.NewRouter()
		(&API{pool: pool}).RegisterRoutes(r)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path+url.QueryEscape(term), http.NoBody))
		require.Equal(t, http.StatusOK, w.Code, "%s: %s", path, w.Body.String())
		bound := false
		for i, sql := range pool.sqls {
			require.NotContains(t, sql, "DROP TABLE", path)
			for _, arg := range pool.args[i] {
				if arg == pattern {
					bound = true
				}
			}
		}
		require.True(t, bound, "%s binds the pattern", path)
	}

	// The statement filter's parameter precedes the limit's, which stays last.
	pool := &searchCapturePool{mockPool: &mockPool{queryRowVals: []*mockRow{{vals: []any{false}}}}}
	r := chi.NewRouter()
	(&API{pool: pool}).RegisterRoutes(r)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/statements?instance_id="+instance+"&database=app&limit=5&q=orders", http.NoBody))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	args := pool.args[0]
	require.Equal(t, "%orders%", args[len(args)-2])
	require.Equal(t, 6, args[len(args)-1], "one row past the limit tells has_more")
	require.Contains(t, pool.sqls[0], "FROM agg\n\t\tWHERE (agg.queryid::text ILIKE $4 ESCAPE '\\'")
	require.Contains(t, pool.sqls[0], "LIMIT $5")
}

// Without q the statements query has no search predicate and binds the same
// arguments it always did: instance, database, from, to and limit.
func TestSearchAbsentKeepsTheStatementsQuery(t *testing.T) {
	t.Parallel()
	instance := uuid.NewString()
	for _, query := range []string{"", "&q="} {
		pool := &searchCapturePool{mockPool: &mockPool{queryRowVals: []*mockRow{{vals: []any{false}}}}}
		r := chi.NewRouter()
		(&API{pool: pool}).RegisterRoutes(r)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/statements?instance_id="+instance+
			"&database=app&from=2026-10-01T00:00:00Z&to=2026-10-02T00:00:00Z&limit=5"+query, http.NoBody))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.NotContains(t, pool.sqls[0], "ILIKE", query)
		require.Len(t, pool.args[0], 5, query)
		require.Equal(t, 5, pool.args[0][4], "the limit is not raised without q")
		require.NotContains(t, w.Body.String(), "has_more")
	}
}
