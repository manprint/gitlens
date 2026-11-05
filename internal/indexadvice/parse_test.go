package indexadvice

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func preds(a Analysis) []string {
	out := []string{}
	for _, p := range a.Predicates {
		s := p.Table + "." + p.Column + ":" + string(p.Kind)
		if p.Dir != "" {
			s += ":" + p.Dir
		}
		if p.Limit {
			s += ":limit"
		}
		out = append(out, s)
	}
	return out
}

func tables(a Analysis) []string {
	out := []string{}
	for _, t := range a.Tables {
		s := t.Name
		if t.Schema != "" {
			s = t.Schema + "." + t.Name
		}
		if t.Alias != "" {
			s += " " + t.Alias
		}
		out = append(out, s)
	}
	return out
}

func skips(a Analysis) []string {
	out := []string{}
	for _, s := range a.Skipped {
		out = append(out, string(s))
	}
	return out
}

func TestIDXAPARSE001_Simple(t *testing.T) {
	a := Parse(`SELECT id FROM res_partner WHERE name = $1`)
	require.Equal(t, "select", a.Statement)
	require.Equal(t, []string{"res_partner"}, tables(a))
	require.Equal(t, []string{"res_partner.name:eq"}, preds(a))
	require.Empty(t, a.Skipped)
}

func TestIDXAPARSE002_Alias(t *testing.T) {
	a := Parse(`SELECT 1 FROM res_partner p WHERE p.name = $1`)
	require.Equal(t, []string{"res_partner p"}, tables(a))
	require.Equal(t, []string{"res_partner.name:eq"}, preds(a))
	a = Parse(`SELECT 1 FROM "public"."res_partner" AS "p" WHERE "p"."name" = $1`)
	require.Equal(t, []string{"public.res_partner p"}, tables(a))
	require.Equal(t, []string{"res_partner.name:eq"}, preds(a))
	a = Parse(`SELECT 1 FROM res_partner WHERE res_partner.name = $1`)
	require.Equal(t, []string{"res_partner.name:eq"}, preds(a))
}

func TestIDXAPARSE003_JoinOn(t *testing.T) {
	a := Parse(`SELECT 1 FROM sale_order o JOIN res_partner p ON o.partner_id = p.id LEFT JOIN res_users u ON (u.id = o.user_id AND u.active) WHERE o.state = $1`)
	require.Equal(t, []string{"sale_order o", "res_partner p", "res_users u"}, tables(a))
	require.ElementsMatch(t, []string{"sale_order.partner_id:eq", "res_partner.id:eq", "res_users.id:eq", "sale_order.user_id:eq", "sale_order.state:eq"}, preds(a))
	require.Equal(t, []string{"res_users.id:eq", "sale_order.user_id:eq"}, preds(Parse(`SELECT 1 FROM sale_order o LEFT JOIN res_users u ON (u.id = o.user_id AND u.active)`)))
}

func TestIDXAPARSE004_InAny(t *testing.T) {
	a := Parse(`SELECT 1 FROM t WHERE a IN ($1, $2) AND b = ANY($3) AND c IN (SELECT x FROM u WHERE y = $4)`)
	require.Equal(t, []string{"t.a:eq", "t.b:eq", "t.c:eq", "u.y:eq"}, preds(a))
}

func TestIDXAPARSE005_RangeBetween(t *testing.T) {
	a := Parse(`SELECT 1 FROM t WHERE a >= $1 AND a < $2 AND b BETWEEN $3 AND $4 AND c = $5`)
	require.Equal(t, []string{"t.a:range", "t.a:range", "t.b:range", "t.c:eq"}, preds(a))
	require.Empty(t, a.Skipped)
}

func TestIDXAPARSE006_Like(t *testing.T) {
	require.Equal(t, []string{"t.a:like_prefix"}, preds(Parse(`SELECT 1 FROM t WHERE a LIKE 'abc%'`)))
	require.Equal(t, []string{"t.a:like_prefix"}, preds(Parse(`SELECT 1 FROM t WHERE a ILIKE 'abc%'`)))
	require.Empty(t, preds(Parse(`SELECT 1 FROM t WHERE a LIKE $1`)))
	require.Empty(t, preds(Parse(`SELECT 1 FROM t WHERE a LIKE '%abc'`)))
	require.Empty(t, preds(Parse(`SELECT 1 FROM t WHERE a ILIKE '%abc%'`)))
	require.Empty(t, Parse(`SELECT 1 FROM t WHERE a LIKE $1`).Skipped)
}

func TestIDXAPARSE007_OrderLimit(t *testing.T) {
	a := Parse(`SELECT 1 FROM t WHERE a = $1 ORDER BY a DESC, b LIMIT 10`)
	require.Equal(t, []string{"t.a:eq", "t.a:order:desc:limit", "t.b:order:asc:limit"}, preds(a))
	a = Parse(`SELECT 1 FROM t ORDER BY b ASC NULLS LAST`)
	require.Equal(t, []string{"t.b:order:asc"}, preds(a))
	a = Parse(`SELECT 1 FROM t ORDER BY lower(b), 1`)
	require.Equal(t, []string{"expression"}, skips(a))
}

