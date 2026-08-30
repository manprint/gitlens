package advisor

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/manprint/pglens/internal/pgtype"
)

const (
	maxWorkMemShare     = 0.25
	minWorkMemBytes     = 4 * 1024 * 1024
	minMaintenanceBytes = 256 * 1024 * 1024
	archiveStallSeconds = 3600
)

type configRule struct {
	id       string
	severity Severity
	needs    []string
	eval     func(*Snapshot) []Finding
}

func (r configRule) ID() string                     { return r.id }
func (r configRule) Severity() Severity             { return r.severity }
func (r configRule) Scope() Scope                   { return ScopeInstance }
func (r configRule) Needs() []string                { return append([]string(nil), r.needs...) }
func (r configRule) MinTier() pgtype.PermTier       { return pgtype.TierReadOnly }
func (r configRule) Evaluate(s *Snapshot) []Finding { return r.eval(s) }

func settingFloat(s *Snapshot, key string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s.Settings[key]), 64)
	return v, err == nil
}
func configFinding(id string, sev Severity, title, detail, remediation string, evidence map[string]any) Finding {
	return Finding{RuleID: id, Severity: sev, State: StateOpen, Scope: ScopeInstance, Title: title, Detail: detail, Remediation: remediation, Evidence: evidence}
}
func degradedConfig(id string, sev Severity) Finding {
	return Finding{RuleID: id, Severity: sev, State: StateDegraded, Scope: ScopeInstance, Title: "Host metrics unavailable", Detail: "host metrics unavailable for this instance", DegradedReason: "host metrics unavailable for this instance"}
}
func usableMemory(s *Snapshot) (float64, bool) {
	if !s.Host.Available {
		return 0, false
	}
	if s.Host.CgroupLimitBytes > 0 {
		return s.Host.CgroupLimitBytes, true
	}
	return s.Host.TotalBytes, s.Host.TotalBytes > 0
}
func memoryRule(id string, sev Severity, eval func(float64, *Snapshot) []Finding) configRule {
	return configRule{id: id, severity: sev, needs: []string{"settings", "host"}, eval: func(s *Snapshot) []Finding {
		m, ok := usableMemory(s)
		if !ok {
			return []Finding{degradedConfig(id, sev)}
		}
		return eval(m, s)
	}}
}
func boolSetting(s *Snapshot, key string, def bool) bool {
	v, ok := s.Settings[key]
	if !ok {
		return def
	}
	return strings.EqualFold(v, "on") || strings.EqualFold(v, "true")
}
func metricAbove(id string, sev Severity, metric string, threshold float64, title, remediation string) configRule {
	return configRule{id: id, severity: sev, needs: []string{metric}, eval: func(s *Snapshot) []Finding {
		v, ok := metricValue(s, metric)
		if ok && v > threshold {
			return []Finding{configFinding(id, sev, title, fmt.Sprintf("%s is %.2f, above %.2f", metric, v, threshold), remediation, nil)}
		}
		return nil
	}}
}

