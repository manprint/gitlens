package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func secondsOf(d time.Duration) *float64 {
	s := d.Seconds()
	return &s
}

const day = 24 * time.Hour

// defaultPolicies is what migrations 0003 and 0007 leave: 30 days of
// retention (bloat 90) and a 48-hour compression horizon (bloat 7 days).
func defaultPolicies() map[string]livePolicy {
	out := map[string]livePolicy{}
	for _, f := range retentionFamilies {
		compress := 48 * time.Hour
		if f.Name == "metrics_bloat" {
			compress = 7 * day
		}
		out[f.Hypertable] = livePolicy{DropAfter: secondsOf(time.Duration(f.DefaultDays) * day), CompressAfter: secondsOf(compress)}
	}
	return out
}

type replaceCall struct {
	hypertable string
	days       int
	seen       *float64
}

// stubPolicyStore is a policyStore in memory. Replace records the call and,
// unless an error is queued, applies it to the policies.
type stubPolicyStore struct {
	policies    map[string]livePolicy
	policiesErr error
	preview     chunkPreview
	previewErr  error
	replaceErrs []error

	policyReads int
	previews    []int
	replaces    []replaceCall
}

func (s *stubPolicyStore) Policies(context.Context) (map[string]livePolicy, error) {
	s.policyReads++
	if s.policiesErr != nil {
		return nil, s.policiesErr
	}
	out := make(map[string]livePolicy, len(s.policies))
	for k, v := range s.policies {
		out[k] = v
	}
	return out, nil
}

func (s *stubPolicyStore) Preview(_ context.Context, _ string, days int) (chunkPreview, error) {
	s.previews = append(s.previews, days)
	return s.preview, s.previewErr
}

func (s *stubPolicyStore) Replace(_ context.Context, hypertable string, days int, seen *float64) error {
	s.replaces = append(s.replaces, replaceCall{hypertable: hypertable, days: days, seen: seen})
	if len(s.replaceErrs) > 0 {
		err := s.replaceErrs[0]
		s.replaceErrs = s.replaceErrs[1:]
		if err != nil {
			return err
		}
	}
	p := s.policies[hypertable]
	p.DropAfter = secondsOf(time.Duration(days) * day)
	s.policies[hypertable] = p
	return nil
}

func retentionRequest(t *testing.T, api *API, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, target, http.NoBody)
	} else {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func getRetentionRows(t *testing.T, api *API) []retentionRow {
	t.Helper()
	rec := retentionRequest(t, api, http.MethodGet, "/api/v1/settings/retention", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var rows []retentionRow
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rows))
	return rows
}

// RET-001: GET lists the seven families in D17 order, 30 days each and 90 for
// metrics_bloat, all default, with min_days 3 (metrics_bloat 8) and max 365. A
// family without a retention policy reads days null, source custom; one without
// a compression policy gets min_days 3; another value is custom.
func TestRET001(t *testing.T) {
	t.Parallel()
	rows := getRetentionRows(t, &API{retention: &stubPolicyStore{policies: defaultPolicies()}})
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.Family)
		require.Equal(t, row.Family, row.Hypertable)
		require.Equal(t, "default", row.Source, row.Family)
		require.Equal(t, 365, row.MaxDays)
		require.NotNil(t, row.Days)
		if row.Family == "metrics_bloat" {
			require.Equal(t, 90, *row.Days)
			require.Equal(t, 8, row.MinDays)
		} else {
			require.Equal(t, 30, *row.Days, row.Family)
			require.Equal(t, 3, row.MinDays, row.Family)
		}
	}
	require.Equal(t, []string{"metrics", "metrics_statements", "metrics_ash", "metrics_replication", "metrics_tables", "metrics_indexes", "metrics_bloat"}, names)

	policies := defaultPolicies()
	policies["metrics_ash"] = livePolicy{DropAfter: secondsOf(14 * day), CompressAfter: secondsOf(48 * time.Hour)}
	policies["metrics_tables"] = livePolicy{CompressAfter: secondsOf(48 * time.Hour)}
	policies["metrics_indexes"] = livePolicy{DropAfter: secondsOf(30 * day)}
	policies["metrics"] = livePolicy{DropAfter: secondsOf(36 * time.Hour), CompressAfter: secondsOf(48 * time.Hour)}
	rec := retentionRequest(t, &API{retention: &stubPolicyStore{policies: policies}}, http.MethodGet, "/api/v1/settings/retention", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `{"family":"metrics_tables","hypertable":"metrics_tables","days":null,"source":"custom","min_days":3,"max_days":365}`)
	var got []retentionRow
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	byName := map[string]retentionRow{}
	for _, row := range got {
		byName[row.Family] = row
	}
	require.Equal(t, 14, *byName["metrics_ash"].Days)
	require.Equal(t, "custom", byName["metrics_ash"].Source)
	require.Equal(t, 3, byName["metrics_indexes"].MinDays, "no compression policy: the floor")
	require.Equal(t, "default", byName["metrics_indexes"].Source)
	require.Equal(t, 2, *byName["metrics"].Days, "a part of a day reads rounded up")
	require.Equal(t, "custom", byName["metrics"].Source)

	// No store, or a failed read: 503.
	requireAPIError(t, retentionRequest(t, &API{}, http.MethodGet, "/api/v1/settings/retention", ""), http.StatusServiceUnavailable, "store unavailable")
	failing := &stubPolicyStore{policiesErr: errors.New("connection refused")}
	requireAPIError(t, retentionRequest(t, &API{retention: failing}, http.MethodGet, "/api/v1/settings/retention", ""), http.StatusServiceUnavailable, "store unavailable")
}

