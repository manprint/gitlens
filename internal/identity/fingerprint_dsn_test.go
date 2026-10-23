package identity_test

import (
	"testing"

	"github.com/manprint/pglens/internal/identity"
	"github.com/stretchr/testify/require"
)

// The fingerprints of authority-form DSNs are the instance ids already stored
// in identity.json. A change to any of them re-mints every instance on upgrade,
// so these values are pinned: a refactor of Fingerprint must reproduce them.
func TestFingerprint_AuthorityFormUnchanged(t *testing.T) {
	t.Parallel()
	pinned := map[string]string{
		"postgres://pglens@db.internal:5432/app":                      "1d990f07602d1e920f667f25ced988ba36359a22f6e53f47865f10126e7b850b",
		"postgres://u:secret@Host/db?application_name=x&a=1&b=2":      "748963750899a64c5fb7e91fe6f1e8352663c9739b2014f204729b8a2d724674",
		"postgres://U@host/db?custom=1":                               "083d57f8fd475a63d7b96caa3fe4635e9550dc437554e6c6d7a9c5b24993d332",
		"postgresql://monitor@10.0.0.5:6432/postgres?sslmode=require": "176922f2cbbfb7ab902666d6dadd376c58c43dbe49be14ef07c8763ae2ae46fc",
		"postgres://u@[::1]:5432/db":                                  "1960a3ac2b484e9cb65256a5d82eb9de523620937fd8dd77d754ccbe19487110",
		"postgres://u@localhost/db?host=%2Ftmp":                       "54ee0a37b755c5e2324702446cff7f55e166bce2ea5f476caab7362346bda424",
	}
	for dsn, want := range pinned {
		got, err := identity.Fingerprint(dsn)
		require.NoError(t, err, dsn)
		require.Equal(t, want, got, dsn)
	}
}

// A DSN whose authority has no host names its server in the query: a Unix
// socket directory, or a host and port, as pgx reads them.
func TestFingerprint_SocketAndHostInQuery(t *testing.T) {
	t.Parallel()
	socket, err := identity.Fingerprint("postgres://pglens@/app?host=/var/run/postgresql")
	require.NoError(t, err)

	// The password is still dropped.
	withPassword, err := identity.Fingerprint("postgres://pglens:secret@/app?host=/var/run/postgresql")
	require.NoError(t, err)
	require.Equal(t, socket, withPassword)

	// Another socket directory, or another port on the same one, is another server.
	otherDir, err := identity.Fingerprint("postgres://pglens@/app?host=/tmp")
	require.NoError(t, err)
	require.NotEqual(t, socket, otherDir)
	otherPort, err := identity.Fingerprint("postgres://pglens@/app?host=/var/run/postgresql&port=5433")
	require.NoError(t, err)
	require.NotEqual(t, socket, otherPort)

	// A socket directory is a path, so its case is kept.
	otherCase, err := identity.Fingerprint("postgres://pglens@/app?host=/Var/Run/postgresql")
	require.NoError(t, err)
	require.NotEqual(t, socket, otherCase)

	// The same server spelt with its host in the authority or in the query
	// is the same instance.
	authority, err := identity.Fingerprint("postgres://pglens@127.0.0.1:5432/app")
	require.NoError(t, err)
	inQuery, err := identity.Fingerprint("postgres://pglens@/app?host=127.0.0.1&port=5432")
	require.NoError(t, err)
	require.Equal(t, authority, inQuery)

	defaultPort, err := identity.Fingerprint("postgres://pglens@127.0.0.1/app")
	require.NoError(t, err)
	require.Equal(t, authority, defaultPort)

	ipv6Authority, err := identity.Fingerprint("postgres://pglens@[::1]/app")
	require.NoError(t, err)
	ipv6InQuery, err := identity.Fingerprint("postgres://pglens@/app?host=::1")
	require.NoError(t, err)
	require.Equal(t, ipv6Authority, ipv6InQuery)

	// A DSN that names no host connects to the local default socket: one
	// identity for it, distinct from any port or database.
	local, err := identity.Fingerprint("postgres:///app")
	require.NoError(t, err)
	localAgain, err := identity.Fingerprint("postgres://@/app")
	require.NoError(t, err)
	require.Equal(t, local, localAgain)
	localPort, err := identity.Fingerprint("postgres:///app?port=5433")
	require.NoError(t, err)
	require.NotEqual(t, local, localPort)
	otherDB, err := identity.Fingerprint("postgres:///other")
	require.NoError(t, err)
	require.NotEqual(t, local, otherDB)
}
