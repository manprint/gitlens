# Phase 3 — Embedded SPA serving and dev proxy

> **Intent:** make `pglens-server` serve a single-page application from `/` out
> of the binary itself, with a correct client-side-routing fallback and correct
> caching, before any frontend code exists.
> **Shippable alone?** yes — it ships a placeholder page and the serving
> machinery; phase 4 replaces the placeholder with the real build.
> **Preconditions:** phase 2 DONE (the exemption for non-`/api/` paths must
> already exist, otherwise the assets are gated and the login page cannot load).

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

## Sub-phases

### 3.1 The embed package and its placeholder

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `internal/webui/webui.go` (new), `internal/webui/dist/index.html`
  (new, committed placeholder), `internal/webui/doc.go` (new), `.gitignore`
- **Change:**
  1. Create the package `webui` under `internal/` — it is server-side Go, so it
     belongs beside the other `internal/` packages; do not invent a new top-level
     directory.
  2. `internal/webui/webui.go`:
     ```go
     package webui

     import (
         "embed"
         "io/fs"
     )

     //go:embed all:dist
     var embedded embed.FS

     // Assets returns the built single-page application rooted at dist/.
     func Assets() (fs.FS, error) { return fs.Sub(embedded, "dist") }

     // IsPlaceholder reports whether the embedded tree is the committed
     // placeholder rather than a real build, which is true exactly when
     // dist/.built is absent.
     func IsPlaceholder() bool { … }
     ```
     `all:dist` is required rather than `dist`: without the `all:` prefix
     `go:embed` skips files beginning with `_` or `.`, and Vite emits hashed
     asset directories that must all be included.
  3. Commit `internal/webui/dist/index.html` containing a minimal, valid HTML
     document that says the pglens interface was not built into this binary and
     names the command that builds it (`make web-build`). It must render legibly
     with no CSS and no JavaScript. This file is what keeps `go build ./...`
     working in a checkout that never ran `pnpm` — deleting it breaks the build.
  4. `make web-build` (phase 4) writes the real build into
     `internal/webui/dist/`, overwriting the placeholder and adding a marker file
     `internal/webui/dist/.built` containing the build timestamp and the Vite
     version. Add to `.gitignore`:
     ```gitignore
     internal/webui/dist/*
     !internal/webui/dist/index.html
     ```
     so a real build is never committed while the placeholder always is.
  5. Add `internal/webui/doc.go` with a package comment stating the placeholder
     contract in two sentences, so a future contributor does not "clean up" the
     committed file.
- **Unit tests:** in `internal/webui/webui_test.go`:
  `TestAssets_ContainsIndexHTML` — `Assets()` returns a filesystem where
  `index.html` is readable and non-empty;
  `TestIsPlaceholder_TrueWithoutMarker` — on a clean checkout the placeholder is
  reported;
  `TestPlaceholderMentionsBuildCommand` — the committed `index.html` contains
  the literal string `make web-build`, so the message cannot rot into
  uselessness.
- **e2e tests:** none.
- **Done:** `go build ./...` succeeds on a tree that has never run `pnpm`; gates
  green; closed in `STATE.md`.

### 3.2 The static handler and the SPA fallback

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation.
  **`agent-1:opus` review gate** — route ordering. A fallback registered too
  broadly swallows `/api/` 404s and turns every API typo into an HTML page.
