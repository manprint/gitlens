package advisor

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/wire"
)

// bloatBlindSnapshot is a snapshot whose bloat estimate ran, and whose
// database app reported count skipped tables, the first ones named.
func bloatBlindSnapshot(now time.Time, count int, observed time.Time, datname string) *Snapshot {
	tables := []any{}
	for i := range min(count, bloatBlindNamed) {
		tables = append(tables, map[string]any{"schemaname": "public", "relname": fmt.Sprintf("t%02d", i)})
	}
	s := &Snapshot{Now: now, Bloat: []BloatStat{}, Facts: map[string]map[string]Fact{}}
	s.Facts[wire.BloatUnreadableFact] = map[string]Fact{
		databaseFactKey(datname, "unreadable_tables"): {
			Key: "unreadable_tables", Datname: datname, ObservedAt: observed,
			Value: map[string]any{"count": float64(count), "tables": tables},
		},
	}
	return s
}

func bloatBlindRuleUnderTest(t *testing.T) Rule {
	t.Helper()
	return ruleByID(t, "bloat.estimate_blind")
}

// TestBloatBlind_NamesTheLargestSkippedTables: the finding is degraded and
// informational, names at most ten tables, and says how many more there are.
func TestBloatBlind_NamesTheLargestSkippedTables(t *testing.T) {
	t.Parallel()
	now := time.Now()
	findings := bloatBlindRuleUnderTest(t).Evaluate(bloatBlindSnapshot(now, 12, now, "app"))
	require.Len(t, findings, 1)
	f := findings[0]
	require.Equal(t, SeverityInfo, f.Severity)
	require.Equal(t, StateDegraded, f.State)
	require.Equal(t, ScopeDatabase, f.Scope)
	require.Equal(t, "app", f.Datname)
	require.NotEmpty(t, f.ObjectName, "a degraded finding with no object would count as a whole-instance failure")
	require.Contains(t, f.DegradedReason, "bloat estimate blind: no readable column statistics for public.t00, public.t01")
	require.Contains(t, f.DegradedReason, "and 2 more")
	require.Equal(t, 12, f.Evidence["skipped_tables"])
	require.Len(t, f.Evidence["tables"], bloatBlindNamed)
}

// TestBloatBlind_ZeroCountIsQuiet: once every table is readable the fact says
// zero, and the rule answers nothing, so an open finding is resolved.
func TestBloatBlind_ZeroCountIsQuiet(t *testing.T) {
	t.Parallel()
	now := time.Now()
	rule := bloatBlindRuleUnderTest(t)
	s := bloatBlindSnapshot(now, 0, now, "app")
	findings, panicked := (&Engine{}).evaluateRule(rule, s)
	require.Empty(t, findings)
	require.False(t, degradedAsWhole(findings))
	require.False(t, panicked)
}

// TestBloatBlind_StaleFactIsIgnored: a database whose estimate has not run in
// the estimate's window gives no answer; the finding does not outlive it.
func TestBloatBlind_StaleFactIsIgnored(t *testing.T) {
	t.Parallel()
	now := time.Now()
	s := bloatBlindSnapshot(now, 3, now.Add(-bloatEstimateWindow-time.Minute), "app")
	require.Empty(t, bloatBlindRuleUnderTest(t).Evaluate(s))
}

// TestBloatBlind_EachDatabaseHasItsOwnFinding: two databases of one instance
// each get a finding, keyed apart by database.
func TestBloatBlind_EachDatabaseHasItsOwnFinding(t *testing.T) {
	t.Parallel()
	now := time.Now()
	s := bloatBlindSnapshot(now, 1, now, "app")
	other := bloatBlindSnapshot(now, 2, now, "analytics")
	for k, v := range other.Facts[wire.BloatUnreadableFact] {
		s.Facts[wire.BloatUnreadableFact][k] = v
	}
	findings := bloatBlindRuleUnderTest(t).Evaluate(s)
	require.Len(t, findings, 2)
	require.Equal(t, "analytics", findings[0].Datname)
	require.Equal(t, "app", findings[1].Datname)
	require.NotEqual(t, findings[0].ID(), findings[1].ID())
}

// TestBloatBlind_MissingBloatInputDegradesTheWhole: without the bloat estimate
// the rule cannot say whether anything is blind, so it degrades as a whole.
func TestBloatBlind_MissingBloatInputDegradesTheWhole(t *testing.T) {
	t.Parallel()
	now := time.Now()
	s := bloatBlindSnapshot(now, 3, now, "app")
	s.Bloat = nil
	findings, _ := (&Engine{}).evaluateRule(bloatBlindRuleUnderTest(t), s)
	require.Len(t, findings, 1)
	require.Equal(t, StateDegraded, findings[0].State)
	require.True(t, degradedAsWhole(findings))
}
