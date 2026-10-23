package deploy

import (
	"bytes"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/agent"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// composeFile is the part of a compose file these tests read.
type composeFile struct {
	Services map[string]struct {
		Image       string            `yaml:"image"`
		Environment map[string]string `yaml:"environment"`
		Ports       []string          `yaml:"ports"`
		Volumes     []string          `yaml:"volumes"`
		ReadOnly    bool              `yaml:"read_only"`
		Tmpfs       []string          `yaml:"tmpfs"`
		CapDrop     []string          `yaml:"cap_drop"`
		SecurityOpt []string          `yaml:"security_opt"`
	} `yaml:"services"`
}

func readFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(".", name))
	require.NoError(t, err)
	return data
}

// The example agent must not monitor the cluster as its superuser. It
// connects as pglens_monitor, which the example stack creates on first boot
// by running the shipped monitoring script, and takes the password from
// PGPASSWORD so the example file holds no secret.
func TestExampleStack_AgentConnectsAsMonitoringRole(t *testing.T) {
	var agent struct {
		Targets []struct {
			DSN string `yaml:"dsn"`
		} `yaml:"targets"`
	}
	require.NoError(t, yaml.Unmarshal(readFile(t, "agent.example.yaml"), &agent))
	require.NotEmpty(t, agent.Targets)
	for _, target := range agent.Targets {
		dsn, err := url.Parse(target.DSN)
		require.NoError(t, err)
		require.Equal(t, "pglens_monitor", dsn.User.Username(), target.DSN)
		_, hasPassword := dsn.User.Password()
		require.False(t, hasPassword, "the password must come from PGPASSWORD")
	}

	var stack composeFile
	require.NoError(t, yaml.Unmarshal(readFile(t, "docker-compose.yml"), &stack))
	db := stack.Services["timescaledb"]
	require.Contains(t, db.Volumes, "./sql/monitoring_user.sql:/opt/pglens/monitoring_user.sql:ro")
	require.Contains(t, db.Volumes, "./sql/init_monitoring_user.sh:/docker-entrypoint-initdb.d/01-monitoring.sh:ro")
	require.Contains(t, string(readFile(t, filepath.Join("sql", "init_monitoring_user.sh"))), "role=pglens_monitor")
	require.NotEmpty(t, stack.Services["pglens-agent"].Environment["PGPASSWORD"])
	require.True(t, strings.HasPrefix(stack.Services["pglens-agent"].Environment["PGPASSWORD"], "${PGLENS_MONITORING_PASSWORD"))
}

// The example stacks publish their ports on loopback only, and take the
// superuser password and the bearer token from the environment with no
// default: a published superuser port or a shipped token reachable from the
// network is an open door.
func TestExampleStack_PublishesLoopbackOnlyAndRequiresSecrets(t *testing.T) {
	for _, name := range composeFiles(t) {
		var stack composeFile
		require.NoError(t, yaml.Unmarshal(readFile(t, name), &stack))
		for service, s := range stack.Services {
			for _, port := range s.Ports {
				require.True(t, strings.HasPrefix(port, "127.0.0.1:"), "%s: %s publishes %q on every interface", name, service, port)
			}
			// A superuser password with a default is one that anyone who can
			// reach the published port knows, so every database service of
			// every stack requires one. runsDatabase finds those services by
			// heuristic, so a database it does not recognise needs its own check.
			if runsDatabase(s.Image, s.Environment, s.Volumes) {
				password, ok := s.Environment["POSTGRES_PASSWORD"]
				require.True(t, ok, "%s: %s runs a database without POSTGRES_PASSWORD", name, service)
				require.Contains(t, password, ":?", "%s: %s has a default superuser password", name, service)
			}
		}
	}

	var stack composeFile
	require.NoError(t, yaml.Unmarshal(readFile(t, "docker-compose.yml"), &stack))
	for service, key := range map[string]string{
		"timescaledb":   "POSTGRES_PASSWORD",
		"pglens-server": "PGLENS_BOOTSTRAP_TOKEN",
		"pglens-agent":  "PGLENS_BOOTSTRAP_TOKEN",
	} {
		require.Contains(t, stack.Services[service].Environment[key], ":?", "%s %s has a default", service, key)
	}
}

