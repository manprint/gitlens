package server

import (
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
	got := ""
	if h := r.Header.Get("Authorization"); len(h) > 7 && h[:7] == "Bearer " {
		got = h[7:]
	}
	if len(got) != len(a.token) {
		// Still do constant-time compare to avoid timing leak on length
		_ = subtle.ConstantTimeCompare([]byte(got), []byte(a.token))
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(a.token)) == 1
}
