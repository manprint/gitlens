// Package i18n holds the server's translated texts: one catalog per language
// in locales/<code>.json, plus area files locales/<area>.<code>.json merged
// into it (advisor_<rules file>.<code>.json hold the finding texts), English the reference
// and the fallback. A message
// uses a subset of ICU MessageFormat: {name}, {n, plural, =0 {…} one {…}
// other {…}} with # for the number, and {v, select, a {…} other {…}}. Adding
// a language is adding its catalog file and its plural rule below.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"math"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Default is the reference language and the fallback of every other.
const Default = "en"

//go:embed locales/*.json
var files embed.FS

var catalogs = mustLoad()

func mustLoad() map[string]map[string]string {
	entries, err := files.ReadDir("locales")
	if err != nil {
		panic(err)
	}
	out := map[string]map[string]string{}
	for _, e := range entries {
		raw, err := files.ReadFile(path.Join("locales", e.Name()))
		if err != nil {
			panic(err)
		}
		cat := map[string]string{}
		if err := json.Unmarshal(raw, &cat); err != nil {
			panic(fmt.Sprintf("i18n: %s: %v", e.Name(), err))
		}
		code := strings.TrimSuffix(e.Name(), ".json")
		if i := strings.LastIndex(code, "."); i >= 0 {
			code = code[i+1:]
		}
		if out[code] == nil {
			out[code] = map[string]string{}
		}
		for key, msg := range cat {
			if _, dup := out[code][key]; dup {
				panic(fmt.Sprintf("i18n: %s: key %s defined twice", e.Name(), key))
			}
			out[code][key] = msg
		}
	}
	if out[Default] == nil {
		panic("i18n: no " + Default + " catalog")
	}
	return out
}

// Locales lists the languages that have a catalog, sorted.
func Locales() []string {
	out := make([]string, 0, len(catalogs))
	for code := range catalogs {
		out = append(out, code)
	}
	sort.Strings(out)
	return out
}

// Resolve maps a requested language ("it", "it-IT", "IT_it") to one with a
// catalog, or to Default.
func Resolve(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if _, ok := catalogs[code]; ok {
		return code
	}
	if _, ok := catalogs[language(code)]; ok {
		return language(code)
	}
	return Default
}

// language is the language part of a code ("it" of "it-IT"), lower case.
func language(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if i := strings.IndexAny(code, "-_"); i > 0 {
		return code[:i]
	}
	return code
}

// Args are the values a message's placeholders name.
type Args = map[string]any

// T is the text of key in locale, its placeholders filled from args. A key the
// locale lacks falls back to English, and one English lacks reads as the key.
func T(locale, key string, args Args) string {
	locale = Resolve(locale)
	msg, ok := catalogs[locale][key]
	if !ok {
		if msg, ok = catalogs[Default][key]; !ok {
			return key
		}
		locale = Default
	}
	return Format(locale, msg, args)
}

// Has reports whether the reference catalog defines key.
func Has(key string) bool {
	_, ok := catalogs[Default][key]
	return ok
}

// Raw is the reference (English) message of key, unformatted.
func Raw(key string) (string, bool) {
	msg, ok := catalogs[Default][key]
	return msg, ok
}

// Format fills one message in locale.
func Format(locale, msg string, args Args) string {
	var b strings.Builder
	formatInto(&b, locale, msg, args, nil)
	return b.String()
}

func formatInto(b *strings.Builder, locale, msg string, args Args, hash *float64) {
	for i := 0; i < len(msg); {
		switch c := msg[i]; {
		case c == '{':
			end := matchBrace(msg, i)
			if end < 0 {
				b.WriteString(msg[i:])
				return
			}
			formatArg(b, locale, msg[i+1:end], args, hash)
			i = end + 1
		case c == '#' && hash != nil:
			b.WriteString(Number(locale, *hash))
			i++
		default:
			b.WriteByte(c)
			i++
		}
	}
}

