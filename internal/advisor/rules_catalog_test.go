package advisor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func triggerFact(datname, table, trigger, kind, state string, observed time.Time, extra map[string]string) Fact {
	labels := map[string]string{
		"schemaname": "public", "relname": table, "trigger": trigger, "kind": kind,
		"function": "public.audit", "timing": "AFTER", "events": "INSERT", "level": "ROW",
	}
	for k, v := range extra {
		labels[k] = v
	}
	return Fact{Key: "public." + table + "." + trigger, Datname: datname, ValueText: state, Labels: labels, ObservedAt: observed}
}

func catalogSnapshot(now time.Time, kind string, facts ...Fact) *Snapshot {
	s := &Snapshot{Now: now, Facts: map[string]map[string]Fact{kind: {}}, SkippedChecks: map[string]string{}}
	for _, f := range facts {
		s.Facts[kind][databaseFactKey(f.Datname, f.Key)] = f
	}
	return s
}

// Each database's facts are read from its latest batch only: a fact a
// truncated scrape did not reach again, or a database that stopped
// reporting hours ago, says nothing about the objects now.
func TestCurrentFacts_LatestBatchPerDatabase(t *testing.T) {
	now := time.Now()
	s := catalogSnapshot(now, "trigger",
		triggerFact("app", "a", "t", "user", "enabled", now, nil),
		triggerFact("app", "b", "t", "user", "enabled", now.Add(-30*time.Second), nil),
		triggerFact("app", "old", "t", "user", "disabled", now.Add(-10*time.Minute), nil),
		triggerFact("crm", "c", "t", "user", "disabled", now.Add(-20*time.Minute), nil),
		triggerFact("gone", "d", "t", "user", "disabled", now.Add(-4*time.Hour), nil),
	)
	var got []string
	for _, f := range currentFacts(s, "trigger") {
		got = append(got, f.Datname+"/"+f.Labels["relname"])
	}
	require.Equal(t, []string{"app/a", "app/b", "crm/c"}, got)
	require.Empty(t, currentFacts(s, "sequence"))
}

func TestTriggerRules_SatisfyContract(t *testing.T) {
	now := time.Now()
	for _, s := range []*Snapshot{
		catalogSnapshot(now, "trigger"),
		catalogSnapshot(now, "trigger",
			triggerFact("app", "orders", "RI_ConstraintTrigger_c_1", "internal", "disabled", now, map[string]string{"constraint": "orders_customer_fkey", "constraint_type": "foreign key"}),
			triggerFact("app", "orders", "audit", "user", "disabled", now, nil)),
	} {
		assertRuleContract(t, ruleByID(t, "trigger.constraint_disabled"), s)
		assertRuleContract(t, ruleByID(t, "trigger.user_disabled"), s)
	}
}

// The two internal triggers of one foreign key make one finding, and a
// second foreign key on the same table another one.
func TestTriggerRules_ConstraintDisabled(t *testing.T) {
	now := time.Now()
	fk := func(trigger, constraint string) Fact {
		return triggerFact("app", "Orders", trigger, "internal", "disabled", now, map[string]string{"constraint": constraint, "constraint_type": "foreign key"})
	}
	s := catalogSnapshot(now, "trigger",
		fk("RI_ConstraintTrigger_c_1", "orders_customer_fkey"),
		fk("RI_ConstraintTrigger_c_2", "orders_customer_fkey"),
		fk("RI_ConstraintTrigger_c_3", "orders_product_fkey"),
		triggerFact("app", "Orders", "audit", "user", "disabled", now, nil),
	)
	got := ruleByID(t, "trigger.constraint_disabled").Evaluate(s)
	require.Len(t, got, 2)
	require.Equal(t, SeverityCritical, got[0].Severity)
	require.Equal(t, "The foreign key orders_customer_fkey is not enforced", got[0].Title)
	require.Equal(t, "public.Orders.orders_customer_fkey", got[0].ObjectName)
	require.Equal(t, "app", got[0].Datname)
	require.Equal(t, []string{"RI_ConstraintTrigger_c_1", "RI_ConstraintTrigger_c_2"}, got[0].Evidence["triggers"])
	require.Contains(t, got[0].Remediation, `ALTER TABLE "public"."Orders" ENABLE TRIGGER ALL`)
	require.Equal(t, "public.Orders.orders_product_fkey", got[1].ObjectName)
	require.NotEqual(t, got[0].ID(), got[1].ID())

	// Enabled again: the internal triggers are no longer reported at all.
	require.Empty(t, ruleByID(t, "trigger.constraint_disabled").Evaluate(catalogSnapshot(now, "trigger",
		triggerFact("app", "Orders", "audit", "user", "enabled", now, nil))))
}

