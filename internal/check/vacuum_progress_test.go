package check

import (
	"context"
	"errors"
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
			return &mockRows{rows: [][]any{{int32(7), "app", uint32(16384), "scanning heap", int64(100), int64(50), int64(0), int64(0), int64(0), int64(0), nil}}}, nil
		}
		return &mockRows{}, nil
	}, queryRowFunc: maintenanceAnswers}
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
		Monitored:  map[string]bool{"app": true},
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

// The 20-row cap keeps the first rows of a deliberate subset. Truncated means a
// configured budget dropped keys, so the cap must read as Partial only.
func TestVacuumProgress_CapIsPartialNotTruncated(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		jobs    int
		partial bool
	}{{20, false}, {21, true}} {
		rows := make([][]any, 0, tc.jobs)
		for i := 0; i < tc.jobs; i++ {
			rows = append(rows, []any{int32(i + 1), "app", uint32(0), "scanning heap", int64(10), int64(2), int64(1), int64(0), int64(2), int64(1), nil})
		}
		conn := &mockConn{queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			if strings.Contains(sql, "pg_stat_progress_vacuum") && !strings.Contains(sql, "information_schema") {
				return &mockRows{rows: rows}, nil
			}
			return &mockRows{}, nil
		}, queryRowFunc: maintenanceAnswers}
		r, err := (&vacuumProgressCheck{}).Scrape(context.Background(), maintenanceTarget(conn))
		require.NoError(t, err)
		require.Equal(t, tc.partial, r.Partial, "%d running vacuums", tc.jobs)
		require.False(t, r.Truncated, "%d running vacuums", tc.jobs)
	}
}

// The 20 series go to the longest-running jobs, not the first 20 rows of the
// view: here the oldest job is the last row, and it must keep its series.
func TestVacuumProgress_KeepsLongestRunningJobs(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)
	rows := make([][]any, 0, 25)
	for i := 1; i <= 25; i++ {
		// Row i started (26-i) minutes after base: row 25 is the oldest.
		started := base.Add(time.Duration(26-i) * time.Minute)
		rows = append(rows, []any{int32(i), "app", uint32(0), "scanning heap", int64(i * 1000), int64(0), int64(0), int64(0), int64(0), int64(0), &started})
	}
	conn := &mockConn{queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
		if strings.Contains(sql, "pg_stat_progress_vacuum") && !strings.Contains(sql, "information_schema") {
			return &mockRows{rows: rows}, nil
		}
		return &mockRows{}, nil
	}, queryRowFunc: maintenanceAnswers}
	r, err := (&vacuumProgressCheck{}).Scrape(context.Background(), maintenanceTarget(conn))
	require.NoError(t, err)
	require.Equal(t, 25.0, metricValue(r, "pg_vacuum_jobs_running", nil))
	var kept []float64
	for _, m := range r.Metrics {
		if m.Name == "pg_vacuum_heap_blks_total" {
			kept = append(kept, m.Value)
		}
	}
	require.Len(t, kept, 20)
	require.Contains(t, kept, 25000.0)
	require.True(t, r.Partial)
}

// maintenanceAnswers is the shared connection of a scrape: it is attached to
// the database "postgres", and its pg_class names only that database's relids.
func maintenanceAnswers(_ context.Context, sql string, args ...any) pgx.Row {
	switch {
	case strings.Contains(sql, "current_database()"):
		return &mockRow{vals: []any{"postgres"}}
	case strings.Contains(sql, "pg_class"):
		if args[0] == uint32(1001) {
			return &mockRow{vals: []any{"maint_t"}}
		}
		return &mockRow{err: pgx.ErrNoRows}
	}
	return &mockRow{}
}