// runsDatabase is a heuristic for the services that run PostgreSQL or one of
// its derivatives: the image name matches one of the common builds, or the
// service sets a POSTGRES_ variable, or it mounts the data directory.
func runsDatabase(image string, environment map[string]string, volumes []string) bool {
	for _, name := range []string{"postgres", "timescale", "postgis", "pgvector"} {
		if strings.Contains(image, name) {
			return true
		}
	}
	for key := range environment {
		if strings.HasPrefix(key, "POSTGRES_") {
			return true
		}
	}
	for _, volume := range volumes {
		if strings.Contains(volume, "/var/lib/postgresql") {
			return true
		}
	}
	return false
}

// Each signal of runsDatabase must find a database on its own, so a database
// image outside the first list, or one with no PostgreSQL name in its image,
// still requires a superuser password.
func TestExampleStack_RecognisesEveryPostgreSQLDerivative(t *testing.T) {
	for _, image := range []string{
		"timescale/timescaledb:2.31.0-pg17",
		"postgres:17",
		"postgis/postgis:17-3.5",
		"pgvector/pgvector:pg17",
	} {
		require.True(t, runsDatabase(image, nil, nil), image)
	}
	require.True(t, runsDatabase("example/database:1", map[string]string{"POSTGRES_DB": "pglens"}, nil))
	require.True(t, runsDatabase("example/database:1", nil, []string{"data:/var/lib/postgresql/data"}))
	require.False(t, runsDatabase("ghcr.io/manprint/pglens-server:dev", map[string]string{"PGLENS_LISTEN": ":8080"}, []string{"/tmp"}))
}

// The server runs its migrations against PGLENS_DSN on startup. The example
// stack takes that DSN from its own TimescaleDB service and never from the
// host environment, so a PGLENS_DSN exported in the shell that starts the
// stack cannot point the migrations at a monitored database.
func TestExampleStack_ServerStoresIntoItsOwnDatabase(t *testing.T) {
	var stack composeFile
	require.NoError(t, yaml.Unmarshal(readFile(t, "docker-compose.yml"), &stack))
	require.Equal(t,
		"postgres://pglens:${PGLENS_DB_PASSWORD:?set PGLENS_DB_PASSWORD}@timescaledb:5432/pglens?sslmode=disable",
		stack.Services["pglens-server"].Environment["PGLENS_DSN"])
}

// composeReference is a ${...} reference in a compose file. Compose fills it in
// before the server reads its DSN, so the tests put a placeholder in its place.
var composeReference = regexp.MustCompile(`\$\{[^}]*\}`)

// The server writes its migrations and its samples into the database its
// PGLENS_DSN names, so the example agent must not monitor that database: pglens
// never writes to a monitored database. The check takes the database each DSN
// connects to, and every include pattern, so both are held to it.
func TestExampleStack_AgentDoesNotMonitorTheStorageDatabase(t *testing.T) {
	var stack composeFile
	require.NoError(t, yaml.Unmarshal(readFile(t, "docker-compose.yml"), &stack))
	storageDB := effectiveDatabase(t, composeReference.ReplaceAllString(stack.Services["pglens-server"].Environment["PGLENS_DSN"], "placeholder"))

	var example struct {
		Targets []struct {
			Name      string `yaml:"name"`
			DSN       string `yaml:"dsn"`
			Databases struct {
				Include []string `yaml:"include"`
			} `yaml:"databases"`
		} `yaml:"targets"`
	}
	require.NoError(t, yaml.Unmarshal(readFile(t, "agent.example.yaml"), &example))
	require.NotEmpty(t, example.Targets)
	for _, target := range example.Targets {
		ways := storageMonitoring(t, target.DSN, target.Databases.Include, storageDB)
		require.Empty(t, ways, "target %s monitors %s, the database the server writes to: %v", target.Name, storageDB, ways)
	}
}

