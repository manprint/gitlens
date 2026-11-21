# Plan 007 — execution protocol (read once; live state is STATE.md)

1. Start: read STATE.md §1. If ownership is active and you are not its writer,
   an explicit user invocation counts as a resume request: take over only if
   STATE.md and a fresh snapshot (step 3) are unchanged between first read and
   takeover write; record previous/new writer, time, reason; otherwise ask.
   The header's repo path is a locator: when the checkout moved, confirm the
   branch and baseline exist here, update it, and go on. Only Execute sets or
   replaces the saved scope; Execute verify, Task, Bug, and Verify run their
   own units beside it and never change §3 defaults, and Task, Bug, and Verify
   also keep the roster. Resume recovers OPEN or pending work first, then
   continues the saved scope, never a TODO unit named by ID or work outside it.
2. Recover: compare HEAD and a fresh snapshot with the unit base; preserve
   unrelated changes; an unresolved `Last closure` reference (step 7) is a
   pending closure to finish first. Resume OPEN work at its first unverified
   step; inspect the diff even if §6 is stale. A Verify of unfinished work saves
   §1 and §6 to §12 first, no nested suspension.
3. Open before editing: §1 unit, attempt, OPEN, step S1, assigned → actual
   agent; mark sub-phase and phase IN_PROGRESS. Base = HEAD + snapshot tree:
   ```bash
   idx="$(git rev-parse --git-dir)/pev-snapshot.$$.index"
   { GIT_INDEX_FILE="$idx" git read-tree HEAD 2>/dev/null ||
     GIT_INDEX_FILE="$idx" git read-tree --empty; } &&
   GIT_INDEX_FILE="$idx" git add -A && GIT_INDEX_FILE="$idx" git write-tree
   rm -f "$idx"
   ```
   Compare later with `git diff --stat <base-tree> <tree>`. Checkpoint §6 only
   before delegation, risky/long work, a blocker, or a likely pause.
4. Implement the phase file's steps; locate symbols, not line numbers. Missing
   or contradictory design, or two failed fixes of one issue, go to agent-1,
   who may revise technical steps within the approved scope; never weaken
   tests. Run the unit's focused checks and confirm tests actually executed.
   Required review by the assigned reviewer; `self-review` when it is the
   implementer.
5. Autonomy loop: continue to the next eligible unit in the same turn. Stop
   only at: scope complete; with autonomy false, after a phase closure P<N>
   (Task/Bug: at the approval gate); a real blocker needing unavailable
   capability or authority (BLOCKED, with question and owner); a safety stop
   (a possibly live writer, unowned changes a commit would capture); a user
   stop, handoff, or deadline; the host ending the session. Never stop for a
   closed sub-phase, a commit, context size, a substitutable agent, or to
   report progress; never ask the user to type resume inside the scope.
   With autonomy true never ask the user anything: agent-1 weighs the options
   against the goal, picks the one it recommends, and records it (`D<n>`
   `autonomous`, or history.md notes) with alternatives, rationale, risk, and
   reversal; never as a way to push, deploy, use unauthorized network,
   credentials, upgrade dependencies, destroy data, or weaken tests. Permission
   modes never change the scope or the stop latch.
6. Roster: run units assigned to your position or a higher-numbered one;
   record substitutions. Reviews and plan revisions are agent-1's; a model
   outside the roster coordinates but implements nothing.
