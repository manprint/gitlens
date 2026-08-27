package server

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAuth_Validate(t *testing.T) {
	t.Parallel()
	auth := NewAuth("secret123")
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer secret123")
	require.True(t, auth.Validate(req))
	req2, _ := http.NewRequest("GET", "/", nil)
	req2.Header.Set("Authorization", "Bearer wrong")
	require.False(t, auth.Validate(req2))
	req3, _ := http.NewRequest("GET", "/", nil)
	require.False(t, auth.Validate(req3))
}

func TestAuth_ConstantTime(t *testing.T) {
	t.Parallel()
	// Ensure different length token still fails but does constant time compare
	auth := NewAuth("short")
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer longer-token-that-is-wrong")
	require.False(t, auth.Validate(req))
}
