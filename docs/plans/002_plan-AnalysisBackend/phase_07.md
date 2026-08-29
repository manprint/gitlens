# Phase 6 — Host and container metrics

> **Intent:** collect the minimum host facts the configuration advisor needs —
> total and available memory, CPU, load, free space on the data filesystem — and
> get them right inside a container, where `/proc` lies and cgroup limits are the
> real ceiling.
> **Shippable alone?** yes — one new collector, one config block, one endpoint.
> **Preconditions:** phase 2 `DONE`. Phase 5 is not required, but the advisor
> rules of phase 7 need both.

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

## The association problem, decided here

Host metrics belong to a **machine**, but everything else in the data model
belongs to an **instance**. The agent cannot know in general whether it shares a
machine with the PostgreSQL it monitors — in remote mode it does not, and
attaching the agent's own host metrics to a remote instance would be a lie that
the advisor would then act on.

The rule, and it is the only rule:

> Host metrics are attached to a target instance **only** when the target is
> declared local. A target is local when `targets[].host_local` is `true`, or
> when that field is unset and the DSN host is `localhost`, `127.0.0.1`, `::1`
> or a Unix socket path. Everything else gets no host metrics, and the API says
> so explicitly rather than returning zeros.

This is a config-visible decision, so it is documented in the README in
sub-phase 6.6.

---

## Sub-phases

