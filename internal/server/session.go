package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
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

const (
	sessionCookieName       = "pglens_session"
	sessionLoginMaxBody     = 4 << 10
	sessionLoginFailureWait = 250 * time.Millisecond
)

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

// RegisterSessionRoutes installs the browser login, status, and logout
// endpoints. The GET and POST endpoints remain middleware exemptions so an
// unauthenticated browser can establish and inspect its session state.
func RegisterSessionRoutes(r chi.Router, cfg UIConfig, store *SessionStore) {
	r.Post("/api/v1/session", func(w http.ResponseWriter, req *http.Request) {
		var payload struct {
			Password string `json:"password"`
		}

		decoder := json.NewDecoder(http.MaxBytesReader(w, req.Body, sessionLoginMaxBody))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&payload); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON", "request body must contain a password")
			return
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			writeError(w, http.StatusBadRequest, "invalid JSON", "request body must contain one JSON object")
			return
		}

		passwordMatches := cfg.Password != "" && constantTimePasswordMatch(payload.Password, cfg.Password)
		if !passwordMatches {
			time.Sleep(sessionLoginFailureWait)
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid password")
			return
		}
		if store == nil {
			writeError(w, http.StatusServiceUnavailable, "session_unavailable", "session store is not configured")
			return
		}

		token, _, err := store.Create()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "session_unavailable", "could not create session")
			return
		}
		http.SetCookie(w, sessionCookie(cfg, req, token, int(sessionTTL(cfg, store)/time.Second)))
		w.WriteHeader(http.StatusNoContent)
	})

	r.Get("/api/v1/session", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if store != nil {
			if cookie, err := req.Cookie(sessionCookieName); err == nil {
				if expiresAt, ok := store.Validate(cookie.Value); ok {
					writeJSON(w, http.StatusOK, map[string]any{
						"authenticated": true,
						"expires_at":    expiresAt.Format(time.RFC3339Nano),
					})
					return
				}
			}
		}
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"authenticated": false,
			"configured":    cfg.Password != "",
		})
	})

	r.Delete("/api/v1/session", func(w http.ResponseWriter, req *http.Request) {
		if store != nil {
			if cookie, err := req.Cookie(sessionCookieName); err == nil {
				store.Delete(cookie.Value)
			}
		}
		http.SetCookie(w, sessionCookie(cfg, req, "", -1))
		w.WriteHeader(http.StatusNoContent)
	})
}

func sessionTTL(cfg UIConfig, store *SessionStore) time.Duration {
	if cfg.SessionTTL > 0 {
		return cfg.SessionTTL
	}
	if store != nil && store.ttl > 0 {
		return store.ttl
	}
	return 24 * time.Hour
}

func constantTimePasswordMatch(got, want string) bool {
	if len(got) != len(want) {
		_ = subtle.ConstantTimeCompare([]byte(got), []byte(want))
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func sessionCookie(cfg UIConfig, req *http.Request, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secureCookie(cfg, req),
		SameSite: http.SameSiteStrictMode,
	}
}

func secureCookie(cfg UIConfig, req *http.Request) bool {
	switch strings.ToLower(strings.TrimSpace(cfg.CookieSecure)) {
	case "true":
		return true
	case "false":
		return false
	default:
		return req.TLS != nil || strings.EqualFold(strings.TrimSpace(req.Header.Get("X-Forwarded-Proto")), "https")
	}
}