// RET-002: PUT and preview refuse days below the family's min_days (2; 7 for
// metrics_bloat) and above 365 with 400, an unknown family with 404, a missing
// or non-integer days with 400, no store with 503; nothing is written.
func TestRET002(t *testing.T) {
	t.Parallel()
	stub := &stubPolicyStore{policies: defaultPolicies()}
	api := &API{retention: stub}
	put := func(family, body string) *httptest.ResponseRecorder {
		return retentionRequest(t, api, http.MethodPut, "/api/v1/settings/retention/"+family, body)
	}
	requireAPIError(t, put("metrics", `{"days":2,"confirm":true}`), http.StatusBadRequest, "invalid days")
	requireAPIError(t, put("metrics_bloat", `{"days":7,"confirm":true}`), http.StatusBadRequest, "invalid days")
	requireAPIError(t, put("metrics_ash", `{"days":366}`), http.StatusBadRequest, "invalid days")
	requireAPIError(t, put("metrics_ash", `{"days":0,"confirm":true}`), http.StatusBadRequest, "invalid days")
	requireAPIError(t, put("metrics_ash", `{"days":-30,"confirm":true}`), http.StatusBadRequest, "invalid days")
	requireAPIError(t, put("metrics_ash", `{"confirm":true}`), http.StatusBadRequest, "invalid days")
	requireAPIError(t, put("metrics_ash", `{"days":14.5}`), http.StatusBadRequest, "invalid body")
	requireAPIError(t, put("metrics_ash", `{"days":"30"}`), http.StatusBadRequest, "invalid body")
	requireAPIError(t, put("metrics_ash", `{"days":30,"force":true}`), http.StatusBadRequest, "invalid body")
	requireAPIError(t, put("events", `{"days":30}`), http.StatusNotFound, "unknown family")
	requireAPIError(t, put("metrics;drop", `{"days":30}`), http.StatusNotFound, "unknown family")
	rec := put("metrics_bloat", `{"days":8,"confirm":true}`)
	require.Equal(t, http.StatusOK, rec.Code, "8 is metrics_bloat's min_days: %s", rec.Body.String())
	rec = put("metrics_ash", `{"days":365}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, stub.replaces, 2, "only the two valid writes reached the store")

	preview := func(query string) *httptest.ResponseRecorder {
		return retentionRequest(t, api, http.MethodGet, "/api/v1/settings/retention/preview"+query, "")
	}
	requireAPIError(t, preview("?family=metrics&days=2"), http.StatusBadRequest, "invalid days")
	requireAPIError(t, preview("?family=metrics_bloat&days=7"), http.StatusBadRequest, "invalid days")
	requireAPIError(t, preview("?family=metrics&days=366"), http.StatusBadRequest, "invalid days")
	requireAPIError(t, preview("?family=metrics"), http.StatusBadRequest, "invalid days")
	requireAPIError(t, preview("?family=metrics&days=1.5"), http.StatusBadRequest, "invalid days")
	requireAPIError(t, preview("?days=10"), http.StatusBadRequest, "invalid family")
	requireAPIError(t, preview("?family=query_texts&days=10"), http.StatusNotFound, "unknown family")
	require.Empty(t, stub.previews)

	// A form body is refused unread, as on every JSON write.
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings/retention/metrics", strings.NewReader(`days=3`))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnsupportedMediaType, rec.Code)

	// The bounds every family shares are checked before the store is read:
	// 400 even with no store.
	for _, body := range []string{`{"days":2,"confirm":true}`, `{"days":366}`} {
		requireAPIError(t, retentionRequest(t, &API{}, http.MethodPut, "/api/v1/settings/retention/metrics", body), http.StatusBadRequest, "invalid days")
	}
	for _, days := range []string{"2", "366"} {
		requireAPIError(t, retentionRequest(t, &API{}, http.MethodGet, "/api/v1/settings/retention/preview?family=metrics&days="+days, ""), http.StatusBadRequest, "invalid days")
	}
	down := &stubPolicyStore{policiesErr: errors.New("connection refused")}
	requireAPIError(t, retentionRequest(t, &API{retention: down}, http.MethodPut, "/api/v1/settings/retention/metrics", `{"days":0}`), http.StatusBadRequest, "invalid days")
	require.Zero(t, down.policyReads, "out of bounds: the store is not read")

	// No store: 503 once the request itself is valid.
	requireAPIError(t, retentionRequest(t, &API{}, http.MethodPut, "/api/v1/settings/retention/metrics", `{"days":40}`), http.StatusServiceUnavailable, "store unavailable")
	requireAPIError(t, retentionRequest(t, &API{}, http.MethodGet, "/api/v1/settings/retention/preview?family=metrics&days=40", ""), http.StatusServiceUnavailable, "store unavailable")
	require.Len(t, stub.replaces, 2)
}

// RET-002: the retention write sits behind the credential gate and the
// cross-origin refusal of the production router.
func TestRET002GatedWrite(t *testing.T) {
	t.Parallel()
	router, token := securityRouter(t)
	send := func(method, target string, cookie bool, headers map[string]string) int {
		req := httptest.NewRequest(method, target, strings.NewReader(`{"days":40}`))
		req.Header.Set("Content-Type", "application/json")
		if cookie {
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
		}
		for name, value := range headers {
			req.Header.Set(name, value)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}
	target := "/api/v1/settings/retention/metrics_ash"
	require.Equal(t, http.StatusUnauthorized, send(http.MethodPut, target, false, nil))
	require.Equal(t, http.StatusForbidden, send(http.MethodPut, target, true, map[string]string{"Sec-Fetch-Site": "cross-site"}))
	require.Equal(t, http.StatusForbidden, send(http.MethodPut, target, true, map[string]string{"Origin": "https://evil.example"}))
	// Past the gate the request reaches the missing store.
	require.Equal(t, http.StatusServiceUnavailable, send(http.MethodPut, target, true, map[string]string{"Sec-Fetch-Site": "same-origin"}))
	require.Equal(t, http.StatusUnauthorized, send(http.MethodGet, "/api/v1/settings/retention", false, nil))
	require.Equal(t, http.StatusUnauthorized, send(http.MethodGet, "/api/v1/settings/retention/preview?family=metrics&days=10", false, nil))
}

// RET-003: shortening without confirm is a 409 that carries the preview and
// writes nothing; with confirm it is applied. A family without a retention
// policy counts as shortening.
func TestRET003(t *testing.T) {
	t.Parallel()
	oldest := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	newest := time.Date(2026, 10, 16, 6, 0, 0, 0, time.UTC)
	policies := defaultPolicies()
	policies["metrics_tables"] = livePolicy{CompressAfter: secondsOf(48 * time.Hour)}
	stub := &stubPolicyStore{
		policies: policies,
		preview:  chunkPreview{Chunks: 12, Bytes: 7 << 20, OldestChunkAt: &oldest, NewestDroppedEnd: &newest},
	}
	api := &API{retention: stub}

	rec := retentionRequest(t, api, http.MethodPut, "/api/v1/settings/retention/metrics_ash", `{"days":14}`)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	var conflict retentionConfirmationRequired
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &conflict))
	require.Equal(t, "confirmation required", conflict.Error)
	require.NotEmpty(t, conflict.Detail)
	require.Equal(t, retentionPreviewResp{Family: "metrics_ash", Days: 14, ChunksToDrop: 12, BytesToFree: 7 << 20, OldestChunkAt: &oldest, NewestDroppedChunkEnd: &newest}, conflict.Preview)
	rec = retentionRequest(t, api, http.MethodPut, "/api/v1/settings/retention/metrics_ash", `{"days":14,"confirm":false}`)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	rec = retentionRequest(t, api, http.MethodPut, "/api/v1/settings/retention/metrics_tables", `{"days":365}`)
	require.Equal(t, http.StatusConflict, rec.Code, "no policy: any retention shortens: %s", rec.Body.String())
	require.Empty(t, stub.replaces)

	rec = retentionRequest(t, api, http.MethodPut, "/api/v1/settings/retention/metrics_ash", `{"days":14,"confirm":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var row retentionRow
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &row))
	require.Equal(t, retentionRow{Family: "metrics_ash", Hypertable: "metrics_ash", Days: new(14), Source: "custom", MinDays: 3, MaxDays: 365}, row)
	require.Equal(t, []replaceCall{{hypertable: "metrics_ash", days: 14, seen: secondsOf(30 * day)}}, stub.replaces)

	rec = retentionRequest(t, api, http.MethodPut, "/api/v1/settings/retention/metrics_tables", `{"days":30,"confirm":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Nil(t, stub.replaces[1].seen, "the write expects the family to have no policy")
}

// RET-003: a write that finds the policy changed under it decides again on the
// new value, and a failing store answers 500 with the old policy kept.
func TestRET003ConcurrentChange(t *testing.T) {
	t.Parallel()
	stub := &stubPolicyStore{policies: defaultPolicies(), replaceErrs: []error{errRetentionChanged}}
	rec := retentionRequest(t, &API{retention: stub}, http.MethodPut, "/api/v1/settings/retention/metrics", `{"days":60}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 2, stub.policyReads)
	require.Len(t, stub.replaces, 2)

	stuck := &stubPolicyStore{policies: defaultPolicies(), replaceErrs: []error{errRetentionChanged, errRetentionChanged, errRetentionChanged}}
	requireAPIError(t, retentionRequest(t, &API{retention: stuck}, http.MethodPut, "/api/v1/settings/retention/metrics", `{"days":60}`), http.StatusInternalServerError, "retention update failed")
	require.Len(t, stuck.replaces, retentionAttempts)

	broken := &stubPolicyStore{policies: defaultPolicies(), replaceErrs: []error{fmt.Errorf("add_retention_policy: %w", &pgconn.PgError{Code: "XX000", Message: "boom"})}}
	requireAPIError(t, retentionRequest(t, &API{retention: broken}, http.MethodPut, "/api/v1/settings/retention/metrics", `{"days":60}`), http.StatusInternalServerError, "retention update failed")
	require.Equal(t, defaultPolicies()["metrics"], broken.policies["metrics"])
}

