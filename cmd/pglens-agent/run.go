package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/internal/agent/buffer"
	"github.com/manprint/pglens/internal/check"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/host"
	"github.com/manprint/pglens/internal/wire"
)

// clockSkewWarnThreshold is the |skew| beyond which the agent logs a
// one-time warning (internal/server/ingest.go's maxSampleAge, 12h, is what
// actually governs whether a sample is ever rejected for being stale).
const clockSkewWarnThreshold = 60 * time.Second

// runAgentCommand is the agent's default mode: load config, monitor every
// configured target, push envelopes to the server on push_interval, serve
// /healthz. This is what the container's ENTRYPOINT with no arguments runs.
func runAgentCommand(args []string) {
	fs := flag.NewFlagSet("pglens-agent run", flag.ExitOnError)
	configPath := fs.String("config", envOr("PGLENS_CONFIG", "/etc/pglens/agent.yaml"), "path to the agent config file")
	_ = fs.Parse(args) // flag.ExitOnError: Parse never returns on failure

	cfg, err := agent.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: load config %s: %v\n", *configPath, err)
		os.Exit(1)
	}

	// internal/agent/conn.go's ensureCache actually reads the identity file
	// path from PGLENS_IDENTITY_PATH, not from cfg.IdentityPath — the config
	// field is validated as required (agent.LoadConfig) and documented, but
	// was never actually wired to anything: every deployment has silently
	// run on the env var's own hardcoded default
	// ("/var/lib/pglens/identity.json") this whole time, which happens to
	// equal every existing container config's identity_path by coincidence
	// (the container mounts its identity volume there), masking the gap
	// completely. Found live implementing AgentModeBinary — the first
	// deployment mode where identity_path legitimately points somewhere
	// else (a host temp directory), which made ensureCache fail to open the
	// identity store on every single attempt, forever, with no rate limit
	// distinguishable from a real connectivity problem. Setting the env var
	// here (only if not already set) makes the config file the real source
	// of truth for every mode, while preserving an explicit override for
	// anyone already relying on the env var directly — the same
	// override-wins precedent PGLENS_BOOTSTRAP_TOKEN already established.
	if os.Getenv("PGLENS_IDENTITY_PATH") == "" {
		_ = os.Setenv("PGLENS_IDENTITY_PATH", cfg.IdentityPath)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	clk := clock.System()
	// PGLENS_DEBUG_CLOCK_OFFSET simulates a misconfigured system clock (e.g.
	// NTP drift) without touching the container's real time — a signed
	// Go duration string, e.g. "-5m" for a clock 5 minutes behind. Not a
	// real deployment knob (no production reason to lie about the agent's
	// own clock); exists so SYS-AGENT-004 can exercise clock-skew detection
	// against a real, injectable clock instead of the host's actual time.
	if raw := os.Getenv("PGLENS_DEBUG_CLOCK_OFFSET"); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil {
			clk = clock.WithOffset(clk, d)
		} else {
			fmt.Fprintf(os.Stderr, "warning: invalid PGLENS_DEBUG_CLOCK_OFFSET %q: %v\n", raw, err)
		}
	}
	pusher := agent.NewPusher(cfg.Server.URL, cfg.GetToken(), clk)

	buf, err := openAgentBuffer(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open buffer: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = buf.Close() }()
	pusher.SetBuffer(buf)

	// Start liveness before target initialization. A target connection can
	// legitimately take longer than Docker's healthcheck grace period; the
	// agent is still alive and must expose its health while it retries.
	healthzAddr := envOr("PGLENS_HEALTHZ_LISTEN", ":9187")
	healthSrv := &http.Server{Addr: healthzAddr, Handler: pusher.HealthzHandler()}
	go func() {
		if err := healthSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "healthz server: %v\n", err)
		}
	}()

	// Accumulate scrape results per target between push cycles, build a
	// wire.Envelope per cycle, and hand it to the pusher. Push failures stay
	// queued in the pusher's disk-backed buffer (SetBuffer above) for the
	// at-least-once/crash-durability contract; the collection loop itself
	// never blocks on the network either way, since Pusher.Push runs on its
	// own ticker, independent of the scheduler's Results() drain below.
	var mu sync.Mutex
	pending := map[string][]wire.Result{}   // target name -> results since last flush
	lastEdgeState := map[string]edgeState{} // target name -> last actual replication_receiver reading, across cycles with none
	ashEnabled := map[string]bool{}         // target name -> whether the dedicated ASH sampler actually started for it

	scheduler := agent.NewScheduler(agent.DefaultScheduleOptions())
	configureChecks(cfg)
	managers := make([]*agent.Manager, 0, len(cfg.Targets))
	var ashStops []func()
	for _, tc := range cfg.Targets {
		mgr, err := agent.NewManager(ctx, tc.Name, tc.DSN, connOptionsFromConfig(tc), clk)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: connect target %s: %v\n", tc.Name, err)
			os.Exit(1)
		}
		managers = append(managers, mgr)
		waitForTarget(ctx, mgr)
		addScheduleEntries(scheduler, mgr, ctx, cfg)
		ashStop, enabled := startASH(ctx, mgr, tc.Name, cfg.Checks["ash"], clk, &mu, pending)
		ashStops = append(ashStops, ashStop)
		ashEnabled[tc.Name] = enabled
	}
	if cfg.HostEnabled() {
		collector := host.NewCollector(cfg.HostProcPath(), cfg.HostSysPath())
		go func() {
			ticker := clk.NewTicker(cfg.GetHostInterval())
			defer ticker.Stop()
			collect := func() {
				sample, err := collector.Collect(ctx, "")
				if err != nil {
					fmt.Fprintf(os.Stderr, "warning: host collection: %v\n", err)
					return
				}
				for _, tc := range cfg.Targets {
					if !agent.TargetIsLocal(tc) {
						continue
					}
					mu.Lock()
					pending[tc.Name] = append(pending[tc.Name], hostWireResult(sample, clk.Now()))
					mu.Unlock()
				}
			}
			collect()
			for {
				select {
				case <-ticker.C():
					collect()
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	defer func() {
		for _, stop := range ashStops {
			stop()
		}
	}()

	if err := scheduler.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "error: start scheduler: %v\n", err)
		os.Exit(1)
	}

	// Commands are long-polled once per agent, independently from the check
	// scheduler. Executors are registered by the command-executor phases; the
	// dispatcher itself is wired here so commands.enabled=false can disable all
	// polling without affecting scheduled collection.
	if len(managers) > 0 {
		dispatcher := agent.NewDispatcher(cfg.Server.URL, cfg.GetToken(), managers[0].AgentID(), cfg.CommandsEnabled())
		dispatcher.AddExecutor(agent.NewExplainExecutor())
		dispatcher.AddExecutor(agent.NewCancelExecutor())
		dispatcher.AddExecutor(agent.NewTerminateExecutor())
		dispatcher.AddExecutor(agent.NewPgstattupleExecutor())
		for i, mgr := range managers {
			dispatcher.AddTarget(mgr.InstanceID(), mgr, cfg.Targets[i].AllowExplainAnalyze, cfg.Targets[i].AllowSignal)
		}
		go dispatcher.Run(ctx)
	}

	go func() {
		for res := range scheduler.Results() {
			m := make([]wire.Metric, 0, len(res.Metrics))
			for _, pm := range res.Metrics {
				m = append(m, wire.Metric{Name: pm.Name, Labels: pm.Labels, Value: pm.Value, Kind: string(pm.Kind)})
			}
			wr := wire.Result{Check: res.Entry.CheckName, TS: clk.Now(), Database: res.Entry.Database, Metrics: m, Truncated: res.Truncated}
			for _, f := range res.Facts {
				wf := wire.Fact{Kind: f.Kind, Key: f.Key, Labels: f.Labels, ValueText: f.ValueText, ValueJSON: f.ValueJSON}
				if err := wf.Validate(); err != nil {
					fmt.Fprintf(os.Stderr, "warning: drop invalid fact check=%s key=%s: %v\n", res.Entry.CheckName, f.Key, err)
					continue
				}
				wr.Facts = append(wr.Facts, wf)
			}
			if res.Err != nil {
				wr.Error = res.Err.Error()
			}
			// check.Result.QueryTexts (int64 -> text) was computed by
			// stat_statements.go all along but never actually reached the
			// wire — agent.EntryResult never carried it and this loop never
			// converted it, found live re-verifying phase 7.5's README:
			// /api/v1/ash/top's query_text join always returned "" because
			// query_texts was never being written by anything, since
			// pipeline.go's own wire.Result.QueryTexts consumer (already
			// correct) had nothing to consume.
			if len(res.QueryTexts) > 0 {
				wr.QueryTexts = make(map[string]string, len(res.QueryTexts))
				for qid, text := range res.QueryTexts {
					wr.QueryTexts[strconv.FormatInt(qid, 10)] = text
				}
			}
			// Same class of gap, found immediately after: check.Result.StatsReset
			// (stat_statements.go's own pg_stat_statements_info.reset_time
			// read) never reached the wire either — SYS-RESET-002 needs the
			// server's delta engine to see an explicit reset hint, not just
			// infer one from a counter decrease.
			wr.StatsReset = res.StatsReset
			mu.Lock()
			pending[res.Entry.TargetName] = append(pending[res.Entry.TargetName], wr)
			mu.Unlock()
		}
	}()

	pushInterval := cfg.GetParsedPushInterval()
	pushTicker := clk.NewTicker(pushInterval)
	defer pushTicker.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-pushTicker.C():
				flushEnvelope(ctx, managers, &mu, pending, lastEdgeState, ashEnabled, pusher) //nolint:contextcheck // flushEnvelope->HasExtension: Manager caches this at connect time, the accessor takes no context
			}
		}
	}()
	go func() {
		pushLoopTicker := clk.NewTicker(1 * time.Second)
		defer pushLoopTicker.Stop()
		skewWarned := false
		revokedStopped := false
		for {
			select {
			case <-ctx.Done():
				return
			case <-pushLoopTicker.C():
				_ = pusher.Push(ctx)
				// clockSkewWarnThreshold is well below maxSampleAge (12h,
				// internal/server/ingest.go) — samples still get accepted at
				// this level, this is purely an operator-facing signal that
				// something (usually NTP) is wrong.
				if skew := pusher.ClockSkew(); !skewWarned && (skew > clockSkewWarnThreshold || skew < -clockSkewWarnThreshold) {
					fmt.Fprintf(os.Stderr, "warning: clock skew detected: agent clock differs from server by %v\n", skew)
					skewWarned = true
				}
				// A revoked agent stops collecting: the server will reject
				// every future push anyway (SYS-AGENT-005), so continuing to
				// scrape targets only wastes connections. Stops the
				// scheduler only, not the per-target ASH samplers (their
				// stop() releases a pooled connection and is only safe to
				// call once — the deferred shutdown-time call already owns
				// that) — scheduler.Stop() alone covers the checks that
				// actually drive the bulk of what gets pushed.
				if !revokedStopped && pusher.HealthState() == agent.HealthRevoked {
					fmt.Fprintf(os.Stderr, "warning: agent revoked by server, stopping collection\n")
					scheduler.Stop()
					revokedStopped = true
				}
			}
		}
	}()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
	<-ch

	cancel()
	scheduler.Stop()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	_ = healthSrv.Shutdown(shutdownCtx)
	for _, mgr := range managers {
		mgr.Close()
	}
}

