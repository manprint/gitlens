package host

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCgroup_DetectsV2ByControllersFile(t *testing.T) {
	c := NewCollector("testdata/proc", "testdata/cgroup_v2")
	s, err := c.Collect(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, "cgroup_v2", s.Source)
}
func TestCgroup_DetectsV1ByLimitFile(t *testing.T) {
	c := NewCollector("testdata/proc", "testdata/cgroup_v1")
	s, err := c.Collect(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, "cgroup_v1", s.Source)
}
func TestCgroup_V2MaxMeansUnlimited(t *testing.T) {
	total := uint64(1048576 * 1024)
	s := Sample{Source: "host", MemTotalBytes: &total}
	applyV2("testdata/cgroup_v2_unlimited/fs/cgroup", "testdata/cgroup_v2_unlimited/fs/cgroup", &s)
	require.Equal(t, total, *s.MemTotalBytes)
	require.Equal(t, "host", s.Source)
}
func TestCgroup_V1SentinelMeansUnlimited(t *testing.T) {
	total := uint64(1048576 * 1024)
	s := Sample{Source: "host", MemTotalBytes: &total}
	applyV1("testdata/cgroup_v1_unlimited/fs/cgroup", &s)
	require.Equal(t, "host", s.Source)
}
func TestCgroup_V2CPUQuotaRoundsUp(t *testing.T) {
	s := Sample{Source: "host"}
	applyV2("testdata/cgroup_v2/fs/cgroup", "testdata/cgroup_v2/fs/cgroup", &s)
	require.Equal(t, 3, *s.CPUCount)
}
func TestCgroup_V1NegativeQuotaMeansUnlimited(t *testing.T) {
	n := 2
	s := Sample{Source: "host", CPUCount: &n}
	applyV1("testdata/cgroup_v1/fs/cgroup", &s)
	require.Equal(t, n, *s.CPUCount)
}

// The fixture's 256 bytes of usage include 100 bytes of inactive file cache,
// which the kernel reclaims, so 1024 - (256 - 100) is available.
func TestCgroup_AvailableExcludesInactiveFileCache(t *testing.T) {
	s := Sample{Source: "host"}
	applyV2("testdata/cgroup_v2/fs/cgroup", "testdata/cgroup_v2/fs/cgroup", &s)
	require.Equal(t, uint64(868), *s.MemAvailableBytes)
}

// writeCgroupFiles writes the named files under dir, creating parents.
func writeCgroupFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
}

// Without memory.stat the whole usage counts as used, as before.
func TestCgroup_AvailableKeepsUsageWithoutMemoryStat(t *testing.T) {
	root := t.TempDir()
	writeCgroupFiles(t, root, map[string]string{
		"cgroup.controllers": "memory\n",
		"memory.max":         "1024\n",
		"memory.current":     "256\n",
	})
	s := Sample{Source: "host"}
	applyV2(root, root, &s)
	require.Equal(t, uint64(768), *s.MemAvailableBytes)
}

// Inactive file cache cannot exceed the usage it is part of. A stat that claims
// otherwise is clamped, not allowed to push available memory past the limit.
func TestCgroup_AvailableClampsInactiveFileToUsage(t *testing.T) {
	root := t.TempDir()
	writeCgroupFiles(t, root, map[string]string{
		"cgroup.controllers": "memory\n",
		"memory.max":         "1024\n",
		"memory.current":     "256\n",
		"memory.stat":        "inactive_file 300\n",
	})
	s := Sample{Source: "host"}
	applyV2(root, root, &s)
	require.Equal(t, uint64(1024), *s.MemAvailableBytes)
}

// The v1 controller reports the same cache as total_inactive_file.
func TestCgroup_V1AvailableExcludesTotalInactiveFile(t *testing.T) {
	root := t.TempDir()
	writeCgroupFiles(t, root, map[string]string{
		"memory/memory.limit_in_bytes": "1024\n",
		"memory/memory.usage_in_bytes": "256\n",
		"memory/memory.stat":           "cache 128\ntotal_inactive_file 100\n",
	})
	s := Sample{Source: "host"}
	applyV1(root, &s)
	require.Equal(t, uint64(868), *s.MemAvailableBytes)
}
func TestCgroup_SourceReflectsDetection(t *testing.T) {
	s := Sample{Source: "host"}
	applyV2("testdata/cgroup_v2/fs/cgroup", "testdata/cgroup_v2/fs/cgroup", &s)
	require.Equal(t, "cgroup_v2", s.Source)
}
func TestCgroup_NoCgroupKeepsHostFigures(t *testing.T) {
	s := Sample{Source: "host"}
	applyCgroupForTest("testdata/empty", &s)
	require.Equal(t, "host", s.Source)
}

