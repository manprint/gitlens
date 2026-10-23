package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// serverChildEnv marks the re-executed test binary as the server process.
const serverChildEnv = "PGLENS_TEST_SERVER_CHILD"

// TestServerExitsNonZeroWhenListenFails runs the server in a child process,
// with its listen address already taken. The server used to log the failure
// and exit with status 0, which a supervisor reads as a clean stop and does
// not restart. The child gets an explicit environment with no database, so it
// cannot reach one.
func TestServerExitsNonZeroWhenListenFails(t *testing.T) {
	if os.Getenv(serverChildEnv) == "1" {
		main()
		return
	}
	held, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = held.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestServerExitsNonZeroWhenListenFails$")
	cmd.Env = []string{
		serverChildEnv + "=1",
		"PGLENS_LISTEN=" + held.Addr().String(),
		"PGLENS_BOOTSTRAP_TOKEN=test-bootstrap-token",
		"PGLENS_DSN=",
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "a server that cannot listen must exit with an error status")
	require.Equal(t, 1, exitErr.ExitCode())
	// log.Fatalf also exits 1, so the status alone would pass for an unrelated
	// failure: the listen error must be the reason.
	require.Contains(t, stderr.String(), "listen:")
	require.Contains(t, stderr.String(), "address already in use")
}

// TestShutdownCancelsRequestsAfterItsBudget: a request still running when the
// shutdown budget ends must see its context cancelled, so that it releases the
// connection it holds. Shutdown alone does not cancel it, and the pool close
// that follows used to wait for it. The client keeps its connection open, so a
// disconnect cannot cancel the request and hide the fault.
func TestShutdownCancelsRequestsAfterItsBudget(t *testing.T) {
	requestCtx, cancelRequests := context.WithCancel(context.Background())
	defer cancelRequests()
	entered := make(chan struct{})
	released := make(chan struct{})
	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(released)
	})
	srv := newHTTPServer("127.0.0.1:0", handler, requestCtx)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(ln) }()

	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		resp, getErr := http.Get("http://" + ln.Addr().String() + "/")
		if getErr == nil {
			_ = resp.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the request never reached the handler")
	}

	err = shutdownServer(srv, cancelRequests, 200*time.Millisecond)
	require.ErrorIs(t, err, context.DeadlineExceeded, "the request is still running when the budget ends")
	select {
	case <-released:
	case <-time.After(10 * time.Second):
		t.Fatal("the request was not cancelled after the shutdown budget")
	}
	select {
	case <-clientDone:
	case <-time.After(10 * time.Second):
		t.Fatal("the client never got the response to its cancelled request")
	}
}

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
