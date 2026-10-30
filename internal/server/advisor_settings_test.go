package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/advisor"
)

// stubAdvisorSettings is an advisorSettingsStore in memory.
type stubAdvisorSettings struct {
	overrides    map[string]bool
	windows      map[string]int64
	overridesErr error
	windowsErr   error
	writeErr     error
	overrideSets int
	windowSets   int
}

func newStubAdvisorSettings() *stubAdvisorSettings {
	return &stubAdvisorSettings{overrides: map[string]bool{}, windows: map[string]int64{}}
}

func (s *stubAdvisorSettings) RuleOverrides(context.Context) (map[string]bool, error) {
	if s.overridesErr != nil {
		return nil, s.overridesErr
	}
	out := map[string]bool{}
	for k, v := range s.overrides {
		out[k] = v
	}
	return out, nil
}

func (s *stubAdvisorSettings) SetRuleOverride(_ context.Context, ruleID string, enabled bool) error {
	s.overrideSets++
	if s.writeErr != nil {
		return s.writeErr
	}
	if enabled {
		delete(s.overrides, ruleID)
	} else {
		s.overrides[ruleID] = false
	}
	return nil
}

func (s *stubAdvisorSettings) Windows(context.Context) (map[string]int64, error) {
	if s.windowsErr != nil {
		return nil, s.windowsErr
	}
	out := map[string]int64{}
	for k, v := range s.windows {
		out[k] = v
	}
	return out, nil
}

func (s *stubAdvisorSettings) SetWindows(_ context.Context, set map[string]int64, reset []string) error {
	s.windowSets++
	if s.writeErr != nil {
		return s.writeErr
	}
	for k, v := range set {
		s.windows[k] = v
	}
	for _, k := range reset {
		delete(s.windows, k)
	}
	return nil
}

func advisorRules(t *testing.T, api *API) map[string]advisorRuleJSON {
	t.Helper()
	rec := retentionRequest(t, api, http.MethodGet, "/api/v1/advisor/rules", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var rows []advisorRuleJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rows))
	require.Len(t, rows, len(advisor.All()))
	out := map[string]advisorRuleJSON{}
	for _, row := range rows {
		out[row.ID] = row
	}
	return out
}

func advisorWindowRows(t *testing.T, rec *httptest.ResponseRecorder) []advisorWindowJSON {
	t.Helper()
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var rows []advisorWindowJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rows))
	return rows
}

