package advisor

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/pgtype"
)

var configNow = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

func configBase() *Snapshot {
	return &Snapshot{Now: configNow, FirstSeen: configNow.Add(-30 * 24 * time.Hour), Role: pgtype.RolePrimary,
		Host: HostInfo{Available: true, TotalBytes: 16 * 1024 * 1024 * 1024}, Settings: map[string]string{
			"work_mem": "4096", "work_mem_bytes": "4194304", "max_connections": "100", "shared_buffers_bytes": "4294967296",
			"effective_cache_size_bytes": "8589934592", "maintenance_work_mem_bytes": "536870912", "track_io_timing": "on",
			"fsync": "on", "full_page_writes": "on", "archive_mode": "on",
		},
		Metrics: map[string]map[string]float64{"pg_archiver_failed_ratio": {"": 0}},
		Facts:   map[string]map[string]Fact{"backup": {"basebackup_running": {Key: "basebackup_running", ObservedAt: configNow.Add(-24 * time.Hour)}}},
	}
}

func TestPackC_FiringAndQuietMatrix(t *testing.T) {
	tests := []struct {
		id          string
		fire, quiet *Snapshot
	}{
		{"config.work_mem_oversized", func() *Snapshot {
			s := configBase()
			s.Settings["work_mem_bytes"] = "1073741824"
			s.Settings["max_connections"] = "10"
			return s
		}(), configBase()},
		{"config.work_mem_low", func() *Snapshot {
			s := configBase()
			s.Settings["work_mem_bytes"] = "1"
			s.Statements = []StatementStat{{Calls: 2, TempBytes: 10}}
			return s
		}(), configBase()},
		{"config.shared_buffers_low", func() *Snapshot { s := configBase(); s.Settings["shared_buffers_bytes"] = "1"; return s }(), configBase()},
		{"config.shared_buffers_high", func() *Snapshot { s := configBase(); s.Settings["shared_buffers_bytes"] = "7516192768"; return s }(), configBase()},
		{"config.effective_cache_size_mismatch", func() *Snapshot { s := configBase(); s.Settings["effective_cache_size_bytes"] = "1"; return s }(), configBase()},
		{"config.maintenance_work_mem_low", func() *Snapshot { s := configBase(); s.Settings["maintenance_work_mem_bytes"] = "1"; return s }(), configBase()},
		{"config.max_connections_high", func() *Snapshot {
			s := configBase()
			s.Settings["max_connections"] = "201"
			s.Metrics["pg_connections_used_ratio"] = map[string]float64{"": .2}
			return s
		}(), configBase()},
		{"config.track_io_timing_off", func() *Snapshot { s := configBase(); s.Settings["track_io_timing"] = "off"; return s }(), configBase()},
		{"config.checkpoints_too_frequent", func() *Snapshot {
			s := configBase()
			s.Metrics["pg_checkpoints_requested_ratio"] = map[string]float64{"": .21}
			return s
		}(), configBase()},
		{"config.wal_keep_size_low", func() *Snapshot {
			s := configBase()
			s.Settings["wal_keep_size_bytes"] = "10"
			s.Replication = &ReplicationInfo{Standbys: 1, MaxLagBytes: 32 * 1024 * 1024}
			return s
		}(), configBase()},
		{"config.fsync_off", func() *Snapshot { s := configBase(); s.Settings["fsync"] = "off"; return s }(), configBase()},
		{"config.full_page_writes_off", func() *Snapshot { s := configBase(); s.Settings["full_page_writes"] = "off"; return s }(), configBase()},
		{"config.drift", func() *Snapshot {
			s := configBase()
			s.Siblings = []SiblingInfo{{Settings: map[string]string{"work_mem": "8192"}}}
			return s
		}(), func() *Snapshot {
			s := configBase()
			s.Siblings = []SiblingInfo{{Settings: map[string]string{"port": "9999"}}}
			return s
		}()},
		{"conn.saturation", func() *Snapshot {
			s := configBase()
			s.Metrics["pg_connections_used_ratio"] = map[string]float64{"": .81}
			return s
		}(), configBase()},
		{"conn.idle_share_high", func() *Snapshot {
			s := configBase()
			s.Metrics["pg_connections_idle_share"] = map[string]float64{"": .71}
			s.Metrics["pg_connections_used"] = map[string]float64{"": 101}
			return s
		}(), configBase()},
		{"archive.disabled", func() *Snapshot { s := configBase(); s.Settings["archive_mode"] = "off"; return s }(), configBase()},
		{"archive.failing", func() *Snapshot {
			s := configBase()
			s.Metrics["pg_archiver_failed_ratio"] = map[string]float64{"": 1}
			return s
		}(), configBase()},
		{"archive.stalled", func() *Snapshot {
			s := configBase()
			s.Metrics["pg_archiver_last_archived_age_seconds"] = map[string]float64{"": 3601}
			s.Metrics["pg_archiver_ready_files"] = map[string]float64{"": 2}
			return s
		}(), configBase()},
		{"backup.no_basebackup_seen", func() *Snapshot {
			s := configBase()
			s.Facts["backup"]["basebackup_running"] = Fact{Key: "basebackup_running", ObservedAt: configNow.Add(-8 * 24 * time.Hour)}
			return s
		}(), configBase()},
		{"backup.no_strategy", func() *Snapshot {
			s := configBase()
			s.Settings["archive_mode"] = "off"
			delete(s.Facts, "backup")
			return s
		}(), configBase()},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			r := ruleByID(t, tc.id)
			f := r.Evaluate(tc.fire)
			require.NotEmpty(t, f)
			assertRuleContract(t, r, tc.fire)
			require.Empty(t, r.Evaluate(tc.quiet))
		})
	}
}

