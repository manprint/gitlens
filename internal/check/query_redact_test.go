package check

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A statement that carries a credential is withheld whole, in every spelling a
// role, a server or user mapping, a subscription or a dblink call can give it.
func TestRedactQueryText_WithholdsCredentials(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, in string }{
		{"create role", "CREATE ROLE tmp_secret_probe LOGIN PASSWORD 'hunter2-probe'"},
		{"alter role", "ALTER ROLE tmp_secret_probe PASSWORD 'hunter2-probe-2'"},
		{"encrypted and unencrypted", "CREATE USER u ENCRYPTED PASSWORD 'md5abc'; ALTER USER u UNENCRYPTED PASSWORD 'plain'"},
		{"doubled quote inside the literal", "ALTER ROLE u PASSWORD 'it''s secret' LOGIN"},
		{"escape string", `ALTER ROLE u PASSWORD E'pa\'ss' LOGIN`},
		{"no blank before the literal", "ALTER ROLE u PASSWORD'hunter2'"},
		{"comment before the literal", "CREATE USER u PASSWORD/**/'hunter2'"},
		{"nested comment before the literal", "ALTER ROLE u PASSWORD /* a /* b */ c */ 'hunter2'"},
		{"nested comment with a quote before the literal", "ALTER ROLE u PASSWORD /* a /* b */ 'x' c */ 'hunter2'"},
		{"three levels of nested comment", "ALTER ROLE u PASSWORD /* a /* b /* c */ 'x' */ 'y' */ 'hunter2'"},
		{"line marker inside a block comment", "ALTER ROLE u PASSWORD /* -- */ 'hunter2'"},
		{"comment with a quote before the literal", "ALTER ROLE u PASSWORD /* it's */ 'hunter2'"},
		{"line comment before the literal", "ALTER ROLE u PASSWORD --c\n'hunter2'"},
		{"carriage return ends a line comment", "ALTER ROLE u PASSWORD --c\r'hunter2' LOGIN\n'x'"},
		{"line comment and blanks before the literal", "ALTER ROLE u PASSWORD -- it's a note\n  'hunter2' LOGIN"},
		{"backslash in a plain literal", `ALTER ROLE u PASSWORD 'pa\'ss' LOGIN`},
		{"two statements after a backslash", `ALTER ROLE a PASSWORD 'x\'; ALTER ROLE b PASSWORD 'hunter2'`},
		{"keyword after a backslash literal", `SELECT 'a\'; ALTER ROLE b PASSWORD 'hunter2'`},
		{"backslash at the text limit", `ALTER ROLE u PASSWORD 'cut-here\`},
		{"escape string cut after a backslash", `ALTER ROLE u PASSWORD E'cut-here\`},
		{"unicode escape literal", "ALTER ROLE u PASSWORD U&'hunter2'"},
		{"option without a blank", "CREATE USER MAPPING FOR u SERVER s OPTIONS (password'hunter2')"},
		{"dollar quoted ends at its tag", "CREATE ROLE u PASSWORD $$a'b$$ VALID UNTIL 'x'"},
		{"dollar quoted with a non-ASCII tag", "ALTER ROLE u PASSWORD $é$hunter2$é$"},
		{"unterminated at the text limit", "ALTER ROLE u PASSWORD 'cut-in-the-mid"},
		{"lower case and a line break", "alter role u\n  password 'x'"},
		{"option of a user mapping", "CREATE USER MAPPING FOR u SERVER s OPTIONS (user 'bob', password 'sekret')"},
		{"quoted identifier option", `CREATE USER MAPPING FOR u SERVER s OPTIONS ("password" 'hunter2')`},
		{"unicode identifier option", `CREATE USER MAPPING FOR u SERVER s OPTIONS (U&"\0070assword" 'hunter2')`},
		{"unicode identifier option with UESCAPE", `CREATE USER MAPPING FOR u SERVER s OPTIONS (U&"!0070assword" UESCAPE '!' 'hunter2')`},
		{"nested in a dollar body", "DO $$ BEGIN EXECUTE 'ALTER ROLE u PASSWORD ''hunter2'''; END $$"},
		{"doubled quote then a blank in a body", "DO $$ BEGIN EXECUTE 'ALTER ROLE u PASSWORD '' hunter2 '''; END $$"},
		{"value built by concatenation in a body", "DO $$ BEGIN EXECUTE 'ALTER ROLE u PASSWORD ' || quote_literal('hunter2'); END $$"},
		{"value named earlier in a body", "DO $$ DECLARE pw text := 'hunter2'; BEGIN EXECUTE 'ALTER ROLE u PASSWORD ' || quote_literal(pw); END $$"},
		{"value passed to format", "DO $$ BEGIN EXECUTE format('ALTER ROLE u PASSWORD %L', 'hunter2'); END $$"},
		{"nested in an E-string body", `DO E'BEGIN EXECUTE \'ALTER ROLE u PASSWORD \'\'hunter2\'\'\'; END'`},
		{"nested E-string in an E-string body", `DO E'BEGIN EXECUTE \'ALTER ROLE u PASSWORD E\'\'hunter2\'\'\'; END'`},
		{"escaped quote in an E-string constant", `EXECUTE E'ALTER ROLE u PASSWORD \'hunter2\''`},
		{"doubled quotes in a plain constant", "EXECUTE 'ALTER ROLE u PASSWORD E''hunter2'''"},
		{"line feed joins two constants", "ALTER ROLE u PASSWORD 'hunt'\n'er2' LOGIN"},
		{"carriage return joins two constants", "ALTER ROLE u PASSWORD 'hunt'\r'er2' LOGIN"},
		{"CRLF joins two constants", "ALTER ROLE u PASSWORD 'hunt'\r\n'er2' LOGIN"},
		{"comment before the break joins two constants", "ALTER ROLE u PASSWORD 'hunt' -- c\n'er2' LOGIN"},
		{"E-string first part joins in E mode", "ALTER ROLE u PASSWORD E'hunt'\n'er\\'2' LOGIN"},
		{"unicode first part joins", "ALTER ROLE u PASSWORD U&'hunt'\n'er2' LOGIN"},
		{"constants on one line do not join", "ALTER ROLE u PASSWORD 'hunt' 'er2'"},
		{"libpq value with a doubled quote", "CREATE SUBSCRIPTION s CONNECTION 'host=p password=ab''cd' PUBLICATION pub"},
		{"libpq value with a backslash blank", `CREATE SUBSCRIPTION s CONNECTION 'host=p password=a\ b dbname=x' PUBLICATION pub`},
		{"libpq value in an E-string", `CREATE SUBSCRIPTION s CONNECTION E'host=p password=ab\'cd' PUBLICATION pub`},
		{"libpq value with a quote in a dollar body", "CREATE SUBSCRIPTION s CONNECTION $$host=p password=ab'cd dbname=x$$ PUBLICATION pub"},
		{"quoted libpq value with an escaped quote in a dollar body", `CREATE SUBSCRIPTION s CONNECTION $$host=p password='ab\'cd' dbname=x$$ PUBLICATION pub`},
		{"libpq value ends a dollar body", "CREATE SUBSCRIPTION s CONNECTION $$host=p password=abc$$"},
		{"libpq value ends a plain literal", "CREATE SUBSCRIPTION s CONNECTION 'host=p password=abc'"},
		{"libpq value with a doubled quote and no blank", "CREATE SUBSCRIPTION s CONNECTION 'host=p password=ab''cd dbname=x' PUBLICATION pub"},
		{"libpq value continued on the next line", "CREATE SUBSCRIPTION s CONNECTION 'host=p password=ab'\n'cd' PUBLICATION pub"},
		{"subscription connection", "CREATE SUBSCRIPTION s CONNECTION 'host=p password=abc dbname=x' PUBLICATION pub"},
		{"subscription quoted password", "CREATE SUBSCRIPTION s CONNECTION 'host=p password=''a b''' PUBLICATION pub"},
		{"subscription uri", "ALTER SUBSCRIPTION s CONNECTION 'postgresql://rep:abc@primary:5432/app'"},
		{"primary_conninfo", "ALTER SYSTEM SET primary_conninfo = 'host=p sslpassword = ''x y'''"},
		{"column set with a doubled quote", "UPDATE users SET password = 'ab''cd' WHERE id = 1"},
		{"column set", "UPDATE users SET password = 'abcd' WHERE id = 1"},
		{"setting with a doubled quote", "SET app.password = 'ab''cd'"},
		{"named argument", "SELECT f(password => 'hunter2')"},
		{"old named argument", "SELECT f(password := 'hunter2')"},
		{"column set from a backslash literal", `UPDATE users SET password = 'pa\'ss' WHERE id = 1`},
		{"column set from a concatenation", "UPDATE users SET password = 'hunt' || 'er2' WHERE id = 1"},
		{"sslpassword option of a user mapping", "CREATE USER MAPPING FOR u SERVER s OPTIONS (user 'bob', sslpassword 'sekret')"},
		{"quoted sslpassword option", `CREATE USER MAPPING FOR u SERVER s OPTIONS ("sslpassword" 'sekret')`},
		{"sslpassword option in a constant", "EXECUTE 'CREATE USER MAPPING FOR u SERVER s OPTIONS (sslpassword ''x'')'"},
		{"quoted name set in a body", `DO $$ BEGIN UPDATE t SET "password" = 'x'; END $$`},
		{"libpq value joined by ||", "SELECT dblink_connect('myconn', 'host=h dbname=app user=app password=' || 'pw123')"},
		{"libpq value continued by ||", "SELECT dblink_connect('host=h password=pw' || '123')"},
		{"libpq value filled by format", "SELECT dblink_connect(format('host=h password=%s', 'pw123'))"},
		{"libpq value partly filled by format", "SELECT dblink_connect(format('host=h password=pw%s dbname=x', '123'))"},
		{"libpq value from a variable of the body", "DO $$ DECLARE pw text := 'hunter2'; BEGIN PERFORM dblink_connect('host=h password=' || pw || ' dbname=x'); END $$"},
		{"libpq quoted value followed by more than a blank", "CREATE SUBSCRIPTION s CONNECTION 'host=h password=''ab''cd' PUBLICATION pub"},
		{"libpq value with an unclosed quote", "CREATE SUBSCRIPTION s CONNECTION 'host=h password=''ab cd' PUBLICATION pub"},
		{"libpq value as one argument of several", "SELECT dblink('host=h password=pw', 'SELECT 1')"},
		{"libpq value in a body passed to a function", "DO $$ BEGIN PERFORM dblink_connect('host=h password=' || 'pw123'); END $$"},
		{"column set with a doubled quote in a body", "DO $$ BEGIN UPDATE t SET password = 'ab''cd'; END $$"},
		{"named argument in a body", "DO $$ BEGIN PERFORM f(password => E'pw'); END $$"},
		{"column set in a body", "DO $$ BEGIN UPDATE t SET password = 'abcd' WHERE id = 1; END $$"},
		{"libpq value with a backslash before a quote", `SELECT dblink_connect('host=h password=ab\'cd dbname=x')`},
	} {
		require.Equal(t, withheldQuery, redactQueryText(tc.in), tc.name)
	}
}

