package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/manprint/pglens/internal/alert"
	"github.com/manprint/pglens/internal/server"
	"go.yaml.in/yaml/v3"
)

var openapiExemptRoutes = map[string]struct{}{}

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

func TestOpenAPIOperationsHaveTags(t *testing.T) {
	document := loadOpenAPIDocument(t)
	paths := document["paths"].(map[string]any)
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
			tags, ok := operation["tags"].([]any)
			if !ok || len(tags) == 0 {
				t.Errorf("%s: tags missing", operationID)
			}
		}
	}
}

func TestOpenAPIOperationIdsAreUnique(t *testing.T) {
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
				continue
			}
			location := method + " " + path
			if previous, duplicate := seen[operationID]; duplicate {
				t.Errorf("operationId %q is used by %s and %s", operationID, previous, location)
			}
			seen[operationID] = location
		}
	}
}

func TestOpenAPICoversEveryRoute(t *testing.T) {
	router := server.NewRouter(
		server.UIConfig{},
		nil,
		nil,
		server.NewAuth("t"),
		server.NewInventory(nil),
		server.NewPipeline(nil, nil),
		server.NewAPI(nil),
		server.NewTopologyAPI(nil),
		server.NewAshAPI(nil),
		server.NewAlertAPI(nil, nil),
	)

	registered := make(map[string]struct{})
	err := chi.Walk(router.(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		normalized := canonicalizeRegisteredRoute(method, route)
		if _, exempt := openapiExemptRoutes[method+" "+normalized]; !exempt {
			registered[method+" "+normalized] = struct{}{}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk production routes: %v", err)
	}

	documented := make(map[string]struct{})
	paths := loadOpenAPIDocument(t)["paths"].(map[string]any)
	for path, rawPathItem := range paths {
		pathItem := rawPathItem.(map[string]any)
		for method := range pathItem {
			if method == "parameters" {
				continue
			}
			documented[strings.ToUpper(method)+" "+normalizeChiRoute(path)] = struct{}{}
		}
	}

	registeredOnly := difference(registered, documented)
	documentedOnly := difference(documented, registered)
	if len(registeredOnly) > 0 {
		t.Errorf("routes registered but absent from api/openapi.yaml: %s", strings.Join(registeredOnly, ", "))
	}
	if len(documentedOnly) > 0 {
		t.Errorf("operations documented but not registered: %s", strings.Join(documentedOnly, ", "))
	}
}

func TestNormalizeChiRoute(t *testing.T) {
	tests := map[string]string{
		"/api/v1/clusters/":        "/api/v1/clusters",
		"/api/v1/instances/{id}/*": "/api/v1/instances/{id}",
		"//api/v1/x":               "/api/v1/x",
		"/api/v1/plain":            "/api/v1/plain",
	}
	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			if got := normalizeChiRoute(input); got != want {
				t.Fatalf("normalizeChiRoute(%q) = %q, want %q", input, got, want)
			}
		})
	}
}

func normalizeChiRoute(route string) string {
	route = strings.TrimSuffix(route, "/*")
	if route != "/" {
		route = strings.TrimSuffix(route, "/")
	}
	for strings.Contains(route, "//") {
		route = strings.ReplaceAll(route, "//", "/")
	}
	return route
}

func canonicalizeRegisteredRoute(method, route string) string {
	normalized := normalizeChiRoute(route)
	if !strings.HasSuffix(route, "/*") || normalized != "/api/v1/findings" {
		return normalized
	}
	// Finding IDs may contain slashes, so the production router uses a
	// catch-all. GET serves the finding; the write methods serve only the mute
	// action, because their handlers answer a bare id with 404. The generic
	// normalizer above still strips only chi's route suffix.
	if method == "GET" {
		return "/api/v1/findings/{finding-id}"
	}
	return "/api/v1/findings/{finding-id}/mute"
}

