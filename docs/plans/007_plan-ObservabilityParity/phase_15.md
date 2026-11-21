# Phase 14 — MCP server

Intent: an AI assistant connects to pglens over the Model Context Protocol and reads the same data as the REST API, with the same credential, and can change nothing.
Prerequisites: P1–P13.
Phase closure: P14; review by agent-1.

## State and ownership contract
Read STATE.md §1 first (and PROTOCOL.md once without this skill) for scope, ownership,
recovery, checks, and commit rules. Open before edits; checkpoint at a recovery boundary;
close with evidence. A delegated worker follows its assigned OPEN unit. Missing design goes
to agent-1, the supervisor. Executor power: low.

## Local design context
Plan revision: 1.
- D17, I-8. R10: Streamable HTTP; one endpoint; a POST carries one JSON-RPC message; a request is answered with `Content-Type: application/json` and one JSON object; a notification is answered `202 Accepted` with no body; a server without a stream answers `405` to GET; the server must check `Origin`.
- Package `internal/mcp` uses only the standard library and imports no other pglens package.
- Supported protocol versions: `2025-11-25`, `2025-06-18`. Negotiation: the client's `protocolVersion` if supported, else `2025-11-25`.
- JSON-RPC 2.0 messages. Error codes: `-32700` parse error, `-32600` invalid request (also for a JSON array body: batches are not supported), `-32601` method not found, `-32602` invalid params (also unknown tool name). A message without `id` is a notification: no response body.
- Methods:
  | Method | Result |
  |--------|--------|
  | `initialize` | `{"protocolVersion": <negotiated>, "capabilities": {"tools": {"listChanged": false}}, "serverInfo": {"name": "pglens", "version": <server version string>}, "instructions": <fixed English text: read-only access to pglens monitoring data; start with pglens_list_instances>}` |
  | `notifications/initialized` | 202, no body |
  | `ping` | `{}` |
  | `tools/list` | `{"tools": [{"name", "title", "description", "inputSchema", "annotations": {"readOnlyHint": true, "openWorldHint": false}}]}` — the whole list, no cursor |
  | `tools/call` | `{"content": [{"type": "text", "text": <the REST response body>}], "structuredContent": <the body, only when it is a JSON object>, "isError": <true when the REST status is ≥ 400>}` |
  The server keeps no session: every POST is handled alone; a `tools/call` without a prior `initialize` works.