// The shapes an adversarial review found leaking through the redactor that
// read the text as PostgreSQL does. None of SECRET, or of its halves SEC and
// RET, may survive.
func TestRedactQueryText_WithholdsReviewedLeaks(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		// A URI password built with format(), || or concat(), or joined across lines.
		"SELECT dblink_connect(format('postgresql://app:%s@db/app', 'SECRET'))",
		"SELECT dblink_connect('postgresql://app:' || 'SECRET' || '@db/app')",
		"SELECT dblink_connect(concat('postgresql://app:', 'SECRET', '@db/app'))",
		"ALTER SUBSCRIPTION s CONNECTION 'postgresql://rep:SEC'\n'RET@primary/app'",
		"SELECT dblink_connect('postgresql:' || '//app:SECRET@db/app')",
		// An earlier \' that standard_conforming_strings=off reads as a quote.
		`SELECT dblink_connect('O\'Brien', 'host=h password=SECRET dbname=d')`,
		`SET application_name = 'Bob\'s job'; SELECT dblink_connect('host=h password=SECRET')`,
		`INSERT INTO remote_servers (label, conninfo) VALUES ('Bob\'s replica', 'host=h user=u password=SECRET')`,
		`SELECT dblink_exec('O\'Brien', 'UPDATE users SET password = ''SECRET'' WHERE id = 1')`,
		`UPDATE t SET a = 'it\'s', password = $$SEC RET$$ WHERE id = 1`,
		// A prefixed or dollar-quoted value with a blank, in a body or a constant.
		"DO $$ BEGIN UPDATE users SET password = E'correct SECRET' WHERE id = 1; END $$",
		"DO $f$ BEGIN UPDATE users SET password = $p$correct SECRET$p$ WHERE id = 1; END $f$",
		"DO $$ BEGIN UPDATE users SET password = U&'correct SECRET' WHERE id = 1; END $$",
		"DO $$ BEGIN UPDATE users SET password = N'correct SECRET' WHERE id = 1; END $$",
		"DO $$ BEGIN UPDATE users SET password = ('correct SECRET') WHERE id = 1; END $$",
		"SELECT dblink_exec('host=h dbname=d', 'UPDATE users SET password = E''correct SECRET'' WHERE id = 1')",
		// A value after PASSWORD that is not the next constant.
		"UPDATE users SET password = N'SECRET' WHERE id = 1",
		"SELECT f(password => N'SECRET')",
		"SET app.password TO 'SECRET'",
		"ALTER ROLE u SET app.password TO 'SECRET'",
		"ALTER SYSTEM SET app.password TO 'SECRET'",
		"UPDATE users SET password = ('SECRET') WHERE id = 1",
		"UPDATE users SET password = text 'SECRET' WHERE id = 1",
		// A keyword or a scheme spelled with escapes.
		`SELECT dblink_connect(E'host=h \x70assword=SECRET')`,
		`SELECT dblink_connect(E'host=h \160assword=SECRET')`,
		`SELECT dblink_connect(E'host=h \u0070assword=SECRET')`,
		`SELECT dblink_connect(E'host=h \U00000070assword=SECRET')`,
		`SELECT dblink_connect(E'host=h \p\a\s\s\w\o\r\d=SECRET')`,
		`SELECT dblink_connect(U&'host=h \0070assword=SECRET')`,
		`SELECT dblink_connect(U&'host=h \+000070assword=SECRET')`,
		`SELECT dblink_connect(U&'host=h !0070assword=SECRET' UESCAPE '!')`,
		`SELECT dblink_connect(E'host=h password=\x27SEC RET\x27 dbname=x')`,
		`ALTER SUBSCRIPTION s CONNECTION E'postgresql\x3a//rep:SECRET\x40primary/app'`,
		`DO E'BEGIN ALTER ROLE u \x50ASSWORD ''SECRET''; END'`,
		// A libpq value continued after a byte that is not an operator.
		"SELECT dblink_connect(('host=h password=SEC') || 'RET')",
		`SELECT dblink_connect('host=h password=SEC' COLLATE "C" || 'RET')`,
		"SELECT dblink_connect(CAST('host=h password=SEC' AS text) || 'RET')",
		"SELECT dblink_connect(format('%s', 'host=h password=SEC') || 'RET')",
		// Comments, a key split across constants, and other libpq secrets.
		"ALTER ROLE u PASSWORD 'new' -- old one was 'SECRET'",
		"SELECT 1 /* host=h password=SECRET */",
		"SELECT dblink_connect('host=h pass' || 'word=SECRET')",
		"SELECT dblink_connect('host=h pass'\n'word=SECRET')",
		"SELECT dblink_connect('host=h oauth_client_secret=SECRET')",
		"SELECT id FROM users WHERE password LIKE 'SECRET'",
		"INSERT INTO accounts (login, passwd) VALUES ('bob', 'hunter2')",
		`SELECT dblink_connect(E'host=h passw\x4Frd=hunter2')`,
	} {
		out := redactQueryText(in)
		require.Equal(t, withheldQuery, out, in)
		require.NotContains(t, out, "SEC", in)
		require.NotContains(t, out, "RET", in)
	}
}

