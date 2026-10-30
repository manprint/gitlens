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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/store"
)

// renameRequest sends a PATCH with a JSON body through the API routes.
func renameRequest(t *testing.T, api *API, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodPatch, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// requireAPIError checks the status and the error field of an error answer.
func requireAPIError(t *testing.T, rec *httptest.ResponseRecorder, status int, msg string) {
	t.Helper()
	require.Equal(t, status, rec.Code, rec.Body.String())
	var got apiError
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, msg, got.Error)
}

// CLNAME-001: a plain name is kept, surrounding blanks are trimmed, and an
// empty, all-blank or null name clears the name.
func TestCLNAME001(t *testing.T) {
	t.Parallel()
	got, err := validateName(strPtr("prod-eu"))
	require.NoError(t, err)
	require.Equal(t, "prod-eu", *got)

	got, err = validateName(strPtr("  prod  "))
	require.NoError(t, err)
	require.Equal(t, "prod", *got)

	for _, clear := range []*string{nil, strPtr(""), strPtr("   "), strPtr("\t \n")} {
		got, err = validateName(clear)
		require.NoError(t, err)
		require.Nil(t, got)
	}

	got, err = validateName(strPtr(strings.Repeat("a", 64)))
	require.NoError(t, err)
	require.Len(t, *got, 64)
}

// CLNAME-002: a name over 64 characters, a control character inside the name,
// an unknown field, a missing field and a non-string value are refused.
func TestCLNAME002(t *testing.T) {
	t.Parallel()
	_, err := validateName(strPtr(strings.Repeat("a", 65)))
	require.ErrorContains(t, err, "64 characters")
	_, err = validateName(strPtr("a\tb"))
	require.ErrorContains(t, err, "control or invisible characters")
	// Invisible format characters and separators would make look-alike names.
	for _, name := range []string{"a\u200bb", "a\u202eb", "a\ufeffb", "a\u2028b"} {
		_, err = validateName(strPtr(name))
		require.ErrorContains(t, err, "control or invisible characters", "%q", name)
	}
	_, err = validateName(strPtr("a\u0000b"))
	require.ErrorContains(t, err, "control or invisible characters")
	_, err = validateName(strPtr("a\xffb"))
	require.ErrorContains(t, err, "UTF-8")

	api := &API{pool: &mockPool{}}
	target := "/api/v1/clusters/" + pgtype.ClusterID(42).String()
	requireAPIError(t, renameRequest(t, api, target, `{"name":"x","extra":1}`), http.StatusBadRequest, "invalid body")
	requireAPIError(t, renameRequest(t, api, target, `{"name":"`+strings.Repeat("é", 65)+`"}`), http.StatusBadRequest, "invalid name")
	requireAPIError(t, renameRequest(t, api, target, `{"name":"a\tb"}`), http.StatusBadRequest, "invalid name")
	requireAPIError(t, renameRequest(t, api, target, `{}`), http.StatusBadRequest, "invalid name")
	requireAPIError(t, renameRequest(t, api, target, `{"name":5}`), http.StatusBadRequest, "invalid name")
	requireAPIError(t, renameRequest(t, api, target, `{"name":`), http.StatusBadRequest, "invalid body")

	// A form body is refused unread, as on every JSON write.
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodPatch, target, strings.NewReader(`name=x`))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnsupportedMediaType, rec.Code)

	// Nothing reached the store.
	pool := api.pool.(*mockPool)
	require.Zero(t, pool.queryRowCalls)
}

// CLNAME-003: a Unicode name is accepted and counted in characters, not bytes.
func TestCLNAME003(t *testing.T) {
	t.Parallel()
	got, err := validateName(strPtr("Produzione è ok"))
	require.NoError(t, err)
	require.Equal(t, "Produzione è ok", *got)

	got, err = validateName(strPtr(strings.Repeat("è", 64)))
	require.NoError(t, err)
	require.Equal(t, 64, len([]rune(*got)))
}