func hostWireResult(s host.Sample, now time.Time) wire.Result {
	r := wire.Result{Check: "host", TS: now}
	add := func(name string, v *uint64) {
		if v != nil {
			r.Metrics = append(r.Metrics, wire.Metric{Name: name, Value: float64(*v), Kind: "gauge"})
		}
	}
	addFloat := func(name string, v *float64) {
		if v != nil {
			r.Metrics = append(r.Metrics, wire.Metric{Name: name, Value: *v, Kind: "gauge"})
		}
	}
	addInt := func(name string, v *int) {
		if v != nil {
			r.Metrics = append(r.Metrics, wire.Metric{Name: name, Value: float64(*v), Kind: "gauge"})
		}
	}
	add("host_mem_total_bytes", s.MemTotalBytes)
	add("host_mem_available_bytes", s.MemAvailableBytes)
	add("host_swap_total_bytes", s.SwapTotalBytes)
	add("host_swap_used_bytes", s.SwapUsedBytes)
	addInt("host_cpu_count", s.CPUCount)
	addFloat("host_cpu_used_ratio", s.CPUUsedRatio)
	addFloat("host_load1", s.Load1)
	addFloat("host_load5", s.Load5)
	addFloat("host_load15", s.Load15)
	add("host_disk_total_bytes", s.DiskTotalBytes)
	add("host_disk_free_bytes", s.DiskFreeBytes)
	if s.DiskTotalBytes != nil && *s.DiskTotalBytes > 0 && s.DiskFreeBytes != nil {
		ratio := float64(*s.DiskFreeBytes) / float64(*s.DiskTotalBytes)
		addFloat("host_disk_free_ratio", &ratio)
	}
	source := map[string]float64{"host": 0, "cgroup_v1": 1, "cgroup_v2": 2}[s.Source]
	r.Metrics = append(r.Metrics, wire.Metric{Name: "host_metrics_source", Value: source, Kind: "gauge"})
	return r
}

