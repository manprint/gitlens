# Phase 0 — Scaffolding and toolchain

> **Intent:** Create the repository skeleton, the Go module, the build and lint
> gates, and CI, so every later phase has a green baseline to land on.
> **Shippable alone?** yes — pure additive, no behavior exists yet to regress.
> **Preconditions:** none. The working directory currently contains only
> `IDEA.md`, `TESTING.md` and `docs/plans/`, and is **not** a git repository.

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

### 0.1 Initialize the repository

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — mechanical scaffolding.
- **Files:** `.git/` (created), `.gitignore`, `.editorconfig`, `LICENSE`, `NOTICE`
- **Change:**
  1. The repository root
     (`/mnt/fabio/dati/Git/SperimentazioniAI/postgres-analyze`) **is already a git
     repository**, initialized by the user on branch `main` with no commits and
     no remote. Verify that state with `git rev-parse --is-inside-work-tree`,
     `git branch --show-current` and `git remote -v`, and do not re-run
     `git init`. Do not create a nested repository anywhere else.
     **Do not add the `origin` remote yet:** the GitHub repository at
     `https://github.com/manprint/pglens` has not been created. Add it with
     `git remote add origin https://github.com/manprint/pglens.git` once it
     exists, which is the user's action and not part of this sub-phase.
  2. Create `LICENSE` containing the **verbatim Apache License 2.0** text
     (decision D23). Do not paraphrase, do not truncate, do not reformat — an
     altered licence text is not the licence.
  3. Create `NOTICE` with exactly:
     ```
     pglens
     Copyright 2026 The pglens Authors

     This product includes software developed at
     https://github.com/manprint/pglens
     ```
  4. Create `.gitignore`:
     ```gitignore
     # binaries
     /bin/
     /dist/
     pglens-agent
     pglens-server

     # go
     *.test
     *.out
     coverage.out
     coverage.html

     # test artifacts
     /test/e2e/_artifacts/
     /test/**/testdata/fuzz/**/[0-9a-f]*

     # local state
     .env
     *.local.yaml
     ```
  5. Create `.editorconfig`:
     ```ini
     root = true

     [*]
     charset = utf-8
     end_of_line = lf
     insert_final_newline = true
     trim_trailing_whitespace = true

     [*.go]
     indent_style = tab

     [*.{yml,yaml,json,md,sql}]
     indent_style = space
     indent_size = 2
     ```
  6. Do **not** commit anything. This plan does not commit unless `STATE.md` §3
     turns WIP commits on.
- **Unit tests:** none (no code).
- **e2e tests:** none (no behavior change).
- **Done:** `git rev-parse --is-inside-work-tree` prints `true` and
  `git branch --show-current` prints `main`; `git status` lists `IDEA.md`,
  `TESTING.md`, `docs/`, `LICENSE`, `NOTICE`, `.gitignore`, `.editorconfig` as
  untracked; `git remote -v` is still empty; the first line of `LICENSE` is
  `                                 Apache License`; closed in `STATE.md`
  (§1 → next unit, §4 ledger row, §6 `none`, §11 board).

