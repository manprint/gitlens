# Phase 0 — Plan 002 closure and contract prerequisites

> **Intent:** close the three `OPEN MINOR` findings of plan 002's V001 audit and
> close its deferred question Q-B, so plan 003 starts on a clean register.
> **Shippable alone?** yes — documentation, state and test-evidence only, plus
> one Makefile addition. No product behaviour changes.
> **Preconditions:** none. This is the first phase of plan 003.

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

> **Cross-plan write, intentional.** Sub-phases 0.1 to 0.4 write inside
> `docs/plans/002_plan-AnalysisBackend/`. This is sanctioned by decision **D12**
> in [overview.md](overview.md). Record it once in `STATE.md` §8 as a deviation
> row so a later audit does not read it as scope creep.

---

## Sub-phases

### 0.1 Capture durable evidence for `make test-e2e-full` (closes V001-F1)

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `Makefile:38` (the `test-e2e-full` target), `scripts/e2e_evidence.sh`
  (new), `docs/plans/002_plan-AnalysisBackend/STATE.md` §7, `.gitignore`
- **Change:**
  1. Add `scripts/e2e_evidence.sh`, modelled on the existing
     `scripts/coverage_gate.sh:1` style (`#!/usr/bin/env bash`, `set -euo
     pipefail`). It takes the command to run as arguments, writes combined
     stdout and stderr to `test/e2e/_artifacts/e2e-full-<UTC timestamp>.log`,
     and appends a final machine-readable line to that log:
     `EXIT_STATUS=<n> FINISHED_AT=<RFC3339 UTC> COMMAND=<argv>`. It exits with
     the wrapped command's status. The point of the finding is that the exit
     status must survive the RPC window, so it is written to disk before the
     script returns.
  2. After the wrapped command returns, the script asserts cleanup and appends
     the results to the same log: `RESIDUAL_CONTAINERS=<n>` from
     `docker ps -a --filter 'name=pglens' --format '{{.Names}}' | wc -l`, and
     `RESIDUAL_NETWORKS=<n>` from
     `docker network ls --filter 'name=pglens' --format '{{.Name}}' | wc -l`.
     A non-zero count does **not** change the exit status; it is recorded, so a
     leak is visible without masking the test result.
  3. Add a Makefile target beside the existing E2E targets:
     ```make
     test-e2e-full-evidence:
     	./scripts/e2e_evidence.sh $(MAKE) test-e2e-full
     ```
     Add `test-e2e-full-evidence` to the `.PHONY` list at `Makefile:6`.
  4. Ensure `test/e2e/_artifacts/` log files are ignored by git if the repository
     ignores that directory's contents already; if it does not, add
     `test/e2e/_artifacts/*.log` to `.gitignore`. Do not commit log files.
  5. Run `make test-e2e-full-evidence`. This takes roughly 60 minutes. When it
     returns, read the final line of the produced log and copy the literal
     `EXIT_STATUS=…` line into `docs/plans/002_plan-AnalysisBackend/STATE.md` §7,
     replacing the `V001-e2e-full` row's evidence text with the log filename and
     that line.
  6. If `EXIT_STATUS` is not `0`, stop and fix the failure before closing this
     sub-phase. Record the failing output verbatim in `STATE.md` §7 of **plan
     003**, and open a `bug` unit if the fix is not a one-line correction.
- **Unit tests:** `TestE2EEvidenceScriptRecordsExitStatus` in
  `scripts/e2e_evidence_test.sh` is **not** required — the repository has no
  shell test harness. Instead, prove the script in the Go test suite:
  `TestE2EEvidenceScript_PropagatesExitStatus` in
  `internal/buildinfo/scripts_test.go` (new file, package `buildinfo_test`, no
  build tag) runs `scripts/e2e_evidence.sh /bin/sh -c 'exit 7'` with a temporary
  `test/e2e/_artifacts` root, asserts the command's own exit code is `7`, and
  asserts the produced log's last lines contain `EXIT_STATUS=7`. Skip the test
  with `t.Skip` when `runtime.GOOS != "linux"` or when `bash` is absent.
  > If `internal/buildinfo` does not exist, place the test in a new package
  > `internal/scripts` with a `doc.go` that states the package holds tests for
  > repository shell scripts. Do not invent a package that also holds product
  > code.