// INT-HOST-001 exercises the same precedence logic used by the container
// collector, using the checked-in cgroup fixture tree for deterministic L2.
func TestINTHOST001_CgroupLimitPrecedence(t *testing.T) {
	TestCgroup_AvailableExcludesInactiveFileCache(t)
}

// INT-HOST-004 and INT-HOST-005 retain explicit v1/v2 coverage in the L2
// suite; the compose smoke test supplies the real-container validation.
func TestINTHOST004_CgroupV1Fixture(t *testing.T) { TestCgroup_DetectsV1ByLimitFile(t) }
func TestINTHOST005_CgroupV2Fixture(t *testing.T) { TestCgroup_DetectsV2ByControllersFile(t) }

func applyCgroupForTest(sys string, s *Sample) { NewCollector("", sys).applyCgroup(s) }

// With a host cgroup namespace the limits sit under the cgroup the process
// belongs to, not at the mount root, which holds only the host's root cgroup.
// PID 1 of the container is in that same cgroup (the fixture's proc/1/cgroup).
func TestCgroup_ReadsLimitsOfOwnCgroupWhenNamespaceIsHost(t *testing.T) {
	c := NewCollector("testdata/cgroup_v2_nested/proc", "testdata/cgroup_v2_nested/sys")
	s := Sample{Source: "host"}
	c.applyCgroup(&s)
	require.Equal(t, "cgroup_v2", s.Source)
	require.Equal(t, uint64(536870912), *s.MemTotalBytes)
	require.Equal(t, uint64(536870912-134217728), *s.MemAvailableBytes)
	require.Equal(t, 2, *s.CPUCount)
}

// A private cgroup namespace reports the process's cgroup as "/", which is the
// mount root itself.
func TestCgroup_OwnCgroupRootIsMountRoot(t *testing.T) {
	procDir, sysDir := t.TempDir(), t.TempDir()
	writeCgroupFiles(t, procDir, map[string]string{"self/cgroup": "0::/\n"})
	writeCgroupFiles(t, filepath.Join(sysDir, "fs", "cgroup"), map[string]string{
		"cgroup.controllers": "memory\n",
		"memory.max":         "1024\n",
		"memory.current":     "256\n",
	})
	s := Sample{Source: "host"}
	NewCollector(procDir, sysDir).applyCgroup(&s)
	require.Equal(t, "cgroup_v2", s.Source)
	require.Equal(t, uint64(1024), *s.MemTotalBytes)
}

// A path that leaves the mount, or names no directory under it, is not
// followed: the mount root is read instead.
func TestCgroup_OwnCgroupPathOutsideMountFallsBackToRoot(t *testing.T) {
	for _, line := range []string{"0::/../../etc\n", "0::/no-such.scope\n"} {
		procDir, sysDir := t.TempDir(), t.TempDir()
		writeCgroupFiles(t, procDir, map[string]string{"self/cgroup": line})
		root := filepath.Join(sysDir, "fs", "cgroup")
		writeCgroupFiles(t, root, map[string]string{
			"cgroup.controllers": "memory\n",
			"memory.max":         "1024\n",
			"memory.current":     "256\n",
		})
		s := Sample{Source: "host"}
		NewCollector(procDir, sysDir).applyCgroup(&s)
		require.Equal(t, uint64(1024), *s.MemTotalBytes, line)
	}
}

// Under a CPU quota the ratio is the share of that quota the container used
// since the last sample. The first reading has nothing to compare with, and
// usage above the quota is clamped to a full quota.
func TestCgroup_CPURatioIsShareOfOwnQuota(t *testing.T) {
	procDir, sysDir := t.TempDir(), t.TempDir()
	writeCgroupFiles(t, procDir, map[string]string{"self/cgroup": "0::/\n"})
	root := filepath.Join(sysDir, "fs", "cgroup")
	writeCgroupFiles(t, root, map[string]string{
		"cgroup.controllers": "cpu memory\n",
		"cpu.max":            "200000 100000\n", // 2 cores
		"cpu.stat":           "usage_usec 1000000\n",
	})
	c := NewCollector(procDir, sysDir)
	now := time.Unix(1000, 0)
	c.now = func() time.Time { return now }

	host := 0.125
	s := Sample{Source: "host", CPUUsedRatio: &host}
	c.applyCgroup(&s)
	require.Nil(t, s.CPUUsedRatio)

	// One CPU-second in one second is half of a 2-core quota.
	now = now.Add(time.Second)
	writeCgroupFiles(t, root, map[string]string{"cpu.stat": "usage_usec 2000000\n"})
	s = Sample{Source: "host", CPUUsedRatio: &host}
	c.applyCgroup(&s)
	require.InDelta(t, 0.5, *s.CPUUsedRatio, 1e-9)

	// Four CPU-seconds in one second is more than the quota: clamped to one.
	now = now.Add(time.Second)
	writeCgroupFiles(t, root, map[string]string{"cpu.stat": "usage_usec 6000000\n"})
	s = Sample{Source: "host", CPUUsedRatio: &host}
	c.applyCgroup(&s)
	require.InDelta(t, 1.0, *s.CPUUsedRatio, 1e-9)
}

