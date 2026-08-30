package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConfig_AlertIntervalDefault(t *testing.T) {
	c, err := LoadAlertConfig(func(string) string { return "" }, nil)
	require.NoError(t, err)
	require.Equal(t, 30*time.Second, c.Interval)
}
func TestConfig_WebhookFileTakesPrecedence(t *testing.T) {
	c, err := LoadAlertConfig(func(k string) string {
		if k == "PGLENS_SLACK_WEBHOOK_URL" {
			return "inline"
		}
		if k == "PGLENS_SLACK_WEBHOOK_URL_FILE" {
			return "secret"
		}
		return ""
	}, func(string) ([]byte, error) { return []byte("from-file\n"), nil })
	require.NoError(t, err)
	require.Equal(t, "from-file", c.SlackURL)
}

func TestConfig_AlertSlackAlias(t *testing.T) {
	c, err := LoadAlertConfig(func(k string) string {
		if k == "PGLENS_ALERT_SLACK_WEBHOOK_URL" {
			return "alert-inline"
		}
		if k == "PGLENS_SLACK_WEBHOOK_URL" {
			return "legacy-inline"
		}
		return ""
	}, nil)
	require.NoError(t, err)
	require.Equal(t, "alert-inline", c.SlackURL)
}

func TestConfig_AlertSlackFileAlias(t *testing.T) {
	c, err := LoadAlertConfig(func(k string) string {
		if k == "PGLENS_ALERT_SLACK_WEBHOOK_URL_FILE" {
			return "/run/secrets/slack"
		}
		return ""
	}, func(path string) ([]byte, error) {
		require.Equal(t, "/run/secrets/slack", path)
		return []byte("alert-file\n"), nil
	})
	require.NoError(t, err)
	require.Equal(t, "alert-file", c.SlackURL)
}
func TestConfig_InvalidIntervalIsRejected(t *testing.T) {
	_, err := LoadAlertConfig(func(k string) string {
		if k == "PGLENS_ALERT_INTERVAL" {
			return "nope"
		}
		return ""
	}, nil)
	require.Error(t, err)
}
