package server

import (
	"fmt"
	"net/http"
	"sort"
)

// MetricsHandler exposes phase_04.md §3.6's self-monitoring metrics in
// Prometheus text exposition format. staleness.go's own metric primitives
// (pglens_up, pglens_agent_last_seen_seconds, pglens_ingest_envelopes_total,
// etc.) existed and were incremented/set from day one, but nothing ever
// read them back out — this is the exposition its own top comment already
// anticipated ("a real Prometheus exposition would read from these
// globals") and that was never built.
func MetricsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		writeGaugeVec(w, "pglens_up", "1 if the instance's agent is currently considered reachable, 0 otherwise", "instance_id", pglensUpGauge.Snapshot())
		writeGaugeVec(w, "pglens_agent_last_seen_seconds", "unix timestamp of the instance's last accepted push", "instance_id", pglensAgentLastSeenGauge.Snapshot())
		writeGaugeVec(w, "pglens_series_total", "distinct metric series currently tracked for the instance (I-8 cardinality budget)", "instance_id", pglensSeriesTotalGauge.Snapshot())
		writeCounterVec(w, "pglens_ingest_envelopes_total", "envelopes accepted by /api/v1/push, by outcome", "result", pglensIngestEnvelopesTotal.Snapshot())
		writeCounterVec(w, "pglens_ingest_rejected_total", "envelopes rejected by /api/v1/push, by reason", "reason", pglensIngestRejectedTotal.Snapshot())
		writeCounterVec(w, "pglens_check_error_total", "check scrapes that reported an error, by check name", "check", pglensCheckErrorTotal.Snapshot())
		writeCounter(w, "pglens_samples_too_old_total", "samples rejected for exceeding maxSampleAge", pglensSamplesTooOldTotal.Get())
		writeCounter(w, "pglens_cardinality_truncated_total", "envelopes where a cardinality budget truncated results", pglensCardinalityTruncatedTotal.Get())
		writeGauge(w, "pglens_agent_clock_skew_seconds", "most recently observed agent/server clock skew", pglensAgentClockSkewSeconds.Get())
	}
}

func writeGauge(w http.ResponseWriter, name, help string, v float64) {
	_, _ = fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n%s %v\n", name, help, name, name, v)
}

func writeCounter(w http.ResponseWriter, name, help string, v float64) {
	_, _ = fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n%s %v\n", name, help, name, name, v)
}

func writeGaugeVec(w http.ResponseWriter, name, help, labelName string, vals map[string]float64) {
	_, _ = fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n", name, help, name)
	writeVecLines(w, name, labelName, vals)
}

func writeCounterVec(w http.ResponseWriter, name, help, labelName string, vals map[string]float64) {
	_, _ = fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n", name, help, name)
	writeVecLines(w, name, labelName, vals)
}

// writeVecLines renders each label value's line in a stable (sorted) order,
// so repeated scrapes diff cleanly and tests can assert on exact output.
func writeVecLines(w http.ResponseWriter, name, labelName string, vals map[string]float64) {
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		_, _ = fmt.Fprintf(w, "%s{%s=%q} %v\n", name, labelName, k, vals[k])
	}
}