// RET-003: a write whose store does not answer (connection refused, timeout:
// no PostgreSQL error) is 503 store unavailable, not a failed policy call; the
// old policy is kept.
func TestRET003StoreDown(t *testing.T) {
	t.Parallel()
	for _, err := range []error{
		errors.New("failed to connect to `host=db`: connection refused"),
		fmt.Errorf("lock: %w", context.DeadlineExceeded),
	} {
		down := &stubPolicyStore{policies: defaultPolicies(), replaceErrs: []error{err}}
		rec := retentionRequest(t, &API{retention: down}, http.MethodPut, "/api/v1/settings/retention/metrics", `{"days":60}`)
		requireAPIError(t, rec, http.StatusServiceUnavailable, "store unavailable")
		require.Equal(t, defaultPolicies()["metrics"], down.policies["metrics"])
	}
}

// RET-004: lengthening applies without confirm; the same days answer the row
// without a write; a preview that does not shorten drops nothing.
func TestRET004(t *testing.T) {
	t.Parallel()
	oldest := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	stub := &stubPolicyStore{policies: defaultPolicies(), preview: chunkPreview{Chunks: 3, Bytes: 1 << 20, OldestChunkAt: &oldest, NewestDroppedEnd: &oldest}}
	api := &API{retention: stub}

	rec := retentionRequest(t, api, http.MethodPut, "/api/v1/settings/retention/metrics_ash", `{"days":45}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var row retentionRow
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &row))
	require.Equal(t, 45, *row.Days)
	require.Equal(t, "custom", row.Source)
	require.Equal(t, []replaceCall{{hypertable: "metrics_ash", days: 45, seen: secondsOf(30 * day)}}, stub.replaces)

	// The same days again: answered, not rewritten.
	rec = retentionRequest(t, api, http.MethodPut, "/api/v1/settings/retention/metrics_ash", `{"days":45}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, stub.replaces, 1)
	rec = retentionRequest(t, api, http.MethodPut, "/api/v1/settings/retention/metrics_bloat", `{"days":90,"confirm":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"source":"default"`)
	require.Len(t, stub.replaces, 1)

	// Back to 30 is a shortening from 45; it is the default again.
	rec = retentionRequest(t, api, http.MethodPut, "/api/v1/settings/retention/metrics_ash", `{"days":30,"confirm":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"source":"default"`)

	rec = retentionRequest(t, api, http.MethodGet, "/api/v1/settings/retention/preview?family=metrics&days=40", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var preview retentionPreviewResp
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &preview))
	require.Equal(t, retentionPreviewResp{Family: "metrics", Days: 40, OldestChunkAt: &oldest}, preview)
	require.Contains(t, rec.Body.String(), `"newest_dropped_chunk_end":null`)

	rec = retentionRequest(t, api, http.MethodGet, "/api/v1/settings/retention/preview?family=metrics&days=10", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &preview))
	require.Equal(t, int64(3), preview.ChunksToDrop)
	require.Equal(t, int64(1<<20), preview.BytesToFree)
}

