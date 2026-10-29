//go:build integration

package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/host"
	"github.com/manprint/pglens/test/pgtest"
)

// INT-HOST-001: a Tier 0 role reads the container's data directory, and the
// collector on the test host, which cannot see that path, reports disk
// metrics as unavailable while keeping cpu and memory.
func TestINTHOST001_DataDirectoryFromATierZeroRole(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		ctx := context.Background()
		mgr, err := NewManager(ctx, "host-dd", pg.DSN("postgres", pgtest.RoleT0), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()

		dir, err := mgr.DataDirectory(ctx)
		require.NoError(t, err)
		require.True(t, filepath.IsAbs(dir), dir)
		require.Equal(t, filepath.Clean(dir), dir)
		again, err := mgr.DataDirectory(ctx)
		require.NoError(t, err)
		require.Equal(t, dir, again)

		if _, statErr := os.Stat(dir); statErr == nil {
			t.Skipf("the container's data directory %s also exists on the test host", dir)
		}
		sample, err := host.NewCollector("/proc", "/sys").Collect(ctx, dir)
		require.NoError(t, err)
		require.Nil(t, sample.DiskTotalBytes)
		require.NotEmpty(t, sample.DiskUnavailable)
		require.NotNil(t, sample.MemTotalBytes)
	})
}
