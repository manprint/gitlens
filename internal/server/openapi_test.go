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
			if !strings.HasPrefix(path, "/api/v1/") {
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

func TestOpenAPIOperationsHaveTagsAndUniqueIDs(t *testing.T) {
	document := loadOpenAPIDocument(t)
	paths := document["paths"].(map[string]any)
	seen := make(map[string]string)
	for path, rawPathItem := range paths {
		pathItem := rawPathItem.(map[string]any)
		for method, rawOperation := range pathItem {
			if method == "parameters" {
				continue
			}
			operation := rawOperation.(map[string]any)
			operationID, ok := operation["operationId"].(string)
			if !ok || operationID == "" {
				t.Errorf("%s %s: operationId missing", method, path)
				continue
			}
			if previous, duplicate := seen[operationID]; duplicate {
				t.Errorf("operationId %q is used by %s and %s %s", operationID, previous, method, path)
			}
			seen[operationID] = method + " " + path
			tags, ok := operation["tags"].([]any)
			if !ok || len(tags) == 0 {
				t.Errorf("%s: tags missing", operationID)
			}
		}
	}
}

func TestOpenAPIAgentRoutesAreBearerOnly(t *testing.T) {
	document := loadOpenAPIDocument(t)
	paths := document["paths"].(map[string]any)
	want := map[string]struct {
		method string
		id     string
	}{
		"/api/v1/push":                       {method: "post", id: "pushEnvelope"},
		"/api/v1/agents/{agent_id}/commands": {method: "get", id: "pollCommands"},
		"/api/v1/commands/{id}/result":       {method: "post", id: "submitCommandResult"},
	}
	for path, route := range want {
		rawPathItem, ok := paths[path]
		if !ok {
			t.Errorf("agent route %s missing", path)
			continue
		}
		pathItem := rawPathItem.(map[string]any)
		rawOperation, ok := pathItem[route.method]
		if !ok {
			t.Errorf("agent operation %s %s missing", route.method, path)
			continue
		}
		operation := rawOperation.(map[string]any)
		if got := operation["operationId"]; got != route.id {
			t.Errorf("%s %s: operationId = %v, want %s", route.method, path, got, route.id)
		}
		security, ok := operation["security"].([]any)
		if !ok || len(security) != 1 {
			t.Errorf("%s %s: security must contain only agentBearer", route.method, path)
			continue
		}
		bearer, ok := security[0].(map[string]any)
		if !ok || len(bearer) != 1 {
			t.Errorf("%s %s: security must contain only agentBearer", route.method, path)
			continue
		}
		if values, ok := bearer["agentBearer"].([]any); !ok || len(values) != 0 {
			t.Errorf("%s %s: security must be agentBearer: []", route.method, path)
		}
	}

	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		pathItem := paths[path].(map[string]any)
		operation := pathItem["get"].(map[string]any)
		security, ok := operation["security"].([]any)
		if !ok || len(security) != 0 {
			t.Errorf("GET %s: security must be []", path)
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