func flushEnvelope(ctx context.Context, managers []*agent.Manager, mu *sync.Mutex, pending map[string][]wire.Result, lastEdgeState map[string]edgeState, ashEnabled map[string]bool, pusher *agent.Pusher) {
	mu.Lock()
	batch := make(map[string][]wire.Result, len(pending))
	for k, v := range pending {
		batch[k] = v
		delete(pending, k)
	}
	mu.Unlock()

	if len(batch) == 0 {
		return
	}

	env := &wire.Envelope{ProtocolVersion: wire.ProtocolVersion, SentAt: time.Now(), AgentVersion: version}
	// Every Manager shares the same identity.json (PGLENS_IDENTITY_PATH is
	// one path for the whole process), so any of them reports the same
	// process-wide agent_id — this was never actually sent before
	// (inventory.go silently fell back to using instance_id as a stand-in,
	// which broke per-agent identification: a multi-target agent's targets
	// each looked like a separate "agent" to the server, and SYS-AGENT-005's
	// per-agent revocation had nothing real to key off).
	if len(managers) > 0 {
		if id := managers[0].AgentID(); id != uuid.Nil { //nolint:contextcheck // Manager caches this at connect time; the accessor takes no context
			env.AgentID = id.String()
		}
	}
	for _, mgr := range managers {
		results, ok := batch[mgr.Database()]
		if !ok {
			continue
		}
		exts := map[string]bool{}
		for _, name := range []string{"pg_stat_statements"} {
			exts[name] = mgr.HasExtension(name) //nolint:contextcheck // Manager caches this at connect time; the accessor takes no context
		}
		// Discover was never being called again after startup, so
		// wire.Instance.Databases (and therefore the server's `databases`
		// table / db_budget skip_reason reporting) stayed permanently
		// empty on every real push — nothing had ever exercised this path
		// end to end before SYS-DB-001 (test/scenario/db.go).
		dbs, err := mgr.Discover(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: discover databases for %s: %v\n", mgr.Database(), err)
		}
		// Role() alone would report a permanently stale, cached-at-connect
		// value — see RefreshRole's own doc comment for why this matters
		// (SYS-REPL-001). A RefreshRole failure means this target could not
		// be reached AT ALL this cycle — found live via SYS-REPL-003
		// (kill one target of a two-target agent, never promote the other):
		// every scheduled check for the dead target still produced an
		// error-only wire.Result every cycle, so `results`/`ok` above stayed
		// non-empty forever and this wire.Instance kept being pushed with a
		// fresh `last_seen` indefinitely — the server had no way to tell
		// "still alive" from "every check on it has failed for minutes,"
		// which made internal/server/staleness.go's no_primary_in_cluster
		// (and agent_down, for the same reason) structurally unreachable for
		// a single dead target inside an otherwise-healthy multi-target
		// agent process. Skipping the whole instance when there is no live
		// signal at all lets the server's own staleness clock start.
		if err := mgr.RefreshRole(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "warning: refresh role for %s: %v (target unreachable this cycle, omitting from envelope)\n", mgr.Database(), err)
			continue
		}
		env.Instances = append(env.Instances, wire.Instance{
			InstanceID:      mgr.InstanceID().String(), //nolint:contextcheck // Manager caches this at connect time; the accessor takes no context
			ClusterID:       mgr.ClusterID().String(),  //nolint:contextcheck // same as above
			ClusterIDSource: "system_identifier",
			Addr:            mgr.Addr(),
			Port:            mgr.Port(),
			PGVersion:       int(mgr.PGVersion()),    //nolint:contextcheck // same as above
			Role:            string(mgr.Role()),      //nolint:contextcheck // same as above
			PermTier:        mgr.PermTier().String(), //nolint:contextcheck // same as above
			Databases:       dbs,
			Results:         results,
			TopologyEdges:   buildTopologyEdges(mgr, results, lastEdgeState), //nolint:contextcheck // buildTopologyEdges->InstanceID: Manager caches this at connect time; the accessor takes no context
			ASHEnabled:      boolPtr(ashEnabled[mgr.Database()]),
		})
	}
	pusher.Queue(env)
}

