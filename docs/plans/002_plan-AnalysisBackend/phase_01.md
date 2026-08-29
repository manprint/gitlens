# Phase 0 — Alert data model and pure logic

> **Intent:** create the persisted alert model and every piece of alert logic
> that can be decided without a database or a network, fully unit-tested.
> **Shippable alone?** yes — it adds tables and pure packages; nothing evaluates
> yet, so no behavior changes.
> **Preconditions:** none. Plan 001 is `DONE`.

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

## Conventions that apply to every sub-phase in this phase

- Follow the existing repository layout. New Go packages go under `internal/`,
  one directory per package, with a `doc.go` holding the package comment — this
  is the pattern of every existing package (`internal/delta/doc.go`,
  `internal/cardinality/doc.go`, and so on). Do not create new top-level
  directories.
- Migrations are numbered `.sql` files in `internal/store/migrations/`, embedded
  by `//go:embed migrations/*.sql` in `internal/store/migrate.go:13` and applied
  in filename order at server startup. They are forward-only and must be
  idempotent (`CREATE TABLE IF NOT EXISTS`, `CREATE INDEX IF NOT EXISTS`).
- Every table carries `tenant_id text NOT NULL DEFAULT 'default'` (plan 001 D9).
- Unit tests live next to the code they test, in the same package, named
  `<file>_test.go`.
- No emojis, no informal language in any file.

---

## Sub-phases