// CLNAME-004: an unknown cluster is 404, a malformed id 400, no store 503; a
// rename answers the cluster as the list shows it.
func TestCLNAME004(t *testing.T) {
	t.Parallel()
	cid := pgtype.ClusterID(18446744073709551615) // the top bit set: stored negative
	target := "/api/v1/clusters/" + cid.String()

	notFound := &mockPool{queryRowVals: []*mockRow{{err: pgx.ErrNoRows}}}
	requireAPIError(t, renameRequest(t, &API{pool: notFound}, target, `{"name":"prod-eu"}`), http.StatusNotFound, "cluster not found")

	requireAPIError(t, renameRequest(t, &API{pool: &mockPool{}}, "/api/v1/clusters/not-a-number", `{"name":"prod-eu"}`), http.StatusBadRequest, "invalid cluster_id")
	requireAPIError(t, renameRequest(t, &API{pool: &mockPool{}}, "/api/v1/clusters/-1", `{"name":"prod-eu"}`), http.StatusBadRequest, "invalid cluster_id")
	requireAPIError(t, renameRequest(t, &API{}, target, `{"name":"prod-eu"}`), http.StatusServiceUnavailable, "store unavailable")

	cidDB := store.ToDB(cid)
	iid := uuid.New()
	pool := &mockPool{
		queryRowVals: []*mockRow{{vals: []any{cidDB, "prod-eu", "system_identifier"}}},
		queryResults: []*mockRows{
			{rows: [][]any{{cidDB, iid.String(), "10.0.0.1", 5432, "primary", 170000, "T0", time.Now(), "db-1"}}},
		},
	}
	rec := renameRequest(t, &API{pool: pool}, target, `{"name":"  prod-eu  "}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got clusterResp
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, cid.String(), got.ClusterID)
	require.NotNil(t, got.Name)
	require.Equal(t, "prod-eu", *got.Name)
	require.Equal(t, "ok", got.Health)
	require.Equal(t, 1, got.InstanceCount)
	require.Equal(t, iid.String(), *got.Primary)
	require.Equal(t, "db-1", *got.Instances[0].DisplayName)

	// The update matched, but the last instance went away before the read.
	gone := &mockPool{queryRowVals: []*mockRow{{vals: []any{cidDB, nil, "manual"}}}}
	requireAPIError(t, renameRequest(t, &API{pool: gone}, target, `{"name":null}`), http.StatusNotFound, "cluster not found")
}

// CLNAME-004: the rename is a write behind the credential gate and the
// cross-origin refusal of the production router.
func TestCLNAME004GatedWrite(t *testing.T) {
	t.Parallel()
	router, token := securityRouter(t)
	send := func(target string, cookie bool, headers map[string]string) int {
		req := httptest.NewRequest(http.MethodPatch, target, strings.NewReader(`{"name":"x","display_name":"x"}`))
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
	for _, target := range []string{
		"/api/v1/clusters/" + pgtype.ClusterID(42).String(),
		"/api/v1/instances/" + uuid.NewString(),
	} {
		require.Equal(t, http.StatusUnauthorized, send(target, false, nil), target)
		require.Equal(t, http.StatusForbidden, send(target, true, map[string]string{"Sec-Fetch-Site": "cross-site"}), target)
		require.Equal(t, http.StatusForbidden, send(target, true, map[string]string{"Origin": "https://evil.example"}), target)
	}
	// Past the gate, an unknown field is the client's error, before the store is
	// asked; a body that fits reaches the missing store.
	require.Equal(t, http.StatusBadRequest, send("/api/v1/clusters/"+pgtype.ClusterID(42).String(), true, map[string]string{"Sec-Fetch-Site": "same-origin"}))
	require.Equal(t, http.StatusBadRequest, send("/api/v1/instances/"+uuid.NewString(), true, map[string]string{"Sec-Fetch-Site": "same-origin"}))
}

// CLNAME-005: a unique violation from the store is a 409 that names the reason.
func TestCLNAME005(t *testing.T) {
	t.Parallel()
	pool := &mockPool{queryRowVals: []*mockRow{{err: &pgconn.PgError{Code: "23505", ConstraintName: "clusters_name_uniq"}}}}
	rec := renameRequest(t, &API{pool: pool}, "/api/v1/clusters/"+pgtype.ClusterID(7).String(), `{"name":"PROD-EU"}`)
	requireAPIError(t, rec, http.StatusConflict, "name already in use")

	other := &mockPool{queryRowVals: []*mockRow{{err: &pgconn.PgError{Code: "57014"}}}}
	rec = renameRequest(t, &API{pool: other}, "/api/v1/clusters/"+pgtype.ClusterID(7).String(), `{"name":"x"}`)
	requireAPIError(t, rec, http.StatusInternalServerError, "cluster rename failed")
}

// INAME-001: an instance rename trims the display name, answers the instance
// summary, and refuses a bad name or an unknown field before the store.
func TestINAME001(t *testing.T) {
	t.Parallel()
	iid := uuid.New()
	cid := pgtype.ClusterID(9002)
	target := "/api/v1/instances/" + iid.String()
	pool := &mockPool{queryRowVals: []*mockRow{
		{vals: []any{iid.String(), store.ToDB(cid), "10.0.0.9", 5432, "primary", 170000, "T0", time.Now(), "Database è 1"}},
		{vals: []any{iid.String(), store.ToDB(cid), "10.0.0.9", 5432, "primary", 170000, "T0", time.Now().Add(-time.Hour), nil}},
	}}
	rec := renameRequest(t, &API{pool: pool}, target, `{"display_name":" Database è 1 "}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got instanceListResp
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, iid.String(), got.InstanceID)
	require.Equal(t, cid.String(), got.ClusterID)
	require.Equal(t, "Database è 1", *got.DisplayName)
	require.True(t, got.Up)

	rec = renameRequest(t, &API{pool: pool}, target, `{"display_name":null}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"display_name":null`)
	require.False(t, strings.Contains(rec.Body.String(), `"up":true`))

	empty := &mockPool{}
	requireAPIError(t, renameRequest(t, &API{pool: empty}, target, `{"display_name":"x","name":"y"}`), http.StatusBadRequest, "invalid body")
	requireAPIError(t, renameRequest(t, &API{pool: empty}, target, `{"display_name":"`+strings.Repeat("a", 65)+`"}`), http.StatusBadRequest, "invalid name")
	requireAPIError(t, renameRequest(t, &API{pool: empty}, target, `{"display_name":"a\nb"}`), http.StatusBadRequest, "invalid name")
	requireAPIError(t, renameRequest(t, &API{pool: empty}, target, `{"name":"x"}`), http.StatusBadRequest, "invalid body")
	require.Zero(t, empty.queryRowCalls)
}