// edgeState is the last actually-observed replication_receiver reading for
// one target, persisted across push cycles in lastEdgeState. host is sticky
// (only overwritten by a non-empty reading) precisely because
// replication_receiver.go's own query reports an empty sender_host once
// genuinely disconnected (COALESCE(w.sender_host, ”) — pg_stat_wal_receiver
// has no row at all once the walreceiver process exits), so the host has to
// be remembered separately from the current connected-ness.
type edgeState struct {
	host      string
	connected bool
}

// buildTopologyEdges derives this cycle's replication edge for a standby
// target from the replication_receiver check's own result (sender_host,
// status), which is the only place that data already exists — no extra
// query. A currently-streaming/catching-up receiver is a high-confidence
// edge to its sender host; the server resolves that host to an instance_id
// (internal/server/pipeline.go). Once disconnected, the edge becomes
// low-confidence but keeps pointing at the last known host — required for
// internal/topology.Engine's orphan-standby detection to start its clock at
// all (an edge object with a name is required — see the engine's own
// TestEngine_OrphanStandby). A standby that has never once connected has no
// known host and gets no edge at all, which is also correct: it was never
// attached to anything an orphan could be measured against.
//
// replication_receiver's own interval (10s) is longer than push_interval
// (5s) in every fixture this plan tests, so roughly half of all push cycles
// carry no fresh reading at all. Starting from the persisted state and only
// touching the fields a fresh reading actually reports (rather than
// replacing the whole struct) means an absent reading this cycle reuses
// both the last host and the last connected-ness — found live twice in a
// row verifying phase 6.5's README topology example: first as a
// continuously-healthy standby flapping to "low confidence" on empty
// cycles, then (this fix) as SYS-REPL-003 regressing because a corrected
// version of this function replaced the whole struct wholesale on any
// reading, including a genuine disconnect whose empty sender_host wiped out
// the very host the low-confidence edge needs to point at.
func buildTopologyEdges(mgr *agent.Manager, results []wire.Result, lastEdgeState map[string]edgeState) []wire.Edge {
	name := mgr.Database() //nolint:contextcheck // Manager caches this at connect time; the accessor takes no context
	from := mgr.InstanceID().String()

	state := lastEdgeState[name] // zero value {"", false} if never seen before
	for _, r := range results {
		if r.Check != "replication_receiver" {
			continue
		}
		for _, m := range r.Metrics {
			if m.Name != "replication_receiver_status" {
				continue
			}
			state.connected = m.Value == 1 || m.Value == 2 // streaming or catchup
			if host := m.Labels["sender_host"]; host != "" {
				state.host = host
			}
		}
	}
	lastEdgeState[name] = state

	if state.host == "" {
		return nil
	}
	confidence := "low"
	if state.connected {
		confidence = "high"
	}
	return []wire.Edge{{From: from, To: state.host, Type: "streaming", Confidence: confidence}}
}