func TestTriggerRules_UserDisabled(t *testing.T) {
	now := time.Now()
	s := catalogSnapshot(now, "trigger",
		triggerFact("app", "orders", "audit", "user", "disabled", now, nil),
		triggerFact("app", "orders", "stamp", "user", "enabled", now, nil),
		triggerFact("app", "orders", "sync", "user", "replica", now, nil),
		triggerFact("app", "orders", "RI_ConstraintTrigger_c_1", "internal", "disabled", now, map[string]string{"constraint": "orders_customer_fkey", "constraint_type": "foreign key"}),
	)
	got := ruleByID(t, "trigger.user_disabled").Evaluate(s)
	require.Len(t, got, 1)
	require.Equal(t, SeverityWarning, got[0].Severity)
	require.Equal(t, "public.orders.audit", got[0].ObjectName)
	require.Equal(t, "Trigger audit on public.orders is disabled", got[0].Title)
	require.Contains(t, got[0].Remediation, `ALTER TABLE "public"."orders" ENABLE TRIGGER "audit"`)
}

// A skipped trigger inventory degrades both rules instead of reporting that
// no trigger is disabled.
func TestTriggerRules_SkippedInventoryDegrades(t *testing.T) {
	s := catalogSnapshot(time.Now(), "trigger")
	s.SkippedChecks["trigger_inventory"] = "permission denied"
	for _, id := range []string{"trigger.constraint_disabled", "trigger.user_disabled"} {
		findings, _ := (&Engine{}).evaluateRule(ruleByID(t, id), s)
		require.Len(t, findings, 1, id)
		require.Equal(t, StateDegraded, findings[0].State, id)
	}
}

func fkFact(datname, table, constraint string, bytes string, observed time.Time) Fact {
	return Fact{
		Key: "public." + table + "." + constraint, Datname: datname, ValueText: "customer_id, region",
		Labels: map[string]string{
			"schemaname": "public", "relname": table, "constraint": constraint,
			"referenced": "public.customers", "columns_sql": `customer_id, "Region"`, "table_bytes": bytes,
		},
		ObservedAt: observed,
	}
}

// A foreign key on a large table is a warning, on a small one information;
// the remediation names the columns as SQL.
func TestFKUnindexedRule(t *testing.T) {
	now := time.Now()
	rule := ruleByID(t, "index.fk_unindexed")
	s := catalogSnapshot(now, "fk_unindexed",
		fkFact("app", "Orders", "orders_customer_fkey", "52428800", now),
		fkFact("app", "notes", "notes_customer_fkey", "8192", now),
	)
	assertRuleContract(t, rule, s)
	assertRuleContract(t, rule, catalogSnapshot(now, "fk_unindexed"))
	got := rule.Evaluate(s)
	require.Len(t, got, 2)
	require.Equal(t, "public.Orders.orders_customer_fkey", got[0].ObjectName)
	require.Equal(t, SeverityWarning, got[0].Severity)
	require.Contains(t, got[0].Detail, "(50.0 MiB)")
	require.Contains(t, got[0].Remediation, `CREATE INDEX CONCURRENTLY ON "public"."Orders" (customer_id, "Region")`)
	require.Equal(t, SeverityInfo, got[1].Severity)
	require.Contains(t, got[1].Detail, "(8.0 KiB)")

	s.SkippedChecks["fk_indexes"] = "permission denied"
	findings, _ := (&Engine{}).evaluateRule(rule, s)
	require.Len(t, findings, 1)
	require.Equal(t, StateDegraded, findings[0].State)
}

func TestFormatCatalogBytes(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KiB", 10 << 20: "10.0 MiB", 3 << 30: "3.0 GiB"} {
		require.Equal(t, want, formatCatalogBytes(n))
	}
}

func extensionFact(datname, name, version, available, upgrade string, observed time.Time) Fact {
	return Fact{Key: name, Datname: datname, ValueText: version, ObservedAt: observed,
		Labels: map[string]string{"schema": "public", "default_version": available, "upgrade": upgrade}}
}

