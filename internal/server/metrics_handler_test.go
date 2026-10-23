package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/manprint/pglens/internal/check"
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
	instID := uuid.NewString()
	IncCheckError(instID, "activity")
	IncCheckOK(instID, "activity")

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	MetricsHandler()(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("Content-Type"), "text/plain")

	body := w.Body.String()
	require.Contains(t, body, "# TYPE pglens_up gauge")
	require.Contains(t, body, "# TYPE pglens_check_error_total counter")
	require.Contains(t, body, fmt.Sprintf(`pglens_check_error_total{instance_id="%s",check="activity"} 1`, instID))
	// The success counter is what makes the E2E "no unexpected check errors"
	// invariant decidable: without it the harness cannot tell a check that is
	// broken from one whose target was briefly taken away.
	require.Contains(t, body, "# TYPE pglens_check_ok_total counter")
	require.Contains(t, body, fmt.Sprintf(`pglens_check_ok_total{instance_id="%s",check="activity"} 1`, instID))
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

// A check name the agent invents is counted under "other", so it neither reaches
// /metrics nor adds a series of its own.
func TestCheckLabel_UnknownNameIsBucketed(t *testing.T) {
	t.Parallel()
	instID := uuid.NewString()
	IncCheckError(instID, "made_up_check_name_for_test")
	IncCheckError(instID, "another_made_up_check_name")

	require.Equal(t, 2.0, pglensCheckErrorTotal.Get(checkKey{instance: instID, check: "other"}))
	require.Zero(t, pglensCheckErrorTotal.Get(checkKey{instance: instID, check: "made_up_check_name_for_test"}))

	w := httptest.NewRecorder()
	MetricsHandler()(w, httptest.NewRequest("GET", "/metrics", nil))
	body := w.Body.String()
	require.Contains(t, body, fmt.Sprintf(`pglens_check_error_total{instance_id="%s",check="other"} 2`, instID))
	require.NotContains(t, body, "made_up_check_name_for_test")
}

// The counters are per instance, so one instance failing a check is not hidden
// by another instance succeeding at it.
func TestCheckCounters_KeyedByInstance(t *testing.T) {
	t.Parallel()
	failing, healthy := uuid.NewString(), uuid.NewString()
	IncCheckError(failing, "activity")
	IncCheckOK(healthy, "activity")

	require.Equal(t, 1.0, pglensCheckErrorTotal.Get(checkKey{instance: failing, check: "activity"}))
	require.Zero(t, pglensCheckOKTotal.Get(checkKey{instance: failing, check: "activity"}))
	require.Zero(t, pglensCheckErrorTotal.Get(checkKey{instance: healthy, check: "activity"}))
	require.Equal(t, 1.0, pglensCheckOKTotal.Get(checkKey{instance: healthy, check: "activity"}))
}

// The known names must cover every check an agent registers. A check missing
// from the list would be counted as "other" with nothing to say so.
func TestKnownChecks_CoverTheRegisteredChecks(t *testing.T) {
	t.Parallel()
	for _, c := range check.All() {
		require.True(t, knownChecks[c.Name()], "check %q is registered by the agent but missing from knownChecks", c.Name())
	}
	for _, name := range []string{"agent_self", "host"} {
		require.True(t, knownChecks[name], "the agent's own result %q is missing from knownChecks", name)
	}
}

// Prometheus defines three escapes for a label value, and a tab is allowed as it
// is. %q writes a tab as an escape the text format does not define, and one
// invalid line fails the whole scrape.
func TestPromLabelValue_EscapesWhatTheTextFormatDefines(t *testing.T) {
	t.Parallel()
	require.Equal(t, `plain`, promLabelValue(`plain`))
	require.Equal(t, `back\\slash \"quote\" line\nfeed`, promLabelValue("back\\slash \"quote\" line\nfeed"))
	require.Equal(t, "tab\there", promLabelValue("tab\there"))
}