// RET-005: the policy SQL takes the hypertable and the days as bound
// parameters only, the families are a fixed whitelist of hypertable names, and
// the store sends exactly those statements with those arguments.
func TestRET005(t *testing.T) {
	t.Parallel()
	statements := map[string]string{
		"policies": retentionPoliciesSQL,
		"preview":  retentionPreviewSQL,
		"lock":     retentionLockSQL,
		"current":  retentionCurrentSQL,
		"alter":    retentionAlterSQL,
		"add":      retentionAddSQL,
	}
	goVerb := regexp.MustCompile(`%[-+# 0-9.]*[sdvqxXT]`)
	for name, sql := range statements {
		require.False(t, goVerb.MatchString(sql), "%s: no Go format verb in SQL", name)
		for _, f := range retentionFamilies {
			require.NotContains(t, sql, "'"+f.Hypertable+"'", "%s: no literal hypertable", name)
		}
	}
	for _, name := range []string{"policies", "current"} {
		require.Contains(t, statements[name], "$1", name)
	}
	for _, name := range []string{"preview", "alter", "add"} {
		require.Contains(t, statements[name], "$1", name)
		require.Contains(t, statements[name], "$2::int", name)
	}
	// Identifiers are quoted by format('%I'), never concatenated.
	require.Contains(t, retentionAddSQL, `format('public.%I', $1::text)::regclass`)
	require.Contains(t, retentionAddSQL, `make_interval(days => $2::int)`)
	// A new policy first runs a day later; an existing one is altered in
	// place, never removed and added again (which would run it at once).
	require.Contains(t, retentionAddSQL, `alter_job(n.job_id, next_start => now() + interval '1 day')`)
	require.NotContains(t, retentionAddSQL, `initial_start`)
	require.Contains(t, retentionAlterSQL, `alter_job(j.job_id,`)
	require.Contains(t, retentionAlterSQL, `to_jsonb(make_interval(days => $2::int)::text)`)
	require.Contains(t, retentionAlterSQL, `j.hypertable_name = $1::text`)
	// A job removed meanwhile counts 0 (decided again), never an error.
	require.Contains(t, retentionAlterSQL, `if_exists => true`)
	require.Contains(t, retentionAlterSQL, `SELECT count(a.job_id)`)
	for name, sql := range statements {
		require.NotContains(t, sql, "remove_retention_policy", name)
	}
	// The alter keeps the job's schedule: it sets no next start.
	require.NotContains(t, retentionAlterSQL, "next_start")
	require.Contains(t, retentionPreviewSQL, `show_chunks(h.rel, older_than => $2::int * interval '1 day')`)
	require.Contains(t, retentionPreviewSQL, `chunks_detailed_size(h.rel)`)

	ident := regexp.MustCompile(`^metrics(_[a-z]+)?$`)
	seen := map[string]bool{}
	for _, f := range retentionFamilies {
		require.Regexp(t, ident, f.Hypertable)
		require.Equal(t, f.Name, f.Hypertable)
		require.False(t, seen[f.Name])
		seen[f.Name] = true
	}
	require.Len(t, retentionFamilies, 7)
	_, ok := lookupRetentionFamily("metrics_ash")
	require.True(t, ok)
	for _, bad := range []string{"", "Metrics", "metrics_ash ", "public.metrics", "metrics_ash;", "events"} {
		_, ok := lookupRetentionFamily(bad)
		require.False(t, ok, bad)
	}

	// The store sends the statements above with the whitelisted name and the days as arguments.
	pool := &retentionRecPool{current: &mockRow{vals: []any{secondsOf(30 * day), int64(0)}}}
	ps := pgPolicyStore{pool: pool}
	_, err := ps.Policies(context.Background())
	require.NoError(t, err)
	_, err = ps.Preview(context.Background(), "metrics_ash", 14)
	require.NoError(t, err)
	require.NoError(t, ps.Replace(context.Background(), "metrics_ash", 14, secondsOf(30*day)))
	require.Equal(t, []recordedExec{
		{sql: retentionPoliciesSQL, args: []any{retentionHypertables()}},
		{sql: retentionPreviewSQL, args: []any{"metrics_ash", 14}},
		{sql: retentionLockSQL},
		{sql: retentionCurrentSQL, args: []any{"metrics_ash"}},
		{sql: retentionAlterSQL, args: []any{"metrics_ash", 14}},
	}, pool.calls)
	require.True(t, pool.tx.committed)

	// A hypertable without a policy gets one created, nothing altered.
	none := &retentionRecPool{current: &mockRow{vals: []any{nil, int64(0)}}}
	require.NoError(t, pgPolicyStore{pool: none}.Replace(context.Background(), "metrics_tables", 30, nil))
	require.Equal(t, []recordedExec{
		{sql: retentionLockSQL},
		{sql: retentionCurrentSQL, args: []any{"metrics_tables"}},
		{sql: retentionAddSQL, args: []any{"metrics_tables", 30}},
	}, none.calls)
	require.True(t, none.tx.committed)
}

