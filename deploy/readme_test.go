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
// command through docker compose config with only the variables its block
// exports, each set to a dummy value, and without the .env file beside the
// compose file, and expects the file to validate. Compose checks the whole file
// before it runs any subcommand, so exec needs the same secrets as up. A docker
// compose command outside a sh block would escape the check, so the test counts
// them.
func TestReadme_ComposeCommandsRunWithTheSecretsTheyExport(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is required: compose config checks the documented commands")
	}
	// A missing or broken compose plugin is a tooling problem. Reporting it here
	// keeps it from looking like a README that lacks an export. The version check
	// runs in the environment of the config calls, so a plugin they cannot see is
	// reported the same way.
	version := exec.Command("docker", "compose", "version")
	version.Env = composeEnv(nil)
	if out, err := version.CombinedOutput(); err != nil {
		t.Fatalf("docker compose version failed: this is a tooling failure, not a README one: %v\n%s", err, out)
	}

	var checked []string
	for _, doc := range []string{filepath.Join("..", "README.md"), filepath.Join("sql", "README.md")} {
		text := string(readFile(t, doc))
		calls, total := composeCalls(text)
		for _, call := range calls {
			out, err := composeConfig("..", call.file, call.exports)
			require.NoError(t, err, "%s runs docker compose -f %s %s, but its code block does not export every secret the file needs: %s", doc, call.file, call.subcommand, out)
			checked = append(checked, call.file+" "+call.subcommand)
		}
		// The count is of the text, not of the expression, so a command the
		// expression does not parse fails here instead of dropping out.
		require.Equal(t, total, len(calls), "%s has a docker compose command outside a sh block, or one the check does not parse", doc)
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

// composeCall is one docker compose command that a sh block runs, with the
// variables that block exports.
type composeCall struct {
	file, subcommand string
	exports          []string
}

// composeCalls returns the docker compose commands that the sh blocks of text
// run, and the number of docker compose commands the text holds in all.
func composeCalls(text string) ([]composeCall, int) {
	fence := "```"
	shellBlock := regexp.MustCompile("(?s)" + fence + "sh\n(.*?)" + fence)
	// A command may continue on the next line after a trailing backslash. The
	// space before the backslash goes too, or the subcommand would follow two
	// spaces, which the command pattern does not match.
	continuation := regexp.MustCompile(`[ \t]*\\\n\s*`)
	composeCommand := regexp.MustCompile(`(?m)^docker compose -f (\S+) (\S+)`)
	exported := regexp.MustCompile(`(?m)^export ([A-Z][A-Z0-9_]*)=`)

	var calls []composeCall
	for _, block := range shellBlock.FindAllStringSubmatch(text, -1) {
		body := continuation.ReplaceAllString(block[1], " ")
		var exports []string
		for _, name := range exported.FindAllStringSubmatch(body, -1) {
			exports = append(exports, name[1])
		}
		for _, command := range composeCommand.FindAllStringSubmatch(body, -1) {
			calls = append(calls, composeCall{file: command[1], subcommand: command[2], exports: exports})
		}
	}
	return calls, strings.Count(text, "docker compose")
}

// The check reads a continued command as one command, whether or not a space
// comes before its backslash, and it counts a command outside a sh block
// without running it.
func TestReadme_ComposeCallsReadContinuedLines(t *testing.T) {
	text := "```sh\nexport PGLENS_DB_PASSWORD='x'\ndocker compose -f deploy/docker-compose.yml \\\n  up -d\n```\n\nor docker compose -f deploy/compose/timescaledb.yml up -d\n"
	calls, total := composeCalls(text)
	require.Equal(t, 2, total)
	require.Equal(t, []composeCall{{file: "deploy/docker-compose.yml", subcommand: "up", exports: []string{"PGLENS_DB_PASSWORD"}}}, calls)
}

// composeEnv is the environment of every docker compose call the check makes.
// It holds the variables a block exports and nothing else from the shell, so a
// secret left in the shell cannot satisfy a block. PATH and HOME find docker,
// and DOCKER_CONFIG is passed on when it is set, because a compose plugin can
// live there.
func composeEnv(exports []string) []string {
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
	if dir := os.Getenv("DOCKER_CONFIG"); dir != "" {
		env = append(env, "DOCKER_CONFIG="+dir)
	}
	for _, name := range exports {
		env = append(env, name+"=dummy")
	}
	return env
}

// composeConfig validates the compose file, relative to dir, with the
// environment of composeEnv. --env-file /dev/null keeps compose from reading
// the .env beside the file, which would supply secrets that a block does not
// export.
func composeConfig(dir, file string, exports []string) ([]byte, error) {
	compose := exec.Command("docker", "compose", "-f", file, "--env-file", os.DevNull, "config", "-q")
	compose.Dir = dir
	compose.Env = composeEnv(exports)
	return compose.CombinedOutput()
}

// The project directory of a compose file holds the .env that compose reads on
// its own. The check must not read it, or a block that exports no secret would
// pass because that file holds one.
func TestReadme_ComposeConfigIgnoresTheProjectDotEnv(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is required: compose config reads the file")
	}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("PGLENS_CHECK_SECRET=set\n"), 0o600))
	stack := "name: pglens-readme-check\nservices:\n  app:\n    image: busybox\n    environment:\n      SECRET: ${PGLENS_CHECK_SECRET:?set PGLENS_CHECK_SECRET}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stack.yml"), []byte(stack), 0o600))
	out, err := composeConfig(dir, "stack.yml", nil)
	require.Error(t, err, "the .env beside the file supplied the secret: %s", out)
	require.Contains(t, string(out), "PGLENS_CHECK_SECRET")
}

