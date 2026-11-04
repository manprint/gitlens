package sessionorigin

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// SORIGIN-001
func TestSORIGIN001_OriginTable(t *testing.T) {
	for _, tc := range []struct {
		name, app, user, role, want string
	}{
		{"bare", "pglens", "u", "", Pglens},
		{"check", "pglens/check", "u", "", Pglens},
		{"ash", "pglens/ash", "u", "", Pglens},
		{"shared", "pglens/shared", "u", "", Pglens},
		{"database", "pglens/odoo", "u", "", Pglens},
		{"agent prefix", "pglens-agent/x", "u", "", Pglens},
		{"lookalike underscore", "pglens_x", "u", "", Application},
		{"lookalike upper", "PGLENS", "u", "", Application},
		{"lookalike suffix", "mypglens", "u", "", Application},
		{"no slash", "pglens-agent", "u", "", Application},
		{"application", "psql", "u", "", Application},
		{"empty app and user, empty role", "", "", "", Application},
		{"empty role never matches empty user", "psql", "", "", Application},
		{"role match with empty app", "", "monitor", "monitor", Pglens},
		{"role match with app", "psql", "monitor", "monitor", Pglens},
		{"other role", "psql", "app", "monitor", Application},
		{"role set, empty user", "psql", "", "monitor", Application},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, Origin(tc.app, tc.user, tc.role))
			if tc.role == "" {
				require.Equal(t, tc.want == Pglens, IsPglensApplication(tc.app))
			}
		})
	}
}

// SORIGIN-003 (static half): the predicate names every prefix, the equality
// and the current_user comparison; the PostgreSQL half is ACTCFG-INT-001.
func TestSORIGIN003_PredicateSQLContents(t *testing.T) {
	sql := PredicateSQL("application_name", "usename")
	require.Contains(t, sql, "= 'pglens'")
	for _, p := range Prefixes {
		require.Contains(t, sql, "'"+p+"'")
	}
	require.Contains(t, sql, "usename = current_user")
	require.Contains(t, ApplicationSQL("a.application_name"), "COALESCE(a.application_name, '')")
	require.NotContains(t, ApplicationSQL("x"), "current_user")
}
