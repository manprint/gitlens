package alert

import (
	"fmt"
	"strings"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
)

func Key(r Rule, s Sample) string {
	subject := ""
	switch r.Scope {
	case ScopeCluster:
		if s.ClusterID != nil {
			subject = fmt.Sprint(*s.ClusterID)
		}
	case ScopeDatabase:
		subject = uuidText(s.InstanceID) + ":" + s.Datname
	default:
		subject = uuidText(s.InstanceID)
		// A sample that names a database is that database's episode, even when
		// its rule is instance-scoped. The databases of one instance used to
		// share an episode, so a database below the threshold closed the
		// episode of the one above it.
		if s.Datname != "" {
			subject += ":" + s.Datname
		}
	}
	return r.ID + "/" + subject + "/" + pgtype.CanonicalLabels(s.Labels)
}
func uuidText(v interface{}) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

// DedupID is the notification identity of an episode. The start is kept to the
// microsecond, the precision the alerts table keeps. A start can carry nanoseconds
// in memory, when the engine clock sets it (an agent_down episode does), and the
// copy read back from the table after a change of leader or a restart does not.
// Both must name the same episode, or a RESOLVED would not share the dedup_id of
// its FIRING, and the notification row that makes a claim unique would differ too.
func DedupID(a Alert) string {
	start := a.StartedAt.UTC().Truncate(time.Microsecond)
	return strings.Join([]string{a.Key, start.Format("2006-01-02T15:04:05.999999999Z07:00")}, "/")
}
