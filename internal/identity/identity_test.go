package identity_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/manprint/pglens/internal/identity"
	"github.com/stretchr/testify/require"
)

func TestStore_CreatesOnFirstOpen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "identity.json")
	s, err := identity.Open(path)
	require.NoError(t, err)
	require.NotEqual(t, 0, s.AgentID())
	_, err = os.Stat(path)
	require.NoError(t, err)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	dirInfo, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0700), dirInfo.Mode().Perm())
	// No tmp file remains
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), "*.tmp"))
	require.NoError(t, err)
	require.Empty(t, matches)
}

func TestStore_AgentIDStableAcrossReopen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")
	s1, err := identity.Open(path)
	require.NoError(t, err)
	id1 := s1.AgentID()
	s2, err := identity.Open(path)
	require.NoError(t, err)
	require.Equal(t, id1, s2.AgentID())
}

func TestStore_InstanceIDStableAcrossReopen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")
	s1, err := identity.Open(path)
	require.NoError(t, err)
	fp := "abc123"
	id1, created, err := s1.InstanceID(fp)
	require.NoError(t, err)
	require.True(t, created)
	id2, created, err := s1.InstanceID(fp)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, id1, id2)

	s2, err := identity.Open(path)
	require.NoError(t, err)
	id3, created, err := s2.InstanceID(fp)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, id1, id3)
}

func TestStore_DistinctFingerprintsDistinctIDs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")
	s, err := identity.Open(path)
	require.NoError(t, err)
	a, _, err := s.InstanceID("fp1")
	require.NoError(t, err)
	b, _, err := s.InstanceID("fp2")
	require.NoError(t, err)
	require.NotEqual(t, a, b)
}

func TestStore_AtomicWrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")
	s, err := identity.Open(path)
	require.NoError(t, err)
	_, _, err = s.InstanceID("fp1")
	require.NoError(t, err)
	matches, err := filepath.Glob(filepath.Join(dir, "*.tmp"))
	require.NoError(t, err)
	require.Empty(t, matches)
}

func TestStore_CorruptFileIsAnError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")
	require.NoError(t, os.WriteFile(path, []byte("{"), 0600))
	_, err := identity.Open(path)
	require.Error(t, err)
}

func TestStore_Concurrent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")
	s, err := identity.Open(path)
	require.NoError(t, err)
	errCh := make(chan error, 20)
	for i := 0; i < 20; i++ {
		go func(n int) {
			fp := "fp-common"
			if n%2 == 1 {
				fp = "fp-other"
			}
			_, _, e := s.InstanceID(fp)
			errCh <- e
		}(i)
	}
	for i := 0; i < 20; i++ {
		require.NoError(t, <-errCh)
	}
	// Verify file is still valid
	s2, err := identity.Open(path)
	require.NoError(t, err)
	require.NotEqual(t, 0, s2.AgentID())
}

func TestStore_OpenNilInstances(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")
	// Create file with agent_id but no instances key (nil map)
	s1, err := identity.Open(path)
	require.NoError(t, err)
	agentID := s1.AgentID()
	// Overwrite file with missing instances
	data := `{"agent_id":"` + agentID.String() + `","version":1}`
	require.NoError(t, os.WriteFile(path, []byte(data), 0600))
	s2, err := identity.Open(path)
	require.NoError(t, err)
	require.Equal(t, agentID, s2.AgentID())
	// Should be able to allocate instance
	id, created, err := s2.InstanceID("new-fp")
	require.NoError(t, err)
	require.True(t, created)
	require.NotEqual(t, 0, id)
}

func TestStore_OpenMissingAgentID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"instances":{},"version":1}`), 0600))
	_, err := identity.Open(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing agent_id")
}
