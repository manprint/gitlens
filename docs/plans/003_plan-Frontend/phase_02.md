# Phase 1 — OpenAPI contract and route-coverage gate

> **Intent:** publish `api/openapi.yaml` as the single authority for the HTTP
> contract, and make it impossible for the router and the document to drift.
> **Shippable alone?** yes — additive. No handler behaviour changes.
> **Preconditions:** phase 0 DONE.

## State contract (mandatory)

1. Before touching anything: read [STATE.md](STATE.md). If §1 `Status` is `OPEN`,
   finish or revert that unit first (§6 says how far it got). Run the gate
   commands in STATE.md **§3** and check the result against what §1, §7, and §11
   claim; the repo wins, so correct the file when they disagree.
2. **Open the sub-phase in STATE.md §1 before editing any code**: `Type:
   sub-phase`, its `ID`, `Status: OPEN`, `Intent`, `Next action:`, and §6 set to
   `claimed — nothing written yet`.
3. **Close it after the gates are green**: append the §4 ledger row, reset §6 to
   `none — tree consistent`, update §5 §7 §8 §9 §10 and the §11 board, point §1
   at the next unit with `Status: none`, bump the timestamp. When STATE.md §3 has
   WIP commits on, commit the closed sub-phase and put its sha in the §4 row. A
   sub-phase is not done until this is written.
4. If the session ends mid-sub-phase, leave §1 `OPEN` and write exactly what is
   half-finished into §6 before stopping — plus a `wip(<N.Y>)` commit when WIP
   commits are on.

---

## Route inventory (authoritative for this phase)

Every route currently registered by `NewRouter` (`internal/server/http.go:11`)
and the `RegisterRoutes` methods it calls. The spec must contain exactly these,
plus the three session routes phase 2 adds.

```text
GET    /healthz
GET    /readyz
GET    /metrics
POST   /api/v1/push
GET    /api/v1/clusters
GET    /api/v1/clusters/{id}/topology
GET    /api/v1/clusters/{id}/replication
GET    /api/v1/clusters/{id}/settings-drift
GET    /api/v1/instances
GET    /api/v1/instances/{id}
GET    /api/v1/instances/{id}/activity
GET    /api/v1/instances/{id}/databases
GET    /api/v1/instances/{id}/host
GET    /api/v1/instances/{id}/settings
GET    /api/v1/instances/{id}/tables
GET    /api/v1/instances/{id}/indexes
GET    /api/v1/instances/{id}/bloat
GET    /api/v1/instances/{id}/command-audit
GET    /api/v1/locks
GET    /api/v1/metrics/query
GET    /api/v1/events
GET    /api/v1/statements
GET    /api/v1/ash
GET    /api/v1/ash/top
GET    /api/v1/plans
POST   /api/v1/instances/{id}/commands
GET    /api/v1/commands/{id}
GET    /api/v1/agents/{agent_id}/commands
POST   /api/v1/commands/{id}/result
GET    /api/v1/alerts
GET    /api/v1/alerts/{alert_key}
GET    /api/v1/alert-rules
PUT    /api/v1/alert-rules/{rule_id}
GET    /api/v1/silences
POST   /api/v1/silences
DELETE /api/v1/silences/{id}
GET    /api/v1/findings
GET    /api/v1/findings/{finding-id}
POST   /api/v1/findings/{finding-id}/mute
DELETE /api/v1/findings/{finding-id}/mute
GET    /api/v1/advisor/rules
```

If `chi.Walk` reports a pattern not in this list, the list is stale, not the
router: add the route to the spec and to this file, and record the addition in
`STATE.md` §8.

---

## Sub-phases

### 1.1 Author `api/openapi.yaml` — document skeleton and shared components

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation.
  **`agent-1:opus` review gate** — this file becomes the contract every later
  phase generates types from; a wrong shape propagates into every page.
- **Files:** `api/openapi.yaml` (new; create the `api/` directory — no existing
  directory holds interface definitions, and `internal/` is for Go code only)
