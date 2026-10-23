package advisor

import (
	"testing"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
)

func conflictKey(db, reason string) string {
	return pgtype.CanonicalLabels(map[string]string{"database": db, "reason": reason})
}

func TestStandbyRecoveryConflictsRule(t *testing.T) {
	rule := ruleByID(t, "standby.recovery_conflicts")
	s := &Snapshot{Now: time.Now(), Metrics: map[string]map[string]float64{
		"pg_recovery_conflicts_total": {
			conflictKey("app", "snapshot"): 0.05,
			conflictKey("app", "lock"):     0.1,
			conflictKey("app", "deadlock"): 0,
			conflictKey("crm", "snapshot"): 0,
		},
	}}
	assertRuleContract(t, rule, s)
	require.Equal(t, ScopeDatabase, rule.Scope())
	got := rule.Evaluate(s)
	require.Len(t, got, 1)
	require.Equal(t, "standby.recovery_conflicts/app/recovery_conflicts", got[0].ID())
	require.Equal(t, "Standby queries on app are cancelled by WAL replay", got[0].Title)
	require.Contains(t, got[0].Detail, "lock 6.0/min, snapshot 3.0/min")
	require.Contains(t, got[0].Detail, "vacuum_truncate = off")
	require.Contains(t, got[0].Detail, "hot_standby_feedback = on")
	require.NotContains(t, got[0].Detail, "deadlock")

	// A primary reports no conflict series at all: the rule is quiet, not
	// short of input.
	require.Empty(t, rule.Evaluate(&Snapshot{Now: time.Now()}))
}

func TestStandbyReplayPausedRule(t *testing.T) {
	rule := ruleByID(t, "standby.replay_paused")
	paused := &Snapshot{Now: time.Now(), Metrics: map[string]map[string]float64{"pg_recovery_replay_paused": {"": 1}}}
	assertRuleContract(t, rule, paused)
	got := rule.Evaluate(paused)
	require.Len(t, got, 1)
	require.Contains(t, got[0].Remediation, "pg_wal_replay_resume()")
	require.Empty(t, rule.Evaluate(&Snapshot{Metrics: map[string]map[string]float64{"pg_recovery_replay_paused": {"": 0}}}))
	require.Empty(t, rule.Evaluate(&Snapshot{}))
}

func TestWALDirectoryLargeRule(t *testing.T) {
	rule := ruleByID(t, "wal.directory_large")
	snapshot := func(dirBytes float64, ready float64) *Snapshot {
		return &Snapshot{Now: time.Now(),
			Settings: map[string]string{"max_wal_size_bytes": "1073741824", "wal_keep_size_bytes": "536870912"},
			Metrics: map[string]map[string]float64{
				"pg_wal_dir_bytes":        {"": dirBytes},
				"pg_wal_dir_files":        {"": dirBytes / (16 << 20)},
				"pg_archiver_ready_files": {"": ready},
			}}
	}
	// 2 x 1 GiB + 512 MiB is what checkpoints, recycling and wal_keep_size explain.
	require.Empty(t, rule.Evaluate(snapshot(2.5*(1<<30), 0)))
	s := snapshot(6*(1<<30), 40)
	assertRuleContract(t, rule, s)
	got := rule.Evaluate(s)
	require.Len(t, got, 1)
	require.Equal(t, "pg_wal holds 6.0 GiB, more than max_wal_size and wal_keep_size explain", got[0].Title)
	require.Contains(t, got[0].Detail, "384 files")
	require.Contains(t, got[0].Detail, "40 segments wait for archive_command")
	require.Equal(t, 40.0, got[0].Evidence["archiver_ready_files"])
	// Without the listing (pg_ls_waldir denied) or max_wal_size, it is quiet.
	require.Empty(t, rule.Evaluate(&Snapshot{Settings: map[string]string{"max_wal_size_bytes": "1073741824"}}))
	require.Empty(t, rule.Evaluate(&Snapshot{Settings: map[string]string{}, Metrics: map[string]map[string]float64{"pg_wal_dir_bytes": {"": 1 << 40}}}))
}
