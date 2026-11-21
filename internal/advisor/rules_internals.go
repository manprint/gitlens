package advisor

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/manprint/pglens/internal/i18n"
)

// recoveryConflictReasons are the reasons of pg_stat_database_conflicts that
// the standby.recovery_conflicts message explains, in its order.
var recoveryConflictReasons = []string{"active_logicalslot", "bufferpin", "deadlock", "lock", "snapshot", "tablespace"}

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
			var counted []string
			for _, reason := range reasons {
				counted = append(counted, fmt.Sprintf("%s %.1f/min", reason, rates[db][reason]*60))
			}
			args := i18n.Args{"database": db, "counted": strings.Join(counted, ", ")}
			hasAdvice := false
			for _, reason := range recoveryConflictReasons {
				advised := slices.Contains(reasons, reason)
				args["adv_"+reason] = advised
				hasAdvice = hasAdvice || advised
			}
			args["has_advice"] = hasAdvice
			out = append(out, Finding{RuleID: id, Severity: SeverityWarning, State: StateOpen, Scope: ScopeDatabase, Datname: db, ObjectName: "recovery_conflicts",
				Evidence: map[string]any{"conflicts_per_second": rates[db]},
			}.say(id, args))
		}
		return out
	}}})
	Register(instanceObjectRule{relationRule{id: "standby.replay_paused", severity: SeverityWarning, needs: []string{"recovery"}, eval: func(s *Snapshot) []Finding {
		const id = "standby.replay_paused"
		if paused, ok := s.Metric("pg_recovery_replay_paused", nil); !ok || paused < 1 {
			return nil
		}
		return []Finding{Finding{RuleID: id, Severity: SeverityWarning, State: StateOpen, Scope: ScopeInstance, ObjectName: "wal_replay",
			Evidence: map[string]any{"replay_paused": true},
		}.say(id, nil)}
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
		args := i18n.Args{"size": formatCatalogBytes(int64(size)), "files": fmt.Sprintf("%.0f", files),
			"max_wal": formatCatalogBytes(int64(maxWAL)), "keep": formatCatalogBytes(int64(keep)),
			"has_archive": false, "archive_ready": ""}
		if ready, ok := metricValue(s, "pg_archiver_ready_files"); ok {
			evidence["archiver_ready_files"] = ready
			if ready > 0 {
				args["has_archive"] = true
				args["archive_ready"] = fmt.Sprintf("%.0f", ready)
			}
		}
		return []Finding{Finding{RuleID: id, Severity: SeverityWarning, State: StateOpen, Scope: ScopeInstance, ObjectName: "pg_wal",
			Evidence: evidence,
		}.say(id, args)}
	}}})
}
