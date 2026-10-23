package scripts_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// floorKey matches one internal package in the FLOORS table of
// scripts/coverage_gate.sh. The test reads the table from the script, so it
// cannot drift from the floors the gate enforces.
var floorKey = regexp.MustCompile(`\["\$MOD/(internal/[^"]+)"\]=\d+`)

// A package with a floor that has no statements in the profile is a package
// the gate no longer measures, so the gate must fail instead of skipping it.
func TestCoverageGateScript_FailsWhenAFloorLosesItsPackage(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is required")
	}
	script := filepath.Join("..", "..", "scripts", "coverage_gate.sh")
	table, err := os.ReadFile(script)
	if err != nil {
		t.Fatalf("read coverage gate: %v", err)
	}
	var packages []string
	for _, m := range floorKey.FindAllSubmatch(table, -1) {
		packages = append(packages, string(m[1]))
	}
	if len(packages) == 0 {
		t.Fatal("no floors found in scripts/coverage_gate.sh")
	}

	// profileWithout writes one fully covered block per floored package,
	// except omit, so the gate's global and per-package figures are 100%.
	profileWithout := func(omit string) string {
		var b strings.Builder
		b.WriteString("mode: atomic\n")
		for _, pkg := range packages {
			if pkg != omit {
				fmt.Fprintf(&b, "github.com/manprint/pglens/%s/file.go:1.1,2.2 1 1\n", pkg)
			}
		}
		return b.String()
	}
	run := func(profile string) (string, error) {
		path := filepath.Join(t.TempDir(), "coverage.out")
		if err := os.WriteFile(path, []byte(profile), 0o644); err != nil {
			t.Fatalf("write profile: %v", err)
		}
		out, err := exec.Command("bash", script, path).CombinedOutput()
		return string(out), err
	}

	out, err := run(profileWithout(""))
	if err != nil {
		t.Fatalf("gate failed with every floored package present: %v\n%s", err, out)
	}

	const lost = "internal/topology"
	out, err = run(profileWithout(lost))
	if err == nil {
		t.Fatalf("gate succeeded without %s in the profile; want failure\n%s", lost, out)
	}
	if !strings.Contains(out, "github.com/manprint/pglens/"+lost) || !strings.Contains(out, "FAIL") {
		t.Fatalf("gate output does not name %s as failing:\n%s", lost, out)
	}
}
