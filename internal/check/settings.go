package check

import (
	"context"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
)

var settingsAllowlist = []string{
	"archive_command", "archive_mode", "archive_timeout", "autovacuum",
	"autovacuum_analyze_scale_factor", "autovacuum_max_workers",
	"autovacuum_naptime", "autovacuum_vacuum_cost_delay",
	"autovacuum_vacuum_cost_limit", "autovacuum_vacuum_scale_factor",
	"autovacuum_work_mem", "checkpoint_completion_target", "checkpoint_timeout",
	"default_statistics_target", "effective_cache_size", "effective_io_concurrency",
	"fsync", "full_page_writes", "hot_standby", "hot_standby_feedback", "huge_pages",
	"maintenance_work_mem", "max_connections", "max_parallel_workers",
	"max_parallel_workers_per_gather", "max_replication_slots",
	"max_standby_streaming_delay", "max_wal_senders", "max_wal_size",
	"max_worker_processes", "min_wal_size", "random_page_cost", "shared_buffers",
	"statement_timeout", "synchronous_commit", "synchronous_standby_names",
	"temp_buffers", "track_io_timing", "wal_buffers", "wal_compression",
	"wal_keep_size", "wal_level", "work_mem",
}

func init() { Register(&settingsCheck{}) }

type settingsCheck struct{}

func (c *settingsCheck) Name() string { return "settings" }
func (c *settingsCheck) Requires() Requirements {
	return Requirements{Scope: ScopeInstance, PermTier: pgtype.TierReadOnly}
}
func (c *settingsCheck) DefaultInterval() time.Duration { return time.Hour }
func (c *settingsCheck) Timeout() time.Duration         { return 10 * time.Second }

type settingRow struct {
	name, setting, unit, source, bootVal, resetVal *string
	pendingRestart                                 bool
	vartype, context, shortDesc                    *string
}

type settingText struct {
	name, setting, unit, source, bootVal, resetVal string
	pendingRestart                                 bool
	vartype, context, shortDesc                    string
}

func settingTextValue(raw settingRow) settingText {
	value := func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	}
	return settingText{
		name: value(raw.name), setting: value(raw.setting), unit: value(raw.unit),
		source: value(raw.source), bootVal: value(raw.bootVal), resetVal: value(raw.resetVal),
		pendingRestart: raw.pendingRestart, vartype: value(raw.vartype), context: value(raw.context),
		shortDesc: value(raw.shortDesc),
	}
}

func (c *settingsCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.Conn(ctx)
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()
	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}
	rows, err := conn.Query(ctx, `
SELECT name, setting, unit, source, boot_val, reset_val, pending_restart,
       vartype, context, short_desc
FROM pg_settings`)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()

	allowlisted := make(map[string]struct{}, len(settingsAllowlist))
	for _, name := range settingsAllowlist {
		allowlisted[name] = struct{}{}
	}
	var result Result
	var pending float64
	for rows.Next() {
		var raw settingRow
		if err := rows.Scan(&raw.name, &raw.setting, &raw.unit, &raw.source, &raw.bootVal, &raw.resetVal,
			&raw.pendingRestart, &raw.vartype, &raw.context, &raw.shortDesc); err != nil {
			return Result{}, err
		}
		s := settingTextValue(raw)
		if s.pendingRestart {
			pending++
		}
		_, curated := allowlisted[s.name]
		if !curated && s.setting == s.bootVal {
			continue
		}
		valueText := s.setting
		if s.name == "archive_command" {
			valueText = redactArchiveCommand(s.setting)
		}
		result.Facts = append(result.Facts, Fact{
			Kind: "setting", Key: s.name, ValueText: valueText,
			Labels: map[string]string{
				"unit": s.unit, "source": s.source, "vartype": s.vartype,
				"context": s.context, "pending_restart": strconv.FormatBool(s.pendingRestart),
			},
		})
		if value, ok := normalizeSetting(s.setting, s.unit); ok {
			metricName := ""
			switch settingUnitKind(s.unit) {
			case "bytes":
				metricName = "pg_setting_bytes"
			case "seconds":
				metricName = "pg_setting_seconds"
			}
			if metricName != "" {
				result.Metrics = append(result.Metrics, pgtype.Metric{
					Name: metricName, Value: value, Kind: pgtype.KindGauge,
					Labels: map[string]string{"name": s.name},
				})
			}
		}
	}
	if err := rows.Err(); err != nil {
		return Result{}, err
	}
	result.Metrics = append(result.Metrics, pgtype.Metric{
		Name: "pg_settings_pending_restart", Value: pending, Kind: pgtype.KindGauge,
	})
	return result, nil
}

func settingUnitKind(unit string) string {
	switch unit {
	case "8kB", "kB", "MB", "GB":
		return "bytes"
	case "ms", "s", "min":
		return "seconds"
	default:
		return ""
	}
}

func normalizeSetting(setting, unit string) (float64, bool) {
	value, err := strconv.ParseFloat(strings.TrimSpace(setting), 64)
	if err != nil {
		return 0, false
	}
	switch unit {
	case "8kB":
		return value * 8 * 1024, true
	case "kB":
		return value * 1024, true
	case "MB":
		return value * 1024 * 1024, true
	case "GB":
		return value * 1024 * 1024 * 1024, true
	case "ms":
		return value / 1000, true
	case "s":
		return value, true
	case "min":
		return value * 60, true
	default:
		if unit != "" {
			log.Printf("settings: skipping unknown unit %q for setting %q", unit, setting)
		}
		return 0, false
	}
}

func redactArchiveCommand(command string) string {
	parts := strings.Fields(command)
	if len(parts) <= 1 {
		return command
	}
	return parts[0] + " [redacted]"
}