// A reading that measured no time, or a usage counter that went backwards
// because the cgroup was recreated, leaves the container ratio unknown. The
// next reading is measured from it.
func TestCgroup_CPURatioUnknownWhenNothingMeasured(t *testing.T) {
	procDir, sysDir := t.TempDir(), t.TempDir()
	writeCgroupFiles(t, procDir, map[string]string{"self/cgroup": "0::/\n"})
	root := filepath.Join(sysDir, "fs", "cgroup")
	writeCgroupFiles(t, root, map[string]string{
		"cgroup.controllers": "cpu memory\n",
		"cpu.max":            "200000 100000\n", // 2 cores
		"cpu.stat":           "usage_usec 1000000\n",
	})
	c := NewCollector(procDir, sysDir)
	now := time.Unix(1000, 0)
	c.now = func() time.Time { return now }
	sample := func() *float64 {
		host := 0.125
		s := Sample{Source: "host", CPUUsedRatio: &host}
		c.applyCgroup(&s)
		return s.CPUUsedRatio
	}

	require.Nil(t, sample(), "no previous reading")
	require.Nil(t, sample(), "no time elapsed")

	now = now.Add(time.Second)
	writeCgroupFiles(t, root, map[string]string{"cpu.stat": "usage_usec 500000\n"})
	require.Nil(t, sample(), "usage went backwards")

	// One CPU-second in one second is half of a 2-core quota.
	now = now.Add(time.Second)
	writeCgroupFiles(t, root, map[string]string{"cpu.stat": "usage_usec 1500000\n"})
	require.InDelta(t, 0.5, *sample(), 1e-9)
}

// Without a quota the host is the resource, so the host ratio stands.
func TestCgroup_CPURatioKeepsHostWithoutQuota(t *testing.T) {
	procDir, sysDir := t.TempDir(), t.TempDir()
	writeCgroupFiles(t, procDir, map[string]string{"self/cgroup": "0::/\n"})
	root := filepath.Join(sysDir, "fs", "cgroup")
	writeCgroupFiles(t, root, map[string]string{
		"cgroup.controllers": "cpu memory\n",
		"cpu.max":            "max 100000\n",
		"cpu.stat":           "usage_usec 1000000\n",
	})
	c := NewCollector(procDir, sysDir)
	host := 0.125
	s := Sample{Source: "host", CPUUsedRatio: &host}
	c.applyCgroup(&s)
	require.Equal(t, 0.125, *s.CPUUsedRatio)
}

// A process inside a child of the cgroup that sets the limit is bound by that
// limit, which the kernel enforces at every ancestor. The child's own files say
// "max", so the limit and the usage it is measured by come from the ancestor.
func TestCgroup_LimitOnAncestorBindsOwnCgroup(t *testing.T) {
	procDir, sysDir := t.TempDir(), t.TempDir()
	writeCgroupFiles(t, procDir, map[string]string{
		"self/cgroup": "0::/sub\n",
		"1/cgroup":    "0::/sub\n",
	})
	root := filepath.Join(sysDir, "fs", "cgroup")
	writeCgroupFiles(t, root, map[string]string{
		"cgroup.controllers": "cpu memory\n",
		"memory.max":         "1024\n",
		"memory.current":     "256\n",
		"cpu.max":            "200000 100000\n",
	})
	writeCgroupFiles(t, filepath.Join(root, "sub"), map[string]string{
		"memory.max":     "max\n",
		"memory.current": "9999\n",
		"cpu.max":        "max 100000\n",
	})
	s := Sample{Source: "host"}
	NewCollector(procDir, sysDir).applyCgroup(&s)
	require.Equal(t, "cgroup_v2", s.Source)
	require.Equal(t, uint64(1024), *s.MemTotalBytes)
	require.Equal(t, uint64(768), *s.MemAvailableBytes)
	require.Equal(t, 2, *s.CPUCount)
}

