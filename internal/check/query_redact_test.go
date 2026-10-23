package check

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Credentials in utility statements are redacted, in every spelling a role,
// a server or user mapping, or a subscription can take. Everything else in
// the text is kept.
func TestRedactQueryText_RemovesCredentials(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, in, want string }{
		{"create role", "CREATE ROLE tmp_secret_probe LOGIN PASSWORD 'hunter2-probe'",
			"CREATE ROLE tmp_secret_probe LOGIN PASSWORD '[redacted]'"},
		{"alter role", "ALTER ROLE tmp_secret_probe PASSWORD 'hunter2-probe-2'",
			"ALTER ROLE tmp_secret_probe PASSWORD '[redacted]'"},
		{"encrypted and unencrypted", "CREATE USER u ENCRYPTED PASSWORD 'md5abc'; ALTER USER u UNENCRYPTED PASSWORD 'plain'",
			"CREATE USER u ENCRYPTED PASSWORD '[redacted]'; ALTER USER u UNENCRYPTED PASSWORD '[redacted]'"},
		{"doubled quote inside the literal", "ALTER ROLE u PASSWORD 'it''s secret' LOGIN",
			"ALTER ROLE u PASSWORD '[redacted]' LOGIN"},
		{"escape string", `ALTER ROLE u PASSWORD E'pa\'ss' LOGIN`,
			"ALTER ROLE u PASSWORD '[redacted]' LOGIN"},
		{"no blank before the literal", "ALTER ROLE u PASSWORD'hunter2'",
			"ALTER ROLE u PASSWORD'[redacted]'"},
		{"comment before the literal", "CREATE USER u PASSWORD/**/'hunter2'",
			"CREATE USER u PASSWORD/**/'[redacted]'"},
		{"nested comment before the literal", "ALTER ROLE u PASSWORD /* a /* b */ c */ 'hunter2'",
			"ALTER ROLE u PASSWORD /* a /* b */ c */ '[redacted]'"},
		{"nested comment with a quote before the literal", "ALTER ROLE u PASSWORD /* a /* b */ 'x' c */ 'hunter2'",
			"ALTER ROLE u PASSWORD /* a /* b */ 'x' c */ '[redacted]'"},
		{"three levels of nested comment", "ALTER ROLE u PASSWORD /* a /* b /* c */ 'x' */ 'y' */ 'hunter2'",
			"ALTER ROLE u PASSWORD /* a /* b /* c */ 'x' */ 'y' */ '[redacted]'"},
		{"line marker inside a block comment", "ALTER ROLE u PASSWORD /* -- */ 'hunter2'",
			"ALTER ROLE u PASSWORD /* -- */ '[redacted]'"},
		{"comment with a quote before the literal", "ALTER ROLE u PASSWORD /* it's */ 'hunter2'",
			"ALTER ROLE u PASSWORD /* it's */ '[redacted]'"},
		{"line comment before the literal", "ALTER ROLE u PASSWORD --c\n'hunter2'",
			"ALTER ROLE u PASSWORD --c\n'[redacted]'"},
		{"carriage return ends a line comment", "ALTER ROLE u PASSWORD --c\r'hunter2' LOGIN\n'x'",
			"ALTER ROLE u PASSWORD --c\r'[redacted]' LOGIN\n'x'"},
		{"line comment and blanks before the literal", "ALTER ROLE u PASSWORD -- it's a note\n  'hunter2' LOGIN",
			"ALTER ROLE u PASSWORD -- it's a note\n  '[redacted]' LOGIN"},
		// With standard_conforming_strings off the backslash escapes the quote, so
		// the password is pa'ss, and with it on the literal ends after the
		// backslash. The text is not known here, so a plain literal with a
		// backslash redacts everything after it.
		{"backslash in a plain literal", `ALTER ROLE u PASSWORD 'pa\'ss' LOGIN`,
			"ALTER ROLE u PASSWORD '[redacted]'"},
		{"two statements after a backslash", `ALTER ROLE a PASSWORD 'x\'; ALTER ROLE b PASSWORD 'hunter2'`,
			"ALTER ROLE a PASSWORD '[redacted]'"},
		{"keyword after a backslash literal", `SELECT 'a\'; ALTER ROLE b PASSWORD 'hunter2'`,
			"SELECT '[redacted]'"},
		{"backslash at the text limit", `ALTER ROLE u PASSWORD 'cut-here\`,
			"ALTER ROLE u PASSWORD '[redacted]'"},
		{"escape string cut after a backslash", `ALTER ROLE u PASSWORD E'cut-here\`,
			"ALTER ROLE u PASSWORD '[redacted]'"},
		{"unicode escape literal", "ALTER ROLE u PASSWORD U&'hunter2'",
			"ALTER ROLE u PASSWORD '[redacted]'"},
		{"option without a blank", "CREATE USER MAPPING FOR u SERVER s OPTIONS (password'hunter2')",
			"CREATE USER MAPPING FOR u SERVER s OPTIONS (password'[redacted]')"},
		// A dollar-quoted value ends at its closing tag, which is not the end of
		// the statement.
		{"dollar quoted ends at its tag", "CREATE ROLE u PASSWORD $$a'b$$ VALID UNTIL 'x'",
			"CREATE ROLE u PASSWORD '[redacted]' VALID UNTIL 'x'"},
		{"dollar quoted with a non-ASCII tag", "ALTER ROLE u PASSWORD $é$hunter2$é$",
			"ALTER ROLE u PASSWORD '[redacted]'"},
		{"unterminated at the text limit", "ALTER ROLE u PASSWORD 'cut-in-the-mid",
			"ALTER ROLE u PASSWORD '[redacted]'"},
		{"lower case and a line break", "alter role u\n  password 'x'",
			"alter role u\n  password '[redacted]'"},
		{"option of a user mapping", "CREATE USER MAPPING FOR u SERVER s OPTIONS (user 'bob', password 'sekret')",
			"CREATE USER MAPPING FOR u SERVER s OPTIONS (user 'bob', password '[redacted]')"},
		{"quoted identifier option", `CREATE USER MAPPING FOR u SERVER s OPTIONS ("password" 'hunter2')`,
			`CREATE USER MAPPING FOR u SERVER s OPTIONS ("password" '[redacted]')`},
		{"unicode identifier option", `CREATE USER MAPPING FOR u SERVER s OPTIONS (U&"\0070assword" 'hunter2')`,
			`CREATE USER MAPPING FOR u SERVER s OPTIONS (U&"\0070assword" '[redacted]')`},
		{"unicode identifier option with UESCAPE", `CREATE USER MAPPING FOR u SERVER s OPTIONS (U&"!0070assword" UESCAPE '!' 'hunter2')`,
			`CREATE USER MAPPING FOR u SERVER s OPTIONS (U&"!0070assword" UESCAPE '!' '[redacted]')`},
		// A keyword inside a constant or a body may be run by EXECUTE, so the text
		// from that constant to the end is redacted.
		{"nested in a dollar body", "DO $$ BEGIN EXECUTE 'ALTER ROLE u PASSWORD ''hunter2'''; END $$",
			"DO '[redacted]'"},
		{"doubled quote then a blank in a body", "DO $$ BEGIN EXECUTE 'ALTER ROLE u PASSWORD '' hunter2 '''; END $$",
			"DO '[redacted]'"},
		{"value built by concatenation in a body", "DO $$ BEGIN EXECUTE 'ALTER ROLE u PASSWORD ' || quote_literal('hunter2'); END $$",
			"DO '[redacted]'"},
		{"value named earlier in a body", "DO $$ DECLARE pw text := 'hunter2'; BEGIN EXECUTE 'ALTER ROLE u PASSWORD ' || quote_literal(pw); END $$",
			"DO '[redacted]'"},
		{"value passed to format", "DO $$ BEGIN EXECUTE format('ALTER ROLE u PASSWORD %L', 'hunter2'); END $$",
			"DO '[redacted]'"},
		{"nested in an E-string body", `DO E'BEGIN EXECUTE \'ALTER ROLE u PASSWORD \'\'hunter2\'\'\'; END'`,
			"DO '[redacted]'"},
		{"nested E-string in an E-string body", `DO E'BEGIN EXECUTE \'ALTER ROLE u PASSWORD E\'\'hunter2\'\'\'; END'`,
			"DO '[redacted]'"},
		{"escaped quote in an E-string constant", `EXECUTE E'ALTER ROLE u PASSWORD \'hunter2\''`,
			"EXECUTE '[redacted]'"},
		{"doubled quotes in a plain constant", "EXECUTE 'ALTER ROLE u PASSWORD E''hunter2'''",
			"EXECUTE '[redacted]'"},
		// Two constants with a line break between them are one value.
		{"line feed joins two constants", "ALTER ROLE u PASSWORD 'hunt'\n'er2' LOGIN",
			"ALTER ROLE u PASSWORD '[redacted]' LOGIN"},
		{"carriage return joins two constants", "ALTER ROLE u PASSWORD 'hunt'\r'er2' LOGIN",
			"ALTER ROLE u PASSWORD '[redacted]' LOGIN"},
		{"CRLF joins two constants", "ALTER ROLE u PASSWORD 'hunt'\r\n'er2' LOGIN",
			"ALTER ROLE u PASSWORD '[redacted]' LOGIN"},
		{"comment before the break joins two constants", "ALTER ROLE u PASSWORD 'hunt' -- c\n'er2' LOGIN",
			"ALTER ROLE u PASSWORD '[redacted]' LOGIN"},
		{"E-string first part joins in E mode", "ALTER ROLE u PASSWORD E'hunt'\n'er\\'2' LOGIN",
			"ALTER ROLE u PASSWORD '[redacted]' LOGIN"},
		{"unicode first part joins", "ALTER ROLE u PASSWORD U&'hunt'\n'er2' LOGIN",
			"ALTER ROLE u PASSWORD '[redacted]' LOGIN"},
		{"constants on one line do not join", "ALTER ROLE u PASSWORD 'hunt' 'er2'",
			"ALTER ROLE u PASSWORD '[redacted]' 'er2'"},
		{"libpq value with a doubled quote", "CREATE SUBSCRIPTION s CONNECTION 'host=p password=ab''cd' PUBLICATION pub",
			"CREATE SUBSCRIPTION s CONNECTION 'host=p password=[redacted]' PUBLICATION pub"},
		{"libpq value with a backslash blank", `CREATE SUBSCRIPTION s CONNECTION 'host=p password=a\ b dbname=x' PUBLICATION pub`,
			`CREATE SUBSCRIPTION s CONNECTION 'host=p password=[redacted] dbname=x' PUBLICATION pub`},
		{"libpq value in an E-string", `CREATE SUBSCRIPTION s CONNECTION E'host=p password=ab\'cd' PUBLICATION pub`,
			`CREATE SUBSCRIPTION s CONNECTION E'host=p password=[redacted]' PUBLICATION pub`},
		// In a dollar body a quote is a plain character, so the value keeps it.
		{"libpq value with a quote in a dollar body", "CREATE SUBSCRIPTION s CONNECTION $$host=p password=ab'cd dbname=x$$ PUBLICATION pub",
			"CREATE SUBSCRIPTION s CONNECTION $$host=p password=[redacted] dbname=x$$ PUBLICATION pub"},
		{"quoted libpq value with an escaped quote in a dollar body", `CREATE SUBSCRIPTION s CONNECTION $$host=p password='ab\'cd' dbname=x$$ PUBLICATION pub`,
			`CREATE SUBSCRIPTION s CONNECTION $$host=p password=[redacted] dbname=x$$ PUBLICATION pub`},
		{"libpq value ends a dollar body", "CREATE SUBSCRIPTION s CONNECTION $$host=p password=abc$$",
			"CREATE SUBSCRIPTION s CONNECTION $$host=p password=[redacted]$$"},
		{"libpq value ends a plain literal", "CREATE SUBSCRIPTION s CONNECTION 'host=p password=abc'",
			"CREATE SUBSCRIPTION s CONNECTION 'host=p password=[redacted]'"},
		{"libpq value with a doubled quote and no blank", "CREATE SUBSCRIPTION s CONNECTION 'host=p password=ab''cd dbname=x' PUBLICATION pub",
			"CREATE SUBSCRIPTION s CONNECTION 'host=p password=[redacted] dbname=x' PUBLICATION pub"},
		// Constants joined by a line break are one libpq string, so the value
		// goes on into the next constant.
		{"libpq value continued on the next line", "CREATE SUBSCRIPTION s CONNECTION 'host=p password=ab'\n'cd' PUBLICATION pub",
			"CREATE SUBSCRIPTION s CONNECTION 'host=p password=[redacted]' PUBLICATION pub"},
		{"subscription connection", "CREATE SUBSCRIPTION s CONNECTION 'host=p password=abc dbname=x' PUBLICATION pub",
			"CREATE SUBSCRIPTION s CONNECTION 'host=p password=[redacted] dbname=x' PUBLICATION pub"},
		{"subscription quoted password", "CREATE SUBSCRIPTION s CONNECTION 'host=p password=''a b''' PUBLICATION pub",
			"CREATE SUBSCRIPTION s CONNECTION 'host=p password=[redacted]' PUBLICATION pub"},
		{"subscription uri", "ALTER SUBSCRIPTION s CONNECTION 'postgresql://rep:abc@primary:5432/app'",
			"ALTER SUBSCRIPTION s CONNECTION 'postgresql://rep:[redacted]@primary:5432/app'"},
		{"primary_conninfo", "ALTER SYSTEM SET primary_conninfo = 'host=p sslpassword = ''x y'''",
			"ALTER SYSTEM SET primary_conninfo = 'host=p sslpassword = [redacted]'"},
		// A password column or parameter set with an operator takes the whole
		// constant after it, which is SQL, not a libpq string.
		{"column set with a doubled quote", "UPDATE users SET password = 'ab''cd' WHERE id = 1",
			"UPDATE users SET password = '[redacted]' WHERE id = 1"},
		{"column set", "UPDATE users SET password = 'abcd' WHERE id = 1",
			"UPDATE users SET password = '[redacted]' WHERE id = 1"},
		{"setting with a doubled quote", "SET app.password = 'ab''cd'",
			"SET app.password = '[redacted]'"},
		{"named argument", "SELECT f(password => 'hunter2')",
			"SELECT f(password => '[redacted]')"},
		{"old named argument", "SELECT f(password := 'hunter2')",
			"SELECT f(password := '[redacted]')"},
		{"column set from a backslash literal", `UPDATE users SET password = 'pa\'ss' WHERE id = 1`,
			"UPDATE users SET password = '[redacted]'"},
		{"column set from a concatenation", "UPDATE users SET password = 'hunt' || 'er2' WHERE id = 1",
			"UPDATE users SET password = '[redacted]'"},
		{"sslpassword option of a user mapping", "CREATE USER MAPPING FOR u SERVER s OPTIONS (user 'bob', sslpassword 'sekret')",
			"CREATE USER MAPPING FOR u SERVER s OPTIONS (user 'bob', sslpassword '[redacted]')"},
		{"quoted sslpassword option", `CREATE USER MAPPING FOR u SERVER s OPTIONS ("sslpassword" 'sekret')`,
			`CREATE USER MAPPING FOR u SERVER s OPTIONS ("sslpassword" '[redacted]')`},
		{"sslpassword option in a constant", "EXECUTE 'CREATE USER MAPPING FOR u SERVER s OPTIONS (sslpassword ''x'')'",
			"EXECUTE '[redacted]'"},
		{"quoted name set in a body", `DO $$ BEGIN UPDATE t SET "password" = 'x'; END $$`,
			"DO '[redacted]'"},
		// A libpq value the constant does not hold in full redacts the text from
		// that constant to the end.
		{"libpq value joined by ||", "SELECT dblink_connect('myconn', 'host=h dbname=app user=app password=' || 'pw123')",
			"SELECT dblink_connect('myconn', '[redacted]'"},
		{"libpq value continued by ||", "SELECT dblink_connect('host=h password=pw' || '123')",
			"SELECT dblink_connect('[redacted]'"},
		{"libpq value filled by format", "SELECT dblink_connect(format('host=h password=%s', 'pw123'))",
			"SELECT dblink_connect(format('[redacted]'"},
		{"libpq value partly filled by format", "SELECT dblink_connect(format('host=h password=pw%s dbname=x', '123'))",
			"SELECT dblink_connect(format('[redacted]'"},
		// The value reads as a closed quoted value, but it is a variable set
		// earlier in the body.
		{"libpq value from a variable of the body", "DO $$ DECLARE pw text := 'hunter2'; BEGIN PERFORM dblink_connect('host=h password=' || pw || ' dbname=x'); END $$",
			"DO '[redacted]'"},
		{"libpq quoted value followed by more than a blank", "CREATE SUBSCRIPTION s CONNECTION 'host=h password=''ab''cd' PUBLICATION pub",
			"CREATE SUBSCRIPTION s CONNECTION '[redacted]'"},
		{"libpq value with an unclosed quote", "CREATE SUBSCRIPTION s CONNECTION 'host=h password=''ab cd' PUBLICATION pub",
			"CREATE SUBSCRIPTION s CONNECTION '[redacted]'"},
		{"libpq value as one argument of several", "SELECT dblink('host=h password=pw', 'SELECT 1')",
			"SELECT dblink('[redacted]'"},
		{"libpq value in a body passed to a function", "DO $$ BEGIN PERFORM dblink_connect('host=h password=' || 'pw123'); END $$",
			"DO '[redacted]'"},
		{"column set with a doubled quote in a body", "DO $$ BEGIN UPDATE t SET password = 'ab''cd'; END $$",
			"DO '[redacted]'"},
		{"named argument in a body", "DO $$ BEGIN PERFORM f(password => E'pw'); END $$",
			"DO '[redacted]'"},
		{"column set in a body", "DO $$ BEGIN UPDATE t SET password = 'abcd' WHERE id = 1; END $$",
			"DO $$ BEGIN UPDATE t SET password = [redacted] WHERE id = 1; END $$"},
		{"libpq value with a backslash before a quote", `SELECT dblink_connect('host=h password=ab\'cd dbname=x')`,
			"SELECT dblink_connect('[redacted]'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, redactQueryText(tc.in))
		})
	}
}