// effectiveDatabase is the database pgx connects to for dsn. A dbname or
// database query key overrides the path, and a DSN that names no database
// connects to the database named after its user. The PG variables that could
// supply either are cleared for the call, so the test's environment cannot
// change the answer.
func effectiveDatabase(t *testing.T, dsn string) string {
	t.Helper()
	for _, name := range []string{"PGDATABASE", "PGUSER", "PGSERVICE", "PGSERVICEFILE"} {
		t.Setenv(name, "")
	}
	cfg, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	if cfg.Database == "" {
		return cfg.User
	}
	return cfg.Database
}

// storageMonitoring lists the ways a target with this DSN and include list
// monitors storageDB: it connects to it, or the include list selects it. As in
// the agent, an empty include list selects every database, and a pattern
// matches any part of a name. Exclude patterns are not consulted, which can
// only over-report. An empty result means the target does not monitor storageDB.
func storageMonitoring(t *testing.T, dsn string, include []string, storageDB string) []string {
	t.Helper()
	var ways []string
	if effectiveDatabase(t, dsn) == storageDB {
		ways = append(ways, "connects to it")
	}
	if len(include) == 0 {
		ways = append(ways, "has no include list, so it monitors every database")
	}
	for _, pattern := range include {
		matched, err := regexp.MatchString(pattern, storageDB)
		require.NoError(t, err)
		if matched {
			ways = append(ways, "include pattern "+pattern+" matches it")
		}
	}
	return ways
}

// The check is held to each way a target can reach the storage database, so a
// mutation of the example file fails it, and so does a DSN shape it has not
// seen before.
func TestExampleStack_StorageMonitoringFlagsEveryWayToMonitorIt(t *testing.T) {
	const storage = "pglens"
	const example = "postgres://pglens_monitor@timescaledb:5432/postgres?sslmode=disable"
	cases := []struct {
		name    string
		dsn     string
		include []string
		want    bool
	}{
		{"example target", example, []string{"^postgres$"}, false},
		{"include removed", example, nil, true},
		{"include empty", example, []string{}, true},
		{"include names storage", example, []string{"^postgres$", "^pglens$"}, true},
		{"include matches inside a name", example, []string{"glen"}, true},
		{"storage in the DSN path", "postgres://pglens_monitor@timescaledb:5432/pglens?sslmode=disable", []string{"^postgres$"}, true},
		{"storage in dbname", example + "&dbname=pglens", []string{"^postgres$"}, true},
		{"storage in database", example + "&database=pglens", []string{"^postgres$"}, true},
		{"dbname overrides a storage path", "postgres://pglens_monitor@timescaledb:5432/pglens?sslmode=disable&dbname=postgres", []string{"^postgres$"}, false},
		{"no database, user is storage", "postgres://pglens@timescaledb:5432?sslmode=disable", []string{"^postgres$"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ways := storageMonitoring(t, tc.dsn, tc.include, storage)
			require.Equal(t, tc.want, len(ways) > 0, "%v", ways)
		})
	}
}

// composeFiles lists every YAML file under deploy/. A compose file added
// later is held to the same rules as the shipped ones.
func composeFiles(t *testing.T) []string {
	t.Helper()
	var names []string
	require.NoError(t, filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".yml") {
			names = append(names, path)
		}
		return nil
	}))
	require.NotEmpty(t, names)
	return names
}

// A service that starts before the database it needs accepts connections
// exits, so every dependency on a service of the stack waits for that service
// to be healthy. The shipped stack has one such dependency per service; a
// stack that lists them as a bare name starts in the wrong order.
func TestExampleStack_DependenciesWaitForHealthyServices(t *testing.T) {
	type dependsOn struct {
		Services map[string]struct {
			DependsOn yaml.Node `yaml:"depends_on"`
		} `yaml:"services"`
	}
	for _, name := range composeFiles(t) {
		var stack dependsOn
		require.NoError(t, yaml.Unmarshal(readFile(t, name), &stack), name)
		for service, s := range stack.Services {
			unhealthy := unhealthyDependencies(t, s.DependsOn)
			require.Empty(t, unhealthy, "%s: %s depends on %v without waiting for it to be healthy", name, service, unhealthy)
		}
	}
}

