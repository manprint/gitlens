package check

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/pgtype"
)

// The PostgreSQL 13 variants drop exactly what 13 lacks and keep the
// column count of the newer query.
func TestPG13QueryVariants(t *testing.T) {
	locks13 := locksQueryFor(pgtype.PG13)
	require.NotContains(t, locks13, "a.query_id")
	require.NotContains(t, locks13, "l.waitstart")
	require.Equal(t, strings.Count(locksQuery, ","), strings.Count(locks13, ","))
	require.Equal(t, locksQuery, locksQueryFor(pgtype.PG14))
	require.Equal(t, locksQuery, locksQueryFor(0))

	for _, v := range []pgtype.PGVersion{pgtype.PG13, pgtype.PG14} {
		q := subscriptionsQueryFor(v)
		require.NotContains(t, q, "pg_stat_subscription_stats", v)
		require.Contains(t, q, "'{}'::pg_catalog.text\n", v)
		require.NotContains(t, q, "to_jsonb(st)", v)
	}
	require.Equal(t, subscriptionsQuery, subscriptionsQueryFor(pgtype.PG15))
	require.NotContains(t, subscriptionsQueryFor(pgtype.PG13), "s.oid")

	require.NotContains(t, recoveryStateQuery13, "pg_get_wal_replay_pause_state")
}
