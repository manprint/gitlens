package deploy

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// workflow is the part of a GitHub Actions workflow these tests read.
type workflow struct {
	Permissions map[string]string `yaml:"permissions"`
	Env         map[string]string `yaml:"env"`
	Jobs        map[string]struct {
		Permissions map[string]string `yaml:"permissions"`
		Uses        string            `yaml:"uses"`
		Steps       []struct {
			Uses string `yaml:"uses"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func workflows(t *testing.T) map[string]workflow {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", ".github", "workflows", "*.yml"))
	require.NoError(t, err)
	require.NotEmpty(t, files)
	out := make(map[string]workflow, len(files))
	for _, file := range files {
		data, err := os.ReadFile(file)
		require.NoError(t, err)
		var w workflow
		require.NoError(t, yaml.Unmarshal(data, &w), file)
		out[filepath.Base(file)] = w
	}
	return out
}

// A tag can be moved to another commit, so a workflow that names one runs
// whatever the tag points at when it starts, with the token of its job. Every
// action from outside this repository is pinned to a full commit SHA; the
// version stays in a trailing comment.
var commitPinned = regexp.MustCompile(`@[0-9a-f]{40}$`)

func TestWorkflows_RemoteActionsArePinnedToCommits(t *testing.T) {
	for name, w := range workflows(t) {
		for jobName, job := range w.Jobs {
			uses := []string{job.Uses}
			for _, step := range job.Steps {
				uses = append(uses, step.Uses)
			}
			for _, ref := range uses {
				if ref == "" || strings.HasPrefix(ref, "./") {
					continue
				}
				require.Regexp(t, commitPinned, ref, "%s: job %s runs %s, which is not pinned to a commit", name, jobName, ref)
			}
		}
	}
}

// A write scope granted to every job lets any job write, and a job that runs
// third-party actions with contents: write could be used to change a release.
// The job that holds contents: write runs no action at all.
func TestWorkflows_WriteScopeStaysWithAJobThatRunsNoAction(t *testing.T) {
	for name, w := range workflows(t) {
		for scope, level := range w.Permissions {
			require.NotEqual(t, "write", level, "%s grants %s: write to every job", name, scope)
		}
		for jobName, job := range w.Jobs {
			if job.Permissions["contents"] != "write" {
				continue
			}
			require.Empty(t, job.Uses, "%s: job %s holds contents: write and calls a workflow", name, jobName)
			for _, step := range job.Steps {
				require.Empty(t, step.Uses, "%s: job %s holds contents: write and runs %s", name, jobName, step.Uses)
			}
		}
	}
}

// The security gate reads the output and exit status of govulncheck, so the
// scanner has to change by a commit, not by a release. An install at @latest
// changes with every release. One version is pinned in the Makefile, the
// Makefile's hint names it, and ci.yml installs the same one.
func TestWorkflows_GovulncheckIsPinnedToTheMakefileVersion(t *testing.T) {
	makefile := string(readFile(t, filepath.Join("..", "Makefile")))
	pin := regexp.MustCompile(`(?m)^GOVULNCHECK_VERSION \?= (v\d+\.\d+\.\d+)$`).FindStringSubmatch(makefile)
	require.NotNil(t, pin, "Makefile must pin GOVULNCHECK_VERSION to a release")
	require.Equal(t, pin[1], workflows(t)["ci.yml"].Env["GOVULNCHECK_VERSION"], "ci.yml must install the Makefile's govulncheck")
	ci := string(readFile(t, filepath.Join("..", ".github", "workflows", "ci.yml")))
	require.Contains(t, ci, "govulncheck@${{ env.GOVULNCHECK_VERSION }}")
	require.NotContains(t, ci, "@latest")
	require.NotContains(t, makefile, "@latest")
}