### 6.1 The host collector

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/host/doc.go` (new), `internal/host/collector.go` (new),
  `internal/host/collector_test.go` (new), `go.mod` (modified)
- **Change:** create package `host` collecting the minimum set (plan 002 D10).
  Promote `github.com/shirou/gopsutil/v4` from an indirect to a direct
  requirement in `go.mod` — it is already present at v4.26.7 and pulled in
  transitively, so no new module enters the build graph.

  ```go
  // Sample is one host observation. Every field is a pointer or carries an
  // explicit Available flag, because "not measurable here" must stay
  // distinguishable from zero.
  type Sample struct {
      MemTotalBytes     *uint64
      MemAvailableBytes *uint64
      SwapTotalBytes    *uint64
      SwapUsedBytes     *uint64
      CPUCount          *int
      CPUUsedRatio      *float64  // 0..1, averaged over the interval
      Load1, Load5, Load15 *float64
      DiskTotalBytes    *uint64   // filesystem holding the data directory
      DiskFreeBytes     *uint64
      Source            string    // "host" | "cgroup_v1" | "cgroup_v2"
  }

  type Collector struct { /* proc/sys paths, previous CPU sample */ }

  func NewCollector(procPath, sysPath string) *Collector

  // Collect returns one sample. dataDir is the PostgreSQL data directory as
  // reported by the instance, used to choose the filesystem for the disk
  // figures; an empty dataDir leaves the disk fields nil.
  func (c *Collector) Collect(ctx context.Context, dataDir string) (Sample, error)
  ```

  `procPath` and `sysPath` come from `HOST_PROC` and `HOST_SYS`, which the agent
  already reads (`IDEA.md` §3.2 and the existing agent environment handling).
  gopsutil honors those through its own `common.HostProcWithContext`
  mechanism; set them via `gopsutil`'s documented environment or path override
  rather than re-implementing `/proc` parsing.

  `CPUUsedRatio` needs two samples to be meaningful. Keep the previous cumulative
  CPU time on the collector and return `nil` on the very first call — never
  return 0, which would read as an idle machine at agent startup, exactly when
  someone is watching.

- **Unit tests:** in `internal/host/collector_test.go`, against a fixture
  `/proc` tree under `testdata/` —
  `TestCollect_ReadsMemTotalFromProc`,
  `TestCollect_FirstCallHasNilCPURatio`,
  `TestCollect_SecondCallComputesCPURatio`,
  `TestCollect_EmptyDataDirLeavesDiskNil`,
  `TestCollect_LoadAverages`,
  `TestCollect_SourceIsHostWhenNoCgroup`.
- **e2e tests:** none yet.
- **Done:** gates green + `go mod tidy` leaves `gopsutil` as a direct
  requirement + closed in `STATE.md`.

### 6.2 cgroup v1 and v2 detection

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation; `agent:gpt5.6-luna` reviews,
  because getting the limit precedence wrong makes every memory ratio in the
  advisor wrong
- **Files:** `internal/host/cgroup.go` (new),
  `internal/host/cgroup_test.go` (new),
  `internal/host/testdata/cgroup_v1/` and `internal/host/testdata/cgroup_v2/`
  (new fixture trees)
- **Change:** detect the cgroup version and read the memory and CPU limits that
  actually apply to this container, overriding the host figures.

  Detection, in this order:
  1. If `<sysPath>/fs/cgroup/cgroup.controllers` exists, this is **cgroup v2**.
  2. Else if `<sysPath>/fs/cgroup/memory/memory.limit_in_bytes` exists, this is
     **cgroup v1**.
  3. Else there is no cgroup; keep the host figures and set
     `Source = "host"`.

  cgroup v2 reads:
  - memory limit: `memory.max` — the literal string `max` means unlimited, in
    which case keep the host total.
  - memory usage: `memory.current`.
  - CPU quota: `cpu.max`, which is `"<quota> <period>"` or `"max <period>"`;
    `CPUCount` becomes `quota / period` rounded up, and stays the host count
    when unlimited.

  cgroup v1 reads:
  - memory limit: `memory/memory.limit_in_bytes`. A value at or above
    `9223372036854771712` means unlimited — the sentinel is a rounded
    `math.MaxInt64`, and comparing for equality against `MaxInt64` misses it on
    some kernels. Compare with `>=` against `1 << 62` instead, which is
    unambiguous.
  - memory usage: `memory/memory.usage_in_bytes`.
  - CPU quota: `cpu/cpu.cfs_quota_us` and `cpu/cpu.cfs_period_us`; a quota of
    `-1` means unlimited.

  `MemAvailableBytes` inside a container is `limit - current`, **not**
  `/proc/meminfo`'s `MemAvailable`, which reports the host. Getting this wrong is
  the single most common bug in container-aware monitoring and it makes
  `shared_buffers` ratios meaningless, which is why this sub-phase is a review
  gate.

  Set `Source` to `cgroup_v1` or `cgroup_v2` whenever a limit was actually
  applied, so the API and the advisor can say which world the numbers describe.

- **Unit tests:** `TestCgroup_DetectsV2ByControllersFile`,
  `TestCgroup_DetectsV1ByLimitFile`,
  `TestCgroup_V2MaxMeansUnlimited`,
  `TestCgroup_V1SentinelMeansUnlimited`,
  `TestCgroup_V2CPUQuotaRoundsUp`,
  `TestCgroup_V1NegativeQuotaMeansUnlimited`,
  `TestCgroup_AvailableIsLimitMinusCurrent`,
  `TestCgroup_SourceReflectsDetection`,
  `TestCgroup_NoCgroupKeepsHostFigures`.
- **e2e tests:** `INT-HOST-001` — run the collector inside a container started
  with a memory limit through the existing testcontainers helpers, and assert
  `MemTotalBytes` equals the limit and `Source` is a cgroup value. If the CI
  runner's cgroup version is not the one under test, skip with an explicit
  reason rather than passing vacuously.
- **Done:** gates green + closed in `STATE.md`.

### 6.3 Agent wiring

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/agent/host.go` (new),
  `internal/agent/config.go` (modified),
  `internal/agent/scheduler.go` (modified),
  `internal/agent/host_test.go` (new),
  `deploy/agent.example.yaml` (modified)