7. Close: append the unit's evidence to history.md (`cat >> history.md`), update
   §1, §11, next eligible unit, and compact STATE.md (below). With commits
   enabled (autonomy true, or WIP on), record `Last closure: unit:<ID>:<attempt>`,
   stage only owned changes (`git add -A -- <paths>`; for shared paths apply
   only the unit's hunks: `git diff --binary <base-tree> <closure-tree> --
   <path>` → `git reset -q <base-HEAD> -- <path>` → `git apply --cached`, with
   `--3way` fallback, else restore and report a blocker), plus STATE.md and
   history.md, and commit with the mandatory subject (`<plan>` is the folder
   number, `003` or `000_adhoc`, written identically in every subject, trailer,
   and this file's header):
   - `sub-phase(<plan>/<N.M>): sub-phase <N.M> of phase <N> complete — <title>`
   - `phase(<plan>/P<N>): PHASE <N> COMPLETE — <title>; gates and review passed`
   - `plan(<plan>): PLAN <plan> COMPLETE — <feature>; <K> phases, final gates passed`
   - `task(<plan>/T-A<NNN>)`, `bug(<plan>/B-A<NNN>)`,
     `correction(<plan>/V<NNN>-C<n>)`, `verify(<plan>/V<NNN>)`,
     `wip(<plan>/<unit>)`, `handoff(request-by-user)`
   and a final trailer block `PEV-Plan: <plan>`, `PEV-Unit`, `PEV-Attempt`,
   `PEV-Result: complete|wip` beside any Co-Authored-By. Hooks run; never
   `--no-verify`; never push or switch branches. Resolve the reference by exact
   trailer values in `git log --format='%H%x09%(trailers:key=PEV-Unit,valueonly)' HEAD`
   (and PEV-Plan, PEV-Attempt, PEV-Result); zero matches = pending closure.
   With commits disabled, add a §4 row `uncommitted <base-tree>..<closure-tree>`.
8. Phase closure P<N>: all sub-phases DONE or SKIPPED with reason, README
   obligation met, full phase gates pass, agent-1 approves. P<N> never edits
   code or tests; a blocked gate keeps it OPEN/BLOCKED; a failure closes the
   attempt `failed` (commit none), reopens the responsible unit (attempt + 1)
   or revises the plan, then P<N> reruns. After the last P<N>, the plan closure
   PLAN runs G-FINAL and the reference scenario, agent-1's final review, marks
   the plan COMPLETE, and commits — no stop between them.
9. Compaction: STATE.md holds only the present. §4 only unsettled rows; §5/§6
   only the OPEN unit; §7 latest result per gate of the open phase; §8–§10 only
   what pending work needs; §11 expands a phase from its phase file when its
   first unit opens and collapses it to one row at P<N> DONE; a correction that
   reopens a DONE phase re-expands it, every sub-phase DONE with attempt and
   reference from the Git trailers, plus the correction rows. Detail goes to
   history.md first. This file belongs
   to `Format: pev-state 2`; never write a STATE.md of a newer format.
10. Handoff (`handoff`, or a `--deadline`): always honored, whatever the
    autonomy. A deadline is binding: the report is written by that time; keep
    a reserve of at least 10 minutes, start nothing that would cross the cutoff
    (deadline minus reserve), bound long commands with `timeout`. Before the
    cutoff a deadline is no stop, also when it arrives while awaiting a worker:
    record the §1 deadline, keep the worker running, send it the cutoff, arm a
    host wake-up at the cutoff, and continue; a completion notice before the
    cutoff continues the loop. Only a session with no run in progress (its
    invocation already ended with its final report, nothing it started still
    pending) hands off at once. At the cutoff
    or on `handoff`, set §1 `Stop latch: HANDOFF
    <trigger> <time>` and start nothing new; close the OPEN unit only if it takes
    about 10 minutes and still leaves time for the handoff before the
    deadline, else stop at a step boundary; save §6 with commands not
    run; result HANDOFF; release ownership. Code work: one local handoff commit
    even with commits disabled — owned changes of the OPEN or pending unit and
    every §4 `uncommitted` unit (one patch per unit), STATE.md, history.md;
    subject `handoff(request-by-user): <plan>/<unit> stopped <point>`; trailers
    as above with `PEV-Result: wip`, `PEV-Handoff: user|deadline`,
    `PEV-Includes: <id>:<attempt>, …`; those rows become
    `handoff:<id>:<attempt>` with their trees. Never amended. Report the resume
    command, then stop: no tool call after the report, whatever the autonomy or
    permission mode. Only a new explicit user invocation clears the latch.
11. Task/Bug: agent-1 writes or validates the mini-plan (a Bug's with root
    cause) before code. With autonomy false stop once, entry PROPOSED, until a
    user reply or `resume <plan> <entry-id>` approves it.
12. Run end only: cancel any deadline wake-up, release ownership, then end the report with
    `Next` — Mode: the run's mode and every effective setting with its source
    (autonomy, WIP, executor power, deadline, roster, style); Continue: one
    exact `/plan-execute-verify` command and the settings it runs with; Stop:
    `/plan-execute-verify handoff` when uncommitted code work or a pending
    closure remains, else nothing to save. Suggest both; never run them.
````

