package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
)

// Auth validates the bootstrap token using constant-time compare.
type Auth struct {
	token string
}

func NewAuth(token string) *Auth {
	return &Auth{token: token}
}

func (a *Auth) Validate(r *http.Request) bool {
	// An unconfigured token must never authenticate anything. Without this,
	// a server built with an empty token accepted every request that carried
	// no Authorization header at all (both sides being the empty string made
	// the constant-time compare below succeed), silently turning the ingest
	// endpoint and the whole read API into anonymous surfaces.
	if a == nil || a.token == "" {
		return false
	}
	got := ""
	if h := r.Header.Get("Authorization"); len(h) > 7 && h[:7] == "Bearer " {
		got = h[7:]
	}
	return secretsEqual(got, a.token)
}

// secretsEqual compares two secrets without revealing their lengths.
// ConstantTimeCompare returns at once when the lengths differ, so comparing
// the secrets directly would tell a caller how long the configured one is.
// The digests of both are always the same size.
func secretsEqual(got, want string) bool {
	gotDigest := sha256.Sum256([]byte(got))
	wantDigest := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(gotDigest[:], wantDigest[:]) == 1
}