- **Change:**
  1. Config:

     ```yaml
     host:
       enabled: true          # default true
       interval: 30s          # default 30s
       proc_path: /proc       # default: $HOST_PROC, else /proc
       sys_path: /sys         # default: $HOST_SYS, else /sys

     targets:
       - name: primary
         dsn: "postgres://..."
         host_local: true     # optional; inferred from the DSN host when unset
     ```

  2. The scheduler runs the collector once per `host.interval`, **not once per
     target** — one machine, one measurement — and then attaches the resulting
     metrics to every local target's results, with the target's `instance_id`.
     A single collection fanned out is what keeps the numbers consistent between
     two instances on the same box.
  3. Metric names and kinds, all gauges except where noted:
     `host_mem_total_bytes`, `host_mem_available_bytes`,
     `host_swap_total_bytes`, `host_swap_used_bytes`, `host_cpu_count`,
     `host_cpu_used_ratio`, `host_load1`, `host_load5`, `host_load15`,
     `host_disk_total_bytes`, `host_disk_free_bytes`,
     `host_disk_free_ratio` (feeds the seeded `disk.free_low` rule), and
     `host_metrics_source` as a gauge carrying `0` for host, `1` for cgroup v1
     and `2` for cgroup v2.
  4. A nil field emits **no metric**. The API turns absence into an explicit
     "not available", never into zero.
  5. The data directory needed for the disk figures comes from the existing
     `instance_info` check, which already reads instance-level facts; if it does
     not currently collect `data_directory`, add it there — it is a `pg_settings`
     read and stays within tier T0 — and note the cross-phase edit in
     `STATE.md` §8. When it is unavailable, the disk metrics are simply not
     emitted.

  > **Do not make the host collector a `check.Check`.** It does not scrape
  > PostgreSQL, it has no `Target`, it needs no connection, and forcing it into
  > that interface would give it a database connection it must never use. It is
  > a scheduler-driven producer that contributes metrics to the same results
  > envelope, which is a smaller change than it sounds.

- **Unit tests:** `TestHostLocal_InferredFromLocalhostDSN`,
  `TestHostLocal_InferredFromUnixSocketDSN`,
  `TestHostLocal_ExplicitFalseWins`,
  `TestHost_RemoteTargetGetsNoHostMetrics`,
  `TestHost_CollectedOncePerIntervalNotPerTarget`,
  `TestHost_NilFieldEmitsNoMetric`,
  `TestHost_DisabledEmitsNothing`.
- **e2e tests:** `INT-HOST-002` — with two local targets configured, one
  collection produces identical `host_mem_total_bytes` values attached to both
  instance ids.
- **Done:** gates green + `INT-CFG-001` still passes and now shows the host
  collector state in `pglens-agent check` output + closed in `STATE.md`.

### 6.4 Server routing and host API

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — implementation
- **Files:** `internal/server/api_host.go` (new),
  `internal/server/api_host_test.go` (new),
  `internal/server/api_host_integration_test.go` (new),
  `internal/server/http.go` (modified)
- **Change:** host metrics need **no routing change** — they are ordinary
  metrics with an `instance_id` and no relation labels, so `destinationTable`
  already sends them to `metrics`. Verify that with a test rather than assuming
  it.

  Add `GET /api/v1/instances/{id}/host`:

  ```json
  {
    "instance_id": "…",
    "available": true,
    "source": "cgroup_v2",
    "sampled_at": "2026-08-29T10:00:00Z",
    "stale": false,
    "mem_total_bytes": 8589934592,
    "mem_available_bytes": 2147483648,
    "cpu_count": 4,
    "cpu_used_ratio": 0.37,
    "load1": 1.2,
    "disk_total_bytes": 107374182400,
    "disk_free_bytes": 21474836480,
    "disk_free_ratio": 0.2
  }
  ```

  When the instance is remote, return `200` with
  `{"instance_id": "…", "available": false, "reason": "target is not local to any agent"}`
  and no numeric fields at all. A field that is absent must not be rendered as
  `0` by any client, and the surest way to guarantee that is not to send it.

- **Unit tests:** `TestHostAPI_RemoteInstanceReportsUnavailable`,
  `TestHostAPI_OmitsAbsentFields`,
  `TestHostAPI_SourceIsDecoded`,
  `TestHostAPI_StalenessThreshold`,
  `TestPipeline_HostMetricsGoToGenericTable`.
- **e2e tests:** `INT-HOST-003` — ingest host metrics and read them back with
  the source decoded correctly.
- **Done:** gates green + `make test-integration` green + closed in `STATE.md`.

### 6.5 Container validation

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — testing
- **Files:** `test/compose/agent-container.yml` (modified),
  `internal/host/cgroup_integration_test.go` (new)
