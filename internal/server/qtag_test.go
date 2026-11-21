package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const qtagTraceID = "5bd66ef5095369c7b0d1f8f4bd33716a"

// TestQTAG012: the pipeline stores a valid trace_id label and NULL for any
// other value, on log events and on plan samples.
func TestQTAG012(t *testing.T) {
	inst := uuid.New()
	for v, want := range map[string]*string{
		qtagTraceID:                        ptr(qtagTraceID),
		"xyz":                              nil,
		"5BD66EF5095369C7B0D1F8F4BD33716A": nil,
		"00000000000000000000000000000000": nil,
		"":                                 nil,
	} {
		l := logLabels()
		l["trace_id"] = v
		row, ok := logEventRow(logFact(l, map[string]string{"message": "m"}), "default", 3, inst)
		require.True(t, ok)
		require.Equal(t, want, row.TraceID, v)

		p, skip, reason := autoPlanFromFact(map[string]string{
			"source": planSourceAuto, "ts": time.Now().UTC().Format(time.RFC3339Nano), "queryid": "77",
			"duration_ms": "1", "plan_hash": "h1", "trace_id": v,
		}, nil, "default", 3, inst)
		require.False(t, skip)
		require.Empty(t, reason)
		require.Equal(t, want, p.sample.TraceID, v)
	}
}

// TestQTAG013: PGLENS_TRACE_URL_TEMPLATE must be an http(s) URL holding
// {trace_id}; a valid one reaches a signed-in session.
func TestQTAG013(t *testing.T) {
	load := func(tpl string) (UIConfig, error) {
		return LoadUIConfig(func(k string) string {
			if k == "PGLENS_TRACE_URL_TEMPLATE" {
				return tpl
			}
			return ""
		}, nil)
	}
	for _, bad := range []string{
		"https://tempo.example/trace",
		"javascript:alert('{trace_id}')",
		"ftp://tempo.example/{trace_id}",
		"https:///{trace_id}",
		"/trace/{trace_id}",
	} {
		_, err := load(bad)
		require.Error(t, err, bad)
	}
	const tpl = "https://tempo.example/explore?trace={trace_id}"
	cfg, err := load(tpl)
	require.NoError(t, err)
	require.Equal(t, tpl, cfg.TraceURLTemplate)
	cfg, err = load("")
	require.NoError(t, err)
	require.Empty(t, cfg.TraceURLTemplate)

	cfg = UIConfig{Password: "correct", SessionTTL: time.Hour, TraceURLTemplate: tpl}
	store := NewSessionStore(time.Hour)
	token, _, err := store.Create()
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	newSessionRoutes(cfg, store).ServeHTTP(rec, req)
	var status map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&status))
	require.Equal(t, tpl, status["trace_url_template"])

	rec = httptest.NewRecorder()
	newSessionRoutes(cfg, store).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/session", nil))
	require.NotContains(t, rec.Body.String(), "tempo.example", "a signed-out page does not learn the tracing URL")
}

// TestQTAG014: a trace_id filter that is not 32 lowercase hex is a 400 on logs
// and on plan samples; a valid one is bound, not spliced.
func TestQTAG014(t *testing.T) {
	inst := uuid.NewString()
	for _, bad := range []string{"xyz", "5BD66EF5095369C7B0D1F8F4BD33716A", "00000000000000000000000000000000", qtagTraceID + "'"} {
		rec := logsRequest(t, &mockPool{}, "/api/v1/logs?instance_id="+inst+"&trace_id="+bad)
		require.Equal(t, http.StatusBadRequest, rec.Code, bad)
		rec = httptest.NewRecorder()
		(&API{}).handlePlanSamples(rec, httptest.NewRequest(http.MethodGet, "/api/v1/plans/samples?instance_id="+inst+"&queryid=77&trace_id="+bad, nil))
		require.Equal(t, http.StatusBadRequest, rec.Code, bad)
	}
	pool := &sqlArgsPool{mockPool: &mockPool{queryResults: []*mockRows{{}}}}
	rec := logsRequest(t, pool, "/api/v1/logs?instance_id="+inst+"&trace_id="+qtagTraceID)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, pool.sql[0], "AND trace_id = $")
	require.NotContains(t, pool.sql[0], qtagTraceID)
}