- **Change:**
  1. Create `api/openapi.yaml` with `openapi: 3.1.0`, an `info` block
     (`title: pglens HTTP API`, `version: 1.0.0`, `license: Apache-2.0`), and
     `servers: [{url: "/", description: "the pglens server itself"}]`.
  2. Define `components.securitySchemes`:
     - `agentBearer`: `type: http`, `scheme: bearer` — the agent's bootstrap
       token, checked by `(*Auth).Validate` (`internal/server/auth.go:17`).
     - `uiSession`: `type: apiKey`, `in: cookie`, `name: pglens_session` — added
       now so phase 2 only has to reference it.
  3. Define the shared schemas every response reuses. Names are normative:
     - `Error` — `{error: string, detail: string}`, matching `apiError`
       (`internal/server/api.go:89`). Both fields required.
     - `ClusterId` — `type: string`, `pattern: '^[0-9]+$'`, with a description
       stating it is a `uint64` rendered as a decimal string because it exceeds
       IEEE-754 exact integer range.
     - `InstanceId` — `type: string`, `format: uuid`.
     - `Timestamp` — `type: string`, `format: date-time`, RFC 3339.
     - `NullableSeconds` — `type: [number, "null"]`, description stating that
       `null` means "not measured", never zero.
     - `SeriesPoint` — `{ts: Timestamp, value: [number,"null"]}`.
     - `PermTier` — `type: string`, `enum: [T0, T1, T2]`.
     - `Health` — `type: string`, `enum: [ok, degraded, critical]`.
     - `Truncated` — `type: boolean`, description stating that `true` means the
       relation or cardinality budget cut the result.
  4. Define `components.responses` for the reusable error responses:
     `BadRequest` (400), `Unauthorized` (401), `NotFound` (404),
     `Unprocessable` (422), `PayloadTooLarge` (413) — each
     `content: application/json: schema: $ref: '#/components/schemas/Error'`.
  5. Add a document-level `security: [{uiSession: []}, {agentBearer: []}]`, then
     override per operation where the rule differs (see 1.2 and 1.3).
  6. Do not invent fields. Every property must be traceable to a handler or to
     the README's documented example output. When a shape is uncertain, run the
     endpoint against the L3 stack and copy the observed shape; record the
     uncertainty in `STATE.md` §8 rather than guessing.
- **Unit tests:** `TestOpenAPIDocumentParses` in `internal/server/openapi_test.go`
  (new, no build tag) — reads `api/openapi.yaml` via `os.ReadFile` on a path
  resolved from the repo root, unmarshals it with `sigs.k8s.io/yaml` or
  `gopkg.in/yaml.v3` (use whichever is already in `go.mod`; if neither is, add
  `gopkg.in/yaml.v3` and record it in `STATE.md` §8), and asserts
  `openapi == "3.1.0"` and that `components.schemas.Error` exists.
- **e2e tests:** none (no behaviour change).
- **Done:** gates green; the document parses; `agent-1:opus` has reviewed the
  shared schemas; closed in `STATE.md`.

### 1.2 Document the read endpoints

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `api/openapi.yaml`
- **Change:** add one `paths` entry per `GET` route in the inventory above,
  except `/healthz`, `/readyz`, `/metrics` and the agent-only routes (1.3). For
  each operation provide:
  - `operationId` in `lowerCamelCase`, derived from the path
    (`getClusters`, `getClusterTopology`, `getClusterReplication`,
    `getClusterSettingsDrift`, `getInstances`, `getInstance`,
    `getInstanceActivity`, `getInstanceDatabases`, `getInstanceHost`,
    `getInstanceSettings`, `getInstanceTables`, `getInstanceIndexes`,
    `getInstanceBloat`, `getInstanceCommandAudit`, `getLocks`,
    `queryMetrics`, `getEvents`, `getStatements`, `getAsh`, `getAshTop`,
    `getPlans`, `getAlerts`, `getAlert`, `getAlertRules`, `getSilences`,
    `getFindings`, `getFinding`, `getAdvisorRules`). These become the frontend's
    generated function names; they are part of the contract.
  - every query parameter with its type, whether it is required, its default and
    its documented bound. The ones that are already specified and must be
    reproduced exactly:
    - `from`, `to` — `Timestamp`, **required** on
      `/api/v1/clusters/{id}/replication` and `/api/v1/metrics/query`.
    - `step` — string duration on `/api/v1/metrics/query`; a `step` producing
      more than 10000 points returns **422**.
    - `limit` — integer; `/api/v1/events` caps at 1000; `/api/v1/findings` has
      default 100 and max 1000 (`internal/server/api_findings.go:120`).
    - `group_by` on `/api/v1/ash` — `enum: [wait_event_type, wait_event, queryid, state, datname]`.
    - `order_by` on `/api/v1/statements` — enum including `total_exec_time`.
    - `changed_since` on `/api/v1/instances/{id}/settings` — `Timestamp`.
    - `instance_id`, `cluster_id`, `database`, `datname`, `queryid`, `type`,
      `severity` filters where the handler reads them.
  - a `200` response with a fully specified schema, and **at least one
    `examples` entry per operation**. The examples are not decoration: phase 5
    validates the frontend's fixtures against these schemas, and the examples are
    the seed for those fixtures. Copy the example payloads already published in
    `README.md` (clusters, topology, replication, metrics query, ASH, ASH top,
    findings, advisor rules, alerts) verbatim where they exist.
  - the error responses each endpoint can actually produce, referencing
    `components.responses`.
  - `null` semantics stated in the description wherever the handler can emit
    `null`: missing intervals in a series, `max_replay_lag_seconds`,
    `avg_active_sessions` when `ticks == 0`, `sampled_at` on an empty lock tree.
