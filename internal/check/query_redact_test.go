package check

import (
	"testing"

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
		{"dollar quoted runs to the end", "CREATE ROLE u PASSWORD $$a'b$$ VALID UNTIL 'x'",
			"CREATE ROLE u PASSWORD '[redacted]'"},
		{"unterminated at the text limit", "ALTER ROLE u PASSWORD 'cut-in-the-mid",
			"ALTER ROLE u PASSWORD '[redacted]'"},
		{"lower case and a line break", "alter role u\n  password 'x'",
			"alter role u\n  password '[redacted]'"},
		{"option of a user mapping", "CREATE USER MAPPING FOR u SERVER s OPTIONS (user 'bob', password 'sekret')",
			"CREATE USER MAPPING FOR u SERVER s OPTIONS (user 'bob', password '[redacted]')"},
		{"subscription connection", "CREATE SUBSCRIPTION s CONNECTION 'host=p password=abc dbname=x' PUBLICATION pub",
			"CREATE SUBSCRIPTION s CONNECTION 'host=p password=[redacted] dbname=x' PUBLICATION pub"},
		{"subscription quoted password", "CREATE SUBSCRIPTION s CONNECTION 'host=p password=''a b''' PUBLICATION pub",
			"CREATE SUBSCRIPTION s CONNECTION 'host=p password=[redacted]' PUBLICATION pub"},
		{"subscription uri", "ALTER SUBSCRIPTION s CONNECTION 'postgresql://rep:abc@primary:5432/app'",
			"ALTER SUBSCRIPTION s CONNECTION 'postgresql://rep:[redacted]@primary:5432/app'"},
		{"primary_conninfo", "ALTER SYSTEM SET primary_conninfo = 'host=p sslpassword = ''x y'''",
			"ALTER SYSTEM SET primary_conninfo = 'host=p sslpassword = [redacted]'"},
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
		"CREATE TABLE t (password text DEFAULT 'x')",
		"SELECT id, password_hash FROM users WHERE id = $1",
		"SELECT pg_stat_statements_reset()",
		"CREATE ROLE u LOGIN VALID UNTIL 'infinity'",
	} {
		require.Equal(t, in, redactQueryText(in))
	}
}
