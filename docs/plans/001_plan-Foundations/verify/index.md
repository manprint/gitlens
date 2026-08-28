# pglens Foundations — Audit register

> Findings never disappear: they move to `FIXED`, `ACCEPTED`, or `OBSOLETE`,
> always with evidence. Statuses here and in the reports must agree.

## Reports

| # | File | Date | Verdict | Blocker | Major | Minor | Auditor |
|---|------|------|---------|---------|-------|-------|---------|
| V001 | [verify_001_2026-08-27.md](verify_001_2026-08-27.md) | 2026-08-27 | `FAIL` | 2 | 4 | 2 | `agent-1:opus` |
| V002 | [verify_002_2026-08-27.md](verify_002_2026-08-27.md) | 2026-08-27 | `FAIL` | 2 | 3 | 1 | `agent-1:opus` |
| V003 | [verify_003_2026-08-28.md](verify_003_2026-08-28.md) | 2026-08-28 | `FAIL` | 0 | 2 | 1 | `agent-1:opus` |

## Findings

| ID | Severity | Category | Title | Status | Closed by | Evidence |
|----|----------|----------|-------|--------|-----------|----------|
| V001-F01 | `BLOCKER` | divergent | Phases 3–7 marked DONE but implement stubs, acceptance unmet | `FIXED` | V003 | Current implementations, named tests, and acceptance path re-verified |
| V001-F02 | `BLOCKER` | stale-state | STATE.md ledger/board disagree, resume guarantee broken | `FIXED` | `execute verify V001` C06 2026-08-27 | `STATE.md:122-157` ledger 1–53 sequential, §5 55 files, §11 IN_PROGRESS |
| V001-F03 | `MAJOR` | divergent | stat_statements check (§2.5) is stub, cardinality cap unproven | `FIXED` | V003 | Real implementation and INT-STMT integration coverage re-verified |
| V001-F04 | `MAJOR` | untested | Named integration/E2E tests for phases 2–7 absent | `FIXED` | V003 | Named IDs are present and current integration/E2E evidence covers the phase tables |
| V001-F05 | `MAJOR` | rule-violation | README per-phase deliverables for phases 3–7 absent | `FIXED` | `execute verify V001` C05 2026-08-27 | `README.md:1` 405 lines, `CONTRIBUTING.md` 40 lines — re-confirmed V002 |
| V001-F06 | `MAJOR` | failing-gate | make build-images and make lint gates not operational | `FIXED` | `execute verify V001` C07 2026-08-27 | re-confirmed twice by V002: real `make build-images` (exit 0) and `golangci-lint run` → `0 issues` |
| V001-F07 | `MINOR` | divergent | Coverage gate floors pass but mask stub packages | `OBSOLETE` | V003 | The cited packages now contain real implementations; current coverage is ASH 93.9% and topology 90.9% |
| V001-F08 | `MINOR` | divergent | Timescale schema correct, but store write path stubbed | `FIXED` | V003 | DML is real, wired, and integration-tested |
| V001-F09 | `MINOR` | divergent | Per-check `interval`/`top_n` config is parsed but not consumed | `OPEN` | — | `STATE.md:936`; `cmd/pglens-agent/run.go:411-447` schedules checks without `Config.Checks` overrides |
| V001-F10 | `MINOR` | divergent | AgentModeBinary was initially only a name | `FIXED` | V003 | Binary mode and primary-standby matrix are implemented and recorded in row 164 |
| V001-F11 | `MINOR` | untested | Prometheus series/error metrics were missing | `FIXED` | V003 | `/metrics`, series totals, and check errors are implemented and covered |
| V001-F12 | `MINOR` | divergent | Small disk buffers can permanently latch `buffer_full` | `OPEN` | — | `STATE.md:940`; fixed 8 MiB segment roll can prevent recovery below that capacity |
| V001-F13 | `MINOR` | divergent | `Config.IdentityPath` was not consumed | `FIXED` | V003 | Agent startup now seeds the identity-path environment from YAML |
| V001-F14 | `MINOR` | untested | Standby fixture does not attach pre-created physical slot | `OPEN` | — | `test/fixtures/sql/standby_init.sh:29` omits `-S standby1` |
| V002-F01 | `BLOCKER` | divergent | Inventory/Pipeline/API/Staleness implemented but never wired into the running server | `FIXED` | `execute verify V002` C02 2026-08-27 | `main.go` now builds the pool, runs `store.Migrate`, constructs Inventory/Pipeline/API/Staleness and starts staleness; `http.go` calls `api.RegisterRoutes`; `ingest.go` calls `inv.Upsert`→`pipeline.Process`. Proven by `TestIngest_EndToEnd_PushThenClusters` (push then `GET /clusters` reflects it) against a real DB |
| V002-F02 | `BLOCKER` | failing-gate | Syntax error in pipeline_test.go breaks internal/server compilation | `FIXED` | `execute verify V002` C01 2026-08-27 | stray `}` removed at 3 sites (424, 433, 434); two further latent compile bugs uncovered once parsing got past that point and fixed in the same unit: `api_test.go` missing `context` import, `staleness_test.go` calling `gaugeVec.Add` (method added). `go vet`/`gofmt -l` clean, `make test` green |
| V002-F03 | `MAJOR` | rule-violation | L1/L2 test-level separation violated: Docker tests untagged | `FIXED` | `execute verify V002` C03 2026-08-27 | split into `*_integration_test.go` files (`//go:build integration`) for every DB-touching test across `api`, `inventory`, `pipeline`, `staleness`, `setup`; `go test ./internal/server/...` now completes in ~1s with zero Docker activity, `-tags=integration` runs all 24 DB tests green |
| V002-F04 | `MAJOR` | stale-state | STATE.md §1/§6 claim "nothing written yet" while ~2300 lines exist | `FIXED` | `verify V002` 2026-08-27 (state-file edit only) | §6 rewritten to the true half-finished condition at audit time |
| V002-F05 | `MINOR` | rule-violation | Dead no-op methods kept only for stale stub-era tests | `FIXED` | `execute verify V002` C05 2026-08-27 | `Writer.Write()` and `API.Handler()` deleted along with the one test each that only existed to call them; `TestWriter_Wrappers` (the real delegation methods) untouched |
| V002-F06 | `MINOR` | rule-violation | gofmt violations in setup_test.go / write_test.go | `FIXED` | `execute verify V002` C06 2026-08-27 | `gofmt -l .` empty |
| V002-F07 | `BLOCKER` | failing-gate | `make coverage-gate` now fails (56.1% global vs 75% floor) as a direct, correct consequence of fixing F01/F03 | `FIXED` | `execute verify V002` C07 2026-08-27 | user chose "add pool-mocked L1 tests" (mockTx pattern from `internal/store/write_test.go`, generalized to `internal/server/mockpool_test.go`'s `dbPool`/`mockPool`/`mockRow`/`mockRows`/`mockTx`). `internal/server` L1 coverage 41.5% → 75.7%; global 56.1% → **76.3%** (floor 75%). `make coverage-gate` passes |
| V002-F08 | `MINOR` | scope-creep | Test-only DB schema in `setup_test.go` duplicates (does not derive from) the real migrations in `internal/store/migrations/*.sql` | `OPEN` | — | `internal/server/setup_test.go` still hand-copies schema; repeated by V002-F12 |
| V002-F09 | `MINOR` | rule-violation | `README.md` claims `/readyz` checks migration/pool health, but the handler is static 200 | `OPEN` | — | `internal/server/http.go:17-20` is unconditional |
| V002-F10 | `MAJOR` | failing-gate | Coverage gate regressed after real code landed | `FIXED` | V003 | `make coverage-gate` passes at global 76.0% against 75% |
| V002-F11 | `MAJOR` | divergent | Cluster health omitted lag/split-brain semantics | `FIXED` | V003 | Health logic and integration behavior re-verified |
| V002-F12 | `MINOR` | scope-creep | Repeated test-schema drift caused another integration failure | `OPEN` | — | Second occurrence of V002-F08 remains documented |
| V003-F01 | `MAJOR` | stale-state | Audit register and prior report statuses are incomplete/stale | `FIXED` | V003 | `verify/index.md:17-33` and prior report status markers were synchronized during V003; all known findings are now represented with current statuses |
| V003-F02 | `MAJOR` | stale-state | Work-ledger commit cells do not match repository history | `FIXED` | STATE.md row 166 | Reconciled against `git log`/`git show --stat`: rows 1-117 → `bab16f2`, rows 118-158 → `b81f4bf` (rows 159-164 were already correct) |
| V003-F03 | `MINOR` | rule-violation | Authoritative gate commands drift across plan documents | `FIXED` | STATE.md row 166 | `overview.md:185` and `phase_06.md:102-104,119` now quote the Makefile's/`pr.yml`'s actual `35m`/`45m`/`timeout-minutes: 45` |

Status values: `OPEN` · `FIXED` · `ACCEPTED` (user decided to live with it, with the
reason) · `OBSOLETE` (no longer applies, with the reason)

## Open blockers

No implementation blocker is currently open. V003 could not complete the
authoritative `make lint` gate because the bare `golangci-lint` command is not
on `PATH`; the installed GOPATH binary passes directly (`0 issues`). Both of
V003's own findings (F02, F03) were fixed same-day (STATE.md row 166). Open
non-blocking findings are V001-F09, V001-F12, V001-F14, V002-F08, V002-F09,
and V002-F12 — none block any phase or the plan's own acceptance criteria.
