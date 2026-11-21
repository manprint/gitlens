package server

import (
	"fmt"
	"net/mail"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/manprint/pglens/internal/alert/notify"
)

type AlertConfig struct {
	Interval   time.Duration
	SlackURL   string
	WebhookURL string
	// GoogleChatURL is a Google Chat incoming-webhook URL; it carries its key
	// and token in the query string, so it is a secret like the Slack URL.
	GoogleChatURL string
	// TeamsURL is a Microsoft Teams workflow URL; its sig parameter signs
	// the request, so it is a secret like the Slack URL.
	TeamsURL string
	// PagerDutyRoutingKey is the Events API v2 key of a PagerDuty service.
	PagerDutyRoutingKey string
	// SMTP is the mail server of the email channel; an empty Host disables it.
	SMTP SMTPConfig
}

// SMTPConfig is the mail server of the email channel (PGLENS_SMTP_*).
type SMTPConfig = notify.SMTPSettings

type UIConfig struct {
	Enabled      bool
	Password     string
	SessionTTL   time.Duration
	CookieSecure string // "auto" | "true" | "false"
	// TimeZone (PGLENS_TZ, IANA) and Language (PGLENS_LANG) are the console
	// defaults the session endpoint hands the browser; empty reads as UTC and en.
	TimeZone string
	Language string
}

func (c UIConfig) String() string {
	return fmt.Sprintf("UIConfig{Enabled:%t Password:[redacted] SessionTTL:%s CookieSecure:%s TimeZone:%s Language:%s}", c.Enabled, c.SessionTTL, c.CookieSecure, c.TimeZone, c.Language)
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
	c.GoogleChatURL = strings.TrimSpace(getenv("PGLENS_GOOGLE_CHAT_WEBHOOK_URL"))
	if p := strings.TrimSpace(getenv("PGLENS_GOOGLE_CHAT_WEBHOOK_URL_FILE")); p != "" {
		b, err := readFile(p)
		if err != nil {
			return AlertConfig{}, fmt.Errorf("read Google Chat webhook file: %w", err)
		}
		c.GoogleChatURL = strings.TrimSpace(string(b))
	}
	c.TeamsURL = strings.TrimSpace(getenv("PGLENS_TEAMS_WEBHOOK_URL"))
	if p := strings.TrimSpace(getenv("PGLENS_TEAMS_WEBHOOK_URL_FILE")); p != "" {
		b, err := readFile(p)
		if err != nil {
			return AlertConfig{}, fmt.Errorf("read Teams webhook file: %w", err)
		}
		c.TeamsURL = strings.TrimSpace(string(b))
	}
	c.PagerDutyRoutingKey = strings.TrimSpace(getenv("PGLENS_PAGERDUTY_ROUTING_KEY"))
	if p := strings.TrimSpace(getenv("PGLENS_PAGERDUTY_ROUTING_KEY_FILE")); p != "" {
		b, err := readFile(p)
		if err != nil {
			return AlertConfig{}, fmt.Errorf("read PagerDuty routing key file: %w", err)
		}
		c.PagerDutyRoutingKey = strings.TrimSpace(string(b))
	}
	if c.PagerDutyRoutingKey != "" {
		if err := notify.ValidatePagerDutyKey(c.PagerDutyRoutingKey); err != nil {
			return AlertConfig{}, fmt.Errorf("invalid PGLENS_PAGERDUTY_ROUTING_KEY: %w", err)
		}
	}
	smtpCfg, err := LoadSMTPConfig(getenv, readFile)
	if err != nil {
		return AlertConfig{}, err
	}
	c.SMTP = smtpCfg
	return c, nil
}

// LoadSMTPConfig reads the mail server of the email channel. Without
// PGLENS_SMTP_HOST the channel is off and nothing else is read. Its errors
// never quote the password.
func LoadSMTPConfig(getenv func(string) string, readFile func(string) ([]byte, error)) (SMTPConfig, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	if readFile == nil {
		readFile = os.ReadFile
	}
	c := SMTPConfig{Host: strings.TrimSpace(getenv("PGLENS_SMTP_HOST"))}
	if c.Host == "" {
		return SMTPConfig{}, nil
	}
	c.Port = 587
	if raw := strings.TrimSpace(getenv("PGLENS_SMTP_PORT")); raw != "" {
		port, err := strconv.Atoi(raw)
		if err != nil || port < 1 || port > 65535 {
			return SMTPConfig{}, fmt.Errorf("invalid PGLENS_SMTP_PORT %q: must be 1 to 65535", raw)
		}
		c.Port = port
	}
	c.Username = strings.TrimSpace(getenv("PGLENS_SMTP_USERNAME"))
	c.Password = strings.TrimRight(getenv("PGLENS_SMTP_PASSWORD"), "\r\n")
	if p := strings.TrimSpace(getenv("PGLENS_SMTP_PASSWORD_FILE")); p != "" {
		b, err := readFile(p)
		if err != nil {
			return SMTPConfig{}, fmt.Errorf("read SMTP password file: %w", err)
		}
		c.Password = strings.TrimSpace(string(b))
	}
	if (c.Username == "") != (c.Password == "") {
		return SMTPConfig{}, fmt.Errorf("PGLENS_SMTP_USERNAME and PGLENS_SMTP_PASSWORD must be set together")
	}
	c.From = strings.TrimSpace(getenv("PGLENS_SMTP_FROM"))
	if c.From == "" {
		return SMTPConfig{}, fmt.Errorf("PGLENS_SMTP_FROM is required when PGLENS_SMTP_HOST is set")
	}
	if _, err := mail.ParseAddress(c.From); err != nil {
		return SMTPConfig{}, fmt.Errorf("invalid PGLENS_SMTP_FROM: one address is required")
	}
	c.TLS = strings.ToLower(strings.TrimSpace(getenv("PGLENS_SMTP_TLS")))
	switch c.TLS {
	case "":
		c.TLS = notify.SMTPStartTLS
	case notify.SMTPStartTLS, notify.SMTPTLS, notify.SMTPNoTLS:
	default:
		return SMTPConfig{}, fmt.Errorf("invalid PGLENS_SMTP_TLS %q: must be starttls, tls or none", c.TLS)
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
