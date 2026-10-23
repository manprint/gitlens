# STATE — 004 Full review

_Last updated: 2026-10-23._

Single source of execution truth for this plan. When this file and the
repository disagree, the repository wins and this file gets corrected.

## §1 — Current position

| Field | Value |
|-------|-------|
| **Phase** | P4 fix workflow `wf_c0572dac-c08` running (13 file-disjoint lanes); P6 catalog checks in parallel on `feat/catalog` |
| **Status** | OPEN |
| **Next action** | When the fix workflow completes: merge the `fix/<lane>` branches one at a time into `review/full-audit-2026-10`, resolve conflicts, apply `doc_notes`/`cross_lane_notes`, rerun the full matrix. |

## §2 — Ledger

| Unit | Result | Commit |
|------|--------|--------|
| P1 baseline | Go unit, web unit green; integration PG15/18 both profiles green on a warm image cache (a cold first run failed 7 packages while images were still being pulled) | — |
| P1 deps | Go 1.27.3 + modules (pgxmock v5, go.yaml.in/yaml/v3); web latest incl. vitest 5, msw 3, TS 7 (+TS 6 API), pnpm 12; all Go and web gates green | 27dd1b8 |
| P1 images | PG 15.19/16.15/17.11/18.6, TimescaleDB 2.31.0-pg17, toxiproxy 2.13.0 | fbe2b55 |
| P3 EXPLAIN guard (D1) | read-only tx, dropped privileges, VERBOSE-plan inspection, hijacked connection discarded | cb62a05 (wip/ro-hardening) |
| P3 role script | refuses a role that could write (attributes, memberships, ownership, every ACL kind, default privileges), resets per-db read-only overrides, reports PUBLIC write paths; INT-PERM-007/008 | 6317436 |
| P3 search_path | agent sessions search `pg_catalog, pg_temp`; extension objects schema-qualified via `check.ExtensionSchema`; EXPLAIN uses the database default; INTCONN008 | 8e40624 |
| P3 test hygiene | INTCONN004 was vacuous (T0 CREATE DATABASE always failed); integration lint clean | 732ae8d, 889b30f |
| P3 harden_public | `-v harden_public=1` revokes the PUBLIC write paths and warns about what is left; INT-PERM-009 | dcbd5b3 |

| P3 role posture | monitoring_role check, security rules, agent preflight | 802c151 |
| P3 settings | connection-sourced values no longer reported | 153b663 |
| P3 read-only audit | INT-RO-001: every check and command at T2 leaves DB + shared catalogs unchanged; every statement reads | 2967a18 |
| G02 PG19 | Supported 15-19, 19rc1 matrix + CI, ASH Buffer, receiver connecting, wal_fpi_bytes | 63ebae7 |
| Facts | monitoring_role + empty settings delivered; wire fact-kind registry; prune on complete result | 985bb72, 39b9736 |
| P2 review | 380 findings (12 critical, 75 high), 367 open; `findings.md` | ef5b31c |
| Merge | `wip/ro-hardening` into `review/full-audit-2026-10` | 0a24e9f |
| P4 lanes | per-lane files `scratchpad/lanes/<lane>.md`, worktrees `scratchpad/lanes/wt-<lane>`, branches `fix/<lane>` from 0a24e9f | running |

## §2b — Open findings outside the workflow

- Capability tiers read direct `pg_auth_members` rows only: a tier granted
  through another role is not detected (conservative direction).

## §3 — Blockers

none

## §4 — Dead ends

- MSW 3 with fully faked timers: every mocked response parks (setImmediate is
  faked) and mutation notifications land 1 ms in the future (a zero-delay timer
  scheduled during a fake tick). Fixed by leaving setImmediate real and the
  `flushNetwork()` helper; see commit 27dd1b8.
