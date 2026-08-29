package host

import (
	"context"
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
func TestCollect_SecondCallComputesCPURatio(t *testing.T) {
	c := fixtureCollector()
	_, err := c.Collect(context.Background(), "")
	require.NoError(t, err)
	s, err := c.Collect(context.Background(), "")
	require.NoError(t, err)
	require.NotNil(t, s.CPUUsedRatio)
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
