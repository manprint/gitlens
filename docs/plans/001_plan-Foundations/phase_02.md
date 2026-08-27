# Phase 1 — Data model and pure logic

> **Intent:** Build the correctness core — value types, injectable clock,
> persisted identity, counter-to-rate conversion with reset detection, and
> cardinality control — with no IO of any kind, so every branch is testable
> deterministically and cheaply.
> **Shippable alone?** yes — pure additive library code, no runtime behavior.
> **Preconditions:** phase 0 DONE.

Everything in this phase is **L1**: no network, no filesystem except the one
narrowly-scoped identity file, no database, no `time.Now()`. This is where the
project's numerical honesty is decided; a bug here makes every later graph lie.

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

---

## Sub-phases

### 1.1 Core value types

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — type design.
- **Files:** `internal/pgtype/id.go`, `internal/pgtype/role.go`, `internal/pgtype/metric.go`, `internal/pgtype/version.go`, and the matching `_test.go` files
- **Change:** define the vocabulary every other package speaks. No behavior
  beyond parsing, formatting and canonicalization.

  `id.go`:
  ```go
  // ClusterID is a PostgreSQL system_identifier, or a hash of a user-supplied
  // cluster name when the identifier cannot be read. It is stable across
  // failover, promote, rename and IP change; that stability is invariant I-1.
  type ClusterID uint64

  type IDSource string

  const (
  	IDSourceSystemIdentifier IDSource = "system_identifier"
  	IDSourceManual           IDSource = "manual"
  )

  // String renders the identifier in decimal. It is transported as a JSON
  // string, never a JSON number: uint64 exceeds the exact integer range of
  // IEEE-754 doubles and would be silently rounded by any JSON consumer
  // (decision D18).
  func (c ClusterID) String() string

  func ParseClusterID(s string) (ClusterID, error)

  // ManualClusterID derives a stable ClusterID from a user-supplied name using
  // FNV-1a 64. Two agents given the same cluster name always agree.
  func ManualClusterID(name string) ClusterID

  type InstanceID = uuid.UUID
  type AgentID = uuid.UUID
  ```

  `role.go`:
  ```go
  type Role string

  const (
  	RolePrimary Role = "primary"
  	RoleStandby Role = "standby"
  	RoleUnknown Role = "unknown"
  )

  func RoleFromRecovery(inRecovery bool) Role // true -> standby, false -> primary

  type PermTier int

  const (
  	TierReadOnly  PermTier = iota // T0: pg_monitor + pg_control_system()
  	TierExplain                   // T1: + pg_read_all_data
  	TierSignal                    // T2: + pg_signal_backend
  	TierExtension                 // T3: + CREATE EXTENSION
  )

  func (t PermTier) String() string // "T0".."T3"
  func ParsePermTier(s string) (PermTier, error)
  ```

  `metric.go`:
  ```go
  type MetricKind string

  const (
  	KindGauge   MetricKind = "gauge"
  	KindCounter MetricKind = "counter" // only counters go through internal/delta
  )

  type Metric struct {
  	Name   string
  	Labels map[string]string
  	Value  float64
  	Kind   MetricKind
  }

  // SeriesKey identifies one time series. It must be comparable so it can be a
  // map key, which is why Labels is carried as a canonical string rather than a
  // map.
  type SeriesKey struct {
  	Metric   string
  	Instance InstanceID
  	Database string // "" means instance scope, never the literal "postgres"
  	Labels   string // produced by CanonicalLabels
  }

  // CanonicalLabels serializes labels deterministically: keys sorted
  // lexicographically, each rendered as key=value, pairs joined by "\x1f"
  // (unit separator). Empty map yields "". The separator is chosen because it
  // cannot appear in a PostgreSQL identifier or in any label value this project
  // produces.
  func CanonicalLabels(l map[string]string) string
  ```

  `version.go`:
  ```go
  // PGVersion is server_version_num, e.g. 160004 for PostgreSQL 16.4.
  type PGVersion int

  func (v PGVersion) Major() int      // 160004 -> 16
  func (v PGVersion) Minor() int      // 160004 -> 4
  func (v PGVersion) String() string  // "16.4"

  // Supported reports whether this project supports the version at all
  // (decision D11: PostgreSQL 15 through 18 inclusive).
  func (v PGVersion) Supported() bool

  const (
  	PG15 PGVersion = 150000
  	PG16 PGVersion = 160000
  	PG17 PGVersion = 170000
  	PG18 PGVersion = 180000
  	PG19 PGVersion = 190000 // exclusive upper bound
  )
  ```