### 0.1 Migration `0006_alerts.sql`

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/store/migrations/0006_alerts.sql` (new), `internal/store/migrate_test.go` (new; no existing migration test file is present)
- **Change:** create the four relational tables the alert subsystem needs. Write
  exactly this DDL, in this order:

  ```sql
  -- Alert rules. Tier 0 rules are built into the code and are NOT stored here
  -- (plan 002 D12); this table holds only Tier 1 rules, seeded below and
  -- editable through the API.
  CREATE TABLE IF NOT EXISTS alert_rules (
    rule_id      text        NOT NULL,
    tenant_id    text        NOT NULL DEFAULT 'default',
    enabled      boolean     NOT NULL DEFAULT true,
    severity     text        NOT NULL CHECK (severity IN ('critical','warning','info')),
    scope        text        NOT NULL CHECK (scope IN ('instance','cluster','database')),
    metric       text,
    comparator   text        CHECK (comparator IN ('gt','ge','lt','le','eq','ne')),
    threshold    double precision,
    for_seconds  integer     NOT NULL DEFAULT 0,
    event_type   text,
    summary      text        NOT NULL,
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, rule_id)
  );

  -- One row per distinct alert instance. alert_key is derived by the code
  -- (see sub-phase 0.5) and is stable for as long as the condition holds.
  CREATE TABLE IF NOT EXISTS alerts (
    alert_key    text        NOT NULL,
    tenant_id    text        NOT NULL DEFAULT 'default',
    rule_id      text        NOT NULL,
    severity     text        NOT NULL CHECK (severity IN ('critical','warning','info')),
    state        text        NOT NULL CHECK (state IN ('pending','firing','resolved')),
    cluster_id   bigint,
    instance_id  uuid,
    datname      text,
    labels       jsonb       NOT NULL DEFAULT '{}'::jsonb,
    value        double precision,
    summary      text        NOT NULL,
    started_at   timestamptz NOT NULL,
    last_eval_at timestamptz NOT NULL,
    resolved_at  timestamptz,
    PRIMARY KEY (tenant_id, alert_key, started_at)
  );
  CREATE INDEX IF NOT EXISTS alerts_state_idx
    ON alerts (tenant_id, state, severity, last_eval_at DESC);
  CREATE INDEX IF NOT EXISTS alerts_instance_idx
    ON alerts (tenant_id, instance_id, last_eval_at DESC);

  -- Silences suppress notification, never evaluation: a silenced alert still
  -- appears in the API with suppressed = true.
  CREATE TABLE IF NOT EXISTS silences (
    silence_id  uuid        PRIMARY KEY,
    tenant_id   text        NOT NULL DEFAULT 'default',
    matchers    jsonb       NOT NULL,
    reason      text        NOT NULL,
    created_by  text        NOT NULL DEFAULT 'api',
    starts_at   timestamptz NOT NULL,
    ends_at     timestamptz NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
  );
  CREATE INDEX IF NOT EXISTS silences_window_idx
    ON silences (tenant_id, ends_at DESC);

  -- Delivery log. The unique index is what makes invariant I-4 true: one
  -- delivery per (alert_key, started_at, channel), whatever replica tried.
  CREATE TABLE IF NOT EXISTS notifications (
    notification_id bigserial PRIMARY KEY,
    tenant_id       text        NOT NULL DEFAULT 'default',
    alert_key       text        NOT NULL,
    started_at      timestamptz NOT NULL,
    channel         text        NOT NULL,
    phase           text        NOT NULL CHECK (phase IN ('fire','resolve')),
    sent_at         timestamptz NOT NULL DEFAULT now(),
    ok              boolean     NOT NULL,
    attempts        integer     NOT NULL DEFAULT 1,
    error           text
  );
  CREATE UNIQUE INDEX IF NOT EXISTS notifications_once_idx
    ON notifications (tenant_id, alert_key, started_at, channel, phase);

  -- Tier 1 seed rules. ON CONFLICT DO NOTHING keeps the migration idempotent
  -- and preserves any edit an operator already made through the API.
  INSERT INTO alert_rules (rule_id, severity, scope, metric, comparator, threshold, for_seconds, summary) VALUES
    ('replica.lag_high',            'warning',  'instance', 'pg_replication_lag_seconds',  'gt', 30,   120, 'Standby replay lag above 30 seconds'),
    ('replica.all_standbys_lagging','critical', 'cluster',  'pg_replication_lag_seconds',  'gt', 30,   120, 'Every standby in the cluster is lagging'),
    ('replica.no_sync_standby',     'critical', 'cluster',  'pg_sync_standby_count',       'lt', 1,     60, 'No synchronous standby available'),
    ('conn.near_max',               'warning',  'instance', 'pg_connections_used_ratio',   'gt', 0.8,  120, 'Connections above 80 percent of max_connections'),
    ('conn.idle_in_transaction',    'warning',  'instance', 'pg_max_idle_in_txn_seconds',  'gt', 300,  120, 'A session has been idle in transaction for over 5 minutes'),
    ('txn.long_running',            'warning',  'instance', 'pg_max_xact_age_seconds',     'gt', 900,  120, 'A transaction has been open for over 15 minutes'),
    ('txn.wraparound_risk',         'critical', 'instance', 'pg_max_datfrozenxid_age',     'gt', 1e9,  300, 'Transaction ID age above one billion'),
    ('db.deadlock_rate',            'warning',  'instance', 'pg_deadlocks_total',          'gt', 0.1,  300, 'Deadlocks are occurring'),
    ('archive.failing',             'critical', 'instance', 'pg_archiver_failed_ratio',    'gt', 0,    300, 'WAL archiving is failing'),
    ('disk.free_low',               'critical', 'instance', 'host_disk_free_ratio',        'lt', 0.15, 300, 'Less than 15 percent free space on the data filesystem')
  ON CONFLICT DO NOTHING;
  ```

  > **Note for the implementer:** some metrics named above are produced by later
  > phases (`pg_archiver_failed_ratio` in phase 5, `host_disk_free_ratio` in
  > phase 6). That is intentional and not a bug: a rule whose metric has no data
  > yet simply never fires. Do not remove those rows.

- **Unit tests:** `TestMigrations_0006_Parses` in
  `internal/store/migrate_test.go` — reads the embedded file and asserts it is
  non-empty and contains the four `CREATE TABLE` statements. The repository has
  no existing migration test file, so create this test file following the
  existing `internal/store` test package conventions.
- **e2e tests:** none (no behavior change).
- **Done:** gates green (`make fmt-check`, `make lint`, `make build`,
  `make test`) + the existing `INT-STORE-003` and `INT-INGEST-002` still pass +
  closed in `STATE.md` (§1 → next unit, §4 ledger row, §6 `none`, §11 board).

### 0.2 Core alert types

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — data model; this is a review gate because
  every later sub-phase of phases 0, 1 and 7 depends on these types
- **Files:** `internal/alert/doc.go` (new), `internal/alert/types.go` (new)
- **Change:** create the package `alert` with these declarations and nothing
  else. Use exactly these names — later sub-phases refer to them.

  ```go
  package alert

  // Severity is the fixed three-level scale of plan 002 D16.
  type Severity string

  const (
      SeverityCritical Severity = "critical"
      SeverityWarning  Severity = "warning"
      SeverityInfo     Severity = "info"
  )

  // Scope says what an alert is about.
  type Scope string

  const (
      ScopeInstance Scope = "instance"
      ScopeCluster  Scope = "cluster"
      ScopeDatabase Scope = "database"
  )

  // State is the lifecycle position of one alert.
  type State string

  const (
      StatePending  State = "pending"  // condition true, for_seconds not yet elapsed
      StateFiring   State = "firing"   // condition true long enough, notified
      StateResolved State = "resolved" // condition no longer true
  )

  // Comparator is the relational operator of a threshold rule.
  type Comparator string

  const (
      GT Comparator = "gt"
      GE Comparator = "ge"
      LT Comparator = "lt"
      LE Comparator = "le"
      EQ Comparator = "eq"
      NE Comparator = "ne"
  )

  // Tier separates the rules that cannot be switched off (Tier 0, built into
  // the code) from the site-tunable ones (Tier 1, stored in alert_rules).
  type Tier int

  const (
      Tier0 Tier = 0
      Tier1 Tier = 1
  )

  // Rule is one alert definition. A rule is either metric-driven (Metric,
  // Comparator, Threshold set) or event-driven (EventType set); never both.
  type Rule struct {
      ID         string
      Tier       Tier
      Enabled    bool
      Severity   Severity
      Scope      Scope
      Metric     string
      Comparator Comparator
      Threshold  float64
      For        time.Duration
      EventType  string
      Summary    string
  }

  // Validate reports why a rule is malformed, or nil when it is usable.
  func (r Rule) Validate() error

  // Sample is one evaluated observation a rule is applied to.
  type Sample struct {
      ClusterID  *int64
      InstanceID *uuid.UUID
      Datname    string
      Labels     map[string]string
      Value      float64
      TS         time.Time
  }

  // Alert is the evaluated state of one rule against one sample subject.
  type Alert struct {
      Key        string
      RuleID     string
      Severity   Severity
      State      State
      ClusterID  *int64
      InstanceID *uuid.UUID
      Datname    string
      Labels     map[string]string
      Value      float64
      Summary    string
      StartedAt  time.Time
      LastEvalAt time.Time
      ResolvedAt *time.Time
      Suppressed bool // a silence matches; evaluation still happened
  }
  ```

  `Validate` must reject, with a distinct error message each: an empty `ID`; a
  `Severity` outside the three constants; a `Scope` outside the three constants;
  a rule with neither `Metric` nor `EventType`; a rule with both; a metric rule
  with an unknown `Comparator`; a negative `For`.

- **Unit tests:** in `internal/alert/types_test.go` —
  `TestRule_Validate_RejectsEmptyID`,
  `TestRule_Validate_RejectsUnknownSeverity`,
  `TestRule_Validate_RejectsUnknownScope`,
  `TestRule_Validate_RejectsNeitherMetricNorEvent`,
  `TestRule_Validate_RejectsBothMetricAndEvent`,
  `TestRule_Validate_RejectsUnknownComparator`,
  `TestRule_Validate_RejectsNegativeFor`,
  `TestRule_Validate_AcceptsMetricRule`,
  `TestRule_Validate_AcceptsEventRule`. Each asserts the specific error, not
  merely that an error occurred.
- **e2e tests:** none (no behavior change).
- **Done:** gates green + `make coverage-gate` still passes + closed in
  `STATE.md`.

### 0.3 Threshold evaluation and `for` hysteresis

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/alert/eval.go` (new), `internal/alert/eval_test.go` (new)
- **Change:** implement pure evaluation. No database, no clock reads — time
  always arrives as an argument.

  ```go
  // Compare applies the comparator. An unknown comparator returns false.
  func Compare(v float64, c Comparator, threshold float64) bool

  // Evaluator advances alert state for one rule across successive samples.
  // It holds no clock and no I/O; the caller supplies now.
  type Evaluator struct { /* unexported: per-key first-true timestamp */ }

  func NewEvaluator() *Evaluator

  // Step applies one sample to one rule and returns the alert state after the
  // step, plus a Transition describing what changed.
  //   - condition false and no prior state           -> nil, TransitionNone
  //   - condition true, For elapsed not yet reached   -> pending, TransitionOpened (first time only)
  //   - condition true, now-firstTrue >= For          -> firing,  TransitionFired (first time only)
  //   - condition false and prior state pending       -> nil,     TransitionCancelled
  //   - condition false and prior state firing        -> resolved, TransitionResolved
  func (e *Evaluator) Step(r Rule, s Sample, now time.Time) (*Alert, Transition)

  // Forget drops per-key state for subjects that disappeared, so a deleted
  // instance does not leak memory. Returns how many keys were dropped.
  func (e *Evaluator) Forget(before time.Time) int
  ```

  `Transition` is a string type with constants `TransitionNone`,
  `TransitionOpened`, `TransitionFired`, `TransitionResolved`,
  `TransitionCancelled`.

  Rules the implementer must get exactly right, because the tests assert them:
  - `For == 0` means the alert goes straight to `firing` on the first true
    sample; there is no `pending` step.
  - The boundary is inclusive: at exactly `now - firstTrue == For` the alert
    fires.
  - `StartedAt` is the timestamp of the **first true sample**, not the moment it
    fired. This is what makes the dedup key stable across a restart.
  - A resolved alert that becomes true again gets a **new** `StartedAt`, and
    therefore a new dedup identity.
  - A flap — true, false, true — cancels the first `pending` and starts a fresh
    `firstTrue`; it never carries the old one forward.

