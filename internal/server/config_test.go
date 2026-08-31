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

func TestLoadUIConfigDefaults(t *testing.T) {
	c, err := LoadUIConfig(func(string) string { return "" }, nil)
	require.NoError(t, err)
	require.True(t, c.Enabled)
	require.Empty(t, c.Password)
	require.Equal(t, 24*time.Hour, c.SessionTTL)
	require.Equal(t, "auto", c.CookieSecure)
}

func TestLoadUIConfigParsesValuesAndPasswordFile(t *testing.T) {
	env := map[string]string{
		"PGLENS_UI_ENABLED":       "false",
		"PGLENS_UI_PASSWORD":      "inline-password",
		"PGLENS_UI_PASSWORD_FILE": " /run/secrets/ui-password ",
		"PGLENS_UI_SESSION_TTL":   "5m",
		"PGLENS_UI_COOKIE_SECURE": "TrUe",
	}
	c, err := LoadUIConfig(func(key string) string { return env[key] }, func(path string) ([]byte, error) {
		require.Equal(t, "/run/secrets/ui-password", path)
		return []byte(" file-password \n"), nil
	})
	require.NoError(t, err)
	require.False(t, c.Enabled)
	require.Equal(t, "file-password", c.Password)
	require.Equal(t, 5*time.Minute, c.SessionTTL)
	require.Equal(t, "true", c.CookieSecure)
}

func TestLoadUIConfigEmptyPasswordFileDoesNotFallBack(t *testing.T) {
	env := map[string]string{
		"PGLENS_UI_PASSWORD":      "inline-password",
		"PGLENS_UI_PASSWORD_FILE": "/run/secrets/ui-password",
	}
	c, err := LoadUIConfig(func(key string) string { return env[key] }, func(string) ([]byte, error) {
		return []byte(" \n\t"), nil
	})
	require.NoError(t, err)
	require.Empty(t, c.Password)
}

func TestLoadUIConfigRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{name: "enabled", key: "PGLENS_UI_ENABLED", value: "sometimes", want: "invalid PGLENS_UI_ENABLED"},
		{name: "ttl syntax", key: "PGLENS_UI_SESSION_TTL", value: "sometimes", want: "invalid PGLENS_UI_SESSION_TTL"},
		{name: "ttl lower bound", key: "PGLENS_UI_SESSION_TTL", value: "4m", want: "accepted range"},
		{name: "ttl upper bound", key: "PGLENS_UI_SESSION_TTL", value: "721h", want: "accepted range"},
		{name: "cookie secure", key: "PGLENS_UI_COOKIE_SECURE", value: "sometimes", want: "invalid PGLENS_UI_COOKIE_SECURE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadUIConfig(func(key string) string {
				if key == tt.key {
					return tt.value
				}
				if key == "PGLENS_UI_PASSWORD" {
					return "super-secret-password"
				}
				return ""
			}, nil)
			require.ErrorContains(t, err, tt.want)
			require.NotContains(t, err.Error(), "super-secret-password")
		})
	}
}

func TestLoadUIConfigPasswordFileErrorDoesNotExposePassword(t *testing.T) {
	_, err := LoadUIConfig(func(key string) string {
		switch key {
		case "PGLENS_UI_PASSWORD":
			return "super-secret-password"
		case "PGLENS_UI_PASSWORD_FILE":
			return "/run/secrets/ui-password"
		default:
			return ""
		}
	}, func(string) ([]byte, error) {
		return nil, errTestPasswordFile
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "read UI password file")
	require.NotContains(t, err.Error(), "super-secret-password")
}

func TestUIConfigStringRedactsPassword(t *testing.T) {
	c := UIConfig{Enabled: true, Password: "super-secret-password", SessionTTL: 24 * time.Hour, CookieSecure: "auto"}
	rendered := c.String()
	require.Contains(t, rendered, "[redacted]")
	require.NotContains(t, rendered, c.Password)
}

var errTestPasswordFile = &testPasswordFileError{}

type testPasswordFileError struct{}

func (*testPasswordFileError) Error() string { return "permission denied" }
