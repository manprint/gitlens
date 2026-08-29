package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

type auditPool struct {
	dbPool
	rows pgx.Rows
	args []any
}

func (p *auditPool) Query(_ context.Context, _ string, args ...any) (pgx.Rows, error) {
	p.args = args
	return p.rows, nil
}

func TestAuditAPI_LimitCapped(t *testing.T) {
	p := &auditPool{rows: &mockRows{rows: [][]any{}}}
	r := chi.NewRouter()
	(&API{pool: p}).RegisterRoutes(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+uuid.NewString()+"/command-audit?limit=9999", http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, p.args, 2)
	require.Equal(t, 500, p.args[1])
}

func TestAuditAPI_NoMutationRoutesRegistered(t *testing.T) {
	r := chi.NewRouter()
	(&API{pool: nil}).RegisterRoutes(r)
	path := "/api/v1/instances/{id}/command-audit"
	seen := map[string]bool{}
	err := chi.Walk(r, func(method, routePath string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if routePath == path {
			seen[method] = true
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"GET": true}, seen)
}

func TestAuditAPI_DecodesRows(t *testing.T) {
	instanceID, commandID := uuid.New(), uuid.New()
	p := &auditPool{rows: &mockRows{rows: [][]any{{int64(1), []byte(commandID.String()), []byte(instanceID.String()), "cancel", []byte(`{"pid":42}`), "api", time.Now(), "ok", nil}}}}
	r := chi.NewRouter()
	(&API{pool: p}).RegisterRoutes(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+instanceID.String()+"/command-audit", http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"outcome":"ok"`)
}
