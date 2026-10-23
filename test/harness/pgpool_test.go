//go:build e2e

package harness

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPGPool_RedialsTheCurrentPort proves a cached pool dials the port docker
// publishes now, not the one it was built with. The test puts a stand-in
// `docker` first on PATH that prints whatever port the test writes, so the
// port can move the way a container restart moves it.
func TestPGPool_RedialsTheCurrentPort(t *testing.T) {
	bin := t.TempDir()
	portFile := filepath.Join(bin, "published-port")
	setPublishedPort := func(port int) {
		t.Helper()
		if err := os.WriteFile(portFile, []byte(fmt.Sprintf("127.0.0.1:%d\n", port)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	setPublishedPort(40001)
	script := "#!/bin/sh\ncat '" + portFile + "'\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	h := &Harness{
		projectName: "pglens-pgpool-test",
		composeDir:  t.TempDir(),
		pools:       make(map[string]*pgxpool.Pool),
	}
	pool := h.pgPool(t, "pg", "postgres", "postgres", "postgres")
	t.Cleanup(pool.Close)

	beforeConnect := pool.Config().BeforeConnect
	if beforeConnect == nil {
		t.Fatal("pool has no BeforeConnect hook, so it keeps the port it was built with")
	}

	// Nothing has moved yet: the hook keeps the port the pool was built with.
	cc := parseTestConn(t, 40001)
	if err := beforeConnect(context.Background(), cc); err != nil {
		t.Fatalf("BeforeConnect: %v", err)
	}
	if cc.Port != 40001 {
		t.Fatalf("before the restart: dial port %d, want 40001", cc.Port)
	}

	// A restart publishes a new port. The next new connection must use it,
	// even though the pool itself is the same cached one.
	setPublishedPort(40002)
	cc = parseTestConn(t, 40001)
	if err := beforeConnect(context.Background(), cc); err != nil {
		t.Fatalf("BeforeConnect after restart: %v", err)
	}
	if cc.Port != 40002 {
		t.Fatalf("after the restart: dial port %d, want 40002 (the port docker now publishes)", cc.Port)
	}
	if again, ok := h.pools["pg"]; !ok || again != pool {
		t.Fatal("pgPool did not keep the cached pool for the service")
	}
}

// parseTestConn returns a connection config for localhost on port, as
// pgxpool builds one before it runs the BeforeConnect hook.
func parseTestConn(t *testing.T, port int) *pgx.ConnConfig {
	t.Helper()
	cc, err := pgx.ParseConfig(fmt.Sprintf("postgres://postgres:postgres@localhost:%d/postgres?sslmode=disable", port))
	if err != nil {
		t.Fatal(err)
	}
	return cc
}
