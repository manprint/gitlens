package readiness

import "github.com/manprint/pglens/internal/i18n"

// messagePrefix opens every readiness key of the catalog
// (internal/i18n/locales/readiness.<code>.json).
const messagePrefix = "readiness."

// strictMessages makes say panic on a key or argument the English catalog
// lacks; the package tests turn it on.
var strictMessages bool

// say is the catalog text of key in the language of in (plan 006 F5).
func (in Input) say(key string, args i18n.Args) string {
	if strictMessages {
		msg, ok := i18n.Raw(messagePrefix + key)
		if !ok {
			panic("readiness message " + key + " is not in the catalog")
		}
		for _, name := range i18n.Placeholders(msg) {
			if _, ok := args[name]; !ok {
				panic("readiness message " + key + " lacks argument " + name)
			}
		}
	}
	return i18n.T(in.Locale, messagePrefix+key, args)
}

// sayAll is say for each key, without arguments.
func (in Input) sayAll(keys ...string) []string {
	out := make([]string, len(keys))
	for i, key := range keys {
		out[i] = in.say(key, nil)
	}
	return out
}