- **e2e tests:** none new — this sub-phase produces the evidence for the existing
  full E2E suite rather than adding a scenario.
- **Done:** `make fmt-check`, `make lint`, `make test`, `make coverage-gate` all
  green; `make test-e2e-full-evidence` recorded `EXIT_STATUS=0`; plan 002's
  `STATE.md` §7 `V001-e2e-full` row names the log file and the captured exit
  line; closed in plan 003's `STATE.md` (§1 → 0.2, §4 ledger row, §6 `none`, §11
  board).

### 0.2 Reconcile the phase-10.5 declared scope (closes V001-F2)

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `docs/plans/002_plan-AnalysisBackend/phase_11.md` (sub-phase 10.5
  block), `docs/plans/002_plan-AnalysisBackend/STATE.md` §8
- **Change:**
  1. Read the phase-10.5 block in `phase_11.md` and the phase-close commit's
     diff (`git show --stat <sha>` for the sha in plan 002's `STATE.md` §4 row
     for 10.5).
  2. Extend 10.5's **Files** list so it names every path the closing commit
     actually touched, including the agent production files and their tests. Do
     not remove the documentation and state entries already listed.
  3. Add one sentence to 10.5's **Change** field cross-referencing the
     phase-10.3 capability work that those agent changes belong to, in the form
     `see § 10.3 — <capability>`; the point is that the file list and the
     narrative agree, not that history is rewritten.
  4. Preserve deviation `D-059` exactly as written in plan 002's `STATE.md` §8;
     append a new row recording this reconciliation rather than editing the old
     one.
  5. Re-run the focused agent gates named in phase 10.5's own gate list so the
     reconciled scope is proven, not asserted.
- **Unit tests:** none — plan-document reconciliation. The proof is the gate run
  in step 5.
- **e2e tests:** none.
- **Done:** `make fmt-check`, `make lint`, `make test` green; phase 10.5's Files
  list is a superset of the closing commit's changed paths; `D-059` unchanged and
  a new deviation row present; closed in plan 003's `STATE.md`.

### 0.3 Make the §11 traceability claim precise and complete (closes V001-F3)

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `docs/plans/002_plan-AnalysisBackend/STATE.md` §11 (`Tests` table
  and the sentence introducing it), `scripts/id_audit.sh` (new)
- **Change:**
  1. Add `scripts/id_audit.sh` (`#!/usr/bin/env bash`, `set -euo pipefail`). It
     extracts every `INT-[A-Z0-9-]+` and `SYS-[A-Z0-9-]+` identifier from the Go
     sources under `internal/`, `test/` and `cmd/` (`grep -rhoE`), sorts them
     unique, extracts the same pattern from a `STATE.md` path given as `$1`, and
     prints three sections: `ONLY_IN_SOURCE`, `ONLY_IN_STATE`, and a final
     `DIFF_COUNT=<n>`. It exits `0` always; it is a reporting tool, not a gate,
     because plan 002 is closed.
  2. Run `./scripts/id_audit.sh docs/plans/002_plan-AnalysisBackend/STATE.md`.
  3. Choose **one** of two resolutions and apply it consistently — do not mix
     them:
     - **(preferred)** Add every `ONLY_IN_SOURCE` ID to plan 002's §11 `Tests`
       table with `Status: DONE` and a one-line note naming the file it lives
       in, so the claim becomes true; or
     - Rewrite the sentence above the table to scope the claim precisely, e.g.
       "every `T-*` planned test ID appears below; repository-level `INT-*` and
       `SYS-*` identifiers are catalogued by `scripts/id_audit.sh`, not
       enumerated here", and record the residual `DIFF_COUNT` in that sentence.
     Prefer the first when `DIFF_COUNT` is at or below roughly 60 IDs, which the
     audit's count of 51 satisfies.
  4. Re-run `./scripts/id_audit.sh` and record the resulting `DIFF_COUNT` in plan
     002's `STATE.md` §7 as a new row `V001-F3-id-audit`.
- **Unit tests:** `TestIDAuditScript_ReportsBothDirections` in the same Go test
  package created in 0.1: writes a temporary source tree containing `INT-X-001`
  and a temporary state file containing `SYS-Y-002`, runs the script, and asserts
  the output contains `ONLY_IN_SOURCE` with `INT-X-001`, `ONLY_IN_STATE` with
  `SYS-Y-002`, and `DIFF_COUNT=2`. Skip when `bash` is absent.
