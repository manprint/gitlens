package server

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type AlertConfig struct {
	Interval   time.Duration
	SlackURL   string
	WebhookURL string
}

type UIConfig struct {
	Enabled      bool
	Password     string
	SessionTTL   time.Duration
	CookieSecure string // "auto" | "true" | "false"
}

func (c UIConfig) String() string {
	return fmt.Sprintf("UIConfig{Enabled:%t Password:[redacted] SessionTTL:%s CookieSecure:%s}", c.Enabled, c.SessionTTL, c.CookieSecure)
}

// LoadBootstrapToken resolves the agent bearer token from
// PGLENS_BOOTSTRAP_TOKEN or PGLENS_BOOTSTRAP_TOKEN_FILE (trailing whitespace
// trimmed, the inline value winning). It returns an error when no token is
// configured, or when a configured token file cannot be read or is empty.
//
// It used to silently fall back to the literal "dev-token" — including when a
// PGLENS_BOOTSTRAP_TOKEN_FILE was configured but unreadable (a wrong path, a
// secret not mounted yet), so a production server could come up happily
// accepting a token published in this repository's own quickstart with
// nothing in its logs saying so. README documents the token as required with
// no default; this makes the binary agree.
func LoadBootstrapToken(getenv func(string) string, readFile func(string) ([]byte, error)) (string, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	if readFile == nil {
		readFile = os.ReadFile
	}
	if token := strings.TrimRight(getenv("PGLENS_BOOTSTRAP_TOKEN"), "\r\n"); token != "" {
		return token, nil
	}
	path := strings.TrimSpace(getenv("PGLENS_BOOTSTRAP_TOKEN_FILE"))
	if path == "" {
		return "", fmt.Errorf("set PGLENS_BOOTSTRAP_TOKEN or PGLENS_BOOTSTRAP_TOKEN_FILE: the agent bearer token has no default")
	}
	data, err := readFile(path)
	if err != nil {
		return "", fmt.Errorf("read bootstrap token file %s: %w", path, err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("bootstrap token file %s is empty", path)
	}
	return token, nil
}

func LoadAlertConfig(getenv func(string) string, readFile func(string) ([]byte, error)) (AlertConfig, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	if readFile == nil {
		readFile = os.ReadFile
	}
	// PGLENS_ALERT_SLACK_WEBHOOK_URL is the descriptive deployment name. Keep
	// the original PGLENS_SLACK_WEBHOOK_URL as a backwards-compatible alias.
	slackURL := getenv("PGLENS_ALERT_SLACK_WEBHOOK_URL")
	if slackURL == "" {
		slackURL = getenv("PGLENS_SLACK_WEBHOOK_URL")
	}
	c := AlertConfig{Interval: 30 * time.Second, SlackURL: slackURL, WebhookURL: getenv("PGLENS_WEBHOOK_URL")}
	if raw := strings.TrimSpace(getenv("PGLENS_ALERT_INTERVAL")); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			if n, e := strconv.Atoi(raw); e == nil {
				d = time.Duration(n) * time.Second
			} else {
				return AlertConfig{}, fmt.Errorf("invalid PGLENS_ALERT_INTERVAL %q: %w", raw, err)
			}
		}
		if d <= 0 {
			return AlertConfig{}, fmt.Errorf("invalid PGLENS_ALERT_INTERVAL %q: must be positive", raw)
		}
		c.Interval = d
	}
	slackFile := strings.TrimSpace(getenv("PGLENS_ALERT_SLACK_WEBHOOK_URL_FILE"))
	if slackFile == "" {
		slackFile = strings.TrimSpace(getenv("PGLENS_SLACK_WEBHOOK_URL_FILE"))
	}
	if p := slackFile; p != "" {
		b, err := readFile(p)
		if err != nil {
			return AlertConfig{}, fmt.Errorf("read Slack webhook file: %w", err)
		}
		c.SlackURL = strings.TrimSpace(string(b))
	}
	return c, nil
}

func LoadUIConfig(getenv func(string) string, readFile func(string) ([]byte, error)) (UIConfig, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	if readFile == nil {
		readFile = os.ReadFile
	}

	config := UIConfig{
		Enabled:      true,
		SessionTTL:   24 * time.Hour,
		CookieSecure: "auto",
	}
	if raw := strings.TrimSpace(getenv("PGLENS_UI_ENABLED")); raw != "" {
		enabled, err := strconv.ParseBool(raw)
		if err != nil {
			return UIConfig{}, fmt.Errorf("invalid PGLENS_UI_ENABLED %q: %w", raw, err)
		}
		config.Enabled = enabled
	}

	passwordFile := strings.TrimSpace(getenv("PGLENS_UI_PASSWORD_FILE"))
	if passwordFile != "" {
		password, err := readFile(passwordFile)
		if err != nil {
			return UIConfig{}, fmt.Errorf("read UI password file: %w", err)
		}
		config.Password = strings.TrimSpace(string(password))
	} else {
		config.Password = getenv("PGLENS_UI_PASSWORD")
	}

	if raw := strings.TrimSpace(getenv("PGLENS_UI_SESSION_TTL")); raw != "" {
		ttl, err := time.ParseDuration(raw)
		if err != nil {
			return UIConfig{}, fmt.Errorf("invalid PGLENS_UI_SESSION_TTL %q: %w", raw, err)
		}
		if ttl < 5*time.Minute || ttl > 720*time.Hour {
			return UIConfig{}, fmt.Errorf("invalid PGLENS_UI_SESSION_TTL %q: accepted range is 5m to 720h", raw)
		}
		config.SessionTTL = ttl
	}

	if raw := strings.TrimSpace(getenv("PGLENS_UI_COOKIE_SECURE")); raw != "" {
		secure := strings.ToLower(raw)
		if secure != "auto" && secure != "true" && secure != "false" {
			return UIConfig{}, fmt.Errorf("invalid PGLENS_UI_COOKIE_SECURE %q: accepted values are auto, true, false", raw)
		}
		config.CookieSecure = secure
	}

	return config, nil
}