// PostGIS extensions are reported together, per database, with the PostGIS
// upgrade function; any other extension on its own.
func TestExtensionRules(t *testing.T) {
	now := time.Now()
	s := catalogSnapshot(now, "extension",
		extensionFact("app", "postgis", "3.5.2", "3.6.4", "available", now),
		extensionFact("app", "postgis_topology", "3.5.2", "3.6.4", "available", now),
		extensionFact("app", "pg_trgm", "1.5", "1.6", "available", now),
		extensionFact("app", "plpgsql", "1.0", "1.0", "none", now),
		extensionFact("gis", "postgis", "3.6.4", "3.6.4", "none", now),
	)
	generic := ruleByID(t, "extension.upgrade_available")
	postgis := ruleByID(t, "postgis.upgrade_available")
	require.Equal(t, ScopeDatabase, generic.Scope())
	assertRuleContract(t, generic, s)
	assertRuleContract(t, postgis, s)

	got := generic.Evaluate(s)
	require.Len(t, got, 1)
	require.Equal(t, "pg_trgm", got[0].ObjectName)
	require.Equal(t, SeverityInfo, got[0].Severity)
	require.Contains(t, got[0].Remediation, `ALTER EXTENSION "pg_trgm" UPDATE`)

	got = postgis.Evaluate(s)
	require.Len(t, got, 1)
	require.Equal(t, "app", got[0].Datname)
	require.Equal(t, SeverityWarning, got[0].Severity)
	require.Contains(t, got[0].Detail, "postgis 3.5.2 → 3.6.4, postgis_topology 3.5.2 → 3.6.4")
	require.Contains(t, got[0].Remediation, "postgis_extensions_upgrade()")
}

func spatialFact(datname, table, column, typ, subtype, srid, known, method, rows string, observed time.Time) Fact {
	return Fact{Key: "public." + table + "." + column, Datname: datname, ValueText: typ, ObservedAt: observed,
		Labels: map[string]string{"schemaname": "public", "relname": table, "column": column, "subtype": subtype,
			"srid": srid, "srid_known": known, "index_method": method, "rows": rows}}
}

func TestPostGISColumnRules(t *testing.T) {
	now := time.Now()
	s := catalogSnapshot(now, "spatial_column",
		spatialFact("app", "parcels", "geom", "geometry", "MultiPolygon", "3857", "true", "", "250000", now),
		spatialFact("app", "pois", "pt", "geometry", "Point", "4326", "true", "gist", "100", now),
		spatialFact("app", "notes", "pt", "geometry", "Point", "4326", "true", "", "12", now),
		spatialFact("app", "legacy", "shape", "geometry", "Polygon", "0", "false", "brin", "500", now),
		spatialFact("app", "odd", "pt", "geometry", "Point", "990000", "false", "gist", "5", now),
		spatialFact("app", "free", "g", "geometry", "Geometry", "", "unknown", "gist", "5", now),
		spatialFact("gis", "roads", "line", "geometry", "LineString", "4326", "true", "gist", "5", now),
	)
	unindexed := ruleByID(t, "postgis.spatial_unindexed")
	unknown := ruleByID(t, "postgis.srid_unknown")
	mixed := ruleByID(t, "postgis.srid_mixed")
	for _, r := range []Rule{unindexed, unknown, mixed} {
		assertRuleContract(t, r, s)
	}

	got := unindexed.Evaluate(s)
	require.Len(t, got, 2)
	require.Equal(t, "public.notes.pt", got[0].ObjectName)
	require.Equal(t, SeverityInfo, got[0].Severity)
	require.Equal(t, "public.parcels.geom", got[1].ObjectName)
	require.Equal(t, SeverityWarning, got[1].Severity)
	require.Contains(t, got[1].Remediation, `CREATE INDEX CONCURRENTLY ON "public"."parcels" USING gist ("geom")`)

	got = unknown.Evaluate(s)
	require.Len(t, got, 2)
	require.Contains(t, got[0].Detail, "is declared without an SRID (0)")
	require.Contains(t, got[1].Detail, "SRID 990000, which spatial_ref_sys does not define")

	got = mixed.Evaluate(s)
	require.Len(t, got, 1)
	require.Equal(t, "app", got[0].Datname)
	require.Contains(t, got[0].Detail, "SRID 3857 (1 column), SRID 4326 (2 columns), SRID 990000 (1 column)")
}
