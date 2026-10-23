package advisor

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// The catalog checks (triggers, sequences, partitions, foreign keys,
// extensions, PostGIS columns) report one fact per object of each database.
// A complete result replaces the database's facts of its kind on the
// server; a truncated one leaves the facts it did not reach in place, so
// rules read only each database's latest batch.
const (
	// catalogBatchSlack is how far before a database's newest fact of a
	// kind a fact may be and still belong to the same scrape.
	catalogBatchSlack = time.Minute
	// catalogFresh is how old a database's newest fact may be. The catalog
	// checks run every five minutes to every hour; a database whose facts
	// stopped being refreshed (dropped, no longer scheduled) says nothing
	// about its objects any more.
	catalogFresh = 3 * time.Hour
)

// currentFacts returns the facts of a kind from each database's latest
// scrape, sorted by database and key.
func currentFacts(s *Snapshot, kind string) []Fact {
	newest := map[string]time.Time{}
	for _, f := range s.Facts[kind] {
		if f.ObservedAt.After(newest[f.Datname]) {
			newest[f.Datname] = f.ObservedAt
		}
	}
	var facts []Fact
	for _, f := range s.Facts[kind] {
		latest := newest[f.Datname]
		if f.ObservedAt.Before(latest.Add(-catalogBatchSlack)) {
			continue
		}
		if !s.Now.IsZero() && s.Now.Sub(latest) > catalogFresh {
			continue
		}
		facts = append(facts, f)
	}
	sort.Slice(facts, func(i, j int) bool {
		if facts[i].Datname != facts[j].Datname {
			return facts[i].Datname < facts[j].Datname
		}
		return facts[i].Key < facts[j].Key
	})
	return facts
}

const triggerFactKind = "trigger"

// databaseRule is a catalog rule whose findings name a database, not one of
// its relations.
type databaseRule struct{ relationRule }

func (r databaseRule) Scope() Scope { return ScopeDatabase }

// catalogTable returns a fact's table as shown (schema.table) and as SQL
// (each part quoted).
func catalogTable(f Fact) (shown, sql string) {
	schema, table := f.Labels["schemaname"], f.Labels["relname"]
	return schema + "." + table, pgx.Identifier{schema, table}.Sanitize()
}

func init() {
	Register(relationRule{id: "trigger.constraint_disabled", severity: SeverityCritical, needs: []string{"trigger_inventory"}, eval: func(s *Snapshot) []Finding {
		const id = "trigger.constraint_disabled"
		type constraint struct {
			datname, table, tableSQL, name, kind string
			triggers                             []string
		}
		byConstraint := map[string]*constraint{}
		var order []string
		for _, f := range currentFacts(s, triggerFactKind) {
			if f.Labels["kind"] != "internal" || f.ValueText != "disabled" {
				continue
			}
			table, tableSQL := catalogTable(f)
			key := f.Datname + "\x00" + table + "\x00" + f.Labels["constraint"]
			c, ok := byConstraint[key]
			if !ok {
				c = &constraint{datname: f.Datname, table: table, tableSQL: tableSQL, name: f.Labels["constraint"], kind: f.Labels["constraint_type"]}
				byConstraint[key] = c
				order = append(order, key)
			}
			c.triggers = append(c.triggers, f.Labels["trigger"])
		}
		var out []Finding
		for _, key := range order {
			c := byConstraint[key]
			kind := c.kind
			if kind == "" {
				kind = "constraint"
			}
			name := c.name
			if name == "" {
				name = strings.Join(c.triggers, ", ")
			}
			out = append(out, relationFinding(id, SeverityCritical, c.datname, c.table+"."+name,
				fmt.Sprintf("The %s %s is not enforced", kind, name),
				fmt.Sprintf("The internal triggers that enforce %s %s on %s in database %s are disabled (%s), as ALTER TABLE ... DISABLE TRIGGER ALL leaves them. Rows that violate the constraint can be written while they stay disabled.", kind, name, c.table, c.datname, strings.Join(c.triggers, ", ")),
				fmt.Sprintf("Run ALTER TABLE %s ENABLE TRIGGER ALL, then look for rows written meanwhile that violate %s: enabling the triggers does not check existing rows.", c.tableSQL, name),
				map[string]any{"constraint": c.name, "constraint_type": c.kind, "table": c.table, "triggers": c.triggers}))
		}
		return out
	}})
	Register(relationRule{id: "trigger.user_disabled", severity: SeverityWarning, needs: []string{"trigger_inventory"}, eval: func(s *Snapshot) []Finding {
		const id = "trigger.user_disabled"
		var out []Finding
		for _, f := range currentFacts(s, triggerFactKind) {
			if f.Labels["kind"] != "user" || f.ValueText != "disabled" {
				continue
			}
			table, tableSQL := catalogTable(f)
			trigger := f.Labels["trigger"]
			out = append(out, relationFinding(id, SeverityWarning, f.Datname, table+"."+trigger,
				fmt.Sprintf("Trigger %s on %s is disabled", trigger, table),
				fmt.Sprintf("Trigger %s (%s %s, %s, function %s) on %s in database %s is disabled: writes to the table no longer run it.", trigger, f.Labels["timing"], f.Labels["events"], f.Labels["level"], f.Labels["function"], table, f.Datname),
				fmt.Sprintf("If the trigger should run, ALTER TABLE %s ENABLE TRIGGER %s. If it is retired, drop it so its state is not mistaken for an accident.", tableSQL, pgx.Identifier{trigger}.Sanitize()),
				map[string]any{"table": table, "trigger": trigger, "function": f.Labels["function"]}))
		}
		return out
	}})
}

