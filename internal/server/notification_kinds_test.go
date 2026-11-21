package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/alert/notify"
)

const canaryTeamsURL = "https://prod-01.westeurope.logic.azure.com:443/workflows/TEAMSFLOW0401/triggers/manual/paths/invoke?api-version=2016-06-01&sig=teamsSig0402"

// NCH-004: a Teams workflow URL set from the console is stored and swapped
// into the slot; an http address of a public host is refused as for every
// webhook, and nothing is stored.
func TestNCH004_SetTeams(t *testing.T) {
	logged := captureLog(t)
	api, st := storedAPI(AlertConfig{})
	require.False(t, notify.Configured(api.notifications.slots["teams"]))

	rec := retentionRequest(t, api, http.MethodPut, "/api/v1/notification-channels/teams", urlBody(t, canaryTeamsURL))
	row := decodeChannelRow(t, rec)
	require.Equal(t, notificationChannelJSON{Kind: "teams", Configured: true, Source: "ui", Host: strPtr("prod-01.westeurope.logic.azure.com")}, row)
	stored, ok := st.stored("teams")
	require.True(t, ok)
	require.Equal(t, canaryTeamsURL, stored)
	require.True(t, notify.Configured(api.notifications.slots["teams"]), "the slot delivers through the stored URL")

	bare, bareStore := storedAPI(AlertConfig{})
	rec = retentionRequest(t, bare, http.MethodPut, "/api/v1/notification-channels/teams", urlBody(t, "http://teams.canary.example/workflows/TEAMSFLOW0401"))
	requireAPIError(t, rec, http.StatusBadRequest, "invalid url")
	_, ok = bareStore.stored("teams")
	require.False(t, ok)
	requireNoSecret(t, []string{"TEAMSFLOW0401", "teamsSig0402"}, rec.Body.String(), logged.String())
}

// NCH-005 (I-7): the listing shows the host of the Teams URL, never the URL;
// the environment variable configures the kind.
func TestNCH005_TeamsListingShowsHostOnly(t *testing.T) {
	cfg, err := LoadAlertConfig(func(k string) string {
		if k == "PGLENS_TEAMS_WEBHOOK_URL" {
			return " " + canaryTeamsURL + " "
		}
		return ""
	}, nil)
	require.NoError(t, err)
	require.Equal(t, canaryTeamsURL, cfg.TeamsURL)

	api, _ := storedAPI(cfg)
	rec := retentionRequest(t, api, http.MethodGet, "/api/v1/notification-channels", "")
	require.Equal(t, http.StatusOK, rec.Code)
	teams := rowOf(t, api, "teams")
	require.Equal(t, notificationChannelJSON{Kind: "teams", Configured: true, Source: "env", Host: strPtr("prod-01.westeurope.logic.azure.com")}, teams)
	require.NotContains(t, rec.Body.String(), `"url"`)
	requireNoSecret(t, []string{canaryTeamsURL, "TEAMSFLOW0401", "teamsSig0402"}, rec.Body.String())
}

func rowOf(t *testing.T, api *API, kind string) notificationChannelJSON {
	t.Helper()
	for _, row := range notificationRows(t, api) {
		if row.Kind == kind {
			return row
		}
	}
	t.Fatalf("no row of %s", kind)
	return notificationChannelJSON{}
}

const canaryPagerDutyKey = "PDKEY0501abcdefghijklmnopqrstuvw"

// NCH-014 (I-7): a routing key of 31 characters is a 400 that quotes neither
// the key nor any part of it, in the response or the log; a valid key is
// stored and listed with the host events.pagerduty.com only. An invalid key in
// the environment stops the start.
func TestNCH014_SetPagerDuty(t *testing.T) {
	logged := captureLog(t)
	api, st := storedAPI(AlertConfig{})
	bodies := []string{}

	rec := retentionRequest(t, api, http.MethodPut, "/api/v1/notification-channels/pagerduty", urlBody(t, canaryPagerDutyKey[:31]))
	bodies = append(bodies, rec.Body.String())
	requireAPIError(t, rec, http.StatusBadRequest, "invalid url")
	require.Contains(t, rec.Body.String(), "routing key must be 32 letters or digits")
	_, ok := st.stored("pagerduty")
	require.False(t, ok)

	rec = retentionRequest(t, api, http.MethodPut, "/api/v1/notification-channels/pagerduty", urlBody(t, canaryPagerDutyKey))
	bodies = append(bodies, rec.Body.String())
	require.Equal(t, notificationChannelJSON{Kind: "pagerduty", Configured: true, Source: "ui", Host: strPtr("events.pagerduty.com")}, decodeChannelRow(t, rec))
	listing := retentionRequest(t, api, http.MethodGet, "/api/v1/notification-channels", "")
	bodies = append(bodies, listing.Body.String())
	require.Equal(t, strPtr("events.pagerduty.com"), rowOf(t, api, "pagerduty").Host)
	require.Contains(t, logged.String(), "; host events.pagerduty.com")
	requireNoSecret(t, []string{canaryPagerDutyKey[:31], "PDKEY0501"}, append(bodies, logged.String())...)

	_, err := LoadAlertConfig(func(k string) string {
		if k == "PGLENS_PAGERDUTY_ROUTING_KEY" {
			return canaryPagerDutyKey[:31]
		}
		return ""
	}, nil)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "PDKEY0501")
	cfg, err := LoadAlertConfig(func(k string) string {
		if k == "PGLENS_PAGERDUTY_ROUTING_KEY" {
			return canaryPagerDutyKey
		}
		return ""
	}, nil)
	require.NoError(t, err)
	require.Equal(t, canaryPagerDutyKey, cfg.PagerDutyRoutingKey)
}

