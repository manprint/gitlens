package server

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSessionStoreCreateValidateDeleteAndExpiry(t *testing.T) {
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	store := NewSessionStore(time.Hour)
	store.now = func() time.Time { return now }

	token, expiresAt, err := store.Create()
	require.NoError(t, err)
	require.Len(t, token, 43)
	require.NotContains(t, token, "=")
	require.Equal(t, now.Add(time.Hour), expiresAt)
	require.Equal(t, 1, store.Len())

	gotExpiry, ok := store.Validate(token)
	require.True(t, ok)
	require.Equal(t, expiresAt, gotExpiry)
	_, ok = store.Validate("not-a-session")
	require.False(t, ok)

	now = now.Add(time.Hour)
	_, ok = store.Validate(token)
	require.False(t, ok)
	store.Delete(token)
	require.Equal(t, 0, store.Len())
}

func TestSessionStoreReapsExpiredSessions(t *testing.T) {
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	store := NewSessionStore(time.Hour)
	store.now = func() time.Time { return now }

	_, _, err := store.Create()
	require.NoError(t, err)
	now = now.Add(time.Hour)
	for i := 0; i < 1024; i++ {
		_, _, err = store.Create()
		require.NoError(t, err)
	}
	require.Equal(t, 1024, store.Len())

	now = now.Add(time.Hour)
	store.reap()
	require.Equal(t, 0, store.Len())
}

func TestSessionStorePropagatesRandomnessFailure(t *testing.T) {
	original := sessionRandRead
	t.Cleanup(func() { sessionRandRead = original })
	wantErr := errors.New("random source unavailable")
	sessionRandRead = func([]byte) (int, error) { return 0, wantErr }

	store := NewSessionStore(time.Hour)
	token, expiresAt, err := store.Create()
	require.ErrorIs(t, err, wantErr)
	require.Empty(t, token)
	require.True(t, expiresAt.IsZero())
	require.Equal(t, 0, store.Len())
}