### 0.2 Go module and directory layout

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — module wiring. **`agent-1:opus` review gate:** the module path and package boundaries are the one thing every later import depends on.
- **Files:** `go.mod`, `go.sum`, `cmd/pglens-agent/main.go`, `cmd/pglens-server/main.go`, and one `doc.go` per `internal/` package listed below
- **Change:**
  1. `go mod init github.com/manprint/pglens` (decision D8 — confirmed by the
     user; the remote will be `https://github.com/manprint/pglens`).
  2. Set the toolchain line to `go 1.26` (D1, R4).
  3. Create the package tree. Each directory gets a `doc.go` holding only a
     package clause and a one-sentence package comment — this both compiles and
     documents intent:
     ```
     internal/pgtype/     core value types (ClusterID, InstanceID, Role, Metric, Sample)
     internal/clock/      injectable clock
     internal/identity/   persisted agent and instance identity
     internal/delta/      counter-to-rate conversion and reset detection
     internal/cardinality/ top-N selection, hysteresis, series budget
     internal/check/      check registry and the Check interface
     internal/wire/       agent<->server JSON envelope
     internal/agent/      agent runtime: scheduler, connections, buffer, pusher
     internal/server/     server runtime: ingest, store, api
     internal/store/      TimescaleDB access and migrations
     internal/topology/   replication graph fusion
     internal/ash/        active session history aggregation
     ```
  4. Create the two binaries. Each is a stub that prints its version and exits 0:
     ```go
     // cmd/pglens-server/main.go
     package main

     import (
     	"flag"
     	"fmt"
     	"os"
     )

     var version = "dev" // overridden at build time with -ldflags

     func main() {
     	showVersion := flag.Bool("version", false, "print version and exit")
     	flag.Parse()
     	if *showVersion {
     		fmt.Println(version)
     		return
     	}
     	fmt.Fprintln(os.Stderr, "pglens-server: not implemented yet")
     	os.Exit(1)
     }
     ```
     `cmd/pglens-agent/main.go` is the same with the name changed.
  5. Add the direct dependencies from the `overview.md` reuse map with
     `go get`, then `go mod tidy`. Do not add anything not listed there; a new
     dependency is a decision, and decisions belong in `overview.md`.
  6. **Follow this layout in every later sub-phase.** No new top-level directory
     is created unless this plan names it.
- **Unit tests:** none yet — but `go build ./...` and `go vet ./...` must both succeed, which is what proves the layout compiles.
- **e2e tests:** none (no behavior change).
- **Done:** `go build ./...` exits 0; `go vet ./...` exits 0; `go run ./cmd/pglens-server -version` prints `dev`; `go mod tidy` leaves `go.mod` unchanged when run twice in a row; closed in `STATE.md`.

### 0.3 Build, format and lint gates

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — mechanical tooling setup.
- **Files:** `Makefile`, `.golangci.yml`
- **Change:**
  1. Create `.golangci.yml` enabling: `errcheck`, `govet`, `staticcheck`,
     `ineffassign`, `unused`, `misspell`, `bodyclose`, `rowserrcheck`,
     `sqlclosecheck`, `contextcheck`, `errorlint`, `nilerr`. Set
     `run.timeout: 5m`. Exclude `_test.go` from `errcheck` only for
     `(*testing.T).Setenv`-style helpers if it proves noisy — nothing broader.
  2. Create `Makefile` with these targets. Every target must work from a clean
     checkout with only Go and Docker installed:
     ```makefile
     GO      ?= go
     PKG     := ./...
     VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
     LDFLAGS := -ldflags "-X main.version=$(VERSION)"

     .PHONY: build fmt fmt-check lint test test-integration test-e2e \
             coverage coverage-gate generate golden clean

     build:
     	$(GO) build $(LDFLAGS) -o bin/pglens-agent  ./cmd/pglens-agent
     	$(GO) build $(LDFLAGS) -o bin/pglens-server ./cmd/pglens-server

     fmt:
     	gofmt -w .

     fmt-check:
     	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "unformatted:"; echo "$$out"; exit 1; fi

     lint:
     	golangci-lint run

     test:
     	$(GO) test -race -shuffle=on $(PKG)

     test-integration:
     	$(GO) test -tags=integration -race -shuffle=on $(PKG)

     test-e2e:
     	$(GO) test -tags=e2e -timeout=20m ./test/e2e/...

     coverage:
     	$(GO) test -race -coverprofile=coverage.out -covermode=atomic $(PKG)
     	$(GO) tool cover -html=coverage.out -o coverage.html

     clean:
     	rm -rf bin dist coverage.out coverage.html
     ```
     `coverage-gate`, `generate` and `golden` are added by later sub-phases
     (1.6, 3.1, 2.6 respectively); declare them now as `.PHONY` targets whose
     recipe is `@echo "not implemented until phase <N>"; exit 0` so the Makefile
     is always complete and never fails on a missing target.
  3. `make fmt` then verify `make fmt-check` passes.
- **Unit tests:** none (build tooling).
- **e2e tests:** none.
- **Done:** `make build` produces `bin/pglens-agent` and `bin/pglens-server`; `make fmt-check` exits 0; `make lint` exits 0 with no findings; `make test` exits 0; closed in `STATE.md`.

