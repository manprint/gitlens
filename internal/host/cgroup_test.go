package host

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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
	applyV2("testdata/cgroup_v2_unlimited/fs/cgroup", &s)
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
	applyV2("testdata/cgroup_v2/fs/cgroup", &s)
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
	applyV2("testdata/cgroup_v2/fs/cgroup", &s)
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
	applyV2(root, &s)
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
	applyV2(root, &s)
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
	applyV2("testdata/cgroup_v2/fs/cgroup", &s)
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
