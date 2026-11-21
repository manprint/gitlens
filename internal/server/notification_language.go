package server

import (
	"context"
	"log"
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/manprint/pglens/internal/i18n"
)

// Plan 006 F7: each notification channel writes its messages in a language
// set from the console; without one it uses the server default (PGLENS_LANG).
// The language lives in the channel's slot, so the next alert reads it.

// notificationLanguageStore is the database side of the channel languages.
// The database store implements it; a test stub may not, and then no language
// is stored.
type notificationLanguageStore interface {
	// NotificationLanguages answers the stored languages, kind to code.
	NotificationLanguages(ctx context.Context) (map[string]string, error)
	// SetNotificationLanguage stores the language of kind, set by updatedBy;
	// "" removes it.
	SetNotificationLanguage(ctx context.Context, kind, language, updatedBy string) error
}

const (
	notificationLanguagesSQL      = `SELECT kind, language FROM notification_channel_languages WHERE tenant_id = 'default'`
	notificationLanguageUpsertSQL = `INSERT INTO notification_channel_languages (tenant_id, kind, language, updated_at, updated_by) VALUES ('default', $1, $2, now(), $3)
		ON CONFLICT (tenant_id, kind) DO UPDATE SET language = EXCLUDED.language, updated_at = now(), updated_by = EXCLUDED.updated_by`
	notificationLanguageDeleteSQL = `DELETE FROM notification_channel_languages WHERE tenant_id = 'default' AND kind = $1`
)

func (s pgNotificationChannelStore) NotificationLanguages(ctx context.Context) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, notificationLanguagesSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var kind, language string
		if err := rows.Scan(&kind, &language); err != nil {
			return nil, err
		}
		out[kind] = language
	}
	return out, rows.Err()
}

func (s pgNotificationChannelStore) SetNotificationLanguage(ctx context.Context, kind, language, updatedBy string) error {
	if language == "" {
		_, err := s.pool.Exec(ctx, notificationLanguageDeleteSQL, kind)
		return err
	}
	_, err := s.pool.Exec(ctx, notificationLanguageUpsertSQL, kind, language, updatedBy)
	return err
}

func (a *API) notificationLanguageStore() notificationLanguageStore {
	ls, _ := a.notificationChannelStore().(notificationLanguageStore)
	return ls
}

// loadNotificationLanguages puts the stored languages in the slots. A language
// the server no longer has is ignored and logged by kind.
func (a *API) loadNotificationLanguages(ctx context.Context) error {
	ls := a.notificationLanguageStore()
	if ls == nil {
		return nil
	}
	stored, err := ls.NotificationLanguages(ctx)
	if err != nil {
		return err
	}
	n := a.notificationState()
	for _, k := range notificationKinds {
		language, ok := stored[k.Kind]
		if !ok {
			continue
		}
		if !slices.Contains(i18n.Locales(), language) {
			log.Printf("notifications: ignoring the language %q of %s: not a console language", language, k.Kind)
			continue
		}
		n.slots[k.Kind].SetLocale(language)
	}
	return nil
}

type notificationLanguageBody struct {
	Language *string `json:"language"`
}

// handleSetNotificationLanguage sets the language of a kind's messages; null
// goes back to the server default. The answer is the kind's status row.
func (a *API) handleSetNotificationLanguage(w http.ResponseWriter, r *http.Request) {
	k, ok := lookupNotificationKind(chi.URLParam(r, "kind"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown channel", "the channel kinds are slack, webhook and google_chat")
		return
	}
	var in notificationLanguageBody
	if !decodeStrictJSONBody(w, r, &in) {
		return
	}
	language := ""
	if in.Language != nil {
		language = *in.Language
		if !slices.Contains(i18n.Locales(), language) {
			writeError(w, http.StatusBadRequest, "invalid language", "language is null (server default) or one of the console languages")
			return
		}
	}
	ls := a.notificationLanguageStore()
	if ls == nil {
		writeError(w, http.StatusServiceUnavailable, "store unavailable", "")
		return
	}
	n := a.notificationState()
	requester := requesterOf(r, a.auth)
	// Not cancelled with the request, as in handleSetNotificationChannel.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer cancel()
	n.change.Lock()
	err := ls.SetNotificationLanguage(ctx, k.Kind, language, requester)
	if err == nil {
		n.slots[k.Kind].SetLocale(language)
	}
	n.change.Unlock()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "store unavailable", n.redact(err).Error())
		return
	}
	shown := language
	if shown == "" {
		shown = "the server default"
	}
	log.Printf("notifications: language of %s set from the console by %s: %s", k.Kind, requester, shown)
	writeJSON(w, http.StatusOK, n.row(k))
}