### 0.4 Continuous integration

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — mechanical CI setup.
- **Files:** `.github/workflows/pr.yml`
- **Change:** create the PR workflow with only the jobs whose gates exist today
  (`lint`, `unit-go`). Later phases add `integration-go` (2.7), `e2e-system`
  (5.8) as separate jobs to this same file — do not create additional workflow
  files.
  ```yaml
  name: PR
  on:
    pull_request:
    push:
      branches: [main]

  concurrency:
    group: ci-${{ github.ref }}
    cancel-in-progress: true

  jobs:
    lint:
      runs-on: ubuntu-latest
      steps:
        - uses: actions/checkout@v4
        - uses: actions/setup-go@v5
          with: { go-version: '1.26', cache: true }
        - run: make fmt-check
        - uses: golangci/golangci-lint-action@v6
          with: { version: latest }

    unit-go:
      runs-on: ubuntu-latest
      steps:
        - uses: actions/checkout@v4
        - uses: actions/setup-go@v5
          with: { go-version: '1.26', cache: true }
        - run: make build
        - run: make test
  ```
- **Unit tests:** none (CI configuration).
- **e2e tests:** none.
- **Done:** the YAML parses (`python3 -c "import yaml,sys;yaml.safe_load(open('.github/workflows/pr.yml'))"` exits 0); both job names appear; every command referenced (`make fmt-check`, `make build`, `make test`) exists in the Makefile and passes locally; closed in `STATE.md`.

### 0.5 Correct the drift in IDEA.md and TESTING.md

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — mechanical documentation correction. **`agent-1:opus` review gate:** these two documents are the project's design record; a wrong number in them propagates into every later decision.
- **Files:** `IDEA.md`, `TESTING.md`
- **Change:** apply exactly these seven corrections and nothing else. Do not
  restructure, do not rewrite prose, do not add sections.
  0. **Project name (do this one first, it touches the most lines).** The name is
     now settled (decision D8): replace `postgres-analyze` with **`pglens`**
     throughout both documents — prose, image names (`postgres-analyze/agent` →
     `ghcr.io/manprint/pglens-agent`, `postgres-analyze/server` →
     `ghcr.io/manprint/pglens-server`), label keys
     (`postgres-analyze.monitor` → `pglens.monitor`, `postgres-analyze.dsn` →
     `pglens.dsn`), paths (`/var/lib/postgres-analyze` → `/var/lib/pglens`) and
     metric prefixes (`postgres_analyze_` → `pglens_`). Then delete the
     "Nome progetto" placeholder paragraph in `IDEA.md` §1 and the trademark and
     `pganalyze`-collision warnings, replacing them with one line recording the
     chosen name and the repository URL — the concern is resolved, and leaving
     the warning in place would make a settled question look open. Update the
     residual "nome definitivo o placeholder" entry in `IDEA.md` §13 the same
     way.
     Note that the working directory is still named `postgres-analyze`; renaming
     it is the user's call and is deliberately **not** part of this sub-phase.
  1. **`IDEA.md` §1 and §3.6 and §11 — supported PostgreSQL range.** Replace
     every occurrence of the range `13 -> 18` / `13-18` / `PG13` support claims
     with **`15 -> 18`**. PostgreSQL 13 reached end of life in November 2025 and
     14 reaches it in November 2026 (decision D11). In §3.6 remove the rows and
     notes that only exist to describe PG13 degradation (`query_id` absent,
     `pg_stat_statements_info.stats_reset` absent) and state in one line that
     from PG15 all of them are unconditionally available. In §11 remove the
     "PG13: funzionalità ridotta" limitation row and replace it with
     "PostgreSQL 13 e 14: non supportati (EOL)".
  2. **`IDEA.md` §4.8 — the window ordering contradiction.** The document
     currently sets buffer `max_age` 6h, server `max_sample_age` 24h, and
     `compress_after` 24h, then states `compress_after` must be *greater than*
     `max_sample_age` — which 24 > 24 does not satisfy. Change
     `max_sample_age` to **12h** and `compress_after` to **48h** (decision D19),
     and keep the sentence explaining why the ordering must be strict.
  3. **`TESTING.md` §1 and §11 and §2 — Go version.** Replace `Go 1.22` /
     `go-version: '1.22'` with **`1.26`** everywhere (R4).
  4. **`TESTING.md` §2 — repository tree.** Replace the `agent/` and `server/`
     top-level entries with `cmd/pglens-agent/` and `cmd/pglens-server/`, and
     rename the `web/` entry to note it is out of scope for the foundations
     plan. Update the `internal/` listing to match the package tree created in
     sub-phase 0.2 (decision D20).
  5. **`TESTING.md` §4.3 and §12.1 — version matrix.** Change the PR matrix from
     `13, 16, 18` to **`15, 18`** and the nightly matrix from `13→18` to
     **`15→18`**. Update the §12.1 table to drop the PG13 and PG14 rows.
  6. **`TESTING.md` §1 and §6 and §7 — frontend scope note.** Add one sentence
     at the top of §6 and §7 stating that L4 and L5 are specified but not
     implemented in the foundations plan (decision D14), so a reader does not
     look for a `web/` directory that does not exist yet.
