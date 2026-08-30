# Phase 2 — UI session authentication

> **Intent:** put the entire `/api/v1` surface behind either a UI session cookie
> or the existing agent bearer token, and add the three session endpoints the SPA
> needs.
> **Shippable alone?** yes — the server becomes strictly more restrictive; the
> agent path is unchanged.
> **Preconditions:** phase 1 DONE (the spec must exist so the new routes are
> documented in the same change).

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

> **Behaviour change, loud.** Before this phase every `GET /api/v1/*` answered
> anonymously. After it, an anonymous request receives `401`. Every `curl`
> example in `README.md` breaks; they are rewritten in phase 17, and the L3
> harness client is updated in sub-phase 2.5 of this phase. Any test that
> exercises the API without a credential must be updated in this phase, not
> later.

> **Security scope, stated plainly.** This is a single shared password with no
> user identity, no roles, and no audit of who acted. It raises the bar from
> "anyone who can reach the port can terminate a backend" to "anyone who knows
> the password can". It is not a substitute for network controls. Sub-phase 2.7
> writes that sentence into `docs/LIMITS.md`.

---

## Sub-phases

### 2.1 UI configuration loader

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation.
  **`agent-1:opus` review gate** — the configuration decides whether the gate is
  on; a permissive default here is a security defect.
- **Files:** `internal/server/config.go` (append; follow the existing
  `LoadAlertConfig` shape at `internal/server/config.go:17`)
- **Change:**
  1. Add
     ```go
     type UIConfig struct {
         Enabled      bool
         Password     string
         SessionTTL   time.Duration
         CookieSecure string // "auto" | "true" | "false"
     }
     ```
  2. Add `LoadUIConfig(getenv func(string) string, readFile func(string) ([]byte, error)) (UIConfig, error)`
     with the same nil-defaulting preamble as `LoadAlertConfig`.
  3. Semantics, exactly:
     - `PGLENS_UI_ENABLED` — parsed with `strconv.ParseBool`; empty means
       `true`; an unparseable value is an error naming the variable and the
       value.
     - `PGLENS_UI_PASSWORD_FILE` takes precedence over `PGLENS_UI_PASSWORD`.
       The file's contents are `strings.TrimSpace`d. A read error is a
       configuration error. An empty result after trimming is treated as unset.
     - `PGLENS_UI_SESSION_TTL` — `time.ParseDuration`; empty means `24h`; below
       `5m` or above `720h` is an error stating the accepted range.
     - `PGLENS_UI_COOKIE_SECURE` — empty means `"auto"`; accepted values are
       `auto`, `true`, `false` (case-insensitive); anything else is an error.
     - The password is never returned in an error message, never logged, and
       never included in `fmt.Errorf` arguments. Add a `String()` method on
       `UIConfig` that renders the password as `[redacted]` so an accidental
       `%v` cannot leak it.
  4. Wire it in `cmd/pglens-server/main.go` beside the existing
     `server.LoadAlertConfig` call (around `cmd/pglens-server/main.go:81`):
     `uiCfg, err := server.LoadUIConfig(nil, nil)` with `log.Fatalf` on error.
     Log one line at startup: `UI enabled` / `UI disabled` / `UI enabled but no
     password configured; API requests will be rejected` — never the value.
- **Unit tests:** in `internal/server/config_test.go`:
  `TestLoadUIConfig_Defaults` (no env → `Enabled true`, `SessionTTL 24h`,
  `CookieSecure "auto"`, empty password);
  `TestLoadUIConfig_FileWinsOverInline` (both set → file value, trimmed);
  `TestLoadUIConfig_TTLBounds` (table: `4m` error, `5m` ok, `720h` ok, `721h`
  error, `abc` error);
  `TestLoadUIConfig_EnabledParsing` (table: `""`→true, `"0"`→false,
  `"false"`→false, `"maybe"`→error);
  `TestUIConfigString_RedactsPassword` (asserts the rendered string contains
  `[redacted]` and does not contain the password).
- **e2e tests:** none.
- **Done:** gates green; no test or log path can emit the password; closed in
  `STATE.md`.

### 2.2 Session store

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation.
  **`agent-1:opus` review gate** — token generation, comparison and expiry.
- **Files:** `internal/server/session.go` (new), `internal/server/session_test.go`
  (new)