- **Unit tests:**
  `TestClusterID_StringRoundTrip` — every value in `{0, 1, math.MaxUint64, 7381927364512345678}` survives `ParseClusterID(c.String())` unchanged, and `math.MaxUint64.String()` is `"18446744073709551615"` (proves no float64 rounding).
  `TestManualClusterID_Stable` — the same name yields the same value across calls; two different names yield different values.
  `TestCanonicalLabels` — `{"b":"2","a":"1"}` and `{"a":"1","b":"2"}` both yield `"a=1\x1fb=2"`; empty map yields `""`; a single pair yields `"a=1"` with no separator.
  `TestSeriesKey_Comparable` — two `SeriesKey` built from label maps in different insertion order are `==` and collide in a `map[SeriesKey]int`.
  `TestPGVersion` — `PGVersion(160004).Major()==16`, `.Minor()==4`, `.String()=="16.4"`; `Supported()` is true for 150000, 160004, 180000 and false for 140018, 190000, 0.
  `TestRoleFromRecovery` — `true` maps to `RoleStandby`, `false` to `RolePrimary`.
  `TestPermTier_StringRoundTrip` — every tier survives `ParsePermTier(t.String())`; an unknown string returns an error.
  `FuzzCanonicalLabels` — for any map built from the fuzzed input, the output is byte-identical across two calls and contains exactly `len(map)-1` separators when the map is non-empty.
- **e2e tests:** none (no behavior change).
- **Done:** `make test` green with `-race -shuffle=on`; `go vet ./internal/pgtype/...` clean; no package outside `internal/pgtype` is imported by these files except `github.com/google/uuid`; closed in `STATE.md`.

### 1.2 Injectable clock

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — concurrency-sensitive utility.
- **Files:** `internal/clock/clock.go`, `internal/clock/fake.go`, `internal/clock/clock_test.go`
- **Change:** half of this project's logic is "compute a rate between two
  instants". That is not testable deterministically if time is read from a
  global, so time is a parameter everywhere (decision recorded in
  `TESTING.md` §3.2).
  ```go
  type Clock interface {
  	Now() time.Time
  	Since(t time.Time) time.Duration
  	NewTicker(d time.Duration) Ticker
  	// Sleep returns ctx.Err() if the context is cancelled first, nil otherwise.
  	Sleep(ctx context.Context, d time.Duration) error
  }

  type Ticker interface {
  	C() <-chan time.Time
  	Stop()
  }

  // System returns the real clock.
  func System() Clock

  // Fake is a manually advanced clock. Every method is safe for concurrent use.
  type Fake struct{ /* mu sync.Mutex, now time.Time, tickers, sleepers */ }

  func NewFake(t time.Time) *Fake
  func (f *Fake) Now() time.Time
  // Advance moves the clock forward, firing every ticker whose period elapsed
  // and releasing every sleeper whose deadline passed, in chronological order.
  // It returns after all of them have been delivered.
  func (f *Fake) Advance(d time.Duration)
  // BlockUntilSleepers blocks until n goroutines are waiting in Sleep. Tests use
  // it to remove the race between starting a goroutine and advancing the clock.
  func (f *Fake) BlockUntilSleepers(n int)
  ```
  Ticker channels are buffered with capacity 1 and use a non-blocking send, so a
  slow consumer never deadlocks `Advance` — the same semantics as
  `time.Ticker`.
