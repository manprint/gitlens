package agent

import (
	"fmt"
	"os"
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

	// Parsed versions for convenience
	parsedPushInterval time.Duration
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
	Name        string          `yaml:"name"`
	DSN         string          `yaml:"dsn"`
	ClusterName string          `yaml:"cluster_name"`
	Databases   DatabasesConfig `yaml:"databases"`
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
	Interval string `yaml:"interval"`
	TopN     int    `yaml:"top_n"` // for stat_statements
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
