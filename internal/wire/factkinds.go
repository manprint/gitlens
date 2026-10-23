package wire

import "sort"

// FactKind describes one kind of fact the server accepts.
type FactKind struct {
	// Check names the check that reports the kind; empty when the kind does
	// not come from a check's scrape (plan, check_skip).
	Check string
	// Replace means a complete result of Check lists every fact of the kind
	// for its database. The server then removes the facts of the kind that
	// result no longer lists: the object was dropped, or the problem the
	// fact reported was fixed. A check that can report nothing at all for a
	// database (no disabled trigger left) needs Replace, or its last fact
	// would stay forever.
	Replace bool
}

// factKinds lists every fact kind. Validate refuses any other, and the agent
// drops a fact Validate refuses before sending it: a check that reports a
// new kind must add it here.
var factKinds = map[string]FactKind{
	"index_def":       {Check: "index_stats", Replace: true},
	"setting":         {Check: "settings", Replace: true},
	"lock_tree":       {Check: "locks"},
	"plan":            {},
	"check_skip":      {},
	"monitoring_role": {Check: "monitoring_role", Replace: true},
}

// LookupFactKind returns the description of a fact kind, and whether the
// kind exists.
func LookupFactKind(kind string) (FactKind, bool) {
	k, ok := factKinds[kind]
	return k, ok
}

// ReplacedFactKinds returns, sorted, the kinds a complete result of the check
// replaces for its database.
func ReplacedFactKinds(check string) []string {
	var kinds []string
	for name, k := range factKinds {
		if k.Replace && k.Check == check {
			kinds = append(kinds, name)
		}
	}
	sort.Strings(kinds)
	return kinds
}