- **Files:** `internal/server/webui.go` (new), `internal/server/http.go:11`
- **Change:**
  1. `func SPAHandler(assets fs.FS) http.Handler` with these rules, in order:
     - Reject any method other than `GET` or `HEAD` with `405`.
     - Clean the request path with `path.Clean` and reject any path that still
       contains `..` with `400`. `http.FS` already prevents escape, but rejecting
       explicitly keeps the intent auditable.
     - If the cleaned path names an existing regular file in `assets`, serve it
       with `http.ServeFileFS` (Go 1.22+) so `Content-Type`, `Last-Modified`,
       range requests and conditional requests come from the standard library.
     - Otherwise serve `index.html` with status `200`. This is the client-side
       routing fallback: `/clusters/7381…` is a React Router route, not a file.
     - Never serve `index.html` for a path under `/assets/`: a missing hashed
       asset must be a `404`, not an HTML document, otherwise a stale
       `index.html` referencing a deleted bundle produces an unreadable error in
       the browser console instead of a clear 404.
  2. Caching:
     - Files under `/assets/` — Vite emits content-hashed names — get
       `Cache-Control: public, max-age=31536000, immutable`.
     - `index.html` and every fallback response get
       `Cache-Control: no-cache, no-store, must-revalidate` so a redeployed
       server is picked up on the next navigation.
     - Add `X-Content-Type-Options: nosniff` to every response from this handler.
  3. Wire it in `NewRouter` **after** every API route is registered:
     ```go
     if uiCfg.Enabled {
         assets, err := webui.Assets()   // handled at construction, not per request
         if err == nil {
             r.NotFound(SPAHandler(assets).ServeHTTP)
         }
     }
     ```
     Using chi's `NotFound` rather than `r.Handle("/*", …)` is deliberate: it
     runs only when no route matched, so it cannot shadow an existing route.
  4. A request to an unmatched path **under `/api/`** must return the JSON
     `Error` shape with `404`, not the SPA. Implement this as the first check
     inside the `NotFound` handler: if the path has the prefix `/api/`, call
     `writeError(w, 404, "not_found", "no such endpoint")`
     (`internal/server/api.go:94`) and return.
  5. `NewRouter` takes the `fs.FS` as a parameter rather than importing
     `internal/webui` directly, so tests can supply an in-memory `fstest.MapFS`.
     `cmd/pglens-server/main.go` performs the `webui.Assets()` call and passes
     the result.
- **Unit tests:** in `internal/server/webui_test.go`, using
  `fstest.MapFS{"index.html": …, "assets/app-abc123.js": …}`:
  `TestSPA_ServesIndexAtRoot`;
  `TestSPA_ServesHashedAsset` — correct body and
  `Cache-Control: public, max-age=31536000, immutable`;
  `TestSPA_FallsBackForClientRoute` — `/clusters/123` returns 200 and the
  `index.html` body;
  `TestSPA_MissingAssetIs404` — `/assets/nope.js` returns 404 with no HTML body;
  `TestSPA_ApiPathIs404Json` — `/api/v1/nope` returns 404 with
  `Content-Type: application/json` and the `Error` shape;
  `TestSPA_RejectsPathTraversal` — `/../go.mod` and `/%2e%2e/go.mod` do not
  escape;
  `TestSPA_RejectsPost` — `POST /` returns 405;
  `TestSPA_IndexIsNotCached` — the fallback response carries `no-store`;
  `TestSPA_SetsNosniff`.
- **e2e tests:** none yet — `SYS-UI-002` covers it through a browser in phase 17.
- **Done:** gates green; `agent-1:opus` has reviewed the ordering and the
  `/api/` guard; closed in `STATE.md`.

### 3.3 Serve the SPA only when the UI is enabled

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `internal/server/http.go`, `cmd/pglens-server/main.go`
- **Change:** with `PGLENS_UI_ENABLED=false` the `NotFound` handler is not
  installed and `/` returns chi's default 404. Log one line at startup naming the
  variable, so an operator who disabled the UI and then wonders where it went can
  find the reason in the logs. Confirm no other behaviour is conditioned on the
  flag: the API keeps working exactly as before.
- **Unit tests:** `TestRouter_UIDisabledServesNoSPA` — a router built with
  `Enabled: false` returns 404 for `/` and still answers `/healthz` with `ok`.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 3.4 Container and compose wiring

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `Dockerfile.server`, `deploy/docker-compose.yml`,
  `deploy/compose/docker-compose.yml`, `deploy/server.example.env`