// A vacuum in the maintenance database is named through the shared connection,
// one in a database the agent monitors through its own connection, and one in a
// database it does not collect from is never connected to at all: the
// connection manager would open a pool to that name. Its relname stays empty
// and its relid still identifies it.
func TestVacuumProgress_NeverConnectsToUnmonitoredDatabase(t *testing.T) {
	t.Parallel()
	maint := &mockConn{queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
		if strings.Contains(sql, "pg_stat_progress_vacuum") && !strings.Contains(sql, "information_schema") {
			return &mockRows{rows: [][]any{
				{int32(1), "postgres", uint32(1001), "scanning heap", int64(10), int64(5), int64(0), int64(0), int64(0), int64(0), nil},
				{int32(2), "app", uint32(16384), "scanning heap", int64(10), int64(5), int64(0), int64(0), int64(0), int64(0), nil},
				{int32(3), "secret", uint32(2002), "scanning heap", int64(10), int64(5), int64(0), int64(0), int64(0), int64(0), nil},
			}}, nil
		}
		return &mockRows{}, nil
	}, queryRowFunc: maintenanceAnswers}
	app := &mockConn{queryRowFunc: func(_ context.Context, sql string, args ...any) pgx.Row {
		if strings.Contains(sql, "current_database()") {
			return &mockRow{vals: []any{"app"}}
		}
		return &mockRow{vals: []any{"zz_vac_probe"}}
	}}
	var opened []string
	target := &SimpleTarget{
		ConnFunc: func(context.Context) (Conn, error) { return maint, nil },
		ConnForFunc: func(_ context.Context, datname string) (Conn, error) {
			opened = append(opened, datname)
			require.Equal(t, "app", datname, "a database outside the monitored set was opened")
			return app, nil
		},
		Monitored:  map[string]bool{"postgres": true, "app": true},
		ClockValue: clock.System(),
	}
	r, err := (&vacuumProgressCheck{}).Scrape(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, []string{"app"}, opened)
	names := map[string]string{}
	for _, m := range r.Metrics {
		if m.Name == "pg_vacuum_heap_blks_total" {
			names[m.Labels["datname"]] = m.Labels["relname"]
		}
	}
	require.Equal(t, map[string]string{"postgres": "maint_t", "app": "zz_vac_probe", "secret": ""}, names)
}

// scheduledVacuumTarget keeps a schedule's database set the way the agent's
// manager does. The gate and the schedule's connection allow the database, and
// the command path refuses it, as it does once a discovery has ranked it out.
type scheduledVacuumTarget struct {
	*SimpleTarget
	scheduled Conn
}

func (s scheduledVacuumTarget) ScheduledConnFor(_ context.Context, datname string) (Conn, error) {
	if datname != "app" {
		return nil, errors.New("not scheduled")
	}
	return s.scheduled, nil
}

// A database the schedule still collects from keeps its relname after the latest
// discovery has ranked it out. The relation is read through the schedule's
// connection, and the command path is never asked for it.
func TestVacuumProgress_NamesRelationThroughScheduledConnection(t *testing.T) {
	t.Parallel()
	maint := &mockConn{queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
		if strings.Contains(sql, "pg_stat_progress_vacuum") && !strings.Contains(sql, "information_schema") {
			return &mockRows{rows: [][]any{{int32(7), "app", uint32(16384), "scanning heap", int64(100), int64(50), int64(0), int64(0), int64(0), int64(0), nil}}}, nil
		}
		return &mockRows{}, nil
	}, queryRowFunc: maintenanceAnswers}
	app := &mockConn{queryRowFunc: func(_ context.Context, sql string, args ...any) pgx.Row {
		require.Contains(t, sql, "pg_class")
		require.Equal(t, []any{uint32(16384)}, args)
		return &mockRow{vals: []any{"zz_vac_probe"}}
	}}
	target := scheduledVacuumTarget{
		SimpleTarget: &SimpleTarget{
			ConnFunc: func(context.Context) (Conn, error) { return maint, nil },
			ConnForFunc: func(context.Context, string) (Conn, error) {
				return nil, errors.New("refused by the command path")
			},
			Monitored:  map[string]bool{"app": true},
			ClockValue: clock.System(),
		},
		scheduled: app,
	}
	r, err := (&vacuumProgressCheck{}).Scrape(context.Background(), target)
	require.NoError(t, err)
	found := false
	for _, m := range r.Metrics {
		if m.Name == "pg_vacuum_heap_blks_total" && m.Labels["datname"] == "app" {
			found = true
			require.Equal(t, "zz_vac_probe", m.Labels["relname"])
		}
	}
	require.True(t, found)
	require.Equal(t, 1, app.releaseCalls)
}