// A docker without a working compose plugin must fail the check as a tooling
// problem, not as a README one. The test runs the check in a child process whose
// docker is a stub that always fails, and expects the tooling message.
func TestReadme_MissingComposePluginIsATToolingFailure(t *testing.T) {
	stub := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(stub, "docker"), []byte("#!/bin/sh\nexit 1\n"), 0o755))
	child := exec.Command(os.Args[0], "-test.run=^TestReadme_ComposeCommandsRunWithTheSecretsTheyExport$")
	// exec keeps the last value of a duplicated variable, so the stub wins.
	child.Env = append(os.Environ(), "PATH="+stub+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := child.CombinedOutput()
	require.Error(t, err, "the check must fail when docker has no compose plugin: %s", out)
	require.Contains(t, string(out), "tooling failure", "a missing compose plugin must be reported as a tooling failure")
	require.NotContains(t, string(out), "does not export every secret")
}

// The version check and the config calls share one environment. A compose
// plugin that only the test process can see would pass the version check and
// then fail every config call, which would read as a README failure, so the
// check must report it as a tooling failure instead. The stub finds compose only
// when STUB_COMPOSE_PLUGIN is set, the way a plugin found through a variable
// would be, and the checks drop that variable.
func TestReadme_PluginOnlyTheTestSeesIsATToolingFailure(t *testing.T) {
	stub := t.TempDir()
	script := "#!/bin/sh\nif [ -n \"$STUB_COMPOSE_PLUGIN\" ]; then exit 0; fi\necho \"docker: 'compose' is not a docker command.\" >&2\nexit 1\n"
	require.NoError(t, os.WriteFile(filepath.Join(stub, "docker"), []byte(script), 0o755))
	child := exec.Command(os.Args[0], "-test.run=^TestReadme_ComposeCommandsRunWithTheSecretsTheyExport$")
	child.Env = append(os.Environ(), "PATH="+stub+string(os.PathListSeparator)+os.Getenv("PATH"), "STUB_COMPOSE_PLUGIN=1")
	out, err := child.CombinedOutput()
	require.Error(t, err, "the check must fail when its calls cannot see the compose plugin: %s", out)
	require.Contains(t, string(out), "tooling failure", "a plugin the checks cannot see must be reported as a tooling failure")
	require.NotContains(t, string(out), "does not export every secret")
}
