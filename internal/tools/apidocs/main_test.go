package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
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