- **Change:**
  1. `Dockerfile.server`: add a Node build stage **before** the Go build stage.
     Use `node:24-alpine` (Vite 8 requires Node `^20.19.0 || >=22.12.0`, R2),
     `corepack enable`, copy `web/package.json` and `web/pnpm-lock.yaml`, run
     `pnpm install --frozen-lockfile`, copy the rest of `web/`, run
     `pnpm build`, and copy the resulting `web/dist` into the Go stage at
     `internal/webui/dist` before `go build`. Guard the layer ordering so a
     source-only change does not re-run `pnpm install`.
  2. Keep the final image single-stage-slim as it is today; the Node stage must
     not appear in the runtime image.
  3. `deploy/server.example.env`: add the five `PGLENS_UI_*` variables with
     commented defaults and an explicit note that `PGLENS_UI_PASSWORD` must be
     set for the interface to be usable.
  4. `deploy/docker-compose.yml` and `deploy/compose/docker-compose.yml`: add
     `PGLENS_UI_PASSWORD: ${PGLENS_UI_PASSWORD:-}` to the server service's
     environment. Do not invent a default password. Add a comment stating that
     the interface is reachable on the same port as the API.
  5. Do **not** add a `gui` service. D2 removed it: the server serves the UI.
- **Unit tests:** none — build plumbing. Proven by 3.5.
- **e2e tests:** covered by the existing `test/e2e/deploy_test.go` suite, which
  must keep passing.
- **Done:** `make build-images` succeeds and the resulting server image serves a
  non-placeholder page once phase 4 exists; until then it serves the placeholder;
  gates green; closed in `STATE.md`.

### 3.5 Prove both build modes

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — verification
- **Files:** none (verification); `STATE.md` §7
- **Change:** record all four results in `STATE.md` §7:
  1. `git stash -u` any local `internal/webui/dist` build, run `go build ./...`
     and `make test` — proves the placeholder path.
  2. Start the binary with `PGLENS_UI_ENABLED=true` and no `PGLENS_DSN`, then
     `curl -s localhost:8080/ | head -5` — the placeholder page mentioning
     `make web-build` is returned with `Cache-Control: no-store`.
  3. `curl -si localhost:8080/api/v1/nope` — 404 with
     `Content-Type: application/json`.
  4. `make build-images` then `make test-e2e` — the L3 suite is green with the
     SPA handler installed.
- **Unit tests:** none.
- **e2e tests:** the existing L3 smoke suite as the regression guard.
- **Done:** four results recorded; closed in `STATE.md`.

### 3.6 Update README.md

Mandatory closing sub-phase. User guide only — no implementation detail.

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — documentation
- **Files:** `README.md`
- **Change:** in **Running the server**, add two sentences: the web interface is
  served by the server itself on the same address as the API (`http://<host>:8080/`),
  and it requires `PGLENS_UI_PASSWORD` (set in phase 2's configuration table) to
  be usable. Add `PGLENS_UI_ENABLED=false` to the same paragraph as the way to
  turn the interface off. State that a binary built without running the frontend
  build serves a placeholder page. Do not describe the UI's pages yet — nothing is
  usable until phase 8.
- **Unit tests:** none (documentation).
- **e2e tests:** none — the described placeholder response was observed in 3.5.
- **Done:** a user knows where the interface lives and how to disable it; no
  implementation detail present; gates green; closed in `STATE.md` with the §11
  docs row for phase 3 set.

---

## Phase gates

- **Fmt:** `make fmt-check`
- **Lint:** `make lint`
- **Test subset:** `make test` and `make coverage-gate`
- **Build:** `go build ./...` on a tree with only the committed placeholder
- **Regression guard:** `make test-e2e` green, including
  `test/e2e/deploy_test.go`
- **README:** the interface's address, its password requirement and the disable
  switch

## Phase done criterion

A `pglens-server` binary built without any frontend tooling serves a legible
placeholder at `/`, an unmatched `/api/` path returns JSON 404, a client-side
route falls back to `index.html`, a missing hashed asset returns 404,
`PGLENS_UI_ENABLED=false` serves nothing at `/`, the L3 suite is green, and
`STATE.md` §11 shows phase 3 `DONE` with every sub-phase closed.
