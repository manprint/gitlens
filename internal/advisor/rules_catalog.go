package advisor

import (
	"fmt"
	"sort"
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