- **Unit tests:** none (documentation).
- **e2e tests:** none.
- **Done:** `grep -rn "postgres-analyze" IDEA.md TESTING.md` returns nothing; `grep -n "1\.22" TESTING.md` returns nothing; `grep -nE "PG ?13|13 ?-> ?18|13→18" IDEA.md TESTING.md` returns only lines that describe PG13 as unsupported; `IDEA.md` §4.8 shows 6h / 12h / 48h; `TESTING.md` §2 shows `cmd/pglens-agent`; `IDEA.md` §1 records the name `pglens` and the URL `https://github.com/manprint/pglens` with no trademark warning left standing; closed in `STATE.md` with the §11 docs row for phase 0 set.

### 0.6 Update README.md

Mandatory closing sub-phase of every phase. User guide only — no implementation
detail.

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — documentation; `agent-1:opus` reads it on the final phase.
- **Files:** `README.md` (repository root — created here for the first time)
- **Change:** create the initial README with these sections and nothing more:
  - **pglens** — one paragraph: what it is (a self-hostable monitoring system for
    fleets of PostgreSQL instances, focused on replication and wait-event
    analysis) and who it is for (DBAs and platform teams running self-hosted or
    managed PostgreSQL).
  - **Project status** — state plainly that this is pre-alpha, that nothing is
    usable yet, and that the current release builds two binaries which exit with
    an error when run. Do not promise dates.
  - **Requirements** — Go 1.26 or later to build; Docker and Docker Compose for
    the test suites; PostgreSQL 15 to 18 as monitoring targets.
  - **Building** — `make build`, and the paths of the two resulting binaries.
  - **Running the checks** — `make fmt-check`, `make lint`, `make test`, with the
    one-line meaning of each.
  - **Licence** — Apache License 2.0, pointing at `LICENSE`.
  Exclude: module or package names, internal file layout, algorithms, plan or
  phase references, roadmap of unshipped work.
  Write in English, matching the licence and code comments. No emojis, no
  informal language.
  This phase ships nothing user-visible beyond a build: say so explicitly in
  **Project status** rather than implying usable functionality.
- **Unit tests:** none (documentation).
- **e2e tests:** none — every command shown in the README was executed and produced the documented output.
- **Done:** a reader who has never seen the repository can clone it, install Go 1.26, run `make build` and `make test` successfully using only the README; no package, module or internal file name appears anywhere in it; `make fmt-check`, `make lint`, `make test` all green; closed in `STATE.md` with the §11 docs row for phase 0 set.

---

## Phase gates

- **Fmt:** `make fmt-check`
- **Lint:** `make lint`
- **Test subset:** `make test` (passes trivially — there is no test yet, and that is the correct state for this phase)
- **Build:** `make build` produces both binaries
- **Regression guard:** none — nothing existed before this phase
- **README:** created, describing only what this phase made usable (a build), free of implementation detail

## Phase done criterion

From a clean clone with Go 1.26 installed, `make build && make fmt-check &&
make lint && make test` succeeds end to end; the repository is a git repository
on branch `main` with an Apache 2.0 `LICENSE`; `IDEA.md` and `TESTING.md` no
longer contain the version and layout drift listed in 0.5. README.md reflects
this phase's shipped behavior, and `STATE.md` §11 shows phase 0 `DONE` with
every sub-phase closed.