const fkUnindexedFactKind = "fk_unindexed"

// fkUnindexedWarnBytes is the referencing-table size from which an unindexed
// foreign key is a warning: below it a scan per referenced row stays cheap.
const fkUnindexedWarnBytes = 10 << 20

func init() {
	Register(relationRule{id: "index.fk_unindexed", severity: SeverityWarning, needs: []string{"fk_indexes"}, eval: func(s *Snapshot) []Finding {
		const id = "index.fk_unindexed"
		var out []Finding
		for _, f := range currentFacts(s, fkUnindexedFactKind) {
			table, tableSQL := catalogTable(f)
			constraint := f.Labels["constraint"]
			bytes, _ := strconv.ParseInt(f.Labels["table_bytes"], 10, 64)
			severity := SeverityInfo
			if bytes >= fkUnindexedWarnBytes {
				severity = SeverityWarning
			}
			columnsSQL := f.Labels["columns_sql"]
			if columnsSQL == "" {
				columnsSQL = f.ValueText
			}
			out = append(out, relationFinding(id, severity, f.Datname, table+"."+constraint,
				fmt.Sprintf("Foreign key %s on %s has no index", constraint, table),
				fmt.Sprintf("No index on %s in database %s leads with (%s), the columns of foreign key %s referencing %s. Every DELETE of a row of %s, and every UPDATE of its key, scans %s (%s) for referencing rows while holding its locks.", table, f.Datname, f.ValueText, constraint, f.Labels["referenced"], f.Labels["referenced"], table, formatCatalogBytes(bytes)),
				fmt.Sprintf("If rows of %s are deleted or their keys updated, CREATE INDEX CONCURRENTLY ON %s (%s). If they never are, the index is not needed.", f.Labels["referenced"], tableSQL, columnsSQL),
				map[string]any{"table": table, "constraint": constraint, "columns": f.ValueText, "referenced": f.Labels["referenced"], "table_bytes": bytes}))
		}
		return out
	}})
}

// formatCatalogBytes renders a size in binary units.
func formatCatalogBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value, exp := float64(n)/unit, 0
	for value >= unit && exp < 4 {
		value /= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", value, "KMGTP"[exp])
}

const (
	extensionFactKind     = "extension"
	spatialColumnFactKind = "spatial_column"
	// spatialRowsWarn is the table size, in estimated rows, from which an
	// unindexed spatial column is a warning.
	spatialRowsWarn = 10000
)

