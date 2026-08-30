# Phase 15 — Alerts, silences, rules, events

> **Intent:** the on-call surface: what is firing now, what is suppressed and
> why, which rules are configured, and the event timeline across the fleet.
> **Shippable alone?** yes.
> **Preconditions:** phase 14 DONE.

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

## Contract facts this page must respect

- Ten built-in **Tier 0** rules are always enabled and cannot be disabled:
  `agent_down`, `instance_unreachable`, `check_failing`, `no_primary_in_cluster`,
  `agent_buffer_full`, `clock_skew`, `cardinality_budget_exceeded`,
  `failover_detected`, `split_brain_detected`, `slot_inactive`. The UI must show
  them as non-editable rather than offering a control that will fail.
- Tier 1 rules are editable through `PUT /api/v1/alert-rules/{rule_id}`.
- A silence **suppresses notification** while the alert remains visible and
  continues to be evaluated. `suppressed: true` on an alert is not "resolved".
- Delivery is Slack and generic webhook only. Email and PagerDuty do not exist,
  and the UI must not imply otherwise.
- Alerts carry `alert_key`, `state`, `severity`, `cluster_id` (a string) and
  `suppressed`.

---

## Sub-phases

### 15.1 Alerts derivation library

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/lib/alerts.ts` (new)
- **Change:** pure functions:
  - `rankAlerts(alerts)` → firing before resolved, `critical` before `warning`
    before `info`, unsuppressed before suppressed, then by cluster and key.
    Suppressed alerts sort **below** but are never removed.
  - `summariseAlerts(alerts)` → `{firing: {critical, warning, info},
    suppressed: n, resolved: n}` where `firing` counts **unsuppressed** firing
    alerts and `suppressed` is reported alongside, never folded in.
  - `matchSilence(alert, silence)` → whether a silence's matchers select an
    alert, implementing the same name/value semantics the server uses, so the
    silence editor can preview its effect before it is created.
  - `silenceWindow(silence, now)` → `pending | active | expired` with the
    remaining time.
- **Unit tests (pure):**
  `UI-ALERT-001 ranks firing before resolved and unsuppressed before
  suppressed`;
  `UI-ALERT-002 summarise reports suppressed separately from firing`;
  `UI-ALERT-003 matchSilence selects on an exact name and value`;
  `UI-ALERT-004 matchSilence does not select on a partial value`;
  `UI-ALERT-005 silenceWindow classifies pending, active and expired against the
  frozen clock`;
  `UI-ALERT-006 cluster_id is compared as a string`.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 15.2 The alert list

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/alerts/AlertsPage.tsx`,
  `AlertRow.tsx` (new)
- **Change:**
  1. A summary bar of firing alerts by severity, with the suppressed count shown
     beside it as a separate figure. An on-call view that hides suppression is
     how an incident gets missed.
  2. The list, ranked, each row showing the alert key, severity, state, the
     cluster and instance as links, when it started, and — when suppressed — the
     matching silence with its reason and remaining time, as a link to that
     silence.
  3. Filters for state, severity, cluster, and suppressed, in the URL.
  4. Selecting an alert opens its detail from `getAlert`, showing the labels, the
     rule that produced it, and its history.
  5. `agent_down` and `instance_unreachable` alerts carry a link to the README's
     troubleshooting section, because those two have a known checklist.
- **Unit tests:**
  `UI-ALERT-010 the summary shows suppressed separately from firing`;
  `UI-ALERT-011 a suppressed alert is listed with its silence and remaining
  time`;
  `UI-ALERT-012 filters round-trip through the URL`;
  `UI-ALERT-013 the cluster link uses the exact string cluster id`;
  `UI-ALERT-014 an agent_down alert links to troubleshooting`;
  `expectNoA11yViolations` on the populated list.
- **e2e tests:** `SYS-UI-001` asserts the `failover_detected` alert appears
  after a promote.
- **Done:** gates green; closed in `STATE.md`.

### 15.3 Alert rules

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation.
  **`agent-1:opus` review gate** — this is one of only three places the UI
  writes configuration; a silently failing edit is worse than no editor.
- **Files:** `web/src/features/alerts/RulesPage.tsx`, `RuleEditor.tsx` (new)
- **Change:**
  1. A table from `getAlertRules` showing rule id, tier, severity, thresholds and
     enabled state.
  2. **Tier 0 rules render as non-editable** with a one-line explanation that
     staleness alerts are always on by design — not as a disabled control that
     looks like a permission problem, and never as an editable control that will
     be rejected.
  3. Tier 1 rules open an editor: threshold, duration, severity and enabled,
     with client-side validation matching the server's accepted ranges, and
     `PUT` on save.
  4. No optimistic update. Show the request in flight, render the server's
     response, and on failure keep the editor open with the server's `detail`
     message displayed. A configuration editor that clears itself on failure
     loses the operator's work.
  5. After a successful save, invalidate the rules query and show a confirmation
     naming the rule and the new value.
- **Unit tests:**
  `UI-ALERT-020 tier 0 rules are not editable and explain why`;
  `UI-ALERT-021 a tier 1 rule opens the editor with its current values`;
  `UI-ALERT-022 saving issues a PUT with the changed fields`;
  `UI-ALERT-023 a validation failure is shown without closing the editor`;
  `UI-ALERT-024 a server error keeps the entered values and shows the detail`;
  `UI-ALERT-025 a successful save invalidates the rules query`;
  `expectNoA11yViolations` on the editor`.
- **e2e tests:** none.
- **Done:** gates green; `agent-1:opus` has reviewed; closed in `STATE.md`.

### 15.4 Silences

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/alerts/SilencesPage.tsx`,
  `SilenceEditor.tsx` (new)
