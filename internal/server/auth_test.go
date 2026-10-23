package server

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAuth_Validate(t *testing.T) {
	t.Parallel()
	auth := NewAuth("secret123")
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer secret123")
	require.True(t, auth.Validate(req))
	req2, _ := http.NewRequest("GET", "/", nil)
	req2.Header.Set("Authorization", "Bearer wrong")
	require.False(t, auth.Validate(req2))
	req3, _ := http.NewRequest("GET", "/", nil)
	require.False(t, auth.Validate(req3))
}

// An unconfigured token must never authenticate: the empty configured token
// and the empty presented token used to compare equal, so a server built
// without a token accepted every unauthenticated request.
func TestAuth_EmptyTokenNeverValidates(t *testing.T) {
	t.Parallel()
	auth := NewAuth("")

	anonymous, _ := http.NewRequest("GET", "/", nil)
	require.False(t, auth.Validate(anonymous))

	emptyBearer, _ := http.NewRequest("GET", "/", nil)
	emptyBearer.Header.Set("Authorization", "Bearer ")
	require.False(t, auth.Validate(emptyBearer))

	guessed, _ := http.NewRequest("GET", "/", nil)
	guessed.Header.Set("Authorization", "Bearer anything")
	require.False(t, auth.Validate(guessed))

	var nilAuth *Auth
	require.False(t, nilAuth.Validate(anonymous))
}

func TestAuth_ConstantTime(t *testing.T) {
	t.Parallel()
	// Ensure different length token still fails but does constant time compare
	auth := NewAuth("short")
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer longer-token-that-is-wrong")
	require.False(t, auth.Validate(req))
}

// The comparison must be right for every relation between the two secrets: a
// digest that is equal for different inputs would be a bypass, and a length
// check that short-circuits would be the timing leak the digests remove.
func TestSecretsEqual(t *testing.T) {
	t.Parallel()
	const secret = "a-much-longer-configured-password-value"
	tests := map[string]struct {
		got  string
		want bool
	}{
		"equal":               {secret, true},
		"same length differs": {"a-much-longer-configured-password-valuX", false},
		"prefix":              {secret[:10], false},
		"longer":              {secret + "!", false},
		"empty guess":         {"", false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, secretsEqual(tc.got, secret))
		})
	}
}
