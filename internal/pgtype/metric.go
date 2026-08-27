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
// (unit separator). Empty map yields "". The separator is chosen because it
// cannot appear in a PostgreSQL identifier or in any label value this project
// produces.
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
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(l[k])
	}
	return b.String()
}
