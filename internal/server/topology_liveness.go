package server

import (
	"fmt"
	"time"
)

// lowStreamingNote explains a stored low-confidence edge: the standby's
// last-known upstream while its WAL receiver is not streaming (see
// wire.Edge.Confidence). The pipeline drops an edge it cannot resolve.
const lowStreamingNote = "The standby is not streaming from this upstream right now; check connectivity"

// liveEdge serves an edge as live only while its reporting standby is up by the
// rule of instances[].up (isUp). A silent reporter keeps its last known upstream,
// served as low confidence; a NULL last_seen counts as not up.
func liveEdge(confidence string, fromLastSeen *time.Time, now time.Time) (string, *string) {
	if fromLastSeen == nil || !isUp(*fromLastSeen, now) {
		note := fmt.Sprintf("The standby has not reported for more than %d s; this is its last known upstream", int(upThreshold.Seconds()))
		return "low", &note
	}
	if confidence == "low" {
		note := lowStreamingNote
		return "low", &note
	}
	return confidence, nil
}