// RET-005: a failing policy call rolls the transaction back, so the old policy
// stays; a policy changed since it was read is not touched.
func TestRET005Rollback(t *testing.T) {
	t.Parallel()
	pool := &retentionRecPool{current: &mockRow{vals: []any{secondsOf(30 * day), int64(0)}}, alterErr: &pgconn.PgError{Code: "XX000", Message: "alter failed"}}
	err := pgPolicyStore{pool: pool}.Replace(context.Background(), "metrics_ash", 14, secondsOf(30*day))
	require.ErrorContains(t, err, "alter_job")
	require.False(t, pool.tx.committed)
	require.True(t, pool.tx.rolledBack)

	added := &retentionRecPool{current: &mockRow{vals: []any{nil, int64(0)}}, addErr: &pgconn.PgError{Code: "XX000", Message: "add failed"}}
	err = pgPolicyStore{pool: added}.Replace(context.Background(), "metrics_tables", 30, nil)
	require.ErrorContains(t, err, "add_retention_policy")
	require.False(t, added.tx.committed)
	require.True(t, added.tx.rolledBack)

	// The policy read under the lock is gone by the alter: nothing changed.
	vanished := &retentionRecPool{current: &mockRow{vals: []any{secondsOf(30 * day), int64(0)}}, altered: &mockRow{vals: []any{int64(0)}}}
	err = pgPolicyStore{pool: vanished}.Replace(context.Background(), "metrics_ash", 14, secondsOf(30*day))
	require.ErrorIs(t, err, errRetentionChanged)
	require.False(t, vanished.tx.committed)
	require.True(t, vanished.tx.rolledBack)

	changed := &retentionRecPool{current: &mockRow{vals: []any{secondsOf(20 * day), int64(0)}}}
	err = pgPolicyStore{pool: changed}.Replace(context.Background(), "metrics_ash", 14, secondsOf(30*day))
	require.ErrorIs(t, err, errRetentionChanged)
	require.False(t, changed.tx.committed)
	require.True(t, changed.tx.rolledBack)
	for _, call := range changed.calls {
		require.NotEqual(t, retentionAlterSQL, call.sql, "nothing altered")
		require.NotEqual(t, retentionAddSQL, call.sql, "nothing added")
	}
}

