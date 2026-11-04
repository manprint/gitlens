package main

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/internal/host"
	"github.com/manprint/pglens/internal/wire"
)

func decodeFlags(t *testing.T, r wire.Result) wire.AgentFlags {
	t.Helper()
	require.Equal(t, wire.AgentFlagsCheck, r.Check)
	require.Len(t, r.Facts, 1)
	require.NoError(t, r.Facts[0].Validate())
	require.Equal(t, wire.AgentFlagsKey, r.Facts[0].Key)
	var f wire.AgentFlags
	require.NoError(t, json.Unmarshal(r.Facts[0].ValueJSON, &f))
	return f
}

func flagsConfig(host string) (*agent.Config, agent.TargetConfig) {
	target := agent.TargetConfig{Name: "t1", DSN: "postgres://u@" + host + "/postgres"}
	return &agent.Config{Targets: []agent.TargetConfig{target}}, target
}

// RDY-AGENT-001: the flags come from the resolved configuration: defaults,
// explicit switches, host_local, and the by_application value the activity
// check really runs with (a key absent from agent.yaml leaves the check's own
// default, not false by construction).
func TestRDYAGENT001_FlagsFromResolvedConfig(t *testing.T) {
	now := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)

	cfg, target := flagsConfig("127.0.0.1")
	configureChecks(cfg)
	f := decodeFlags(t, agentFlagsWireResult(cfg, target, nil, now))
	require.True(t, *f.CommandsEnabled, "commands.enabled defaults to true")
	require.False(t, *f.AllowSignal)
	require.NotNil(t, f.ByApplication, "the activity check reports its value even when the key is absent")
	require.True(t, *f.HostLocal)
	require.Nil(t, f.SessionQueryText, "no agent setting controls it yet: unknown, never false")
	require.Nil(t, f.DataDirectoryReadable, "unknown until a host collection knew the data directory")

	off, on := false, true
	cfg, target = flagsConfig("db.example.com")
	cfg.Commands.Enabled = &off
	cfg.Host.Enabled = &on
	target.AllowSignal = true
	cfg.Checks = agent.ChecksConfig{"activity": {ByApplication: true}}
	configureChecks(cfg)
	f = decodeFlags(t, agentFlagsWireResult(cfg, target, &on, now))
	require.False(t, *f.CommandsEnabled)
	require.True(t, *f.AllowSignal)
	require.True(t, *f.ByApplication)
	require.False(t, *f.HostLocal, "a remote DSN is not host-local")
	require.True(t, *f.DataDirectoryReadable)

	// An explicit by_application: false is honoured after a true one.
	cfg.Checks = agent.ChecksConfig{"activity": {ByApplication: false}}
	configureChecks(cfg)
	f = decodeFlags(t, agentFlagsWireResult(cfg, target, nil, now))
	require.False(t, *f.ByApplication)

	// Host collection disabled: not host-local whatever the DSN.
	cfg, target = flagsConfig("127.0.0.1")
	cfg.Host.Enabled = &off
	f = decodeFlags(t, agentFlagsWireResult(cfg, target, nil, now))
	require.False(t, *f.HostLocal)
}

// RDY-AGENT-002: the result carries no secret and no path.
func TestRDYAGENT002_NoSecretNoPath(t *testing.T) {
	cfg, target := flagsConfig("127.0.0.1")
	cfg.Server.Token = "super-secret-token"
	target.DSN = "postgres://pglens:hunter2@127.0.0.1/postgres"
	cfg.Host.ProcPath = "/host/proc"
	raw := agentFlagsWireResult(cfg, target, nil, time.Now()).Facts[0].ValueJSON
	for _, banned := range []string{"super-secret-token", "hunter2", "/host/proc", "postgres://"} {
		require.NotContains(t, string(raw), banned)
	}
}

// RDY-AGENT-003: queueAll puts one result per target in pending, and
// observeHost reports a changed data directory readability once.
func TestRDYAGENT003_QueueAndObserveHost(t *testing.T) {
	cfg, target := flagsConfig("127.0.0.1")
	second := agent.TargetConfig{Name: "t2", DSN: "postgres://u@db.example.com/postgres"}
	cfg.Targets = []agent.TargetConfig{target, second}
	state := newAgentFlagsState(cfg)
	now := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)

	var mu sync.Mutex
	pending := map[string][]wire.Result{}
	state.queueAll(&mu, pending, now)
	require.Len(t, pending["t1"], 1)
	require.Len(t, pending["t2"], 1)

	total := uint64(100)
	// A sample without a disk reading says nothing about the data directory.
	_, ok := state.observeHost("t1", host.Sample{}, now)
	require.False(t, ok)
	// The first readable sample is a change from "unknown".
	r, ok := state.observeHost("t1", host.Sample{DiskTotalBytes: &total}, now)
	require.True(t, ok)
	require.True(t, *decodeFlags(t, r).DataDirectoryReadable)
	// The same reading again is not.
	_, ok = state.observeHost("t1", host.Sample{DiskTotalBytes: &total}, now)
	require.False(t, ok)
	// Unavailable is a change.
	r, ok = state.observeHost("t1", host.Sample{DiskUnavailable: "permission denied"}, now)
	require.True(t, ok)
	require.False(t, *decodeFlags(t, r).DataDirectoryReadable)
	// A target the state does not know is ignored.
	_, ok = state.observeHost("nope", host.Sample{DiskTotalBytes: &total}, now)
	require.False(t, ok)
	// The next hourly result carries the last reading.
	require.False(t, *decodeFlags(t, state.results(now)["t1"]).DataDirectoryReadable)
	require.Nil(t, decodeFlags(t, state.results(now)["t2"]).DataDirectoryReadable)
}

// RDY-AGENT-004: an unreachable server must not cost the hour-long agent_flags
// result: requeueSkips puts the newest one back, once.
func TestRDYAGENT004_RequeueKeepsFlags(t *testing.T) {
	var mu sync.Mutex
	cfg, target := flagsConfig("127.0.0.1")
	old := agentFlagsWireResult(cfg, target, nil, time.Unix(1, 0))
	newer := agentFlagsWireResult(cfg, target, nil, time.Unix(2, 0))
	batch := []wire.Result{old, {Check: "activity"}, newer}

	pending := map[string][]wire.Result{}
	requeueSkips(&mu, pending, "t1", batch)
	require.Len(t, pending["t1"], 1)
	require.Equal(t, newer.TS, pending["t1"][0].TS)

	// A fresh one already queued is kept alone.
	pending = map[string][]wire.Result{"t1": {agentFlagsWireResult(cfg, target, nil, time.Unix(3, 0))}}
	requeueSkips(&mu, pending, "t1", batch)
	require.Len(t, pending["t1"], 1)
	require.Equal(t, time.Unix(3, 0), pending["t1"][0].TS)
}