- **Unit tests:**
  `TestFake_Now` — `Now()` is the constructor value until `Advance` is called, then exactly value+d.
  `TestFake_TickerFires` — a 1s ticker fires exactly 3 times after `Advance(3*time.Second)`, and 0 times after `Advance(999*time.Millisecond)`.
  `TestFake_TickerStop` — after `Stop()`, `Advance` delivers nothing and does not panic.
  `TestFake_SleepReleases` — a goroutine in `Sleep(ctx, 5s)` returns nil after `Advance(5s)`; uses `BlockUntilSleepers(1)` first, never a real sleep.
  `TestFake_SleepContextCancel` — cancelling the context releases `Sleep` with `ctx.Err()` even though the clock never advanced.
  `TestFake_SlowConsumerDoesNotBlock` — a ticker whose channel is never drained still allows 100 successive `Advance` calls to return.
  `TestFake_Concurrent` — 50 goroutines calling `Now()` while another calls `Advance()` runs clean under `-race`.
- **e2e tests:** none (no behavior change).
- **Done:** `make test` green under `-race`; no test in this package calls `time.Sleep`; closed in `STATE.md`.

### 1.3 Persisted identity store

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — durability-sensitive, security-sensitive (handles DSNs).
- **Files:** `internal/identity/identity.go`, `internal/identity/fingerprint.go`, and the matching `_test.go` files
- **Change:** implements decision D8 / `IDEA.md` §2.2. `instance_id` must survive
  agent restarts, or every restart of a containerized agent duplicates every
  instance.
  ```go
  // File is the on-disk shape. Written atomically, mode 0600.
  type File struct {
  	AgentID   uuid.UUID            `json:"agent_id"`
  	Instances map[string]uuid.UUID `json:"instances"` // fingerprint -> instance id
  	Version   int                  `json:"version"`   // currently 1
  }

  type Store struct{ /* path string, mu sync.Mutex, f File */ }

  // Open loads the store, creating it with a fresh AgentID when absent. The
  // parent directory is created with mode 0700 if missing.
  func Open(path string) (*Store, error)

  func (s *Store) AgentID() uuid.UUID

  // InstanceID returns the stable id for a fingerprint, allocating and
  // persisting a new one on first sight. created reports whether it was
  // allocated by this call.
  func (s *Store) InstanceID(fingerprint string) (id uuid.UUID, created bool, err error)
  ```
  `fingerprint.go`:
  ```go
  // Fingerprint derives a stable, non-reversible key for a DSN. The password is
  // removed before hashing and the result never contains any part of the DSN,
  // so a fingerprint is safe to log; a DSN never is.
  //
  // Normalization, in order: parse the DSN; drop the password; lowercase host;
  // apply the default port 5432 when absent; sort query parameters by key and
  // drop those that do not affect target identity (application_name,
  // connect_timeout, statement_timeout, sslmode); re-render; SHA-256; hex.
  func Fingerprint(dsn string) (string, error)
  ```
  Persistence rules, all mandatory:
  - write to `<path>.tmp` in the same directory, `fsync`, then `os.Rename` onto
    `<path>` — a crash mid-write must never leave a truncated identity file
  - `0600` on the file, `0700` on the directory it creates
  - **never log the DSN, and never include it in an error**; wrap errors with the
    fingerprint instead
- **Unit tests:**
  `TestStore_CreatesOnFirstOpen` — opening a path under `t.TempDir()` creates the file; the mode is exactly `0600`; the parent directory mode is `0700`.
  `TestStore_AgentIDStableAcrossReopen` — `Open`, read `AgentID`, close, `Open` again: identical UUID.
  `TestStore_InstanceIDStableAcrossReopen` — the same fingerprint yields the same UUID after reopening; `created` is true on the first call and false afterwards.
  `TestStore_DistinctFingerprintsDistinctIDs` — two fingerprints never collide.
  `TestStore_AtomicWrite` — after a successful `InstanceID` call no `*.tmp` file remains in the directory.
  `TestStore_CorruptFileIsAnError` — a file containing `{` returns an error from `Open` and **does not** silently regenerate the identity: silently regenerating would duplicate every instance without telling anyone.
  `TestFingerprint_PasswordIgnored` — `postgres://u:secret1@h/db` and `postgres://u:secret2@h/db` produce the same fingerprint.
  `TestFingerprint_NeverLeaksDSN` — the returned string is 64 lowercase hex characters and contains none of the substrings `secret`, `u@`, `h`, `db`.
  `TestFingerprint_Normalization` — `postgres://U@Host/db`, `postgres://U@host:5432/db` and `postgres://U@host/db?application_name=x` all produce the same fingerprint; changing the host, port, user or database changes it.
  `TestStore_Concurrent` — 20 goroutines requesting the same and different fingerprints run clean under `-race` and produce a consistent file.
  `FuzzFingerprint` — never panics; either returns an error or a 64-character hex string.