func difference(left, right map[string]struct{}) []string {
	var out []string
	for item := range left {
		if _, ok := right[item]; !ok {
			out = append(out, item)
		}
	}
	sort.Strings(out)
	return out
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

// Generated clients handle only the statuses the document lists. The login
// route can also answer 500 and 503, and the poll route's 403 has a second
// cause: a valid agent token sent without the identity header.
func TestOpenAPIDocumentsSessionAndPollResponses(t *testing.T) {
	paths := loadOpenAPIDocument(t)["paths"].(map[string]any)

	login := paths["/api/v1/session"].(map[string]any)["post"].(map[string]any)
	responses := login["responses"].(map[string]any)
	for _, status := range []string{"204", "400", "401", "429", "500", "503"} {
		if _, ok := responses[status]; !ok {
			t.Errorf("POST /api/v1/session: response %s is not documented", status)
		}
	}

	poll := paths["/api/v1/agents/{agent_id}/commands"].(map[string]any)["get"].(map[string]any)
	forbidden := poll["responses"].(map[string]any)["403"].(map[string]any)
	if description, _ := forbidden["description"].(string); !strings.Contains(description, "X-PGLENS-Agent-ID") {
		t.Errorf("GET agent commands: 403 must name the identity header, got %q", description)
	}
}

// ruleListStore answers the rule listing, the only store call the catalogue
// route makes. Any other call panics on the nil embedded store.
type ruleListStore struct{ alert.Store }

func (ruleListStore) Rules(context.Context) ([]alert.Rule, error) { return nil, nil }

// A reader copies the catalogue example, so each example rule must be the rule
// the server returns. The example once named a heartbeat metric that no rule
// uses and left out the fields the rule carries.
func TestOpenAPIAlertRuleExampleMatchesTheServer(t *testing.T) {
	router := chi.NewRouter()
	server.NewAlertAPI(nil, ruleListStore{}).RegisterRoutes(router)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/alert-rules", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/alert-rules = %d, want 200", rec.Code)
	}
	var served []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &served); err != nil {
		t.Fatalf("decode served alert rules: %v", err)
	}

	paths := loadOpenAPIDocument(t)["paths"].(map[string]any)
	get := paths["/api/v1/alert-rules"].(map[string]any)["get"].(map[string]any)
	content := get["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)
	examples := content["application/json"].(map[string]any)["examples"].(map[string]any)
	for _, rawRule := range examples["default"].(map[string]any)["value"].([]any) {
		// The example is decoded from YAML and the response from JSON, so both
		// are normalized through JSON before they are compared.
		raw, err := json.Marshal(rawRule)
		if err != nil {
			t.Fatalf("encode example rule: %v", err)
		}
		var want map[string]any
		if err := json.Unmarshal(raw, &want); err != nil {
			t.Fatalf("decode example rule: %v", err)
		}
		id, _ := want["id"].(string)
		var got map[string]any
		for _, rule := range served {
			if rule["id"] == id {
				got = rule
			}
		}
		if got == nil {
			t.Errorf("example rule %q is not served", id)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("example rule %q = %v, the server returns %v", id, want, got)
		}
	}
}

// A counter is stored under its own name as a per-second rate, and the query
// returns the stored value. A client that reads it as the cumulative count
// would chart the rate as a total, so the document must say it is a rate.
func TestOpenAPIMetricsQueryDocumentsCounterRates(t *testing.T) {
	paths := loadOpenAPIDocument(t)["paths"].(map[string]any)
	get := paths["/api/v1/metrics/query"].(map[string]any)["get"].(map[string]any)
	ok := get["responses"].(map[string]any)["200"].(map[string]any)
	if description, _ := ok["description"].(string); !strings.Contains(description, "per-second rate") {
		t.Errorf("GET /api/v1/metrics/query 200 must say counters are per-second rates, got %q", description)
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
	if len(document) == 0 {
		t.Fatal("OpenAPI document is empty")
	}
	return document
}

// The JSON write routes refuse a body that is not application/json with 415
// and one that is too large with 413, so the spec documents both for each of
// them. The unmute route reads no body and is not listed.
func TestOpenAPIDocumentsRefusedJSONBodies(t *testing.T) {
	paths := loadOpenAPIDocument(t)["paths"].(map[string]any)

	for _, route := range []struct{ path, method string }{
		{"/api/v1/alert-rules/{rule_id}", "put"},
		{"/api/v1/silences", "post"},
		{"/api/v1/findings/{finding-id}/mute", "post"},
	} {
		operation := paths[route.path].(map[string]any)[route.method].(map[string]any)
		responses := operation["responses"].(map[string]any)
		for _, status := range []string{"413", "415"} {
			if _, ok := responses[status]; !ok {
				t.Errorf("%s %s: response %s is not documented", strings.ToUpper(route.method), route.path, status)
			}
		}
	}
}

// updateRule answers the threshold from a nullable column, so a rule whose
// stored threshold is NULL is answered with null. The schema must allow it.
func TestOpenAPIAlertRuleUpdateResponseThresholdIsNullable(t *testing.T) {
	schemas := loadOpenAPIDocument(t)["components"].(map[string]any)["schemas"].(map[string]any)
	properties := schemas["AlertRuleUpdateResponse"].(map[string]any)["properties"].(map[string]any)
	threshold := properties["threshold"].(map[string]any)
	if want := []any{"number", "null"}; !reflect.DeepEqual(threshold["type"], want) {
		t.Errorf("AlertRuleUpdateResponse.threshold type = %v, want %v", threshold["type"], want)
	}
}

// The statements truncated flag is set by two records, and a client reads a
// clear flag as a complete window. The description must name the event that
// keeps a historical cut, and say that the partial record is cleared by the
// next complete result, so a partial that ended is not reported.
func TestOpenAPIStatementsTruncatedNamesItsSources(t *testing.T) {
	schemas := loadOpenAPIDocument(t)["components"].(map[string]any)["schemas"].(map[string]any)
	properties := schemas["StatementsResponse"].(map[string]any)["properties"].(map[string]any)
	description, _ := properties["truncated"].(map[string]any)["description"].(string)
	for _, want := range []string{"cardinality_truncated", "next complete result"} {
		if !strings.Contains(description, want) {
			t.Errorf("StatementsResponse.truncated description must mention %q, got %q", want, description)
		}
	}
}
