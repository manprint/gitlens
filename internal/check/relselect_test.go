package check

import (
	"testing"

	"github.com/manprint/pglens/internal/cardinality"
	"github.com/stretchr/testify/require"
)

func TestRelSelector_RespectsBudget(t *testing.T) {
	s := NewRelSelector(2, 2)
	got, tr := s.Select(RelKindTable, 1, []cardinality.Candidate{{Key: "a", Primary: 3}, {Key: "b", Primary: 2}, {Key: "c", Primary: 1}})
	require.Len(t, got, 2)
	require.True(t, tr)
}
func TestRelSelector_HighestScoreSurvives(t *testing.T) {
	s := NewRelSelector(1, 1)
	got, _ := s.Select(RelKindBloat, 1, []cardinality.Candidate{{Key: "low", Primary: 1}, {Key: "high", Primary: 9}})
	require.Equal(t, "high", got[0].Key)
}
func TestRelSelector_TieBrokenByName(t *testing.T) {
	s := NewRelSelector(1, 1)
	got, _ := s.Select(RelKindIndex, 1, []cardinality.Candidate{{Key: "z", Primary: 1}, {Key: "a", Primary: 1}})
	require.Equal(t, "a", got[0].Key)
}
func TestRelSelector_BudgetIsSharedAcrossDatabases(t *testing.T) {
	s := NewRelSelector(1, 1)
	got, _ := s.Select(RelKindTable, 1, []cardinality.Candidate{{Key: "db1.t", Primary: 2}, {Key: "db2.t", Primary: 1}})
	require.Len(t, got, 1)
}

// SYS-IDX-002: the relation budget is shared across database-qualified keys.
func TestSYSIDX002_RelationBudgetSharedAcrossDatabases(t *testing.T) {
	s := NewRelSelector(1, 1)
	got, truncated := s.Select(RelKindIndex, 7, []cardinality.Candidate{
		{Key: "db_a.orders_id_idx", Primary: 100},
		{Key: "db_b.orders_id_idx", Primary: 1},
	})
	require.Len(t, got, 1)
	require.True(t, truncated)
	require.Equal(t, "db_a.orders_id_idx", got[0].Key)
}
func TestRelSelector_HysteresisPreventsFlapping(t *testing.T) {
	s := NewRelSelector(1, 1)
	_, _ = s.Select(RelKindTable, 1, []cardinality.Candidate{{Key: "a", Primary: 2}, {Key: "b", Primary: 1}})
	got, _ := s.Select(RelKindTable, 2, []cardinality.Candidate{{Key: "b", Primary: 3}, {Key: "a", Primary: 1}})
	require.NotEmpty(t, got)
}
func TestRelSelector_EmitsTruncatedCounter(t *testing.T) {
	s := NewRelSelector(1, 1)
	_, tr := s.Select(RelKindTable, 1, []cardinality.Candidate{{Key: "a", Primary: 2}, {Key: "b", Primary: 1}})
	require.True(t, tr)
}
func TestRelSelector_ResetsPerCycle(t *testing.T) {
	s := NewRelSelector(1, 1)
	_, tr := s.Select(RelKindTable, 2, []cardinality.Candidate{{Key: "a", Primary: 2}, {Key: "b", Primary: 1}})
	require.True(t, tr)
}