- **e2e tests:** `SYS-AGENT-001` / `SYS-AGENT-002` in phase 5 prove the real consequence (restart with a volume keeps the id, without a volume duplicates the instance). Named here so the link is not lost.
- **Done:** `make test` green under `-race`; `grep -rn "dsn" internal/identity/*.go` shows no logging or error-formatting call that embeds it; closed in `STATE.md`.

### 1.4 Delta engine and counter-reset detection

- **Model:** `agent-1:opus`
- **Assignment:** `agent-1:opus` — correctness core. **This is an `agent-1` review gate by construction:** every metric rate in the product flows through this file, and `IDEA.md` §4.5 identifies missing reset detection as a guaranteed-bug class. Coverage gate 90%.
- **Files:** `internal/delta/engine.go`, `internal/delta/engine_test.go`
- **Change:** convert cumulative counters into rates, and refuse to emit a point
  whenever the conversion would be meaningless. Deltas are computed server-side
  (decision D17); the agent ships raw counters plus `stats_reset`.
  ```go
  type Observation struct {
  	Key        pgtype.SeriesKey
  	TS         time.Time
  	Value      float64
  	StatsReset *time.Time // from pg_stat_database.stats_reset et al; nil when the source has none
  }

  type Point struct {
  	Key  pgtype.SeriesKey
  	TS   time.Time
  	Rate float64 // units per second
  }

  type ResetReason string

  const (
  	ResetStatsChanged         ResetReason = "stats_reset_changed"
  	ResetCounterWentBackwards ResetReason = "counter_went_backwards"
  )

  type ResetEvent struct {
  	Key    pgtype.SeriesKey
  	TS     time.Time
  	Reason ResetReason
  }

  type Options struct {
  	MaxGap time.Duration // default 10m; see rule 5
  }

  type Engine struct{ /* mu sync.Mutex, prev map[pgtype.SeriesKey]state */ }

  func New(opts Options) *Engine

  // Observe records one counter observation and returns the derived rate point,
  // the detected reset, or neither. It never returns both.
  func (e *Engine) Observe(o Observation) (*Point, *ResetEvent)

  // Evict drops series whose last observation is older than the cutoff and
  // returns how many were removed. Bounds memory when instances disappear.
  func (e *Engine) Evict(before time.Time) int
  ```
  **The decision table. Implement it exactly; the order of the rules matters.**

  | # | Condition | Point | Event | State update |
  |---|-----------|-------|-------|--------------|
  | 1 | no previous observation for the key | nil | **nil** | store baseline |
  | 2 | `o.TS <= prev.TS` | nil | nil | **none** — a duplicate or out-of-order sample must not corrupt the baseline |
  | 3 | `o.StatsReset != nil && prev.StatsReset != nil && !o.StatsReset.Equal(*prev.StatsReset)` | nil | `ResetStatsChanged` | store new baseline |
  | 4 | `o.Value < prev.Value` | nil | `ResetCounterWentBackwards` | store new baseline |
  | 5 | `o.TS.Sub(prev.TS) > MaxGap` | nil | nil | store new baseline |
  | 6 | otherwise | `(o.Value-prev.Value) / dt.Seconds()` | nil | store new baseline |

  Rule 1 emits **no event**: a first observation is normal operation, not an
  anomaly, and writing it to the events table would flood it on every agent
  start.
  Rule 5 exists because a rate averaged across a ten-minute outage is not
  wrong-looking, it is *plausible* and wrong — the worst kind. Emitting nothing
  makes the gap visible, which the UI is required to render as a gap
  (`IDEA.md` §6, "principio di onestà UI").
  The engine handles **counters only**. Gauges are written straight through by
  the caller and must never reach `Observe`; document that on the type.
