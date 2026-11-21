package advisor

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/manprint/pglens/internal/i18n"
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
	// "no base backup seen" means something: the default of
	// Windows.BackupHistory, which the console can change.
	backupHistory = 7 * 24 * time.Hour
	// basebackupRecentAge is the age below which a base backup is recent. It
	// is fixed, not the configured window: a shorter window must not make a
	// two-day-old backup stale.
	basebackupRecentAge = 7 * 24 * time.Hour
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

func degradedBackup(id string, sev Severity, window time.Duration) Finding {
	return Finding{RuleID: id, Severity: sev, State: StateDegraded, Scope: ScopeInstance, DegradedReason: "insufficient backup history"}.say("backup.history_unavailable", i18n.Args{"window": windowText(window)})
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

// configMessage is an open config finding whose text is the catalog entry
// of its own rule id.
func configMessage(id string, sev Severity, args i18n.Args, evidence map[string]any) Finding {
	return Finding{RuleID: id, Severity: sev, State: StateOpen, Scope: ScopeInstance, Evidence: evidence}.say(id, args)
}

func configFinding(id string, sev Severity, title, detail, remediation string, evidence map[string]any) Finding {
	return Finding{RuleID: id, Severity: sev, State: StateOpen, Scope: ScopeInstance, Title: title, Detail: detail, Remediation: remediation, Evidence: evidence}
}
func degradedConfig(id string, sev Severity) Finding {
	return Finding{RuleID: id, Severity: sev, State: StateDegraded, Scope: ScopeInstance, DegradedReason: "host metrics unavailable for this instance"}.say("config.host_unavailable", nil)
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
func metricAbove(id string, sev Severity, metric string, threshold float64) configRule {
	return configRule{id: id, severity: sev, needs: []string{metric}, eval: func(s *Snapshot) []Finding {
		v, ok := metricValue(s, metric)
		if ok && v > threshold {
			return []Finding{Finding{RuleID: id, Severity: sev, State: StateOpen, Scope: ScopeInstance}.say(id, i18n.Args{"metric": metric, "observed": i18n.Fixed{V: v, Digits: 2}, "threshold": i18n.Fixed{V: threshold, Digits: 2}})}
		}
		return nil
	}}
}

func init() {
	Register(memoryRule("config.work_mem_oversized", SeverityWarning, func(mem float64, s *Snapshot) []Finding {
		w, wok := settingFloat(s, "work_mem_bytes")
		c, cok := settingFloat(s, "max_connections")
		if wok && cok && w*c > maxWorkMemShare*mem {
			return []Finding{configMessage("config.work_mem_oversized", SeverityWarning, i18n.Args{"share_pct": i18n.Fixed{V: w * c / mem * 100, Digits: 1}}, nil)}
		}
		return nil
	}))
	Register(configRule{id: "config.work_mem_low", severity: SeverityInfo, needs: []string{"settings", "query.temp_bytes_high"}, eval: func(s *Snapshot) []Finding {
		w, ok := settingFloat(s, "work_mem_bytes")
		if ok && w < minWorkMemBytes && len(ruleByPrefix(s, "query.temp_bytes_high")) > 0 {
			var evidence map[string]any
			if files, hasFiles := tempFiles(s); hasFiles {
				evidence = map[string]any{"pg_temp_files_total": files}
			}
			return []Finding{configMessage("config.work_mem_low", SeverityInfo, nil, evidence)}
		}
		return nil
	}})
	Register(memoryRule("config.shared_buffers_low", SeverityWarning, func(mem float64, s *Snapshot) []Finding {
		v, ok := settingFloat(s, "shared_buffers_bytes")
		if ok && v < .15*mem {
			return []Finding{configMessage("config.shared_buffers_low", SeverityWarning, nil, nil)}
		}
		return nil
	}))
	Register(memoryRule("config.shared_buffers_high", SeverityWarning, func(mem float64, s *Snapshot) []Finding {
		v, ok := settingFloat(s, "shared_buffers_bytes")
		if ok && v > .40*mem {
			return []Finding{configMessage("config.shared_buffers_high", SeverityWarning, nil, nil)}
		}
		return nil
	}))
	Register(memoryRule("config.effective_cache_size_mismatch", SeverityInfo, func(mem float64, s *Snapshot) []Finding {
		v, ok := settingFloat(s, "effective_cache_size_bytes")
		if ok && (v < .25*mem || v > 1.5*mem) {
			return []Finding{configMessage("config.effective_cache_size_mismatch", SeverityInfo, nil, nil)}
		}
		return nil
	}))
	Register(memoryRule("config.maintenance_work_mem_low", SeverityInfo, func(mem float64, s *Snapshot) []Finding {
		v, ok := settingFloat(s, "maintenance_work_mem_bytes")
		if ok && mem > 8*1024*1024*1024 && v < minMaintenanceBytes {
			return []Finding{configMessage("config.maintenance_work_mem_low", SeverityInfo, nil, nil)}
		}
		return nil
	}))
	Register(configRule{id: "config.max_connections_high", severity: SeverityWarning, needs: []string{"settings", "pg_connections_used_ratio"}, eval: func(s *Snapshot) []Finding {
		c, ok := settingFloat(s, "max_connections")
		u, _ := metricValue(s, "pg_connections_used_ratio")
		if ok && c > 200 && u < .3 {
			return []Finding{configMessage("config.max_connections_high", SeverityWarning, i18n.Args{"max_connections": fmt.Sprintf("%.0f", c), "used_pct": fmt.Sprintf("%.0f", u*100)}, map[string]any{"max_connections": c, "used_ratio": u})}
		}
		return nil
	}})
	Register(configRule{id: "config.track_io_timing_off", severity: SeverityInfo, needs: []string{"settings"}, eval: func(s *Snapshot) []Finding {
		if !boolSetting(s, "track_io_timing", true) {
			return []Finding{configMessage("config.track_io_timing_off", SeverityInfo, nil, nil)}
		}
		return nil
	}})
	Register(metricAbove("config.checkpoints_too_frequent", SeverityWarning, "pg_checkpoints_requested_ratio", .20))
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
			return []Finding{configMessage("config.wal_keep_size_low", SeverityWarning, i18n.Args{"lag_bytes": fmt.Sprintf("%.0f", r.MaxLagBytes), "keep_bytes": fmt.Sprintf("%.0f", keep), "without_slot": fmt.Sprintf("%d", r.Standbys-r.ActivePhysicalSlots), "standbys": fmt.Sprintf("%d", r.Standbys)}, map[string]any{"lag_bytes": r.MaxLagBytes, "wal_keep_size_bytes": keep, "standbys": r.Standbys, "active_physical_slots": r.ActivePhysicalSlots})}
		}
		return nil
	}})
	Register(configRule{id: "config.fsync_off", severity: SeverityCritical, needs: []string{"settings"}, eval: func(s *Snapshot) []Finding {
		if !boolSetting(s, "fsync", true) {
			return []Finding{configMessage("config.fsync_off", SeverityCritical, nil, nil)}
		}
		return nil
	}})
	Register(configRule{id: "config.full_page_writes_off", severity: SeverityCritical, needs: []string{"settings"}, eval: func(s *Snapshot) []Finding {
		if !boolSetting(s, "full_page_writes", true) {
			return []Finding{configMessage("config.full_page_writes_off", SeverityCritical, nil, nil)}
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
			f := configMessage("config.drift", SeverityWarning, i18n.Args{"setting": k, "value": strconv.Quote(v), "siblings": strings.Join(names, ", ")}, map[string]any{"setting": k, "value": v, "sibling_values": others})
			f.ObjectName = "setting:" + k
			out = append(out, f)
		}
		return out
	}})
	Register(metricAbove("conn.saturation", SeverityWarning, "pg_connections_used_ratio", .80))
	Register(configRule{id: "conn.idle_share_high", severity: SeverityInfo, needs: []string{"pg_connections_idle_share", "pg_connections_used"}, eval: func(s *Snapshot) []Finding {
		// The share is derived from pg_backends by the snapshot and the
		// count is the activity check's own: both used to be inputs
		// nothing produced.
		idle, _ := metricValue(s, "pg_connections_idle_share")
		c, _ := metricValue(s, "pg_connections_used")
		if idle > .70 && c > 100 {
			return []Finding{configMessage("conn.idle_share_high", SeverityInfo, i18n.Args{"idle_pct": fmt.Sprintf("%.0f", idle*100), "connections": fmt.Sprintf("%.0f", c)}, map[string]any{"idle_share": idle, "connections": c})}
		}
		return nil
	}})
	Register(configRule{id: "archive.disabled", severity: SeverityInfo, needs: []string{"settings"}, eval: func(s *Snapshot) []Finding {
		mode := archiveMode(s.Settings)
		if mode == "" || deferToPrimary(s) || clusterArchiving(s) {
			return nil
		}
		return []Finding{configMessage("archive.disabled", SeverityInfo, i18n.Args{"archive_mode": mode}, map[string]any{"archive_mode": mode})}
	}})
	Register(configRule{id: "archive.failing", severity: SeverityCritical, needs: []string{"settings"}, eval: func(s *Snapshot) []Finding {
		if !archivingActive(s.Role, archiveMode(s.Settings)) {
			return nil
		}
		v, ok := metricValue(s, "pg_archiver_failed_ratio")
		if !ok {
			return []Finding{Finding{RuleID: "archive.failing", Severity: SeverityCritical, State: StateDegraded, Scope: ScopeInstance, DegradedReason: "missing input: pg_archiver_failed_ratio"}.say("archive.failing.unavailable", nil)}
		}
		if v > .999 {
			return []Finding{configMessage("archive.failing", SeverityCritical, nil, map[string]any{"failed_ratio": v})}
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
			// Without the count of waiting segments an idle archiver and a
			// stuck one look the same. Reporting critical then would flag
			// every idle server whose agent lacks pg_monitor, so the rule
			// says it cannot tell, as archive.failing does when its input is
			// missing.
			if !readyKnown {
				return []Finding{Finding{RuleID: "archive.stalled", Severity: SeverityCritical, State: StateDegraded, Scope: ScopeInstance, DegradedReason: "missing input: pg_archiver_ready_files"}.say("archive.stalled.unconfirmed", i18n.Args{"age_seconds": fmt.Sprintf("%.0f", age)})}
			}
			evidence := map[string]any{"last_archived_age_seconds": age, "limit_seconds": limit, "ready_files": ready}
			return []Finding{configMessage("archive.stalled", SeverityCritical, i18n.Args{"age_seconds": fmt.Sprintf("%.0f", age), "ready_files": fmt.Sprintf("%.0f", ready)}, evidence)}
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
			if age < basebackupRecentAge {
				return nil
			}
			days := age.Hours() / 24
			return []Finding{Finding{RuleID: "backup.no_basebackup_seen", Severity: SeverityWarning, State: StateOpen, Scope: ScopeInstance, Evidence: map[string]any{"age_days": days}}.say("backup.no_basebackup_seen.stale", i18n.Args{"age_days": fmt.Sprintf("%.0f", days)})}
		}
		if window := s.windows().BackupHistory; monitoredFor(s) < window {
			return []Finding{degradedBackup("backup.no_basebackup_seen", SeverityWarning, window)}
		}
		days := monitoredFor(s).Hours() / 24
		return []Finding{configMessage("backup.no_basebackup_seen", SeverityWarning, i18n.Args{"monitored_days": fmt.Sprintf("%.0f", days)}, map[string]any{"monitored_days": days})}
	}})
	Register(configRule{id: "backup.no_strategy", severity: SeverityCritical, needs: []string{"settings"}, eval: func(s *Snapshot) []Finding {
		// basebackup_seen, which this rule read, was never set: every
		// instance with archive_mode off was critical, standbys whose
		// primary archives and archive_mode=always included.
		if archiveMode(s.Settings) == "" || deferToPrimary(s) || clusterArchiving(s) {
			return nil
		}
		last, seen := lastBasebackup(s)
		if seen && s.Now.Sub(last) <= basebackupMaxAge {
			return nil
		}
		// A base backup seen more than 30 days ago already answers, however
		// short the monitored history against the window: degrading would
		// say that no base backup has been seen yet, which is false.
		if window := s.windows().BackupHistory; !seen && monitoredFor(s) < window {
			return []Finding{degradedBackup("backup.no_strategy", SeverityCritical, window)}
		}
		// The detail names what pglens watched: a base backup seen too long
		// ago, or none at all in the monitored history, which may be shorter
		// than 30 days.
		if seen {
			return []Finding{Finding{RuleID: "backup.no_strategy", Severity: SeverityCritical, State: StateOpen, Scope: ScopeInstance}.say("backup.no_strategy.recent", nil)}
		}
		days := monitoredFor(s).Hours() / 24
		return []Finding{configMessage("backup.no_strategy", SeverityCritical, i18n.Args{"monitored_days": fmt.Sprintf("%.0f", days)}, map[string]any{"monitored_days": days})}
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
