package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/internal/check"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/wire"
)

var version = "dev" // overridden at build time with -ldflags

func main() {
	// No subcommand at all (the container's bare ENTRYPOINT) means "run the
	// agent" — this is the default, not an error, since that is exactly how
	// the compose/systemd deployment invokes this binary.
	if len(os.Args) < 2 {
		runAgentCommand(nil)
		return
	}

	command := os.Args[1]

	switch command {
	case "-version", "--version":
		fmt.Println(version)
	case "--healthcheck":
		os.Exit(runAgentHealthcheck())
	case "run":
		runAgentCommand(os.Args[2:])
	case "check":
		runCheckCommand(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", command)
		showUsage()
		os.Exit(1)
	}
}

func showUsage() {
	fmt.Fprintf(os.Stderr, "pglens-agent: agent for PostgreSQL monitoring\n")
	fmt.Fprintf(os.Stderr, "usage: pglens-agent [run --config <path>] | [check --dsn <dsn> [--identity-path <path>]] | [--version] | [--healthcheck]\n")
}

// runAgentHealthcheck is invoked as `pglens-agent --healthcheck` from the
// container's own HEALTHCHECK directive — the distroless image has no shell
// or curl to do this any other way.
func runAgentHealthcheck() int {
	addr := envOr("PGLENS_HEALTHZ_LISTEN", ":9187")
	client := &http.Client{Timeout: 3 * time.Second}
	url := healthcheckURL(addr, "/healthz")
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	// /healthz answers 503 for revoked, unauthorized or incompatible, and the
	// agent is not shipping data in any of them. The probe must fail so that
	// Docker shows the container unhealthy and the body says which state it is
	// in. Docker does not restart a container for being unhealthy, so the agent
	// stays up and keeps retrying, as it did before.
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		fmt.Fprintf(os.Stderr, "healthcheck: %s: %s %s\n", url, resp.Status, body)
		return 1
	}
	return 0
}

// healthcheckURL is the URL the --healthcheck probe requests for path on the
// address the process listens on. A specific host is probed as it is, because
// a bind to one address does not accept on loopback. Only an unspecified host
// (empty, 0.0.0.0 or ::) falls back to 127.0.0.1, which such a bind accepts on.
func healthcheckURL(listen, path string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		// A bare port has no host part to keep.
		host, port = "", listen
	}
	if host == "" || net.ParseIP(host).IsUnspecified() {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + path
}

func runCheckCommand(args []string) {
	fs := flag.NewFlagSet("pglens-agent check", flag.ExitOnError)
	dsn := fs.String("dsn", "", "PostgreSQL DSN to check")
	identityPath := fs.String("identity-path", "/var/lib/pglens/identity.json", "path to identity file")

	_ = fs.Parse(args) // flag.ExitOnError: Parse never returns on failure

	if *dsn == "" {
		fmt.Fprintf(os.Stderr, "error: --dsn is required\n")
		fs.Usage()
		os.Exit(1)
	}

	runCheck(*dsn, *identityPath)
}