// unhealthyDependencies names the dependencies that do not wait for a healthy
// service. depends_on is a map from service to options, whose condition has
// to be service_healthy, or a bare list of names, which waits for nothing.
func unhealthyDependencies(t *testing.T, depends yaml.Node) []string {
	t.Helper()
	var names []string
	switch depends.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(depends.Content); i += 2 {
			var options struct {
				Condition string `yaml:"condition"`
			}
			require.NoError(t, depends.Content[i+1].Decode(&options))
			if options.Condition != "service_healthy" {
				names = append(names, depends.Content[i].Value)
			}
		}
	case yaml.SequenceNode:
		for _, item := range depends.Content {
			names = append(names, item.Value)
		}
	}
	return names
}

// The rule is checked on fixtures too, so that it is known to flag each shape
// of a dependency that does not wait for a healthy service.
func TestExampleStack_DependencyRuleFlagsEveryUnhealthyWait(t *testing.T) {
	cases := []struct {
		name    string
		compose string
		want    []string
	}{
		{"healthy", "depends_on:\n  db:\n    condition: service_healthy\n", nil},
		{"bare list", "depends_on:\n  - db\n", []string{"db"}},
		{"no condition", "depends_on:\n  db: {}\n", []string{"db"}},
		{"service started", "depends_on:\n  db:\n    condition: service_started\n", []string{"db"}},
		{"no dependency", "image: example\n", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var service struct {
				DependsOn yaml.Node `yaml:"depends_on"`
			}
			require.NoError(t, yaml.Unmarshal([]byte(tc.compose), &service))
			require.Equal(t, tc.want, unhealthyDependencies(t, service.DependsOn))
		})
	}
}

// Two DynamicUser units that name the same state directory share it, and
// systemd re-owns the whole tree for whichever unit starts last. The other
// unit then cannot write its own files, so each unit gets a directory of its
// own, or none when it keeps no state.
func TestSystemdUnits_DoNotShareADynamicStateDirectory(t *testing.T) {
	units, err := filepath.Glob(filepath.Join("systemd", "*.service"))
	require.NoError(t, err)
	require.NotEmpty(t, units)
	owner := map[string]string{}
	for _, unit := range units {
		for _, line := range strings.Split(string(readFile(t, unit)), "\n") {
			value, ok := strings.CutPrefix(line, "StateDirectory=")
			if !ok {
				continue
			}
			for _, dir := range strings.Fields(value) {
				require.NotContains(t, owner, dir, "%s and %s both name the state directory %s", owner[dir], unit, dir)
				owner[dir] = unit
			}
		}
	}
}

// The pglens containers keep their root filesystem read-only and drop every
// capability, and no setuid binary may give one back. Their state is in a
// volume and /tmp is memory, so nothing in them needs more.
func TestExampleStack_PglensContainersDropPrivileges(t *testing.T) {
	var stack composeFile
	require.NoError(t, yaml.Unmarshal(readFile(t, "docker-compose.yml"), &stack))
	for _, service := range []string{"pglens-server", "pglens-agent"} {
		s := stack.Services[service]
		require.True(t, s.ReadOnly, "%s has a writable root filesystem", service)
		require.Contains(t, s.Tmpfs, "/tmp", service)
		require.Equal(t, []string{"ALL"}, s.CapDrop, "%s keeps capabilities", service)
		require.Contains(t, s.SecurityOpt, "no-new-privileges:true", service)
	}
}

// The units give their process no capability, no socket family beyond TCP
// and Unix sockets, and only the standard service system calls. Neither the
// agent nor the server needs the rest, so a compromised process has none of it.
func TestSystemdUnits_DropCapabilitiesSocketFamiliesAndSyscalls(t *testing.T) {
	units, err := filepath.Glob(filepath.Join("systemd", "*.service"))
	require.NoError(t, err)
	require.NotEmpty(t, units)
	for _, unit := range units {
		lines := strings.Split(string(readFile(t, unit)), "\n")
		for _, directive := range []string{
			"CapabilityBoundingSet=",
			"RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6",
			"SystemCallFilter=@system-service",
		} {
			require.Contains(t, lines, directive, unit)
		}
	}
}

