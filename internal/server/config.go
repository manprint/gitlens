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
	c := AlertConfig{Interval: 30 * time.Second, SlackURL: getenv("PGLENS_SLACK_WEBHOOK_URL"), WebhookURL: getenv("PGLENS_WEBHOOK_URL")}
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
	if p := strings.TrimSpace(getenv("PGLENS_SLACK_WEBHOOK_URL_FILE")); p != "" {
		b, err := readFile(p)
		if err != nil {
			return AlertConfig{}, fmt.Errorf("read Slack webhook file: %w", err)
		}
		c.SlackURL = strings.TrimSpace(string(b))
	}
	return c, nil
}