// runCheck connects to the database and prints a diagnostic report.
func runCheck(dsn string, identityPath string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Connect to the database
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: parse DSN: %v\n", err)
		os.Exit(1)
	}
	cfg.RuntimeParams["application_name"] = "pglens/check"

	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: connect to database: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = conn.Close(ctx) }()
	// The report only reads, but this connection is opened outside the
	// Manager, so it gets the same read-only session the agent's own do.
	if err := agent.ReadOnlySession(ctx, conn); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Get server version
	var serverVersionNum int
	if err := conn.QueryRow(ctx, "SELECT current_setting('server_version_num')::int").Scan(&serverVersionNum); err != nil {
		fmt.Fprintf(os.Stderr, "error: query server_version_num: %v\n", err)
		os.Exit(1)
	}
	pgVersion := pgtype.PGVersion(serverVersionNum)

	// Get role
	var inRecovery bool
	if err := conn.QueryRow(ctx, "SELECT pg_is_in_recovery()").Scan(&inRecovery); err != nil {
		fmt.Fprintf(os.Stderr, "error: query pg_is_in_recovery: %v\n", err)
		os.Exit(1)
	}
	role := pgtype.RoleFromRecovery(inRecovery)

	// Get system_identifier
	var systemID string
	if err := conn.QueryRow(ctx, "SELECT CAST(system_identifier AS text) FROM pg_control_system()").Scan(&systemID); err != nil {
		systemID = "N/A (requires pg_control_system privilege)"
	}

	// Determine permission tier
	tier := determinePermTier(ctx, conn)
	posture, postureErr := check.ReadRolePosture(ctx, conn)

	// Get extensions
	extensions := getExtensions(ctx, conn)

	// Get databases. The check has no target configuration, so it applies the
	// agent's default selection: the default excludes and a budget of 10.
	candidates, err := getCheckDatabases(ctx, conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: query databases: %v\n", err)
		os.Exit(1)
	}
	databases := agent.SelectDatabases(candidates, agent.DefaultConnOptions())
	// As the agent does when it schedules its checks, a monitored database's
	// pg_stat_statements view stands in for a maintenance database without one.
	statementsVia := ""
	if !extensions["pg_stat_statements"] {
		if statementsVia = locateStatementsView(ctx, cfg, databases); statementsVia != "" {
			extensions["pg_stat_statements"] = true
		}
	}

	// Print the report
	fmt.Printf("pglens agent check\n")
	fmt.Printf("  server version   : %s  (server_version_num %d)", pgVersion.String(), serverVersionNum)
	if pgVersion.Supported() {
		fmt.Printf(" — supported\n")
	} else {
		fmt.Printf(" — NOT SUPPORTED\n")
	}
	fmt.Printf("  role             : %s\n", role)
	fmt.Printf("  system_identifier: %s\n", systemID)
	fmt.Printf("  permission tier  : %s\n", formatPermTier(tier))
	fmt.Printf("  monitoring role  : %s\n", formatRolePosture(posture, postureErr))
	fmt.Printf("  extensions       : ")
	extStr := formatExtensions(extensions)
	if statementsVia != "" {
		extStr += fmt.Sprintf(" (in database %s)", statementsVia)
	}
	fmt.Printf("%s\n", extStr)

	fmt.Printf("  databases        : %s\n\n", formatDatabaseSummary(len(candidates), databases))

	// Print checks status
	fmt.Printf("checks\n")
	mandatoryCount, mandatoryEnabled := printChecks(os.Stdout, check.All(), role, pgVersion, tier, extensions)

	// Print warnings
	warnings := getCheckWarnings(identityPath)
	if postureErr == nil && !strings.EqualFold(posture.ReadOnlyDefault, "on") {
		warnings = append(warnings, fmt.Sprintf("default_transaction_read_only is %q for role %s: pglens sets it in every session, but the role default is the second safeguard (run deploy/sql/monitoring_user.sql)", posture.ReadOnlyDefault, posture.Role))
	}
	if len(warnings) > 0 {
		fmt.Printf("\nwarnings\n")
		for _, w := range warnings {
			fmt.Printf("  - %s\n", w)
		}
	}

	os.Exit(checkExitCode(mandatoryCount, mandatoryEnabled, pgVersion, postureErr, posture))
}

// printChecks prints the status of every check for this server and returns how
// many mandatory checks there are and how many of them are enabled.
func printChecks(w io.Writer, checks []check.Check, role pgtype.Role, pgVersion pgtype.PGVersion, tier pgtype.PermTier, extensions map[string]bool) (mandatoryCount, mandatoryEnabled int) {
	for _, c := range checks {
		req := c.Requires()
		supported, reason := req.Supports(role, pgVersion, tier, extensions)

		var status string
		enabled := false
		if !supported {
			status = fmt.Sprintf("disabled  — %s", reason)
		} else if len(req.Roles) > 0 {
			// Check if this requires a specific role
			roleAllowed := false
			for _, allowedRole := range req.Roles {
				if allowedRole == role {
					roleAllowed = true
					break
				}
			}
			if !roleAllowed {
				status = "disabled  — requires role=primary or standby with a upstream"
			} else {
				status = "enabled"
				enabled = true
			}
		} else {
			status = "enabled"
			enabled = true
		}
		// A mandatory check counts whether or not it is enabled. Counting only
		// the enabled ones made the exit code unable to see a disabled check.
		if isMandatory(c.Name()) {
			mandatoryCount++
			if enabled {
				mandatoryEnabled++
			}
		}

		_, _ = fmt.Fprintf(w, "  %-18s %s\n", c.Name(), status)
	}
	return mandatoryCount, mandatoryEnabled
}

// checkExitCode is the exit status of `pglens-agent check`, as the README
// documents it: 0 when every mandatory check is enabled, the server version is
// supported and the monitoring role cannot write, 1 otherwise. The report says
// NOT SUPPORTED for a version outside 15-19, so the status must not be 0 for it.
func checkExitCode(mandatoryCount, mandatoryEnabled int, pgVersion pgtype.PGVersion, postureErr error, posture check.RolePosture) int {
	if mandatoryEnabled < mandatoryCount || !pgVersion.Supported() {
		return 1
	}
	if postureErr != nil || len(posture.Problems) > 0 {
		return 1
	}
	return 0
}

