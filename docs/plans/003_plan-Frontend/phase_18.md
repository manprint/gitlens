# Phase 17 — Packaging, UI acceptance suite, documentation

> **Intent:** run the interface against the real stack in a real browser, prove
> the reference scenario, wire it into CI, and finish the documentation.
> **Shippable alone?** yes — this is the release phase.
> **Preconditions:** phases 0 to 16 DONE.

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

### 17.1 The Go-driven UI acceptance runner

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `test/e2e/ui_test.go` (new),
  `test/compose/server-ui.yml` (new), `Makefile`
- **Change:**
  1. `test/e2e/ui_test.go` with the `//go:build e2e` tag, following the shape of
     the existing suites (`test/e2e/smoke_test.go:1`). It:
     - starts the stack with `harness.Start(t, cfg)` using the
       `primary-standby` topology and the current `AGENT_MODE`, plus the new
       `server-ui.yml` overlay that sets `PGLENS_UI_PASSWORD`;
     - waits for the API to report at least one cluster, using the harness's
       existing eventually helper (`test/harness/eventually.go`) rather than a
       sleep;
     - resolves the server's mapped port from the harness and exports
       `PGLENS_UI_BASE_URL`, `PGLENS_UI_PASSWORD` and
       `PGLENS_BOOTSTRAP_TOKEN` into the child environment;
     - runs `pnpm exec playwright test` in `web/` with `exec.CommandContext`,
       streaming output to the test log, and fails the Go test on a non-zero
       exit;
     - registers each UI scenario in `test/scenario` with a `Covers` entry
       naming this plan's phase file and sub-phase, exactly as the existing
       scenarios do, so the repository keeps one traceability mechanism.
  2. `test/compose/server-ui.yml` is an **overlay**, not a copy: it adds only
     the `PGLENS_UI_PASSWORD` environment variable to the server service. The
     default L3 suites must keep running without a UI password so the anonymous
     bearer path stays covered.
  3. Makefile:
     ```make
     test-ui-e2e: web-build build-images
     	$(GO) test -tags=e2e -timeout=40m -count=1 ./test/e2e/... -run 'UI'
     ```
     added to `.PHONY`. `-count=1` matches the existing E2E targets: a cached UI
     pass is a lie.
  4. Playwright browsers are installed once with
     `pnpm exec playwright install --with-deps chromium`; the Makefile target
     checks for the browser and prints that command rather than installing it
     silently, because it downloads a large binary and needs system packages.
  5. Run `SYS-UI-000` (phase 5 § 5.9) end to end to prove the wiring before any
     feature scenario is written.
- **Unit tests:** none — this is test infrastructure.
- **e2e tests:** `SYS-UI-000` — the stack starts, the interface is served, the
  page title is `pglens`.
- **Done:** `make test-ui-e2e` runs `SYS-UI-000` green in both `AGENT_MODE`
  values; the default `make test-e2e` is unaffected; closed in `STATE.md`.

### 17.2 The acceptance scenarios

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation.
  **`agent-1:opus` review gate** — these assertions are what the plan is judged
  by. A weak assertion here makes every preceding phase unprovable.