// INAME-002: an unknown instance is 404, a malformed id 400, no store 503.
func TestINAME002(t *testing.T) {
	t.Parallel()
	target := "/api/v1/instances/" + uuid.NewString()
	notFound := &mockPool{queryRowVals: []*mockRow{{err: pgx.ErrNoRows}}}
	requireAPIError(t, renameRequest(t, &API{pool: notFound}, target, `{"display_name":"db-1"}`), http.StatusNotFound, "instance not found")
	requireAPIError(t, renameRequest(t, &API{pool: &mockPool{}}, "/api/v1/instances/bad-uuid", `{"display_name":"db-1"}`), http.StatusBadRequest, "invalid instance_id")
	requireAPIError(t, renameRequest(t, &API{}, target, `{"display_name":"db-1"}`), http.StatusServiceUnavailable, "store unavailable")
}

// INAME-003: a unique violation from the store is a 409.
func TestINAME003(t *testing.T) {
	t.Parallel()
	pool := &mockPool{queryRowVals: []*mockRow{{err: &pgconn.PgError{Code: "23505", ConstraintName: "instances_display_name_uniq"}}}}
	rec := renameRequest(t, &API{pool: pool}, "/api/v1/instances/"+uuid.NewString(), `{"display_name":"DB-1"}`)
	requireAPIError(t, rec, http.StatusConflict, "name already in use")
}
