package advisor

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/i18n"
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
			out = append(out, relationFinding(id, SeverityCritical, c.datname, c.table+"."+name, "", "", "",
				map[string]any{"constraint": c.name, "constraint_type": c.kind, "table": c.table, "triggers": c.triggers}).
				say(id, i18n.Args{"kindKey": constraintKindKey(kind), "kind": kind, "name": name, "table": c.table, "datname": c.datname, "triggers": strings.Join(c.triggers, ", "), "tableSQL": c.tableSQL}))
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
			out = append(out, relationFinding(id, SeverityWarning, f.Datname, table+"."+trigger, "", "", "",
				map[string]any{"table": table, "trigger": trigger, "function": f.Labels["function"]}).
				say(id, i18n.Args{"trigger": trigger, "table": table, "timing": f.Labels["timing"], "events": f.Labels["events"], "level": f.Labels["level"], "function": f.Labels["function"], "datname": f.Datname, "tableSQL": tableSQL, "triggerSQL": pgx.Identifier{trigger}.Sanitize()}))
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
			out = append(out, relationFinding(id, severity, f.Datname, table+"."+constraint, "", "", "",
				map[string]any{"table": table, "constraint": constraint, "columns": f.ValueText, "referenced": f.Labels["referenced"], "table_bytes": bytes}).
				say(id, i18n.Args{"constraint": constraint, "table": table, "datname": f.Datname, "columns": f.ValueText, "referenced": f.Labels["referenced"], "size": formatCatalogBytes(bytes), "tableSQL": tableSQL, "columnsSQL": columnsSQL}))
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
				Evidence: map[string]any{"extension": f.Key, "installed_version": f.ValueText, "default_version": f.Labels["default_version"]},
			}.say(id, i18n.Args{"name": f.Key, "installed": f.ValueText, "version": f.Labels["default_version"], "datname": f.Datname, "nameSQL": pgx.Identifier{f.Key}.Sanitize()}))
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
				Evidence: map[string]any{"extensions": p.versions},
			}.say(id, i18n.Args{"datname": datname, "versions": strings.Join(p.versions, ", ")}))
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
			out = append(out, relationFinding(id, severity, f.Datname, table+"."+column, "", "", "",
				map[string]any{"table": table, "column": column, "type": f.ValueText, "rows": rows}).
				say(id, i18n.Args{"table": table, "column": column, "datname": f.Datname, "type": f.ValueText, "rows": strconv.FormatFloat(rows, 'f', 0, 64), "tableSQL": tableSQL, "columnSQL": pgx.Identifier{column}.Sanitize()}))
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
			out = append(out, relationFinding(id, SeverityWarning, f.Datname, table+"."+column, "", "", "",
				map[string]any{"table": table, "column": column, "srid": f.Labels["srid"]}).
				say(id, i18n.Args{"table": table, "column": column, "datname": f.Datname, "type": f.ValueText, "subtype": f.Labels["subtype"], "noSRID": f.Labels["srid"] == "0", "srid": f.Labels["srid"]}))
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
				Evidence: map[string]any{"srids": counts},
			}.say(id, i18n.Args{"datname": datname, "count": len(srids), "parts": strings.Join(parts, ", ")}))
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
	return i18n.T(i18n.Default, messagePrefix+"sequence.exhaustion_risk.remediation", sequenceRemediationArgs(f))
}

