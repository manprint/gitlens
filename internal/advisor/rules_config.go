package advisor

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
)

const (
	maxWorkMemShare     = 0.25
	minWorkMemBytes     = 4 * 1024 * 1024
	minMaintenanceBytes = 256 * 1024 * 1024
	archiveStallSeconds = 3600
	// walSegmentBytes is the default WAL segment size, the smallest unit of
	// WAL a primary recycles.
	walSegmentBytes = 16 * 1024 * 1024
	// backupHistory is how long an instance must have been watched before
	// "no base backup seen" means something.
	backupHistory = 7 * 24 * time.Hour
	// basebackupMaxAge is the age past which a base backup no longer counts
	// as a backup strategy.
	basebackupMaxAge = 30 * 24 * time.Hour
)

// DriftExcludedSettings are settings expected to differ between members of
// one cluster. The settings-drift API and the config.drift rule share the
// list: the rule used to exclude only three of them and reported, as drift,
// differences the drift view deliberately hides.
var DriftExcludedSettings = []string{
	"data_directory", "hot_standby", "listen_addresses", "port",
	"primary_conninfo", "primary_slot_name", "synchronous_standby_names",
}

// SettingDriftExcluded reports whether a difference in a setting between
// cluster members is expected rather than drift. Internal settings are fixed
// at build or initdb time or report server state (in_hot_standby).
func SettingDriftExcluded(name, context string) bool {
	return context == "internal" || slices.Contains(DriftExcludedSettings, name)
}

// archiveMode is the archive_mode setting ("off", "on", "always"), or ""
// before the settings have been collected.
func archiveMode(settings map[string]string) string {
	return strings.ToLower(strings.TrimSpace(settings["archive_mode"]))
}

// archivingActive reports whether an instance archives WAL. A standby
// archives only with archive_mode=always; "always" used to read as off
// everywhere, and "on" as archiving on a standby, where it does nothing.
func archivingActive(role pgtype.Role, mode string) bool {
	switch mode {
	case "always":
		return true
	case "on":
		return role != pgtype.RoleStandby
	}
	return false
}

// clusterArchiving reports whether any known member of the cluster archives
// WAL: a standby with archive_mode off is covered by its primary's archive.
func clusterArchiving(s *Snapshot) bool {
	if archivingActive(s.Role, archiveMode(s.Settings)) {
		return true
	}
	for _, sib := range s.Siblings {
		if archivingActive(sib.Role, archiveMode(sib.Settings)) {
			return true
		}
	}
	return false
}

// deferToPrimary reports whether a cluster-wide finding belongs to a
// monitored primary instead of to this standby, so that it is raised once.
func deferToPrimary(s *Snapshot) bool {
	if s.Role != pgtype.RoleStandby {
		return false
	}
	for _, sib := range s.Siblings {
		if sib.Role == pgtype.RolePrimary {
			return true
		}
	}
	return false
}

// lastBasebackup is the last time a base backup was seen streaming from any
// member of the cluster: backups are often taken from a standby.
func lastBasebackup(s *Snapshot) (time.Time, bool) {
	var last time.Time
	for _, f := range s.Facts["backup"] {
		if f.ObservedAt.After(last) {
			last = f.ObservedAt
		}
	}
	for _, sib := range s.Siblings {
		if sib.BasebackupSeenAt != nil && sib.BasebackupSeenAt.After(last) {
			last = *sib.BasebackupSeenAt
		}
	}
	return last, !last.IsZero()
}

// monitoredFor is how long this instance has been monitored.
func monitoredFor(s *Snapshot) time.Duration {
	if s.FirstSeen.IsZero() || s.Now.IsZero() {
		return 0
	}
	return s.Now.Sub(s.FirstSeen)
}