// A statement without a sign of a credential is kept as it is: a libpq string
// without a password, a database named postgres, a URL, backslashes that
// decode to no keyword, and text beyond ASCII.
func TestRedactQueryText_KeepsTextWithoutCredentials(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"SELECT pg_stat_statements_reset()",
		"CREATE ROLE u LOGIN VALID UNTIL 'infinity'",
		"CREATE SUBSCRIPTION s CONNECTION 'host=p dbname=x' PUBLICATION pub",
		"SELECT 'nothing hidden' FROM t WHERE note = 'x'",
		"SELECT dblink('host=h dbname=x', 'SELECT 1')",
		"SELECT datname FROM pg_database WHERE datname = 'postgres'::name",
		"SELECT * FROM postgres_fdw_get_connections()",
		"INSERT INTO links (url) VALUES ('https://example.com/a?b=c')",
		`SELECT x FROM t WHERE x ~ '\d+\s*$' AND y = E'line\n'`,
		`SELECT 'C:\Users\x', E'\x41\101\u0042'`,
		"SELECT 'café', 'Ünïcödé' FROM t",
		"SELECT $1, $2 FROM t WHERE id = $3",
	} {
		require.Equal(t, in, redactQueryText(in))
	}
}

// A text that only names a password, without setting one, is withheld too.
// That is the cost of not reading the text: such a statement loses its text,
// and the redaction stays on the safe side.
func TestRedactQueryText_WithholdsTextThatOnlyNamesAPassword(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"UPDATE users SET password = $1 WHERE id = $2",
		"ALTER ROLE u PASSWORD NULL",
		"SELECT id, password_hash FROM users WHERE id = $1",
		"SELECT id FROM tokens WHERE kind = 'password_reset'",
		"SELECT 'nothing secret' FROM t",
		"SELECT id FROM t /* bypass word */",
	} {
		require.Equal(t, withheldQuery, redactQueryText(in), in)
	}
}