// waitForTarget blocks (up to 60s) until mgr's identity cache initializes
// successfully, retrying every second. addScheduleEntries below reads
// mgr.Role()/PGVersion()/PermTier()/HasExtension() exactly once, at startup,
// to decide which checks ever get scheduled for this target's entire
// lifetime — those accessors DO retry internally on a later call (their
// underlying ensureCache doesn't latch a failure), but addScheduleEntries
// itself never re-evaluates, so a target that isn't reachable yet at the
// single moment addScheduleEntries runs would otherwise end up with zero
// scheduled checks forever. This matters concretely: compose can't declare
// a static `depends_on: pg` in agent-container.yml (it's shared by both the
// standalone and primary-standby topologies, which name their PostgreSQL
// services differently), so the agent routinely starts before its target
// is ready to accept connections.
func waitForTarget(ctx context.Context, mgr *agent.Manager) {
	deadline := time.Now().Add(60 * time.Second)
	for {
		if mgr.PGVersion() != 0 { //nolint:contextcheck // Manager caches this at connect time; the accessor takes no context
			return
		}
		if time.Now().After(deadline) {
			fmt.Fprintf(os.Stderr, "warning: target %s not reachable after 60s, scheduling checks anyway\n", mgr.Database())
			return
		}
		time.Sleep(1 * time.Second)
	}
}