// formatRolePosture reports whether the role the DSN logs in as could write.
func formatRolePosture(posture check.RolePosture, err error) string {
	if err != nil {
		return fmt.Sprintf("unknown (%v)", err)
	}
	if len(posture.Problems) == 0 {
		return posture.Role + " — read-only"
	}
	return fmt.Sprintf("%s — COULD WRITE: %s (run deploy/sql/monitoring_user.sql for the fix)", posture.Role, strings.Join(posture.Problems, "; "))
}

// determinePermTier queries the database to determine the permission tier.
func determinePermTier(ctx context.Context, conn *pgx.Conn) pgtype.PermTier {
	tier := pgtype.TierReadOnly

	// Check for pg_read_all_data (T1)
	var hasReadAllData bool
	err := conn.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM pg_auth_members m
			JOIN pg_roles r ON m.roleid = r.oid
			WHERE r.rolname = 'pg_read_all_data'
			AND m.member = current_user::regrole::oid
		)
	`).Scan(&hasReadAllData)
	if err == nil && hasReadAllData {
		tier = pgtype.TierExplain
	}

	// Check for pg_signal_backend (T2)
	var hasSignalBackend bool
	err = conn.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM pg_auth_members m
			JOIN pg_roles r ON m.roleid = r.oid
			WHERE r.rolname = 'pg_signal_backend'
			AND m.member = current_user::regrole::oid
		)
	`).Scan(&hasSignalBackend)
	if err == nil && hasSignalBackend {
		tier = pgtype.TierSignal
	}

	return tier
}

// checkExtensions are the extensions the report lists. pg_buffercache used
// to be listed too, although nothing in pglens reads it.
var checkExtensions = []string{"pg_stat_statements"}

// getExtensions queries the database for installed extensions.
func getExtensions(ctx context.Context, conn *pgx.Conn) map[string]bool {
	exts := make(map[string]bool)

	rows, err := conn.Query(ctx, "SELECT extname FROM pg_extension WHERE extname = ANY($1)", checkExtensions)
	if err != nil {
		for _, name := range checkExtensions {
			exts[name] = false
		}
		return exts
	}
	defer rows.Close()

	installedExts := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		installedExts[name] = true
	}

	for _, name := range checkExtensions {
		exts[name] = installedExts[name]
	}

	return exts
}

// getCheckDatabases lists the connectable databases, as Manager.Discover
// does, for agent.SelectDatabases to choose from. Every one used to be
// reported as monitored, whatever the budget or the excludes said.
func getCheckDatabases(ctx context.Context, conn *pgx.Conn) ([]agent.DatabaseCandidate, error) {
	rows, err := conn.Query(ctx, `
		SELECT d.datname, COALESCE(s.xact_commit, 0) AS xact_commit
		FROM pg_database d
		LEFT JOIN pg_stat_database s ON s.datname = d.datname
		WHERE d.datallowconn AND NOT d.datistemplate
		ORDER BY 2 DESC, 1 ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candidates []agent.DatabaseCandidate
	for rows.Next() {
		var c agent.DatabaseCandidate
		if err := rows.Scan(&c.Name, &c.XactCommit); err != nil {
			return nil, err
		}
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

// locateStatementsView returns the first monitored database that has the
// pg_stat_statements view, or "" when none has it.
func locateStatementsView(ctx context.Context, cfg *pgx.ConnConfig, databases []wire.Database) string {
	for _, db := range databases {
		if !db.Monitored {
			continue
		}
		dbCfg := cfg.Copy()
		dbCfg.Database = db.Name
		conn, err := pgx.ConnectConfig(ctx, dbCfg)
		if err != nil {
			continue
		}
		installed := false
		if agent.ReadOnlySession(ctx, conn) == nil {
			_ = conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_stat_statements')`).Scan(&installed)
		}
		_ = conn.Close(ctx)
		if installed {
			return db.Name
		}
	}
	return ""
}

// getCheckWarnings generates warnings for the diagnostics.
func getCheckWarnings(identityPath string) []string {
	var warnings []string

	// Check if identity path is on a persistent mount
	warning := checkMountWarning(identityPath)
	if warning != "" {
		warnings = append(warnings, warning)
	}

	return warnings
}

// checkMountWarning checks if the identity directory is on a persistent mount.
func checkMountWarning(identityPath string) string {
	mounted, err := isMountWarning(identityPath, "/proc/self/mountinfo")
	if err != nil {
		return ""
	}
	if mounted {
		return fmt.Sprintf("identity path %s is not on a persistent mount:\n        instance ids will change on restart and instances will be duplicated", identityPath)
	}
	return ""
}

