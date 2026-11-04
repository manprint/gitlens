package check_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/check"
	"github.com/manprint/pglens/internal/pgtype"
)

// RDY-SKIP-001: the skip reasons Supports produces start with the prefixes the
// readiness evaluator treats as by design ("requires ", "role ") or as a
// fixable gap ("missing extension "). A reason of another shape would be raised
// as an unknown gap (RDY-CHK-02), so changing one needs the evaluator changed
// with it.
func TestRDYSKIP001_SupportsReasonShapes(t *testing.T) {
	cases := []struct {
		name    string
		req     check.Requirements
		version pgtype.PGVersion
		prefix  string
	}{
		{"role", check.Requirements{Roles: []pgtype.Role{pgtype.RolePrimary}}, 140000, "role "},
		{"min version", check.Requirements{MinPG: 150000}, 140000, "requires "},
		{"max version", check.Requirements{MaxPG: 130000}, 170000, "requires "},
		{"tier", check.Requirements{PermTier: pgtype.TierSignal}, 140000, "requires "},
		{"extension", check.Requirements{Extensions: []string{"pgstattuple"}}, 140000, "missing extension "},
	}
	for _, c := range cases {
		ok, reason := c.req.Supports(pgtype.RoleStandby, c.version, pgtype.TierReadOnly, map[string]bool{})
		require.False(t, ok, c.name)
		require.True(t, strings.HasPrefix(reason, c.prefix), "%s: %q should start with %q", c.name, reason, c.prefix)
	}
}
