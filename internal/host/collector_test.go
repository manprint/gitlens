package host

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func fixtureCollector() *Collector {
	return NewCollector("testdata/proc", "testdata/sys")
}

func TestCollect_ReadsMemTotalFromProc(t *testing.T) {
	s, err := fixtureCollector().Collect(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, uint64(1048576*1024), *s.MemTotalBytes)
}
func TestCollect_FirstCallHasNilCPURatio(t *testing.T) {
	s, err := fixtureCollector().Collect(context.Background(), "")
	require.NoError(t, err)
	require.Nil(t, s.CPUUsedRatio)
}

// copyFixtureProc copies the proc fixture into a temp dir, so a test can advance
// /proc/stat between two collections.
func copyFixtureProc(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	entries, err := os.ReadDir("testdata/proc")
	require.NoError(t, err)
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join("testdata/proc", e.Name()))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, e.Name()), data, 0o644))
	}
	return dir
}

// setAggregateCPU replaces the aggregate cpu line, the first line of stat. The
// ratio is measured in jiffies, so this is what moves between two samples.
func setAggregateCPU(t *testing.T, dir, line string) {
	t.Helper()
	path := filepath.Join(dir, "stat")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	rest := strings.SplitN(string(data), "\n", 2)[1]
	require.NoError(t, os.WriteFile(path, []byte(line+"\n"+rest), 0o644))
}

// The ratio is the busy share of the jiffies since the previous sample. Between
// these two readings user and idle each advance by 100, so half of the 200
// jiffies were busy.
func TestCollect_SecondCallComputesCPURatio(t *testing.T) {
	dir := copyFixtureProc(t)
	c := NewCollector(dir, "testdata/sys")
	_, err := c.Collect(context.Background(), "")
	require.NoError(t, err)
	setAggregateCPU(t, dir, "cpu  200 20 30 950 0 0 0 0 0 0")
	s, err := c.Collect(context.Background(), "")
	require.NoError(t, err)
	require.NotNil(t, s.CPUUsedRatio)
	require.InDelta(t, 0.5, *s.CPUUsedRatio, 1e-9)
}

// An interval that measured nothing leaves the ratio unknown rather than idle:
// one with no jiffies elapsed, or one whose counters went backwards. The next
// reading is measured from the one before it.
func TestCollect_UnmeasuredIntervalIsUnknown(t *testing.T) {
	ctx := context.Background()
	dir := copyFixtureProc(t)
	c := NewCollector(dir, "testdata/sys")

	s, err := c.Collect(ctx, "")
	require.NoError(t, err)
	require.Nil(t, s.CPUUsedRatio, "no previous reading")

	s, err = c.Collect(ctx, "")
	require.NoError(t, err)
	require.Nil(t, s.CPUUsedRatio, "no jiffies elapsed")

	setAggregateCPU(t, dir, "cpu  50 20 30 800 0 0 0 0 0 0")
	s, err = c.Collect(ctx, "")
	require.NoError(t, err)
	require.Nil(t, s.CPUUsedRatio, "counters went backwards")

	// From the backwards reading: 100 busy of the 250 jiffies that elapsed.
	setAggregateCPU(t, dir, "cpu  150 20 30 950 0 0 0 0 0 0")
	s, err = c.Collect(ctx, "")
	require.NoError(t, err)
	require.NotNil(t, s.CPUUsedRatio)
	require.InDelta(t, 0.4, *s.CPUUsedRatio, 1e-9)
}
func TestCollect_EmptyDataDirLeavesDiskNil(t *testing.T) {
	s, err := fixtureCollector().Collect(context.Background(), "")
	require.NoError(t, err)
	require.Nil(t, s.DiskTotalBytes)
	require.Nil(t, s.DiskFreeBytes)
}
func TestCollect_LoadAverages(t *testing.T) {
	s, err := fixtureCollector().Collect(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, 0.42, *s.Load1)
	require.Equal(t, 1.23, *s.Load5)
	require.Equal(t, 2.34, *s.Load15)
}
func TestCollect_SourceIsHostWhenNoCgroup(t *testing.T) {
	s, err := fixtureCollector().Collect(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, "host", s.Source)
}