func degradedBackup(id string, sev Severity) Finding {
	return Finding{RuleID: id, Severity: sev, State: StateDegraded, Scope: ScopeInstance, Title: "Backup history unavailable", Detail: "the instance has been monitored for less than 7 days and no base backup has been seen yet", DegradedReason: "insufficient backup history"}
}

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
		if ok && c > 200 && u < .3 {
			return []Finding{configFinding("config.max_connections_high", SeverityWarning, "max_connections is high", fmt.Sprintf("max_connections is %.0f and %.0f%% of it is in use", c, u*100), "Use a pooler or reduce max_connections after workload review", map[string]any{"max_connections": c, "used_ratio": u})}
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
	Register(configRule{id: "config.wal_keep_size_low", severity: SeverityWarning, needs: []string{"settings", "Replication"}, eval: func(s *Snapshot) []Finding {
		// The lag and the slot protection used to come from a metric and a
		// setting nothing produced, so the rule could never fire. A standby
		// is protected when every streaming standby can have an active
		// physical slot: which slot serves which standby is not visible.
		r := s.Replication
		if r == nil || r.Standbys == 0 || r.ActivePhysicalSlots >= r.Standbys {
			return nil
		}
		keep, ok := settingFloat(s, "wal_keep_size_bytes")
		if !ok {
			return nil
		}
		segment, ok := settingFloat(s, "wal_segment_size_bytes")
		if !ok || segment <= 0 {
			segment = walSegmentBytes
		}
		if r.MaxLagBytes > keep && r.MaxLagBytes > segment {
			return []Finding{configFinding("config.wal_keep_size_low", SeverityWarning, "wal_keep_size is low", fmt.Sprintf("a standby is %.0f bytes of WAL behind, more than wal_keep_size (%.0f bytes), and %d of %d streaming standbys have no active physical slot", r.MaxLagBytes, keep, r.Standbys-r.ActivePhysicalSlots, r.Standbys), "Increase wal_keep_size or configure a replication slot", map[string]any{"lag_bytes": r.MaxLagBytes, "wal_keep_size_bytes": keep, "standbys": r.Standbys, "active_physical_slots": r.ActivePhysicalSlots})}
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
		// One finding per differing setting, in a stable order: the rule
		// used to stop at the first difference of a random map walk, so
		// its subject changed from pass to pass and muting it hid every
		// other difference too.
		keys := make([]string, 0, len(s.Settings))
		for k := range s.Settings {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var out []Finding
		for _, k := range keys {
			if SettingDriftExcluded(k, s.SettingContexts[k]) {
				continue
			}
			v := s.Settings[k]
			others := map[string]string{}
			var names []string
			for _, sib := range s.Siblings {
				if other, ok := sib.Settings[k]; ok && other != v && !SettingDriftExcluded(k, sib.SettingContexts[k]) {
					others[sib.InstanceID.String()] = other
					names = append(names, fmt.Sprintf("%q on %s", other, sib.InstanceID))
				}
			}
			if len(others) == 0 {
				continue
			}
			f := configFinding("config.drift", SeverityWarning, "Configuration drift", fmt.Sprintf("setting %s is %q here and %s", k, v, strings.Join(names, ", ")), "Reconcile shared settings while retaining intentional per-instance values", map[string]any{"setting": k, "value": v, "sibling_values": others})
			f.ObjectName = "setting:" + k
			out = append(out, f)
		}
		return out
	}})
	Register(metricAbove("conn.saturation", SeverityWarning, "pg_connections_used_ratio", .80, "Connections are saturated", "Add capacity or use a pooler"))
	Register(configRule{id: "conn.idle_share_high", severity: SeverityInfo, needs: []string{"pg_connections_idle_share", "pg_connections_used"}, eval: func(s *Snapshot) []Finding {
		// The share is derived from pg_backends by the snapshot and the
		// count is the activity check's own: both used to be inputs
		// nothing produced.
		idle, _ := metricValue(s, "pg_connections_idle_share")
		c, _ := metricValue(s, "pg_connections_used")
		if idle > .70 && c > 100 {
			return []Finding{configFinding("conn.idle_share_high", SeverityInfo, "Idle connection share is high", fmt.Sprintf("%.0f%% of %.0f client connections are idle", idle*100, c), "Use pooling and review client connection lifetimes", map[string]any{"idle_share": idle, "connections": c})}
		}
		return nil
	}})
	Register(configRule{id: "archive.disabled", severity: SeverityInfo, needs: []string{"settings"}, eval: func(s *Snapshot) []Finding {
		mode := archiveMode(s.Settings)
		if mode == "" || deferToPrimary(s) || clusterArchiving(s) {
			return nil
		}
		return []Finding{configFinding("archive.disabled", SeverityInfo, "Archiving is disabled", fmt.Sprintf("no member of the cluster archives WAL (archive_mode is %s here)", mode), "Confirm an alternate backup strategy", map[string]any{"archive_mode": mode})}
	}})
	Register(configRule{id: "archive.failing", severity: SeverityCritical, needs: []string{"settings"}, eval: func(s *Snapshot) []Finding {
		if !archivingActive(s.Role, archiveMode(s.Settings)) {
			return nil
		}
		v, ok := metricValue(s, "pg_archiver_failed_ratio")
		if !ok {
			return []Finding{{RuleID: "archive.failing", Severity: SeverityCritical, State: StateDegraded, Scope: ScopeInstance, Title: "Archiver statistics unavailable", Detail: "archiving is enabled but no archiver statistics were received", DegradedReason: "missing input: pg_archiver_failed_ratio"}}
		}
		if v > .999 {
			return []Finding{configFinding("archive.failing", SeverityCritical, "Archiving is failing", "the last archive attempt failed after the last success", "Repair archive_command and verify WAL retention", map[string]any{"failed_ratio": v})}
		}
		return nil
	}})
	Register(configRule{id: "archive.stalled", severity: SeverityCritical, needs: []string{"settings"}, eval: func(s *Snapshot) []Finding {
		if !archivingActive(s.Role, archiveMode(s.Settings)) {
			return nil
		}
		age, ok := metricValue(s, "pg_archiver_last_archived_age_seconds")
		if !ok {
			return nil
		}
		// An idle server writes no WAL and has nothing to archive: the age
		// of the last archived segment grows without anything being stuck.
		// Agents that can list pg_wal/archive_status report the segments
		// waiting, and none waiting is not a stall.
		ready, readyKnown := metricValue(s, "pg_archiver_ready_files")
		if readyKnown && ready == 0 {
			return nil
		}
		timeout, _ := settingFloat(s, "archive_timeout_seconds")
		limit := float64(archiveStallSeconds)
		if timeout > 0 && timeout*10 > limit {
			limit = timeout * 10
		}
		if age > limit {
			detail := fmt.Sprintf("last archive is %.0f seconds old", age)
			evidence := map[string]any{"last_archived_age_seconds": age, "limit_seconds": limit}
			if readyKnown {
				detail += fmt.Sprintf(" and %.0f WAL segments are waiting", ready)
				evidence["ready_files"] = ready
			}
			return []Finding{configFinding("archive.stalled", SeverityCritical, "Archiving is stalled", detail, "Repair archiving before WAL retention is exhausted", evidence)}
		}
		return nil
	}})
	Register(configRule{id: "backup.no_basebackup_seen", severity: SeverityWarning, needs: []string{"settings"}, eval: func(s *Snapshot) []Finding {
		// pg_basebackup_age_days, which this rule read, was never produced:
		// every archiving instance was degraded forever. The server now
		// records when a base backup was last seen streaming, from any
		// member of the cluster.
		if deferToPrimary(s) || !clusterArchiving(s) {
			return nil
		}
		if last, ok := lastBasebackup(s); ok {
			age := s.Now.Sub(last)
			if age < backupHistory {
				return nil
			}
			days := age.Hours() / 24
			return []Finding{configFinding("backup.no_basebackup_seen", SeverityWarning, "No recent base backup", fmt.Sprintf("the last base backup seen streaming from the cluster is %.0f days old", days), "Establish and verify a regular base backup schedule; backups copied outside the replication protocol (file-level tools, storage snapshots) are not visible here", map[string]any{"age_days": days})}
		}
		if monitoredFor(s) < backupHistory {
			return []Finding{degradedBackup("backup.no_basebackup_seen", SeverityWarning)}
		}
		days := monitoredFor(s).Hours() / 24
		return []Finding{configFinding("backup.no_basebackup_seen", SeverityWarning, "No recent base backup", fmt.Sprintf("no base backup has been seen streaming from the cluster in %.0f days of monitoring", days), "Establish and verify a regular base backup schedule; backups copied outside the replication protocol (file-level tools, storage snapshots) are not visible here", map[string]any{"monitored_days": days})}
	}})
	Register(configRule{id: "backup.no_strategy", severity: SeverityCritical, needs: []string{"settings"}, eval: func(s *Snapshot) []Finding {
		// basebackup_seen, which this rule read, was never set: every
		// instance with archive_mode off was critical, standbys whose
		// primary archives and archive_mode=always included.
		if archiveMode(s.Settings) == "" || deferToPrimary(s) || clusterArchiving(s) {
			return nil
		}
		if last, ok := lastBasebackup(s); ok && s.Now.Sub(last) <= basebackupMaxAge {
			return nil
		}
		if monitoredFor(s) < backupHistory {
			return []Finding{degradedBackup("backup.no_strategy", SeverityCritical)}
		}
		return []Finding{configFinding("backup.no_strategy", SeverityCritical, "No visible backup strategy", "no member of the cluster archives WAL and no base backup has been seen streaming in the last 30 days", "Configure archiving or establish a verified base backup strategy", nil)}
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
