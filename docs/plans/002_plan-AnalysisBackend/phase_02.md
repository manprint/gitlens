# Phase 1 — Alert engine, notifiers, alert API

> **Intent:** make the alert model live — a single-leader evaluation loop that
> reads TimescaleDB and `events`, persists alert state, notifies exactly once per
> alert identity, and exposes it all over HTTP.
> **Shippable alone?** yes — with no webhook configured the engine evaluates and
> persists but sends nothing, which is already useful through the API.
> **Preconditions:** phase 0 `DONE`.

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

- Integration tests are `//go:build integration` files named
  `<file>_integration_test.go`, next to the code, run by `make test-integration`.
  They obtain a real PostgreSQL through the existing `test/pgtest` helpers. Read
  `internal/server/staleness_integration_test.go` for the established shape
  before writing a new one.
- Everything that reads the current time takes a `clock.Clock`
  (`internal/clock/clock.go`) so tests can drive it with `clock.Fake`.
- HTTP handlers follow the shape of `internal/server/api.go`: a struct holding
  the pool and the clock, a `RegisterRoutes(chi.Router)` method, one handler per
  route, JSON encoded with `encoding/json`, errors via `http.Error` with a
  plain-text body.

---

## Sub-phases

### 1.1 Engine loop and leader election

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — concurrency and lifecycle; review gate
- **Files:** `internal/alert/engine.go` (new), `internal/alert/engine_test.go` (new)
- **Change:** implement the evaluation loop, copying the proven lifecycle of
  `Staleness` in `internal/server/staleness.go` — a dedicated pooled connection
  held for the life of the evaluator, `pg_try_advisory_lock` on it, a ticker, a
  `stopCh`/`doneCh` pair guarded by `sync.Once`.

  ```go
  type Engine struct {
      pool     *pgxpool.Pool
      clock    clock.Clock
      interval time.Duration     // default 30s, from PGLENS_ALERT_INTERVAL
      eval     *Evaluator
      store    Store             // sub-phase 1.4
      notifier Notifier          // sub-phase 1.3
      sources  []Source          // sub-phase 1.2
  }

  func NewEngine(pool *pgxpool.Pool, clk clock.Clock, st Store, n Notifier, srcs []Source) *Engine
  func (e *Engine) Start(ctx context.Context)
  func (e *Engine) Stop()
  // Tick runs exactly one evaluation cycle. Exported so tests drive it
  // directly instead of waiting on a ticker.
  func (e *Engine) Tick(ctx context.Context) error
  ```

  The advisory lock key is `hashtext('pglens:alert-engine')` — a different key
  from `pglens:staleness`, so the two evaluators can be led by different
  replicas without interfering.

  `Tick` does, in this order:
  1. If not leader, try to acquire the lock. If still not leader, return `nil`
     immediately — a follower does no work and logs nothing at info level.
  2. Load the rule set: `Builtin()` plus enabled rows from `alert_rules`.
     A stored rule whose id collides with a built-in id is **ignored with a
     warning**; Tier 0 always wins.
  3. Load active silences.
  4. For every rule, ask its `Source` for the current samples.
  5. Feed each sample through `Evaluator.Step`.
  6. Persist every returned alert through `Store.Upsert`.
  7. For `TransitionFired` and `TransitionResolved`, and only when no silence
     matches, hand the alert to the notifier.
  8. Call `Evaluator.Forget(now - 1h)` to release state for vanished subjects.

  > **Behavior change to mark loudly:** a follower replica must not notify. If
  > the lock is lost mid-run — the connection dropped — the tick aborts before
  > the notification step rather than finishing with stale leadership.

- **Unit tests:** in `internal/alert/engine_test.go`, with a nil pool, a fake
  clock, a stub `Source`, an in-memory `Store` and a recording `Notifier` —
  `TestTick_FollowerDoesNothing`,
  `TestTick_NotifiesOnFire`,
  `TestTick_NotifiesOnResolve`,
  `TestTick_SilencedAlertIsPersistedButNotNotified`,
  `TestTick_BuiltinWinsOverStoredRuleWithSameID`,
  `TestTick_ForgetsStaleEvaluatorState`,
  `TestStop_IsIdempotent`.
