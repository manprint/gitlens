package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/command"
)

type commandFakePool struct {
	dbPool
	row      pgx.Row
	execErr  error
	execArgs []any
	tx       pgx.Tx
}

func (p *commandFakePool) QueryRow(context.Context, string, ...any) pgx.Row { return p.row }
func (p *commandFakePool) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	p.execArgs = args
	if p.execErr != nil {
		return pgconn.CommandTag{}, p.execErr
	}
	return pgconn.NewCommandTag("OK"), nil
}
func (p *commandFakePool) Begin(context.Context) (pgx.Tx, error) { return p.tx, nil }

type commandFakeTx struct {
	pgx.Tx
	row       pgx.Row
	revoked   bool // the agent's revoked_at flag, answered to the revocation lookup
	execCalls int
	committed bool
}

func (t *commandFakeTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	if strings.Contains(sql, "revoked_at") {
		return &mockRow{vals: []any{t.revoked}}
	}
	return t.row
}
func (t *commandFakeTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	t.execCalls++
	return pgconn.NewCommandTag("OK"), nil
}
func (t *commandFakeTx) Commit(context.Context) error {
	t.committed = true
	return nil
}
func (t *commandFakeTx) Rollback(context.Context) error { return nil }

func commandRouter(s *CommandService) *chi.Mux {
	r := chi.NewRouter()
	s.RegisterRoutes(r)
	return r
}

func TestEnqueue_ValidatesArgs(t *testing.T) {
	s := NewCommandService(&commandFakePool{}, NewAuth("secret"))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+uuid.NewString()+"/commands", bytes.NewBufferString(`{"kind":"explain","args":{}}`))
	rec := httptest.NewRecorder()
	commandRouter(s).ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "queryid")
}

func TestEnqueue_RejectsUnavailableAndMalformedRequests(t *testing.T) {
	tests := []struct {
		name string
		path string
		body string
		want int
	}{
		{"unavailable", "/api/v1/instances/" + uuid.NewString() + "/commands", `{"kind":"explain","args":{"queryid":7}}`, http.StatusServiceUnavailable},
		{"bad instance", "/api/v1/instances/not-a-uuid/commands", `{}`, http.StatusBadRequest},
		{"bad json", "/api/v1/instances/" + uuid.NewString() + "/commands", `{`, http.StatusBadRequest},
		{"unknown field", "/api/v1/instances/" + uuid.NewString() + "/commands", `{"kind":"explain","args":{"queryid":7},"extra":true}`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var pool dbPool = &commandFakePool{}
			if tt.name == "unavailable" {
				pool = nil
			}
			s := NewCommandService(pool, NewAuth("secret"))
			req := httptest.NewRequest(http.MethodPost, tt.path, bytes.NewBufferString(tt.body))
			rec := httptest.NewRecorder()
			commandRouter(s).ServeHTTP(rec, req)
			require.Equal(t, tt.want, rec.Code)
		})
	}
}

func TestEnqueue_SetsExpiry(t *testing.T) {
	instanceID, agentID := uuid.New(), uuid.New()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	p := &commandFakePool{row: &mockRow{vals: []any{[]byte(agentID.String()), int64(42), "default"}}}
	s := NewCommandService(p, NewAuth("secret"))
	s.now = func() time.Time { return now }
	s.ttl = 2 * time.Minute
	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+instanceID.String()+"/commands", bytes.NewBufferString(`{"kind":"explain","args":{"queryid":7}}`))
	rec := httptest.NewRecorder()
	commandRouter(s).ServeHTTP(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code)
	require.Len(t, p.execArgs, 9)
	require.Equal(t, now.Add(2*time.Minute), p.execArgs[7])
	require.Equal(t, "ui-session from 192.0.2.1", p.execArgs[8], "the request is audited with who made it")
}

