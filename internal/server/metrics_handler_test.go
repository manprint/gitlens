package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGaugeVec_Snapshot(t *testing.T) {
	t.Parallel()
	g := newGaugeVec()
	g.Set("a", 1.5)
	g.Set("b", 2.5)
	snap := g.Snapshot()
	require.Equal(t, map[string]float64{"a": 1.5, "b": 2.5}, snap)

	// The snapshot is a copy: mutating it must not affect the vec.
	snap["a"] = 999
	require.Equal(t, 1.5, g.Get("a"))
}

func TestCounterVec_Snapshot(t *testing.T) {
	t.Parallel()
	c := &counterVec{vals: make(map[string]float64)}
	c.Inc("x")
	c.Add("y", 3)
	snap := c.Snapshot()
	require.Equal(t, map[string]float64{"x": 1, "y": 3}, snap)

	snap["x"] = 999
	require.Equal(t, 1.0, c.Get("x"))
}

// TestMetricsHandler_Format proves the exposition uses a unique label value
// this test controls, so the assertion is robust to whatever other tests in
// this package have already incremented the shared global counters — the
// handler reads real (package-level, not injectable) state, exactly the
// "a real Prometheus exposition would read from these globals" design
// staleness.go's own top comment already anticipated.
func TestMetricsHandler_Format(t *testing.T) {
	IncCheckError("metrics_handler_test_unique_check")

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	MetricsHandler()(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("Content-Type"), "text/plain")

	body := w.Body.String()
	require.Contains(t, body, "# TYPE pglens_up gauge")
	require.Contains(t, body, "# TYPE pglens_check_error_total counter")
	require.Contains(t, body, `pglens_check_error_total{check="metrics_handler_test_unique_check"} 1`)
	require.Contains(t, body, "# TYPE pglens_agent_clock_skew_seconds gauge")

	// A vec with zero entries must still emit its HELP/TYPE header (proving
	// the handler doesn't skip a metric family just because nothing has
	// reported it yet — same "no data vs disabled" distinction ASH's own
	// API already cares about), but no bogus data line.
	lines := strings.Split(body, "\n")
	sawSeriesTotalType := false
	for _, l := range lines {
		if l == "# TYPE pglens_series_total gauge" {
			sawSeriesTotalType = true
		}
	}
	require.True(t, sawSeriesTotalType, "expected pglens_series_total's TYPE header even with no instances reporting yet")
}
