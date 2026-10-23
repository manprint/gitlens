package main

import (
	"bytes"
	"context"
	"net"
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