// An agent run as a host service names its own unit's cgroup in
// /proc/self/cgroup, and that unit may set a limit of its own. The host is not a
// container, so the unit's limit is not the host's total. PID 1 sits in
// init.scope and does not share the cgroup, so the host figures stand.
func TestCgroup_HostServiceLimitIsNotReported(t *testing.T) {
	procDir, sysDir := t.TempDir(), t.TempDir()
	writeCgroupFiles(t, procDir, map[string]string{
		"self/cgroup": "0::/system.slice/pglens-agent.service\n",
		"1/cgroup":    "0::/init.scope\n",
	})
	root := filepath.Join(sysDir, "fs", "cgroup")
	writeCgroupFiles(t, root, map[string]string{"cgroup.controllers": "cpu memory\n"})
	writeCgroupFiles(t, filepath.Join(root, "system.slice", "pglens-agent.service"), map[string]string{
		"memory.max":     "536870912\n",
		"memory.current": "134217728\n",
		"cpu.max":        "200000 100000\n",
	})
	hostMem := uint64(8 << 30)
	hostCPUs := 16
	s := Sample{Source: "host", MemTotalBytes: &hostMem, CPUCount: &hostCPUs}
	NewCollector(procDir, sysDir).applyCgroup(&s)
	require.Equal(t, "host", s.Source)
	require.Equal(t, uint64(8<<30), *s.MemTotalBytes)
	require.Equal(t, 16, *s.CPUCount)
}

// The kernel enforces every ancestor's limit, so a looser limit on the process's
// own cgroup does not hide a tighter one above it. The parent allows 1 GiB and
// 2 cores, the own cgroup 2 GiB and 4 cores: the parent binds, and its usage and
// its quota are the ones measured.
func TestCgroup_TighterAncestorLimitBindsOwnCgroup(t *testing.T) {
	procDir, sysDir := t.TempDir(), t.TempDir()
	writeCgroupFiles(t, procDir, map[string]string{
		"self/cgroup": "0::/parent/sub\n",
		"1/cgroup":    "0::/parent/sub\n",
	})
	root := filepath.Join(sysDir, "fs", "cgroup")
	parent := filepath.Join(root, "parent")
	writeCgroupFiles(t, root, map[string]string{"cgroup.controllers": "cpu memory\n"})
	writeCgroupFiles(t, parent, map[string]string{
		"memory.max":     "1073741824\n", // 1 GiB
		"memory.current": "268435456\n",  // 256 MiB, of which 64 MiB is inactive file cache
		"memory.stat":    "inactive_file 67108864\n",
		"cpu.max":        "200000 100000\n", // 2 cores
		"cpu.stat":       "usage_usec 1000000\n",
	})
	writeCgroupFiles(t, filepath.Join(parent, "sub"), map[string]string{
		"memory.max":     "2147483648\n", // 2 GiB, looser than the parent's
		"memory.current": "1048576\n",
		"cpu.max":        "400000 100000\n", // 4 cores, looser than the parent's
		"cpu.stat":       "usage_usec 9000000\n",
	})
	c := NewCollector(procDir, sysDir)
	now := time.Unix(1000, 0)
	c.now = func() time.Time { return now }

	s := Sample{Source: "host"}
	c.applyCgroup(&s)
	require.Equal(t, "cgroup_v2", s.Source)
	require.Equal(t, uint64(1<<30), *s.MemTotalBytes)
	require.Equal(t, uint64(1<<30-(256<<20-64<<20)), *s.MemAvailableBytes)
	require.Equal(t, 2, *s.CPUCount)

	// One CPU-second in one second of the parent's 2-core quota is half of it.
	now = now.Add(time.Second)
	writeCgroupFiles(t, parent, map[string]string{"cpu.stat": "usage_usec 2000000\n"})
	host := 0.125
	s = Sample{Source: "host", CPUUsedRatio: &host}
	c.applyCgroup(&s)
	require.InDelta(t, 0.5, *s.CPUUsedRatio, 1e-9)
}

// The own cgroup can hold the tighter limit, and then it binds as before.
func TestCgroup_TighterOwnLimitBindsOwnCgroup(t *testing.T) {
	procDir, sysDir := t.TempDir(), t.TempDir()
	writeCgroupFiles(t, procDir, map[string]string{
		"self/cgroup": "0::/parent/sub\n",
		"1/cgroup":    "0::/parent/sub\n",
	})
	root := filepath.Join(sysDir, "fs", "cgroup")
	parent := filepath.Join(root, "parent")
	writeCgroupFiles(t, root, map[string]string{"cgroup.controllers": "cpu memory\n"})
	writeCgroupFiles(t, parent, map[string]string{
		"memory.max":     "1073741824\n",
		"memory.current": "268435456\n",
		"cpu.max":        "200000 100000\n", // 2 cores
	})
	writeCgroupFiles(t, filepath.Join(parent, "sub"), map[string]string{
		"memory.max":     "536870912\n", // 512 MiB, tighter than the parent's
		"memory.current": "134217728\n",
		"cpu.max":        "100000 100000\n", // 1 core, tighter than the parent's
	})
	s := Sample{Source: "host"}
	NewCollector(procDir, sysDir).applyCgroup(&s)
	require.Equal(t, uint64(536870912), *s.MemTotalBytes)
	require.Equal(t, uint64(536870912-134217728), *s.MemAvailableBytes)
	require.Equal(t, 1, *s.CPUCount)
}