// TestRequesterOf_NamesTheCredentialNotTheSecret: the audit tells a bearer
// token from a UI session, and sessions apart by a digest, without ever
// recording the session token.
func TestRequesterOf_NamesTheCredentialNotTheSecret(t *testing.T) {
	auth := NewAuth("secret")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/x/commands", nil)
	req.Header.Set("Authorization", "Bearer secret")
	require.Equal(t, "api-token from 192.0.2.1", requesterOf(req, auth))

	req = httptest.NewRequest(http.MethodPost, "/api/v1/instances/x/commands", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-token-value"})
	who := requesterOf(req, auth)
	require.Regexp(t, `^ui-session [0-9a-f]{8} from 192\.0\.2\.1$`, who)
	require.NotContains(t, who, "session-token-value")
}

func TestEnqueue_UnknownInstanceIs404(t *testing.T) {
	p := &commandFakePool{row: &mockRow{err: pgx.ErrNoRows}}
	s := NewCommandService(p, NewAuth("secret"))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+uuid.NewString()+"/commands", bytes.NewBufferString(`{"kind":"explain","args":{"queryid":7}}`))
	rec := httptest.NewRecorder()
	commandRouter(s).ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestPoll_RejectsForeignAgentWith403(t *testing.T) {
	s := NewCommandService(nil, NewAuth("secret"))
	requested, authenticated := uuid.New(), uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/"+requested.String()+"/commands", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set(commandAgentIDHeader, authenticated.String())
	rec := httptest.NewRecorder()
	commandRouter(s).ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestPoll_CapsWaitAt30s(t *testing.T) {
	wait, err := parseCommandWait("45s")
	require.NoError(t, err)
	require.Equal(t, maxCommandWait, wait)
}

func TestPoll_RejectsUnauthenticatedAndInvalidRequests(t *testing.T) {
	agentID := uuid.NewString()
	tests := []struct {
		name   string
		mutate func(*http.Request)
		want   int
	}{
		{"missing auth", func(_ *http.Request) {}, http.StatusUnauthorized},
		{"missing identity", func(r *http.Request) { r.Header.Set("Authorization", "Bearer secret") }, http.StatusForbidden},
		{"invalid agent id", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer secret")
			r.Header.Set(commandAgentIDHeader, "not-a-uuid")
			r.URL.Path = "/api/v1/agents/not-a-uuid/commands"
		}, http.StatusBadRequest},
		{"invalid wait", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer secret")
			r.Header.Set(commandAgentIDHeader, agentID)
			r.URL.RawQuery = "wait=nope"
		}, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewCommandService(nil, NewAuth("secret"))
			req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/"+agentID+"/commands", nil)
			tt.mutate(req)
			rec := httptest.NewRecorder()
			commandRouter(s).ServeHTTP(rec, req)
			require.Equal(t, tt.want, rec.Code)
		})
	}
}

func TestPoll_ReturnsNoContentOnTimeout(t *testing.T) {
	s := NewCommandService(nil, NewAuth("secret"))
	agentID := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/"+agentID.String()+"/commands?wait=0s", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set(commandAgentIDHeader, agentID.String())
	rec := httptest.NewRecorder()
	commandRouter(s).ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)
}

// A plan of a wide view runs to megabytes. The result endpoint used to share
// the 1 MiB limit of command requests, so such a result was refused as invalid
// JSON and the command expired with no answer on record.
func TestResult_AcceptsAPlanLargerThanARequestBody(t *testing.T) {
	commandID, instanceID, claimToken := uuid.New(), uuid.New(), uuid.New()
	tx := &commandFakeTx{row: &mockRow{vals: []any{"default", []byte(instanceID.String()), int64(0), command.KindExplain, []byte(`{"queryid":7}`), "api"}}}
	s := NewCommandService(&commandFakePool{tx: tx}, NewAuth("secret"))
	body := map[string]any{"claim_token": claimToken, "outcome": "ok", "result": map[string]any{"plan": map[string]any{"Node Type": strings.Repeat("x", 1_500_000)}, "plan_hash": "0123456789abcdef"}}
	data, err := json.Marshal(body)
	require.NoError(t, err)
	require.Greater(t, len(data), 1<<20)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/commands/"+commandID.String()+"/result", bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	commandRouter(s).ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.True(t, tx.committed)
}

// A result over command.MaxResultBytes is refused as a size problem, not as
// malformed JSON, so the cause is visible to whoever reads the response.
func TestResult_RefusesAResultOverTheLimitAsTooLarge(t *testing.T) {
	commandID := uuid.New()
	tx := &commandFakeTx{row: &mockRow{err: pgx.ErrNoRows}}
	s := NewCommandService(&commandFakePool{tx: tx}, NewAuth("secret"))
	body := `{"claim_token":"` + uuid.NewString() + `","outcome":"ok","result":"` + strings.Repeat("x", command.MaxResultBytes) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/commands/"+commandID.String()+"/result", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	commandRouter(s).ServeHTTP(rec, req)
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.Contains(t, rec.Body.String(), "payload too large")
	require.Equal(t, 0, tx.execCalls)
}

// Command requests keep the 1 MiB limit: only results may be larger.
func TestEnqueue_RefusesARequestOverOneMiB(t *testing.T) {
	s := NewCommandService(&commandFakePool{}, NewAuth("secret"))
	body := `{"kind":"cancel","args":{"pid":42,"datname":"` + strings.Repeat("x", 1<<20) + `"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+uuid.NewString()+"/commands", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	commandRouter(s).ServeHTTP(rec, req)
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestResult_MismatchedClaimTokenIsConflict(t *testing.T) {
	commandID := uuid.New()
	tx := &commandFakeTx{row: &mockRow{err: pgx.ErrNoRows}}
	s := NewCommandService(&commandFakePool{tx: tx}, NewAuth("secret"))
	body := `{"claim_token":"` + uuid.NewString() + `","outcome":"ok","result":{}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/commands/"+commandID.String()+"/result", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	commandRouter(s).ServeHTTP(rec, req)
	require.Equal(t, http.StatusConflict, rec.Code)
	require.False(t, tx.committed)
	require.Equal(t, 0, tx.execCalls)
}

func TestGetAndResult_RejectUnavailableOrMalformedRequests(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{"get unavailable", http.MethodGet, "/api/v1/commands/" + uuid.NewString(), "", http.StatusServiceUnavailable},
		{"get bad id", http.MethodGet, "/api/v1/commands/not-a-uuid", "", http.StatusBadRequest},
		{"result unauthorized", http.MethodPost, "/api/v1/commands/" + uuid.NewString() + "/result", `{}`, http.StatusUnauthorized},
		{"result unavailable", http.MethodPost, "/api/v1/commands/" + uuid.NewString() + "/result", `{}`, http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewCommandService(nil, NewAuth("secret"))
			if tc.name == "get bad id" {
				s = NewCommandService(&commandFakePool{}, NewAuth("secret"))
			}
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			if tc.name == "result unavailable" {
				req.Header.Set("Authorization", "Bearer secret")
			}
			rec := httptest.NewRecorder()
			commandRouter(s).ServeHTTP(rec, req)
			require.Equal(t, tc.want, rec.Code)
		})
	}
}