- **Unit tests:** `TestOpenAPIReadOperationsHaveExamples` in
  `internal/server/openapi_test.go` — walks `paths`, and for every operation
  whose `operationId` starts with `get` or equals `queryMetrics`, asserts the
  `200` response's `content."application/json"` has either `example` or
  `examples` and a `schema`. Fails with the offending `operationId` in the
  message.
- **e2e tests:** none.
- **Done:** gates green; every read route from the inventory has an operation
  with an example; closed in `STATE.md`.

### 1.3 Document the write, agent and infrastructure endpoints

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `api/openapi.yaml`
- **Change:**
  - `POST /api/v1/instances/{id}/commands` (`createCommand`) with the request
    body `{kind: enum[explain,cancel,terminate,pgstattuple], args: object}` and
    the documented per-kind arguments (`queryid`, `datname`, `analyze` for
    `explain`; `pid` for `cancel`/`terminate`; relation identifiers for
    `pgstattuple`). State in the description that **query text is never sent**.
  - `GET /api/v1/commands/{id}` (`getCommand`) with the command state machine
    enumerated: `queued`, `claimed`, `succeeded`, `failed`, `expired`,
    `rejected`. The exact strings must be read from the handler
    (`internal/server/commands.go`, `internal/server/api_commands.go`), not
    assumed.
  - `PUT /api/v1/alert-rules/{rule_id}` (`updateAlertRule`),
    `POST /api/v1/silences` (`createSilence`),
    `DELETE /api/v1/silences/{id}` (`deleteSilence`, 204),
    `POST /api/v1/findings/{finding-id}/mute` (`muteFinding`),
    `DELETE /api/v1/findings/{finding-id}/mute` (`unmuteFinding`, 204).
  - Agent-only routes, marked `security: [{agentBearer: []}]` and tagged
    `agent`: `POST /api/v1/push` (`pushEnvelope`; 400 unknown
    `protocol_version` or stale `sent_at`, 401 revoked, 413 body over 32 MiB),
    `GET /api/v1/agents/{agent_id}/commands` (`pollCommands`),
    `POST /api/v1/commands/{id}/result` (`submitCommandResult`).
  - Infrastructure routes with `security: []` (no auth) and
    `content: text/plain`: `GET /healthz`, `GET /readyz` (200 `ok`, 503
    `not ready`), `GET /metrics` (Prometheus exposition; describe it as
    `text/plain` and do not attempt to schema it).
  - Tag every operation: `clusters`, `instances`, `metrics`, `events`,
    `statements`, `ash`, `commands`, `alerts`, `findings`, `agent`,
    `infrastructure`, `session`. Tags drive the generated client's grouping and
    the documentation page in 1.5.
- **Unit tests:** `TestOpenAPIAgentRoutesAreBearerOnly` — asserts the three
  agent operations declare exactly `[{agentBearer: []}]`, and that `/healthz`,
  `/readyz`, `/metrics` declare `security: []`.
- **e2e tests:** none.
- **Done:** gates green; every route in the inventory is present; closed in
  `STATE.md`.

### 1.4 Route-coverage gate: `TestOpenAPICoversEveryRoute`

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation.
  **`agent-1:opus` review gate** — this test is the mechanism that makes D4
  real; a weak version of it makes the whole contract decorative.
- **Files:** `internal/server/openapi_test.go`, `internal/server/http.go:11`
  (read only — no change)