// ADVO-001: an override round trip through the API. Disabling stores a row and
// the rule reads enabled false, overridden true; enabling deletes the row, back
// to enabled true, overridden false. Every other rule stays enabled.
func TestADVO001_OverrideRoundTrip(t *testing.T) {
	t.Parallel()
	stub := newStubAdvisorSettings()
	api := &API{advisorSettings: stub}

	for id, rule := range advisorRules(t, api) {
		require.True(t, rule.Enabled, id)
		require.False(t, rule.Overridden, id)
	}

	rec := retentionRequest(t, api, http.MethodPut, "/api/v1/advisor/rules/index.unused/override", `{"enabled":false}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var updated advisorRuleJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &updated))
	require.Equal(t, "index.unused", updated.ID)
	require.False(t, updated.Enabled)
	require.True(t, updated.Overridden)
	require.Equal(t, map[string]bool{"index.unused": false}, stub.overrides)

	rules := advisorRules(t, api)
	require.False(t, rules["index.unused"].Enabled)
	require.True(t, rules["index.unused"].Overridden)
	require.True(t, rules["query.slow_mean"].Enabled)

	rec = retentionRequest(t, api, http.MethodPut, "/api/v1/advisor/rules/index.unused/override", `{"enabled":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"id":"index.unused","severity":"`+string(rules["index.unused"].Severity)+`","scope":"`+string(rules["index.unused"].Scope)+`","needs":`+mustJSON(t, rules["index.unused"].Needs)+`,"min_tier":"`+rules["index.unused"].MinTier+`","enabled":true,"overridden":false}`, rec.Body.String())
	require.Empty(t, stub.overrides)
	require.True(t, advisorRules(t, api)["index.unused"].Enabled)

	// A row that says enabled (written outside the console) is still an override.
	stub.overrides["index.unused"] = true
	rules = advisorRules(t, api)
	require.True(t, rules["index.unused"].Enabled)
	require.True(t, rules["index.unused"].Overridden)
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return string(data)
}

// ADVO-001 (C5): without a database every rule is enabled (200); an
// unreadable override table is a 503.
func TestADVO001_RulesWithoutStoreAndOnReadError(t *testing.T) {
	t.Parallel()
	for id, rule := range advisorRules(t, NewAPI(nil)) {
		require.True(t, rule.Enabled, id)
		require.False(t, rule.Overridden, id)
	}
	stub := newStubAdvisorSettings()
	stub.overridesErr = errors.New("relation advisor_rule_overrides does not exist")
	requireAPIError(t, retentionRequest(t, &API{advisorSettings: stub}, http.MethodGet, "/api/v1/advisor/rules", ""), http.StatusServiceUnavailable, "store unavailable")
}

// ADVO-005: an unknown rule is a 404 and nothing is written; a body without a
// boolean enabled is a 400; a failed write is a 503.
func TestADVO005_UnknownRuleAndBadBody(t *testing.T) {
	t.Parallel()
	stub := newStubAdvisorSettings()
	api := &API{advisorSettings: stub}

	requireAPIError(t, retentionRequest(t, api, http.MethodPut, "/api/v1/advisor/rules/no.such_rule/override", `{"enabled":false}`), http.StatusNotFound, "unknown rule")
	for _, body := range []string{`{}`, `{"enabled":null}`, `{"enabled":"false"}`, `{"enabled":0}`, `{"enabled":false,"extra":1}`, `[]`} {
		rec := retentionRequest(t, api, http.MethodPut, "/api/v1/advisor/rules/index.unused/override", body)
		require.Equal(t, http.StatusBadRequest, rec.Code, body)
	}
	require.Zero(t, stub.overrideSets)

	stub.writeErr = errors.New("connection refused")
	requireAPIError(t, retentionRequest(t, api, http.MethodPut, "/api/v1/advisor/rules/index.unused/override", `{"enabled":false}`), http.StatusServiceUnavailable, "store unavailable")
	requireAPIError(t, retentionRequest(t, NewAPI(nil), http.MethodPut, "/api/v1/advisor/rules/index.unused/override", `{"enabled":false}`), http.StatusServiceUnavailable, "store unavailable")
}

// ADVW-001: without stored values both windows are 7 days (604800 s), default,
// in the fixed order unused_index then backup_history, with their bounds.
func TestADVW001_DefaultWindows(t *testing.T) {
	t.Parallel()
	rec := retentionRequest(t, &API{advisorSettings: newStubAdvisorSettings()}, http.MethodGet, "/api/v1/settings/advisor-windows", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `[
		{"key":"window.unused_index","seconds":604800,"default_seconds":604800,"min_seconds":3600,"max_seconds":7776000,"source":"default"},
		{"key":"window.backup_history","seconds":604800,"default_seconds":604800,"min_seconds":3600,"max_seconds":7776000,"source":"default"}
	]`, rec.Body.String())

	stub := newStubAdvisorSettings()
	stub.windows["window.backup_history"] = 86400
	rows := advisorWindowRows(t, retentionRequest(t, &API{advisorSettings: stub}, http.MethodGet, "/api/v1/settings/advisor-windows", ""))
	require.Equal(t, "window.unused_index", rows[0].Key)
	require.Equal(t, "default", rows[0].Source)
	require.Equal(t, "window.backup_history", rows[1].Key)
	require.Equal(t, int64(86400), rows[1].Seconds)
	require.Equal(t, "custom", rows[1].Source)

	requireAPIError(t, retentionRequest(t, NewAPI(nil), http.MethodGet, "/api/v1/settings/advisor-windows", ""), http.StatusServiceUnavailable, "store unavailable")
	stub.windowsErr = errors.New("timeout")
	requireAPIError(t, retentionRequest(t, &API{advisorSettings: stub}, http.MethodGet, "/api/v1/settings/advisor-windows", ""), http.StatusServiceUnavailable, "store unavailable")
}

// ADVW-002: a window is an integer number of seconds between 3600 and
// 7776000; anything else is a 400 and nothing is written, the other key of the
// same body included (all or none).
func TestADVW002_BoundsEnforced(t *testing.T) {
	t.Parallel()
	stub := newStubAdvisorSettings()
	api := &API{advisorSettings: stub}
	const target = "/api/v1/settings/advisor-windows"

	for _, body := range []string{
		`{"window.unused_index":3599}`,
		`{"window.unused_index":7776001}`,
		`{"window.unused_index":0}`,
		`{"window.unused_index":-3600}`,
		`{"window.unused_index":3600.5}`,
		`{"window.unused_index":3.6e3}`,
		`{"window.unused_index":"3600"}`,
		`{"window.unused_index":true}`,
		`{"window.backup_history":86400,"window.unused_index":1}`,
		`[]`,
		`null`,
		`3600`,
	} {
		rec := retentionRequest(t, api, http.MethodPut, target, body)
		require.Equal(t, http.StatusBadRequest, rec.Code, body)
	}
	require.Zero(t, stub.windowSets)

	for _, seconds := range []string{"3600", "7776000"} {
		rows := advisorWindowRows(t, retentionRequest(t, api, http.MethodPut, target, `{"window.unused_index":`+seconds+`}`))
		require.Equal(t, seconds, mustJSON(t, rows[0].Seconds))
		require.Equal(t, "custom", rows[0].Source)
	}

	// The SQL takes every value as a parameter (I-3).
	goVerb := regexp.MustCompile(`%[-+# 0-9.]*[sdvqxXT]`)
	for name, sql := range map[string]string{
		"overrides": advisorOverridesSQL, "override upsert": advisorOverrideUpsertSQL, "override delete": advisorOverrideDeleteSQL,
		"windows": advisorWindowsSQL, "window upsert": advisorWindowUpsertSQL, "window delete": advisorWindowDeleteSQL,
	} {
		require.False(t, goVerb.MatchString(sql), name)
		require.NotContains(t, sql, "window.", name)
	}
	pool := &retentionRecPool{policyRows: [][]any{
		{"window.unused_index", []byte("3600")},
		{"window.backup_history", []byte(`"bogus"`)},
	}}
	st := pgAdvisorSettingsStore{pool: pool}
	got, err := st.Windows(context.Background())
	require.NoError(t, err)
	require.Equal(t, map[string]int64{"window.unused_index": 3600}, got, "an invalid stored value reads as the default")
	require.NoError(t, st.SetWindows(context.Background(), map[string]int64{"window.backup_history": 7200}, []string{"window.unused_index"}))
	require.NoError(t, st.SetRuleOverride(context.Background(), "index.unused", false))
	require.NoError(t, st.SetRuleOverride(context.Background(), "index.unused", true))
	require.Equal(t, []recordedExec{
		{sql: advisorWindowsSQL, args: []any{[]string{"window.unused_index", "window.backup_history"}}},
		{sql: advisorWindowDeleteSQL, args: []any{"window.unused_index"}},
		{sql: advisorWindowUpsertSQL, args: []any{"window.backup_history", int64(7200)}},
		{sql: advisorOverrideUpsertSQL, args: []any{"index.unused", false}},
		{sql: advisorOverrideDeleteSQL, args: []any{"index.unused"}},
	}, pool.calls)
	require.True(t, pool.tx.committed)
}

// ADVW-003: null resets one key to its default and leaves the other; {} is a
// 200 no-op that writes nothing.
func TestADVW003_PerKeyReset(t *testing.T) {
	t.Parallel()
	stub := newStubAdvisorSettings()
	api := &API{advisorSettings: stub}
	const target = "/api/v1/settings/advisor-windows"

	rows := advisorWindowRows(t, retentionRequest(t, api, http.MethodPut, target, `{"window.unused_index":3600,"window.backup_history":172800}`))
	require.Equal(t, int64(3600), rows[0].Seconds)
	require.Equal(t, int64(172800), rows[1].Seconds)

	rows = advisorWindowRows(t, retentionRequest(t, api, http.MethodPut, target, `{"window.unused_index":null}`))
	require.Equal(t, advisorWindowJSON{Key: "window.unused_index", Seconds: 604800, DefaultSeconds: 604800, MinSeconds: 3600, MaxSeconds: 7776000, Source: "default"}, rows[0])
	require.Equal(t, int64(172800), rows[1].Seconds)
	require.Equal(t, "custom", rows[1].Source)

	writes := stub.windowSets
	rows = advisorWindowRows(t, retentionRequest(t, api, http.MethodPut, target, `{}`))
	require.Equal(t, writes, stub.windowSets, "{} writes nothing")
	require.Equal(t, int64(172800), rows[1].Seconds)

	stub.writeErr = errors.New("connection refused")
	requireAPIError(t, retentionRequest(t, api, http.MethodPut, target, `{"window.backup_history":null}`), http.StatusServiceUnavailable, "store unavailable")
}

// ADVW-004: any key but the two windows is a 400, window.pgstattuple and the
// other window.* keys included (D22, D22b), and nothing is written.
func TestADVW004_OtherKeysRejected(t *testing.T) {
	t.Parallel()
	stub := newStubAdvisorSettings()
	api := &API{advisorSettings: stub}
	for _, body := range []string{
		`{"window.pgstattuple":3600}`,
		`{"window.partition_horizon":3600}`,
		`{"window.unused_index":3600,"window.pgstattuple":3600}`,
		`{"unused_index":3600}`,
		`{"Window.unused_index":3600}`,
		`{"window.unused_index ":3600}`,
	} {
		rec := retentionRequest(t, api, http.MethodPut, "/api/v1/settings/advisor-windows", body)
		requireAPIError(t, rec, http.StatusBadRequest, "unknown key")
	}
	require.Zero(t, stub.windowSets)
	require.Empty(t, stub.windows)
}

// ADVO-005: the writes sit behind the credential gate and the cross-origin
// refusal of the production router.
func TestADVO005_GatedWrites(t *testing.T) {
	t.Parallel()
	router, token := securityRouter(t)
	send := func(method, target, body string, cookie bool, headers map[string]string) int {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
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
	for _, write := range []struct{ target, body string }{
		{"/api/v1/advisor/rules/index.unused/override", `{"enabled":false}`},
		{"/api/v1/settings/advisor-windows", `{"window.unused_index":3600}`},
	} {
		require.Equal(t, http.StatusUnauthorized, send(http.MethodPut, write.target, write.body, false, nil), write.target)
		require.Equal(t, http.StatusForbidden, send(http.MethodPut, write.target, write.body, true, map[string]string{"Sec-Fetch-Site": "cross-site"}), write.target)
		require.Equal(t, http.StatusForbidden, send(http.MethodPut, write.target, write.body, true, map[string]string{"Origin": "https://evil.example"}), write.target)
		// Past the gate the request reaches the missing store.
		require.Equal(t, http.StatusServiceUnavailable, send(http.MethodPut, write.target, write.body, true, map[string]string{"Sec-Fetch-Site": "same-origin"}), write.target)
	}
	require.Equal(t, http.StatusUnauthorized, send(http.MethodGet, "/api/v1/settings/advisor-windows", "", false, nil))
}

// ADVO-002 (wiring): the engine's overrides source reads the same rows the API
// writes, and without a pool disables nothing.
func TestADVO002_OverridesSourceWiring(t *testing.T) {
	t.Parallel()
	require.Nil(t, AdvisorOverridesSource(nil))
	pool := &retentionRecPool{policyRows: [][]any{{"index.unused", false}}}
	got, err := pgAdvisorSettingsStore{pool: pool}.RuleOverrides(context.Background())
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"index.unused": false}, got)
	require.Equal(t, []recordedExec{{sql: advisorOverridesSQL}}, pool.calls)
}
