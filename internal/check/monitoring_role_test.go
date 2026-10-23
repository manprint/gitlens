package check

import (
	"context"
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
	f := scrapeMonitoringRoleForTest(t, []any{"pglens", false, false, false, false, false, []string{}, int64(0), readOnlyValue("on")})
	require.Equal(t, monitoringRoleReadOnly, f.ValueText)
	require.Equal(t, map[string]string{"role": "pglens", "read_only_default": "on"}, f.Labels)
}

func TestMonitoringRole_ListsEveryProblem(t *testing.T) {
	f := scrapeMonitoringRoleForTest(t, []any{"pglens", false, true, false, true, true,
		[]string{"app_writer", "pg_write_all_data"}, int64(3), readOnlyValue("off")})
	require.Equal(t, "has CREATEDB; has REPLICATION; has BYPASSRLS; is a member of app_writer; is a member of pg_write_all_data; owns 3 object(s)", f.ValueText)
	require.Equal(t, "off", f.Labels["read_only_default"])
}

func TestMonitoringRole_Superuser(t *testing.T) {
	f := scrapeMonitoringRoleForTest(t, []any{"postgres", true, true, true, true, true, []string{}, int64(0), nil})
	require.Equal(t, "has SUPERUSER; has CREATEDB; has CREATEROLE; has REPLICATION; has BYPASSRLS", f.ValueText)
	require.Empty(t, f.Labels["read_only_default"])
}
