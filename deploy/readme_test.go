package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The README's quick start and "Running the server" sections, and the exec
// command of deploy/sql/README.md, operate the example stacks. A stack refuses
// to start without its secrets, so each documented docker compose command must
// sit in a sh code block that exports every secret it needs. The check runs each
// command through docker compose config in a shell that sees only the variables
// its block exports, each set to a dummy value, and expects the file to
// validate. Compose checks the whole file before it runs any subcommand, so exec
// needs the same secrets as up. A docker compose command outside a sh block
// would escape the check, so the test counts them.
func TestReadme_ComposeCommandsRunWithTheSecretsTheyExport(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is required: compose config checks the documented commands")
	}
	fence := "```"
	shellBlock := regexp.MustCompile("(?s)" + fence + "sh\n(.*?)" + fence)
	// A command may continue on the next line after a trailing backslash.
	continuation := regexp.MustCompile(`\\\n\s*`)
	composeCommand := regexp.MustCompile(`(?m)^docker compose -f (\S+) (\S+)`)
	exported := regexp.MustCompile(`(?m)^export ([A-Z][A-Z0-9_]*)=`)

	var checked []string
	for _, doc := range []string{filepath.Join("..", "README.md"), filepath.Join("sql", "README.md")} {
		text := string(readFile(t, doc))
		found := 0
		for _, block := range shellBlock.FindAllStringSubmatch(text, -1) {
			body := continuation.ReplaceAllString(block[1], " ")
			exports := exported.FindAllStringSubmatch(body, -1)
			for _, command := range composeCommand.FindAllStringSubmatch(body, -1) {
				found++
				env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
				for _, name := range exports {
					env = append(env, name[1]+"=dummy")
				}
				compose := exec.Command("docker", "compose", "-f", command[1], "config", "-q")
				compose.Dir = ".."
				compose.Env = env
				out, err := compose.CombinedOutput()
				require.NoError(t, err, "%s runs docker compose -f %s %s, but its code block does not export every secret the file needs: %s", doc, command[1], command[2], out)
				checked = append(checked, command[1]+" "+command[2])
			}
		}
		// The count is of the text, not of the expression, so a command the
		// expression does not parse fails here instead of dropping out.
		require.Equal(t, strings.Count(text, "docker compose"), found, "%s has a docker compose command outside a sh block, or one the check does not parse", doc)
	}
	// The test names the commands it expects: the quick start and both commands
	// under Running the server, and the exec command of the monitoring role.
	require.ElementsMatch(t, []string{
		"deploy/docker-compose.yml up",
		"deploy/docker-compose.yml up",
		"deploy/compose/timescaledb.yml up",
		"deploy/docker-compose.yml exec",
	}, checked, "the documented compose commands changed: update this list")
}
