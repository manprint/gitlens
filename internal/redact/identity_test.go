package redact

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A free-text identity is cut to 64 bytes, on a character boundary, and is
// withheld when it may hold a credential, cut or whole.
func TestIdentity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, in, want string }{
		{"short name is shipped as it is", "orders_gid", "orders_gid"},
		{"empty name stays empty", "", ""},
		{"long name is cut to 64 bytes", strings.Repeat("a", 100), strings.Repeat("a", 64)},
		{"cut keeps a whole multi-byte character", strings.Repeat("a", 63) + "é", strings.Repeat("a", 63)},
		{"credential word is withheld", "password=hunter2", Withheld},
		{"credential word past the cut is withheld", strings.Repeat("x", 70) + "password", Withheld},
		{"connection URI is withheld", "postgres://u:p@h/db", Withheld},
	} {
		require.Equal(t, tc.want, Identity(tc.in), tc.name)
	}
}

// Identity is applied again to a value that was already shipped in its
// withheld or cut form, and must not change it.
func TestIdentityIsIdempotent(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"short", strings.Repeat("b", 90), "password", Withheld} {
		once := Identity(in)
		require.Equal(t, once, Identity(once), "input %q", in)
	}
}