- **Files:** `web/e2e/*.spec.ts`, `test/e2e/ui_test.go`
- **Change:** implement each scenario below. Every one waits on application
  state through `expect.poll` or `expect(locator)`, never on a timeout (phase 5
  § 5.9).

  **`SYS-UI-001` — failover is visible and identity is stable (the reference
  scenario).**
  1. Sign in; the Fleet Overview shows the cluster with health `ok`.
  2. Capture the rendered `cluster_id` **string** from the DOM and the primary's
     address.
  3. Promote the standby through the harness (`pg_ctl promote`, the existing
     `SYS-REPL` mechanism).
  4. Poll until the Fleet Overview shows the new primary. Assert: the rendered
     `cluster_id` is **byte-identical** to the captured one (invariant I-1, now
     proven at the UI layer, not only the API); the previously-standby instance
     carries the `primary` badge; the Cluster Detail topology reflects the new
     direction; the Cluster Detail timeline contains a `failover_detected` entry
     naming the old and new primary; the Alerts page lists a firing
     `failover_detected` alert.
  5. Assert the API's own `cluster_id` for the same cluster equals the rendered
     one, using the `api` fixture — the UI and the API must agree, which is a
     stronger statement than either alone.

  **`SYS-UI-002` — the interface requires a session.**
  Without signing in, navigating to `/` renders the login form and no cluster
  data; a direct `fetch` of `/api/v1/clusters` from the page context returns
  401; after signing in, the fleet renders; after signing out, `/` returns to
  the login form and the browser back button does not restore data.

  **`SYS-UI-003` — a down agent is visible on the landing page.**
  Stop the agent container through the harness; poll until the Fleet Overview
  shows the agent-down strip naming the instance, and the affected cluster's
  values are marked stale rather than presented as current.

  **`SYS-UI-004` — an unknown value is never rendered as zero.**
  On a standalone topology with no standby, assert the cluster card's maximum
  replay lag renders the unknown treatment and that the string `0 s` does not
  appear in that field.

  **`SYS-UI-005` — ASH disabled reads as disabled.**
  Using the existing `agent-container-ash-disabled.yml` overlay, assert the ASH
  page renders the `Disabled` state naming the configuration key, and that the
  empty-state text is absent.

  **`SYS-UI-006` — the EXPLAIN flow works end to end.**
  On a T1 target, request a plan-only `EXPLAIN` for a known query; poll the
  command to a terminal state; assert the plan tree renders, the plan appears in
  plan history, and an entry appears in the command audit. Assert the recorded
  request contains no query text.

  **`SYS-UI-007` — charts actually paint.**
  Assert the replication lag chart and the ASH chart render a non-empty canvas
  in a real browser (a positive canvas size and a non-blank pixel sample). This
  is the one place pixels are checked, and it exists because every other chart
  test asserts option objects (decision D14).

  **`SYS-UI-008` — contention appears in the blocking tree.**
  Use the existing workload tool (`test/workload`, driven by
  `(*Harness).Workload`) to create a blocking pair; poll until the Locks page
  shows a root blocking one session, with the blocked session listed beneath it.

  **`SYS-UI-009` — cancel is gated and works.**
  On a T0 target, assert the cancel control is disabled and names T2. On a
  T2 target with `allow_signal: true`, start a long-running query, cancel it
  through the UI with the confirmation, and assert the session disappears from
  the activity list and the action appears in the command audit.

  **`SYS-UI-010` — a finding can be muted and unmuted.**
  Poll until at least one finding is open; mute it with a reason and a short
  expiry; assert its state becomes `muted` and the reason is shown; unmute it;
  assert the state returns to its real value on the next pass.

  **`SYS-UI-011` — accessibility across every route.**
  Visit every route in the phase 7 route table with a populated fleet and run
  `@axe-core/playwright` (R14) with the two rules disabled in jsdom (`region`,
  `color-contrast`) **re-enabled**. Assert zero `serious` and zero `critical`
  violations. Attach the full axe report as a Playwright artefact on failure.
- **Unit tests:** none.
- **e2e tests:** `SYS-UI-001` through `SYS-UI-011` as specified.
- **Done:** all eleven scenarios green in `AGENT_MODE=container`; `SYS-UI-001`,
  `SYS-UI-002`, `SYS-UI-003` and `SYS-UI-011` additionally green in
  `AGENT_MODE=binary`; `agent-1:opus` has reviewed the assertions; closed in
  `STATE.md`.

### 17.3 CI integration

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `.github/workflows/e2e.yml`, `.github/workflows/ci.yml`
- **Change:**
  1. Add a `ui-acceptance` job to `e2e.yml`: checkout, `actions/setup-go@v5`,
     `pnpm/action-setup@v4`, `actions/setup-node@v4` with Node 24 and the pnpm
     cache, `make web-install`, `pnpm exec playwright install --with-deps
     chromium`, `make test-ui-e2e`. Upload `test/e2e/_artifacts/ui/**` on
     failure with `actions/upload-artifact@v4`.
  2. Run it on the same matrix dimension the existing E2E job uses
     (`agent_mode`), but restrict the UI matrix to the four scenarios listed in
     17.2 for `binary` to keep the run time bounded; document the restriction in
     the workflow with a comment.
  3. The `web` job from phase 4 § 4.7 stays as the fast gate; `ui-acceptance` is
     the slow one and does not block it.