- **Unit tests:** one table-driven test with a subtest per rule, plus:
  `TestEngine_Rule1_FirstObservationNoPointNoEvent`
  `TestEngine_Rule2_DuplicateTimestampLeavesBaselineIntact` — observe (t0,100), (t0,999), then (t0+60s,160): the resulting rate is 1.0, proving the 999 never became the baseline.
  `TestEngine_Rule2_OutOfOrderIgnored` — an observation with `TS` before the baseline changes nothing.
  `TestEngine_Rule3_StatsResetChanged` — value drops from 100 to 5 with a new `stats_reset`: nil point, `ResetStatsChanged`; the next observation resumes from 5.
  `TestEngine_Rule3_StatsResetUnchangedIsNotAReset` — identical `stats_reset` and rising value gives a normal rate.
  `TestEngine_Rule3_NilStatsResetNeverTriggers` — sources without `stats_reset` fall through to rule 4.
  `TestEngine_Rule4_CounterWentBackwards` — value 100 then 4 with unchanged `stats_reset`: nil point, `ResetCounterWentBackwards`.
  `TestEngine_Rule5_GapExceedsMaxGap` — 11 minutes apart with `MaxGap` 10m: nil point, nil event, baseline moves.
  `TestEngine_Rule5_GapAtBoundary` — exactly `MaxGap` still produces a point (the comparison is `>`, not `>=`).
  `TestEngine_Rule6_FlatCounterYieldsZeroNotNil` — an unchanged counter produces a point with `Rate == 0`; this is the difference between "nothing happened" and "no data", and conflating them is the failure the project exists to avoid.
  `TestEngine_Rule6_LargeCounterPrecision` — values around `1<<53` still produce an exact rate (asserts no float32 narrowing anywhere).
  `TestEngine_NeverEmitsBothPointAndEvent` — for a matrix of generated inputs, `point != nil && event != nil` never holds.
  `TestEngine_NeverEmitsNegativeRate` — for a matrix including every reset shape, no returned `Point.Rate` is below zero (invariant I-2).
  `TestEngine_SeriesAreIndependent` — a reset on one `SeriesKey` does not disturb another.
  `TestEngine_Evict` — series older than the cutoff are removed and the count is returned; an evicted series behaves as rule 1 on its next observation.
  `TestEngine_Concurrent` — 100 goroutines observing interleaved keys run clean under `-race`.
  `FuzzEngine_NoNegativeRateNoPanic` — a fuzzed sequence of observations never panics and never produces a negative rate.
- **e2e tests:** `SYS-RESET-001` (PostgreSQL restart), `SYS-RESET-002` (external `pg_stat_statements_reset()`), `SYS-RESET-003` (eviction from the statements hash table) in phase 5.
- **Done:** `make test` green under `-race -shuffle=on`; `go tool cover -func` reports **>= 90%** for `internal/delta`; every row of the decision table has a named subtest; `agent-1:opus` has reviewed the decision table against `IDEA.md` §4.5; closed in `STATE.md`.

### 1.5 Cardinality control: top-N, hysteresis, budget

