package agent

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level agent configuration.
type Config struct {
	Server       ServerConfig   `yaml:"server"`
	IdentityPath string         `yaml:"identity_path"`
	PushInterval string         `yaml:"push_interval"` // string to parse duration
	Buffer       BufferConfig   `yaml:"buffer"`
	Targets      []TargetConfig `yaml:"targets"`
	Checks       ChecksConfig   `yaml:"checks"`
	Commands     CommandsConfig `yaml:"commands"`
	Host         HostConfig     `yaml:"host"`

	// Parsed versions for convenience
	parsedPushInterval   time.Duration
	parsedBufferMaxSize  int64
	parsedBufferMaxAge   time.Duration
	parsedCheckIntervals map[string]time.Duration
	parsedHostInterval   time.Duration
}

// ServerConfig configures the server connection.
type ServerConfig struct {
	URL       string `yaml:"url"`
	Token     string `yaml:"token"`
	TokenFile string `yaml:"token_file"`
}

// BufferConfig configures the disk buffer.
type BufferConfig struct {
	Path    string `yaml:"path"`
	MaxSize string `yaml:"max_size"` // "512MiB" etc
	MaxAge  string `yaml:"max_age"`  // "6h" etc
}

// TargetConfig is a single PostgreSQL target to monitor.
type TargetConfig struct {
	Name                string          `yaml:"name"`
	DSN                 string          `yaml:"dsn"`
	ClusterName         string          `yaml:"cluster_name"`
	Databases           DatabasesConfig `yaml:"databases"`
	HostLocal           *bool           `yaml:"host_local"`
	AllowExplainAnalyze bool            `yaml:"allow_explain_analyze"`
	AllowSignal         bool            `yaml:"allow_signal"`
}

// CommandsConfig controls the agent-side command channel. Enabled defaults to
// true; setting it explicitly to false makes the agent strictly read-only by
// stopping command polling entirely.
type CommandsConfig struct {
	Enabled *bool `yaml:"enabled"`
}

type HostConfig struct {
	Enabled  *bool  `yaml:"enabled"`
	Interval string `yaml:"interval"`
	ProcPath string `yaml:"proc_path"`
	SysPath  string `yaml:"sys_path"`
}