const canarySMTPPassword = "smtpPw0601canary"

func smtpEnv(values map[string]string) func(string) string {
	return func(k string) string { return values[k] }
}

// NCH-023: each invalid SMTP environment stops the start, without quoting the
// password; no host means the channel is off.
func TestNCH023_LoadSMTPConfig(t *testing.T) {
	base := map[string]string{"PGLENS_SMTP_HOST": "mail.canary.example", "PGLENS_SMTP_FROM": "pglens <alerts@canary.example>"}
	with := func(k, v string) map[string]string {
		out := map[string]string{}
		for key, value := range base {
			out[key] = value
		}
		out[k] = v
		return out
	}
	for name, env := range map[string]map[string]string{
		"missing from":      with("PGLENS_SMTP_FROM", ""),
		"from not address":  with("PGLENS_SMTP_FROM", "not an address"),
		"username only":     with("PGLENS_SMTP_USERNAME", "user"),
		"password only":     with("PGLENS_SMTP_PASSWORD", canarySMTPPassword),
		"port zero":         with("PGLENS_SMTP_PORT", "0"),
		"port too high":     with("PGLENS_SMTP_PORT", "65536"),
		"port not a number": with("PGLENS_SMTP_PORT", "smtp"),
		"tls unknown":       with("PGLENS_SMTP_TLS", "ssl"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadSMTPConfig(smtpEnv(env), nil)
			require.Error(t, err)
			require.NotContains(t, err.Error(), canarySMTPPassword)
			_, err = LoadAlertConfig(smtpEnv(env), nil)
			require.Error(t, err)
		})
	}

	off, err := LoadSMTPConfig(smtpEnv(map[string]string{"PGLENS_SMTP_FROM": "a@b.example"}), nil)
	require.NoError(t, err)
	require.Equal(t, SMTPConfig{}, off)

	cfg, err := LoadSMTPConfig(smtpEnv(base), nil)
	require.NoError(t, err)
	require.Equal(t, SMTPConfig{Host: "mail.canary.example", Port: 587, From: "pglens <alerts@canary.example>", TLS: "starttls"}, cfg)

	full := with("PGLENS_SMTP_PORT", "465")
	full["PGLENS_SMTP_TLS"] = "TLS"
	full["PGLENS_SMTP_USERNAME"] = "user"
	full["PGLENS_SMTP_PASSWORD_FILE"] = "/run/secrets/smtp"
	cfg, err = LoadSMTPConfig(smtpEnv(full), func(string) ([]byte, error) { return []byte(canarySMTPPassword + "\n"), nil })
	require.NoError(t, err)
	require.Equal(t, SMTPConfig{Host: "mail.canary.example", Port: 465, Username: "user", Password: canarySMTPPassword, From: "pglens <alerts@canary.example>", TLS: "tls"}, cfg)
}

// NCH-024 (I-7): without a mail server the email kind is a 409
// smtp_not_configured; with one, 21 recipients are a 400 and two are stored
// and listed as "2 recipients". Neither the recipients nor the password
// appear in a response or the log.
func TestNCH024_SetEmail(t *testing.T) {
	logged := captureLog(t)
	two := "Ops <ops0601@canary.example>, dba0602@canary.example"
	bodies := []string{}

	off, offStore := storedAPI(AlertConfig{})
	rec := retentionRequest(t, off, http.MethodPut, "/api/v1/notification-channels/email", urlBody(t, two))
	bodies = append(bodies, rec.Body.String())
	requireAPIError(t, rec, http.StatusConflict, "smtp_not_configured")
	_, ok := offStore.stored("email")
	require.False(t, ok)

	smtpCfg := SMTPConfig{Host: "mail.canary.example", Port: 587, Username: "user", Password: canarySMTPPassword, From: "alerts@canary.example", TLS: "starttls"}
	api, st := storedAPI(AlertConfig{SMTP: smtpCfg})
	require.False(t, notify.Configured(api.notifications.slots["email"]))
	many := make([]string, 21)
	for i := range many {
		many[i] = "ops0603@canary.example"
	}
	for _, bad := range []string{strings.Join(many, ", "), "", "not an address", "ops0604@canary.example\r\nBcc: x@canary.example"} {
		rec = retentionRequest(t, api, http.MethodPut, "/api/v1/notification-channels/email", urlBody(t, bad))
		bodies = append(bodies, rec.Body.String())
		requireAPIError(t, rec, http.StatusBadRequest, "invalid url")
		require.Contains(t, rec.Body.String(), "between 1 and 20 recipients are required")
	}
	_, ok = st.stored("email")
	require.False(t, ok)

	rec = retentionRequest(t, api, http.MethodPut, "/api/v1/notification-channels/email", urlBody(t, two))
	bodies = append(bodies, rec.Body.String())
	require.Equal(t, notificationChannelJSON{Kind: "email", Configured: true, Source: "ui", Host: strPtr("2 recipients")}, decodeChannelRow(t, rec))
	stored, ok := st.stored("email")
	require.True(t, ok)
	require.Equal(t, two, stored)
	require.True(t, notify.Configured(api.notifications.slots["email"]))
	listing := retentionRequest(t, api, http.MethodGet, "/api/v1/notification-channels", "")
	bodies = append(bodies, listing.Body.String())
	require.Equal(t, strPtr("2 recipients"), rowOf(t, api, "email").Host)
	require.Contains(t, logged.String(), "; host 2 recipients")
	require.Equal(t, "1 recipient", emailRecipientsHost("ops@canary.example"))
	requireNoSecret(t, []string{canarySMTPPassword, "ops0601", "dba0602", "ops0603", "ops0604"}, append(bodies, logged.String())...)
}