- **Unit tests:** in `internal/alert/eval_test.go` —
  `TestCompare_AllOperators` (table-driven over the six comparators including
  the equality boundary),
  `TestStep_ForZeroFiresImmediately`,
  `TestStep_PendingUntilForElapses`,
  `TestStep_FiresExactlyAtBoundary`,
  `TestStep_StartedAtIsFirstTrueSample`,
  `TestStep_FalseCancelsPending`,
  `TestStep_FalseResolvesFiring`,
  `TestStep_RefireGetsNewStartedAt`,
  `TestStep_FlapDoesNotCarryFirstTrue`,
  `TestForget_DropsStaleKeys`.
- **e2e tests:** none.
- **Done:** gates green + `internal/alert` at or above 90 % statement coverage
  measured by `go test -cover ./internal/alert/...` + closed in `STATE.md`.

### 0.4 Silence matching

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/alert/silence.go` (new), `internal/alert/silence_test.go` (new)
- **Change:** implement silence matching as a pure function.

  ```go
  // Matcher is one label predicate. Name is a label key, or one of the
  // reserved keys "rule_id", "severity", "cluster_id", "instance_id",
  // "datname". IsRegex switches Value from exact match to regular expression;
  // an invalid regular expression makes the whole silence inert and is
  // reported by Validate, never by panicking at match time.
  type Matcher struct {
      Name    string `json:"name"`
      Value   string `json:"value"`
      IsRegex bool   `json:"is_regex,omitempty"`
      Negate  bool   `json:"negate,omitempty"`
  }

  type Silence struct {
      ID       uuid.UUID
      Matchers []Matcher
      Reason   string
      StartsAt time.Time
      EndsAt   time.Time
  }

  func (s Silence) Validate() error

  // Active reports whether now falls inside [StartsAt, EndsAt).
  func (s Silence) Active(now time.Time) bool

  // Matches reports whether every matcher matches the alert. An empty matcher
  // list matches nothing — a silence with no matchers would silence the whole
  // fleet, which is never what the operator meant.
  func (s Silence) Matches(a Alert) bool

  // FirstMatch returns the first active, matching silence, or nil.
  func FirstMatch(silences []Silence, a Alert, now time.Time) *Silence
  ```

  Matching is conjunctive: all matchers must match. Regular expressions are
  anchored — the implementation wraps the pattern as `^(?:` + pattern + `)$`
  before compiling, so `warn` does not match `warning`.

- **Unit tests:** in `internal/alert/silence_test.go` —
  `TestSilence_Validate_RejectsEmptyMatchers`,
  `TestSilence_Validate_RejectsBadRegex`,
  `TestSilence_Validate_RejectsEndBeforeStart`,
  `TestSilence_Active_WindowIsHalfOpen`,
  `TestSilence_Matches_AllMatchersMustMatch`,
  `TestSilence_Matches_RegexIsAnchored`,
  `TestSilence_Matches_NegateInverts`,
  `TestSilence_Matches_ReservedKeys`,
  `TestFirstMatch_SkipsInactiveSilence`,
  `TestFirstMatch_ReturnsNilWhenNoneMatch`.
- **e2e tests:** none.
- **Done:** gates green + coverage of `internal/alert` still at or above 90 % +
  closed in `STATE.md`.

### 0.5 Alert key derivation and dedup identity

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/alert/key.go` (new), `internal/alert/key_test.go` (new)
- **Change:** implement the identity of an alert.

  ```go
  // Key derives the stable identity of an alert instance. It is the rule id
  // followed by the subject, followed by the canonical label string, joined by
  // "/". Reuse pgtype.CanonicalLabels (internal/pgtype/metric.go) so the label
  // ordering rule exists in exactly one place in the codebase.
  //
  //   agent_down/instance=1f8c.../
  //   replica.lag_high/instance=1f8c.../slot=s1
  //
  // The subject is chosen by the rule scope: cluster id for ScopeCluster,
  // instance id for ScopeInstance, instance id plus datname for ScopeDatabase.
  func Key(r Rule, s Sample) string

  // DedupID is the notification identity: alert key plus the RFC 3339 nano
  // representation of StartedAt. Two replicas computing this for the same
  // condition must produce byte-identical strings.
  func DedupID(a Alert) string
  ```

  > **This is the sub-phase that makes invariant I-4 possible.** If `Key` is not
  > deterministic, two server replicas produce two identities and the unique
  > index in `notifications` cannot deduplicate them. Sort every map before
  > serialising; never range over a map to build a string.

