package server

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
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

var sessionExemptPaths = map[string]struct{}{
	"/healthz":             {},
	"/readyz":              {},
	"/metrics":             {},
	"GET /api/v1/session":  {},
	"POST /api/v1/session": {},
}

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

func RequireCredential(cfg UIConfig, auth *Auth, store *SessionStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.Enabled || sessionRequestExempt(r) {
				next.ServeHTTP(w, r)
				return
			}
			if auth != nil && auth.Validate(r) {
				next.ServeHTTP(w, r)
				return
			}
			if cfg.Password == "" {
				writeError(w, http.StatusServiceUnavailable, "ui_password_not_configured", "configure PGLENS_UI_PASSWORD or PGLENS_UI_PASSWORD_FILE")
				return
			}
			if store != nil {
				if cookie, err := r.Cookie("pglens_session"); err == nil {
					if _, ok := store.Validate(cookie.Value); ok {
						next.ServeHTTP(w, r)
						return
					}
				}
			}
			writeError(w, http.StatusUnauthorized, "unauthorized", "sign in or present an agent token")
		})
	}
}

func sessionRequestExempt(r *http.Request) bool {
	path := r.URL.Path
	if !strings.HasPrefix(path, "/api/") {
		return true
	}
	if _, ok := sessionExemptPaths[r.Method+" "+path]; ok {
		return true
	}
	_, ok := sessionExemptPaths[path]
	return ok
}
