package server

import (
	"net/http"
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