- **Change:**
  1. A list from `getSilences` with matchers, reason, window, and state
     (`pending`, `active`, `expired`) from `silenceWindow`.
  2. Creation: matchers as name/value pairs, a mandatory reason, and a start and
     end time with presets. **A live preview**, driven by `matchSilence`, shows
     exactly which currently firing alerts the silence would suppress, before it
     is created. Creating a silence blind is how an operator accidentally
     silences the whole fleet.
  3. A silence matching every currently firing alert requires an extra explicit
     confirmation naming the count.
  4. Deletion via `DELETE`, with confirmation, and an immediate list refresh.
  5. The editor states that a silence suppresses notification only: the alert
     stays visible and continues to be evaluated.
- **Unit tests:**
  `UI-ALERT-030 the preview lists the alerts a silence would suppress`;
  `UI-ALERT-031 a silence matching everything requires extra confirmation naming
  the count`;
  `UI-ALERT-032 the reason is mandatory`;
  `UI-ALERT-033 an end before the start is rejected client-side`;
  `UI-ALERT-034 creation issues a POST with matchers, reason and window`;
  `UI-ALERT-035 deletion confirms and refreshes the list`;
  `UI-ALERT-036 the editor states that suppression is not resolution`;
  `expectNoA11yViolations` on the editor.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 15.5 Notification channels, stated honestly

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/alerts/DeliverySection.tsx` (new)
- **Change:** a small panel stating which delivery channels are configured on
  this server. The API does not expose the configured webhook URLs, and the UI
  must not invent an editor for them: the panel states that Slack and generic
  webhook are the only supported channels, that they are configured by
  environment variable, and names those variables. Where no channel is
  configured, the panel says alerts are persisted but not delivered — which is
  exactly what the server logs at startup, and is the single most surprising
  thing about a fresh deployment.
- **Unit tests:**
  `UI-ALERT-040 the panel names only Slack and generic webhook`;
  `UI-ALERT-041 no channel configured renders the persisted-but-not-delivered
  message`;
  `UI-ALERT-042 no editor for webhook URLs exists` — an explicit absence test.
- **e2e tests:** none.
- **Done:** gates green; closed in `STATE.md`.

### 15.6 Fleet-wide event timeline and T-4 paths

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/features/alerts/EventsPage.tsx`
- **Change:**
  1. The event timeline from phase 9 § 9.5, unfiltered by cluster, with cluster
     and type filters in the URL and `limit` respected (capped at 1000 by the
     server; the UI must state when it has reached the cap rather than implying
     the history ends there).
  2. The remaining T-4 rows: no alerts firing (a good state that must still name
     when it was last evaluated), no events in the range, stale data, 401, 500,
     and a partial failure where alerts load but silences do not — in which case
     suppression state is unknown and every suppressed indication must render
     `Unknown` rather than "not suppressed".
- **Unit tests (route kind):**
  `UI-ALERT-050 no firing alerts renders a positive state naming the last
  evaluation`;
  `UI-ALERT-051 reaching the event limit states that the list is capped`;
  `UI-ALERT-052 a failing silences endpoint renders suppression as Unknown, not
  as not-suppressed`;
  `UI-ALERT-053 stale data renders Stale`;
  `UI-ALERT-054 a 401 navigates to login exactly once`;
  `UI-ALERT-055 a 500 renders ErrorState with a working retry`;
  `UI-ALERT-056 the page polls at the alerts interval`.
- **e2e tests:** none.
- **Done:** every applicable T-4 row has a named test; gates green; closed in
  `STATE.md`.

### 15.7 Update README.md

Mandatory closing sub-phase. User guide only — no implementation detail.

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — documentation
- **Files:** `README.md`
- **Change:** extend **Alerting** with the UI: the alerts page and its
  suppression display, that Tier 0 rules are always on and not editable while
  Tier 1 rules can be edited, that creating a silence previews which alerts it
  would suppress and that suppression is not resolution, and that Slack and
  generic webhook remain the only delivery channels. Keep the existing `curl`
  examples. One paragraph.
- **Unit tests:** none (documentation).
- **e2e tests:** none — the page was exercised against the L3 alerting stack.
- **Done:** an on-call user understands suppression and rule tiers from the
  README alone; gates green; closed in `STATE.md` with the §11 docs row for
  phase 15 set.

---

## Phase gates

- **Fmt / Lint / Typecheck:** `make fmt-check`, `make web-lint`,
  `make web-typecheck`
- **Test subset:** `make web-test`
- **Coverage:** `make web-coverage-gate` — `src/lib/alerts.ts` at or above 95
- **Regression guard:** `make test` and `make test-e2e` still green
- **README:** the alerting UI paragraph

## Phase done criterion

The alerts page counts suppressed alerts separately from firing ones, shows
Tier 0 rules as permanently on rather than as failed edits, previews a silence's
effect before creation and demands extra confirmation for a fleet-wide match,
renders suppression as `Unknown` when the silences endpoint fails, states the
event limit when reached, and `STATE.md` §11 shows phase 15 `DONE` with every
sub-phase closed.