- **Change:**
  1. `type SessionStore struct` holding a `sync.RWMutex`, a
     `map[string]time.Time` of token to expiry, a `ttl time.Duration`, and a
     `now func() time.Time` seam defaulting to `time.Now` so tests control the
     clock. Do not use a global clock.
  2. `NewSessionStore(ttl time.Duration) *SessionStore`.
  3. `(*SessionStore) Create() (token string, expiresAt time.Time, err error)` —
     reads 32 bytes from `crypto/rand.Read`, encodes with
     `base64.RawURLEncoding`, stores it with `now().Add(ttl)`, returns the token
     and its expiry. A `crypto/rand` failure is returned, never ignored.
  4. `(*SessionStore) Validate(token string) (expiresAt time.Time, ok bool)` —
     `ok` is false for an unknown or expired token. Lookup is a map read; a
     constant-time compare is not required here because the token is a
     high-entropy map key rather than a compared secret, and a timing-safe map
     does not exist. Record that reasoning as a comment on the method so a later
     reviewer does not "fix" it into a linear scan.
  5. `(*SessionStore) Delete(token string)`.
  6. `(*SessionStore) reap()` — removes expired entries. Call it from `Create`
     when the map exceeds 1024 entries, so an unbounded map cannot grow from
     repeated logins. No background goroutine: the store must not need a
     lifecycle.
  7. `(*SessionStore) Len() int` for tests.
  8. Document at the top of the file that the store is in-memory and that a
     server restart invalidates every session (open question Q-C).
- **Unit tests:**
  `TestSessionStore_CreateAndValidate` — a fresh token validates and returns an
  expiry equal to `now+ttl`;
  `TestSessionStore_Expiry` — advance the injected clock past the TTL; `Validate`
  returns `ok=false`;
  `TestSessionStore_Delete` — deleted token no longer validates;
  `TestSessionStore_UnknownToken` — empty string and a random string both fail;
  `TestSessionStore_TokensAreUnique` — 1000 `Create` calls produce 1000 distinct
  tokens of 43 characters (32 bytes, RawURL);
  `TestSessionStore_ReapsExpired` — create 1100 sessions with an advancing clock
  and assert `Len()` drops below the number created;
  `TestSessionStore_ConcurrentAccess` — run with `-race`: 50 goroutines
  interleaving `Create`, `Validate` and `Delete` complete without a data race.
- **e2e tests:** none.
- **Done:** gates green including `make test` under `-race`; closed in
  `STATE.md`.

### 2.3 Session middleware and the `either credential` rule

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation.
  **`agent-1:opus` review gate** — this is the hot path and the security
  boundary. Review the exemption list character by character.
- **Files:** `internal/server/session.go`, `internal/server/http.go:11`
- **Change:**
  1. Add
     `func RequireCredential(cfg UIConfig, auth *Auth, store *SessionStore) func(http.Handler) http.Handler`.
  2. The middleware allows a request when **any** of these holds:
     - the path is exempt (see 3 below);
     - `auth.Validate(r)` is true — the agent's bearer token, unchanged
       behaviour (`internal/server/auth.go:17`);
     - the request carries a `pglens_session` cookie whose value validates
       against the store.
     Otherwise it writes `401` using `writeError` (`internal/server/api.go:94`)
     with `error: "unauthorized"` and `detail: "sign in or present an agent
     token"`. It must not include a `WWW-Authenticate` header: a browser basic-auth
     prompt is not the intended flow.
  3. Exempt paths, exactly and only: `/healthz`, `/readyz`, `/metrics`,
     `POST /api/v1/session`, `GET /api/v1/session`, and every path that does not
     begin with `/api/`. The last clause is what lets the SPA's own assets load
     before login; the SPA itself contains no data. Express the list as a
     package-level `var sessionExemptPaths = map[string]struct{}{…}` plus the
     prefix check, never as inline string comparisons scattered in the handler.
     `DELETE /api/v1/session` is **not** exempt — logging out requires being
     logged in.
  4. When `cfg.Enabled` is true and `cfg.Password` is empty, every non-exempt
     `/api/v1` request returns `503` with
     `error: "ui_password_not_configured"`, so a misconfigured deployment fails
     visibly instead of serving data anonymously. `GET /api/v1/session` still
     answers, reporting `{"authenticated":false,"configured":false}` so the login
     page can explain the problem.
  5. When `cfg.Enabled` is false the middleware is not installed at all and the
     previous anonymous behaviour returns; log one line at startup saying so.
  6. Install it in `NewRouter` as the **first** middleware, before any route is
     registered: `r.Use(RequireCredential(uiCfg, auth, store))`. `NewRouter`'s
     signature grows two parameters, `uiCfg UIConfig` and `store *SessionStore`;
     update the single call site at `cmd/pglens-server/main.go:110` and every
     test constructing a router. Prefer widening the existing signature over
     adding a second constructor — two constructors will drift.
