//go:build integration

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestMain gives every Manager in this package a writable identity store.
// The production default, /var/lib/pglens/identity.json, is not writable on
// a developer machine or a CI runner, and t.Setenv cannot be used under
// pgtest.ForEach's parallel subtests.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "pglens-identity-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if os.Getenv("PGLENS_IDENTITY_PATH") == "" {
		_ = os.Setenv("PGLENS_IDENTITY_PATH", filepath.Join(dir, "identity.json"))
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