// RET-006: min_days follows the family's compression policy: one day past its
// horizon, never under 3 (compress_after 10 days gives 11).
func TestRET006(t *testing.T) {
	t.Parallel()
	require.Equal(t, 3, minRetentionDays(nil))
	require.Equal(t, 3, minRetentionDays(secondsOf(48*time.Hour)))
	require.Equal(t, 3, minRetentionDays(secondsOf(time.Hour)))
	require.Equal(t, 4, minRetentionDays(secondsOf(49*time.Hour)))
	require.Equal(t, 8, minRetentionDays(secondsOf(7*day)))
	require.Equal(t, 11, minRetentionDays(secondsOf(10*day)))

	policies := defaultPolicies()
	policies["metrics_ash"] = livePolicy{DropAfter: secondsOf(30 * day), CompressAfter: secondsOf(10 * day)}
	stub := &stubPolicyStore{policies: policies}
	api := &API{retention: stub}
	for _, row := range getRetentionRows(t, api) {
		if row.Family == "metrics_ash" {
			require.Equal(t, 11, row.MinDays)
		}
	}
	requireAPIError(t, retentionRequest(t, api, http.MethodPut, "/api/v1/settings/retention/metrics_ash", `{"days":10,"confirm":true}`), http.StatusBadRequest, "invalid days")
	requireAPIError(t, retentionRequest(t, api, http.MethodGet, "/api/v1/settings/retention/preview?family=metrics_ash&days=10", ""), http.StatusBadRequest, "invalid days")
	rec := retentionRequest(t, api, http.MethodPut, "/api/v1/settings/retention/metrics_ash", `{"days":11,"confirm":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"min_days":11`)
}

