//go:build integration

package check

import (
	"testing"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
)

// INT-CHECK-016 keeps the four maintenance checks visible in the T0 registry sweep.
func TestINTCHECK016_MaintenanceChecksAreTierZeroDatabaseSafe(t *testing.T) {
	want := map[string]Scope{"table_stats": ScopeDatabase, "index_stats": ScopeDatabase, "vacuum_progress": ScopeInstance, "bloat_estimate": ScopeDatabase}
	seen := 0
	for _, c := range All() {
		if scope, ok := want[c.Name()]; ok {
			seen++
			require.Equal(t, scope, c.Requires().Scope)
			require.Equal(t, pgtype.TierReadOnly, c.Requires().PermTier)
		}
	}
	require.Equal(t, len(want), seen)
}
