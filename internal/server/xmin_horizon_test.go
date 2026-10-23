package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/redact"
	"github.com/stretchr/testify/require"
)

// The server applies the agent's identity rule to the oldest xmin holder again:
// a standby's application_name and a prepared transaction's gid are cut and
// withheld, and the other identities and the other fields are kept.
func TestWithholdXminIdentity(t *testing.T) {
	t.Parallel()
	gid := strings.Repeat("g", 150)
	got := withholdXminIdentity([]byte(`{"holder_kind":"prepared","identity":"` + gid + `","age_xids":9,"age_seconds":5.5}`))
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(got, &decoded))
	require.Equal(t, strings.Repeat("g", 64), decoded["identity"])
	require.Equal(t, float64(9), decoded["age_xids"])
	require.Equal(t, 5.5, decoded["age_seconds"])

	got = withholdXminIdentity([]byte(`{"holder_kind":"standby","identity":"postgresql://u:p@h/db","age_xids":3}`))
	require.JSONEq(t, `{"holder_kind":"standby","identity":"`+redact.Withheld+`","age_xids":3}`, string(got))

	clean := []byte(`{"holder_kind":"session","identity":"4242","age_xids":7}`)
	require.Equal(t, clean, withholdXminIdentity(clean))
	require.JSONEq(t, `{}`, string(withholdXminIdentity([]byte(`"not an object"`))))
}

// The activity endpoint answers the oldest holder of the latest sample, with
// its kind, identity, ages and the time of the sample.
func TestActivityAPI_ReportsOldestXminHolder(t *testing.T) {
	id := uuid.New()
	sampled := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	pool := &mockPool{queryRowVals: []*mockRow{{vals: []any{
		[]byte(`{"holder_kind":"slot","identity":"cdc","age_xids":120,"age_seconds":null}`), sampled,
	}}}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+id.String()+"/activity", http.NoBody)
	resp := httptest.NewRecorder()
	r := chi.NewRouter()
	(&API{pool: pool}).registerContentionRoutes(r)
	r.ServeHTTP(resp, req)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	var got activityAPIResponse
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	require.NotNil(t, got.XminHorizon)
	require.Equal(t, "slot", got.XminHorizon.HolderKind)
	require.Equal(t, "cdc", got.XminHorizon.Identity)
	require.Equal(t, int64(120), got.XminHorizon.AgeXIDs)
	require.Nil(t, got.XminHorizon.AgeSeconds)
	require.True(t, sampled.Equal(got.XminHorizon.SampledAt))
}

// With no fresh holder the field is null, and the rest of the answer is as it was.
func TestActivityAPI_NoFreshXminHolderIsNull(t *testing.T) {
	id := uuid.New()
	pool := &mockPool{queryRowVals: []*mockRow{{err: pgx.ErrNoRows}}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+id.String()+"/activity", http.NoBody)
	resp := httptest.NewRecorder()
	r := chi.NewRouter()
	(&API{pool: pool}).registerContentionRoutes(r)
	r.ServeHTTP(resp, req)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.Contains(t, resp.Body.String(), `"xmin_horizon":null`)
}

// A holder fact that does not decode is logged and left out, not served.
func TestActivityAPI_UnreadableXminHolderIsLeftOut(t *testing.T) {
	id := uuid.New()
	pool := &mockPool{queryRowVals: []*mockRow{{vals: []any{[]byte(`[1,2]`), time.Now()}}}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+id.String()+"/activity", http.NoBody)
	resp := httptest.NewRecorder()
	r := chi.NewRouter()
	(&API{pool: pool}).registerContentionRoutes(r)
	r.ServeHTTP(resp, req)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.Contains(t, resp.Body.String(), `"xmin_horizon":null`)
}