func init() {
	Register(memoryRule("config.work_mem_oversized", SeverityWarning, func(mem float64, s *Snapshot) []Finding {
		w, wok := settingFloat(s, "work_mem_bytes")
		c, cok := settingFloat(s, "max_connections")
		if wok && cok && w*c > maxWorkMemShare*mem {
			return []Finding{configFinding("config.work_mem_oversized", SeverityWarning, "work_mem budget is oversized", fmt.Sprintf("work_mem times max_connections is %.1f%% of usable memory", w*c/mem*100), "Reduce work_mem or cap connections to protect memory", nil)}
		}
		return nil
	}))
	Register(configRule{id: "config.work_mem_low", severity: SeverityInfo, needs: []string{"settings", "query.temp_bytes_high"}, eval: func(s *Snapshot) []Finding {
		w, ok := settingFloat(s, "work_mem_bytes")
		if ok && w < minWorkMemBytes && len(ruleByPrefix(s, "query.temp_bytes_high")) > 0 {
			return []Finding{configFinding("config.work_mem_low", SeverityInfo, "work_mem is low", "work_mem is below 4 MiB while temporary writes are high", "Review work_mem with concurrent memory usage", nil)}
		}
		return nil
	}})
	Register(memoryRule("config.shared_buffers_low", SeverityWarning, func(mem float64, s *Snapshot) []Finding {
		v, ok := settingFloat(s, "shared_buffers_bytes")
		if ok && v < .15*mem {
			return []Finding{configFinding("config.shared_buffers_low", SeverityWarning, "shared_buffers is low", "shared_buffers is below 15% of usable memory", "Review shared_buffers for the workload", nil)}
		}
		return nil
	}))
	Register(memoryRule("config.shared_buffers_high", SeverityWarning, func(mem float64, s *Snapshot) []Finding {
		v, ok := settingFloat(s, "shared_buffers_bytes")
		if ok && v > .40*mem {
			return []Finding{configFinding("config.shared_buffers_high", SeverityWarning, "shared_buffers is high", "shared_buffers exceeds 40% of usable memory", "Leave memory for connections and the operating system", nil)}
		}
		return nil
	}))
	Register(memoryRule("config.effective_cache_size_mismatch", SeverityInfo, func(mem float64, s *Snapshot) []Finding {
		v, ok := settingFloat(s, "effective_cache_size_bytes")
		if ok && (v < .25*mem || v > 1.5*mem) {
			return []Finding{configFinding("config.effective_cache_size_mismatch", SeverityInfo, "effective_cache_size is mismatched", "effective_cache_size is far outside the expected memory range", "Align the estimate with usable memory and workload", nil)}
		}
		return nil
	}))
	Register(memoryRule("config.maintenance_work_mem_low", SeverityInfo, func(mem float64, s *Snapshot) []Finding {
		v, ok := settingFloat(s, "maintenance_work_mem_bytes")
		if ok && mem > 8*1024*1024*1024 && v < minMaintenanceBytes {
			return []Finding{configFinding("config.maintenance_work_mem_low", SeverityInfo, "maintenance_work_mem is low", "maintenance_work_mem is below 256 MiB on a large host", "Review maintenance memory for vacuum and index builds", nil)}
		}
		return nil
	}))
	Register(configRule{id: "config.max_connections_high", severity: SeverityWarning, needs: []string{"settings", "pg_connections_used_ratio"}, eval: func(s *Snapshot) []Finding {
		c, ok := settingFloat(s, "max_connections")
		u, _ := metricValue(s, "pg_connections_used_ratio")
		if ok && c > 200 && !boolSetting(s, "pooler_observed", false) && u < .3 {
			return []Finding{configFinding("config.max_connections_high", SeverityWarning, "max_connections is high", "many configured connections are rarely used", "Use a pooler or reduce max_connections after workload review", nil)}
		}
		return nil
	}})
	Register(configRule{id: "config.track_io_timing_off", severity: SeverityInfo, needs: []string{"settings"}, eval: func(s *Snapshot) []Finding {
		if !boolSetting(s, "track_io_timing", true) {
			return []Finding{configFinding("config.track_io_timing_off", SeverityInfo, "I/O timing is disabled", "track_io_timing is off", "Enable it when I/O diagnosis is required", nil)}
		}
		return nil
	}})
	Register(metricAbove("config.checkpoints_too_frequent", SeverityWarning, "pg_checkpoints_requested_ratio", .20, "Checkpoints are too frequent", "Increase max_wal_size after sizing storage"))
	Register(configRule{id: "config.wal_keep_size_low", severity: SeverityWarning, needs: []string{"settings", "pg_replication_lag_bytes"}, eval: func(s *Snapshot) []Finding {
		lag, _ := metricValue(s, "pg_replication_lag_bytes")
		keep, _ := settingFloat(s, "wal_keep_size_bytes")
		if keep > 0 && lag > keep && s.Settings["replication_slot_protects"] != "true" {
			return []Finding{configFinding("config.wal_keep_size_low", SeverityWarning, "wal_keep_size is low", "replication lag exceeds wal_keep_size without a protecting slot", "Increase wal_keep_size or configure a replication slot", nil)}
		}
		return nil
	}})
	Register(configRule{id: "config.fsync_off", severity: SeverityCritical, needs: []string{"settings"}, eval: func(s *Snapshot) []Finding {
		if !boolSetting(s, "fsync", true) {
			return []Finding{configFinding("config.fsync_off", SeverityCritical, "fsync is disabled", "a power failure can lose committed data", "Enable fsync in production to preserve durability", nil)}
		}
		return nil
	}})
	Register(configRule{id: "config.full_page_writes_off", severity: SeverityCritical, needs: []string{"settings"}, eval: func(s *Snapshot) []Finding {
		if !boolSetting(s, "full_page_writes", true) {
			return []Finding{configFinding("config.full_page_writes_off", SeverityCritical, "full page writes are disabled", "the first post-checkpoint page image is not protected", "Enable full_page_writes unless storage guarantees replacement", nil)}
		}
		return nil
	}})
	Register(configRule{id: "config.drift", severity: SeverityWarning, needs: []string{"settings", "Siblings"}, eval: func(s *Snapshot) []Finding {
		excluded := map[string]bool{"primary_conninfo": true, "port": true, "listen_addresses": true}
		for k, v := range s.Settings {
			if excluded[k] {
				continue
			}
			for _, sib := range s.Siblings {
				if other, ok := sib.Settings[k]; ok && other != v {
					return []Finding{configFinding("config.drift", SeverityWarning, "Configuration drift", fmt.Sprintf("setting %s differs between cluster members", k), "Reconcile shared settings while retaining intentional per-instance values", map[string]any{"setting": k})}
				}
			}
		}
		return nil
	}})
	Register(metricAbove("conn.saturation", SeverityWarning, "pg_connections_used_ratio", .80, "Connections are saturated", "Add capacity or use a pooler"))
	Register(configRule{id: "conn.idle_share_high", severity: SeverityInfo, needs: []string{"settings", "pg_connections_idle_share"}, eval: func(s *Snapshot) []Finding {
		idle, _ := metricValue(s, "pg_connections_idle_share")
		c, _ := settingFloat(s, "connections_count")
		if idle > .70 && c > 100 {
			return []Finding{configFinding("conn.idle_share_high", SeverityInfo, "Idle connection share is high", "more than 70% of connections are idle", "Use pooling and review client connection lifetimes", nil)}
		}
		return nil
	}})
	Register(configRule{id: "archive.disabled", severity: SeverityInfo, needs: []string{"settings"}, eval: func(s *Snapshot) []Finding {
		if !boolSetting(s, "archive_mode", true) {
			return []Finding{configFinding("archive.disabled", SeverityInfo, "Archiving is disabled", "archive_mode is off", "Confirm an alternate backup strategy", nil)}
		}
		return nil
	}})
	Register(metricAbove("archive.failing", SeverityCritical, "pg_archiver_failed_ratio", .999, "Archiving is failing", "Repair archive_command and verify WAL retention"))
	Register(configRule{id: "archive.stalled", severity: SeverityCritical, needs: []string{"settings", "pg_archiver_last_archived_age_seconds"}, eval: func(s *Snapshot) []Finding {
		age, _ := metricValue(s, "pg_archiver_last_archived_age_seconds")
		timeout, _ := settingFloat(s, "archive_timeout_seconds")
		limit := float64(archiveStallSeconds)
		if timeout > 0 && timeout*10 > limit {
			limit = timeout * 10
		}
		if age > limit {
			return []Finding{configFinding("archive.stalled", SeverityCritical, "Archiving is stalled", fmt.Sprintf("last archive is %.0f seconds old", age), "Repair archiving before WAL retention is exhausted", nil)}
		}
		return nil
	}})
	Register(configRule{id: "backup.no_basebackup_seen", severity: SeverityWarning, needs: []string{"settings", "pg_basebackup_age_days"}, eval: func(s *Snapshot) []Finding {
		if !boolSetting(s, "archive_mode", false) {
			return nil
		}
		age, ok := metricValue(s, "pg_basebackup_age_days")
		if !ok {
			return []Finding{{RuleID: "backup.no_basebackup_seen", Severity: SeverityWarning, State: StateDegraded, Scope: ScopeInstance, Title: "Backup history unavailable", Detail: "base backup history is shorter than 7 days", DegradedReason: "insufficient backup history"}}
		}
		if age < 7 {
			return nil
		}
		return []Finding{configFinding("backup.no_basebackup_seen", SeverityWarning, "No recent base backup", "no base backup has been observed for at least 7 days", "Establish and verify a regular base backup schedule", map[string]any{"age_days": age})}
	}})
	Register(configRule{id: "backup.no_strategy", severity: SeverityCritical, needs: []string{"settings"}, eval: func(s *Snapshot) []Finding {
		if !boolSetting(s, "archive_mode", false) && !boolSetting(s, "basebackup_seen", false) {
			return []Finding{configFinding("backup.no_strategy", SeverityCritical, "No visible backup strategy", "archiving is off and no base backup was observed", "Configure archiving or establish a verified base backup strategy", nil)}
		}
		return nil
	}})
}

func ruleByPrefix(s *Snapshot, id string) []Finding {
	for _, r := range All() {
		if r.ID() == id {
			return r.Evaluate(s)
		}
	}
	return nil
}
