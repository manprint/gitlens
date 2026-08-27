# pglens Foundations — Audit register

> Findings never disappear: they move to `FIXED`, `ACCEPTED`, or `OBSOLETE`,
> always with evidence. Statuses here and in the reports must agree.

## Reports

| # | File | Date | Verdict | Blocker | Major | Minor | Auditor |
|---|------|------|---------|---------|-------|-------|---------|
| V001 | [verify_001_2026-08-27.md](verify_001_2026-08-27.md) | 2026-08-27 | `FAIL` | 2 | 4 | 2 | `agent-1:opus` |
| V002 | [verify_002_2026-08-27.md](verify_002_2026-08-27.md) | 2026-08-27 | `FAIL` | 2 | 3 | 1 | `agent-1:opus` |

## Findings

| ID | Severity | Category | Title | Status | Closed by | Evidence |
|----|----------|----------|-------|--------|-----------|----------|
| V001-F01 | `BLOCKER` | divergent | Phases 3–7 marked DONE but implement stubs, acceptance unmet | `OPEN` | — | server portion (phase 3) FIXED by `execute verify V002` C01/C02 2026-08-27 — see V002-F01. Phases 4–7 (agent/topology/ash) untouched, still stubs; `V001-C02`–`C04` not started |
| V001-F02 | `BLOCKER` | stale-state | STATE.md ledger/board disagree, resume guarantee broken | `FIXED` | `execute verify V001` C06 2026-08-27 | `STATE.md:122-157` ledger 1–53 sequential, §5 55 files, §11 IN_PROGRESS |
| V001-F03 | `MAJOR` | divergent | stat_statements check (§2.5) is stub, cardinality cap unproven | `OPEN` | — | `internal/check/stat_statements.go:12` returns `Result{},nil` — untouched by V002 |
| V001-F04 | `MAJOR` | untested | Named integration/E2E tests for phases 2–7 absent | `OPEN` | — | still 0 hits for any plan-named `INT-*`/`SYS-*` ID after V002; V002 added one real end-to-end test (`TestIngest_EndToEnd_PushThenClusters`) but not under the plan's ID convention, and only for phase 3's ingest path |
| V001-F05 | `MAJOR` | rule-violation | README per-phase deliverables for phases 3–7 absent | `FIXED` | `execute verify V001` C05 2026-08-27 | `README.md:1` 405 lines, `CONTRIBUTING.md` 40 lines — re-confirmed V002 |
| V001-F06 | `MAJOR` | failing-gate | make build-images and make lint gates not operational | `FIXED` | `execute verify V001` C07 2026-08-27 | re-confirmed twice by V002: real `make build-images` (exit 0) and `golangci-lint run` → `0 issues` |
| V001-F07 | `MINOR` | divergent | Coverage gate floors pass but mask stub packages | `OPEN` | — | `ash 100% (3 stmts)`, `topology 100% (2 stmts)` still stub-sized and unchanged; related new failure now tracked separately as V002-F07 |
| V001-F08 | `MINOR` | divergent | Timescale schema correct, but store write path stubbed | `FIXED` | `execute verify V002` C02 2026-08-27 | `store.WriteMetrics` et al. now called from the wired `pipeline.Process` and proven end to end by `TestIngest_EndToEnd_PushThenClusters` (real row landing in `metrics`) |
| V002-F01 | `BLOCKER` | divergent | Inventory/Pipeline/API/Staleness implemented but never wired into the running server | `FIXED` | `execute verify V002` C02 2026-08-27 | `main.go` now builds the pool, runs `store.Migrate`, constructs Inventory/Pipeline/API/Staleness and starts staleness; `http.go` calls `api.RegisterRoutes`; `ingest.go` calls `inv.Upsert`→`pipeline.Process`. Proven by `TestIngest_EndToEnd_PushThenClusters` (push then `GET /clusters` reflects it) against a real DB |
| V002-F02 | `BLOCKER` | failing-gate | Syntax error in pipeline_test.go breaks internal/server compilation | `FIXED` | `execute verify V002` C01 2026-08-27 | stray `}` removed at 3 sites (424, 433, 434); two further latent compile bugs uncovered once parsing got past that point and fixed in the same unit: `api_test.go` missing `context` import, `staleness_test.go` calling `gaugeVec.Add` (method added). `go vet`/`gofmt -l` clean, `make test` green |
| V002-F03 | `MAJOR` | rule-violation | L1/L2 test-level separation violated: Docker tests untagged | `FIXED` | `execute verify V002` C03 2026-08-27 | split into `*_integration_test.go` files (`//go:build integration`) for every DB-touching test across `api`, `inventory`, `pipeline`, `staleness`, `setup`; `go test ./internal/server/...` now completes in ~1s with zero Docker activity, `-tags=integration` runs all 24 DB tests green |
| V002-F04 | `MAJOR` | stale-state | STATE.md §1/§6 claim "nothing written yet" while ~2300 lines exist | `FIXED` | `verify V002` 2026-08-27 (state-file edit only) | §6 rewritten to the true half-finished condition at audit time |
| V002-F05 | `MINOR` | rule-violation | Dead no-op methods kept only for stale stub-era tests | `FIXED` | `execute verify V002` C05 2026-08-27 | `Writer.Write()` and `API.Handler()` deleted along with the one test each that only existed to call them; `TestWriter_Wrappers` (the real delegation methods) untouched |
| V002-F06 | `MINOR` | rule-violation | gofmt violations in setup_test.go / write_test.go | `FIXED` | `execute verify V002` C06 2026-08-27 | `gofmt -l .` empty |
| V002-F07 | `BLOCKER` | failing-gate | `make coverage-gate` now fails (56.1% global vs 75% floor) as a direct, correct consequence of fixing F01/F03 | `FIXED` | `execute verify V002` C07 2026-08-27 | user chose "add pool-mocked L1 tests" (mockTx pattern from `internal/store/write_test.go`, generalized to `internal/server/mockpool_test.go`'s `dbPool`/`mockPool`/`mockRow`/`mockRows`/`mockTx`). `internal/server` L1 coverage 41.5% → 75.7%; global 56.1% → **76.3%** (floor 75%). `make coverage-gate` passes |
| V002-F08 | `MINOR` | scope-creep | Test-only DB schema in `setup_test.go` duplicates (does not derive from) the real migrations in `internal/store/migrations/*.sql` | `OPEN` | — | discovered while wiring `store.Migrate` into `main.go`; `getSharedPool` creates its own hand-written `CREATE TABLE` schema instead of calling `store.Migrate`, risking drift between the tested schema and the shipped one. Pre-existing (sub-phase 2.1/3.1 era), not introduced by V002; out of scope for this correction plan, recorded as a deviation |
| V002-F09 | `MINOR` | rule-violation | `README.md` claims `/readyz` "returns 200 when migrations are applied and the pool answers `SELECT 1`", but the handler is a static 200 regardless of pool/migration state | `OPEN` | — | `internal/server/http.go`'s `/readyz` route is `w.WriteHeader(http.StatusOK)` unconditionally, same before and after V002. Pre-existing README/code drift from the C05 README correction (written aspirationally ahead of the wiring); out of scope for this correction plan |

Status values: `OPEN` · `FIXED` · `ACCEPTED` (user decided to live with it, with the
reason) · `OBSOLETE` (no longer applies, with the reason)

## Open blockers

- V001-F01 — server portion FIXED; phases 4–7 (agent/E2E/replication/ASH) still stubs, `V001-C02`–`C04` not started
- (V001-F02/F05/F06/F08, V002-F01–F07 all FIXED — see Findings; V001-F03/F04, V002-F08/F09 still OPEN but non-blocker)

