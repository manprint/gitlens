package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// The README's quick start and "Running the server" sections start the example
// stacks. A stack refuses to start without its secrets, so each documented
// command must sit in a code block that exports every secret it needs. The
// check runs each command through docker compose config in a shell that sees
// only the variables its block exports, each set to a dummy value, and expects
// the file to validate.
func TestReadme_ComposeCommandsRunWithTheSecretsTheyExport(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is required: compose config checks the documented commands")
	}
	fence := "```"
	shellBlock := regexp.MustCompile("(?s)" + fence + "sh\n(.*?)" + fence)
	upCommand := regexp.MustCompile(`(?m)^docker compose -f (\S+) up -d`)
	exported := regexp.MustCompile(`(?m)^export ([A-Z][A-Z0-9_]*)=`)

	var checked []string
	for _, block := range shellBlock.FindAllStringSubmatch(string(readFile(t, filepath.Join("..", "README.md"))), -1) {
		exports := exported.FindAllStringSubmatch(block[1], -1)
		for _, command := range upCommand.FindAllStringSubmatch(block[1], -1) {
			env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
			for _, name := range exports {
				env = append(env, name[1]+"=dummy")
			}
			compose := exec.Command("docker", "compose", "-f", command[1], "config", "-q")
			compose.Dir = ".."
			compose.Env = env
			out, err := compose.CombinedOutput()
			require.NoError(t, err, "README runs %s, but its code block does not export every secret the file needs: %s", command[1], out)
			checked = append(checked, command[1])
		}
	}
	// A command that the expression stops matching, for example one reflowed
	// over several lines, would drop out of the check without a sign, so the
	// test names the commands it expects: the quick start and both commands
	// under Running the server.
	require.ElementsMatch(t, []string{
		"deploy/docker-compose.yml",
		"deploy/docker-compose.yml",
		"deploy/compose/timescaledb.yml",
	}, checked, "the README's compose commands changed: update this list")
}
