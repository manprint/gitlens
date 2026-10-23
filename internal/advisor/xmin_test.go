package advisor

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// xminSnapshot is a snapshot whose latest activity sample named holder as the
// oldest xmin holder, observed at now.
func xminSnapshot(now time.Time, holder map[string]any) *Snapshot {
	return &Snapshot{
		Now:     now,
		Metrics: map[string]map[string]float64{"pg_max_xact_age_seconds": {"": 1000}},
		Facts: map[string]map[string]Fact{"xmin_horizon": {"oldest": {
			Key: "oldest", Value: holder, ObservedAt: now,
		}}},
	}
}

// txn.long_running names the oldest xmin holder by kind and identity, and no
// query text, so the operator sees which session keeps the horizon back.
func TestRule_txn_long_running_NamesTheXminHolder(t *testing.T) {
	now := time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)
	s := xminSnapshot(now, map[string]any{
		"holder_kind": "session", "identity": "4242", "age_xids": float64(910), "age_seconds": 33.5,
		"query": "SELECT secret FROM accounts",
	})
	f := ruleByID(t, "txn.long_running").Evaluate(s)
	require.Len(t, f, 1)
	holder, ok := f[0].Evidence["xmin_holder"].(map[string]any)
	require.True(t, ok, "evidence does not name the holder")
	require.Equal(t, "session", holder["holder_kind"])
	require.Equal(t, "4242", holder["identity"])
	require.Equal(t, float64(910), holder["age_xids"])
	require.Equal(t, 33.5, holder["age_seconds"])
	require.NotContains(t, holder, "query")
	require.Contains(t, f[0].Detail, "session with pid 4242")
	require.NotContains(t, f[0].Detail, "accounts")
	assertRuleContract(t, ruleByID(t, "txn.long_running"), s)
}

// A prepared transaction is named by its gid, and a holder without a start
// time has no age in seconds.
func TestRule_txn_long_running_NamesAPreparedHolderWithoutSeconds(t *testing.T) {
	now := time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)
	f := ruleByID(t, "txn.long_running").Evaluate(xminSnapshot(now, map[string]any{
		"holder_kind": "prepared", "identity": "orders import", "age_xids": float64(7),
	}))
	require.Len(t, f, 1)
	holder := f[0].Evidence["xmin_holder"].(map[string]any)
	require.Equal(t, "orders import", holder["identity"])
	require.NotContains(t, holder, "age_seconds")
	require.Contains(t, f[0].Detail, "prepared transaction with gid orders import")
}

// Without a fresh holder the finding is what it was: no holder is invented.
func TestRule_txn_long_running_NoHolderLeavesTheFindingAlone(t *testing.T) {
	now := time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)
	s := &Snapshot{Now: now, Metrics: map[string]map[string]float64{"pg_max_xact_age_seconds": {"": 1000}}}
	f := ruleByID(t, "txn.long_running").Evaluate(s)
	require.Len(t, f, 1)
	require.NotContains(t, f[0].Evidence, "xmin_holder")
	require.NotContains(t, f[0].Detail, "xmin holder")
}

// A holder from a sample older than the recent window is no longer the
// instance's holder, and the finding does not name it.
func TestRule_txn_long_running_StaleHolderIsNotNamed(t *testing.T) {
	now := time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)
	s := xminSnapshot(now, map[string]any{"holder_kind": "slot", "identity": "cdc", "age_xids": float64(5)})
	fact := s.Facts["xmin_horizon"]["oldest"]
	fact.ObservedAt = now.Add(-recentWindow - time.Second)
	s.Facts["xmin_horizon"]["oldest"] = fact
	f := ruleByID(t, "txn.long_running").Evaluate(s)
	require.Len(t, f, 1)
	require.NotContains(t, f[0].Evidence, "xmin_holder")
}

// vacuum.starvation names the holder that keeps the dead rows from being
// removed, in its evidence and its detail.
func TestRule_vacuum_starvation_NamesTheXminHolder(t *testing.T) {
	now := time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)
	s := &Snapshot{
		Now:      now,
		Tables:   makeDeadTables(11),
		Metrics:  map[string]map[string]float64{"pg_vacuum_jobs_running": {"": 3}},
		Settings: map[string]string{"autovacuum_max_workers": "3"},
		Facts: map[string]map[string]Fact{"xmin_horizon": {"oldest": {
			Key: "oldest", ObservedAt: now,
			Value: map[string]any{"holder_kind": "standby", "identity": "replica_1", "age_xids": float64(120)},
		}}},
	}
	f := relationRuleByID(t, "vacuum.starvation").Evaluate(s)
	require.Len(t, f, 1)
	holder, ok := f[0].Evidence["xmin_holder"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "standby", holder["holder_kind"])
	require.Equal(t, "replica_1", holder["identity"])
	require.True(t, strings.Contains(f[0].Detail, "standby with application_name replica_1"), f[0].Detail)
	require.Equal(t, 11, f[0].Evidence["tables"])
	assertRuleContract(t, relationRuleByID(t, "vacuum.starvation"), s)
}

// The evidence carries the four fields of the holder, and nothing else from
// the fact, so no field a sender adds reaches a finding.
func TestXminHolderEvidence_CopiesOnlyTheHolderFields(t *testing.T) {
	now := time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)
	s := xminSnapshot(now, map[string]any{"holder_kind": "slot", "identity": "cdc", "age_xids": float64(5), "extra": "x"})
	got := xminHolderEvidence(s)
	require.Equal(t, map[string]any{"holder_kind": "slot", "identity": "cdc", "age_xids": float64(5)}, got)
	require.Nil(t, xminHolderEvidence(&Snapshot{Now: now}))
}