- **Change:** the containerised agent needs the host paths bind-mounted
  read-only to see the real machine (`IDEA.md` §3.2). Add to the agent service:

  ```yaml
  volumes:
    - /proc:/host/proc:ro
    - /sys:/host/sys:ro
  environment:
    HOST_PROC: /host/proc
    HOST_SYS: /host/sys
  ```

  Read-only bind mounts are deliberately preferred to `--privileged`; state that
  in the README in sub-phase 6.6.

  Then prove both worlds: inside the container with the mounts, the collector
  reports the **host** memory total; with a container memory limit set and the
  mounts present, it reports the **limit**. Those two assertions together are
  what says the precedence logic of 6.2 is wired correctly and not merely
  unit-tested.

- **Unit tests:** none (this sub-phase is the tests).
- **e2e tests:** `INT-HOST-004` — containerised collector with host mounts and
  no memory limit reports the host total and `source = "host"`.
  `INT-HOST-005` — the same with a memory limit reports the limit and a cgroup
  source.
- **Done:** both green, or explicitly skipped with a reason naming the runner's
  cgroup version + `SYS-HARNESS-001` still passes with the modified compose file
  + closed in `STATE.md`.

### 6.6 Update README.md

Mandatory closing sub-phase of every phase. User guide only — no implementation
detail.

- **Model:** `agent:gpt5.6-luna`
- **Assignment:** `agent:gpt5.6-luna` — documentation
- **Files:** `README.md` (repo root)
- **Change:** update these sections for what this phase actually made usable:
  - **Configuration** — the whole `host:` block with defaults, and
    `targets[].host_local` with the inference rule spelled out.
  - **Running the agent in Docker** — the two read-only bind mounts and the two
    environment variables, with the note that read-only mounts are preferred to
    running the container privileged.
  - **Usage** — a `curl` against `/api/v1/instances/{id}/host` with expected
    JSON for both the local and the remote case.
  - **Limitations** — host metrics are collected **only for targets local to the
    agent**; a remote target reports them as unavailable rather than as zero.
    Inside a container with a memory limit, the reported total is the cgroup
    limit, not the machine's memory. Per-device IOPS, disk latency and network
    metrics are not collected.
- **Unit tests:** none (documentation).
- **e2e tests:** none — the `curl` examples were executed and produced the
  documented output.
- **Done:** a user running the agent in Docker can get correct host memory from
  the README alone; the local-only limitation is stated where a reader meets it
  before trusting a ratio; gates green; closed in `STATE.md` with the §11 docs
  row for phase 6 set.

---

## Execution record

- **Assignment:** `agent:gpt5.6-luna`.
- The agent health server starts before target initialization, so the
  container healthcheck on port 9187 remains available during connection
  retries.
- The initial SYS-LOAD-008 diagnostic exposed an unconditional extra rotation
  at the 390s boundary and an orphaned `workloadctl` child after an interrupted
  runner. The correction uses an exact duration-window rotation count and a
  Linux parent-death signal; it does not alter the 390s acceptance scenario or
  the product contract.
- Focused verification passed: the rotation-boundary unit test and real
  `SYS-HARNESS-001` container startup/teardown smoke exited 0 in 27.323s. The
  complete fmt/lint/build/unit-race/coverage/L2 matrix also passed, with
  coverage 75.1% (4377/5830). The interrupted long diagnostic remains in
  STATE.md as evidence only and is not claimed as a pass.

## Phase gates

- **Fmt:** `make fmt-check`
- **Lint:** `make lint`
- **Build:** `make build`
- **Test subset:** `make test`, `make test-integration`
- **Coverage:** `make coverage-gate`
- **Regression guard:** `SYS-HARNESS-001`, `INT-CFG-001` and every existing
  agent test must still pass — the scheduler was modified.
- **README:** the Docker mounts and the local-only rule are documented.

## Phase done criterion

An agent running in a container with the host mounts reports the machine's
memory when unconstrained and the cgroup limit when constrained, attaches those
figures only to local targets, and `GET /api/v1/instances/{id}/host` reports
`available: false` with a reason for a remote instance instead of zeros.
`INT-HOST-001` to `INT-HOST-005` are green. README.md reflects this phase's
shipped behavior, and `STATE.md` §11 shows phase 6 `DONE` with every sub-phase
closed.
