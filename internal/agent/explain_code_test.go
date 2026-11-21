package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func scannedCalls(facts codeFacts) []string {
	names := make([]string, 0, len(facts.calls))
	for _, ref := range facts.calls {
		names = append(names, ref.Name)
	}
	return names
}

// A cast check like the isnumeric() many databases define calls nothing and
// runs no dynamic SQL: the guard admits it.
func TestScanCode_CastCheckIsPlain(t *testing.T) {
	facts := scanCode(`
DECLARE x NUMERIC;
BEGIN
    x = $1::NUMERIC;
    RETURN TRUE;
EXCEPTION WHEN others THEN
    RETURN FALSE;
END;`, false)
	require.Empty(t, facts.dynamic)
	require.Empty(t, facts.calls)
}

func TestScanCode_FindsCalls(t *testing.T) {
	facts := scanCode(`BEGIN PERFORM pg_advisory_lock(1); RETURN app.helper($1) + lower('x'); END`, false)
	require.Empty(t, facts.dynamic)
	require.Subset(t, scannedCalls(facts), []string{"pg_advisory_lock", "helper", "lower"})
}

func TestScanCode_FlagsDynamicSQL(t *testing.T) {
	cases := map[string]string{
		`BEGIN EXECUTE 'DROP TABLE ' || $1; END`:                                  "EXECUTE",
		`BEGIN execute format('select %s', $1); END`:                              "EXECUTE",
		`BEGIN COPY t TO '/tmp/out'; END`:                                         "COPY",
		`BEGIN DO $x$ BEGIN PERFORM 1; END $x$; END`:                              "DO",
		`BEGIN DO 'BEGIN PERFORM 1; END'; END`:                                    "DO",
		`BEGIN DO LANGUAGE plpgsql $$ BEGIN END $$; END`:                          "DO",
		`BEGIN RETURN QUERY EXECUTE 'SELECT 1'; END`:                              "EXECUTE",
		`SELECT 1 /* nothing */; EXECUTE /* x */ 'SELECT 2'`:                      "EXECUTE",
		`BEGIN INSERT INTO t VALUES (1) ON CONFLICT DO NOTHING; EXECUTE 'x'; END`: "EXECUTE",
	}
	for body, want := range cases {
		require.Equal(t, want, scanCode(body, false).dynamic, body)
	}
}

func TestScanCode_IgnoresKeywordsInStringsAndComments(t *testing.T) {
	for _, body := range []string{
		`BEGIN RAISE NOTICE 'execute copy do'; END`,
		`BEGIN -- execute this later
RETURN 1; END`,
		`BEGIN /* COPY t TO stdout */ RETURN 1; END`,
		`BEGIN INSERT INTO t VALUES (1) ON CONFLICT DO NOTHING; END`,
		`BEGIN INSERT INTO t VALUES (1) ON CONFLICT (id) DO UPDATE SET v = 2; END`,
		`SELECT "execute" FROM t`,
	} {
		require.Empty(t, scanCode(body, false).dynamic, body)
	}
}

func TestScanCode_WordsNameRelations(t *testing.T) {
	facts := scanCode(`SELECT a FROM App_View JOIN "Mixed" ON true`, false)
	require.Contains(t, facts.words, "app_view")
	require.Contains(t, facts.words, "Mixed")
}
