# ConsoleInsights — Audit register

## Open audit
none

## Historical reports
| ID | File | Date | Original verdict | Original counts | Auditor |
|----|------|------|------------------|-----------------|---------|
| V001 | [verify_001_2026-11-07.md](verify_001_2026-11-07.md) | 2026-11-07 | PASS WITH FINDINGS | 0 blocker, 0 major, 3 minor | agent-1:claude-opus-5-5 |

Verdicts: PASS, PASS WITH FINDINGS, FAIL, INCOMPLETE. Historical counts/verdicts
are not recomputed as findings are fixed; current unresolved findings are below.

## Findings
| ID | Severity | Title | Current status | Closed by / date | Evidence |
|----|----------|-------|----------------|------------------|----------|
| V001-F1 | MINOR | STATE.md not compacted at PLAN COMPLETE; stale present-state fields | FIXED | V001-C1 2026-11-07 | STATE.md 150 lines, stale fields gone, detail in history.md |
| V001-F2 | MINOR | Documentation row of P4 reads TODO although delivered | FIXED | V001-C1 2026-11-07 | evidence moved to history.md "V001-C1" |
| V001-F3 | MINOR | Closure commits not resolvable by the exact trailer rule | FIXED | V001-C2 2026-11-07 | legacy commit map in history.md "V001-C2" (56 rows, all ancestors of HEAD) |

Statuses: OPEN, FIXED, ACCEPTED (user decision), OBSOLETE (reason recorded).

## Open blockers
none