- **Unit tests:** in `internal/alert/key_test.go` —
  `TestKey_StableAcrossLabelOrder` (build the same labels map twice with
  different insertion order, assert byte equality),
  `TestKey_DiffersPerInstance`,
  `TestKey_ClusterScopeUsesClusterID`,
  `TestKey_DatabaseScopeIncludesDatname`,
  `TestKey_EmptyLabelsProducesTrailingSlash`,
  `TestDedupID_IncludesStartedAt`,
  `TestDedupID_DiffersAfterRefire`.
- **e2e tests:** none.
- **Done:** gates green + closed in `STATE.md`.

### 0.6 Built-in Tier 0 rule catalogue

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — the catalogue is a product decision, not a
  mechanical one; this is a review gate
- **Files:** `internal/alert/builtin.go` (new), `internal/alert/builtin_test.go` (new)
- **Change:** declare the Tier 0 rules in code. They cannot be disabled
  (plan 002 D12) and therefore must not be stored in `alert_rules`.

  ```go
  // Builtin returns the Tier 0 rules, in a deterministic order. The slice is
  // freshly built on every call so a caller cannot mutate the catalogue.
  func Builtin() []Rule
  ```

  The catalogue, exactly these ten, all `Tier0`, all `Enabled: true`:

  | `ID` | Severity | Scope | Driver | Threshold / `For` | Summary |
  |------|----------|-------|--------|-------------------|---------|
  | `agent_down` | critical | instance | metric `up` | `lt 1`, `For 90s` | No payload received from the agent |
  | `instance_unreachable` | critical | instance | event `instance_unreachable` | `For 0` | The agent is alive but cannot reach the instance |
  | `check_failing` | warning | instance | metric `pglens_check_error_rate` | `gt 0`, `For 5m` | A check has been failing for over five minutes |
  | `no_primary_in_cluster` | critical | cluster | event `no_primary_in_cluster` | `For 0` | No instance in the cluster reports the primary role |
  | `agent_buffer_full` | warning | instance | metric `pglens_samples_dropped_rate` | `gt 0`, `For 60s` | The agent is dropping samples |
  | `clock_skew` | warning | instance | metric `pglens_agent_clock_skew_seconds` | `gt 30`, `For 60s` | Agent clock skew above 30 seconds |
  | `cardinality_budget_exceeded` | warning | instance | metric `pglens_cardinality_truncated_rate` | `gt 0`, `For 5m` | Series are being truncated by the cardinality budget |
  | `failover_detected` | critical | cluster | event `failover_detected` | `For 0` | A failover was observed |
  | `split_brain_detected` | critical | cluster | event `split_brain_detected` | `For 0` | More than one primary in the cluster |
  | `slot_inactive` | warning | instance | event `slot_inactive` | `For 0` | A replication slot has been inactive and is retaining WAL |

  Event-driven rules resolve when the inverse event arrives or when the
  condition disappears from the topology; the engine handles that in phase 1.
  Here the catalogue only has to be correct and validated.