func configureChecks(cfg *agent.Config) {
	for _, c := range check.All() {
		policy, ok := cfg.Checks[c.Name()]
		if !ok {
			continue
		}
		if policy.TopN > 0 {
			if configurable, ok := c.(check.TopNConfigurable); ok {
				configurable.SetTopN(policy.TopN)
			}
		}
		if c.Name() == "activity" {
			if configurable, ok := c.(interface{ SetByApplication(bool) }); ok {
				configurable.SetByApplication(policy.ByApplication)
			}
		}
	}
}

func addScheduleEntries(scheduler *agent.Scheduler, mgr *agent.Manager, ctx context.Context, cfg *agent.Config) {
	role := mgr.Role()                                                                    //nolint:contextcheck // Manager caches this at connect time; the accessor takes no context
	version := mgr.PGVersion()                                                            //nolint:contextcheck // same as above
	tier := mgr.PermTier()                                                                //nolint:contextcheck // same as above
	exts := map[string]bool{"pg_stat_statements": mgr.HasExtension("pg_stat_statements")} //nolint:contextcheck // same as above

	dbs, err := mgr.Discover(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: discover databases for %s: %v\n", mgr.Database(), err)
	}

	for _, c := range check.All() {
		interval := cfg.GetCheckInterval(c.Name())
		if c.Name() == "ash" {
			// Driven by a dedicated sampler+aggregator (startASH, ash.go),
			// not the scheduler's once-per-interval Scrape() model — a
			// continuously-running 1-second loop doesn't fit that shape.
			// The registered check itself stays for its own unit tests, but
			// must never actually be scheduled, or its own superficial
			// count-only scrape would double-report under the same "ash"
			// check name.
			continue
		}
		req := c.Requires()
		if ok, _ := req.Supports(role, version, tier, exts); !ok {
			continue
		}
		if req.Scope == check.ScopeDatabase {
			for _, db := range dbs {
				if db.Monitored {
					scheduler.AddEntryWithInterval(mgr, c, db.Name, interval)
				}
			}
			continue
		}
		scheduler.AddEntryWithInterval(mgr, c, "", interval)
	}
}

func connOptionsFromConfig(tc agent.TargetConfig) agent.ConnOptions {
	opts := agent.DefaultConnOptions()
	if tc.Databases.Max > 0 {
		opts.MaxDatabases = tc.Databases.Max
	}
	return opts
}

func openAgentBuffer(cfg *agent.Config) (*buffer.Buffer, error) {
	path := cfg.Buffer.Path
	if path == "" {
		path = "/var/lib/pglens/buffer"
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return nil, err
	}
	return buffer.Open(path, buffer.Options{
		MaxSize: cfg.GetParsedBufferMaxSize(),
		MaxAge:  cfg.GetParsedBufferMaxAge(),
		Clock:   clock.System(),
	})
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func boolPtr(v bool) *bool { return &v }
