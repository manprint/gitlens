// Package sqltag reads sqlcommenter tags: the one comment an application
// framework appends to a statement, /*key='value',key2='value2'*/, with keys
// and values URL-encoded and a quote inside a value written \'.
package sqltag

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/manprint/pglens/internal/redact"
)

const (
	maxBody  = 4096
	maxPairs = 20
	maxLabel = 128
)

var traceparent = regexp.MustCompile(`^[0-9a-f]{2}-([0-9a-f]{32})-[0-9a-f]{16}-[0-9a-f]{2}$`)

// Parse returns the tags of the comment that ends text, or an empty map when
// text does not end with a well-formed sqlcommenter comment.
func Parse(text string) map[string]string {
	tags := map[string]string{}
	text = strings.TrimRightFunc(text, unicode.IsSpace)
	text = strings.TrimSuffix(text, ";")
	text = strings.TrimRightFunc(text, unicode.IsSpace)
	if !strings.HasSuffix(text, "*/") {
		return tags
	}
	head := text[:len(text)-2]
	start := strings.LastIndex(head, "/*")
	if start < 0 {
		return tags
	}
	body := head[start+2:]
	if len(body) > maxBody || strings.HasPrefix(body, "+") {
		return tags
	}
	pairs, ok := splitPairs(body)
	if !ok || len(pairs) == 0 || len(pairs) > maxPairs {
		return tags
	}
	for _, p := range pairs {
		key, err := decode(p[0])
		if err != nil || key == "" {
			continue
		}
		value, err := decode(p[1])
		if err != nil {
			continue
		}
		tags[key] = value
	}
	return tags
}

func decode(s string) (string, error) {
	return url.PathUnescape(strings.ReplaceAll(s, `\'`, `'`))
}

// splitPairs reads key='value' pairs separated by commas, with optional
// spaces around the commas. Any other text makes the whole body invalid.
func splitPairs(body string) ([][2]string, bool) {
	var pairs [][2]string
	i, n := 0, len(body)
	skipSpaces := func() {
		for i < n && (body[i] == ' ' || body[i] == '\t' || body[i] == '\n' || body[i] == '\r') {
			i++
		}
	}
	for {
		skipSpaces()
		keyStart := i
		for i < n && body[i] != '=' && body[i] != '\'' && body[i] != ',' && body[i] != ' ' {
			i++
		}
		if i == keyStart || i >= n || body[i] != '=' {
			return nil, false
		}
		key := body[keyStart:i]
		i++
		if i >= n || body[i] != '\'' {
			return nil, false
		}
		i++
		valueStart := i
		for i < n && body[i] != '\'' {
			if body[i] == '\\' && i+1 < n && body[i+1] == '\'' {
				i += 2
				continue
			}
			i++
		}
		if i >= n {
			return nil, false
		}
		pairs = append(pairs, [2]string{key, body[valueStart:i]})
		if len(pairs) > maxPairs {
			return pairs, true
		}
		i++
		skipSpaces()
		if i == n {
			return pairs, true
		}
		if body[i] != ',' {
			return nil, false
		}
		i++
	}
}

// Service is the application that issued the statement.
func Service(tags map[string]string) string {
	return label(tags["application"])
}

// Endpoint is the route, else controller#action, else controller, else action.
func Endpoint(tags map[string]string) string {
	if route := tags["route"]; route != "" {
		return label(route)
	}
	controller, action := tags["controller"], tags["action"]
	switch {
	case controller != "" && action != "":
		return label(controller + "#" + action)
	case controller != "":
		return label(controller)
	default:
		return label(action)
	}
}

// TraceID is the trace id of a W3C traceparent tag, or "" when there is none
// or it is malformed or all zeros.
func TraceID(tags map[string]string) string {
	m := traceparent.FindStringSubmatch(tags["traceparent"])
	if m == nil || m[1] == strings.Repeat("0", 32) {
		return ""
	}
	return m[1]
}

func label(s string) string {
	if len(s) > maxLabel {
		cut := maxLabel
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut]
	}
	if !utf8.ValidString(s) {
		return ""
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return ""
		}
	}
	if redact.MayHoldCredential(s) {
		return ""
	}
	return s
}