// isMountWarning reports whether path is not on a persistent mount. The mount
// that holds path is the one with the longest mountpoint containing it: a
// volume under the root holds its own paths, and a tmpfs nested in a volume
// holds the paths under it. For testing, mountinfoPath can be overridden.
func isMountWarning(path string, mountinfoPath string) (bool, error) {
	data, err := os.ReadFile(mountinfoPath)
	if err != nil {
		return false, err
	}

	path = filepath.Clean(path)
	holder := -1 // length of the mountpoint of the mount holding path so far
	persistent := false
	for _, line := range strings.Split(string(data), "\n") {
		mountpoint, fstype, ok := parseMountinfoLine(line)
		if !ok || !mountContains(mountpoint, path) {
			continue
		}
		// mountinfo lists stacked mounts in mount order, so the last one at a
		// mountpoint is the one on top, and the one that holds path.
		if len(mountpoint) >= holder {
			holder = len(mountpoint)
			persistent = !volatileFSTypes[fstype]
		}
	}
	// No mount containing path means nothing shows it is persistent.
	return !persistent, nil
}

// volatileFSTypes are the filesystems that do not keep their contents across a
// restart: memory filesystems, the container's overlay root, and the kernel's
// initial rootfs, which is a ramfs.
var volatileFSTypes = map[string]bool{"tmpfs": true, "ramfs": true, "rootfs": true, "overlay": true}

// mountinfoEscapes undoes the octal escapes mountinfo uses for the characters
// that would otherwise break its whitespace-separated fields.
var mountinfoEscapes = strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)

// parseMountinfoLine returns the mountpoint (field 5) and the filesystem type of
// one line of /proc/self/mountinfo. The optional fields before the "-" separator
// vary in number, so the type is found after the separator, not by its index.
func parseMountinfoLine(line string) (mountpoint, fstype string, ok bool) {
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return "", "", false
	}
	for i := 6; i+1 < len(fields); i++ {
		if fields[i] == "-" {
			return mountinfoEscapes.Replace(fields[4]), fields[i+1], true
		}
	}
	return "", "", false
}

// mountContains reports whether path is mountpoint or lies below it. It compares
// whole path components, so /var/lib/pg does not contain /var/lib/pglens.
func mountContains(mountpoint, path string) bool {
	if mountpoint == "/" {
		return true
	}
	return path == mountpoint || strings.HasPrefix(path, mountpoint+"/")
}

// Helper functions

func formatPermTier(tier pgtype.PermTier) string {
	switch tier {
	case pgtype.TierReadOnly:
		return "T0 (pg_monitor)"
	case pgtype.TierExplain:
		return "T1 (pg_monitor + pg_read_all_data)"
	case pgtype.TierSignal:
		return "T2 (pg_monitor + pg_read_all_data + pg_signal_backend)"
	default:
		return "unknown"
	}
}

func formatExtensions(exts map[string]bool) string {
	var parts []string
	for _, name := range checkExtensions {
		if installed, ok := exts[name]; ok {
			status := "no"
			if installed {
				status = "yes"
			}
			parts = append(parts, fmt.Sprintf("%s=%s", name, status))
		}
	}
	return strings.Join(parts, "  ")
}

// formatDatabaseSummary describes the selection: found counts every
// connectable database, and the ones missing from databases were excluded.
func formatDatabaseSummary(found int, databases []wire.Database) string {
	monitored := 0
	reasons := make(map[string]int)
	for _, db := range databases {
		if db.Monitored {
			monitored++
		} else {
			reasons[db.SkipReason]++
		}
	}
	summary := fmt.Sprintf("%d found, %d monitored", found, monitored)
	var parts []string
	if excluded := found - len(databases); excluded > 0 {
		parts = append(parts, fmt.Sprintf("%d excluded", excluded))
	}
	if skipped := len(databases) - monitored; skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped: %s", skipped, formatSkipReasons(reasons)))
	}
	if len(parts) > 0 {
		summary += " (" + strings.Join(parts, "; ") + ")"
	}
	return summary + ", default selection"
}

func formatSkipReasons(reasons map[string]int) string {
	names := make([]string, 0, len(reasons))
	for reason := range reasons {
		names = append(names, reason)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, reason := range names {
		parts = append(parts, fmt.Sprintf("%d %s", reasons[reason], reason))
	}
	return strings.Join(parts, ", ")
}

func isMandatory(checkName string) bool {
	// For now, consider instance_info, activity, database_stats as mandatory
	// All other checks can be optional based on permissions
	switch checkName {
	case "instance_info", "activity", "database_stats":
		return true
	default:
		return false
	}
}