- **e2e tests:** none yet (phase 9 covers it).
- **Done:** gates green (`make fmt-check`, `make lint`, `make build`,
  `make test`) + `go test -race ./internal/alert/...` green + closed in
  `STATE.md`.

### 1.2 SQL sources

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/alert/source.go` (new), `internal/alert/source_sql.go` (new),
  `internal/alert/source_sql_integration_test.go` (new)
- **Change:** define how a rule gets its samples, and implement the two real
  sources.

  ```go
  // Source produces the samples a rule is evaluated against.
  type Source interface {
      // Kind reports which rules this source serves: "metric" or "event".
      Kind() string
      // Samples returns the current samples for one rule.
      Samples(ctx context.Context, r Rule, now time.Time) ([]Sample, error)
  }
  ```

  **`metricSource`** answers a metric rule with the latest value per subject
  inside a lookback window of `max(2*interval, 2m)`. It must read from the right
  table: `metrics` for a generic metric name, and the typed tables when the
  metric belongs to one. Route with a single switch that mirrors
  `destinationTable` in `internal/server/pipeline.go:66` so the two never drift;
  when in doubt, query `metrics`.

  For `ScopeCluster` rules the source aggregates before returning: it produces
  one sample per cluster, whose `Value` is the aggregate named by the rule id
  suffix — count of matching instances for
  `replica.all_standbys_lagging`, and the raw gauge for
  `replica.no_sync_standby`. Implement the aggregation as a `switch r.ID` with a
  `default` that returns an error naming the unsupported cluster rule, so
  adding a cluster rule without an aggregation fails loudly instead of silently
  producing nothing.

  **`eventSource`** answers an event rule by reading rows from `events`
  (`internal/store/migrations/0001_meta.sql:73`) whose `type` equals
  `r.EventType` and whose `ts` is inside the lookback window, producing one
  sample per `(cluster_id, instance_id)` with `Value = 1`. An event rule resolves
  when no matching event appears in the window — that is what makes
  `failover_detected` clear itself after the window passes rather than sticking
  forever.

- **Unit tests:** in `internal/alert/source_test.go` —
  `TestMetricSource_RoutesToTypedTable` (asserts the SQL text targets
  `metrics_replication` for `pg_replication_lag_seconds`),
  `TestMetricSource_ClusterRuleWithoutAggregationErrors`,
  `TestEventSource_LookbackWindow`.
- **e2e tests:** `INT-ALERT-001` — against a real database seeded with two
  instances, one lagging: `metricSource.Samples` for `replica.lag_high` returns
  exactly one sample, for the lagging instance, with the expected value.
  `INT-ALERT-002` — after inserting a `failover_detected` row into `events`,
  `eventSource.Samples` returns one sample; after advancing the fake clock past
  the window, it returns none.
- **Done:** gates green + `make test-integration` green + closed in `STATE.md`.

### 1.3 Notifiers

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation; `agent:gpt5.6-luna` reviews the
  delivery semantics, because invariant I-4 lives here
- **Files:** `internal/alert/notify/notify.go` (new),
  `internal/alert/notify/slack.go` (new),
  `internal/alert/notify/webhook.go` (new),
  `internal/alert/notify/notify_test.go` (new)
- **Change:** implement the delivery layer.

  ```go
  package notify

  // Message is what a channel renders. The alert package converts an Alert
  // into this so notify does not import alert and create a cycle.
  type Message struct {
      DedupID  string
      RuleID   string
      Severity string
      Phase    string // "fire" or "resolve"
      Summary  string
      Cluster  string
      Instance string
      Database string
      Value    float64
      At       time.Time
      Labels   map[string]string
  }

  type Channel interface {
      Name() string                                   // "slack" | "webhook"
      Send(ctx context.Context, m Message) error
  }

  func NewSlack(webhookURL string, hc *http.Client) Channel
  func NewWebhook(url string, hc *http.Client) Channel
  ```

  **Slack** (R6): `POST` the webhook URL with
  `{"text": "<severity upper> <summary>", "blocks": [...]}`. Build a two-block
  layout: a `section` with the summary and a `context` with cluster, instance,
  database, value and timestamp. A non-`2xx` response is an error carrying the
  status code and the first 200 bytes of the body.

  **Webhook**: `POST` the URL with the `Message` marshalled as JSON, content
  type `application/json`.

  Both share a retry policy: three attempts, backoff `1s`, `4s`, `9s`
  (quadratic, deterministic — no jitter, so tests are reproducible), a per
  attempt timeout of 10 s and an overall context deadline of 45 s. Retry only
  on a transport error or a `5xx`; a `4xx` is permanent and returns immediately.

  > **Never log the webhook URL.** It is a credential. Log the channel name and
  > the status code only. Redact any `hooks.slack.com` path segment if it ever
  > reaches an error string.

- **Unit tests:** in `internal/alert/notify/notify_test.go`, against
  `httptest.Server` —
  `TestSlack_PostsExpectedBody`,
  `TestSlack_NonJSONErrorIsTruncated`,
  `TestWebhook_PostsJSON`,
  `TestRetry_ThreeAttemptsOn5xx`,
  `TestRetry_NoRetryOn4xx`,
  `TestRetry_BackoffIsDeterministic`,
  `TestSend_RespectsContextCancellation`,
  `TestError_DoesNotContainWebhookURL`.
- **e2e tests:** covered by `SYS-ALERT-001` in phase 9.
- **Done:** gates green + closed in `STATE.md`.

### 1.4 Alert persistence and once-only delivery

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/alert/store.go` (new),
  `internal/alert/store_integration_test.go` (new)
