package advisor

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func configBase() *Snapshot {
	return &Snapshot{Host: HostInfo{Available: true, TotalBytes: 16 * 1024 * 1024 * 1024}, Settings: map[string]string{
		"work_mem_bytes": "4194304", "max_connections": "100", "shared_buffers_bytes": "4294967296",
		"effective_cache_size_bytes": "8589934592", "maintenance_work_mem_bytes": "536870912", "track_io_timing": "on",
		"fsync": "on", "full_page_writes": "on", "archive_mode": "on", "basebackup_seen": "true",
	}, Metrics: map[string]map[string]float64{"pg_basebackup_age_days": {"": 1}}}
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
			s.Metrics["pg_replication_lag_bytes"] = map[string]float64{"": 11}
			return s
		}(), configBase()},
		{"config.fsync_off", func() *Snapshot { s := configBase(); s.Settings["fsync"] = "off"; return s }(), configBase()},
		{"config.full_page_writes_off", func() *Snapshot { s := configBase(); s.Settings["full_page_writes"] = "off"; return s }(), configBase()},
		{"config.drift", func() *Snapshot {
			s := configBase()
			s.Siblings = []SiblingInfo{{Settings: map[string]string{"work_mem_bytes": "8"}}}
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
			s.Settings["connections_count"] = "101"
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
			return s
		}(), configBase()},
		{"backup.no_basebackup_seen", func() *Snapshot {
			s := configBase()
			delete(s.Settings, "basebackup_seen")
			s.Metrics["pg_basebackup_age_days"] = map[string]float64{"": 8}
			return s
		}(), configBase()},
		{"backup.no_strategy", func() *Snapshot {
			s := configBase()
			s.Settings["archive_mode"] = "off"
			delete(s.Settings, "basebackup_seen")
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
	s.Siblings = []SiblingInfo{{Settings: map[string]string{"port": "9999", "listen_addresses": "*", "primary_conninfo": "x"}}}
	require.Empty(t, ruleByID(t, "config.drift").Evaluate(s))
}
func TestPackC_AllRulesSatisfyContract(t *testing.T) {
	s := &Snapshot{Settings: map[string]string{}, Metrics: map[string]map[string]float64{}}
	for _, r := range All() {
		if len(r.ID()) >= 7 && (r.ID()[:7] == "config." || r.ID()[:5] == "conn." || r.ID()[:8] == "archive." || r.ID()[:7] == "backup.") {
			assertRuleContract(t, r, s)
		}
	}
}