// postgisExtensions are the extensions PostGIS ships and updates together
// with postgis_extensions_upgrade().
var postgisExtensions = map[string]bool{
	"postgis": true, "postgis_raster": true, "postgis_topology": true, "postgis_sfcgal": true,
	"postgis_tiger_geocoder": true, "address_standardizer": true, "address_standardizer_data_us": true,
}

func init() {
	Register(databaseRule{relationRule{id: "extension.upgrade_available", severity: SeverityInfo, needs: []string{"extensions"}, eval: func(s *Snapshot) []Finding {
		const id = "extension.upgrade_available"
		var out []Finding
		for _, f := range currentFacts(s, extensionFactKind) {
			if f.Labels["upgrade"] != "available" || postgisExtensions[f.Key] {
				continue
			}
			out = append(out, Finding{RuleID: id, Severity: SeverityInfo, State: StateOpen, Scope: ScopeDatabase, Datname: f.Datname, ObjectName: f.Key,
				Title:       fmt.Sprintf("Extension %s can be updated to %s", f.Key, f.Labels["default_version"]),
				Detail:      fmt.Sprintf("Database %s runs extension %s %s; the installed packages carry %s. Until the extension is updated, the database keeps the objects of the older version.", f.Datname, f.Key, f.ValueText, f.Labels["default_version"]),
				Remediation: fmt.Sprintf("Read the extension's release notes, then, connected to %s as its owner, ALTER EXTENSION %s UPDATE.", f.Datname, pgx.Identifier{f.Key}.Sanitize()),
				Evidence:    map[string]any{"extension": f.Key, "installed_version": f.ValueText, "default_version": f.Labels["default_version"]},
			})
		}
		return out
	}}})
	Register(databaseRule{relationRule{id: "postgis.upgrade_available", severity: SeverityWarning, needs: []string{"extensions"}, eval: func(s *Snapshot) []Finding {
		const id = "postgis.upgrade_available"
		type pending struct {
			datname  string
			versions []string
		}
		byDatabase := map[string]*pending{}
		var order []string
		for _, f := range currentFacts(s, extensionFactKind) {
			if f.Labels["upgrade"] != "available" || !postgisExtensions[f.Key] {
				continue
			}
			p, ok := byDatabase[f.Datname]
			if !ok {
				p = &pending{datname: f.Datname}
				byDatabase[f.Datname] = p
				order = append(order, f.Datname)
			}
			p.versions = append(p.versions, fmt.Sprintf("%s %s → %s", f.Key, f.ValueText, f.Labels["default_version"]))
		}
		var out []Finding
		for _, datname := range order {
			p := byDatabase[datname]
			out = append(out, Finding{RuleID: id, Severity: SeverityWarning, State: StateOpen, Scope: ScopeDatabase, Datname: datname, ObjectName: "postgis",
				Title:       fmt.Sprintf("PostGIS in database %s is older than its library", datname),
				Detail:      fmt.Sprintf("The PostGIS packages were updated but the extensions of database %s were not (%s). Its SQL functions still come from the older release while the library is the newer one.", datname, strings.Join(p.versions, ", ")),
				Remediation: fmt.Sprintf("Connected to %s as a superuser, SELECT postgis_extensions_upgrade(); after reading the PostGIS upgrade notes.", datname),
				Evidence:    map[string]any{"extensions": p.versions},
			})
		}
		return out
	}}})
	Register(relationRule{id: "postgis.spatial_unindexed", severity: SeverityWarning, needs: []string{"postgis_columns"}, eval: func(s *Snapshot) []Finding {
		const id = "postgis.spatial_unindexed"
		var out []Finding
		for _, f := range currentFacts(s, spatialColumnFactKind) {
			if f.Labels["index_method"] != "" {
				continue
			}
			table, tableSQL := catalogTable(f)
			column := f.Labels["column"]
			rows, _ := strconv.ParseFloat(f.Labels["rows"], 64)
			severity := SeverityInfo
			if rows > spatialRowsWarn {
				severity = SeverityWarning
			}
			out = append(out, relationFinding(id, severity, f.Datname, table+"."+column,
				fmt.Sprintf("Spatial column %s.%s has no spatial index", table, column),
				fmt.Sprintf("No valid GiST, SP-GiST or BRIN index on %s in database %s starts with %s column %s (about %.0f rows). Bounding-box searches (&&, ST_Intersects, ST_DWithin) on it read the whole table.", table, f.Datname, f.ValueText, column, rows),
				fmt.Sprintf("If the column is searched spatially, CREATE INDEX CONCURRENTLY ON %s USING gist (%s).", tableSQL, pgx.Identifier{column}.Sanitize()),
				map[string]any{"table": table, "column": column, "type": f.ValueText, "rows": rows}))
		}
		return out
	}})
	Register(relationRule{id: "postgis.srid_unknown", severity: SeverityWarning, needs: []string{"postgis_columns"}, eval: func(s *Snapshot) []Finding {
		const id = "postgis.srid_unknown"
		var out []Finding
		for _, f := range currentFacts(s, spatialColumnFactKind) {
			if f.Labels["srid_known"] != "false" {
				continue
			}
			table, _ := catalogTable(f)
			column := f.Labels["column"]
			reason := fmt.Sprintf("is declared with SRID %s, which spatial_ref_sys does not define", f.Labels["srid"])
			if f.Labels["srid"] == "0" {
				reason = "is declared without an SRID (0)"
			}
			out = append(out, relationFinding(id, SeverityWarning, f.Datname, table+"."+column,
				fmt.Sprintf("Spatial column %s.%s has no known coordinate system", table, column),
				fmt.Sprintf("Column %s of %s in database %s (%s(%s)) %s, so PostGIS cannot transform its values or measure them in real units.", column, table, f.Datname, f.ValueText, f.Labels["subtype"], reason),
				"Find the coordinate system the data was captured in and declare it: ALTER TABLE ... ALTER COLUMN ... TYPE "+f.ValueText+"("+f.Labels["subtype"]+", <srid>) USING ST_SetSRID(column, <srid>), or add the missing definition to spatial_ref_sys.",
				map[string]any{"table": table, "column": column, "srid": f.Labels["srid"]}))
		}
		return out
	}})
	Register(databaseRule{relationRule{id: "postgis.srid_mixed", severity: SeverityInfo, needs: []string{"postgis_columns"}, eval: func(s *Snapshot) []Finding {
		const id = "postgis.srid_mixed"
		sridsByDatabase := map[string]map[string]int{}
		var order []string
		for _, f := range currentFacts(s, spatialColumnFactKind) {
			srid := f.Labels["srid"]
			if f.ValueText != "geometry" || srid == "" || srid == "0" {
				continue
			}
			if sridsByDatabase[f.Datname] == nil {
				sridsByDatabase[f.Datname] = map[string]int{}
				order = append(order, f.Datname)
			}
			sridsByDatabase[f.Datname][srid]++
		}
		var out []Finding
		for _, datname := range order {
			counts := sridsByDatabase[datname]
			if len(counts) < 2 {
				continue
			}
			srids := make([]string, 0, len(counts))
			for srid := range counts {
				srids = append(srids, srid)
			}
			sort.Strings(srids)
			parts := make([]string, len(srids))
			for i, srid := range srids {
				parts[i] = fmt.Sprintf("SRID %s (%s)", srid, plural(counts[srid], "column"))
			}
			out = append(out, Finding{RuleID: id, Severity: SeverityInfo, State: StateOpen, Scope: ScopeDatabase, Datname: datname, ObjectName: "postgis",
				Title:       fmt.Sprintf("Geometry columns in database %s use %d coordinate systems", datname, len(srids)),
				Detail:      fmt.Sprintf("The geometry columns of database %s are declared in %s. A spatial join or comparison between columns of different SRIDs fails, or needs ST_Transform on every row.", datname, strings.Join(parts, ", ")),
				Remediation: "If the columns are compared with each other, store them in one SRID, or keep a transformed, indexed copy of the ones that are joined.",
				Evidence:    map[string]any{"srids": counts},
			})
		}
		return out
	}}})
}

// plural renders a count with its noun, "1 column" or "2 columns".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
