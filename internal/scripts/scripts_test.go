package scripts_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestE2EEvidenceScript_PropagatesExitStatus(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the repository E2E evidence harness is Linux-only")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is required")
	}

	artifacts := t.TempDir()
	cmd := exec.Command("bash", filepath.Join("..", "..", "scripts", "e2e_evidence.sh"), "/bin/sh", "-c", "exit 7")
	cmd.Env = append(os.Environ(), "E2E_ARTIFACTS_DIR="+artifacts)
	err := cmd.Run()
	if err == nil {
		t.Fatal("evidence script succeeded; want wrapped exit status 7")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 {
		t.Fatalf("evidence script error = %v; want exit status 7", err)
	}

	logs, err := filepath.Glob(filepath.Join(artifacts, "e2e-full-*.log"))
	if err != nil {
		t.Fatalf("glob evidence logs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("evidence logs = %d; want exactly one", len(logs))
	}
	data, err := os.ReadFile(logs[0])
	if err != nil {
		t.Fatalf("read evidence log: %v", err)
	}
	if !strings.Contains(string(data), "EXIT_STATUS=7") {
		t.Fatalf("evidence log does not contain EXIT_STATUS=7:\n%s", data)
	}
}

func TestIDAuditScript_ReportsBothDirections(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is required")
	}

	root := t.TempDir()
	for _, dir := range []string{"internal/sample", "test/sample", "cmd/sample"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}
	source := filepath.Join(root, "internal", "sample", "sample.go")
	sourceID := "INT-" + "X-001"
	if err := os.WriteFile(source, []byte("package sample\n\nvar _ = \""+sourceID+"\"\n"), 0o644); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}
	state := filepath.Join(root, "STATE.md")
	stateID := "SYS-" + "Y-002"
	if err := os.WriteFile(state, []byte(stateID+"\n"), 0o644); err != nil {
		t.Fatalf("write state fixture: %v", err)
	}

	cmd := exec.Command("bash", filepath.Join("..", "..", "scripts", "id_audit.sh"), state)
	cmd.Env = append(os.Environ(), "ID_AUDIT_ROOT="+root)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("identifier audit failed: %v\n%s", err, output)
	}
	text := string(output)
	for _, want := range []string{"ONLY_IN_SOURCE\n" + sourceID, "ONLY_IN_STATE\n" + stateID, "DIFF_COUNT=2"} {
		if !strings.Contains(text, want) {
			t.Fatalf("identifier audit output missing %q:\n%s", want, output)
		}
	}
}