func TestPackC_MemoryRulesDegradeWithoutHost(t *testing.T) {
	s := configBase()
	s.Host.Available = false
	for _, id := range []string{"config.work_mem_oversized", "config.shared_buffers_low", "config.shared_buffers_high", "config.effective_cache_size_mismatch", "config.maintenance_work_mem_low"} {
		f := ruleByID(t, id).Evaluate(s)
		require.Len(t, f, 1)
		require.Equal(t, StateDegraded, f[0].State)
		require.Contains(t, f[0].DegradedReason, "host metrics unavailable")
		assertRuleContract(t, ruleByID(t, id), s)
	}
}
func TestPackC_UsableMemoryPrefersCgroupLimit(t *testing.T) {
	s := configBase()
	s.Host.CgroupLimitBytes = 4 * 1024 * 1024 * 1024
	s.Settings["shared_buffers_bytes"] = "1500000000"
	require.Empty(t, ruleByID(t, "config.shared_buffers_high").Evaluate(s))
	s.Settings["shared_buffers_bytes"] = "2147483649"
	require.NotEmpty(t, ruleByID(t, "config.shared_buffers_high").Evaluate(s))
}
func TestPackC_DriftExcludesPerInstanceSettings(t *testing.T) {
	s := configBase()
	for _, name := range DriftExcludedSettings {
		s.Settings[name] = "here"
	}
	s.Settings["in_hot_standby"] = "off"
	s.SettingContexts = map[string]string{"in_hot_standby": "internal"}
	sibling := SiblingInfo{Settings: map[string]string{"in_hot_standby": "on"}}
	for _, name := range DriftExcludedSettings {
		sibling.Settings[name] = "there"
	}
	s.Siblings = []SiblingInfo{sibling}
	require.Empty(t, ruleByID(t, "config.drift").Evaluate(s))
}

// TestPackC_DriftReportsEverySettingInOrder: one finding per differing
// setting, the same on every pass. The rule used to stop at the first
// difference a map walk returned.
func TestPackC_DriftReportsEverySettingInOrder(t *testing.T) {
	s := configBase()
	s.Settings["random_page_cost"] = "1.1"
	sibling := uuid.New()
	s.Siblings = []SiblingInfo{{InstanceID: sibling, Settings: map[string]string{"work_mem": "8192", "random_page_cost": "4", "fsync": "on"}}}
	r := ruleByID(t, "config.drift")
	for range 5 {
		f := r.Evaluate(s)
		require.Len(t, f, 2)
		require.Equal(t, "setting:random_page_cost", f[0].ObjectName)
		require.Equal(t, "setting:work_mem", f[1].ObjectName)
		require.Equal(t, map[string]string{sibling.String(): "8192"}, f[1].Evidence["sibling_values"])
	}
}

func TestSettingDriftExcluded(t *testing.T) {
	require.True(t, SettingDriftExcluded("port", "postmaster"))
	require.True(t, SettingDriftExcluded("in_hot_standby", "internal"))
	require.False(t, SettingDriftExcluded("work_mem", "user"))
}

// TestPackC_ArchiveModeFollowsRole: archive_mode=always archives on a
// standby, "on" does not, and a standby whose primary archives has a
// backup strategy. "always" used to read as archiving disabled.
func TestPackC_ArchiveModeFollowsRole(t *testing.T) {
	always := configBase()
	always.Settings["archive_mode"] = "always"
	for _, id := range []string{"archive.disabled", "backup.no_strategy", "backup.no_basebackup_seen"} {
		require.Empty(t, ruleByID(t, id).Evaluate(always), id)
	}
	always.Metrics["pg_archiver_failed_ratio"][""] = 1
	require.NotEmpty(t, ruleByID(t, "archive.failing").Evaluate(always))

	standby := configBase()
	standby.Role = pgtype.RoleStandby
	standby.Metrics["pg_archiver_failed_ratio"][""] = 1
	require.Empty(t, ruleByID(t, "archive.failing").Evaluate(standby), "a standby with archive_mode=on does not archive")

	standby.Settings["archive_mode"] = "off"
	delete(standby.Facts, "backup")
	standby.Siblings = []SiblingInfo{{InstanceID: uuid.New(), Role: pgtype.RolePrimary, Settings: map[string]string{"archive_mode": "on"}}}
	for _, id := range []string{"archive.disabled", "backup.no_strategy"} {
		require.Empty(t, ruleByID(t, id).Evaluate(standby), id)
	}
}

