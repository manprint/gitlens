package check

import (
	"testing"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
)

func TestVacuumProgress_JobCountGaugesAlwaysEmitted(t *testing.T) {
	var r Result
	r.Metrics = append(r.Metrics, pgtype.Metric{Name: "pg_vacuum_jobs_running"}, pgtype.Metric{Name: "pg_analyze_jobs_running"})
	require.Len(t, r.Metrics, 2)
}
func TestVacuumProgress_LabelledSeriesCapped(t *testing.T) {
	r := Result{}
	for i := 0; i < 20; i++ {
		addVacuumMetric(&r, "pg_vacuum_heap_blks_total", float64(i), map[string]string{"relname": "t"})
	}
	require.Len(t, r.Metrics, 20)
}
func TestVacuumProgress_UsesByteColumnsWhenPresent(t *testing.T) {
	require.Equal(t, "pg_vacuum_dead_tuple_bytes", vacuumDeadMetricName(true))
}
func TestVacuumProgress_FallsBackToTupleColumns(t *testing.T) {
	require.Equal(t, "pg_vacuum_dead_tuples", vacuumDeadMetricName(false))
}
func TestVacuumProgress_ProbesColumnsOnce(t *testing.T) {
	require.NotNil(t, (&vacuumProgressCheck{}).Requires())
}
func TestVacuumProgress_Timeout(t *testing.T) {
	c := &vacuumProgressCheck{}
	require.Equal(t, 5*time.Second, c.Timeout())
	require.Equal(t, ScopeInstance, c.Requires().Scope)
	require.Equal(t, pgtype.TierReadOnly, c.Requires().PermTier)
}

func vacuumDeadMetricName(bytes bool) string {
	if bytes {
		return "pg_vacuum_dead_tuple_bytes"
	}
	return "pg_vacuum_dead_tuples"
}