// sequenceRemediationArgs gives the arguments of that remediation; limitBy
// picks its branch: the column, the sequence's type, or its MAXVALUE or
// MINVALUE.
func sequenceRemediationArgs(f Fact) i18n.Args {
	_, seqSQL := sequenceName(f)
	column, widen, linked := sequenceColumn(f)
	limitBy := "minvalue"
	switch {
	case f.Labels["limit_source"] == "column" || (linked && f.Labels["limit"] == integerTypeBound[f.Labels["column_type"]] && f.Labels["column_type"] != "bigint"):
		limitBy = "column"
	case f.Labels["data_type"] != "bigint":
		limitBy = "type"
	case !strings.HasPrefix(f.Labels["increment"], "-"):
		limitBy = "maxvalue"
	}
	return i18n.Args{"limitBy": limitBy, "column": column, "widen": widen, "bigintFix": f.Labels["data_type"] != "bigint", "seqSQL": seqSQL}
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
			_, _, linked := sequenceColumn(f)
			args := sequenceRemediationArgs(f)
			args["name"] = name
			args["datname"] = f.Datname
			args["used"] = strconv.FormatFloat(ratio*100, 'f', 0, 64)
			args["dataType"] = f.Labels["data_type"]
			args["increment"] = f.Labels["increment"]
			args["lastValue"] = f.Labels["last_value"]
			args["remaining"] = f.Labels["remaining"]
			args["limit"] = f.Labels["limit"]
			args["linked"] = linked
			out = append(out, relationFinding(id, severity, f.Datname, name, "", "", "",
				map[string]any{"sequence": name, "last_value": f.Labels["last_value"], "limit": f.Labels["limit"], "limit_source": f.Labels["limit_source"], "used_ratio": ratio, "remaining": f.Labels["remaining"]}).
				say(id, args))
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
			out = append(out, relationFinding(id, SeverityWarning, f.Datname, name, "", "", "",
				map[string]any{"sequence": name, "data_type": f.Labels["data_type"], "column": column, "limit": f.Labels["limit"]}).
				say(id, i18n.Args{"name": name, "datname": f.Datname, "dataType": f.Labels["data_type"], "min": f.Labels["min"], "max": f.Labels["max"], "column": column, "limit": f.Labels["limit"], "widen": widen, "seqSQL": seqSQL, "columnType": f.Labels["column_type"]}))
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
				Evidence: map[string]any{"hidden": len(names), "examples": examples},
			}.say(id, i18n.Args{"count": len(names), "datname": datname, "examples": strings.Join(examples, ", ")}))
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
			out = append(out, relationFinding(id, SeverityWarning, f.Datname, table, "", "", "",
				map[string]any{"table": table, "default_partition": def, "rows": rows}).
				say(id, i18n.Args{"table": table, "datname": f.Datname, "defaultPartition": def, "rows": formatCatalogCount(rows), "tableSQL": tableSQL, "defaultSQL": defSQL}))
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
			def := f.Labels["default_partition"]
			lapsed := ahead <= 0
			span := ahead
			if lapsed {
				span = -ahead
			}
			unit, count := durationParts(span)
			severity, consequence := SeverityWarning, "fail_future"
			switch {
			case lapsed && def == "":
				severity, consequence = SeverityCritical, "fail_past"
			case lapsed:
				consequence = "default_past"
			case def != "":
				consequence = "default_future"
			}
			out = append(out, relationFinding(id, severity, f.Datname, table, "", "", "",
				map[string]any{"table": table, "covered_until": f.Labels["covered_until"], "default_partition": f.Labels["default_partition"]}).
				say(id, i18n.Args{"lapsed": lapsed, "table": table, "durationUnit": unit, "durationCount": count, "datname": f.Datname, "partitionKey": f.Labels["partition_key"], "until": until.UTC().Format("2006-01-02 15:04 MST"), "consequence": consequence, "defaultPartition": def, "tableSQL": tableSQL}))
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
			unique := f.Labels["invalid_unique_indexes"] != "" && f.Labels["invalid_unique_indexes"] != "0"
			out = append(out, relationFinding(id, SeverityWarning, f.Datname, table, "", "", "",
				map[string]any{"table": table, "indexes": indexes}).
				say(id, i18n.Args{"count": len(indexes), "table": table, "datname": f.Datname, "indexes": strings.Join(indexes, ", "), "unique": unique}))
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
			out = append(out, relationFinding(id, SeverityInfo, f.Datname, table, "", "", "",
				map[string]any{"table": table, "rows": rows}).
				say(id, i18n.Args{"table": table, "datname": f.Datname, "rows": formatCatalogCount(rows), "partitions": f.Labels["partitions"], "tableSQL": tableSQL}))
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

// durationParts splits a duration into the unit the catalog shows and its
// count: whole hours under a day, whole days from then on.
func durationParts(d time.Duration) (string, int) {
	if d < 24*time.Hour {
		return "hour", int(d.Hours())
	}
	return "day", int(d.Hours() / 24)
}

// formatCatalogDuration renders a duration in whole days, or hours under a
// day.
func formatCatalogDuration(d time.Duration) string {
	unit, n := durationParts(d)
	return plural(n, unit)
}

// constraintKindKey is the catalog's select key for a constraint kind label;
// a kind the catalog does not name is shown as the label itself.
func constraintKindKey(kind string) string {
	switch kind {
	case "foreign key", "unique", "primary key", "exclusion", "constraint trigger":
		return strings.ReplaceAll(kind, " ", "_")
	case "constraint":
		return kind
	}
	return "other"
}
