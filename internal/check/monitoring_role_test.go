package check

import (
	"context"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func scrapeMonitoringRoleForTest(t *testing.T, row []any) Fact {
	t.Helper()
	conn := &mockConn{queryRowFunc: func(_ context.Context, sql string, _ ...any) pgx.Row {
		require.Equal(t, monitoringRoleQuery, sql)
		return &mockRow{vals: row}
	}}
	r, err := (&monitoringRoleCheck{}).Scrape(context.Background(), maintenanceTarget(conn))
	require.NoError(t, err)
	require.Equal(t, 1, conn.releaseCalls)
	require.Len(t, r.Facts, 1)
	require.Equal(t, monitoringRoleFactKind, r.Facts[0].Kind)
	require.Equal(t, monitoringRoleFactKey, r.Facts[0].Key)
	return r.Facts[0]
}

func readOnlyValue(v string) *string { return &v }

// A clean role still reports its fact, saying it is read-only: the advisor
// resolves an earlier finding on it. The value is never empty, since a fact
// without one does not pass wire.Fact.Validate.
func TestMonitoringRole_CleanRoleReportsNoProblem(t *testing.T) {
	f := scrapeMonitoringRoleForTest(t, []any{"pglens", false, false, false, false, false, []string{}, int64(0), readOnlyValue("on"), true, true})
	require.Equal(t, monitoringRoleReadOnly, f.ValueText)
	require.Equal(t, map[string]string{"role": "pglens", "read_only_default": "on", "has_pg_monitor": "true", "has_read_all_stats": "true"}, f.Labels)
}

func TestMonitoringRole_ListsEveryProblem(t *testing.T) {
	f := scrapeMonitoringRoleForTest(t, []any{"pglens", false, true, false, true, true,
		[]string{"app_writer", "pg_write_all_data"}, int64(3), readOnlyValue("off"), true, true})
	require.Equal(t, "has CREATEDB; has REPLICATION; has BYPASSRLS; is a member of app_writer; is a member of pg_write_all_data; owns 3 object(s)", f.ValueText)
	require.Equal(t, "off", f.Labels["read_only_default"])
}

func TestMonitoringRole_Superuser(t *testing.T) {
	f := scrapeMonitoringRoleForTest(t, []any{"postgres", true, true, true, true, true, []string{}, int64(0), nil, true, true})
	require.Equal(t, "has SUPERUSER; has CREATEDB; has CREATEROLE; has REPLICATION; has BYPASSRLS", f.ValueText)
	require.Empty(t, f.Labels["read_only_default"])
}

// RDYLBL-001: the fact carries has_pg_monitor and has_read_all_stats as "true"
// or "false", next to the labels it always had, whose values do not change.
func TestRDYLBL001_MembershipLabels(t *testing.T) {
	for _, tc := range []struct {
		monitor, stats bool
	}{{true, true}, {false, true}, {true, false}, {false, false}} {
		f := scrapeMonitoringRoleForTest(t, []any{"pglens", false, false, false, false, false, []string{}, int64(0), readOnlyValue("on"), tc.monitor, tc.stats})
		require.Equal(t, monitoringRoleReadOnly, f.ValueText, "the value does not depend on the memberships")
		require.Equal(t, map[string]string{
			"role": "pglens", "read_only_default": "on",
			"has_pg_monitor": strconv.FormatBool(tc.monitor), "has_read_all_stats": strconv.FormatBool(tc.stats),
		}, f.Labels)
	}
	require.Contains(t, monitoringRoleQuery, "pg_catalog.pg_has_role(r.oid, 'pg_monitor', 'MEMBER')")
	require.Contains(t, monitoringRoleQuery, "pg_catalog.pg_has_role(r.oid, 'pg_read_all_stats', 'MEMBER')")
}
