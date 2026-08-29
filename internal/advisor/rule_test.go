package advisor

import (
	"sort"
	"testing"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
)

type testRule struct{ id string }

func (r testRule) ID() string                 { return r.id }
func (testRule) Severity() Severity           { return SeverityWarning }
func (testRule) Scope() Scope                 { return ScopeInstance }
func (testRule) Needs() []string              { return []string{"host"} }
func (testRule) MinTier() pgtype.PermTier     { return pgtype.TierReadOnly }
func (testRule) Evaluate(*Snapshot) []Finding { return nil }

func TestRegister_PanicsOnDuplicate(t *testing.T) {
	id := "test.duplicate"
	Register(testRule{id: id})
	require.Panics(t, func() { Register(testRule{id: id}) })
}

func TestAll_IsSortedAndDeterministic(t *testing.T) {
	Register(testRule{id: "test.b"})
	Register(testRule{id: "test.a"})
	got := All()
	ids := sortedRuleIDs(got)
	require.Contains(t, ids, "test.a")
	require.Contains(t, ids, "test.b")
	require.True(t, sort.StringsAreSorted(ids))
}

func TestFinding_IDIsStable(t *testing.T) {
	f := Finding{RuleID: "disk.free_low", ObjectName: "public.events"}
	require.Equal(t, "disk.free_low/public.events", f.ID())
	require.Equal(t, f.ID(), f.ID())
}

func TestFinding_IDDiffersPerSubject(t *testing.T) {
	a := Finding{RuleID: "rule", ObjectName: "public.a"}
	b := Finding{RuleID: "rule", ObjectName: "public.b"}
	require.NotEqual(t, a.ID(), b.ID())
}
