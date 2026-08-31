package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAPIDocsGeneratorIsDeterministic(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate generator test source")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
	spec, err := os.ReadFile(filepath.Join(repoRoot, "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read OpenAPI document: %v", err)
	}

	first, err := generate(spec)
	if err != nil {
		t.Fatalf("first generation: %v", err)
	}
	second, err := generate(spec)
	if err != nil {
		t.Fatalf("second generation: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("generator output is not byte-identical across runs")
	}

	checkedIn, err := os.ReadFile(filepath.Join(repoRoot, "docs", "api.md"))
	if err != nil {
		t.Fatalf("read checked-in API reference: %v", err)
	}
	if !bytes.Equal(first, checkedIn) {
		t.Fatal("docs/api.md is stale; run make api-docs")
	}
}

func TestAPIDocsGeneratorValidationAndOrdering(t *testing.T) {
	tests := []struct {
		name string
		spec string
		want string
	}{
		{name: "invalid yaml", spec: "paths: [", want: "parse OpenAPI document"},
		{name: "missing paths", spec: "paths: {}", want: "has no paths"},
		{name: "missing operation id", spec: "paths:\n  /x:\n    get:\n      tags: [x]\n", want: "has no operationId"},
		{name: "missing tags", spec: "paths:\n  /x:\n    get:\n      operationId: getX\n", want: "has no tags"},
		{name: "invalid operation", spec: "paths:\n  /x:\n    get: nope\n", want: "decode GET /x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := generate([]byte(tt.spec))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("generate() error = %v, want substring %q", err, tt.want)
			}
		})
	}

	spec := []byte(`tags:
  - name: zeta
  - name: zeta
paths:
  /b:
    parameters: []
    post:
      tags: [omega]
      operationId: bPost
      summary: "has | pipe"
    get:
      tags: [zeta]
      operationId: bGet
      summary: "   "
  /a:
    trace:
      tags: [omega]
      operationId: aTrace
      summary: trace
`)
	out, err := generate(spec)
	if err != nil {
		t.Fatalf("generate() ordering fixture: %v", err)
	}
	text := string(out)
	if !strings.Contains(text, "## zeta\n\n| METHOD | Path | operationId | Summary |\n|---|---|---|---|\n| GET | `/b` | `bGet` | — |\n") {
		t.Fatalf("generated output missing ordered zeta section:\n%s", text)
	}
	if !strings.Contains(text, "| POST | `/b` | `bPost` | has \\| pipe |\n| TRACE | `/a` | `aTrace` | trace |\n") {
		t.Fatalf("generated output missing ordered omega endpoints:\n%s", text)
	}
	if strings.Index(text, "## zeta") > strings.Index(text, "## omega") {
		t.Fatalf("declared tags should precede unknown tags:\n%s", text)
	}
}

func TestAPIDocsHelpers(t *testing.T) {
	if got := cleanSummary("  one\n two | three "); got != "one two \\| three" {
		t.Fatalf("cleanSummary() = %q", got)
	}
	if got := cleanSummary(" \n\t"); got != "—" {
		t.Fatalf("cleanSummary(empty) = %q", got)
	}
	methods := []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD", "TRACE"}
	for want, method := range methods {
		if got := methodRank(method); got != want {
			t.Errorf("methodRank(%q) = %d, want %d", method, got, want)
		}
	}
	if got := methodRank("CONNECT"); got != 100 {
		t.Fatalf("methodRank(unknown) = %d, want 100", got)
	}
}
