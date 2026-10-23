//go:build e2e

package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestDumpContainerInspect_InspectsEveryContainerOfTheStack proves the dump
// asks compose for every container of the stack, stopped ones included, with
// the compose files passed explicitly, and keeps the inspect JSON. A stand-in
// `docker` on PATH answers compose ps and inspect and logs its arguments.
func TestDumpContainerInspect_InspectsEveryContainerOfTheStack(t *testing.T) {
	bin := t.TempDir()
	argsLog := filepath.Join(bin, "args.log")
	inspectJSON := `[{"Id":"cid-1","State":{"Status":"exited"}}]`
	script := `#!/bin/sh
echo "$@" >> '` + argsLog + `'
case "$1" in
  compose) printf 'cid-1\ncid-2\n' ;;
  inspect) printf '%s\n' '` + inspectJSON + `' ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	h := &Harness{
		projectName:  "pglens-dump-test",
		composeDir:   t.TempDir(),
		baseFile:     "base.yml",
		topologyFile: "topo.yml",
		agentFile:    "agent.yml",
	}
	artifactDir := t.TempDir()
	dumpContainerInspect(h, artifactDir, t)

	raw, err := os.ReadFile(filepath.Join(artifactDir, "docker-inspect.json"))
	if err != nil {
		t.Fatalf("docker-inspect.json not written: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("docker-inspect.json is not JSON: %v (%q)", err, raw)
	}
	if len(got) != 1 || got[0]["Id"] != "cid-1" {
		t.Errorf("inspect output: got %v", got)
	}

	calls, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(calls)), "\n")
	if len(lines) != 2 {
		t.Fatalf("docker calls: got %d, want compose ps and inspect: %q", len(lines), calls)
	}
	wantPS := "compose -p pglens-dump-test -f base.yml -f topo.yml -f agent.yml ps -a -q"
	if lines[0] != wantPS {
		t.Errorf("compose ps call: got %q, want %q", lines[0], wantPS)
	}
	if wantInspect := "inspect cid-1 cid-2"; lines[1] != wantInspect {
		t.Errorf("inspect call: got %q, want %q", lines[1], wantInspect)
	}
}

// TestWriteMetricTables_DumpsRecentRowsOfEveryHypertable proves the metrics
// dump lists the hypertables of the schema, counts their rows, and keeps the
// newest rows, leaving out plain tables. It runs against a throwaway
// TimescaleDB container that the test starts and removes itself.
func TestWriteMetricTables_DumpsRecentRowsOfEveryHypertable(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not available")
	}
	const image = "timescale/timescaledb:2.29.0-pg17"
	name := fmt.Sprintf("pglens-probe-dump-%d", time.Now().UnixNano())
	out, err := exec.Command("docker", "run", "-d", "--name", name,
		"--label", "pglens.probe=1",
		"-e", "POSTGRES_USER=pglens", "-e", "POSTGRES_PASSWORD=pglens", "-e", "POSTGRES_DB=pglens",
		"-p", "127.0.0.1::5432", image).CombinedOutput()
	if err != nil {
		t.Fatalf("docker run %s: %v: %s", image, err, out)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })

	portOut, err := exec.Command("docker", "port", name, "5432").Output()
	if err != nil {
		t.Fatalf("docker port %s: %v", name, err)
	}
	var port int
	if _, err := fmt.Sscanf(string(portOut), "127.0.0.1:%d", &port); err != nil {
		t.Fatalf("parse published port %q: %v", portOut, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	dsn := fmt.Sprintf("postgres://pglens:pglens@127.0.0.1:%d/pglens?sslmode=disable", port)
	pool, err := waitForPool(ctx, dsn)
	if err != nil {
		t.Fatalf("timescaledb never accepted connections: %v", err)
	}
	defer pool.Close()

	setup := []string{
		`CREATE TABLE metrics (ts timestamptz NOT NULL, value double precision NOT NULL)`,
		`SELECT create_hypertable('metrics', by_range('ts'))`,
		`INSERT INTO metrics SELECT now() - (g || ' seconds')::interval, g FROM generate_series(1, 150) g`,
		`CREATE TABLE metrics_bloat (ts timestamptz NOT NULL, value double precision NOT NULL)`,
		`SELECT create_hypertable('metrics_bloat', by_range('ts'))`,
		`INSERT INTO metrics_bloat SELECT now() - (g || ' seconds')::interval, g FROM generate_series(1, 3) g`,
		`CREATE TABLE instances (id int PRIMARY KEY)`,
	}
	for _, stmt := range setup {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("setup %q: %v", stmt, err)
		}
	}

	artifactDir := t.TempDir()
	writeMetricTables(ctx, pool, artifactDir, t)

	raw, err := os.ReadFile(filepath.Join(artifactDir, "metrics-tables.json"))
	if err != nil {
		t.Fatalf("metrics-tables.json not written: %v", err)
	}
	var got map[string]struct {
		Rows   int64             `json:"rows"`
		Recent []json.RawMessage `json:"recent"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("metrics-tables.json is not JSON: %v", err)
	}
	var keys []string
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if want := []string{"metrics", "metrics_bloat"}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("dumped tables: got %v, want only the hypertables %v", keys, want)
	}
	if got["metrics"].Rows != 150 {
		t.Errorf("metrics rows: got %d, want 150", got["metrics"].Rows)
	}
	// The dump keeps the 100 newest rows; the literal guards against a change
	// to dumpTableRecentRows that would otherwise pass unnoticed.
	if n := len(got["metrics"].Recent); n != 100 {
		t.Fatalf("metrics recent rows: got %d, want 100", n)
	}
	// ts is now() minus g seconds, so the newest row has the smallest value.
	var newest struct {
		Value float64 `json:"value"`
	}
	if err := json.Unmarshal(got["metrics"].Recent[0], &newest); err != nil {
		t.Fatal(err)
	}
	if newest.Value != 1 {
		t.Errorf("newest metrics row: value %v, want 1", newest.Value)
	}
}

// waitForPool retries until the database accepts a query, since the image
// runs a temporary server during initdb before the real one starts.
func waitForPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	for {
		pool, err := pgxpool.New(ctx, dsn)
		if err == nil {
			if _, err = pool.Exec(ctx, "SELECT 1"); err == nil {
				return pool, nil
			}
			pool.Close()
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("%w (last error: %v)", ctx.Err(), err)
		case <-time.After(time.Second):
		}
	}
}
