package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func (s *stubNotificationStore) NotificationLanguages(context.Context) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	out := map[string]string{}
	for k, v := range s.langs {
		out[k] = v
	}
	return out, nil
}

func (s *stubNotificationStore) SetNotificationLanguage(_ context.Context, kind, language, updatedBy string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	if language == "" {
		delete(s.langs, kind)
		return nil
	}
	s.langs[kind] = language
	s.by[kind] = updatedBy
	return nil
}

// NOTI-013 (plan 006 F7): the console sets the language of a channel's
// messages; null goes back to the server default. The row, the list and the
// slot the notifier sends through all read it.
func TestNOTI013_ChannelLanguage(t *testing.T) {
	api, st := storedAPI(AlertConfig{SlackURL: "https://hooks.slack.example/services/X"})

	rec := retentionRequest(t, api, http.MethodPut, "/api/v1/notification-channels/slack/language", `{"language":"it"}`)
	row := decodeChannelRow(t, rec)
	require.NotNil(t, row.Language)
	require.Equal(t, "it", *row.Language)
	require.Equal(t, "env", row.Source, "the language leaves the address alone")
	require.Equal(t, "it", st.langs["slack"])
	require.Equal(t, "it", api.notificationState().slots["slack"].Locale())

	rec = retentionRequest(t, api, http.MethodGet, "/api/v1/notification-channels", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var list []notificationChannelJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	languages := map[string]*string{}
	for _, c := range list {
		languages[c.Kind] = c.Language
	}
	require.Len(t, languages, len(notificationKinds))
	require.Equal(t, "it", *languages["slack"])
	require.Nil(t, languages["webhook"])

	rec = retentionRequest(t, api, http.MethodPut, "/api/v1/notification-channels/slack/language", `{"language":null}`)
	require.Nil(t, decodeChannelRow(t, rec).Language)
	require.NotContains(t, st.langs, "slack")
	require.Equal(t, "", api.notificationState().slots["slack"].Locale())

	for name, tc := range map[string]struct {
		path, body string
		code       int
	}{
		"unknown language": {"/api/v1/notification-channels/slack/language", `{"language":"xx"}`, http.StatusBadRequest},
		"empty language":   {"/api/v1/notification-channels/slack/language", `{"language":""}`, http.StatusBadRequest},
		"unknown field":    {"/api/v1/notification-channels/slack/language", `{"language":"it","locale":"it"}`, http.StatusBadRequest},
		"unknown kind":     {"/api/v1/notification-channels/teams/language", `{"language":"it"}`, http.StatusNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			rec := retentionRequest(t, api, http.MethodPut, tc.path, tc.body)
			require.Equal(t, tc.code, rec.Code, rec.Body.String())
		})
	}
	require.Empty(t, st.langs)
}

// NOTI-013 (load): the stored languages reach the slots at start; a code the
// server no longer has is left out.
func TestNOTI013_LoadChannelLanguages(t *testing.T) {
	api, st := storedAPI(AlertConfig{})
	st.langs["google_chat"] = "it"
	st.langs["webhook"] = "zz"
	logged := captureLog(t)
	require.NoError(t, api.LoadNotificationChannels(context.Background()))
	slots := api.notificationState().slots
	require.Equal(t, "it", slots["google_chat"].Locale())
	require.Equal(t, "", slots["webhook"].Locale())
	require.Equal(t, "", slots["slack"].Locale())
	require.Contains(t, logged.String(), `ignoring the language "zz" of webhook`)
}
