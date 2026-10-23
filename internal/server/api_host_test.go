package server

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func hostTestRouter(api *API) http.Handler {
	r := chi.NewRouter()
	api.registerHostRoutes(r)
	return r
}

func TestHostAPI_RemoteInstanceReportsUnavailable(t *testing.T) {
	id := uuid.New()
	rec := httptest.NewRecorder()
	hostTestRouter(NewAPI(nil)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+id.String()+"/host", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var got map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, false, got["available"])
	require.Equal(t, "target is not local to any agent", got["reason"])
	// No sample was taken, so there is no time to report. The zero time is
	// not a sample, and omitempty does not drop a struct field.
	_, hasSampled := got["sampled_at"]
	require.False(t, hasSampled, rec.Body.String())
	_, hasMemory := got["host_mem_total_bytes"]
	require.False(t, hasMemory)
}

func TestHostAPI_OmitsAbsentFields(t *testing.T) {
	data := map[string]any{"instance_id": uuid.NewString(), "available": true, "source": "host", "sampled_at": time.Now(), "stale": false}
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, data)
	var got map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	_, present := got["host_cpu_used_ratio"]
	require.False(t, present)
}

// A host metric that cannot be encoded must fail the request. The response
// used to be a 200 with the body null, which a client reads as no host data.
func TestHostAPI_UnencodableMetricIsServerError(t *testing.T) {
	id := uuid.New()
	pool := &mockPool{queryResults: []*mockRows{{rows: [][]any{{time.Now(), "host_load1", math.NaN()}}}}}
	rec := httptest.NewRecorder()
	hostTestRouter(&API{pool: pool}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+id.String()+"/host", nil))
	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
}

func TestHostAPI_SourceIsDecoded(t *testing.T) {
	require.Equal(t, "host", hostSource(0))
	require.Equal(t, "cgroup_v1", hostSource(1))
	require.Equal(t, "cgroup_v2", hostSource(2))
	require.Equal(t, "host", hostSource(99))
}

func TestHostAPI_StalenessThreshold(t *testing.T) {
	require.Equal(t, 90*time.Second, hostMetricStaleAfter)
}

func TestPipeline_HostMetricsGoToGenericTable(t *testing.T) {
	require.Equal(t, "metrics", destinationTable("host"))
}

func TestINTHOST003_HostAPIUnavailableIsExplicit(t *testing.T) {
	TestHostAPI_RemoteInstanceReportsUnavailable(t)
}
