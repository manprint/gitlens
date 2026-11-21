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
	rows := notificationRows(t, api)
	teams := rows[len(rows)-1]
	require.Equal(t, notificationChannelJSON{Kind: "teams", Configured: true, Source: "env", Host: strPtr("prod-01.westeurope.logic.azure.com")}, teams)
	require.NotContains(t, rec.Body.String(), `"url"`)
	requireNoSecret(t, []string{canaryTeamsURL, "TEAMSFLOW0401", "teamsSig0402"}, rec.Body.String())
}
