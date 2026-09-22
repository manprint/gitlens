package advisor

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/manprint/pglens/internal/pgtype"
)

const (
	unusedIndexBytes      = 10 * 1024 * 1024
	indexBloatBytes       = 50 * 1024 * 1024
	tableBloatBytes       = 100 * 1024 * 1024
	deadTupleLimit        = 10000
	deadTupleRatio        = .2
	autoAnalyzeRatio      = .2
	largeTableRows        = 100000
	wraparoundRelationAge = 1_000_000_000
)

type relationRule struct {
	id       string
	severity Severity
	needs    []string
	eval     func(*Snapshot) []Finding
}

func (r relationRule) ID() string                     { return r.id }
func (r relationRule) Severity() Severity             { return r.severity }
func (r relationRule) Scope() Scope                   { return ScopeRelation }
func (r relationRule) Needs() []string                { return append([]string(nil), r.needs...) }
func (r relationRule) MinTier() pgtype.PermTier       { return pgtype.TierReadOnly }
func (r relationRule) Evaluate(s *Snapshot) []Finding { return r.eval(s) }

// relationFinding names its relation within its database: the same schema
// and relation can exist in several databases of one instance, and their
// findings used to share one identity and overwrite each other.
func relationFinding(id string, sev Severity, datname, name, title, detail, remediation string, ev map[string]any) Finding {
	return Finding{RuleID: id, Severity: sev, State: StateOpen, Scope: ScopeRelation, Datname: datname, ObjectName: name, Title: title, Detail: detail, Remediation: remediation, Evidence: ev}
}
func degradedRelation(id string, sev Severity, datname, name, reason string) Finding {
	return Finding{RuleID: id, Severity: sev, State: StateDegraded, Scope: ScopeRelation, Datname: datname, ObjectName: name, Title: "Advisor input unavailable", Detail: reason, DegradedReason: reason}
}

// indexMethod is the access method of an index definition as
// pg_get_indexdef prints it ("... USING btree (...)"), or "" if unknown.
// The definition hash covers only the parenthesised part, so a btree and a
// GIN index on the same column share it.
func indexMethod(def string) string {
	i := strings.Index(strings.ToLower(def), " using ")
	if i < 0 {
		return ""
	}
	fields := strings.Fields(def[i+len(" using "):])
	if len(fields) == 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSuffix(fields[0], "("))
}

