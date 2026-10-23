package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeDocker answers the two forms of docker that scripts/e2e_evidence.sh
// runs, ps and network ls, from the "id name" lines in $FAKE_DOCKER_STATE.
// It honours --filter name=... as a substring match and --format with a Name
// field, which is enough to tell the names apart from the ids.
const fakeDocker = `#!/bin/sh
if [ "$1" = network ]; then
	file=$FAKE_DOCKER_STATE/networks
	shift
else
	file=$FAKE_DOCKER_STATE/containers
fi
filter=
name=
while [ $# -gt 0 ]; do
	case $1 in
	--filter) filter=${2#name=}; shift ;;
	--format) case $2 in *Name*) name=1 ;; esac; shift ;;
	esac
	shift
done
while read -r id resource; do
	case $resource in *"$filter"*) ;; *) continue ;; esac
	if [ -n "$name" ]; then echo "$resource"; else echo "$id"; fi
done < "$file"
`

// The residue counts only what the run created. A developer stack or another
// session's pglens resource that exists before the run is not the run's
// leftover, and a container the run started without the pglens name is not
// counted either.
func TestE2EEvidenceScript_CountsOnlyResidueOfTheRun(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the repository E2E evidence harness is Linux-only")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is required")
	}

	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(fakeDocker), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	state := t.TempDir()
	containers := filepath.Join(state, "containers")
	networks := filepath.Join(state, "networks")
	if err := os.WriteFile(containers, []byte("1111 pglens-developer-stack\n"), 0o644); err != nil {
		t.Fatalf("write containers: %v", err)
	}
	if err := os.WriteFile(networks, []byte("aaaa pglens_developer_default\n"), 0o644); err != nil {
		t.Fatalf("write networks: %v", err)
	}

	// The command stands in for the suite: it creates a pglens container and
	// network that stay behind, and an unnamed container that does not count.
	suite := `printf '2222 pglens-run-1\n' >> "$FAKE_DOCKER_STATE/containers"
printf 'bbbb pglens-run-net\n' >> "$FAKE_DOCKER_STATE/networks"
printf '3333 postgres-scratch\n' >> "$FAKE_DOCKER_STATE/containers"`

	artifacts := t.TempDir()
	cmd := exec.Command("bash", filepath.Join("..", "..", "scripts", "e2e_evidence.sh"), "/bin/sh", "-c", suite)
	cmd.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_DOCKER_STATE="+state,
		"E2E_ARTIFACTS_DIR="+artifacts)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("evidence script failed: %v\n%s", err, out)
	}

	logs, err := filepath.Glob(filepath.Join(artifacts, "e2e-full-*.log"))
	if err != nil || len(logs) != 1 {
		t.Fatalf("evidence logs = %v (%v); want exactly one", logs, err)
	}
	data, err := os.ReadFile(logs[0])
	if err != nil {
		t.Fatalf("read evidence log: %v", err)
	}
	for _, want := range []string{"RESIDUAL_CONTAINERS=1\n", "RESIDUAL_NETWORKS=1\n"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("evidence log does not contain %q:\n%s", want, data)
		}
	}
}
