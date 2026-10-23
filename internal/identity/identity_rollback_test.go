package identity

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// failPersist makes the next persist fail: the temporary file it writes is a
// directory, which WriteFile cannot replace, whoever runs the test.
func failPersist(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.Mkdir(path+".tmp", 0o700))
}

// A DSN whose fingerprint is new, and that the store cannot persist, leaves no
// entry behind, so the next attempt allocates the id afresh.
func TestStore_InstanceIDForDSN_RollsBackNewEntryOnPersistFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	s, err := Open(path)
	require.NoError(t, err)
	dsn := "postgres://pglens@db.internal:5432/app"
	fp, err := Fingerprint(dsn)
	require.NoError(t, err)

	failPersist(t, path)
	_, _, err = s.InstanceIDForDSN(dsn)
	require.Error(t, err)
	require.NotContains(t, s.f.Instances, fp, "the failed entry is rolled back")

	require.NoError(t, os.Remove(path+".tmp"))
	id, created, err := s.InstanceIDForDSN(dsn)
	require.NoError(t, err)
	require.True(t, created, "nothing was stored, so the id is allocated now")
	require.NotEqual(t, uuid.Nil, id)
}

// An entry moved from the legacy fingerprint of a query password must come back
// where it was when its move cannot be persisted: the server keeps its id and
// the legacy entry stays in the file for the next attempt.
func TestStore_InstanceIDForDSN_RollsBackMigrationOnPersistFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	s, err := Open(path)
	require.NoError(t, err)
	dsn := "postgres://u@h/db?password=x"
	legacy := legacyFingerprint(dsn)
	require.NotEmpty(t, legacy, "a query password changes the fingerprint")
	fp, err := Fingerprint(dsn)
	require.NoError(t, err)
	legacyID := uuid.New()
	s.f.Instances[legacy] = legacyID
	require.NoError(t, s.persist())

	failPersist(t, path)
	_, _, err = s.InstanceIDForDSN(dsn)
	require.Error(t, err)
	require.Equal(t, legacyID, s.f.Instances[legacy], "the legacy entry is restored")
	require.NotContains(t, s.f.Instances, fp, "the moved entry is rolled back")

	require.NoError(t, os.Remove(path+".tmp"))
	id, created, err := s.InstanceIDForDSN(dsn)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, legacyID, id)
	require.NotContains(t, s.f.Instances, legacy, "the legacy entry moves once it is persisted")
}
