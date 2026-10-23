package identity_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/manprint/pglens/internal/identity"
	"github.com/stretchr/testify/require"
)

// The fingerprints below are pinned as they were stored before the query
// password left the identity, and as they are now. An agent that ran before
// the change holds the legacy one in identity.json.
const (
	legacyQueryPasswordFingerprint = "9f446a88657af205cba2edf758b97c97cdfeefc9f6d70417a50308b589727610" // postgres://u@h/db?password=x
	userinfoFingerprint            = "7f8cb20c9e597fc44c8cfd19959caab753e7724873da1187d2a397a153e2348a" // postgres://u@h/db
)

// A password in the query is a secret, not part of the server's identity: a
// rotated one must not register the same server as a new instance.
func TestFingerprint_QueryPasswordIgnored(t *testing.T) {
	t.Parallel()
	plain, err := identity.Fingerprint("postgres://u@h/db")
	require.NoError(t, err)
	require.Equal(t, userinfoFingerprint, plain)
	for _, dsn := range []string{
		"postgres://u@h/db?password=x",
		"postgres://u@h/db?password=rotated",
		"postgres://u@h/db?sslpassword=key",
		"postgres://u:x@h/db",
	} {
		fp, err := identity.Fingerprint(dsn)
		require.NoError(t, err, dsn)
		require.Equal(t, plain, fp, dsn)
	}
}

// writeIdentityFile writes an identity.json holding the given instances, as an
// agent from before this change would have left it.
func writeIdentityFile(t *testing.T, path string, instances map[string]uuid.UUID) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	data, err := json.Marshal(map[string]any{"agent_id": uuid.New(), "instances": instances, "version": 1})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0600))
}

// An instance recorded under the fingerprint of a query password keeps its id
// after the upgrade, and the hash over that password is removed from the file.
func TestStore_InstanceIDForDSN_MigratesLegacyQueryPassword(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "identity.json")
	legacyID := uuid.New()
	writeIdentityFile(t, path, map[string]uuid.UUID{legacyQueryPasswordFingerprint: legacyID})

	s, err := identity.Open(path)
	require.NoError(t, err)
	id, created, err := s.InstanceIDForDSN("postgres://u@h/db?password=x")
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, legacyID, id)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(raw), legacyQueryPasswordFingerprint)

	reopened, err := identity.Open(path)
	require.NoError(t, err)
	again, created, err := reopened.InstanceIDForDSN("postgres://u@h/db?password=rotated")
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, legacyID, again)
}

// A DSN without a query password has no legacy fingerprint, so it leaves other
// entries alone.
func TestStore_InstanceIDForDSN_UserinfoPasswordLeavesLegacyEntry(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "identity.json")
	writeIdentityFile(t, path, map[string]uuid.UUID{legacyQueryPasswordFingerprint: uuid.New()})

	s, err := identity.Open(path)
	require.NoError(t, err)
	_, created, err := s.InstanceIDForDSN("postgres://u:x@h/db")
	require.NoError(t, err)
	require.True(t, created)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(raw), legacyQueryPasswordFingerprint)
}

// A DSN whose query password changes keeps the instance id it was given first.
func TestStore_InstanceIDForDSN_RotatedQueryPasswordKeepsID(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "identity.json")
	s, err := identity.Open(path)
	require.NoError(t, err)
	first, created, err := s.InstanceIDForDSN("postgres://u@h/db?password=x")
	require.NoError(t, err)
	require.True(t, created)
	second, created, err := s.InstanceIDForDSN("postgres://u@h/db?password=y")
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, first, second)
}