// The socket families above leave out AF_NETLINK, which Go uses to resolve a
// named IPv6 zone. Each unit names that exception, so a reader does not take
// the list for a full account of what the process can reach.
func TestSystemdUnits_NameTheNetlinkException(t *testing.T) {
	units, err := filepath.Glob(filepath.Join("systemd", "*.service"))
	require.NoError(t, err)
	require.NotEmpty(t, units)
	for _, unit := range units {
		require.Contains(t, string(readFile(t, unit)), "AF_NETLINK", unit)
	}
}

// The agent reads host metrics through the paths the compose stack mounts the
// host's /proc and /sys on. The example leaves those paths to HOST_PROC and
// HOST_SYS: a path pinned in the file overrides them, and the agent would then
// read the container's own /proc.
func TestExampleStack_AgentReadsTheHostMounts(t *testing.T) {
	var stack composeFile
	require.NoError(t, yaml.Unmarshal(readFile(t, "docker-compose.yml"), &stack))
	agentService := stack.Services["pglens-agent"]
	mounts := map[string]string{}
	for _, volume := range agentService.Volumes {
		if parts := strings.Split(volume, ":"); len(parts) >= 2 {
			mounts[parts[0]] = parts[1]
		}
	}
	for host, key := range map[string]string{"/proc": "HOST_PROC", "/sys": "HOST_SYS"} {
		target, ok := mounts[host]
		require.True(t, ok, "the agent does not mount the host %s", host)
		require.Equal(t, target, agentService.Environment[key], "%s must name the mount of the host %s", key, host)
		t.Setenv(key, agentService.Environment[key])
	}

	var cfg agent.Config
	decoder := yaml.NewDecoder(bytes.NewReader(readFile(t, "agent.example.yaml")))
	decoder.KnownFields(true)
	require.NoError(t, decoder.Decode(&cfg))
	require.Equal(t, mounts["/proc"], cfg.HostProcPath())
	require.Equal(t, mounts["/sys"], cfg.HostSysPath())
}

// The init script runs the monitoring script in every database the cluster
// lists. A listing that fails must stop the run: a loop over nothing exits 0
// and leaves the cluster without the monitoring role, and an unquoted name is
// split at its spaces. The psql on PATH is a stub that answers the listing
// from FAKE_DATABASES, or fails when FAKE_PSQL_FAIL is set, and records the
// database of each run of the monitoring script.
func TestInitMonitoringUser_RunsEveryDatabaseOrFails(t *testing.T) {
	bin := t.TempDir()
	stub := `#!/bin/sh
for arg do
  if [ "$arg" = "-c" ]; then
    if [ -n "$FAKE_PSQL_FAIL" ]; then echo "psql: connection refused" >&2; exit 2; fi
    printf '%s\n' "$FAKE_DATABASES"
    exit 0
  fi
done
prev=
for arg do
  if [ "$prev" = "--dbname" ]; then printf '%s\n' "$arg" >> "$FAKE_PSQL_LOG"; fi
  prev=$arg
done
exit 0
`
	require.NoError(t, os.WriteFile(filepath.Join(bin, "psql"), []byte(stub), 0o755))
	script, err := filepath.Abs(filepath.Join("sql", "init_monitoring_user.sh"))
	require.NoError(t, err)

	run := func(databases string, fail bool) ([]string, string, error) {
		logFile := filepath.Join(t.TempDir(), "runs")
		cmd := exec.Command("sh", script)
		cmd.Env = append(os.Environ(),
			"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
			"POSTGRES_USER=postgres",
			"PGLENS_MONITORING_PASSWORD=secret",
			"FAKE_DATABASES="+databases,
			"FAKE_PSQL_LOG="+logFile,
		)
		if fail {
			cmd.Env = append(cmd.Env, "FAKE_PSQL_FAIL=1")
		}
		out, err := cmd.CombinedOutput()
		data, _ := os.ReadFile(logFile)
		return strings.Split(strings.TrimSpace(string(data)), "\n"), string(out), err
	}

	_, out, err := run("app", true)
	require.Error(t, err, "a failed listing must fail the run: %s", out)

	_, out, err = run("", false)
	require.Error(t, err, "an empty listing must fail the run: %s", out)

	runs, out, err := run("app\nmy app", false)
	require.NoError(t, err, out)
	require.Equal(t, []string{"app", "my app"}, runs, "every database runs once, with its name whole")
}