- **Change:** implement persistence against the tables of sub-phase 0.1.

  ```go
  type Store interface {
      Rules(ctx context.Context) ([]Rule, error)
      Silences(ctx context.Context, now time.Time) ([]Silence, error)
      Upsert(ctx context.Context, a Alert) error
      // ClaimNotification inserts the notifications row and reports whether
      // this caller won. A duplicate key means another replica already sent it.
      ClaimNotification(ctx context.Context, dedupID, channel, phase string, a Alert) (won bool, err error)
      MarkNotification(ctx context.Context, dedupID, channel, phase string, ok bool, attempts int, sendErr error) error
      Active(ctx context.Context, f Filter) ([]Alert, error)
  }

  func NewPgStore(pool *pgxpool.Pool) Store
  ```

  `Upsert` writes to `alerts` with
  `ON CONFLICT (tenant_id, alert_key, started_at) DO UPDATE` setting `state`,
  `value`, `last_eval_at` and `resolved_at`. It never changes `started_at` — a
  refire produces a new row because `started_at` differs, which is exactly the
  identity model of sub-phase 0.5.

  `ClaimNotification` is the mechanism behind invariant I-4:

  ```sql
  INSERT INTO notifications (tenant_id, alert_key, started_at, channel, phase, ok, attempts)
  VALUES ($1, $2, $3, $4, $5, false, 0)
  ON CONFLICT (tenant_id, alert_key, started_at, channel, phase) DO NOTHING
  RETURNING notification_id
  ```

  No returned row means another replica already claimed it, so this caller must
  not send. The claim is written **before** the HTTP call; `MarkNotification`
  updates `ok`, `attempts` and `error` afterwards. A claim that is never marked
  successful is visible in the table as `ok = false`, which is the honest record
  of a delivery that failed.

- **Unit tests:** none beyond the integration tests; this type is
  database-shaped and a mock would assert only that the code calls itself.
- **e2e tests:** `INT-ALERT-003` — `Upsert` twice with the same key and
  `started_at` updates in place and leaves one row.
  `INT-ALERT-004` — `Upsert` after a refire (new `started_at`) leaves two rows.
  `INT-ALERT-005` — two goroutines call `ClaimNotification` with the same
  arguments concurrently; exactly one gets `won = true`.
  `INT-ALERT-006` — `Rules` returns the ten seeded Tier 1 rows and drops a row
  whose `enabled` is false.
  `INT-ALERT-007` — `Silences` returns only silences active at the given time.
