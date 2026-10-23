package check

import (
	"context"
	"strings"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
)

func init() { Register(&triggerInventoryCheck{}) }

// triggerFactKind is the fact kind of one trigger. The server replaces a
// database's trigger facts on every complete result (wire.factKinds), so a
// trigger dropped or enabled again stops being reported.
const triggerFactKind = "trigger"

// triggerFactLimit bounds the trigger facts of one database. Disabled
// triggers come first, so the ones the advisor reports are never cut.
const triggerFactLimit = 1000

// triggerInventoryQuery lists the triggers of user tables: every user
// trigger, and the internal ones (foreign keys, deferrable unique
// constraints) only when disabled, since an internal trigger is enabled
// unless someone ran ALTER TABLE ... DISABLE TRIGGER ALL, which stops
// enforcing the constraint. A trigger a partition inherits from its
// partitioned table is reported only when its state differs from the
// parent's: a 365-partition table would otherwise report every trigger 366
// times. Everything is read from the catalogs, which any role can read.
const triggerInventoryQuery = `SELECT pg_catalog.quote_ident(n.nspname) || '.' || pg_catalog.quote_ident(c.relname) || '.' || pg_catalog.quote_ident(t.tgname),
       n.nspname, c.relname, t.tgname, t.tgenabled::pg_catalog.text, t.tgisinternal,
       pn.nspname || '.' || p.proname, t.tgtype::pg_catalog.int4,
       COALESCE(con.conname, ''), COALESCE(con.contype::pg_catalog.text, '')
  FROM pg_catalog.pg_trigger t
  JOIN pg_catalog.pg_class c ON c.oid = t.tgrelid
  JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
  JOIN pg_catalog.pg_proc p ON p.oid = t.tgfoid
  JOIN pg_catalog.pg_namespace pn ON pn.oid = p.pronamespace
  LEFT JOIN pg_catalog.pg_constraint con ON con.oid = t.tgconstraint
  LEFT JOIN pg_catalog.pg_trigger parent ON parent.oid = t.tgparentid
 WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
   AND n.nspname !~ '^pg_(toast|temp_|toast_temp_)'
   AND (NOT t.tgisinternal OR t.tgenabled = 'D')
   AND (parent.oid IS NULL OR parent.tgenabled <> t.tgenabled)
 ORDER BY (t.tgenabled = 'D') DESC, t.tgisinternal DESC, 1
 LIMIT $1`

// triggerCountQuery counts every trigger of user tables by kind and state,
// internal and inherited ones included.
const triggerCountQuery = `SELECT t.tgisinternal, t.tgenabled::pg_catalog.text, pg_catalog.count(*)::pg_catalog.int8
  FROM pg_catalog.pg_trigger t
  JOIN pg_catalog.pg_class c ON c.oid = t.tgrelid
  JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
 WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
   AND n.nspname !~ '^pg_(toast|temp_|toast_temp_)'
 GROUP BY 1, 2`

// triggerInventoryCheck reports the triggers of each database: which exist,
// what they run, and which are disabled. A disabled foreign-key trigger means
// the constraint is no longer enforced.
type triggerInventoryCheck struct{}

func (c *triggerInventoryCheck) Name() string { return "trigger_inventory" }
func (c *triggerInventoryCheck) Requires() Requirements {
	return Requirements{Scope: ScopeDatabase, PermTier: pgtype.TierReadOnly}
}
func (c *triggerInventoryCheck) DefaultInterval() time.Duration { return 5 * time.Minute }
func (c *triggerInventoryCheck) Timeout() time.Duration         { return 10 * time.Second }

func (c *triggerInventoryCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.ConnFor(ctx, t.Database())
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()
	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}

	var result Result
	counts, err := conn.Query(ctx, triggerCountQuery)
	if err != nil {
		return Result{}, err
	}
	for counts.Next() {
		var internal bool
		var state string
		var n int64
		if err := counts.Scan(&internal, &state, &n); err != nil {
			counts.Close()
			return Result{}, err
		}
		result.Metrics = append(result.Metrics, pgtype.Metric{
			Name: "pg_triggers", Value: float64(n), Kind: pgtype.KindGauge,
			Labels: map[string]string{"kind": triggerKind(internal), "state": triggerState(state)},
		})
	}
	counts.Close()
	if err := counts.Err(); err != nil {
		return Result{}, err
	}

	rows, err := conn.Query(ctx, triggerInventoryQuery, triggerFactLimit+1)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, schema, table, name, state, function, constraint, constraintType string
		var internal bool
		var tgtype int32
		if err := rows.Scan(&key, &schema, &table, &name, &state, &internal, &function, &tgtype, &constraint, &constraintType); err != nil {
			return Result{}, err
		}
		if len(result.Facts) == triggerFactLimit {
			result.Truncated = true
			break
		}
		timing, events, level := decodeTriggerType(tgtype)
		result.Facts = append(result.Facts, Fact{
			Kind: triggerFactKind, Key: key, ValueText: triggerState(state),
			Labels: map[string]string{
				"schemaname": schema, "relname": table, "trigger": name,
				"kind": triggerKind(internal), "function": function,
				"timing": timing, "events": events, "level": level,
				"constraint": constraint, "constraint_type": constraintTypeName(constraintType),
			},
		})
	}
	if err := rows.Err(); err != nil {
		return Result{}, err
	}
	return result, nil
}

func triggerKind(internal bool) string {
	if internal {
		return "internal"
	}
	return "user"
}

// triggerState names pg_trigger.tgenabled. A replica trigger fires only in
// sessions with session_replication_role = replica (logical replication
// apply workers), so it does not fire for ordinary writes.
func triggerState(tgenabled string) string {
	switch tgenabled {
	case "O":
		return "enabled"
	case "D":
		return "disabled"
	case "R":
		return "replica"
	case "A":
		return "always"
	default:
		return "unknown"
	}
}

func constraintTypeName(contype string) string {
	switch contype {
	case "f":
		return "foreign key"
	case "u":
		return "unique"
	case "p":
		return "primary key"
	case "x":
		return "exclusion"
	case "t":
		return "constraint trigger"
	case "":
		return ""
	default:
		return "other"
	}
}

// The tgtype bits, from PostgreSQL's catalog/pg_trigger.h.
const (
	triggerTypeRow      = 1 << 0
	triggerTypeBefore   = 1 << 1
	triggerTypeInsert   = 1 << 2
	triggerTypeDelete   = 1 << 3
	triggerTypeUpdate   = 1 << 4
	triggerTypeTruncate = 1 << 5
	triggerTypeInstead  = 1 << 6
)

// decodeTriggerType turns pg_trigger.tgtype into its timing (BEFORE, AFTER,
// INSTEAD OF), its events in CREATE TRIGGER order, and its level (ROW,
// STATEMENT).
func decodeTriggerType(tgtype int32) (timing, events, level string) {
	switch {
	case tgtype&triggerTypeInstead != 0:
		timing = "INSTEAD OF"
	case tgtype&triggerTypeBefore != 0:
		timing = "BEFORE"
	default:
		timing = "AFTER"
	}
	var names []string
	for _, e := range []struct {
		bit  int32
		name string
	}{
		{triggerTypeInsert, "INSERT"}, {triggerTypeUpdate, "UPDATE"},
		{triggerTypeDelete, "DELETE"}, {triggerTypeTruncate, "TRUNCATE"},
	} {
		if tgtype&e.bit != 0 {
			names = append(names, e.name)
		}
	}
	level = "STATEMENT"
	if tgtype&triggerTypeRow != 0 {
		level = "ROW"
	}
	return timing, strings.Join(names, " OR "), level
}