- **Unit tests:** in `internal/server/session_test.go`, using
  `httptest.NewServer` over a router built with a `nil` pool:
  `TestRequireCredential_AnonymousApiIs401` — `GET /api/v1/clusters` without a
  credential returns 401 and a JSON `Error` body;
  `TestRequireCredential_BearerTokenStillWorks` — the same request with
  `Authorization: Bearer <token>` passes the middleware (the handler's own result
  may be an error from the nil pool; assert the status is not 401);
  `TestRequireCredential_SessionCookieWorks` — a cookie from the store passes;
  `TestRequireCredential_ExpiredCookieIs401`;
  `TestRequireCredential_ExemptPaths` — table over `/healthz`, `/readyz`,
  `/metrics`, `POST /api/v1/session`, `GET /api/v1/session`, `/`,
  `/assets/app.js`: all reachable anonymously;
  `TestRequireCredential_DeleteSessionIsNotExempt` — `DELETE /api/v1/session`
  without a credential is 401;
  `TestRequireCredential_PushStillUsesBearerOnly` — `POST /api/v1/push` with a
  valid session cookie but no bearer token is rejected by the ingest handler's
  own check with 401, proving the middleware did not weaken ingest;
  `TestRequireCredential_NoPasswordConfiguredIs503` — `cfg.Enabled` true,
  password empty → 503 with `ui_password_not_configured`;
  `TestRequireCredential_DisabledLeavesRoutesOpen`.
- **e2e tests:** none yet — `SYS-UI-002` in phase 17 proves it through a browser.
- **Done:** gates green; every exemption is covered by a named test; closed in
  `STATE.md`.

### 2.4 The three session endpoints

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `internal/server/session.go`, `internal/server/http.go`,
  `api/openapi.yaml`
- **Change:**
  1. `func RegisterSessionRoutes(r chi.Router, cfg UIConfig, store *SessionStore)`
     registering:
     - `POST /api/v1/session` — decodes `{"password":"…"}` from a body limited
       to 4 KiB with `http.MaxBytesReader`. Compares with
       `subtle.ConstantTimeCompare` after a length check, exactly as
       `(*Auth).Validate` does (`internal/server/auth.go:17`). On success it
       creates a session and writes
       `Set-Cookie: pglens_session=<token>; Path=/; HttpOnly; SameSite=Strict;
       Max-Age=<ttl seconds>` plus `Secure` per `cfg.CookieSecure`, then `204`.
       On failure it sleeps a fixed `250ms` and returns `401`. The delay is
       fixed, not randomised and not exponential: a variable delay leaks
       information and a growing delay is a denial-of-service lever against the
       operator.
     - `GET /api/v1/session` — `200 {"authenticated":true,"expires_at":"…"}`
       with a valid cookie; `401 {"authenticated":false,"configured":true}`
       without; `401 {"authenticated":false,"configured":false}` when no password
       is configured. Always `Cache-Control: no-store`.
     - `DELETE /api/v1/session` — deletes the token, writes an expiring cookie
       (`Max-Age=0`, same attributes), returns `204`.
  2. `Secure` resolution: `auto` sets it when `r.TLS != nil` or
     `r.Header.Get("X-Forwarded-Proto") == "https"`; `true` and `false` force it.
     Put this in a small helper `secureCookie(cfg UIConfig, r *http.Request) bool`
     so it is testable on its own.
  3. Add the three operations to `api/openapi.yaml` with `operationId`s
     `createSession`, `getSession`, `deleteSession`, tagged `session`, with
     `security: []` on the first two and `[{uiSession: []}]` on the third.
     `TestOpenAPICoversEveryRoute` from phase 1 § 1.4 will fail until this is
     done — that is the mechanism working.
- **Unit tests:**
  `TestSessionLogin_Success` — correct password returns 204 and a cookie whose
  attributes are `HttpOnly`, `SameSite=Strict`, `Path=/`;
  `TestSessionLogin_WrongPassword` — 401, no `Set-Cookie`, and the elapsed time
  is at least 250 ms;
  `TestSessionLogin_BodyTooLarge` — a 1 MiB body returns 400, not a panic;
  `TestSessionLogin_MalformedJSON` — 400 with the `Error` shape;
  `TestSessionGet_States` — table over authenticated, unauthenticated, and
  not-configured;
  `TestSessionDelete_ClearsCookie` — 204, `Max-Age=0`, and the token no longer
  validates in the store;
  `TestSecureCookie_Resolution` — table over `auto`+TLS, `auto`+`X-Forwarded-Proto`,
  `auto`+plain, `true`, `false`.
- **e2e tests:** none yet.
- **Done:** gates green including `TestOpenAPICoversEveryRoute`; closed in
  `STATE.md`.

### 2.5 Update the E2E harness client to authenticate

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `test/harness/api.go`, `test/harness/harness.go:630` (`API()`),
  and any compose file under `test/compose/` that starts `pglens-server`
