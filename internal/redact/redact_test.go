package redact

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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
