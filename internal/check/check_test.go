package check_test

import (
	"context"
	"testing"
	"time"

	"github.com/manprint/pglens/internal/check"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
)

func TestRequirements_Supports(t *testing.T) {
	t.Parallel()
	req := check.Requirements{
		Roles:      []pgtype.Role{pgtype.RolePrimary},
		MinPG:      pgtype.PG15,
		MaxPG:      pgtype.PG18,
		PermTier:   pgtype.TierReadOnly,
		Extensions: []string{"pg_stat_statements"},
		Scope:      check.ScopeInstance,
	}
	// Happy path
	ok, reason := req.Supports(pgtype.RolePrimary, pgtype.PGVersion(160000), pgtype.TierReadOnly, map[string]bool{"pg_stat_statements": true})
	require.True(t, ok, reason)
	require.Empty(t, reason)

	// Wrong role
	ok, reason = req.Supports(pgtype.RoleStandby, pgtype.PGVersion(160000), pgtype.TierReadOnly, map[string]bool{"pg_stat_statements": true})
	require.False(t, ok)
	require.NotEmpty(t, reason)

	// Below MinPG
	ok, reason = req.Supports(pgtype.RolePrimary, pgtype.PGVersion(140000), pgtype.TierReadOnly, map[string]bool{"pg_stat_statements": true})
	require.False(t, ok)
	require.NotEmpty(t, reason)

	// Above MaxPG
	ok, reason = req.Supports(pgtype.RolePrimary, pgtype.PGVersion(190000), pgtype.TierReadOnly, map[string]bool{"pg_stat_statements": true})
	require.False(t, ok)
	require.NotEmpty(t, reason)

	// Insufficient tier
	req2 := check.Requirements{PermTier: pgtype.TierExplain}
	ok, reason = req2.Supports(pgtype.RolePrimary, pgtype.PGVersion(160000), pgtype.TierReadOnly, nil)
	require.False(t, ok)
	require.NotEmpty(t, reason)

	// Missing extension
	ok, reason = req.Supports(pgtype.RolePrimary, pgtype.PGVersion(160000), pgtype.TierReadOnly, map[string]bool{})
	require.False(t, ok)
	require.NotEmpty(t, reason)
}

func TestRequirements_EmptyRolesMeansAny(t *testing.T) {
	t.Parallel()
	req := check.Requirements{Roles: nil}
	for _, role := range []pgtype.Role{pgtype.RolePrimary, pgtype.RoleStandby, pgtype.RoleUnknown} {
		ok, _ := req.Supports(role, pgtype.PGVersion(160000), pgtype.TierReadOnly, nil)
		require.True(t, ok)
	}
}

func TestRegistry_DuplicatePanics(t *testing.T) {
	check.ResetForTest()
	defer check.ResetForTest()
	c1 := &fakeCheck{name: "dup"}
	check.Register(c1)
	require.Panics(t, func() {
		check.Register(&fakeCheck{name: "dup"})
	})
}

func TestRegistry_AllIsSorted(t *testing.T) {
	check.ResetForTest()
	defer check.ResetForTest()
	check.Register(&fakeCheck{name: "zebra"})
	check.Register(&fakeCheck{name: "alpha"})
	check.Register(&fakeCheck{name: "middle"})
	all := check.All()
	require.Len(t, all, 3)
	require.Equal(t, "alpha", all[0].Name())
	require.Equal(t, "middle", all[1].Name())
	require.Equal(t, "zebra", all[2].Name())
	// Stable across calls
	all2 := check.All()
	require.Equal(t, all[0].Name(), all2[0].Name())
}

func TestRegistry_GetMissing(t *testing.T) {
	check.ResetForTest()
	defer check.ResetForTest()
	_, ok := check.Get("nonexistent")
	require.False(t, ok)
}

type fakeCheck struct {
	name string
}

func (f *fakeCheck) Name() string                 { return f.name }
func (f *fakeCheck) Requires() check.Requirements { return check.Requirements{} }
func (f *fakeCheck) DefaultInterval() time.Duration {
	return time.Second
}
func (f *fakeCheck) Timeout() time.Duration { return time.Second }
func (f *fakeCheck) Scrape(ctx context.Context, t check.Target) (check.Result, error) {
	return check.Result{}, nil
}