- **Change:**
  1. The harness's `APIClient` currently issues anonymous requests. Add the
     agent bootstrap token as an `Authorization: Bearer` header on every request
     it makes. The token is already known to the harness because it configures
     the agent with it; read it from the same source rather than introducing a
     second constant.
  2. Do **not** teach the harness to log in with a UI password. The harness is a
     machine client; the bearer path is the right one and it keeps the L3 suite
     independent of the UI configuration. The browser path is proven separately
     by `SYS-UI-002` in phase 17.
  3. If any compose file sets `PGLENS_UI_PASSWORD`, remove it; the L3 stack must
     exercise the default configuration. Phase 17 adds a dedicated compose
     overlay that sets it for the UI acceptance run.
  4. Search the repository for other anonymous API callers
     (`grep -rn 'api/v1' --include='*.go' test/ internal/ | grep -v _test.go`
     and the `_test.go` files too) and update each one. `test/e2e/*_test.go`
     files that build their own `http.Client` are in scope.
- **Unit tests:** none new — the harness is test infrastructure. Its correctness
  is proven by the L2/L3 suites in 2.6.
- **e2e tests:** the existing L3 suite is the test. Every scenario must still
  pass unchanged.
- **Done:** `make test-integration` and `make test-e2e` green with the middleware
  installed; no anonymous API caller remains in the repository; closed in
  `STATE.md`.

### 2.6 Full regression sweep

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — verification
- **Files:** none (verification only); `STATE.md` §7
- **Change:** run, in order, and record each result in `STATE.md` §7 with the
  command and the timestamp:
  1. `make fmt-check`, `make lint`, `make test`, `make coverage-gate`
  2. `make test-integration`
  3. `make build-images` then `make test-e2e`
  4. `AGENT_MODE=binary make test-e2e`
  Any failure is a defect introduced by this phase; fix it here rather than
  carrying it forward. Record the verbatim failing output in `STATE.md` §7 while
  it is red.
- **Unit tests:** none.
- **e2e tests:** the existing suites, as the regression guard for invariant I-6.
- **Done:** all four steps green and recorded; closed in `STATE.md`.

### 2.7 Update README.md and LIMITS.md

Mandatory closing sub-phase. User guide only — no implementation detail.

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — documentation
- **Files:** `README.md`, `docs/LIMITS.md`
- **Change:**
  - `README.md` **Configuration** table: add `PGLENS_UI_PASSWORD`,
    `PGLENS_UI_PASSWORD_FILE`, `PGLENS_UI_SESSION_TTL`, `PGLENS_UI_ENABLED`,
    `PGLENS_UI_COOKIE_SECURE` with their types, defaults and meanings from
    [overview.md](overview.md) § Interface.
  - `README.md`: add a short subsection **Signing in** under **Running the
    server** showing the login and an authenticated request:
    ```sh
    curl -s -c cookies.txt -X POST localhost:8080/api/v1/session \
      -H 'Content-Type: application/json' -d '{"password":"'"$PGLENS_UI_PASSWORD"'"}'
    curl -s -b cookies.txt localhost:8080/api/v1/clusters | jq .
    ```
    State that the agent's bearer token also authenticates API requests, and that
    `/healthz`, `/readyz` and `/metrics` stay open.
  - `README.md`: add one sentence at the top of **HTTP API** stating that every
    `/api/v1` endpoint now requires a session cookie or a bearer token, so
    readers are not surprised by the unmodified examples further down. The
    examples themselves are rewritten in phase 17 § 17.7; do not rewrite them
    twice.
  - `docs/LIMITS.md`: extend item 10 (`Tenant separation is not an isolation
    boundary`) with the shared-password reality — one password, no user identity,
    no per-user audit, no roles; a session does not survive a server restart; and
    it is not a substitute for network controls.
- **Unit tests:** none (documentation).
- **e2e tests:** none — the two `curl` commands were executed against a running
  server and produced the documented output.
- **Done:** a new user can configure the password and sign in from the README
  alone; `docs/LIMITS.md` states the boundary precisely; gates green; closed in
  `STATE.md` with the §11 docs row for phase 2 set.

---

## Phase gates

- **Fmt:** `make fmt-check`
- **Lint:** `make lint`
- **Test subset:** `make test` (with `-race`) and `make coverage-gate`
- **Integration:** `make test-integration`
- **Regression guard:** `make test-e2e` and `AGENT_MODE=binary make test-e2e`
  green — invariant I-6, the agent path is untouched
- **README:** configuration table, sign-in subsection and the LIMITS entry

## Phase done criterion

An anonymous `GET /api/v1/clusters` returns 401, the same request with a valid
session cookie or the agent bearer token returns data, `POST /api/v1/push` still
accepts only the bearer token, the full L2 and L3 suites are green in both agent
modes, `docs/LIMITS.md` states the shared-password boundary, and `STATE.md` §11
shows phase 2 `DONE` with every sub-phase closed.
