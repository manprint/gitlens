package check

import (
	"context"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
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

// connectionSource reports whether a pg_settings source is specific to the
// connection that reads it.
func connectionSource(source string) bool {
	switch source {
	case "client", "session", "user":
		return true
	}
	return false
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
		// A value from the client, the session or a setting of the role
		// itself describes pglens's own connection, not the instance: the
		// agent sets statement_timeout, search_path and
		// default_transaction_read_only for every session, and the role
		// script sets them for the role. They used to be reported as the
		// instance's settings; what the instance uses for its other roles
		// cannot be read from here.
		if connectionSource(s.source) {
			continue
		}
		valueText := redactSetting(s.name, s.setting)
		result.Facts = append(result.Facts, Fact{
			Kind: "setting", Key: s.name, ValueText: valueText,
			Labels: map[string]string{
				"unit": s.unit, "source": s.source, "vartype": s.vartype,
				"context": s.context, "pending_restart": strconv.FormatBool(s.pendingRestart),
			},
		})
		// -1 (and other negatives) means "disabled" or "use another
		// setting", never a size or a duration.
		if value, ok := normalizeSetting(s.setting, s.unit); ok && value >= 0 {
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
	case "B", "8kB", "kB", "MB", "GB", "TB":
		return "bytes"
	case "us", "ms", "s", "min", "h", "d":
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
	case "B":
		return value, true
	case "8kB":
		return value * 8 * 1024, true
	case "kB":
		return value * 1024, true
	case "MB":
		return value * 1024 * 1024, true
	case "GB":
		return value * 1024 * 1024 * 1024, true
	case "TB":
		return value * 1024 * 1024 * 1024 * 1024, true
	case "us":
		return value / 1e6, true
	case "ms":
		return value / 1000, true
	case "s":
		return value, true
	case "min":
		return value * 60, true
	case "h":
		return value * 3600, true
	case "d":
		return value * 86400, true
	default:
		if unit != "" {
			if _, logged := unknownUnitsLogged.LoadOrStore(unit, true); !logged {
				log.Printf("settings: skipping settings with unknown unit %q (first value seen: %q)", unit, setting)
			}
		}
		return 0, false
	}
}

// unknownUnitsLogged keeps an unsupported unit from being logged on every
// hourly scrape for the lifetime of the agent.
var unknownUnitsLogged sync.Map

// redactSetting strips credentials from the settings that routinely carry
// them before the value leaves the monitored host. Every non-default setting
// is shipped, so this covers the whole family rather than an allowlist.
func redactSetting(name, value string) string {
	switch {
	case strings.HasSuffix(name, "_command"):
		// archive_command, restore_command, archive_cleanup_command,
		// recovery_end_command, ssl_passphrase_command
		return redactCommand(value)
	case strings.HasSuffix(name, "conninfo"):
		// primary_conninfo
		return redactConninfo(value)
	}
	return value
}

// redactCommand keeps only the program a shell command runs. Its arguments,
// and VAR=value assignments in front of it, are where object-store keys and
// passphrases live.
func redactCommand(command string) string {
	all := strings.Fields(command)
	fields := all
	for len(fields) > 0 && strings.Contains(fields[0], "=") {
		fields = fields[1:]
	}
	switch {
	case len(all) == 0:
		return command
	case len(fields) == 0:
		return "[redacted]"
	case len(all) == 1:
		return command
	default:
		return fields[0] + " [redacted]"
	}
}

var (
	conninfoPassword    = regexp.MustCompile(`(?i)\b((?:ssl)?password\s*=\s*)('(?:[^'\\]|\\.)*'|\S+)`)
	conninfoURIPassword = regexp.MustCompile(`(?i)(postgres(?:ql)?://[^:/@\s]*):[^@\s]*@`)
)

// redactConninfo hides the password of a libpq connection string, in both
// the keyword=value and the URI form.
func redactConninfo(conninfo string) string {
	conninfo = conninfoPassword.ReplaceAllString(conninfo, "${1}[redacted]")
	return conninfoURIPassword.ReplaceAllString(conninfo, "${1}:[redacted]@")
}