- **e2e tests:** none.
- **Done:** gates green; `DIFF_COUNT` after the fix is `0` under the preferred
  resolution, or the scoping sentence names the exact residual count; closed in
  plan 003's `STATE.md`.

### 0.4 Update plan 002's audit register

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — mechanical documentation update.
  **`agent-1:opus` review gate** — the register is the audit trail; a wrong
  status here silently erases a finding.
- **Files:** `docs/plans/002_plan-AnalysisBackend/verify/index.md`,
  `docs/plans/002_plan-AnalysisBackend/verify/verify_001_2026-08-30.md`
  (the three `### V001-F<n>` blocks' `Status` lines)
- **Change:** for each of `V001-F1`, `V001-F2`, `V001-F3`, set the status to
  `FIXED` in **both** the register table in `index.md` and the finding block in
  the report, and append to each finding block one `**Fixed:** 2026-…` line that
  names the evidence: the log filename and captured exit line for F1, the
  reconciled Files list and the new deviation row for F2, the `DIFF_COUNT` result
  for F3. Then update the `index.md` summary sentence and the `Findings` column
  of the V001 row from `3 OPEN MINOR` to `0 OPEN MINOR (3 FIXED)`. Update
  `Open blockers` only if it changes; it currently reads `None` and stays `None`.
  Do not delete or reword the original finding text — a finding never disappears.
- **Unit tests:** none (documentation).
- **e2e tests:** none.
- **Done:** every `V001-F<n>` reads `FIXED` in both files with named evidence; the
  register's counts agree with the finding blocks; `agent-1:opus` has read the
  diff; closed in plan 003's `STATE.md`.

### 0.5 Close plan 002's deferred question Q-B

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — documentation
- **Files:** `docs/plans/002_plan-AnalysisBackend/STATE.md` §9 (the `Q-B` row)
- **Change:** set the `Q-B` row's `Status` to
  `CLOSED 2026-… — resolved in plan 003 as D11: existing contract retained`, and
  set its `Resolve at` column to `plan 003 — D11`. Do not change the question
  text or the assumed default. The decision itself lives in plan 003's
  `overview.md` as **D11**; this row only stops plan 002 from claiming an open
  question that is now answered.
- **Unit tests:** none.
- **e2e tests:** none.
- **Done:** plan 002 `STATE.md` §9 has no row whose status is `DEFERRED`; plan
  003's `overview.md` D11 is the single authority for the answer; closed in plan
  003's `STATE.md`.

### 0.6 Update README.md

Mandatory closing sub-phase. User guide only — no implementation detail.

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — documentation
- **Files:** `README.md` (repo root)
- **Change:** this phase ships one user-visible addition: the
  `make test-e2e-full-evidence` target. Add a single row to the command list in
  **Running the checks** describing it as "full E2E suite with a durable log and
  captured exit status (requires Docker, ~60m)". Verify the rest of the section
  is still accurate; change nothing else. No plan or phase references in the
  README.
- **Unit tests:** none (documentation).
- **e2e tests:** none — the added command was executed in 0.1 and produced the
  documented log.
- **Done:** a reader can run the new target from the README alone; no
  implementation detail present; gates green; closed in `STATE.md` with the §11
  docs row for phase 0 set.

---

## Phase gates

- **Fmt:** `make fmt-check`
- **Lint:** `make lint`
- **Test subset:** `make test` and `make coverage-gate`
- **Long gate:** `make test-e2e-full-evidence` — once, in 0.1, with
  `EXIT_STATUS=0` recorded
- **Regression guard:** `make test-e2e` (L3 smoke) still green; no product
  behaviour changed in this phase, so any failure here is a real regression
- **README:** updated for the one new make target, free of implementation detail

## Phase done criterion

`docs/plans/002_plan-AnalysisBackend/verify/index.md` reports
`0 OPEN MINOR (3 FIXED)` with named evidence for each finding, plan 002's
`STATE.md` §9 contains no `DEFERRED` row, `scripts/e2e_evidence.sh` and
`scripts/id_audit.sh` exist with passing Go tests, README.md documents the new
target, and `STATE.md` §11 shows phase 0 `DONE` with every sub-phase closed.
