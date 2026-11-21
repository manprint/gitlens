package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.yaml.in/yaml/v3"
)

func mcpRouterRoutes(t *testing.T) map[string][]string {
	t.Helper()
	router := NewRouter(UIConfig{}, nil, nil, NewAuth("t"), NewInventory(nil), NewPipeline(nil, nil),
		NewAPI(nil), NewTopologyAPI(nil), NewAshAPI(nil), NewAlertAPI(nil, nil))
	routes := map[string][]string{}
	err := chi.Walk(router.(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		route = strings.TrimSuffix(route, "/")
		routes[route] = append(routes[route], method)
		return nil
	})
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	return routes
}

// TestMCP006 guards I-8: every tool is a GET route of the router, and no tool
// reaches a route family that changes state or holds credentials.
func TestMCP006ToolsAreReadOnlyRoutes(t *testing.T) {
	routes := mcpRouterRoutes(t)
	forbidden := []string{"commands", "silences", "session", "push", "workbooks", "notification-channels", "alert-rules", "settings/retention"}
	for _, tool := range mcpTools() {
		methods := routes[tool.Path]
		hasGet := false
		for _, m := range methods {
			hasGet = hasGet || m == http.MethodGet
		}
		if !hasGet {
			t.Errorf("%s: %s is not a GET route (methods %v)", tool.Name, tool.Path, methods)
		}
		for _, word := range forbidden {
			if strings.Contains(tool.Path, word) {
				t.Errorf("%s: path %s names %q", tool.Name, tool.Path, word)
			}
		}
	}
}

type openapiParam struct {
	Ref      string `yaml:"$ref"`
	Name     string `yaml:"name"`
	In       string `yaml:"in"`
	Required bool   `yaml:"required"`
	Schema   struct {
		Ref  string   `yaml:"$ref"`
		Type any      `yaml:"type"`
		Enum []string `yaml:"enum"`
	} `yaml:"schema"`
}

type openapiOp struct {
	Parameters []openapiParam `yaml:"parameters"`
}

type openapiDoc struct {
	Paths map[string]struct {
		Parameters []openapiParam `yaml:"parameters"`
		Get        *openapiOp     `yaml:"get"`
	} `yaml:"paths"`
	Components struct {
		Parameters map[string]openapiParam `yaml:"parameters"`
		Schemas    map[string]struct {
			Type any      `yaml:"type"`
			Enum []string `yaml:"enum"`
		} `yaml:"schemas"`
	} `yaml:"components"`
}

func readOpenAPI(t *testing.T) openapiDoc {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read openapi: %v", err)
	}
	var doc openapiDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse openapi: %v", err)
	}
	return doc
}

// TestMCP007 keeps every argument a path or query parameter of its route in
// api/openapi.yaml, with the same required flag, type and enum.
func TestMCP007ArgumentsMatchOpenAPI(t *testing.T) {
	doc := readOpenAPI(t)
	for _, tool := range mcpTools() {
		item, ok := doc.Paths[tool.Path]
		if !ok || item.Get == nil {
			t.Errorf("%s: GET %s missing from openapi.yaml", tool.Name, tool.Path)
			continue
		}
		params := map[string]openapiParam{}
		for _, p := range append(append([]openapiParam{}, item.Parameters...), item.Get.Parameters...) {
			if p.Ref != "" {
				p = doc.Components.Parameters[p.Ref[strings.LastIndex(p.Ref, "/")+1:]]
			}
			if p.Schema.Ref != "" {
				s := doc.Components.Schemas[p.Schema.Ref[strings.LastIndex(p.Schema.Ref, "/")+1:]]
				p.Schema.Type, p.Schema.Enum = s.Type, s.Enum
			}
			if p.In == "path" || p.In == "query" {
				params[p.Name] = p
			}
		}
		var schema struct {
			Type                 string `json:"type"`
			AdditionalProperties *bool  `json:"additionalProperties"`
			Required             []string
			Properties           map[string]struct {
				Type        string   `json:"type"`
				Description string   `json:"description"`
				Enum        []string `json:"enum"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Fatalf("%s: schema: %v", tool.Name, err)
		}
		if schema.Type != "object" || schema.AdditionalProperties == nil || *schema.AdditionalProperties {
			t.Errorf("%s: schema must be an object without additional properties", tool.Name)
		}
		required := map[string]bool{}
		for _, n := range schema.Required {
			required[n] = true
		}
		for name, prop := range schema.Properties {
			p, ok := params[name]
			if !ok {
				t.Errorf("%s: argument %s is not a parameter of %s", tool.Name, name, tool.Path)
				continue
			}
			if required[name] != (p.Required || p.In == "path") {
				t.Errorf("%s: %s required=%v, openapi %v", tool.Name, name, required[name], p.Required)
			}
			if typ, ok := p.Schema.Type.(string); ok && typ != prop.Type {
				t.Errorf("%s: %s type %s, openapi %s", tool.Name, name, prop.Type, p.Schema.Type)
			}
			if !reflect.DeepEqual(prop.Enum, p.Schema.Enum) {
				t.Errorf("%s: %s enum %v, openapi %v", tool.Name, name, prop.Enum, p.Schema.Enum)
			}
			if prop.Description == "" {
				t.Errorf("%s: %s has no description", tool.Name, name)
			}
		}
		// Every route parameter is offered, so the assistant can use the route fully.
		for name := range params {
			if _, ok := schema.Properties[name]; !ok {
				t.Errorf("%s: openapi parameter %s of %s is not an argument", tool.Name, name, tool.Path)
			}
		}
		got := append(append([]string{}, tool.PathParams...), tool.QueryParams...)
		sort.Strings(got)
		want := make([]string, 0, len(schema.Properties))
		for name := range schema.Properties {
			want = append(want, name)
		}
		sort.Strings(want)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: path+query params %v, schema %v", tool.Name, got, want)
		}
	}
}

var mcpToolName = regexp.MustCompile(`^pglens_[a-z_]{3,48}$`)

func TestMCP008NamesAndDescriptions(t *testing.T) {
	seen := map[string]bool{}
	for _, tool := range mcpTools() {
		if seen[tool.Name] {
			t.Errorf("duplicate tool %s", tool.Name)
		}
		seen[tool.Name] = true
		if !mcpToolName.MatchString(tool.Name) {
			t.Errorf("tool name %q", tool.Name)
		}
		if n := len(tool.Description); n < 20 || n > 300 {
			t.Errorf("%s: description length %d", tool.Name, n)
		}
		if tool.Title == "" {
			t.Errorf("%s: no title", tool.Name)
		}
	}
	if len(seen) != len(mcpToolSpecs) || len(seen) < 30 {
		t.Fatalf("registry has %d tools", len(seen))
	}
}
