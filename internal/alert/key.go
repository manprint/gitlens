package alert

import (
	"fmt"
	"strings"

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
func DedupID(a Alert) string {
	return strings.Join([]string{a.Key, a.StartedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")}, "/")
}