- **Unit tests:** in `internal/alert/builtin_test.go` —
  `TestBuiltin_AllValidate` (every rule passes `Validate`),
  `TestBuiltin_IDsAreUnique`,
  `TestBuiltin_AllAreTier0`,
  `TestBuiltin_OrderIsDeterministic` (two calls produce the same order),
  `TestBuiltin_ReturnsFreshSlice` (mutating the returned slice does not affect
  the next call),
  `TestBuiltin_CoversDocumentedCatalogue` (asserts the exact set of ten ids, so
  removing a rule by accident fails the build).
- **e2e tests:** none.
- **Done:** gates green + closed in `STATE.md`.

### 0.7 Update README.md

Mandatory closing sub-phase of every phase. User guide only — no implementation
detail.

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — documentation
- **Files:** `README.md` (repo root)
- **Change:** No user-visible change in this phase — the tables exist but
  nothing evaluates or notifies yet. Verify the README is still accurate,
  specifically that the "Limitations" section does not yet claim any alerting
  capability, and leave the file otherwise unchanged. Record that verification
  in `STATE.md` §11 docs row for phase 0.
- **Unit tests:** none (documentation).
- **e2e tests:** none — nothing to demonstrate yet.
- **Done:** the README makes no claim this phase has not shipped; gates green;
  closed in `STATE.md` with the §11 docs row for phase 0 set.

---

## Phase gates

- **Fmt:** `make fmt-check`
- **Lint:** `make lint`
- **Build:** `make build`
- **Test subset:** `make test` and `go test -cover ./internal/alert/...`
- **Coverage:** `make coverage-gate`
- **Regression guard:** `INT-STORE-003`, `INT-INGEST-002`, `INT-STALE-003` must
  still pass — the migration must not disturb existing storage behavior.
- **README:** verified accurate, unchanged.

## Phase done criterion

`internal/alert` exists with types, evaluation, silence matching, key derivation
and the Tier 0 catalogue, at or above 90 % statement coverage, and
`internal/store/migrations/0006_alerts.sql` applies cleanly to a fresh database
and to one that already has plan 001's schema. Nothing evaluates yet, and no
existing behavior changed. README.md reflects this phase's shipped behavior
(none), and `STATE.md` §11 shows phase 0 `DONE` with every sub-phase closed.
