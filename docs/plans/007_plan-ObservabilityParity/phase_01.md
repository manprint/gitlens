# Phase 0 — Baseline and test images

Intent: the gates are known green (or their failures recorded) before any change, and a test image with `hypopg` and `pg_hint_plan` exists for phases 10 and 13.
Prerequisites: none.
Phase closure: P0; review by agent-1.

## State and ownership contract
Read STATE.md §1 first (and PROTOCOL.md once without this skill) for scope, ownership,
recovery, checks, and commit rules. Open before edits; checkpoint at a recovery boundary;
close with evidence. A delegated worker follows its assigned OPEN unit. Missing design goes
to agent-1, the supervisor. Executor power: low.

## Local design context
Plan revision: 1.
- D19: a unit that adds a migration takes the next free number in `internal/store/migrations/` at open and records it in STATE.md §6.
- D20: the working tree has about 403 uncommitted paths of plan 006. Never stage a path or hunk that this plan did not write.
- R8, R9: `hypopg` 1.4.5 builds on `postgres:18.6-alpine` with `apk add build-base`; `pg_hint_plan` `REL18_1_8_1` also needs `flex` and `bison` (accepted assumption, guarded here).
- Sandbox build rule (project memory): `docker build --network host` with the six proxy build-args (`HTTP_PROXY`, `HTTPS_PROXY`, `http_proxy`, `https_proxy`, `NO_PROXY`, `no_proxy`). `github.com` is not reachable with a trusted certificate from inside a build container: download source archives on the host with `curl`, then `COPY` them into the image. Never turn certificate checks off.

## Sub-phases

### 0.1 Baseline gates recorded
- **Agent:** agent-2
- **Assignment:** run and record; review after results by agent-1.
- **Files:** READ `Makefile` (targets `fmt-check`, `lint`, `vet-tags`, `test`, `web-typecheck`, `web-lint`, `web-coverage-gate`, `web-budget`, `i18n-check`); WRITE STATE.md §7 only.
- **Change:**
  Preconditions: none.
  Contract: no repository file other than STATE.md changes.
  Steps:
  1. S1 — record `git rev-parse HEAD`, `git status --porcelain | wc -l`, and the list of dirty paths count per top directory in STATE.md §7; expected HEAD `b728cb7…` and about 403 paths, or the new values.
  2. S2 — run G-GO-FULL, G-WEB-FULL, G-I18N, G-RO; record result and test counts per gate.
  3. S3 — for a failing gate, record the failing test names. Do not fix them. A failure that exists at baseline is a known failure for later phases; a new failure later is a regression.
  Recovery boundary: none.
  Failure handling: Docker not available → record G-RO as blocked and ask agent-1.
- **Unit tests:** N/A — no code changes.
- **e2e tests:** N/A — no observable behavior changes.
- **Done:** STATE.md §7 holds one row per baseline gate with result and count; unit closed. No completion commit beyond the state change.

### 0.2 Test image with `hypopg` and `pg_hint_plan`
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** NEW `test/pgext/Dockerfile`, `test/pgext/build.sh`, `test/pgext/README.md`; WRITE `Makefile` (new target `pgext-image`), `.gitignore` (ignore `test/pgext/src/`).
- **Change:**
  Preconditions: 0.1 closed.
  Contract: `make pgext-image` produces the local image `pglens-pgext:18` from `postgres:18.6-alpine` with `hypopg` installed and `pg_hint_plan` installed when its build works. The image is local only; nothing is pushed.
  Steps:
  1. S1 — `build.sh`: create `test/pgext/src/`; with `curl -fsSL` download `https://github.com/HypoPG/hypopg/archive/refs/tags/1.4.5.tar.gz` to `src/hypopg.tgz` and `https://github.com/ossc-db/pg_hint_plan/archive/refs/tags/REL18_1_8_1.tar.gz` to `src/hint.tgz`; then run `docker build --network host` with the six proxy build-args, `-t pglens-pgext:18`, `-f test/pgext/Dockerfile test/pgext`.
  2. S2 — `Dockerfile`: `FROM postgres:18.6-alpine`; `COPY src/*.tgz /tmp/`; one `RUN`: `apk add --no-cache --virtual .build build-base flex bison`, unpack each archive with `--strip-components=1`, build hypopg with `make USE_PGXS=1 with_llvm=no install`, build pg_hint_plan with `make with_llvm=no install || touch /pg_hint_plan.FAILED`, `apk del .build`, remove `/tmp/*`.
  3. S3 — `Makefile` target `pgext-image:` runs `test/pgext/build.sh`. `README.md`: what the image is for, the two versions, how to rebuild.
  4. S4 — check: `docker run --rm pglens-pgext:18 sh -c 'ls /usr/local/lib/postgresql/hypopg.so; ls /usr/local/lib/postgresql/pg_hint_plan.so; ls /pg_hint_plan.FAILED'`; expected: `hypopg.so` listed. Record in STATE.md §7 whether `pg_hint_plan.so` exists.
  Recovery boundary: none.
  Failure handling: hypopg build fails → stop, ask agent-1. pg_hint_plan build fails → record "WBK-INT-003 will be skipped: pg_hint_plan not built" in STATE.md §8 and continue (R9 guard).
- **Unit tests:** N/A — build tooling; S4 is the check.
- **e2e tests:** N/A.
- **Done:** image `pglens-pgext:18` exists locally with `hypopg.so`; the pg_hint_plan outcome is recorded; files reviewed; unit closed and committed (`test/pgext/*`, `Makefile` and `.gitignore` hunks only).

## Phase gates and closure
- Required gates: none beyond 0.1 (baseline) and the S4 check of 0.2.
- README obligation: `test/pgext/README.md` written in 0.2; the root README is unchanged and stays accurate.
- Every sub-phase DONE or explicitly justified SKIPPED.
- agent-1 reviews the recorded baseline and the image check.
- Open closure unit P0, record the review and result, mark the phase DONE, and close. Commit subject `phase(007/P0): PHASE 0 COMPLETE — Baseline and test images; gates and review passed`. Never fabricate an empty commit.
- P0 edits no production code or tests.