- A tool is data: `Tool{Name, Title, Description string; InputSchema json.RawMessage; Path string; PathParams []string; QueryParams []string}`. It maps to exactly one `GET` request on the REST API: `Path` is a route template (`/api/v1/instances/{id}/host`), `PathParams` are argument names placed in the template (each validated against `^[A-Za-z0-9_.:-]{1,128}$`), `QueryParams` are argument names copied into the query string when present (strings, numbers and booleans; arrays joined with commas). An argument not named in the schema is `-32602`.
- In-process call: the route builds a `GET` `*http.Request` with the caller's request context and the caller's `Authorization` and `Cookie` headers, and serves it with the same router that serves the REST API (the handler chain including `RequireCredential`), writing into an in-memory response writer defined in `internal/server` (not `httptest`). So authentication, tenant scope, validation and limits are exactly the REST ones. Response text over 262 144 bytes → `isError: true` with the text `result too large: narrow the time range or lower limit`.
- Tool list (all names start with `pglens_`; the description of each is one or two English sentences saying what it returns and which arguments matter; time arguments are RFC 3339):
  | Tool | REST route | Arguments |
  |------|-----------|-----------|
  | `pglens_list_instances` | `/api/v1/instances` | — |
  | `pglens_get_instance` | `/api/v1/instances/{id}` | `id` |
  | `pglens_list_clusters` | `/api/v1/clusters` | — |
  | `pglens_cluster_topology` | `/api/v1/clusters/{id}/topology` | `id` |
  | `pglens_cluster_replication` | `/api/v1/clusters/{id}/replication` | `id`, `from`, `to` |
  | `pglens_list_findings` | `/api/v1/findings` | `instance_id`, `severity`, `status` |
  | `pglens_list_alerts` | `/api/v1/alerts` | `state` |
  | `pglens_list_events` | `/api/v1/events` | `instance_id`, `type`, `from`, `to`, `limit` |
  | `pglens_activity` | `/api/v1/ash` | `instance_id`, `from`, `to`, `group_by`, filters |
  | `pglens_activity_top` | `/api/v1/ash/top` | `instance_id`, `from`, `to`, `group_by`, `limit` |
  | `pglens_activity_compare` | `/api/v1/ash/compare` | per unit 4.2 |
  | `pglens_top_statements` | `/api/v1/statements` | `instance_id`, `from`, `to`, `order_by`, `limit` |
  | `pglens_statements_compare` | `/api/v1/statements/compare` | per unit 4.1 |
  | `pglens_locks` | `/api/v1/locks` | `instance_id` |
  | `pglens_query_plans` | `/api/v1/plans` | `instance_id`, `queryid`, `database` |
  | `pglens_plan_samples` | `/api/v1/plans/samples` | per unit 7.4 |
  | `pglens_plan_stats` | `/api/v1/plans/stats` | per unit 9.2 |
  | `pglens_logs` | `/api/v1/logs` | per unit 6.6 |
  | `pglens_log_classes` | `/api/v1/logs/classes` | per unit 6.6 |
  | `pglens_autovacuum_runs` | `/api/v1/autovacuum/runs` | per unit 8.3 |
  | `pglens_autovacuum_summary` | `/api/v1/autovacuum/summary` | per unit 8.3 |
  | `pglens_instance_tables` | `/api/v1/instances/{id}/tables` | `id`, and the route's query parameters |
  | `pglens_instance_indexes` | `/api/v1/instances/{id}/indexes` | same |
  | `pglens_index_suggestions` | `/api/v1/instances/{id}/index-suggestions` | `id` |
  | `pglens_instance_bloat` | `/api/v1/instances/{id}/bloat` | `id` |
  | `pglens_instance_settings` | `/api/v1/instances/{id}/settings` | `id` |
  | `pglens_instance_readiness` | `/api/v1/instances/{id}/readiness` | `id` |
  | `pglens_instance_host` | `/api/v1/instances/{id}/host` | `id`, `from`, `to` |
  | `pglens_host_processes` | `/api/v1/instances/{id}/host/processes` | per unit 12.3 |
  | `pglens_metrics_query` | `/api/v1/metrics/query` | the route's query parameters |
  The argument names of each tool are exactly the query parameter names of its route as written in `api/openapi.yaml` at unit open (the table above names the main ones; the unit copies the full list from the OpenAPI file). Not exposed: commands, workbook runs, silences, rule and channel changes, retention, session, push, and `DELETE /plans/samples/parameters`.
- HTTP route `POST /mcp`:
  | Condition | Answer |
  |-----------|--------|
  | `PGLENS_MCP_ENABLED` not `true` | route not registered → 404 |
  | method other than POST | 405, header `Allow: POST` |
  | no valid credential | 401 (from `RequireCredential`) |
  | `Origin` header present and neither equal to the server's own origin nor listed in `PGLENS_MCP_ALLOWED_ORIGINS` | 403 |
  | `Content-Type` not `application/json` | 415 |
  | body over 1 MiB | 413 |
  | header `MCP-Protocol-Version` present and not supported | 400 |
  `PGLENS_MCP_ENABLED`: `true` or `false` (default `false`); another value is a start-up error. `PGLENS_MCP_ALLOWED_ORIGINS`: comma-separated list of origins, each `scheme://host[:port]` with scheme `http` or `https`, no path; an invalid entry is a start-up error. A request without `Origin` (command-line and server-side clients) is allowed. Read `internal/sessionorigin` first: if it already computes "the server's own origin" for the console, reuse it.
  Responses carry `Cache-Control: no-store`. Each call is logged once at debug level with the method and the tool name, never with arguments.

## Sub-phases

