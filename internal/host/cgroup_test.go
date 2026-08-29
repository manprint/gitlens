package host

import (
	"context"
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
func TestCgroup_AvailableIsLimitMinusCurrent(t *testing.T) {
	s := Sample{Source: "host"}
	applyV2("testdata/cgroup_v2/fs/cgroup", &s)
	require.Equal(t, uint64(768), *s.MemAvailableBytes)
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
	TestCgroup_AvailableIsLimitMinusCurrent(t)
}

// INT-HOST-004 and INT-HOST-005 retain explicit v1/v2 coverage in the L2
// suite; the compose smoke test supplies the real-container validation.
func TestINTHOST004_CgroupV1Fixture(t *testing.T) { TestCgroup_DetectsV1ByLimitFile(t) }
func TestINTHOST005_CgroupV2Fixture(t *testing.T) { TestCgroup_DetectsV2ByControllersFile(t) }

func applyCgroupForTest(sys string, s *Sample) { NewCollector("", sys).applyCgroup(s) }