// Text that names a password without setting one is not changed, and neither
// is a column called password or a statement without credentials.
func TestRedactQueryText_KeepsTextWithoutCredentials(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"ALTER ROLE u PASSWORD NULL",
		"ALTER ROLE u PASSWORD $1",
		"CREATE TABLE t (password text DEFAULT 'x')",
		`SELECT "password" FROM t`,
		`CREATE TABLE t ("password" text DEFAULT 'x')`,
		"SELECT id, password_hash FROM users WHERE id = $1",
		"SELECT id FROM tokens WHERE kind = 'password_reset' AND used = false",
		`SELECT 'a\b', password_hash FROM users WHERE id = 1`,
		"SELECT pg_stat_statements_reset()",
		"CREATE ROLE u LOGIN VALID UNTIL 'infinity'",
		"CREATE SUBSCRIPTION s CONNECTION 'host=p dbname=x' PUBLICATION pub",
		"SELECT 'nothing secret' FROM t WHERE note = 'x'",
		"UPDATE users SET password = $1 WHERE id = $2",
		"SELECT id FROM users WHERE password IS NULL AND note = 'x'",
		"SELECT dblink('host=h dbname=x', 'SELECT 1')",
	} {
		require.Equal(t, in, redactQueryText(in))
	}
}

// A constant or a body that holds the word password cuts the rest of the
// statement, because the text may be run later by EXECUTE. This is a known
// cost: the statement loses its tail, and the redaction stays on the safe side.
func TestRedactQueryText_PasswordWordInAConstantCutsTheRest(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{"SELECT 'password reset' FROM t WHERE id = 1", "SELECT '[redacted]'"},
		{"INSERT INTO log(msg) VALUES ('password changed')", "INSERT INTO log(msg) VALUES ('[redacted]'"},
	} {
		require.Equal(t, tc.want, redactQueryText(tc.in), tc.in)
	}
}

