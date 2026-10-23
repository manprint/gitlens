package advisor

import (
	"fmt"
	"sort"
	"strings"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/wire"
)

// bloatBlindNamed bounds how many skipped tables the finding names. The fact
// itself names at most as many (the check's bloatNamedTables).
const bloatBlindNamed = 10

// bloatBlindRule reports, for each database, the tables whose column
// statistics the monitoring role cannot read. The bloat estimate has no row
// for them, so their bloat is unknown, not zero: the finding says so, and
// names up to bloatBlindNamed of the largest.
type bloatBlindRule struct{}

func (bloatBlindRule) ID() string               { return "bloat.estimate_blind" }
func (bloatBlindRule) Severity() Severity       { return SeverityInfo }
func (bloatBlindRule) Scope() Scope             { return ScopeDatabase }
func (bloatBlindRule) Needs() []string          { return []string{"Bloat"} }
func (bloatBlindRule) MinTier() pgtype.PermTier { return pgtype.TierReadOnly }
func (r bloatBlindRule) Evaluate(s *Snapshot) []Finding {
	var out []Finding
	for _, f := range s.Facts[wire.BloatUnreadableFact] {
		// The fact is rewritten by each scrape of its database. One that has
		// not been refreshed within the estimate's window says nothing now.
		if f.Datname == "" || (!s.Now.IsZero() && s.Now.Sub(f.ObservedAt) > bloatEstimateWindow) {
			continue
		}
		count, _ := f.Value["count"].(float64)
		if count < 1 {
			continue
		}
		names := bloatBlindNames(f.Value["tables"])
		reason := "bloat estimate blind: no readable column statistics for " + strings.Join(names, ", ")
		if more := int(count) - len(names); more > 0 {
			reason += fmt.Sprintf(" and %d more", more)
		}
		out = append(out, Finding{
			RuleID:         r.ID(),
			Severity:       SeverityInfo,
			State:          StateDegraded,
			Scope:          ScopeDatabase,
			Datname:        f.Datname,
			ObjectName:     "table statistics",
			Title:          "Bloat estimate blind",
			Detail:         fmt.Sprintf("%d tables have column statistics the monitoring role cannot read, so no bloat is estimated for them", int(count)),
			Remediation:    "Grant the monitoring role SELECT on the columns of these tables and row access that row security does not hide, or accept that they are not estimated",
			DegradedReason: reason,
			Evidence:       map[string]any{"skipped_tables": int(count), "tables": names},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Datname < out[j].Datname })
	return out
}

// bloatBlindNames reads the table names of the fact's value, largest first,
// as "schema.table". A malformed entry is skipped.
func bloatBlindNames(raw any) []string {
	entries, _ := raw.([]any)
	names := []string{}
	for _, entry := range entries {
		if len(names) == bloatBlindNamed {
			break
		}
		m, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		schema, _ := m["schemaname"].(string)
		relname, _ := m["relname"].(string)
		if relname == "" {
			continue
		}
		names = append(names, schema+"."+relname)
	}
	return names
}

func init() { Register(bloatBlindRule{}) }
