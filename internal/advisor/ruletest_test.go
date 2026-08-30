package advisor

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func assertRuleContract(t *testing.T, r Rule, s *Snapshot) {
	t.Helper()
	require.NotEmpty(t, r.ID())
	require.NotEmpty(t, r.Severity())
	require.NotEmpty(t, r.Scope())
	require.NotEmpty(t, r.Needs())
	_ = r.MinTier()
	a := r.Evaluate(s)
	b := r.Evaluate(s)
	for i, f := range a {
		require.Contains(t, []Severity{SeverityCritical, SeverityWarning, SeverityInfo}, f.Severity)
		require.NotEmpty(t, f.Title)
		require.NotEmpty(t, f.Detail)
		if f.State == StateOpen {
			require.NotEmpty(t, f.Remediation)
		}
		if f.State == StateDegraded {
			require.NotEmpty(t, f.DegradedReason)
		}
		require.Equal(t, f.ID(), b[i].ID())
	}
}
