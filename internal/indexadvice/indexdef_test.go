package indexadvice

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func mustDef(t *testing.T, def string) IndexDef {
	t.Helper()
	d, err := ParseIndexDef(def)
	require.NoError(t, err, def)
	return d
}

func TestIDXADEF001_Btree(t *testing.T) {
	d := mustDef(t, `CREATE INDEX sale_order_partner_id_idx ON public.sale_order USING btree (partner_id, state)`)
	require.Equal(t, IndexDef{Schema: "public", Table: "sale_order", Name: "sale_order_partner_id_idx", Method: "btree",
		Columns: []IndexColumn{{Name: "partner_id"}, {Name: "state"}}}, d)
}

func TestIDXADEF002_DescAndNulls(t *testing.T) {
	d := mustDef(t, `CREATE INDEX i ON public.t USING btree (a DESC, b, c DESC NULLS LAST, d NULLS FIRST)`)
	require.Equal(t, []IndexColumn{{Name: "a", Desc: true}, {Name: "b"}, {Name: "c", Desc: true}, {Name: "d"}}, d.Columns)
}

func TestIDXADEF003_Partial(t *testing.T) {
	d := mustDef(t, `CREATE INDEX calendar_event__candidate_id_index ON public.calendar_event USING btree (candidate_id) WHERE (candidate_id IS NOT NULL)`)
	require.Equal(t, "(candidate_id IS NOT NULL)", d.Where)
	require.Equal(t, []IndexColumn{{Name: "candidate_id"}}, d.Columns)
}

func TestIDXADEF004_Include(t *testing.T) {
	d := mustDef(t, `CREATE UNIQUE INDEX i ON ONLY public.t USING btree (a, b) INCLUDE (c, d) WITH (fillfactor='70') TABLESPACE fast WHERE (a > 0)`)
	require.True(t, d.Unique)
	require.Equal(t, []string{"c", "d"}, d.Include)
	require.Equal(t, []IndexColumn{{Name: "a"}, {Name: "b"}}, d.Columns)
	require.Equal(t, "(a > 0)", d.Where)
}

func TestIDXADEF005_Expression(t *testing.T) {
	d := mustDef(t, `CREATE INDEX i ON public.res_partner USING btree (lower((name)::text), id)`)
	require.Equal(t, []IndexColumn{{Name: "lower((name)::text)", Expr: true}, {Name: "id"}}, d.Columns)
	d = mustDef(t, `CREATE INDEX res_partner__x_index ON public.res_partner USING btree (((x IS NOT NULL))) WHERE (x IS NOT NULL)`)
	require.Equal(t, []IndexColumn{{Name: "((x IS NOT NULL))", Expr: true}}, d.Columns)
}

func TestIDXADEF006_OtherMethodsQuotedAndErrors(t *testing.T) {
	d := mustDef(t, `CREATE INDEX i ON public.t USING gin (name gin_trgm_ops)`)
	require.Equal(t, "gin", d.Method)
	require.Equal(t, []IndexColumn{{Name: "name"}}, d.Columns)
	d = mustDef(t, `CREATE INDEX i ON public.t USING hash (code)`)
	require.Equal(t, "hash", d.Method)
	d = mustDef(t, `CREATE INDEX "My Idx" ON "Sch"."My Table" USING btree ("Col A")`)
	require.Equal(t, IndexDef{Schema: "Sch", Table: "My Table", Name: "My Idx", Method: "btree", Columns: []IndexColumn{{Name: "Col A"}}}, d)
	for _, bad := range []string{"", "SELECT 1", "CREATE TABLE t (a int)", "CREATE INDEX", "CREATE INDEX i ON", "CREATE INDEX i ON t USING btree", "CREATE INDEX i ON t USING btree (a", "CREATE INDEX i ON t (a,,b)", "CREATE INDEX i ON t USING btree ('open"} {
		_, err := ParseIndexDef(bad)
		require.Error(t, err, bad)
	}
}