// The text is read once from its start, so a statement that holds many
// PASSWORD keywords costs time in proportion to its length. A rescan of the
// comment for each keyword would take seconds at this size.
func TestRedactQueryText_IsLinearInTextLength(t *testing.T) {
	t.Parallel()
	in := "ALTER ROLE u PASSWORD /*" + strings.Repeat("password/*", 60000) + strings.Repeat("*/", 60001)
	start := time.Now()
	require.Equal(t, in, redactQueryText(in))
	require.Less(t, time.Since(start), 2*time.Second)
}

// Dollar tags and unicode constants joined by line breaks are read in time
// linear in the text, as a session text can be as long as the server allows.
func TestRedactQueryText_IsLinearForDollarTagsAndUnicodeChains(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		strings.Repeat("$a$x", 150000),
		strings.Repeat("U&'x'\n", 100000),
	} {
		start := time.Now()
		require.True(t, redactQueryText(in) == in, "text without a credential is kept")
		require.Less(t, time.Since(start), 2*time.Second)
	}
}

// Many libpq keys in one body are read in time linear in the text. The keys
// inside a value are part of it, and each of them would read a value that
// runs to the end of the body, so reading a value from every key would be
// quadratic.
func TestRedactQueryText_IsLinearForLibpqKeys(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"SELECT $$" + strings.Repeat("password=", 30000) + "x$$",
		"SELECT $$" + strings.Repeat("password='x ", 100000) + "$$",
		"SELECT $$" + strings.Repeat("password=x ", 100000) + "$$ AS c",
	} {
		start := time.Now()
		require.NotContains(t, redactQueryText(in), "x ")
		require.Less(t, time.Since(start), 2*time.Second)
	}
}

