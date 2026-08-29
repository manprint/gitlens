package alert

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuiltin_AllValidate(t *testing.T) {
	for _, r := range Builtin() {
		require.NoError(t, r.Validate())
	}
}
func TestBuiltin_IDsAreUnique(t *testing.T) {
	m := map[string]bool{}
	for _, r := range Builtin() {
		require.False(t, m[r.ID])
		m[r.ID] = true
	}
}
func TestBuiltin_AllAreTier0(t *testing.T) {
	for _, r := range Builtin() {
		require.Equal(t, Tier0, r.Tier)
		require.True(t, r.Enabled)
	}
}
func TestBuiltin_OrderIsDeterministic(t *testing.T) {
	a, b := Builtin(), Builtin()
	require.Equal(t, a, b)
}
func TestBuiltin_ReturnsFreshSlice(t *testing.T) {
	a := Builtin()
	a[0].ID = "x"
	require.NotEqual(t, "x", Builtin()[0].ID)
}
func TestBuiltin_CoversDocumentedCatalogue(t *testing.T) {
	want := []string{"agent_down", "instance_unreachable", "check_failing", "no_primary_in_cluster", "agent_buffer_full", "clock_skew", "cardinality_budget_exceeded", "failover_detected", "split_brain_detected", "slot_inactive"}
	got := []string{}
	for _, r := range Builtin() {
		got = append(got, r.ID)
	}
	require.Equal(t, want, got)
}