func TestCommandHelpers_ValidateInputs(t *testing.T) {
	t.Setenv("PGLENS_COMMAND_TTL", "")
	require.Equal(t, defaultCommandTTL, commandTTL())
	t.Setenv("PGLENS_COMMAND_TTL", "invalid")
	require.Equal(t, defaultCommandTTL, commandTTL())
	t.Setenv("PGLENS_COMMAND_TTL", "2m")
	require.Equal(t, 2*time.Minute, commandTTL())

	for _, raw := range []string{"-1s", "nope"} {
		_, err := parseCommandWait(raw)
		require.Error(t, err)
	}
	wait, err := parseCommandWait("")
	require.NoError(t, err)
	require.Zero(t, wait)

	for _, req := range []resultRequest{
		{},
		{ClaimToken: uuid.New(), Outcome: "unknown"},
		{ClaimToken: uuid.New(), Outcome: "ok", Error: "bad"},
		{ClaimToken: uuid.New(), Outcome: "error"},
		{ClaimToken: uuid.New(), Outcome: "error", Result: json.RawMessage(`{}`), Error: "bad"},
	} {
		require.Error(t, validateResult(req))
	}
	require.NoError(t, validateResult(resultRequest{ClaimToken: uuid.New(), Outcome: "error", Error: "bad"}))
	require.Nil(t, nullableJSON(nil))
	require.Equal(t, json.RawMessage(`{}`), nullableJSON(json.RawMessage(`{}`)))
	require.Nil(t, nullableText(""))
	require.Equal(t, "bad", nullableText("bad"))
}

func TestSleepContextHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, sleepContext(ctx, time.Hour), context.Canceled)
}

func TestSleepContextReturnsWhenTimerFires(t *testing.T) {
	require.NoError(t, sleepContext(context.Background(), time.Nanosecond))
}

func TestResult_WritesAuditRow(t *testing.T) {
	commandID, instanceID, claimToken := uuid.New(), uuid.New(), uuid.New()
	tx := &commandFakeTx{row: &mockRow{vals: []any{"default", []byte(instanceID.String()), int64(0), command.KindExplain, []byte(`{"queryid":7}`), "api"}}}
	s := NewCommandService(&commandFakePool{tx: tx}, NewAuth("secret"))
	body := map[string]any{"claim_token": claimToken, "outcome": "ok", "result": map[string]any{"Plan": "Index Scan"}}
	data, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/commands/"+commandID.String()+"/result", bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	commandRouter(s).ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.True(t, tx.committed)
	require.Equal(t, 1, tx.execCalls)
}

// A revoked agent gets no commands, as its pushes are refused. The poll checks
// the agent before it claims anything.
func TestPoll_RefusesARevokedAgentWith401(t *testing.T) {
	agentID := uuid.New()
	pool := &commandFakePool{row: &mockRow{vals: []any{true}}}
	s := NewCommandService(pool, NewAuth("secret"))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/"+agentID.String()+"/commands?wait=0s", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set(commandAgentIDHeader, agentID.String())
	rec := httptest.NewRecorder()
	commandRouter(s).ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, rec.Body.String(), "revoked")
}

// A result from a revoked agent is refused before anything is written, even
// for a claim the agent held before its revocation.
func TestResult_RefusesAResultFromARevokedAgent(t *testing.T) {
	commandID, instanceID, claimToken := uuid.New(), uuid.New(), uuid.New()
	tx := &commandFakeTx{revoked: true, row: &mockRow{vals: []any{"default", []byte(instanceID.String()), int64(0), command.KindCancel, []byte(`{"pid":42}`), "api"}}}
	s := NewCommandService(&commandFakePool{tx: tx}, NewAuth("secret"))
	body := `{"claim_token":"` + claimToken.String() + `","outcome":"ok","result":{"signalled":true}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/commands/"+commandID.String()+"/result", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	commandRouter(s).ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, rec.Body.String(), "revoked")
	require.False(t, tx.committed)
	require.Equal(t, 0, tx.execCalls)
}