- **Model:** `agent-1:opus`
- **Assignment:** `agent-1:opus` — this is the guard that stops the TSDB from exploding in production. Coverage gate 90%.
- **Files:** `internal/cardinality/selector.go`, `internal/cardinality/budget.go`, and the matching `_test.go` files
- **Change:** implements `IDEA.md` §4.6. Without it a single instance with 5000
  distinct `queryid` values produces roughly 12 million series.
  ```go
  type Candidate struct {
  	Key       string  // opaque, e.g. the decimal queryid
  	Primary   float64 // ranking metric 1, e.g. total_exec_time
  	Secondary float64 // ranking metric 2, e.g. calls
  }

  type Options struct {
  	TopN       int // per ranking metric; default 50
  	Hysteresis int // cycles a key is retained after dropping out; default 5
  	MaxKeys    int // hard cap after the union; default 200
  }

  type Selector struct{ /* mu, opts, lastSeen map[string]uint64 */ }

  func NewSelector(opts Options) *Selector

  // Select picks the keys to report for this cycle. cycle is a monotonically
  // increasing counter supplied by the caller. truncated reports whether MaxKeys
  // forced anything out — the caller must propagate it to Result.Truncated so
  // the UI can say "N not shown" instead of implying full coverage.
  func (s *Selector) Select(cycle uint64, in []Candidate) (kept []Candidate, truncated bool)

  // Forget drops retention state for keys not seen within the window, bounding
  // memory when a workload changes shape.
  func (s *Selector) Forget(cycle uint64) int
  ```
  **Algorithm, in this order:**
  1. `fresh` = (top `TopN` by `Primary` descending) ∪ (top `TopN` by `Secondary` descending). Ties broken by `Key` ascending, so the result is deterministic and testable.
  2. `retained` = keys in `in` that are not in `fresh` but whose `lastSeen` cycle is `>= cycle - Hysteresis`.
  3. `kept` = `fresh` ∪ `retained`, ordered by `Primary` descending then `Key` ascending.
  4. If `len(kept) > MaxKeys`, truncate to `MaxKeys` and set `truncated = true`.
  5. Update `lastSeen[k] = cycle` for every key in `fresh` **only** — not for retained keys, otherwise a key retained by hysteresis would keep renewing itself and never leave.

  Step 5 is the rule that makes the algorithm converge. Getting it wrong
  produces a set that only ever grows, which is precisely the failure mode the
  selector exists to prevent.

  `budget.go`:
  ```go
  // Budget caps the number of series admitted per instance per push.
  type Budget struct{ /* max int */ }

  func NewBudget(max int) *Budget // default max 20000 at the call site

  // Admit reports how many of n series fit and whether anything was refused.
  func (b *Budget) Admit(used, n int) (admitted int, truncated bool)
  ```
- **Unit tests:**
  `TestSelector_UnionOfBothRankings` — a key that is 1st by `calls` but 500th by `total_exec_time` is kept; this is the "many fast queries" case that a single ranking misses.
  `TestSelector_DeterministicTieBreak` — candidates with identical `Primary` are ordered by `Key`, and two runs over shuffled input produce byte-identical output.
  `TestSelector_HysteresisRetains` — a key present in cycle 1 and absent from the top-N in cycles 2 to 6 is still kept in cycles 2 to 6 and dropped in cycle 7 (`Hysteresis` 5).
  `TestSelector_HysteresisDoesNotSelfRenew` — a key kept only by hysteresis for 5 cycles is not re-armed; it leaves on schedule. Asserts the convergence property of step 5.
  `TestSelector_ReentryResetsHysteresis` — a key that re-enters `fresh` gets a fresh window.
  `TestSelector_MaxKeysTruncates` — 500 candidates with `MaxKeys` 200 yields exactly 200 kept, `truncated == true`, and the 200 highest by `Primary`.
  `TestSelector_NoTruncationFlagWhenUnderCap` — `truncated` is false when `len(kept) <= MaxKeys`.
  `TestSelector_EmptyInput` — no panic, empty result, `truncated == false`.
  `TestSelector_5000CandidatesStaysBounded` — 200 cycles of 5000 randomly-ranked candidates never yields more than `MaxKeys`, and `Forget` keeps `lastSeen` bounded. Uses a seeded `rand.New(rand.NewPCG(1,2))`, and the seed is logged.
  `TestBudget_Admit` — under, exactly at, and over the cap; `Admit(0,0)` returns `(0,false)`.
  `FuzzSelector_NeverExceedsMaxKeys` — for any fuzzed candidate slice, `len(kept) <= MaxKeys` always holds.
- **e2e tests:** `SYS-LOAD-008` in phase 5 — 5000 structurally distinct queries against a real instance; the budget holds and `truncated` reaches the API.
- **Done:** `make test` green under `-race -shuffle=on`; `go tool cover -func` reports **>= 90%** for `internal/cardinality`; the convergence test `TestSelector_HysteresisDoesNotSelfRenew` passes; closed in `STATE.md`.

