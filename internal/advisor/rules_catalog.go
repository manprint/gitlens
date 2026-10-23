package advisor

import (
	"fmt"
	"math"
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

const (
	sequenceFactKind = "sequence"
	// sequenceWarnRatio and sequenceCriticalRatio are the used fractions of
	// a sequence's range from which its exhaustion is a warning and critical.
	sequenceWarnRatio     = 0.75
	sequenceCriticalRatio = 0.90
)

// sequenceName returns a sequence fact's name as shown and as SQL.
func sequenceName(f Fact) (shown, sql string) {
	schema, name := f.Labels["schemaname"], f.Labels["sequence"]
	return schema + "." + name, pgx.Identifier{schema, name}.Sanitize()
}

// sequenceColumn returns the column a sequence fact feeds as shown, the SQL
// that widens it to bigint, and whether there is one.
func sequenceColumn(f Fact) (shown, widen string, ok bool) {
	if f.Labels["column"] == "" {
		return "", "", false
	}
	schema, table, column := f.Labels["table_schema"], f.Labels["table"], f.Labels["column"]
	shown = fmt.Sprintf("%s.%s.%s (%s)", schema, table, column, f.Labels["column_type"])
	widen = fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE bigint", pgx.Identifier{schema, table}.Sanitize(), pgx.Identifier{column}.Sanitize())
	return shown, widen, true
}

// integerTypeBound is the largest value of the integer type a sequence or
// column is declared with, by its format_type name.
var integerTypeBound = map[string]string{
	"smallint": strconv.Itoa(math.MaxInt16), "integer": strconv.Itoa(math.MaxInt32), "bigint": strconv.FormatInt(math.MaxInt64, 10),
}

// sequenceRemediation says how to raise the limit that binds a sequence:
// widening the column rewrites its table, while changing the sequence is a
// catalog update.
func sequenceRemediation(f Fact) string {
	_, seqSQL := sequenceName(f)
	column, widen, linked := sequenceColumn(f)
	const rewrite = " This rewrites the table under an ACCESS EXCLUSIVE lock, and the columns that reference it through foreign keys need the same change; on a large table, add a bigint column, fill it in batches and swap it in instead."
	ascending := !strings.HasPrefix(f.Labels["increment"], "-")
	if f.Labels["limit_source"] == "column" || (linked && f.Labels["limit"] == integerTypeBound[f.Labels["column_type"]] && f.Labels["column_type"] != "bigint") {
		text := fmt.Sprintf("The type of column %s sets the limit: %s.", column, widen) + rewrite
		if f.Labels["data_type"] != "bigint" {
			text += fmt.Sprintf(" Then ALTER SEQUENCE %s AS bigint.", seqSQL)
		}
		return text
	}
	if f.Labels["data_type"] != "bigint" {
		return fmt.Sprintf("The sequence's own type sets the limit. ALTER SEQUENCE %s AS bigint raises it without rewriting any table.", seqSQL)
	}
	if ascending {
		return fmt.Sprintf("The sequence's MAXVALUE sets the limit. ALTER SEQUENCE %s NO MAXVALUE raises it to the bigint limit without rewriting any table.", seqSQL)
	}
	return fmt.Sprintf("The sequence's MINVALUE sets the limit. ALTER SEQUENCE %s NO MINVALUE lowers it to the bigint limit without rewriting any table.", seqSQL)
}

func init() {
	Register(relationRule{id: "sequence.exhaustion_risk", severity: SeverityCritical, needs: []string{"sequences"}, eval: func(s *Snapshot) []Finding {
		const id = "sequence.exhaustion_risk"
		var out []Finding
		for _, f := range currentFacts(s, sequenceFactKind) {
			ratio, err := strconv.ParseFloat(f.Labels["used_ratio"], 64)
			if err != nil || f.Labels["exhausts"] != "true" || ratio < sequenceWarnRatio {
				continue
			}
			severity := SeverityWarning
			if ratio >= sequenceCriticalRatio {
				severity = SeverityCritical
			}
			name, _ := sequenceName(f)
			target := "inserts that draw from it"
			if column, _, ok := sequenceColumn(f); ok {
				target = "inserts into " + column
			}
			out = append(out, relationFinding(id, severity, f.Datname, name,
				fmt.Sprintf("Sequence %s has used %.0f%% of its range", name, ratio*100),
				fmt.Sprintf("Sequence %s in database %s (%s, increment %s) is at %s; %s values remain before %s. Once they run out, %s fail.", name, f.Datname, f.Labels["data_type"], f.Labels["increment"], f.Labels["last_value"], f.Labels["remaining"], f.Labels["limit"], target),
				sequenceRemediation(f),
				map[string]any{"sequence": name, "last_value": f.Labels["last_value"], "limit": f.Labels["limit"], "limit_source": f.Labels["limit_source"], "used_ratio": ratio, "remaining": f.Labels["remaining"]}))
		}
		return out
	}})
	Register(relationRule{id: "sequence.column_narrower", severity: SeverityWarning, needs: []string{"sequences"}, eval: func(s *Snapshot) []Finding {
		const id = "sequence.column_narrower"
		var out []Finding
		for _, f := range currentFacts(s, sequenceFactKind) {
			if f.Labels["limit_source"] != "column" {
				continue
			}
			name, seqSQL := sequenceName(f)
			column, widen, _ := sequenceColumn(f)
			out = append(out, relationFinding(id, SeverityWarning, f.Datname, name,
				fmt.Sprintf("Column %s overflows before sequence %s runs out", column, name),
				fmt.Sprintf("Sequence %s in database %s is %s (bounds %s to %s) but feeds column %s: inserts fail once it passes %s, whatever its own limit says.", name, f.Datname, f.Labels["data_type"], f.Labels["min"], f.Labels["max"], column, f.Labels["limit"]),
				fmt.Sprintf("%s (this rewrites the table under an ACCESS EXCLUSIVE lock), or, if the column is meant to stay as it is, give the sequence the column's bound (ALTER SEQUENCE %s AS %s) so the limit is stated where it applies.", widen, seqSQL, f.Labels["column_type"]),
				map[string]any{"sequence": name, "data_type": f.Labels["data_type"], "column": column, "limit": f.Labels["limit"]}))
		}
		return out
	}})
	Register(databaseRule{relationRule{id: "sequence.value_hidden", severity: SeverityInfo, needs: []string{"sequences"}, eval: func(s *Snapshot) []Finding {
		const id = "sequence.value_hidden"
		hidden := map[string][]string{}
		var order []string
		for _, f := range currentFacts(s, sequenceFactKind) {
			if f.ValueText != "hidden" {
				continue
			}
			if hidden[f.Datname] == nil {
				order = append(order, f.Datname)
			}
			name, _ := sequenceName(f)
			hidden[f.Datname] = append(hidden[f.Datname], name)
		}
		var out []Finding
		for _, datname := range order {
			names := hidden[datname]
			examples := names[:min(len(names), 5)]
			out = append(out, Finding{RuleID: id, Severity: SeverityInfo, State: StateOpen, Scope: ScopeDatabase, Datname: datname, ObjectName: "sequences",
				Title:       fmt.Sprintf("pglens cannot read the values of %s in database %s", plural(len(names), "sequence"), datname),
				Detail:      fmt.Sprintf("The monitoring role has neither SELECT nor USAGE on %s of database %s (%s), so how close they are to their limit is unknown; only a column narrower than its sequence is still detected.", plural(len(names), "sequence"), datname, strings.Join(examples, ", ")),
				Remediation: "Run the agent at tier 1 (pg_read_all_data), or GRANT SELECT ON SEQUENCE ... TO the monitoring role for the sequences to watch. Never grant USAGE: it allows nextval(), which advances the sequence.",
				Evidence:    map[string]any{"hidden": len(names), "examples": examples},
			})
		}
		return out
	}}})
}

const (
	partitionedTableFactKind = "partitioned_table"
	// partitionHorizon is how far ahead a time-range partitioned table's
	// partitions should reach.
	partitionHorizon = 7 * 24 * time.Hour
	// partitionLapsedFor is how long after its partitions ran out a table
	// is still reported: one that ran out long ago is more likely retired
	// than forgotten, and rows still arriving show in its default partition.
	partitionLapsedFor = 30 * 24 * time.Hour
	// partitionAnalyzeRows is the size, in estimated rows, from which a
	// partitioned table that was never analyzed is reported.
	partitionAnalyzeRows = 10000
)

func init() {
	Register(relationRule{id: "partition.default_has_rows", severity: SeverityWarning, needs: []string{"partitions"}, eval: func(s *Snapshot) []Finding {
		const id = "partition.default_has_rows"
		var out []Finding
		for _, f := range currentFacts(s, partitionedTableFactKind) {
			rows, err := strconv.ParseFloat(f.Labels["default_rows"], 64)
			if err != nil || rows <= 0 {
				continue
			}
			table, tableSQL := catalogTable(f)
			def := f.Labels["default_partition"]
			defSQL := pgx.Identifier{f.Labels["default_schema"], f.Labels["default_relname"]}.Sanitize()
			out = append(out, relationFinding(id, SeverityWarning, f.Datname, table,
				fmt.Sprintf("Default partition %s holds about %s rows", def, formatCatalogCount(rows)),
				fmt.Sprintf("Rows of %s in database %s that match no partition land in its default partition %s (about %s rows). Creating a partition for a range that has rows there fails until they are moved, and attaching any partition scans the default partition.", table, f.Datname, def, formatCatalogCount(rows)),
				fmt.Sprintf("Create the partitions those rows belong to: in one transaction, ALTER TABLE %s DETACH PARTITION %s, create the missing partitions, move the rows with INSERT INTO %s SELECT ... and DELETE, then ATTACH PARTITION %s DEFAULT.", tableSQL, defSQL, tableSQL, defSQL),
				map[string]any{"table": table, "default_partition": def, "rows": rows}))
		}
		return out
	}})
	Register(relationRule{id: "partition.future_missing", severity: SeverityCritical, needs: []string{"partitions"}, eval: func(s *Snapshot) []Finding {
		const id = "partition.future_missing"
		if s.Now.IsZero() {
			return nil
		}
		var out []Finding
		for _, f := range currentFacts(s, partitionedTableFactKind) {
			until, err := time.Parse(time.RFC3339, f.Labels["covered_until"])
			if err != nil {
				continue
			}
			ahead := until.Sub(s.Now)
			if ahead >= partitionHorizon || ahead < -partitionLapsedFor {
				continue
			}
			table, tableSQL := catalogTable(f)
			hasDefault := f.Labels["default_partition"] != ""
			severity, title := SeverityWarning, fmt.Sprintf("Partitions of %s end in %s", table, formatCatalogDuration(ahead))
			consequence := "rows past that point will fail to insert"
			if hasDefault {
				consequence = "rows past that point will land in the default partition " + f.Labels["default_partition"]
			}
			if ahead <= 0 {
				title = fmt.Sprintf("Partitions of %s ended %s ago", table, formatCatalogDuration(-ahead))
				if hasDefault {
					consequence = "rows since then land in the default partition " + f.Labels["default_partition"]
				} else {
					severity, consequence = SeverityCritical, "inserts of rows since then fail"
				}
			}
			out = append(out, relationFinding(id, severity, f.Datname, table, title,
				fmt.Sprintf("%s in database %s is partitioned by range (%s), and its partitions reach %s: %s.", table, f.Datname, f.Labels["partition_key"], until.UTC().Format("2006-01-02 15:04 MST"), consequence),
				fmt.Sprintf("Create the next partitions ahead of time (CREATE TABLE ... PARTITION OF %s FOR VALUES FROM (...) TO (...)), and schedule their creation, with pg_partman or a cron job, so the table never runs out.", tableSQL),
				map[string]any{"table": table, "covered_until": f.Labels["covered_until"], "default_partition": f.Labels["default_partition"]}))
		}
		return out
	}})
	Register(relationRule{id: "partition.index_invalid", severity: SeverityWarning, needs: []string{"partitions"}, eval: func(s *Snapshot) []Finding {
		const id = "partition.index_invalid"
		var out []Finding
		for _, f := range currentFacts(s, partitionedTableFactKind) {
			if f.Labels["invalid_indexes"] == "" {
				continue
			}
			indexes := strings.Split(f.Labels["invalid_indexes"], ",")
			table, _ := catalogTable(f)
			unique := ""
			if f.Labels["invalid_unique_indexes"] != "" && f.Labels["invalid_unique_indexes"] != "0" {
				unique = " A unique index among them does not enforce uniqueness on the partitions that lack it."
			}
			noun, verb := "index", "is"
			if len(indexes) > 1 {
				noun, verb = "indexes", "are"
			}
			out = append(out, relationFinding(id, SeverityWarning, f.Datname, table,
				fmt.Sprintf("%d partitioned %s of %s %s not valid", len(indexes), noun, table, verb),
				fmt.Sprintf("Partitioned %s %s of %s in database %s %s not valid: some partitions have no matching index attached, so queries on them cannot use it.%s", noun, strings.Join(indexes, ", "), table, f.Datname, verb, unique),
				"For each partition without the index, CREATE INDEX CONCURRENTLY on the partition, then ALTER INDEX <partitioned index> ATTACH PARTITION <new index>. The partitioned index becomes valid once every partition has one attached.",
				map[string]any{"table": table, "indexes": indexes}))
		}
		return out
	}})
	Register(relationRule{id: "partition.parent_not_analyzed", severity: SeverityInfo, needs: []string{"partitions"}, eval: func(s *Snapshot) []Finding {
		const id = "partition.parent_not_analyzed"
		var out []Finding
		for _, f := range currentFacts(s, partitionedTableFactKind) {
			rows, _ := strconv.ParseFloat(f.Labels["rows"], 64)
			if f.Labels["analyzed_at"] != "" || rows < partitionAnalyzeRows {
				continue
			}
			table, tableSQL := catalogTable(f)
			out = append(out, relationFinding(id, SeverityInfo, f.Datname, table,
				fmt.Sprintf("Partitioned table %s has never been analyzed", table),
				fmt.Sprintf("%s in database %s holds about %s rows in %s partitions, but has no statistics of its own: autovacuum analyzes partitions, never a partitioned table. The planner uses the table's statistics to estimate joins and queries that span partitions.", table, f.Datname, formatCatalogCount(rows), f.Labels["partitions"]),
				fmt.Sprintf("ANALYZE %s; and again after large changes in its data, for example from a scheduled job.", tableSQL),
				map[string]any{"table": table, "rows": rows}))
		}
		return out
	}})
}

// formatCatalogCount renders an estimated row count with thousands
// separators.
func formatCatalogCount(n float64) string {
	digits := strconv.FormatFloat(n, 'f', 0, 64)
	var b strings.Builder
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 && digits[i-1] != '-' {
			b.WriteByte(',')
		}
		b.WriteRune(d)
	}
	return b.String()
}

// formatCatalogDuration renders a duration in whole days, or hours under a
// day.
func formatCatalogDuration(d time.Duration) string {
	if d < 24*time.Hour {
		return plural(int(d.Hours()), "hour")
	}
	return plural(int(d.Hours()/24), "day")
}