- **Unit tests:** none.
- **e2e tests:** the workflow itself, validated by a run.
- **Done:** the workflow parses, the job runs green, artefacts are uploaded on a
  deliberately failed run (verify once, then revert); closed in `STATE.md`.

### 17.4 Release packaging

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `Dockerfile.server`, `.github/workflows/release-alpha.yml`,
  `deploy/docker-compose.yml`, `deploy/compose/docker-compose.yml`,
  `deploy/server.example.env`
- **Change:**
  1. Confirm the multi-stage build from phase 3 § 3.4 produces a runtime image
     containing the built interface and no Node runtime; check the image size
     before and after and record both in `STATE.md` §7.
  2. `make build-images-multiarch` must still work: the Node stage has to build
     under `linux/arm64` as well. Verify explicitly rather than assuming.
  3. `release-alpha.yml`: ensure the release job's CI gate now includes the
     `web` job, so an alpha image cannot ship with a failing interface build.
  4. Compose files and `server.example.env`: final review that
     `PGLENS_UI_PASSWORD` is present, unset by default, and documented as
     required for the interface.
  5. Record in `docs/LIMITS.md` the two open questions from
     [overview.md](overview.md): sessions do not survive a server restart (Q-C),
     and there is one shared password rather than user accounts (Q-D).
- **Unit tests:** none.
- **e2e tests:** the `deploy_test.go` suite must stay green.
- **Done:** both image builds succeed, sizes recorded, release workflow gated on
  the web job; closed in `STATE.md`.