### 1.6 Coverage gate

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — mechanical tooling.
- **Files:** `scripts/coverage_gate.sh`, `Makefile` (replace the placeholder `coverage-gate` recipe added in 0.3), `.github/workflows/pr.yml` (add the step)
- **Change:** turn the coverage thresholds from `overview.md` into an enforced
  gate.
  ```bash
  #!/usr/bin/env bash
  # scripts/coverage_gate.sh — fail when a package falls below its floor.
  set -euo pipefail

  PROFILE="${1:-coverage.out}"
  MOD="github.com/manprint/pglens"
  GLOBAL_MIN=75

  # Packages whose correctness the whole product depends on.
  declare -A FLOORS=(
    ["$MOD/internal/delta"]=90
    ["$MOD/internal/cardinality"]=90
    ["$MOD/internal/topology"]=90
    ["$MOD/internal/ash"]=90
    ["$MOD/internal/identity"]=85
  )
  ```
  Behaviour, all mandatory:
  - a package listed in `FLOORS` that **does not exist yet or has no statements
    is skipped, not failed** — `internal/topology` and `internal/ash` arrive in
    phases 6 and 7, and a gate that fails on absent code would block phase 1
  - the global floor is computed over `./internal/...` only; `cmd/`, `doc.go`
    files and generated code are excluded, because a gate that counts
    boilerplate measures boilerplate
  - the output lists every package with its percentage and floor, and the failing
    ones last, so a red run is actionable without re-running anything
  - exit 1 on any breach, 0 otherwise
  Makefile:
  ```makefile
  coverage-gate:
  	$(GO) test -race -coverprofile=coverage.out -covermode=atomic ./internal/...
  	./scripts/coverage_gate.sh coverage.out
  ```
  Add `- run: make coverage-gate` to the `unit-go` job in `.github/workflows/pr.yml`,
  after `make test`.
- **Unit tests:** none (shell tooling). Verified by execution: run it once against the real profile and once against a hand-edited profile where `internal/delta` is below 90, and confirm exit codes 0 and 1 respectively.
- **e2e tests:** none.
- **Done:** `make coverage-gate` exits 0 on the current tree and prints a line per package; artificially lowering `internal/delta` coverage makes it exit 1 naming that package; the CI job runs it; closed in `STATE.md`.

### 1.7 Update README.md

Mandatory closing sub-phase of every phase. User guide only — no implementation
detail.

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — documentation; `agent-1:opus` reads it on the final phase.
- **Files:** `README.md`
- **Change:** **No user-visible change in this phase** — it adds library code
  only, and the binaries still exit with an error. Update only the **Running the
  checks** section to add `make coverage-gate` with a one-line description, and
  verify the rest of the README is still accurate. Do not describe types,
  packages, algorithms or the reset-detection rules: none of that is user-facing.
  Record in `STATE.md` that the remainder was verified and deliberately left
  unchanged.
- **Unit tests:** none (documentation).
- **e2e tests:** none — the commands shown were executed and produced the documented output.
- **Done:** `make coverage-gate` appears in the README and works exactly as documented from a clean clone; no package or type name appears anywhere in the file; closed in `STATE.md` with the §11 docs row for phase 1 set.

---

## Phase gates

- **Fmt:** `make fmt-check`
- **Lint:** `make lint`
- **Test subset:** `make test` (`-race -shuffle=on`)
- **Coverage:** `make coverage-gate` — `internal/delta` and `internal/cardinality` at or above 90%, `internal/identity` at or above 85%, `./internal/...` at or above 75%
- **Regression guard:** phase 0 gates still green; `make build` still produces both binaries
- **README:** updated for this phase (checks section only) or explicitly verified as still accurate, free of implementation detail

## Phase done criterion

`make fmt-check && make lint && make test && make coverage-gate` all pass. The
delta engine implements every row of its decision table with a named test per
row, never returns a negative rate and never returns both a point and an event.
The cardinality selector provably converges — a key retained by hysteresis
leaves on schedule. Identity survives a reopen and never leaks a DSN. No file in
this phase performs network or database IO. README.md reflects this phase's
shipped behavior, and `STATE.md` §11 shows phase 1 `DONE` with every sub-phase
closed.