func TestIDXAPARSE008_OrGroups(t *testing.T) {
	a := Parse(`SELECT 1 FROM t WHERE a = $1 OR b = $2`)
	require.Empty(t, preds(a))
	require.Equal(t, []string{"or_group"}, skips(a))
	a = Parse(`SELECT 1 FROM t WHERE c = $1 AND (a = $2 OR b = $3)`)
	require.Equal(t, []string{"t.c:eq"}, preds(a))
	require.Equal(t, []string{"or_group"}, skips(a))
	a = Parse(`SELECT 1 FROM t WHERE (a = $1 AND b = $2) AND c = $3`)
	require.Equal(t, []string{"t.a:eq", "t.b:eq", "t.c:eq"}, preds(a))
}

func TestIDXAPARSE009_Expression(t *testing.T) {
	a := Parse(`SELECT 1 FROM t WHERE lower(name) = $1 AND code::text = $2 AND NOT flag AND x <> $3`)
	require.Empty(t, preds(a))
	require.Equal(t, []string{"expression", "expression", "expression", "expression"}, skips(a))
}

func TestIDXAPARSE010_SubselectAndCTEScopes(t *testing.T) {
	a := Parse(`WITH recent AS (SELECT id FROM a WHERE a.x = $1) SELECT 1 FROM b WHERE b.y = $2 AND b.z IN (SELECT w FROM c WHERE c.v = $3)`)
	require.Equal(t, []string{"a", "b", "c"}, tables(a))
	require.Equal(t, []string{"a.x:eq", "b.y:eq", "b.z:eq", "c.v:eq"}, preds(a))
	require.Equal(t, []int{a.Predicates[0].Scope, a.Predicates[1].Scope}, []int{a.Predicates[0].Scope, a.Predicates[1].Scope})
	require.NotEqual(t, a.Predicates[0].Scope, a.Predicates[1].Scope)
	// A CTE reference is not a table and its columns are not predicates.
	a = Parse(`WITH r AS (SELECT id FROM a) SELECT 1 FROM r JOIN b ON b.rid = r.id WHERE r.id = $1`)
	require.Equal(t, []string{"a", "b"}, tables(a))
	require.Equal(t, []string{"b.rid:eq"}, preds(a))
	// The sub-select alias map is its own: an outer alias is unresolved inside.
	a = Parse(`SELECT 1 FROM t o WHERE o.a = $1 AND EXISTS (SELECT 1 FROM u WHERE o.b = $2 AND u.c = $3)`)
	require.Equal(t, []string{"t.a:eq", "u.c:eq"}, preds(a))
	require.Equal(t, []string{"unresolved_alias"}, skips(a))
}

func TestIDXAPARSE011_Update(t *testing.T) {
	a := Parse(`UPDATE t SET x = $1 WHERE id = $2`)
	require.Equal(t, "update", a.Statement)
	require.Equal(t, []string{"t"}, tables(a))
	require.Equal(t, []string{"t.id:eq"}, preds(a))
	a = Parse(`UPDATE ONLY public.t AS x SET v = $1 FROM u WHERE x.id = u.tid AND u.k = $2`)
	require.Equal(t, []string{"public.t x", "u"}, tables(a))
	require.ElementsMatch(t, []string{"t.id:eq", "u.tid:eq", "u.k:eq"}, preds(a))
}

func TestIDXAPARSE012_Delete(t *testing.T) {
	a := Parse(`DELETE FROM t WHERE a = $1 AND b < $2`)
	require.Equal(t, "delete", a.Statement)
	require.Equal(t, []string{"t.a:eq", "t.b:range"}, preds(a))
	a = Parse(`DELETE FROM t USING u WHERE t.uid = u.id AND u.k = $1 RETURNING t.id`)
	require.Equal(t, []string{"t", "u"}, tables(a))
	require.ElementsMatch(t, []string{"t.uid:eq", "u.id:eq", "u.k:eq"}, preds(a))
}

func TestIDXAPARSE013_UnresolvedAlias(t *testing.T) {
	a := Parse(`SELECT 1 FROM t WHERE zz.a = $1`)
	require.Empty(t, preds(a))
	require.Equal(t, []string{"unresolved_alias"}, skips(a))
	a = Parse(`SELECT 1 FROM t JOIN u ON u.tid = t.id WHERE name = $1`)
	require.Equal(t, []string{"unresolved_alias"}, skips(a))
}