func (c *Config) HostEnabled() bool              { return c.Host.Enabled == nil || *c.Host.Enabled }
func (c *Config) CommandsEnabled() bool          { return c.Commands.Enabled == nil || *c.Commands.Enabled }
func (c *Config) GetHostInterval() time.Duration { return c.parsedHostInterval }
func (c *Config) HostProcPath() string {
	if c.Host.ProcPath != "" {
		return c.Host.ProcPath
	}
	if p := os.Getenv("HOST_PROC"); p != "" {
		return p
	}
	return "/proc"
}
func (c *Config) HostSysPath() string {
	if c.Host.SysPath != "" {
		return c.Host.SysPath
	}
	if p := os.Getenv("HOST_SYS"); p != "" {
		return p
	}
	return "/sys"
}
func TargetIsLocal(t TargetConfig) bool {
	if t.HostLocal != nil {
		return *t.HostLocal
	}
	u, err := url.Parse(t.DSN)
	if err != nil {
		return false
	}
	h := u.Hostname()
	if h == "" {
		h = u.Query().Get("host")
	}
	if strings.HasPrefix(h, "/") {
		return true
	}
	switch strings.ToLower(h) {
	case "", "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

// DatabasesConfig configures which databases to monitor.
type DatabasesConfig struct {
	Include []string `yaml:"include"`
	Exclude []string `yaml:"exclude"`
	Max     int      `yaml:"max"`
}

// ChecksConfig configures per-check overrides.
type ChecksConfig map[string]CheckConfig

// CheckConfig is a single check's configuration.
type CheckConfig struct {
	Interval      string `yaml:"interval"`
	TopN          int    `yaml:"top_n"`          // for stat_statements
	ByApplication bool   `yaml:"by_application"` // for activity
	// Enabled is nil (default enabled) unless explicitly set; only "ash"
	// consults this today (SYS-ASH-002: disabling it must produce no
	// metrics_ash rows, no error, and an explicit "enabled": false from the
	// API — never an empty result indistinguishable from an idle database).
	Enabled *bool `yaml:"enabled"`
}

// LoadConfig loads the configuration from a YAML file, applying environment overrides.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &Config{}
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true) // strict: unknown fields are errors
	if err := decoder.Decode(cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Apply environment overrides
	if url := os.Getenv("PGLENS_SERVER_URL"); url != "" {
		cfg.Server.URL = url
	}
	if token := os.Getenv("PGLENS_BOOTSTRAP_TOKEN"); token != "" {
		cfg.Server.Token = token
	}
	if tokenFile := os.Getenv("PGLENS_BOOTSTRAP_TOKEN_FILE"); tokenFile != "" {
		cfg.Server.TokenFile = tokenFile
	}

	// Read token from file if specified
	if cfg.Server.TokenFile != "" && cfg.Server.Token == "" {
		tokenData, err := os.ReadFile(cfg.Server.TokenFile)
		if err != nil {
			return nil, fmt.Errorf("read token file: %w", err)
		}
		cfg.Server.Token = strings.TrimSpace(string(tokenData))
	}

	// Validate and set defaults
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// validate performs strict validation on the configuration.
func (c *Config) validate() error {
	if c.Server.URL == "" {
		return fmt.Errorf("validation: server.url is required")
	}
	if c.Server.Token == "" && c.Server.TokenFile == "" {
		return fmt.Errorf("validation: server.token or server.token_file is required")
	}
	if c.IdentityPath == "" {
		return fmt.Errorf("validation: identity_path is required")
	}

	// Parse and validate push_interval
	if c.PushInterval == "" {
		c.parsedPushInterval = 15 * time.Second
	} else {
		d, err := time.ParseDuration(c.PushInterval)
		if err != nil {
			return fmt.Errorf("validation: push_interval: %w", err)
		}
		c.parsedPushInterval = d
	}

	// Validate buffer config
	if c.Buffer.Path == "" {
		return fmt.Errorf("validation: buffer.path is required")
	}
	c.parsedBufferMaxSize = 512 * 1024 * 1024
	if c.Buffer.MaxSize != "" {
		size, err := parseByteSize(c.Buffer.MaxSize)
		if err != nil {
			return fmt.Errorf("validation: buffer.max_size: %w", err)
		}
		c.parsedBufferMaxSize = size
	}
	c.parsedBufferMaxAge = 6 * time.Hour
	if c.Buffer.MaxAge != "" {
		age, err := time.ParseDuration(c.Buffer.MaxAge)
		if err != nil || age <= 0 {
			if err == nil {
				err = fmt.Errorf("must be greater than zero")
			}
			return fmt.Errorf("validation: buffer.max_age: %w", err)
		}
		c.parsedBufferMaxAge = age
	}
	c.parsedCheckIntervals = make(map[string]time.Duration, len(c.Checks))
	c.parsedHostInterval = 30 * time.Second
	if c.Host.Interval != "" {
		d, err := time.ParseDuration(c.Host.Interval)
		if err != nil || d <= 0 {
			if err == nil {
				err = fmt.Errorf("must be greater than zero")
			}
			return fmt.Errorf("validation: host.interval: %w", err)
		}
		c.parsedHostInterval = d
	}
	if c.Host.ProcPath == "" {
		c.Host.ProcPath = c.HostProcPath()
	}
	if c.Host.SysPath == "" {
		c.Host.SysPath = c.HostSysPath()
	}
	for name, check := range c.Checks {
		if check.Interval != "" {
			interval, err := time.ParseDuration(check.Interval)
			if err != nil || interval <= 0 {
				if err == nil {
					err = fmt.Errorf("must be greater than zero")
				}
				return fmt.Errorf("validation: checks.%s.interval: %w", name, err)
			}
			c.parsedCheckIntervals[name] = interval
		}
		if check.TopN < 0 {
			return fmt.Errorf("validation: checks.%s.top_n must be >= 0", name)
		}
	}

	// Validate targets
	if len(c.Targets) == 0 {
		return fmt.Errorf("validation: targets is required and must not be empty")
	}
	for i, t := range c.Targets {
		if t.Name == "" {
			return fmt.Errorf("validation: targets[%d].name is required", i)
		}
		if t.DSN == "" {
			return fmt.Errorf("validation: targets[%d].dsn is required", i)
		}
		// Try to parse DSN to catch obvious issues
		if !strings.HasPrefix(t.DSN, "postgres://") && !strings.HasPrefix(t.DSN, "postgresql://") {
			return fmt.Errorf("validation: targets[%d].dsn must start with postgres:// or postgresql://", i)
		}
		// Validate Max databases
		if t.Databases.Max < 0 {
			return fmt.Errorf("validation: targets[%d].databases.max must be >= 0", i)
		}
	}

	return nil
}

// GetParsedPushInterval returns the parsed push interval duration.
func (c *Config) GetParsedPushInterval() time.Duration {
	return c.parsedPushInterval
}

// GetParsedBufferMaxSize returns the validated buffer size limit.
func (c *Config) GetParsedBufferMaxSize() int64 { return c.parsedBufferMaxSize }

// GetParsedBufferMaxAge returns the validated buffer age limit.
func (c *Config) GetParsedBufferMaxAge() time.Duration { return c.parsedBufferMaxAge }

// GetCheckInterval returns a validated per-check interval, or zero when the
// check uses its implementation default.
func (c *Config) GetCheckInterval(name string) time.Duration {
	return c.parsedCheckIntervals[name]
}

func parseByteSize(raw string) (int64, error) {
	s := strings.TrimSpace(raw)
	units := []struct {
		suffix string
		factor int64
	}{
		{"GIB", 1 << 30}, {"MIB", 1 << 20}, {"KIB", 1 << 10},
		{"GB", 1e9}, {"MB", 1e6}, {"KB", 1e3}, {"B", 1},
	}
	upper := strings.ToUpper(s)
	factor := int64(1)
	number := upper
	for _, unit := range units {
		if strings.HasSuffix(upper, unit.suffix) {
			factor = unit.factor
			number = strings.TrimSpace(upper[:len(upper)-len(unit.suffix)])
			break
		}
	}
	n, err := strconv.ParseInt(number, 10, 64)
	if err != nil || n <= 0 {
		if err == nil {
			err = fmt.Errorf("must be greater than zero")
		}
		return 0, fmt.Errorf("invalid size %q: %w", raw, err)
	}
	maxInt64 := int64(^uint64(0) >> 1)
	if n > maxInt64/factor {
		return 0, fmt.Errorf("size %q overflows int64", raw)
	}
	return n * factor, nil
}

// GetToken returns the server token.
func (c *Config) GetToken() string {
	return c.Server.Token
}

// String returns a string representation of the config without exposing the DSN or token.
// This is safe for logging.
func (c *Config) String() string {
	var sb strings.Builder
	sb.WriteString("Config{")
	fmt.Fprintf(&sb, "Server.URL=%q ", c.Server.URL)
	fmt.Fprintf(&sb, "IdentityPath=%q ", c.IdentityPath)
	sb.WriteString("Targets=[")
	for i, t := range c.Targets {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "(name=%q)", t.Name)
	}
	sb.WriteString("]}")
	return sb.String()
}
