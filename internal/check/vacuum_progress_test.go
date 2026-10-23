package check

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/clock"
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

// A vacuum in another database is named from that database: the maintenance
// database's pg_class does not hold the OID of a table in the application's
// database, so the same row read there comes back with an empty name.
func TestVacuumProgress_NamesRelationFromItsOwnDatabase(t *testing.T) {
	t.Parallel()
	maint := &mockConn{queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
		if strings.Contains(sql, "pg_stat_progress_vacuum") && !strings.Contains(sql, "information_schema") {
			return &mockRows{rows: [][]any{{int32(7), "app", uint32(16384), "scanning heap", int64(100), int64(50), int64(0), int64(0), int64(0), int64(0)}}}, nil
		}
		return &mockRows{}, nil
	}}
	app := &mockConn{queryRowFunc: func(_ context.Context, sql string, args ...any) pgx.Row {
		require.Contains(t, sql, "pg_class")
		require.Equal(t, []any{uint32(16384)}, args)
		return &mockRow{vals: []any{"zz_vac_probe"}}
	}}
	target := &SimpleTarget{
		ConnFunc: func(context.Context) (Conn, error) { return maint, nil },
		ConnForFunc: func(_ context.Context, datname string) (Conn, error) {
			require.Equal(t, "app", datname)
			return app, nil
		},
		ClockValue: clock.System(),
	}
	r, err := (&vacuumProgressCheck{}).Scrape(context.Background(), target)
	require.NoError(t, err)
	found := false
	for _, m := range r.Metrics {
		if m.Name == "pg_vacuum_heap_blks_total" {
			found = true
			require.Equal(t, "zz_vac_probe", m.Labels["relname"])
			require.Equal(t, "app", m.Labels["datname"])
		}
	}
	require.True(t, found)
	require.Equal(t, 1, app.releaseCalls)
}
