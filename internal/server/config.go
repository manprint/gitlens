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