### 14.1 Protocol core
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** NEW `internal/mcp/protocol.go` (message types, error codes, version negotiation), `server.go` (`Server{Info, Instructions, Tools}`; `Caller func(ctx context.Context, tool Tool, args map[string]json.RawMessage) (status int, body []byte, err error)`; `func (s *Server) Handle(ctx context.Context, raw []byte, call Caller) (response []byte, isNotification bool)`), `protocol_test.go`, `server_test.go`.
- **Change:**
  Preconditions: P1–P13 DONE.
  Contract: as in Local design context. `Handle` never panics on any input; unknown fields are ignored; `id` is echoed with its JSON type (number or string); a request with `id: null` is invalid (`-32600`).
  Steps: 1. S1 — types and negotiation. 2. S2 — `Handle` for the five methods with a fake `Caller`. 3. S3 — tests and `FuzzHandle`.
- **Unit tests:** `TestMCP001` (`initialize` with `2025-06-18` → same version; with `2099-01-01` → `2025-11-25`; result has `tools` capability and `serverInfo.name` `pglens`), `TestMCP002` (`notifications/initialized` → notification, no body; `ping` → `{}`), `TestMCP003` (`tools/list` returns every tool with `inputSchema.type` `object` and `readOnlyHint` true), `TestMCP004` (`tools/call`: body object → `structuredContent` set; body array → only text; status 404 → `isError` true; unknown tool or undeclared argument → `-32602`; oversized body → the size text), `TestMCP005` (malformed JSON → `-32700`; array body → `-32600`; unknown method → `-32601`; `id` type echoed). Gate G-GO-FOCUS `./internal/mcp/`.
- **e2e tests:** N/A — the HTTP layer is unit 14.3.
- **Done:** tests pass; fuzz 10 s clean; `go list -deps ./internal/mcp/ | grep manprint/pglens` prints only the package itself; reviewed; unit closed and committed.

### 14.2 Read-only tool registry
- **Agent:** agent-2
- **Assignment:** implement; agent-1 reviews the tool list (names, routes, arguments) before code and the diff after.
- **Files:** READ `api/openapi.yaml` (parameters of each listed route), `internal/server/http.go` (route table); NEW `internal/server/mcp_tools.go` (`func mcpTools() []mcp.Tool`), `mcp_tools_test.go`.
- **Change:**
  Preconditions: 14.1 DONE.
  Contract: the registry of the Local design context, with one JSON Schema per tool (`type: object`, `properties` with `type` and a one-line `description`, `required`, `additionalProperties: false`). Enumerated parameters (`group_by`, `order_by`, `kind`, …) carry the same `enum` as the OpenAPI file.
  Steps: 1. S1 — write the table (tool, route, every argument with type, required, enum) from the OpenAPI file and send it to agent-1. 2. S2 — registry. 3. S3 — tests.
- **Unit tests:** `TestMCP006` (guards I-8: for every tool, `Path` is registered in the router as a `GET` route — walk the chi router with `chi.Walk` — and the same path has no tool mapped to another method because `Tool` has no method field; no tool path contains `commands`, `silences`, `session`, `push`, `workbooks`, `notification-channels`, `alert-rules`, `settings/retention`), `TestMCP007` (every tool argument exists as a path or query parameter of that route in `api/openapi.yaml`, with the same enum; every schema is valid JSON with `additionalProperties: false`), `TestMCP008` (names are unique, match `^pglens_[a-z_]{3,48}$`, descriptions are 20–300 characters). Gate G-GO-FOCUS `./internal/server/`.
- **e2e tests:** N/A — exercised through the route in 14.3.
- **Done:** tests pass; list approved; reviewed; unit closed and committed.

### 14.3 HTTP route, credential, Origin
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** READ `internal/server/http.go` (`NewRouter`, where `RequireCredential` is applied), `internal/server/session.go` (`RequireCredential`), `internal/sessionorigin`; NEW `internal/server/mcp_http.go`, `mcp_http_test.go`, `mcp_http_integration_test.go`; WRITE `internal/server/http.go`, `internal/server/config.go` and `cmd/pglens-server/main.go` (the two environment variables), `api/openapi.yaml` (path `/mcp`, described as JSON-RPC with a link to the README section; request and response schema `object`), G-API outputs.
- **Change:**
  Preconditions: 14.2 DONE.
  Contract: the route table of the Local design context. The in-process `Caller` copies only the `Authorization` and `Cookie` headers from the outer request, sets `Accept: application/json`, and serves the inner request with the router (the route needs a reference to the finished router: build it with a small indirection, for example a handler variable set after `NewRouter` returns its router; an inner request to `/mcp` itself is impossible because no tool maps to it).
  Steps: 1. S1 — configuration. 2. S2 — in-memory response writer and `Caller`. 3. S3 — route with the checks in the table order. 4. S4 — tests. 5. S5 — OpenAPI, G-API.
  Recovery boundary: after S3.