- **Done:** gates green + `make test-integration` green + closed in `STATE.md`.

### 1.5 Alert and silence HTTP API

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/server/api_alerts.go` (new),
  `internal/server/api_alerts_test.go` (new),
  `internal/server/api_alerts_integration_test.go` (new),
  `internal/server/http.go:11` (modified — register the new routes)
- **Change:** add the endpoints listed in `overview.md`. Follow
  `internal/server/api.go:46` for the registration shape and add
  `alertsAPI.RegisterRoutes(r)` to `NewRouter`, extending its parameter list.

  | Route | Behavior |
  |-------|----------|
  | `GET /api/v1/alerts` | filters `state`, `severity`, `cluster_id`, `instance_id`, `rule_id`; default returns `pending` and `firing`, newest `last_eval_at` first, capped at 500 rows |
  | `GET /api/v1/alerts/{alert_key}` | the newest occurrence plus its `notifications` rows; `404` when unknown |
  | `GET /api/v1/alert-rules` | built-in Tier 0 rules and stored Tier 1 rules in one list, each carrying `"tier": 0` or `1` |
  | `PUT /api/v1/alert-rules/{rule_id}` | updates `enabled`, `severity`, `threshold`, `for_seconds` on a Tier 1 rule; **`409 Conflict`** with a body naming the rule when the id is Tier 0; `404` when unknown; `400` when the body fails `Rule.Validate` |
  | `GET /api/v1/silences` | active silences by default, `?all=true` for the history |
  | `POST /api/v1/silences` | body `{matchers, reason, starts_at, ends_at}`; `400` when `Silence.Validate` fails; returns the created silence with its `silence_id` |
  | `DELETE /api/v1/silences/{id}` | sets `ends_at = now()`, so the audit trail survives; `204` on success, `404` when unknown |

  `cluster_id` is rendered as a decimal **string** in every response, matching
  plan 001 D18. Timestamps are RFC 3339 with nanoseconds.

- **Unit tests:** in `internal/server/api_alerts_test.go`, using the existing
  `mockpool_test.go` helpers —
  `TestAlerts_DefaultFilterIsPendingAndFiring`,
  `TestAlerts_RejectsUnknownState`,
  `TestAlertRules_Tier0IsRejectedWithConflict`,
  `TestAlertRules_InvalidBodyIsBadRequest`,
  `TestSilences_CreateValidates`,
  `TestSilences_DeleteIsSoft`,
  `TestAlerts_ClusterIDIsAString`.
- **e2e tests:** `INT-ALERTAPI-001` — against a real database, create a silence
  through `POST`, list it, delete it, and assert `ends_at` moved rather than the
  row disappearing.
  `INT-ALERTAPI-002` — `PUT` on `agent_down` returns `409` and leaves the
  catalogue unchanged.
- **Done:** gates green + `make test-integration` green + every existing API
  test still passes unchanged (plan 002 D25) + closed in `STATE.md`.

### 1.6 Server wiring and configuration

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `cmd/pglens-server/main.go` (modified),
  `internal/server/config.go` (new if absent, else modified)
- **Change:** construct and start the engine at server startup, after migrations
  and after the existing `Staleness` evaluator. Read from the environment:

  | Variable | Default | Meaning |
  |----------|---------|---------|
  | `PGLENS_ALERT_INTERVAL` | `30s` | engine tick |
  | `PGLENS_SLACK_WEBHOOK_URL` | unset | enables the Slack channel |
  | `PGLENS_SLACK_WEBHOOK_URL_FILE` | unset | reads the URL from a file, taking precedence over the inline variable |
  | `PGLENS_WEBHOOK_URL` | unset | enables the generic channel |

  With no channel configured the engine still runs and still persists; it logs
  once at startup, at info level, that no notification channel is configured.
  That single line is what stops an operator believing alerts are being
  delivered when they are not.

  Stop the engine on shutdown before closing the pool, mirroring how
  `Staleness` is stopped today.

- **Unit tests:** `TestConfig_AlertIntervalDefault`,
  `TestConfig_WebhookFileTakesPrecedence`,
  `TestConfig_InvalidIntervalIsRejected` in `internal/server/config_test.go`.
- **e2e tests:** covered by phase 9.
- **Done:** gates green + `make build` produces a server that starts with no new
  environment variables set + closed in `STATE.md`.

### 1.7 Engine integration tests

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — testing
- **Files:** `internal/alert/engine_integration_test.go` (new)
- **Change:** prove the loop against a real database and a real `httptest`
  webhook receiver.
- **Unit tests:** none (this sub-phase is the tests).
- **e2e tests:**
  `INT-ALERT-008` — a metric below threshold produces no alert; raising it above
  threshold and advancing the fake clock past `For` moves the row in `alerts`
  to `firing` and delivers exactly one webhook call.
  `INT-ALERT-009` — **two engines** sharing one database and one receiver, both
  ticked: exactly one webhook call arrives, and `notifications` holds one row.
  This is the unit test of invariant I-4.
  `INT-ALERT-010` — a matching silence leaves the alert `firing` in the API with
  `suppressed: true` and delivers nothing.
  `INT-ALERT-011` — the condition clearing delivers exactly one `resolve`
  notification and sets `resolved_at`.
  `INT-ALERT-012` — killing the leader's connection lets the second engine
  acquire the lock on its next tick and continue without re-notifying the
  already-notified alert.
- **Done:** all six tests green under `make test-integration`, each run twice
  with `-count=1` to prove they are not order-dependent + closed in `STATE.md`.

### 1.8 Update README.md

Mandatory closing sub-phase of every phase. User guide only — no implementation
detail.

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — documentation
- **Files:** `README.md` (repo root)
- **Change:** update these sections for what this phase actually made usable:
  - **Configuration** — the four new server environment variables with their
    defaults and the note that the file variant wins.
  - **Usage** — the new endpoints with a realistic `curl` and its expected JSON
    shape: listing alerts, creating a silence, deleting a silence, listing
    rules.
  - **Alerting** — a new section: what Tier 0 rules are and that they cannot be
    disabled, the ten of them in a table with their conditions, that Tier 1
    rules are editable, and how silencing works.
  - **Limitations** — alerting delivers to Slack and generic webhooks only;
    there is no email and no PagerDuty; silences suppress notification, not
    evaluation.
  Include realistic examples and their expected output. Exclude package names,
  file paths and the advisory-lock mechanism. Preserve the existing README
  structure, tone and language; edit, do not rewrite.
- **Unit tests:** none (documentation).
- **e2e tests:** none — the `curl` examples were executed against a running
  server and produced the documented output.
- **Done:** a new user can configure Slack alerting and create a silence from the
  README alone, with no source reading; no implementation detail present; gates
  green; closed in `STATE.md` with the §11 docs row for phase 1 set.

---

## Phase gates

- **Fmt:** `make fmt-check`
- **Lint:** `make lint`
- **Build:** `make build`
- **Test subset:** `make test`, `go test -race ./internal/alert/...`,
  `make test-integration`
- **Coverage:** `make coverage-gate`
- **Regression guard:** `INT-STALE-003` and every existing `INT-API-*` test must
  still pass — `Staleness` is untouched (D13) and no existing endpoint changed
  (D25).
- **README:** updated with the alerting section and the new configuration.

## Phase done criterion

A server started with `PGLENS_SLACK_WEBHOOK_URL` set evaluates every 30 s as a
single leader, persists alert state, delivers exactly one notification per
`(alert_key, started_at, channel, phase)` even with two replicas running, honors
silences, and exposes alerts, rules and silences over `/api/v1`. `INT-ALERT-001`
through `INT-ALERT-012` and `INT-ALERTAPI-001`/`002` are green. README.md
reflects this phase's shipped behavior, and `STATE.md` §11 shows phase 1 `DONE`
with every sub-phase closed.
