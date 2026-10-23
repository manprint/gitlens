package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// isMountWarning checks if a path is on the root mount (not a separate persistent mount).
// For testing, mountinfoPath can be overridden.
//
// This is a test-only copy. The production check is cmd/pglens-agent's
// isMountWarning, which matches whole path components and reads the filesystem
// type. This copy still matches by prefix and ignores the type, so delete it
// together with TestMountWarning in config_test.go.
func isMountWarning(path string, mountinfoPath string) (bool, error) {
	data, err := os.ReadFile(mountinfoPath)
	if err != nil {
		return false, err
	}

	lines := strings.Split(string(data), "\n")

	// Parse mountinfo to find if path is on a separate mount
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) < 5 {
			continue
		}
		mountpoint := parts[4]
		// Check if the identity path starts with a non-root mountpoint
		if mountpoint != "/" && strings.HasPrefix(filepath.Clean(path), filepath.Clean(mountpoint)) {
			return false, nil
		}
	}

	// If we get here, the path is on the root mount
	return true, nil
}