### 17.5 Performance and bundle budget

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/scripts/bundle-budget.ts` (new), `Makefile`,
  `.github/workflows/ci.yml`
- **Change:**
  1. A budget check over the Vite build output: the initial entry chunk plus its
     synchronous imports must be under **350 KiB** gzipped, and no single lazy
     chunk over **500 KiB** gzipped. ECharts and React Flow are the two libraries
     that will breach this if imported eagerly; the budget is what forces them to
     stay behind the lazy routes they belong to.
  2. `make web-budget` runs it; add it to `ci-local-unit` and to the `web` CI
     job. Failure prints the offending chunk and its size.
  3. Record the measured sizes in `STATE.md` §7 so future erosion is visible.
- **Unit tests:** `bundle-budget` is exercised by running it against the real
  build; add a small unit test asserting the size parser handles a chunk list
  correctly.
- **e2e tests:** none.
- **Done:** `make web-budget` passes with the measured sizes recorded; closed in
  `STATE.md`.

### 17.6 Full regression sweep

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — verification
- **Files:** none; `STATE.md` §7
- **Change:** run and record every gate, in order:
  1. `make fmt-check lint test coverage-gate`
  2. `make web-install web-lint web-typecheck web-test web-coverage-gate web-build web-budget`
  3. `make test-integration`
  4. `make build-images` then `make test-e2e` and `AGENT_MODE=binary make test-e2e`
  5. `make test-e2e-full-evidence` (the durable-evidence wrapper from phase 0)
  6. `make test-ui-e2e` for both agent modes
  Any failure is fixed in this phase. Record each command, result and timestamp
  in `STATE.md` §7, and the captured `EXIT_STATUS` line for step 5.
- **Unit tests:** none.
- **e2e tests:** the complete suite.
- **Done:** every step green and recorded; closed in `STATE.md`.

### 17.7 Rewrite the README's API examples for authentication

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — documentation.
  **`agent-1:opus` review gate** — final documentation read (mandatory on the
  last phase).
- **Files:** `README.md`
- **Change:** every `curl` example under **HTTP API**, **Alerting**, **Advisor
  findings**, **On-demand operations**, **Monitoring a replicated cluster** and
  **Wait-event analysis** currently issues an anonymous request, which has
  returned 401 since phase 2. Rewrite each one to carry a credential, using one
  consistent pattern established once near the top of **HTTP API**:
  ```sh
  # sign in once; the cookie jar authenticates the examples below
  curl -s -c cookies.txt -X POST localhost:8080/api/v1/session \
    -H 'Content-Type: application/json' -d '{"password":"'"$PGLENS_UI_PASSWORD"'"}'
  ```
  and then `-b cookies.txt` on each subsequent example. Note once that
  `-H "Authorization: Bearer $PGLENS_BOOTSTRAP_TOKEN"` is the equivalent for
  scripts. Verify every rewritten command by running it against a live server
  and confirming the documented output still matches; where an example's output
  has drifted for an unrelated reason, correct the output too and note it in
  `STATE.md` §8.
- **Unit tests:** none (documentation).
- **e2e tests:** none — every example in the README was executed against a
  running server in this sub-phase. That execution is the acceptance criterion.
- **Done:** no anonymous `curl` example remains; every example was executed and
  its documented output verified; `agent-1:opus` has read the whole README;
  closed in `STATE.md`.

### 17.8 Final documentation

Mandatory closing sub-phase. User guide only — no implementation detail.

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — documentation.
  **`agent-1:opus` review gate** — final read.
- **Files:** `README.md`, `docs/LIMITS.md`, `TESTING.md`, `CONTRIBUTING.md`,
  `web/README.md`
- **Change:**
  1. `README.md` **Project status**: replace the sentence stating the frontend is
     outside this repository phase with an accurate description of what now
     ships: a web interface served by the server itself, covering fleet, cluster,
     instance, wait analysis, queries, locks, advisor, alerts and settings.
  2. `README.md` **Web interface**: consolidate the paragraphs added by phases 6
     to 16 into one coherent section with a short subsection per page. Remove
     duplication introduced by incremental edits. This is the section a new user
     reads first, and it must read as one document, not as eleven appended
     paragraphs.
  3. `README.md` **Known limits**: add the interface's own limits — one shared
     password with no user accounts or roles, sessions lost on restart, no pooler
     view, polling rather than streaming so freshness is bounded by the poll
     interval, and no mobile layout beyond a responsive minimum.
  4. `docs/LIMITS.md`: confirm items 10 and the new authentication and pooler
     entries are present and numbered consistently.
  5. `TESTING.md`: final pass so the frontend section matches what was actually
     built, including `make test-ui-e2e` and the `SYS-UI-*` scenario list.
  6. `CONTRIBUTING.md`: the full gate list a change must pass, Go and web.
  7. Read the whole README end to end as a new user would and fix every place
     where the interface and the API examples contradict each other.
- **Unit tests:** none (documentation).
- **e2e tests:** none — the documented interface was exercised in a browser
  against a fresh `docker compose` deployment following only the README's own
  instructions. That is the acceptance criterion for this sub-phase: a new user,
  reading only the README, reaches a working interface.
- **Done:** a new user can go from an empty machine to a signed-in interface
  showing real data using only `README.md`; no implementation detail present; no
  plan or phase references; `agent-1:opus` has read every changed document;
  gates green; closed in `STATE.md` with the §11 docs rows for phase 17 set.

---

## Phase gates

- **Fmt / Lint / Typecheck:** `make fmt-check`, `make lint`, `make web-lint`,
  `make web-typecheck`
- **Test subset:** `make test`, `make web-test`
- **Coverage:** `make coverage-gate`, `make web-coverage-gate`
- **Budget:** `make web-budget`
- **Integration:** `make test-integration`
- **E2E:** `make test-e2e` and `AGENT_MODE=binary make test-e2e`
- **Full E2E:** `make test-e2e-full-evidence` with `EXIT_STATUS=0` recorded
- **UI acceptance:** `make test-ui-e2e` in both agent modes
- **README:** the consolidated web interface section, rewritten API examples, and
  the interface's own known limits

## Phase done criterion

`SYS-UI-001` proves the reference scenario in a real browser — the rendered
`cluster_id` is byte-identical across a promote, the new primary is badged, the
failover appears on the timeline and as a firing alert — with `SYS-UI-002`,
`SYS-UI-003` and `SYS-UI-011` green in both agent modes and all eleven scenarios
green in container mode; every gate above is recorded green in `STATE.md` §7; a
new user can reach a working signed-in interface from `README.md` alone; and
`STATE.md` §11 shows every phase `DONE`, every `T-*` and `SYS-UI-*` test row
resolved, and every docs row set.