// RET-007: a retention policy without drop_after (drop_created_before, set
// outside the console) is reported as it is, not as "no policy", and is never
// previewed or replaced; a compression policy on creation time
// (compress_created_before) bounds min_days as compress_after does.
func TestRET007(t *testing.T) {
	t.Parallel()
	// The policy read: compress_after or compress_created_before, and a
	// description of a retention policy without drop_after.
	require.Contains(t, retentionPoliciesSQL, `coalesce(j.config->>'compress_after', j.config->>'compress_created_before')`)
	require.Contains(t, retentionPoliciesSQL, `j.config->>'drop_after' IS NULL`)
	require.Contains(t, retentionCurrentSQL, `count(*) FILTER (WHERE j.config->>'drop_after' IS NULL)`)
	pool := &retentionRecPool{policyRows: [][]any{
		{"metrics_ash", "policy_retention", nil, "drop_created_before 30 days"},
		{"metrics_ash", "policy_compression", secondsOf(48 * time.Hour), nil},
		{"metrics", "policy_retention", secondsOf(30 * day), nil},
		{"metrics", "policy_compression", secondsOf(5 * day), nil},
	}}
	policies, err := pgPolicyStore{pool: pool}.Policies(context.Background())
	require.NoError(t, err)
	require.Equal(t, livePolicy{CompressAfter: secondsOf(48 * time.Hour), Unsupported: "drop_created_before 30 days"}, policies["metrics_ash"])
	require.Equal(t, livePolicy{DropAfter: secondsOf(30 * day), CompressAfter: secondsOf(5 * day)}, policies["metrics"])

	stub := &stubPolicyStore{policies: defaultPolicies(), preview: chunkPreview{Chunks: 4}}
	stub.policies["metrics_ash"] = policies["metrics_ash"]
	stub.policies["metrics"] = policies["metrics"]
	api := &API{retention: stub}
	rec := retentionRequest(t, api, http.MethodGet, "/api/v1/settings/retention", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `{"family":"metrics_ash","hypertable":"metrics_ash","days":null,"source":"custom","min_days":3,"max_days":365,"unsupported_policy":"drop_created_before 30 days"}`)
	require.Contains(t, rec.Body.String(), `{"family":"metrics","hypertable":"metrics","days":30,"source":"default","min_days":6,"max_days":365}`, "compress_created_before 5 days gives min_days 6")

	for _, body := range []string{`{"days":30}`, `{"days":30,"confirm":true}`, `{"days":60,"confirm":true}`} {
		rec = retentionRequest(t, api, http.MethodPut, "/api/v1/settings/retention/metrics_ash", body)
		requireAPIError(t, rec, http.StatusServiceUnavailable, "unsupported retention policy")
		require.Contains(t, rec.Body.String(), "drop_created_before 30 days")
	}
	requireAPIError(t, retentionRequest(t, api, http.MethodGet, "/api/v1/settings/retention/preview?family=metrics_ash&days=30", ""), http.StatusServiceUnavailable, "unsupported retention policy")
	require.Empty(t, stub.replaces)
	require.Empty(t, stub.previews)

	// Such a policy appearing between the read and the write: refused in the
	// transaction, nothing removed.
	late := &retentionRecPool{current: &mockRow{vals: []any{nil, int64(1)}}}
	err = pgPolicyStore{pool: late}.Replace(context.Background(), "metrics_tables", 30, nil)
	require.ErrorIs(t, err, errRetentionUnsupported)
	require.True(t, late.tx.rolledBack)
	for _, call := range late.calls {
		require.NotEqual(t, retentionAlterSQL, call.sql, "nothing altered")
		require.NotEqual(t, retentionAddSQL, call.sql, "nothing added")
	}
	raced := &stubPolicyStore{policies: defaultPolicies(), replaceErrs: []error{errRetentionUnsupported}}
	requireAPIError(t, retentionRequest(t, &API{retention: raced}, http.MethodPut, "/api/v1/settings/retention/metrics", `{"days":60}`), http.StatusServiceUnavailable, "unsupported retention policy")
}