func matchBrace(s string, open int) int {
	depth := 0
	for j := open; j < len(s); j++ {
		switch s[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return -1
}

func formatArg(b *strings.Builder, locale, body string, args Args, hash *float64) {
	parts := strings.SplitN(body, ",", 3)
	name := strings.TrimSpace(parts[0])
	v, ok := args[name]
	if len(parts) < 3 {
		if !ok {
			b.WriteString("{" + name + "}")
			return
		}
		b.WriteString(Value(locale, v))
		return
	}
	branches := parseBranches(parts[2])
	switch strings.TrimSpace(parts[1]) {
	case "plural":
		n, _ := toFloat(v)
		text, found := branches["="+Number(Default, n)]
		if !found {
			if text, found = branches[PluralCategory(locale, n)]; !found {
				text = branches["other"]
			}
		}
		formatInto(b, locale, text, args, &n)
	case "select":
		text, found := branches[fmt.Sprint(v)]
		if !found {
			text = branches["other"]
		}
		formatInto(b, locale, text, args, hash)
	default:
		b.WriteString("{" + body + "}")
	}
}

// parseBranches reads "sel {text} sel {text} …".
func parseBranches(s string) map[string]string {
	out := map[string]string{}
	for i := 0; i < len(s); {
		for i < len(s) && (s[i] == ' ' || s[i] == '\n' || s[i] == '\t') {
			i++
		}
		start := i
		for i < len(s) && s[i] != '{' && s[i] != ' ' {
			i++
		}
		sel := s[start:i]
		for i < len(s) && s[i] != '{' {
			i++
		}
		if i >= len(s) {
			break
		}
		end := matchBrace(s, i)
		if end < 0 {
			break
		}
		out[sel] = s[i+1 : end]
		i = end + 1
	}
	return out
}

// PluralCategory is the CLDR plural category of n in locale, for the
// languages with a catalog and the ones planned next.
func PluralCategory(locale string, n float64) string {
	integer := n == math.Trunc(n)
	switch language(locale) {
	case "fr":
		if n >= 0 && n < 2 {
			return "one"
		}
	default: // en, it, de
		if integer && n == 1 {
			return "one"
		}
	}
	return "other"
}

// decimalComma lists the languages that write 1,5 rather than 1.5.
var decimalComma = map[string]bool{"it": true, "de": true, "fr": true}

// Number writes n with at most two decimals, in the locale's decimal mark.
func Number(locale string, n float64) string {
	s := strconv.FormatFloat(n, 'f', 2, 64)
	return decimalMark(locale, strings.TrimRight(strings.TrimRight(s, "0"), "."))
}

func decimalMark(locale, s string) string {
	if decimalComma[language(locale)] {
		return strings.Replace(s, ".", ",", 1)
	}
	return s
}

// Fixed is a number written with exactly Digits decimals (as %.Nf does), in
// the locale's decimal mark. A stored message keeps it: it travels in JSON as
// {"$fixed":"12.30"}, which Value reads back.
type Fixed struct {
	V      float64
	Digits int
}

const fixedKey = "$fixed"

func (f Fixed) String() string { return strconv.FormatFloat(f.V, 'f', f.Digits, 64) }

// MarshalJSON keeps the digits the rule chose.
func (f Fixed) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{fixedKey: f.String()})
}

// Value writes one placeholder value.
func Value(locale string, v any) string {
	switch x := v.(type) {
	case Fixed:
		return decimalMark(locale, x.String())
	case map[string]any:
		if s, ok := x[fixedKey].(string); ok && len(x) == 1 {
			return decimalMark(locale, s)
		}
	}
	if f, ok := toFloat(v); ok {
		return Number(locale, f)
	}
	return fmt.Sprint(v)
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case float64:
		return x, true
	case float32:
		return float64(x), true
	}
	return 0, false
}

// Placeholders lists the argument names a message uses, sorted, for the
// catalog parity test.
func Placeholders(msg string) []string {
	set := map[string]bool{}
	var walk func(string)
	walk = func(m string) {
		for i := 0; i < len(m); i++ {
			if m[i] != '{' {
				continue
			}
			end := matchBrace(m, i)
			if end < 0 {
				return
			}
			parts := strings.SplitN(m[i+1:end], ",", 3)
			set[strings.TrimSpace(parts[0])] = true
			if len(parts) == 3 {
				for _, text := range parseBranches(parts[2]) {
					walk(text)
				}
			}
			i = end
		}
	}
	walk(msg)
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