// TestPackC_BackupRulesSeeTheClusterAndHistory: a base backup taken from a
// standby counts for the primary, a standby leaves cluster-wide findings to
// its primary, and a newly monitored instance is degraded, not critical.
func TestPackC_BackupRulesSeeTheClusterAndHistory(t *testing.T) {
	s := configBase()
	s.Settings["archive_mode"] = "off"
	delete(s.Facts, "backup")
	recent := configNow.Add(-2 * 24 * time.Hour)
	s.Siblings = []SiblingInfo{{InstanceID: uuid.New(), Role: pgtype.RoleStandby, Settings: map[string]string{"archive_mode": "off"}, BasebackupSeenAt: &recent}}
	require.Empty(t, ruleByID(t, "backup.no_strategy").Evaluate(s))

	s.Siblings = nil
	s.FirstSeen = configNow.Add(-time.Hour)
	f := ruleByID(t, "backup.no_strategy").Evaluate(s)
	require.Len(t, f, 1)
	require.Equal(t, StateDegraded, f[0].State)

	standby := configBase()
	standby.Role = pgtype.RoleStandby
	standby.Settings["archive_mode"] = "off"
	delete(standby.Facts, "backup")
	standby.Siblings = []SiblingInfo{{InstanceID: uuid.New(), Role: pgtype.RolePrimary, Settings: map[string]string{"archive_mode": "off"}}}
	require.Empty(t, ruleByID(t, "backup.no_strategy").Evaluate(standby), "the primary reports it")
	require.Empty(t, ruleByID(t, "archive.disabled").Evaluate(standby), "the primary reports it")
}

// TestPackC_ArchiveStalledNeedsWaitingSegments: an idle server has nothing
// to archive, however old its last archived segment is.
func TestPackC_ArchiveStalledNeedsWaitingSegments(t *testing.T) {
	s := configBase()
	s.Metrics["pg_archiver_last_archived_age_seconds"] = map[string]float64{"": 86400}
	s.Metrics["pg_archiver_ready_files"] = map[string]float64{"": 0}
	require.Empty(t, ruleByID(t, "archive.stalled").Evaluate(s))
	s.Metrics["pg_archiver_ready_files"][""] = 3
	f := ruleByID(t, "archive.stalled").Evaluate(s)
	require.Len(t, f, 1)
	require.Equal(t, 3.0, f[0].Evidence["ready_files"])
}

// TestPackC_ArchiveStalledWithoutReadyFilesIsDegraded: an idle archiver with
// no pg_monitor, so no count of waiting segments, is not a critical stall.
// The rule says it cannot tell, and it does not resolve an open stall either.
func TestPackC_ArchiveStalledWithoutReadyFilesIsDegraded(t *testing.T) {
	s := configBase()
	s.Metrics["pg_archiver_last_archived_age_seconds"] = map[string]float64{"": 86400}
	f := ruleByID(t, "archive.stalled").Evaluate(s)
	require.Len(t, f, 1)
	require.Equal(t, StateDegraded, f[0].State)
	require.Equal(t, "missing input: pg_archiver_ready_files", f[0].DegradedReason)
	require.True(t, degradedAsWhole(f), "the engine keeps the rule's open findings instead of resolving them")
	assertRuleContract(t, ruleByID(t, "archive.stalled"), s)
}

// TestPackC_WalKeepSizeTrustsSlotsAndSmallLag: a standby with a slot, or a
// lag under one segment, is not at risk.
func TestPackC_WalKeepSizeTrustsSlotsAndSmallLag(t *testing.T) {
	s := configBase()
	s.Settings["wal_keep_size_bytes"] = "0"
	s.Replication = &ReplicationInfo{Standbys: 1, MaxLagBytes: 64 * 1024 * 1024, ActivePhysicalSlots: 1}
	require.Empty(t, ruleByID(t, "config.wal_keep_size_low").Evaluate(s))
	s.Replication = &ReplicationInfo{Standbys: 1, MaxLagBytes: 1024}
	require.Empty(t, ruleByID(t, "config.wal_keep_size_low").Evaluate(s))
	s.Replication = &ReplicationInfo{Standbys: 2, MaxLagBytes: 64 * 1024 * 1024, ActivePhysicalSlots: 1}
	require.NotEmpty(t, ruleByID(t, "config.wal_keep_size_low").Evaluate(s))
}
func TestPackC_AllRulesSatisfyContract(t *testing.T) {
	s := &Snapshot{Settings: map[string]string{}, Metrics: map[string]map[string]float64{}}
	for _, r := range All() {
		if len(r.ID()) >= 7 && (r.ID()[:7] == "config." || r.ID()[:5] == "conn." || r.ID()[:8] == "archive." || r.ID()[:7] == "backup.") {
			assertRuleContract(t, r, s)
		}
	}
}
