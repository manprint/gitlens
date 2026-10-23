# STATE — 004 Full review

_Last updated: 2026-10-23._

Single source of execution truth for this plan. When this file and the
repository disagree, the repository wins and this file gets corrected.

## §1 — Current position

| Field | Value |
|-------|-------|
| **Phase** | P2 review workflow finishing; P3 hardening on `wip/ro-hardening` |
| **Status** | OPEN |
| **Next action** | Write `findings.md` from the workflow journal, fix the golden `pg_version` normalization, rerun the integration matrix, commit the image bump, merge `wip/ro-hardening`. |

## §2 — Ledger

| Unit | Result | Commit |
|------|--------|--------|
| P1 baseline | Go unit, web unit green; integration PG15/18 both profiles green on a warm image cache (a cold first run failed 7 packages while images were still being pulled) | — |
| P1 deps | Go 1.27.3 + modules (pgxmock v5, go.yaml.in/yaml/v3); web latest incl. vitest 5, msw 3, TS 7 (+TS 6 API), pnpm 12; all Go and web gates green | 27dd1b8 |
| P1 images | PG 15.19/16.15/17.11/18.6, TimescaleDB 2.31.0-pg17, toxiproxy 2.13.0 | uncommitted |
| P3 EXPLAIN guard (D1) | read-only tx, dropped privileges, VERBOSE-plan inspection, hijacked connection discarded | cb62a05 (wip/ro-hardening) |
| P3 role script | refuses a role that could write (attributes, memberships, ownership, every ACL kind, default privileges), resets per-db read-only overrides, reports PUBLIC write paths; INT-PERM-007/008 | 6317436 |
| P3 search_path | agent sessions search `pg_catalog, pg_temp`; extension objects schema-qualified via `check.ExtensionSchema`; EXPLAIN uses the database default; INTCONN008 | 8e40624 |
| P3 test hygiene | INTCONN004 was vacuous (T0 CREATE DATABASE always failed); integration lint clean | 732ae8d, 889b30f |
| P3 harden_public | `-v harden_public=1` revokes the PUBLIC write paths and warns about what is left; INT-PERM-009 | dcbd5b3 |

## §2b — Open findings outside the workflow

- The settings check reports the agent's own session values (`source` =
  `session`/`user`: `statement_timeout`, `default_transaction_read_only`,
  `search_path`) as instance settings. Fix in P4.
- Capability tiers read direct `pg_auth_members` rows only: a tier granted
  through another role is not detected (conservative direction).

## §3 — Blockers

none

## §4 — Dead ends

- MSW 3 with fully faked timers: every mocked response parks (setImmediate is
  faked) and mutation notifications land 1 ms in the future (a zero-delay timer
  scheduled during a fake tick). Fixed by leaving setImmediate real and the
  `flushNetwork()` helper; see commit 27dd1b8.

## Resume point (2026-10-23 04:35, usage limit)

- wip/ro-hardening (worktree scratchpad/ro-wt) new commits: 63ebae7 PG19 support (all green 19rc1 vanilla + rds-like),
  985bb72 monitoring_role fact + empty settings now pass wire.Fact.Validate (INT-FACT-007, INT-RO-001 validates all facts),
  39b9736 + gofmt: fact kind registry in wire, prune-on-complete (INT-FACT-008), advisor Fact.Datname/Value, database-keyed facts.
- Review workflow wf_ea8749b2-8f6 still verifying at 04:19 (79/84 results); findings.md not yet written. Raw list: scratchpad/journal-findings.txt.
- Known pre-existing lint: contextcheck internal/server/pipeline_integration_test.go:292 (integration tag).
- Next: per-kind typed API route + openapi + UI catalog section (template item G05 triggers), then P4 findings, then P6 fan-out (G03-G12), PostGIS per-database gating inside scrape (Requirements.Extensions is instance-level), extend INT-RO-001 fixture per item.
