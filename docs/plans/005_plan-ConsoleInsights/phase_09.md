# Phase 8 — Tier 2 elevation and final verification

Intent: the monitoring role is raised to Tier 2 on every database of every node with `allow_signal: false` and `commands.enabled: true`; the whole [superseded at 8.1] reference scenario passes; the final report states what was done, what was not, and what needs the user.
Prerequisites: 7 (HA topology verified; roles exist on the clones).
Phase closure: P8 (final phase); review by `advisor`.

## State and ownership contract
Read STATE.md §0–1 first. Open before edits; checkpoint at a recovery boundary; close with evidence. Missing design goes to the `advisor`. Destructive steps need the verified backup first (I-4).

## Local design context
Plan revision: 1.
- REQ-20: raise the deploy to T2 **at the end** (not before); `allow_signal: false` stays (no cancel/terminate), `commands.enabled: true` (already `true` in the stack `agent.yaml` since the first deployment: verify, change nothing). What Tier 2 is, exactly: `deploy/sql/monitoring_user.sql` with `-v tier1=1 -v tier2=1` adds **only** `GRANT pg_signal_backend TO <role>` on top of Tier 1 (cancel/terminate of other sessions); the pgstattuple/explain commands are Tier-1 on-demand commands and need no T2 grant, and the `pgstattuple` extension is created by a DBA (the script leaves it commented). T2 therefore changes no behaviour while `allow_signal: false`: the console keeps offering no signal action [superseded at 8.1]. The grant is cluster-wide (role membership) but the script is idempotent and is run in every database for consistency.
- Invocation (the README example omits the role; the script defaults to the role name `pglens`, the stack's monitoring role is `pglens_monitor`, so `-v role=` is mandatory) — for each database `<db>` in `postgres`, `odoo`, `template1`, on the **current primary** (after the HA migration the node behind the read-write endpoint; standbys replay it):
  `set -a; . /home/agent/pglens-odoo-stack/.env; set +a; docker exec -i <primary container> psql -U odoo -d <db> -v role=pglens_monitor -v pglens_password="$PGLENS_MONITORING_PASSWORD" -v dbname=<db> -v tier1=1 -v tier2=1 -f - < deploy/sql/monitoring_user.sql`
  (the password variable name is the one defined in the stack `.env`; verify the exact name there; the value is never printed or logged).
- I-2: the read-only audit must stay green at T2 (`TestINTRO001`): T2 adds only `pg_signal_backend`, never a write capability the agent uses (signal commands stay refused while `allow_signal: false`).
- Revert (documented in `deploy/sql/README.md`: the script never revokes a higher tier): `REVOKE pg_signal_backend FROM pglens_monitor;` as the superuser (one statement, cluster-wide), then verify `perm_tier = T1`.
- Superseded at 8.1 (autonomous decision, STATE §8 "8.1 allow_signal"): the lab runs `allow_signal: true` on the three targets since 7.5.7 (GAP-8, V-11 at T2 needs it), and the console offers signal actions whenever `perm_tier = T2` (it does not know `allow_signal`; a closed gate is refused by the agent and shown as the command result). So at 8.1 `allow_signal` stays `true` (verified, not changed), `RDY-SIG-01` is absent and Cancel query / Terminate backend are offered and enabled; T2-001 asserts that. Reverting to `allow_signal: false` = restore `ha/agent.ha.yaml.bak-757` and restart `pglens-agent`.
- Final disclosures (REQ-24): the G-UI-LEGACY result (or the exact reason it could not run offline); `tgsend` (AGENTS.md Telegram notifications) is not installed on this machine; the Go module `github.com/pashagolub/pgxmock/v5 v5.2.0` was downloaded through the proxy during integration test runs; assumptions A1, A2, A3 await the user's confirmation; Q2 (webhook URL storage) outcome; Patroni/HAProxy production topology would need network authorisation; the digest-pinned base images of `Dockerfile.agent`/`Dockerfile.server` may have been pulled by the stack image builds (authorised); excluded settings and reasons (`settings-inventory.md`); the Google Chat dev webhook secret lives only in `/home/agent/pglens-odoo-stack/.env` and the user will delete it; the baseline commits of the pre-existing work (D32); REQ-20 deviation: `allow_signal` stays `true` on the three lab targets (console offers Cancel/Terminate at T2), awaiting the user's confirmation; revert = restore `ha/agent.ha.yaml.bak-757` + restart `pglens-agent`; the console does not read `allow_signal` (a closed gate shows up only as the agent's refusal) — product gap for a later plan; local commits exist (WIP/completion), **nothing was pushed**.

## Sub-phases

### 8.1 Tier 2 elevation on the live HA stack
- **Model:** agent-2:claude-opus-5-5
- **Assignment:** execute; `advisor` reviews the grants evidence and `TestINTRO001` result.
- **Files:** READ `deploy/sql/README.md`, `deploy/sql/monitoring_user.sql`, `/home/agent/pglens-odoo-stack/{agent.yaml,.env}`, `internal/agent/readonly_audit_integration_test.go`. WRITE the stack README (what was done, revert command). The stack `agent.yaml` keys are verified, not changed (`commands.enabled: true` in the top-level `commands:` block and `allow_signal: false` on **each** target — a per-target key, one per HA node after 7.2; exact names in `deploy/agent.example.yaml`). Also WRITE `web/e2e/console_visual.spec.ts` (`T2-001`, the unit's live spec; titles start with the test ID).
- **Change:**
  Preconditions: 7 closed; a backup younger than 24 h (take `04-pre-t2` with `backup.sh` if not, from the current primary: `DB_CONTAINER=<leader container>` because the HA lab may have failed over; verify with `verify_backup.sh`).
  Contract: apply the Tier 2 grant with the invocation of the design context in `postgres`, `odoo`, `template1` on the current primary; restart `pglens-agent` (the Playwright restart helpers are guarded by `PGLENS_QA_ALLOW_LIFECYCLE=1`; by hand a plain `docker compose restart` is used); verify `perm_tier = T2` on all three instances through the API; `readiness` (14.x) no longer lists `RDY-T2-01`; the `allow_signal: false` target setting is still `false` (signal controls absent in the UI, `RDY-SIG-01` info present) [superseded at 8.1]; `commands.enabled` is `true` (command audit page reachable); run `go test -tags integration ./internal/agent/ -run TestINTRO001 -count=1` (G-RO) at the end.
  Steps: S1 backup check. S2 apply the grant per database (record the databases done). S3 restart `pglens-agent`. S4 verify via API and UI (Playwright `T2-001`). S5 G-RO.
  Recovery boundary: after S2 (record databases done).
  Failure handling: a grant failing on `template1` → report; revert with `REVOKE pg_signal_backend FROM pglens_monitor;` and verify `perm_tier = T1`.
- **Unit tests:** TestINTRO001 (existing) passes at T2.
- **e2e tests:** `T2-001` (`web/e2e/console_visual.spec.ts`): the three instances show `T2`, the readiness view has no `RDY-T2-01` item and still has `RDY-SIG-01` (informational), no signal controls are offered (cancel/terminate absent) [superseded at 8.1], the Findings and Locks pages still load without errors; `-g T2-001`. T2-001 calls `test.setTimeout(liveTimeout(0, 5 * 30_000))`.
- **Done:** T2 verified on all nodes, G-RO passes, unit closed.

### 8.2 Final gates, documentation and report
- **Model:** agent-2:claude-opus-5-5
- **Assignment:** run and write; `advisor` performs the final review against the reference scenario items 1–17.
- **Files:** READ overview reference scenario, STATE §7/§11. WRITE `README.md`, `docs/*` as owed by phases, `docs/plans/005_plan-ConsoleInsights/FINAL_REPORT.md`.
- **Change:**
  Preconditions: 8.1 done.
  Contract: rerun G-GO-FULL, G-GO-INT-ALL, G-WEB-FULL, G-GO-INT-LONG, G-RO, G-DEPLOY (rebuild from the final tree; `COV-E2E-001` right after it, and excluded from the later all-specs run with `--grep-invert COV-E2E`), G-UI-LEGACY (SYS-UI-000..011 on the final tree), G-VIS (all specs; `VIS-040` after re-running `suggest_ground_truth.py` ≥ 5 minutes and waiting 3 minutes, see 4.5), G-QA (`@indicators`, `@lifecycle` once more on the final build with `--grep-invert LIFE-DOWN-UP-FULL` (the full down/up ran once at 6.4 on the single-node stack and is not repeated on the HA topology; the expected discovery count is the `@lifecycle` count minus one) — the HA-aware harness of 7.2 writes through the read-write endpoint —, `QA-TRACE-001`/`QA-TRACE-002` and `QA-CLEAN-001` with `--project=harness` and an empty `PGLENS_TRACE_DEFER`), G-HA drills quick pass (`HA-STATIC`), the reference scenario items 1–17 each ticked with evidence; verify `git status`: only owned files changed, `internal/webui/dist/index.html` is a regenerable build artefact (it is never staged (commit with explicit pathspecs; `git diff --cached --name-only` must not list it) and never discarded (it was already modified when the plan started, I-4)), no secrets anywhere (G-SECRETS (a) and (b) over the diff, the plan folder, fixtures and e2e artefacts must report zero), no push performed; write `FINAL_REPORT.md` (completed / skipped / blocked / unverified, decisions, deviations, disclosures listed above, next actions, commit list).
  Steps: S1 full gates. S2 live suites. S3 reference-scenario checklist. S4 secrets and tree hygiene check. S5 final report. S6 `advisor` final review.
  Recovery boundary: none.
  Failure handling: any failing gate → correction unit on the owning sub-phase (new attempt) and rerun only invalidated gates.
- **Unit tests:** the full suites (counts recorded).
- **e2e tests:** the live suites listed.
- **Done:** every gate passes on the recorded revision, scenario items proven, report written, scope result COMPLETE (or honestly RUNNING/BLOCKED with the reason), unit closed.

## Phase gates and closure
- Required gates: all (G-GO-FULL, G-GO-INT-ALL, G-GO-INT-LONG, G-UI-LEGACY, G-WEB-FULL, G-RO, G-DEPLOY, G-VIS, G-QA, G-HA, G-STACK, G-SECRETS).
- README obligation: final consistency pass over `README.md`, `docs/LIMITS.md`, `deploy/sql/README.md`, stack README.
- Every sub-phase DONE; `advisor` signs the final review. Open P8, record, mark DONE; commit per STATE §3.
