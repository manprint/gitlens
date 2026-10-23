package advisor

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// recoveryConflictAdvice says, per reason of pg_stat_database_conflicts, what
// the replayed change was and what avoids the cancellation.
var recoveryConflictAdvice = map[string]string{
	"snapshot":           "snapshot: replay removed rows a standby query could still see (VACUUM on the primary). hot_standby_feedback = on keeps them on the primary, at the cost of bloat there.",
	"lock":               "lock: replay took an ACCESS EXCLUSIVE lock (TRUNCATE, DROP, ALTER TABLE, or VACUUM truncating a table's empty tail, which ALTER TABLE ... SET (vacuum_truncate = off) stops).",
	"bufferpin":          "bufferpin: replay waited for a page a standby query kept pinned.",
	"deadlock":           "deadlock: a standby query and replay waited for each other on a buffer pin.",
	"tablespace":         "tablespace: replay dropped a tablespace that standby queries used for temporary files; point temp_tablespaces elsewhere on the standby.",
	"active_logicalslot": "active_logicalslot: a logical slot on the standby was invalidated because the primary removed rows its decoding needs; hot_standby_feedback = on with a physical slot between primary and standby prevents it.",
}

// walDirectoryMultiple is how many times max_wal_size pg_wal may hold, beyond
// wal_keep_size, before the WAL held back is reported. Checkpoints keep
// pg_wal near max_wal_size, and recycled segments add at most as much again.
const walDirectoryMultiple = 2

func init() {
	Register(databaseRule{relationRule{id: "standby.recovery_conflicts", severity: SeverityWarning, needs: []string{"recovery"}, eval: func(s *Snapshot) []Finding {
		const id = "standby.recovery_conflicts"
		rates := map[string]map[string]float64{}
		for key, rate := range s.Metrics["pg_recovery_conflicts_total"] {
			db := canonicalLabel(key, "database")
			if rate <= 0 || db == "" {
				continue
			}
			if rates[db] == nil {
				rates[db] = map[string]float64{}
			}
			rates[db][canonicalLabel(key, "reason")] = rate
		}
		var out []Finding
		for _, db := range slices.Sorted(maps.Keys(rates)) {
			reasons := slices.Sorted(maps.Keys(rates[db]))
			var counted, advice []string
			for _, reason := range reasons {
				counted = append(counted, fmt.Sprintf("%s %.1f/min", reason, rates[db][reason]*60))
				if a, ok := recoveryConflictAdvice[reason]; ok {
					advice = append(advice, a)
				}
			}
			out = append(out, Finding{RuleID: id, Severity: SeverityWarning, State: StateOpen, Scope: ScopeDatabase, Datname: db, ObjectName: "recovery_conflicts",
				Title:       fmt.Sprintf("Standby queries on %s are cancelled by WAL replay", db),
				Detail:      fmt.Sprintf("Since the previous scrape, replay on this standby cancelled queries of database %s that conflicted with the changes it applied (%s). Each one fails with \"canceling statement due to conflict with recovery\". %s", db, strings.Join(counted, ", "), strings.Join(advice, " ")),
				Remediation: "Raise max_standby_streaming_delay (and max_standby_archive_delay) on the standby to let queries finish before replay cancels them, at the cost of the standby falling behind while they run; or remove the cause on the primary, as the detail says per reason.",
				Evidence:    map[string]any{"conflicts_per_second": rates[db]},
			})
		}
		return out
	}}})
	Register(instanceObjectRule{relationRule{id: "standby.replay_paused", severity: SeverityWarning, needs: []string{"recovery"}, eval: func(s *Snapshot) []Finding {
		const id = "standby.replay_paused"
		if paused, ok := s.Metric("pg_recovery_replay_paused", nil); !ok || paused < 1 {
			return nil
		}
		return []Finding{{RuleID: id, Severity: SeverityWarning, State: StateOpen, Scope: ScopeInstance, ObjectName: "wal_replay",
			Title:       "WAL replay is paused on this standby",
			Detail:      "The standby receives WAL but does not apply it, so its queries see ever older data, and the WAL it has not replayed accumulates in its pg_wal. Replay pauses on pg_wal_replay_pause(), and when recovery reaches a target with recovery_target_action = 'pause'.",
			Remediation: "Once the reason for the pause is over, resume replay as a superuser: SELECT pg_wal_replay_resume();",
			Evidence:    map[string]any{"replay_paused": true},
		}}
	}}})
	Register(instanceObjectRule{relationRule{id: "wal.directory_large", severity: SeverityWarning, needs: []string{"wal_directory", "settings"}, eval: func(s *Snapshot) []Finding {
		const id = "wal.directory_large"
		size, ok := s.Metric("pg_wal_dir_bytes", nil)
		if !ok {
			return nil
		}
		maxWAL, ok := settingFloat(s, "max_wal_size_bytes")
		if !ok || maxWAL <= 0 {
			return nil
		}
		keep, _ := settingFloat(s, "wal_keep_size_bytes")
		limit := walDirectoryMultiple*maxWAL + keep
		if size <= limit {
			return nil
		}
		evidence := map[string]any{"wal_dir_bytes": size, "max_wal_size_bytes": maxWAL, "wal_keep_size_bytes": keep}
		files, _ := s.Metric("pg_wal_dir_files", nil)
		evidence["wal_dir_files"] = files
		archive := ""
		if ready, ok := metricValue(s, "pg_archiver_ready_files"); ok {
			evidence["archiver_ready_files"] = ready
			if ready > 0 {
				archive = fmt.Sprintf(" %.0f segments wait for archive_command, which keeps them until it succeeds.", ready)
			}
		}
		return []Finding{{RuleID: id, Severity: SeverityWarning, State: StateOpen, Scope: ScopeInstance, ObjectName: "pg_wal",
			Title: fmt.Sprintf("pg_wal holds %s, more than max_wal_size and wal_keep_size explain", formatCatalogBytes(int64(size))),
			Detail: fmt.Sprintf("pg_wal holds %s in %.0f files, more than twice max_wal_size (%s) plus wal_keep_size (%s). Something keeps WAL the checkpoints would remove: a replication slot whose consumer is behind or gone, a failing archive_command, or a standby that needs it.%s When the disk fills, the server stops.",
				formatCatalogBytes(int64(size)), files, formatCatalogBytes(int64(maxWAL)), formatCatalogBytes(int64(keep)), archive),
			Remediation: "Look at the WAL each replication slot retains (pg_replication_slots, the cluster's slots) and drop the slots no consumer uses with pg_drop_replication_slot(); fix archive_command if segments wait for it (pg_stat_archiver.last_failed_wal).",
			Evidence:    evidence,
		}}
	}}})
}
