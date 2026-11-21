package agent

import (
	"fmt"
	"path/filepath"
	"time"
)

// Log sources accepted by checks.logs.source.
const (
	LogSourceFile = "file"
	LogSourceSQL  = "sql"
)

// checks.logs.max_bytes_per_interval bounds and default (I-9).
const (
	DefaultLogBytesPerInterval int64 = 1 << 20
	MinLogBytesPerInterval     int64 = 64 << 10
	MaxLogBytesPerInterval     int64 = 16 << 20
)

// LogsCheck is the configuration key of the log reader.
const LogsCheck = "logs"

// DefaultLogInterval is the log reader's interval when checks.logs.interval
// is unset.
const DefaultLogInterval = 15 * time.Second

// LogInterval resolves checks.logs.interval.
func (c *Config) LogInterval() time.Duration {
	if d := c.GetCheckInterval(LogsCheck); d > 0 {
		return d
	}
	return DefaultLogInterval
}

// LogsEnabled resolves checks.logs.enabled: off unless explicitly true.
func (c CheckConfig) LogsEnabled() bool { return c.Enabled != nil && *c.Enabled }

// LogSource resolves checks.logs.source; file by default.
func (c CheckConfig) LogSource() string {
	if c.Source == "" {
		return LogSourceFile
	}
	return c.Source
}

// LogStatementText resolves checks.logs.statement_text: off unless true.
func (c CheckConfig) LogStatementText() bool { return c.StatementText != nil && *c.StatementText }

// LogPlanParameters resolves checks.logs.plan_parameters: off unless true.
func (c CheckConfig) LogPlanParameters() bool { return c.PlanParameters != nil && *c.PlanParameters }

// LogBudget resolves checks.logs.max_bytes_per_interval.
func (c CheckConfig) LogBudget() int64 {
	if c.MaxBytesPerInterval == 0 {
		return DefaultLogBytesPerInterval
	}
	return c.MaxBytesPerInterval
}

// LogStatePath resolves checks.logs.state_path: by default pglog-state.json
// beside the identity file.
func (c *Config) LogStatePath() string {
	if p := c.Checks[LogsCheck].StatePath; p != "" {
		return p
	}
	return filepath.Join(filepath.Dir(c.IdentityPath), "pglog-state.json")
}

// hasLogKeys reports whether a check sets a key only checks.logs takes.
func (c CheckConfig) hasLogKeys() bool {
	return c.Source != "" || c.Path != "" || c.StatementText != nil || c.PlanParameters != nil ||
		c.MaxBytesPerInterval != 0 || c.StatePath != ""
}

// validateLogKeys checks the checks.logs keys and refuses them elsewhere.
func validateLogKeys(name string, c CheckConfig) error {
	if name != LogsCheck {
		if c.hasLogKeys() {
			return fmt.Errorf("validation: checks.%s: source, path, statement_text, plan_parameters, max_bytes_per_interval and state_path belong to checks.logs", name)
		}
		return nil
	}
	switch c.Source {
	case "", LogSourceFile, LogSourceSQL:
	default:
		return fmt.Errorf("validation: checks.logs.source must be %q or %q, not %q", LogSourceFile, LogSourceSQL, c.Source)
	}
	if b := c.MaxBytesPerInterval; b != 0 && (b < MinLogBytesPerInterval || b > MaxLogBytesPerInterval) {
		return fmt.Errorf("validation: checks.logs.max_bytes_per_interval must be between %d and %d", MinLogBytesPerInterval, MaxLogBytesPerInterval)
	}
	return nil
}