// The text a constant stands for is read with its doubled quotes, escapes and
// joined constants, and each of its bytes maps back to the statement bytes it
// was read from.
func TestConstantText_MapsEachByteToTheStatement(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{"'a''b'", "a'b"},
		{`E'a\'b\\c\nd'`, `a'b\c\nd`},
		{"'ab'\n'cd'", "abcd"},
		{"'ab' -- c\r\n  'cd'", "abcd"},
		{"$t$a'b$t$", "a'b"},
		{"$t$open", "open"},
		{"'open", "open"},
		{"U&'a''b'", "a'b"},
	} {
		tok := scanToken(tc.in, 0)
		text, from, to := constantText(tc.in, tok, true)
		require.Equal(t, tc.want, text, tc.in)
		require.Len(t, from, len(text))
		require.Len(t, to, len(text))
		for i := range len(text) {
			raw := tc.in[from[i]:to[i]]
			require.True(t, raw == text[i:i+1] || len(raw) == 2 && raw[1] == text[i],
				"%q: byte %d read from %q", tc.in, i, raw)
		}
	}
}

// A secret written as SEKA and SEKB in two halves must not survive, in any
// spelling of the key, operator, encoding of the value or wrapper around the
// statement that the redactor reads. Some of these shapes cut the rest of the
// statement; the test asks only that no half of the secret is left.
func TestRedactQueryText_NoHalfOfTheSecretSurvives(t *testing.T) {
	t.Parallel()
	keys := []string{"password", "PASSWORD", "sslpassword", `"password"`, `U&"\0070assword"`}
	operators := []string{" ", "=", " = ", " => ", " := "}
	values := []string{
		"'SEKASEKB'",
		"'SEKA''SEKB'",
		`E'SEKA\'SEKB'`,
		`'SEKA\'SEKB'`,
		"$v$SEKASEKB$v$",
		"U&'SEKASEKB'",
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
		"'postgresql://u:SEKASEKB@h/x'",
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