- **Change:**
  1. Add `TestOpenAPICoversEveryRoute`. It builds a router the same way
     production does: `NewRouter(NewAuth("t"), NewInventory(nil),
     NewPipeline(nil, nil), NewAPI(nil), NewTopologyAPI(nil), NewAshAPI(nil),
     NewAlertAPI(nil, nil))`. Every constructor already tolerates a `nil` pool
     (`NewAPI` documents this at `internal/server/api.go:43`); if one does not,
     fix the test setup, never the production constructor.
  2. Enumerate the routes with `chi.Walk` (R8):
     ```go
     var got []string
     err := chi.Walk(r.(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
         got = append(got, method+" "+normalize(route))
         return nil
     })
     ```
     `normalize` strips a trailing `/*` and a trailing `/` (chi emits both for
     sub-routers and mounted trees), and collapses `//` to `/`. It must not
     rename path parameters: `{finding-id}` stays `{finding-id}`.
  3. Parse `api/openapi.yaml` and build the same `METHOD /path` set from
     `paths`, upper-casing the method.
  4. Assert **set equality in both directions**, reporting the two differences
     separately: `routes registered but absent from api/openapi.yaml: …` and
     `operations documented but not registered: …`. A one-directional assertion
     is not acceptable — a deleted route must fail too.
  5. Maintain a single explicit allow-list constant `openapiExemptRoutes` for
     routes deliberately undocumented. It starts **empty**. Any entry added later
     needs a comment naming the reason and a `STATE.md` §8 row.
  6. Add a second test `TestOpenAPIOperationIdsAreUnique` asserting no
     `operationId` repeats; duplicated ids silently overwrite generated client
     functions.
- **Unit tests:** the two tests above, plus
  `TestNormalizeChiRoute` — table test covering `"/api/v1/clusters/"`,
  `"/api/v1/instances/{id}/*"`, `"//api/v1/x"` and a plain path.
- **e2e tests:** none.
- **Done:** `make test` fails when a route is added to `http.go` without a spec
  entry — prove it by temporarily adding `r.Get("/api/v1/__probe", ...)`,
  observing the failure, and reverting; record that this proof was performed in
  `STATE.md` §7. Then gates green; closed in `STATE.md`.

### 1.5 Generate a static API reference page

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — mechanical documentation
- **Files:** `Makefile` (`.PHONY` at line 6 and a new target), `docs/api.md`
  (new)
- **Change:** add a make target `api-docs` that renders `api/openapi.yaml` into
  `docs/api.md` — a plain markdown table per tag with `METHOD`, `path`,
  `operationId` and the summary line — using a small Go program under
  `internal/tools/apidocs/` invoked with `go run`. Do **not** add a Node or
  network dependency for this; the repository's documentation build must work
  offline. Commit the generated `docs/api.md`. Link it from `README.md` in 1.6.
- **Unit tests:** `TestAPIDocsGeneratorIsDeterministic` in
  `internal/tools/apidocs/` — runs the generator twice over the checked-in spec
  and asserts byte-identical output, so a regenerated `docs/api.md` never
  produces a spurious diff.
- **e2e tests:** none.
- **Done:** `make api-docs` regenerates `docs/api.md` with no diff on a clean
  tree; gates green; closed in `STATE.md`.

### 1.6 Update README.md

Mandatory closing sub-phase. User guide only — no implementation detail.

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — documentation
- **Files:** `README.md`
- **Change:** in the **HTTP API** section, immediately before **Endpoint index**,
  add a short paragraph stating that the complete machine-readable contract is
  published at `api/openapi.yaml` and rendered at [docs/api.md](docs/api.md), and
  that it is the authority for parameters, defaults and error codes. Add
  `make api-docs` to the command list in **Running the checks**. Leave the
  endpoint index in place — it is still the quick reference.
- **Unit tests:** none (documentation).
- **e2e tests:** none — the two referenced files exist and were regenerated.
- **Done:** a user can find the full contract from the README alone; no
  implementation detail present; gates green; closed in `STATE.md` with the §11
  docs row for phase 1 set.

---

## Phase gates

- **Fmt:** `make fmt-check`
- **Lint:** `make lint`
- **Test subset:** `make test` (must include `TestOpenAPICoversEveryRoute`) and
  `make coverage-gate`
- **Regression guard:** `make test-integration` and `make test-e2e` still green
  — this phase adds no handler, so any failure is a real regression
- **README:** updated with the contract location and the new make target

## Phase done criterion

`api/openapi.yaml` documents all 41 routes in the inventory,
`TestOpenAPICoversEveryRoute` is part of `make test` and has been demonstrated to
fail on an undocumented route, `docs/api.md` regenerates deterministically, and
`STATE.md` §11 shows phase 1 `DONE` with every sub-phase closed.