// retentionRecPool records the statements of the policy store and answers
// them: the policy read with no row, the preview with zeros, the live
// drop_after with current, the alter with altered (one job when nil) or
// alterErr, and the add with a job id or addErr.
type retentionRecPool struct {
	calls      []recordedExec
	policyRows [][]any
	current    *mockRow
	altered    *mockRow
	alterErr   error
	addErr     error
	tx         *retentionRecTx
}

func (p *retentionRecPool) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	p.calls = append(p.calls, recordedExec{sql: sql, args: args})
	return pgconn.NewCommandTag("SELECT 1"), nil
}

func (p *retentionRecPool) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	p.calls = append(p.calls, recordedExec{sql: sql, args: args})
	return &mockRows{rows: p.policyRows}, nil
}

func (p *retentionRecPool) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	p.calls = append(p.calls, recordedExec{sql: sql, args: args})
	switch sql {
	case retentionPreviewSQL:
		return &mockRow{vals: []any{int64(0), int64(0), nil, nil}}
	case retentionCurrentSQL:
		return p.current
	case retentionAlterSQL:
		if p.alterErr != nil {
			return &mockRow{err: p.alterErr}
		}
		if p.altered != nil {
			return p.altered
		}
		return &mockRow{vals: []any{int64(1)}}
	case retentionAddSQL:
		if p.addErr != nil {
			return &mockRow{err: p.addErr}
		}
		return &mockRow{vals: []any{int64(1014)}}
	}
	return &mockRow{err: pgx.ErrNoRows}
}

func (p *retentionRecPool) Begin(context.Context) (pgx.Tx, error) {
	p.tx = &retentionRecTx{pool: p}
	return p.tx, nil
}

type retentionRecTx struct {
	mockTx
	pool *retentionRecPool
}

func (t *retentionRecTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return t.pool.Exec(ctx, sql, args...)
}

func (t *retentionRecTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return t.pool.QueryRow(ctx, sql, args...)
}

// Rollback after Commit is the deferred no-op; only a rollback without a
// commit counts.
func (t *retentionRecTx) Rollback(context.Context) error {
	if !t.committed {
		t.rolledBack = true
	}
	return nil
}
