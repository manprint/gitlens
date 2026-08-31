package server_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOpenAPIDocumentParses(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate openapi test source")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	data, err := os.ReadFile(filepath.Join(repoRoot, "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read OpenAPI document: %v", err)
	}

	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatalf("parse OpenAPI document: %v", err)
	}
	if got := document["openapi"]; got != "3.1.0" {
		t.Fatalf("openapi version = %v, want 3.1.0", got)
	}

	components, ok := document["components"].(map[string]any)
	if !ok {
		t.Fatal("components missing from OpenAPI document")
	}
	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		t.Fatal("components.schemas missing from OpenAPI document")
	}
	if _, ok := schemas["Error"]; !ok {
		t.Fatal("components.schemas.Error missing from OpenAPI document")
	}
}
