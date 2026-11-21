package server

import (
	"encoding/json"
	"net/http"

	"github.com/manprint/pglens/internal/advisor"
	"github.com/manprint/pglens/internal/i18n"
)

// langHeader carries the console's language (plan 006 D10). Without it the
// API answers in English, as before, for scripts and other clients.
const langHeader = "X-Pglens-Lang"

// requestLocale is the catalog locale the request asks for, or "" for English.
func requestLocale(r *http.Request) string {
	code := r.Header.Get(langHeader)
	if code == "" {
		return ""
	}
	if locale := i18n.Resolve(code); locale != i18n.Default {
		return locale
	}
	return ""
}

// localizeFinding renders f's texts in locale from its message; a text the
// catalog lacks keeps the stored English.
func localizeFinding(f *findingJSON, locale string) {
	if locale == "" {
		return
	}
	var m advisor.Message
	if err := json.Unmarshal(f.Message, &m); err != nil || m.Key == "" {
		return
	}
	title, detail, remediation := m.Render(locale)
	if title != "" {
		f.Title = title
	}
	if detail != "" {
		f.Detail = detail
	}
	if remediation != "" {
		f.Remediation = remediation
	}
}