// The readings of the text decode escapes the way an escape string (E
// prefix) and a Unicode string (U& prefix) do.
func TestDecodeEscapes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, escape, unicode string }{
		{`a\x70b`, "apb", `ax70b`},
		{`\160`, "p", `160`},
		{`\0070`, "\x070", "p"},
		{`\u0070`, "p", `u0070`},
		{`\U00000070`, "p", `U00000070`},
		{`\+000070`, "+000070", "p"},
		{`\p\a`, "pa", "pa"},
		{`\\x70`, `\x70`, `\x70`},
		{`\n\t`, "  ", "nt"},
		{`\x`, "x", "x"},
		{`\u12`, "u12", "u12"},
		{`end\`, `end\`, `end\`},
		{"no escapes", "no escapes", "no escapes"},
	} {
		require.Equal(t, tc.escape, decodeEscapeString(tc.in), "escape string %q", tc.in)
		require.Equal(t, tc.unicode, decodeUnicodeString(tc.in), "unicode string %q", tc.in)
	}
}

// The text is read a fixed number of times, so the time grows in proportion
// to its length, as a session text can be as long as the server allows.
func TestRedactQueryText_IsLinearInTextLength(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in   string
		kept bool
	}{
		{"ALTER ROLE u PASSWORD /*" + strings.Repeat("password/*", 60000) + strings.Repeat("*/", 60001), false},
		{strings.Repeat("$a$x", 150000), true},
		{strings.Repeat("U&'x'\n", 100000), true},
		{strings.Repeat(`\x7\u12\U1234567\+12345\0`, 50000), true},
		{"SELECT $$" + strings.Repeat("password=", 30000) + "x$$", false},
	} {
		start := time.Now()
		want := withheldQuery
		if tc.kept {
			want = tc.in
		}
		require.True(t, redactQueryText(tc.in) == want)
		require.Less(t, time.Since(start), 2*time.Second)
	}
}

// A secret written as SEKA and SEKB in two halves must not survive, in any
// spelling of the key, operator, encoding of the value or wrapper around the
// statement.
func TestRedactQueryText_NoHalfOfTheSecretSurvives(t *testing.T) {
	t.Parallel()
	keys := []string{"password", "PASSWORD", "sslpassword", `"password"`, `U&"\0070assword"`}
	operators := []string{" ", "=", " = ", " => ", " := ", " TO "}
	values := []string{
		"'SEKASEKB'",
		"'SEKA''SEKB'",
		`E'SEKA\'SEKB'`,
		`'SEKA\'SEKB'`,
		"$v$SEKASEKB$v$",
		"U&'SEKASEKB'",
		"N'SEKASEKB'",
		"('SEKASEKB')",
		"text 'SEKASEKB'",
		"'SEKA'\n'SEKB'",
		"'SEKA' || 'SEKB'",
	}
	conninfos := []string{
		"'host=h password=SEKASEKB dbname=x'",
		"'host=h password=''SEKA SEKB'' dbname=x'",
		"'host=h password=SEKA''SEKB dbname=x'",
		`E'host=h password=SEKA\'SEKB dbname=x'`,
		`'host=h password=SEKA\'SEKB dbname=x'`,
		"$c$host=h password=SEKA'SEKB dbname=x$c$",
		"'host=h password=SEKA'\n'SEKB dbname=x'",
		"'host=h password=SEKA' || 'SEKB dbname=x'",
		"'host=h password=' || 'SEKASEKB'",
		"format('host=h password=%s dbname=x', 'SEKASEKB')",
		"concat('host=h password=', 'SEKASEKB')",
		"'host=h dbname=x password=SEKASEKB'",
		"'host=h sslpassword = SEKASEKB'",
		"'host=h oauth_client_secret=SEKASEKB'",
		`E'host=h \x70assword=SEKASEKB'`,
		`U&'host=h \0070assword=SEKASEKB'`,
		"'postgresql://u:SEKASEKB@h/x'",
		"'postgres://u:SEKASEKB@h/x'",
		"format('postgresql://u:%s@h/x', 'SEKASEKB')",
		"'postgresql://u:' || 'SEKASEKB' || '@h/x'",
		`E'postgresql://u:SEKASEKB\x40h/x'`,
	}
	var statements []string
	for _, key := range keys {
		for _, op := range operators {
			for _, value := range values {
				statements = append(statements, "X "+key+op+value+" Y")
			}
		}
	}
	for _, conninfo := range conninfos {
		statements = append(statements,
			"SELECT dblink_connect("+conninfo+")",
			"SELECT dblink("+conninfo+", 'SELECT 1')",
			`SELECT dblink_connect('O\'Brien', `+conninfo+")",
			"CREATE SUBSCRIPTION s CONNECTION "+conninfo+" PUBLICATION p",
			"ALTER SYSTEM SET primary_conninfo = "+conninfo,
		)
	}
	var leaks []string
	for _, statement := range statements {
		for _, in := range []string{
			statement,
			"DO $f$ BEGIN " + statement + "; END $f$",
			"DO $f$ BEGIN EXECUTE '" + strings.ReplaceAll(statement, "'", "''") + "'; END $f$",
		} {
			if out := redactQueryText(in); strings.Contains(out, "SEKA") || strings.Contains(out, "SEKB") {
				leaks = append(leaks, fmt.Sprintf("%q -> %q", in, out))
			}
		}
	}
	require.Empty(t, leaks)
}
