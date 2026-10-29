package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/host"
)

func metricNames(s host.Sample) []string {
	var names []string
	for _, m := range hostWireResult(s, time.Unix(0, 0)).Metrics {
		names = append(names, m.Name)
	}
	return names
}

// TestHOSTDD006_HostWireResultDiskMetrics: a measured data directory sends the
// three disk metrics; an unavailable one sends none, the rest still go out.
func TestHOSTDD006_HostWireResultDiskMetrics(t *testing.T) {
	total, free := uint64(1000), uint64(250)
	mem := uint64(4096)
	measured := host.Sample{MemTotalBytes: &mem, DiskTotalBytes: &total, DiskFreeBytes: &free, Source: "host"}
	names := metricNames(measured)
	require.Contains(t, names, "host_disk_total_bytes")
	require.Contains(t, names, "host_disk_free_bytes")
	require.Contains(t, names, "host_disk_free_ratio")
	for _, m := range hostWireResult(measured, time.Unix(0, 0)).Metrics {
		if m.Name == "host_disk_free_ratio" {
			require.InDelta(t, 0.25, m.Value, 1e-12)
		}
	}

	unavailable := measured
	unavailable.DiskUnavailable = "data directory /pgdata is not readable"
	names = metricNames(unavailable)
	for _, name := range names {
		require.False(t, strings.HasPrefix(name, "host_disk_"), name)
	}
	require.Contains(t, names, "host_mem_total_bytes")
	require.Contains(t, names, "host_metrics_source")
}

// TestHOSTDD007_GroupTargetsByDataDir: two targets on one directory share a
// bucket; a target whose directory is unknown goes in the empty bucket.
func TestHOSTDD007_GroupTargetsByDataDir(t *testing.T) {
	buckets := groupTargetsByDataDir(
		[]string{"replica", "primary", "starting"},
		map[string]string{"primary": "/pgdata", "replica": "/pgdata"},
	)
	require.Equal(t, map[string][]string{
		"/pgdata": {"primary", "replica"},
		"":        {"starting"},
	}, buckets)
	require.Empty(t, groupTargetsByDataDir(nil, nil))
}

// TestHOSTDD008_DiskWarningOncePerTargetAndReason: the same reason is logged
// once per target; a new reason is logged again.
func TestHOSTDD008_DiskWarningOncePerTargetAndReason(t *testing.T) {
	var out bytes.Buffer
	w := newDiskWarner(&out)
	w.warn("a", "not mounted")
	w.warn("a", "not mounted")
	w.warn("b", "not mounted")
	w.warn("a", "")
	w.warn("a", "permission denied")
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 3)
	require.Equal(t, "warning: host disk metrics unavailable for target a: not mounted (mount the data directory read-only at the same path in the agent container)", lines[0])
	require.Contains(t, lines[1], "target b: not mounted")
	require.Contains(t, lines[2], "target a: permission denied")
}

// TestHOSTDD008_CollectByDataDirWarnsAndEmitsPerTarget: one collection per
// bucket; every target of a bucket gets the sample; an unmeasurable directory
// warns per target and still emits cpu and memory.
func TestHOSTDD008_CollectByDataDirWarnsAndEmitsPerTarget(t *testing.T) {
	collector := host.NewCollector("../../internal/host/testdata/proc", "../../internal/host/testdata/sys")
	var warnings, errs bytes.Buffer
	got := map[string]host.Sample{}
	collectHostByDataDir(context.Background(), collector,
		map[string][]string{t.TempDir(): {"p1", "p2"}, "/nonexistent/pgdata": {"p3"}, "": {"p4"}},
		newDiskWarner(&warnings), &errs,
		func(target string, s host.Sample) { got[target] = s })

	require.Len(t, got, 4)
	require.NotNil(t, got["p1"].DiskTotalBytes)
	require.Equal(t, got["p1"].DiskTotalBytes, got["p2"].DiskTotalBytes)
	require.Nil(t, got["p3"].DiskTotalBytes)
	require.NotNil(t, got["p3"].MemTotalBytes)
	require.Nil(t, got["p4"].DiskTotalBytes)
	require.Empty(t, got["p4"].DiskUnavailable)
	require.Equal(t, 1, strings.Count(warnings.String(), "\n"))
	require.Contains(t, warnings.String(), "target p3: data directory /nonexistent/pgdata is not readable")
	require.Empty(t, errs.String())
}

// TestHOSTDD009_OneCPUReadingServesEveryDataDir: two data directories in one
// tick both carry the CPU ratio measured over the whole tick. Collecting once
// per directory measured the second over a few milliseconds and lost it.
func TestHOSTDD009_OneCPUReadingServesEveryDataDir(t *testing.T) {
	proc := t.TempDir()
	for _, name := range []string{"loadavg", "meminfo", "vmstat"} {
		data, err := os.ReadFile(filepath.Join("../../internal/host/testdata/proc", name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(proc, name), data, 0o600))
	}
	writeStat := func(user, idle int) {
		line := func(cpu string) string {
			return cpu + " " + itoa(user) + " 0 0 " + itoa(idle) + " 0 0 0 0 0 0\n"
		}
		require.NoError(t, os.WriteFile(filepath.Join(proc, "stat"), []byte(line("cpu ")+line("cpu0")), 0o600))
	}
	writeStat(100, 900)
	collector := host.NewCollector(proc, "../../internal/host/testdata/sys")
	_, err := collector.Collect(context.Background(), "") // previous tick
	require.NoError(t, err)
	writeStat(150, 950) // 50 busy of 100 ticks

	got := map[string]host.Sample{}
	var warnings, errs bytes.Buffer
	collectHostByDataDir(context.Background(), collector,
		map[string][]string{t.TempDir(): {"a"}, t.TempDir(): {"b"}, "": {"c"}},
		newDiskWarner(&warnings), &errs,
		func(target string, s host.Sample) { got[target] = s })

	require.Len(t, got, 3)
	for _, target := range []string{"a", "b", "c"} {
		require.NotNil(t, got[target].CPUUsedRatio, target)
		require.InDelta(t, 0.5, *got[target].CPUUsedRatio, 1e-9, target)
	}
	require.NotNil(t, got["a"].DiskTotalBytes)
	require.NotNil(t, got["b"].DiskTotalBytes)
	require.Nil(t, got["c"].DiskTotalBytes)
	require.Empty(t, warnings.String())
	require.Empty(t, errs.String())
}

// TestHOSTDD010_DiskWarningAgainAfterRecovery: once a target's disk is
// measured, the same failure is logged again when it comes back.
func TestHOSTDD010_DiskWarningAgainAfterRecovery(t *testing.T) {
	var out bytes.Buffer
	w := newDiskWarner(&out)
	w.warn("a", "not mounted")
	w.warn("a", "")
	w.warn("a", "not mounted")
	require.Equal(t, 2, strings.Count(out.String(), "target a: not mounted"))
}

func itoa(n int) string { return strconv.Itoa(n) }
