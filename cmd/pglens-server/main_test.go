package main

import (
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRunHealthcheck_ProbesTheListenAddress: a server bound to one address does
// not answer on loopback, so the probe must dial the address it bound.
func TestRunHealthcheck_ProbesTheListenAddress(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.2:0")
	if err != nil {
		t.Skipf("no 127.0.0.2 loopback address on this host: %v", err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	t.Setenv("PGLENS_LISTEN", ln.Addr().String())

	require.Equal(t, 0, runHealthcheck())
}

func TestHealthcheckURL(t *testing.T) {
	tests := []struct{ listen, want string }{
		{":8080", "http://127.0.0.1:8080/readyz"},
		{"0.0.0.0:8080", "http://127.0.0.1:8080/readyz"},
		{"[::]:8080", "http://127.0.0.1:8080/readyz"},
		{"127.0.0.1:8080", "http://127.0.0.1:8080/readyz"},
		{"10.0.0.5:8080", "http://10.0.0.5:8080/readyz"},
		{"[::1]:8080", "http://[::1]:8080/readyz"},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, healthcheckURL(tt.listen, "/readyz"), "listen %s", tt.listen)
	}
}
