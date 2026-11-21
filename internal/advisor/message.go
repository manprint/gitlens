package advisor

import "github.com/manprint/pglens/internal/i18n"

// messagePrefix scopes the advisor texts in internal/i18n/locales.
const messagePrefix = "advisor."

// Message is a finding's text as catalog data: the keys advisor.<Key>.title,
// .detail and (when the catalog has it) .remediation, filled from Args. The
// stored English text is rendered from the same entries, so each language
// reads one source.
type Message struct {
	Key  string    `json:"key"`
	Args i18n.Args `json:"args,omitempty"`
}

// Render is the title, detail and remediation in locale. A text the catalog
// lacks reads as the empty string, so callers keep the stored English.
func (m Message) Render(locale string) (title, detail, remediation string) {
	text := func(part string) string {
		key := messagePrefix + m.Key + "." + part
		if !i18n.Has(key) {
			return ""
		}
		return i18n.T(locale, key, m.Args)
	}
	return text("title"), text("detail"), text("remediation")
}

// strictMessages makes say panic on a message the catalog lacks or a
// placeholder args do not fill; the package tests set it (TestMain), so a rule
// cannot store a literal {name}.
var strictMessages bool

// say sets f's English texts and its message from the catalog entry key.
func (f Finding) say(key string, args i18n.Args) Finding {
	if strictMessages {
		checkMessage(key, args)
	}
	f.Message = &Message{Key: key, Args: args}
	f.Title, f.Detail, f.Remediation = f.Message.Render(i18n.Default)
	return f
}

func checkMessage(key string, args i18n.Args) {
	for _, part := range []string{"title", "detail", "remediation"} {
		msg, ok := i18n.Raw(messagePrefix + key + "." + part)
		if !ok {
			if part == "remediation" {
				continue
			}
			panic("advisor message " + key + " has no " + part)
		}
		for _, name := range i18n.Placeholders(msg) {
			if _, ok := args[name]; !ok {
				panic("advisor message " + key + "." + part + " lacks argument " + name)
			}
		}
	}
}
