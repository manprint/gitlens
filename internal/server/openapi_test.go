package server_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOpenAPIDocumentParses(t *testing.T) {
	document := loadOpenAPIDocument(t)
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

func TestOpenAPIReadOperationsHaveExamples(t *testing.T) {
	document := loadOpenAPIDocument(t)
	paths, ok := document["paths"].(map[string]any)
	if !ok {
		t.Fatal("paths missing from OpenAPI document")
	}

	for path, rawPathItem := range paths {
		pathItem, ok := rawPathItem.(map[string]any)
		if !ok {
			t.Fatalf("path %q is not an object", path)
		}
		for method, rawOperation := range pathItem {
			if method == "parameters" {
				continue
			}
			operation, ok := rawOperation.(map[string]any)
			if !ok {
				t.Fatalf("operation %s %s is not an object", method, path)
			}
			operationID, _ := operation["operationId"].(string)
			if !strings.HasPrefix(operationID, "get") && operationID != "queryMetrics" {
				continue
			}

			responses, ok := operation["responses"].(map[string]any)
			if !ok {
				t.Errorf("%s: responses missing", operationID)
				continue
			}
			okResponse, ok := responses["200"].(map[string]any)
			if !ok {
				t.Errorf("%s: 200 response missing", operationID)
				continue
			}
			content, ok := okResponse["content"].(map[string]any)
			if !ok {
				t.Errorf("%s: response content missing", operationID)
				continue
			}
			jsonContent, ok := content["application/json"].(map[string]any)
			if !ok {
				t.Errorf("%s: application/json content missing", operationID)
				continue
			}
			if _, ok := jsonContent["schema"]; !ok {
				t.Errorf("%s: 200 application/json schema missing", operationID)
			}
			if _, hasExample := jsonContent["example"]; !hasExample {
				if _, hasExamples := jsonContent["examples"]; !hasExamples {
					t.Errorf("%s: 200 application/json example(s) missing", operationID)
				}
			}
		}
	}
}

func loadOpenAPIDocument(t *testing.T) map[string]any {
	t.Helper()
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
	return document
}
