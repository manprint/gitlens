package server

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sync"
	"time"
)

// SessionStore keeps UI sessions in memory. Sessions are intentionally lost
// when the server restarts; this store has no persistence or background
// lifecycle to manage.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]time.Time
	ttl      time.Duration
	now      func() time.Time
}

var sessionRandRead = rand.Read

func NewSessionStore(ttl time.Duration) *SessionStore {
	return &SessionStore{
		sessions: make(map[string]time.Time),
		ttl:      ttl,
		now:      time.Now,
	}
}

func (s *SessionStore) Create() (token string, expiresAt time.Time, err error) {
	var raw [32]byte
	if _, err := sessionRandRead(raw[:]); err != nil {
		return "", time.Time{}, fmt.Errorf("generate session token: %w", err)
	}

	token = base64.RawURLEncoding.EncodeToString(raw[:])
	now := s.now()
	expiresAt = now.Add(s.ttl)

	s.mu.Lock()
	s.sessions[token] = expiresAt
	needReap := len(s.sessions) > 1024
	s.mu.Unlock()
	if needReap {
		s.reap()
	}
	return token, expiresAt, nil
}

func (s *SessionStore) Validate(token string) (expiresAt time.Time, ok bool) {
	// A map lookup is appropriate here: tokens are high-entropy map keys, not
	// a secret compared against a candidate. A timing-safe map does not exist,
	// and a linear scan would only add work without improving security.
	now := s.now()
	s.mu.RLock()
	expiresAt, ok = s.sessions[token]
	s.mu.RUnlock()
	if !ok || !now.Before(expiresAt) {
		return time.Time{}, false
	}
	return expiresAt, true
}

func (s *SessionStore) Delete(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

func (s *SessionStore) reap() {
	now := s.now()
	s.mu.Lock()
	for token, expiresAt := range s.sessions {
		if !now.Before(expiresAt) {
			delete(s.sessions, token)
		}
	}
	s.mu.Unlock()
}

func (s *SessionStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}
