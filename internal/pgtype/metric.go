package pgtype

import (
	"sort"
	"strings"

	"github.com/google/uuid"
)

// MetricKind distinguishes gauges from counters.
type MetricKind string

const (
	KindGauge   MetricKind = "gauge"
	KindCounter MetricKind = "counter" // only counters go through internal/delta
)

// Metric is a single measurement.
type Metric struct {
	Name   string
	Labels map[string]string
	Value  float64
	Kind   MetricKind
}

// SeriesKey identifies one time series. It must be comparable so it can be a
// map key, which is why Labels is carried as a canonical string rather than a
// map.
type SeriesKey struct {
	Metric   string
	Instance uuid.UUID
	Database string // "" means instance scope, never the literal "postgres"
	Labels   string // produced by CanonicalLabels
}

// CanonicalLabels serializes labels deterministically: keys sorted
// lexicographically, each rendered as key=value, pairs joined by "\x1f"
// (unit separator). Empty map yields "". Keys and values are free text: a
// PostgreSQL identifier can hold control bytes such as "\x1f", so the
// delimiters inside them are escaped. "\x1e" (the escape byte itself) becomes
// "\x1e0", "\x1f" becomes "\x1e1", and "=" in a key becomes "\x1e2". Without
// this, two different label sets could share one canonical string and their
// series would merge. Only those bytes are rewritten, so ordinary labels keep
// their bytes and the series and alert keys hashed from them stay the same.
func CanonicalLabels(l map[string]string) string {
	if len(l) == 0 {
		return ""
	}
	keys := make([]string, 0, len(l))
	for k := range l {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteString("\x1f")
		}
		b.WriteString(labelKeyEscaper.Replace(k))
		b.WriteByte('=')
		b.WriteString(labelValueEscaper.Replace(l[k]))
	}
	return b.String()
}

// The replacers are safe for concurrent use. The key replacer also escapes "="
// because the first unescaped "=" is what separates a key from its value.
var (
	labelValueEscaper = strings.NewReplacer("\x1e", "\x1e0", "\x1f", "\x1e1")
	labelKeyEscaper   = strings.NewReplacer("\x1e", "\x1e0", "\x1f", "\x1e1", "=", "\x1e2")
)