- **Unit tests:** `TestMCP009` (disabled → 404; GET and DELETE → 405 with `Allow: POST`; wrong content type → 415; 2 MiB body → 413; unsupported `MCP-Protocol-Version` → 400), `TestMCP010` (guards I-8: no credential → 401 and the `Caller` is never invoked; with a valid token, `tools/call pglens_list_instances` returns exactly the bytes `GET /api/v1/instances` returns for that token; with a token whose inner REST call is refused, the tool result is `isError: true` carrying the REST error body — the MCP layer adds no access of its own), `TestMCP011` (`Origin`: absent → allowed; own origin → allowed; listed → allowed; other → 403), `TestMCP012` (invalid `PGLENS_MCP_ENABLED` or origin list → start-up error), `TestMCP013` (only `Authorization` and `Cookie` reach the inner request; a client header `X-Forwarded-For` or `X-Pglens-Lang` does not). Gate G-GO-FOCUS `./internal/server/ ./cmd/pglens-server/`.
- **e2e tests:** `TestINTMCP001` (`./internal/server/`, real store): with one instance and pushed data, the sequence `initialize` → `notifications/initialized` → `tools/list` → `tools/call pglens_list_instances` → `tools/call pglens_activity_top` over real HTTP returns the instance and the same top list as the REST call. Gate G-GO-INT.
- **Done:** tests pass; reviewed; unit closed and committed.

### 14.4 Documentation and client configuration
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** WRITE `README.md` (section "MCP server"), `docs/api.md` through `make api-docs`, `web/src/lib/help/topics/{en,it}/` (new topic `mcp.ts`, registered like the others), `web/src/i18n/messages/{en,it}/help.ts` if topic titles live there, the Settings page that lists API tokens (one line with the endpoint URL and a link to the help topic) and its test.
- **Change:**
  Preconditions: 14.3 DONE.
  Contract: README section: what it is, that it is off by default and read-only, the two environment variables, the credential (an API token sent as `Authorization: Bearer <token>`), the tool list (generated from `mcpTools()` by a test-backed helper so the list cannot drift: `TestMCP014` compares the README table with the registry), a generic client configuration example for a client that supports Streamable HTTP with a custom header (JSON with `"url": "https://pglens.example.com/mcp"` and the header), the limits (no stream, no session, no prompts or resources, result size cap), and a security note (the assistant sees everything the token can read, including statement text and log messages when those are collected).
  Steps: 1. S1 — README and the drift test. 2. S2 — help topic in both languages. 3. S3 — Settings line.
- **Unit tests:** `TestMCP014` (README tool table equals the registry names and routes). `UI-MCP-001` (Settings shows the endpoint URL built from the page origin and the help link), `UI-MCP-002` (the help topic exists in `en` and `it` with the same sections). Gates G-GO-FOCUS, G-WEB-STATIC, G-WEB-UNIT.
- **e2e tests:** N/A at unit level; lab check in 15.2.
- **Done:** tests pass; README and help updated; reviewed; unit closed and committed.

## Phase gates and closure
- Required gates: G-GO-FULL, G-GO-INT (`TestINTMCP001`), G-API, G-WEB-FULL, G-I18N; assertions: I-8 tests green; `internal/mcp` has no pglens import.
- README obligation: unit 14.4.
- Every sub-phase DONE or explicitly justified SKIPPED. Run full phase gates now.
- agent-1 reviews the tool list for anything that writes or leaks, the credential path, the Origin rule, documentation.
- Open closure unit P14, record review and result, mark the phase DONE, close, commit `phase(007/P14): PHASE 14 COMPLETE — MCP server; gates and review passed`.
- P14 edits no production code or tests. Blocked gate: P14 stays OPEN and BLOCKED. Failing gate or rejected review: attempt failed; reopen the responsible sub-phase, then rerun P14.