// indexPredicate is the WHERE clause of a partial index definition.
func indexPredicate(def string) string {
	if i := strings.LastIndex(strings.ToLower(def), " where "); i >= 0 {
		return strings.TrimSpace(def[i+len(" where "):])
	}
	return ""
}
func indexColumns(def string) ([]string, bool) {
	start := strings.Index(def, "(")
	if start < 0 {
		return nil, false
	}
	depth := 0
	end := -1
	for i := start; i < len(def); i++ {
		switch def[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				end = i
			}
		}
		if end >= 0 {
			break
		}
	}
	if end <= start+1 {
		return nil, false
	}
	var out []string
	begin := start + 1
	depth = 0
	for i := start + 1; i < end; i++ {
		switch def[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(def[begin:i]))
				begin = i + 1
			}
		}
	}
	out = append(out, strings.TrimSpace(def[begin:end]))
	return out, len(out) > 0
}
func isPrefix(a, b []string) bool {
	if len(a) >= len(b) {
		return false
	}
	for i := range a {
		if !strings.EqualFold(a[i], b[i]) {
			return false
		}
	}
	return true
}
func init() {
	Register(relationRule{id: "index.unused", severity: SeverityWarning, needs: []string{"Indexes"}, eval: func(s *Snapshot) []Finding {
		var out []Finding
		for _, i := range s.Indexes {
			if i.HistoryDays < 7 {
				out = append(out, degradedRelation("index.unused", SeverityWarning, i.Datname, i.Name, "index usage history is shorter than 7 days"))
				continue
			}
			// IdxScan is the peak scan rate over the whole history; the
			// latest rate alone called any index idle for five minutes
			// unused.
			if i.IdxScan == 0 && i.SizeBytes > unusedIndexBytes && !i.IsPrimary && !i.IsUnique {
				out = append(out, relationFinding("index.unused", SeverityWarning, i.Datname, i.Name, "Unused index", fmt.Sprintf("no scan of this index was recorded in %.1f days", i.HistoryDays), "Confirm workload intent, including queries served by standbys, before dropping this candidate", map[string]any{"peak_scan_rate": i.IdxScan, "history_days": i.HistoryDays, "size_bytes": i.SizeBytes}))
			}
		}
		return out
	}})
	Register(relationRule{id: "index.duplicate", severity: SeverityWarning, needs: []string{"Indexes"}, eval: func(s *Snapshot) []Finding {
		// Grouped within one database and one access method: tables of the
		// same name in two databases, or a btree and a GIN index on the
		// same column, are not duplicates.
		groups := map[string][]IndexStat{}
		var keys []string
		for _, i := range s.Indexes {
			if i.DefHash == "" {
				continue
			}
			key := i.Datname + "\x00" + i.TableName + "\x00" + indexMethod(i.IndexDef) + "\x00" + i.DefHash
			if _, seen := groups[key]; !seen {
				keys = append(keys, key)
			}
			groups[key] = append(groups[key], i)
		}
		sort.Strings(keys)
		var out []Finding
		for _, key := range keys {
			g := groups[key]
			if len(g) < 2 {
				continue
			}
			names := make([]string, len(g))
			for j := range g {
				names[j] = g[j].Name
			}
			sort.Strings(names)
			out = append(out, relationFinding("index.duplicate", SeverityWarning, g[0].Datname, g[0].TableName, "Duplicate indexes", fmt.Sprintf("indexes %s share a definition", strings.Join(names, ", ")), "Drop the non-primary, non-unique duplicate after validation", map[string]any{"indexes": names}))
		}
		return out
	}})
	Register(relationRule{id: "index.redundant_prefix", severity: SeverityInfo, needs: []string{"Indexes"}, eval: func(s *Snapshot) []Finding {
		var out []Finding
		for a := range s.Indexes {
			ac, ok := indexColumns(s.Indexes[a].IndexDef)
			if !ok {
				continue
			}
			for b := range s.Indexes {
				if a == b || s.Indexes[a].Datname != s.Indexes[b].Datname || s.Indexes[a].TableName != s.Indexes[b].TableName || s.Indexes[a].IsUnique {
					continue
				}
				// Only a btree serves the queries on a prefix of its
				// columns, and a partial index serves only the rows its
				// predicate selects.
				if method := indexMethod(s.Indexes[b].IndexDef); indexMethod(s.Indexes[a].IndexDef) != method || (method != "" && method != "btree") {
					continue
				}
				if indexPredicate(s.Indexes[a].IndexDef) != indexPredicate(s.Indexes[b].IndexDef) {
					continue
				}
				bc, ok := indexColumns(s.Indexes[b].IndexDef)
				if ok && isPrefix(ac, bc) {
					out = append(out, relationFinding("index.redundant_prefix", SeverityInfo, s.Indexes[a].Datname, s.Indexes[a].Name, "Redundant index prefix", fmt.Sprintf("%s is a prefix of %s", s.Indexes[a].Name, s.Indexes[b].Name), "Review whether the shorter non-unique index can be removed", map[string]any{"prefix": s.Indexes[a].Name, "extended": s.Indexes[b].Name}))
					break
				}
			}
		}
		return out
	}})
	Register(relationRule{id: "index.invalid", severity: SeverityCritical, needs: []string{"Indexes"}, eval: func(s *Snapshot) []Finding {
		var out []Finding
		for _, i := range s.Indexes {
			if !i.IsValid {
				out = append(out, relationFinding("index.invalid", SeverityCritical, i.Datname, i.Name, "Invalid index", "index is not valid and is still maintained on writes", "Rebuild or drop the invalid index", map[string]any{"is_valid": false}))
			}
		}
		return out
	}})
	Register(relationRule{id: "index.bloat_high", severity: SeverityWarning, needs: []string{"Bloat"}, eval: func(s *Snapshot) []Finding {
		return relationBloat(s, "index.bloat_high", SeverityWarning, "index", .4, indexBloatBytes, "Index bloat is high")
	}})
	Register(relationRule{id: "index.divergence", severity: SeverityWarning, needs: []string{"Siblings"}, eval: func(s *Snapshot) []Finding {
		// This instance's index definitions against each sibling's. The
		// rule used to compare the siblings among themselves (never this
		// instance, and nothing at all with a single standby), starting
		// from whichever sibling a map walk returned first.
		own := map[string]string{}
		for key, f := range s.Facts["index_def"] {
			own[key] = f.Labels["def_hash"]
		}
		keys := make([]string, 0, len(own))
		for key := range own {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var out []Finding
		for _, sib := range s.Siblings {
			// A sibling that reported no index definition at all has not
			// been scraped, which is not a divergence.
			if len(sib.IndexHashes) == 0 {
				continue
			}
			for _, key := range keys {
				datname, name, _ := strings.Cut(key, "\x00")
				other, ok := sib.IndexHashes[key]
				switch {
				case !ok:
					out = append(out, relationFinding("index.divergence", SeverityWarning, datname, name+"@"+sib.InstanceID.String(), "Index topology diverges", fmt.Sprintf("index %s is absent on sibling %s", name, sib.InstanceID), "Reconcile index definitions after confirming replication state", map[string]any{"missing_on": sib.InstanceID.String()}))
				case other != own[key]:
					out = append(out, relationFinding("index.divergence", SeverityWarning, datname, name+"@"+sib.InstanceID.String(), "Index topology diverges", fmt.Sprintf("index %s has a different definition on sibling %s", name, sib.InstanceID), "Reconcile index definitions after confirming replication state", map[string]any{"differs_on": sib.InstanceID.String()}))
				}
			}
		}
		return out
	}})
	Register(relationRule{id: "table.dead_tuples_high", severity: SeverityWarning, needs: []string{"Tables"}, eval: func(s *Snapshot) []Finding {
		var out []Finding
		for _, t := range s.Tables {
			r := t.NDeadTup / t.NLiveTup
			if t.NLiveTup > 0 && r > deadTupleRatio && t.NDeadTup >= deadTupleLimit {
				out = append(out, relationFinding("table.dead_tuples_high", SeverityWarning, t.Datname, t.Name, "Dead tuples are high", fmt.Sprintf("dead/live tuple ratio is %.1f%%", r*100), "Investigate autovacuum and vacuum this table", map[string]any{"ratio": r, "dead_tuples": t.NDeadTup}))
			}
		}
		return out
	}})
	Register(relationRule{id: "table.never_autovacuumed", severity: SeverityWarning, needs: []string{"Tables"}, eval: func(s *Snapshot) []Finding {
		// A standby never vacuums: its statistics carry no vacuum
		// timestamp for any table.
		if s.Role == pgtype.RoleStandby {
			return nil
		}
		var out []Finding
		for _, t := range s.Tables {
			if t.NLiveTup > largeTableRows && t.LastAutovacuum == nil && t.LastVacuum == nil {
				out = append(out, relationFinding("table.never_autovacuumed", SeverityWarning, t.Datname, t.Name, "Table never vacuumed", "no vacuum or autovacuum timestamp is available", "Check autovacuum coverage for this table", nil))
			}
		}
		return out
	}})
	Register(relationRule{id: "table.autoanalyze_stale", severity: SeverityInfo, needs: []string{"Tables"}, eval: func(s *Snapshot) []Finding {
		var out []Finding
		for _, t := range s.Tables {
			if t.NLiveTup > 0 && t.NModSinceAnalyze/t.NLiveTup > autoAnalyzeRatio {
				out = append(out, relationFinding("table.autoanalyze_stale", SeverityInfo, t.Datname, t.Name, "Table statistics are stale", "modifications exceed 20% of live tuples", "Analyze the table and review autoanalyze settings", map[string]any{"ratio": t.NModSinceAnalyze / t.NLiveTup}))
			}
		}
		return out
	}})
	Register(relationRule{id: "table.wraparound_risk", severity: SeverityCritical, needs: []string{"Tables"}, eval: func(s *Snapshot) []Finding {
		var out []Finding
		for _, t := range s.Tables {
			if t.RelfrozenXIDAge > wraparoundRelationAge {
				out = append(out, relationFinding("table.wraparound_risk", SeverityCritical, t.Datname, t.Name, "Table wraparound risk", fmt.Sprintf("relfrozenxid age is %.0f", t.RelfrozenXIDAge), "Vacuum this relation immediately", map[string]any{"age": t.RelfrozenXIDAge}))
			}
		}
		return out
	}})
	Register(relationRule{id: "table.bloat_high", severity: SeverityWarning, needs: []string{"Bloat"}, eval: func(s *Snapshot) []Finding {
		return relationBloat(s, "table.bloat_high", SeverityWarning, "table", .3, tableBloatBytes, "Table bloat is high")
	}})
	Register(relationRule{id: "table.seq_scan_heavy", severity: SeverityInfo, needs: []string{"Tables"}, eval: func(s *Snapshot) []Finding {
		var out []Finding
		for _, t := range s.Tables {
			if t.NLiveTup > largeTableRows && t.SeqScanRate > 0 && t.IdxScanRate <= t.SeqScanRate*.1 {
				out = append(out, relationFinding("table.seq_scan_heavy", SeverityInfo, t.Datname, t.Name, "Heavy sequential scans", "this is a missing-index candidate, not a certainty", "Review query predicates and execution plans", map[string]any{"seq_scan_rate": t.SeqScanRate, "idx_scan_rate": t.IdxScanRate}))
			}
		}
		return out
	}})
	Register(relationRule{id: "vacuum.starvation", severity: SeverityCritical, needs: []string{"Tables", "pg_vacuum_jobs_running", "settings"}, eval: func(s *Snapshot) []Finding {
		busy, _ := metricValue(s, "pg_vacuum_jobs_running")
		workers, _ := strconv.ParseFloat(s.Settings["autovacuum_max_workers"], 64)
		count := 0
		for _, t := range s.Tables {
			if t.NLiveTup > 0 && t.NDeadTup/t.NLiveTup > deadTupleRatio {
				count++
			}
		}
		if count > 10 && workers > 0 && busy >= workers {
			return []Finding{relationFinding("vacuum.starvation", SeverityCritical, "", "vacuum", "Vacuum starvation", fmt.Sprintf("%d tables exceed the dead tuple threshold while all workers are busy", count), "Investigate blocked or undersized autovacuum workers", map[string]any{"tables": count, "jobs": busy, "workers": workers})}
		}
		return nil
	}})
	Register(relationRule{id: "vacuum.disabled", severity: SeverityCritical, needs: []string{"settings"}, eval: func(s *Snapshot) []Finding {
		if strings.EqualFold(s.Settings["autovacuum"], "off") {
			return []Finding{relationFinding("vacuum.disabled", SeverityCritical, "", "cluster", "Autovacuum disabled", "autovacuum is off", "Enable autovacuum to prevent table and transaction ID bloat", nil)}
		}
		return nil
	}})
}

// relationBloat evaluates the bloat of one kind of relation. Both bloat
// rules used to walk every row, tables and indexes alike, so a bloated
// index was also reported as a bloated table and the other way round.
func relationBloat(s *Snapshot, id string, sev Severity, kind string, ratio, size float64, title string) []Finding {
	var out []Finding
	for _, b := range s.Bloat {
		if b.Kind == kind && b.Ratio > ratio && b.SizeBytes > size {
			out = append(out, relationFinding(id, sev, b.Datname, b.Name, title, fmt.Sprintf("bloat ratio is %.1f%%", b.Ratio*100), "Schedule maintenance after confirming workload impact", map[string]any{"bloat_ratio": b.Ratio, "size_bytes": b.SizeBytes, "method": b.Method}))
		}
	}
	return out
}
