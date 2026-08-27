package identity_test

import (
	"testing"

	"github.com/manprint/pglens/internal/identity"
	"github.com/stretchr/testify/require"
)

func TestFingerprint_PasswordIgnored(t *testing.T) {
	t.Parallel()
	a, err := identity.Fingerprint("postgres://u:secret1@h/db")
	require.NoError(t, err)
	b, err := identity.Fingerprint("postgres://u:secret2@h/db")
	require.NoError(t, err)
	require.Equal(t, a, b)
}

func TestFingerprint_NeverLeaksDSN(t *testing.T) {
	t.Parallel()
	fp, err := identity.Fingerprint("postgres://u:secret@h/db")
	require.NoError(t, err)
	require.Len(t, fp, 64)
	require.NotContains(t, fp, "secret")
	require.NotContains(t, fp, "u@")
	require.NotContains(t, fp, "/db")
	// Must be lowercase hex
	for _, c := range fp {
		require.True(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'))
	}
}

func TestFingerprint_Normalization(t *testing.T) {
	t.Parallel()
	a, err := identity.Fingerprint("postgres://U@Host/db")
	require.NoError(t, err)
	b, err := identity.Fingerprint("postgres://U@host:5432/db")
	require.NoError(t, err)
	c, err := identity.Fingerprint("postgres://U@host/db?application_name=x")
	require.NoError(t, err)
	require.Equal(t, a, b)
	require.Equal(t, a, c)

	// Changing host, port, user or database changes fingerprint
	d, err := identity.Fingerprint("postgres://U@otherhost/db")
	require.NoError(t, err)
	require.NotEqual(t, a, d)

	e, err := identity.Fingerprint("postgres://U@host:5433/db")
	require.NoError(t, err)
	require.NotEqual(t, a, e)

	f, err := identity.Fingerprint("postgres://otheruser@host/db")
	require.NoError(t, err)
	require.NotEqual(t, a, f)

	g, err := identity.Fingerprint("postgres://U@host/otherdb")
	require.NoError(t, err)
	require.NotEqual(t, a, g)

	// Query params that don't affect identity are ignored, others are kept
	h, err := identity.Fingerprint("postgres://U@host/db?sslmode=require")
	require.NoError(t, err)
	require.Equal(t, a, h)

	i, err := identity.Fingerprint("postgres://U@host/db?connect_timeout=10")
	require.NoError(t, err)
	require.Equal(t, a, i)

	j, err := identity.Fingerprint("postgres://U@host/db?custom=1")
	require.NoError(t, err)
	require.NotEqual(t, a, j)

	// Sorted query params produce same fingerprint regardless of order
	k, err := identity.Fingerprint("postgres://U@host/db?b=2&a=1")
	require.NoError(t, err)
	l, err := identity.Fingerprint("postgres://U@host/db?a=1&b=2")
	require.NoError(t, err)
	require.Equal(t, k, l)
}

func TestFingerprint_Errors(t *testing.T) {
	t.Parallel()
	_, err := identity.Fingerprint("")
	require.Error(t, err)
	_, err = identity.Fingerprint("mysql://u@h/db")
	require.Error(t, err)
	_, err = identity.Fingerprint("postgres:///db")
	require.Error(t, err)
	_, err = identity.Fingerprint("not a dsn")
	require.Error(t, err)
}

func FuzzFingerprint(f *testing.F) {
	f.Add("postgres://u@h/db")
	f.Fuzz(func(t *testing.T, s string) {
		fp, err := identity.Fingerprint(s)
		if err != nil {
			return
		}
		require.Len(t, fp, 64)
		for _, c := range fp {
			require.True(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'))
		}
	})
}