func TestIDXAPARSE014_GarbageNeverPanics(t *testing.T) {
	for _, in := range []string{"", "   ", ";", "SELECT 'open", "SELECT (", "SELECT ) FROM", "WHERE", "SELECT FROM WHERE", "\x00\xff\xfe", strings.Repeat("(", 5000), strings.Repeat("SELECT (", 200), "SELECT 1 FROM t WHERE a = $1 AND", "UPDATE", "DELETE FROM", "INSERT INTO", "WITH", "WITH a AS", "WITH a AS (", "SELECT * FROM (SELECT * FROM"} {
		require.NotPanics(t, func() { Parse(in) }, in)
	}
	require.Equal(t, []string{"parse_error"}, skips(Parse("SELECT 'open")))
	require.Equal(t, []string{"parse_error"}, skips(Parse("SELECT (1")))
	require.Equal(t, []string{"parse_error"}, skips(Parse("SELECT 1 FROM t WHERE a = '"+strings.Repeat("x", MaxInput))))
	deep := "SELECT 1"
	for i := 0; i < 100; i++ {
		deep = "SELECT 1 FROM t WHERE a IN (" + deep + ")"
	}
	require.Equal(t, []string{"parse_error"}, skips(Parse(deep)), "nesting beyond the depth bound")
	require.NotPanics(t, func() { Parse(strings.Repeat("(", 200) + strings.Repeat(")", 200)) })
}

func TestIDXAPARSE015_DeterministicOrder(t *testing.T) {
	sql := `SELECT 1 FROM a JOIN b ON a.x = b.y WHERE a.p = $1 AND b.q > $2 ORDER BY a.z LIMIT 5`
	first := Parse(sql)
	for i := 0; i < 20; i++ {
		require.Equal(t, first, Parse(sql))
	}
	require.Equal(t, []string{"a.x:eq", "b.y:eq", "a.p:eq", "b.q:range", "a.z:order:asc:limit"}, preds(first))
}

func TestIDXAPARSE016_Catalog(t *testing.T) {
	a := Parse(`SELECT oid FROM pg_catalog.pg_class WHERE relname = $1`)
	require.Empty(t, a.Tables)
	require.Empty(t, a.Predicates)
	require.Equal(t, []string{"catalog"}, skips(a))
	a = Parse(`SELECT 1 FROM pg_class c JOIN information_schema.tables i ON i.table_name = c.relname`)
	require.Equal(t, []string{"catalog"}, skips(a))
	a = Parse(`SELECT 1 FROM pg_class c JOIN res_partner p ON p.name = c.relname WHERE p.id = $1`)
	require.Equal(t, []string{"res_partner p"}, tables(a))
	require.Equal(t, []string{"res_partner.name:eq", "res_partner.id:eq"}, preds(a))
	require.Empty(t, a.Skipped)
}

func TestIDXAPARSE017_SetOpsInsertAndUnsupported(t *testing.T) {
	a := Parse(`SELECT id FROM a WHERE x = $1 UNION ALL SELECT id FROM b WHERE y = $2`)
	require.Equal(t, []string{"a.x:eq", "b.y:eq"}, preds(a))
	a = Parse(`INSERT INTO log (a, b) SELECT a, b FROM src WHERE k = $1 ON CONFLICT DO NOTHING`)
	require.Equal(t, []string{"src"}, tables(a))
	require.Equal(t, []string{"src.k:eq"}, preds(a))
	a = Parse(`INSERT INTO log (a) VALUES ($1), ($2)`)
	require.Empty(t, a.Tables)
	require.Empty(t, a.Skipped)
	for _, sql := range []string{"BEGIN", "COMMIT", "SET search_path = x", "COPY t (a) FROM STDIN", "CREATE TABLE t (a int)", "DISCARD ALL", "SAVEPOINT s", "[REDACTED]"} {
		require.Equal(t, []string{"unsupported_statement"}, skips(Parse(sql)), sql)
	}
}

func TestIDXAPARSE018_IsNullReversedAndDistinctFrom(t *testing.T) {
	a := Parse(`SELECT 1 FROM t WHERE a IS NULL AND b IS NOT NULL AND $1 = c AND $2 < d AND e IS DISTINCT FROM $3`)
	require.Equal(t, []string{"t.a:isnull", "t.c:eq", "t.d:range"}, preds(a))
	require.Equal(t, []string{"expression"}, skips(a))
	a = Parse(`SELECT a IS DISTINCT FROM b AS d FROM t WHERE x = $1`)
	require.Equal(t, []string{"t.x:eq"}, preds(a))
}

func TestIDXAPARSE019_SkipCountsAndMultiStatement(t *testing.T) {
	a := Parse(`SELECT 1 FROM t WHERE a = $1 OR b = $2; SELECT 1 FROM u WHERE x = $1 OR y = $2`)
	require.Equal(t, map[SkipReason]int{SkipOrGroup: 2}, a.SkipCounts())
	require.Equal(t, "select", a.Statement)
}

func TestIDXAPARSE020_FunctionsAndLaterals(t *testing.T) {
	a := Parse(`SELECT 1 FROM t, unnest($1::int[]) AS x(id) WHERE t.id = x.id AND t.k = $2`)
	require.Equal(t, []string{"t"}, tables(a))
	require.Equal(t, []string{"t.id:eq", "t.k:eq"}, preds(a))
	a = Parse(`SELECT 1 FROM t JOIN LATERAL (SELECT 1 FROM u WHERE u.tid = t.id LIMIT 1) l ON true WHERE t.z = $1`)
	require.Equal(t, []string{"t", "u"}, tables(a))
	require.Equal(t, []string{"t.z:eq", "u.tid:eq"}, preds(a))
}
