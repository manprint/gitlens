package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/check"
	"github.com/manprint/pglens/internal/pgtype"
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
	port := addr
	if i := lastColon(addr); i >= 0 {
		port = addr[i+1:]
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%s/healthz", port))
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	// /healthz itself reports 503 for revoked/unauthorized/incompatible —
	// that is the agent correctly staying alive and saying so, not a reason
	// to have Docker restart it, so any response at all is a pass here.
	return 0
}

func lastColon(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ':' {
			return i
		}
	}
	return -1
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

	// Get extensions
	extensions := getExtensions(ctx, conn)

	// Get databases
	databases, err := getCheckDatabases(ctx, conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: query databases: %v\n", err)
		os.Exit(1)
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
	fmt.Printf("  extensions       : ")
	extStr := formatExtensions(extensions)
	fmt.Printf("%s\n", extStr)

	// Count databases
	monitored := 0
	unmonitored := 0
	reasons := make(map[string]int)
	for _, db := range databases {
		if db.Monitored {
			monitored++
		} else {
			unmonitored++
			if db.SkipReason != "" {
				reasons[db.SkipReason]++
			}
		}
	}
	fmt.Printf("  databases        : %d found, %d monitored", len(databases), monitored)
	if unmonitored > 0 {
		reasonStr := formatSkipReasons(reasons)
		fmt.Printf(" (%d skipped: %s)", unmonitored, reasonStr)
	}
	fmt.Printf("\n\n")

	// Print checks status
	fmt.Printf("checks\n")
	allChecks := check.All()
	mandatoryEnabled := 0
	mandatoryCount := 0

	for _, c := range allChecks {
		req := c.Requires()
		supported, reason := req.Supports(role, pgVersion, tier, extensions)

		var status string
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
				if isMandatory(c.Name()) {
					mandatoryCount++
					mandatoryEnabled++
				}
			}
		} else {
			status = "enabled"
			if isMandatory(c.Name()) {
				mandatoryCount++
				mandatoryEnabled++
			}
		}

		fmt.Printf("  %-18s %s\n", c.Name(), status)
	}

	// Print warnings
	warnings := getCheckWarnings(identityPath)
	if len(warnings) > 0 {
		fmt.Printf("\nwarnings\n")
		for _, w := range warnings {
			fmt.Printf("  - %s\n", w)
		}
	}

	// Determine exit code: 0 if all mandatory checks are enabled, 1 otherwise
	exitCode := 0
	if mandatoryCount > 0 && mandatoryEnabled < mandatoryCount {
		exitCode = 1
	}

	os.Exit(exitCode)
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

// getExtensions queries the database for installed extensions.
func getExtensions(ctx context.Context, conn *pgx.Conn) map[string]bool {
	exts := make(map[string]bool)

	rows, err := conn.Query(ctx, "SELECT extname FROM pg_extension WHERE extname IN ('pg_stat_statements', 'pg_buffercache')")
	if err != nil {
		exts["pg_stat_statements"] = false
		exts["pg_buffercache"] = false
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

	// Return status for both extensions (installed or not)
	for _, name := range []string{"pg_stat_statements", "pg_buffercache"} {
		exts[name] = installedExts[name]
	}

	return exts
}

// Database is a simple database info struct.
type Database struct {
	Name       string
	Monitored  bool
	SkipReason string
}

// getCheckDatabases retrieves the list of databases that would be monitored.
func getCheckDatabases(ctx context.Context, conn *pgx.Conn) ([]Database, error) {
	// For now, just query the basic databases list
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

	var databases []Database
	for rows.Next() {
		var name string
		var xactCommit int64
		if err := rows.Scan(&name, &xactCommit); err != nil {
			continue
		}
		databases = append(databases, Database{Name: name, Monitored: true})
	}

	return databases, nil
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

// isMountWarning checks if a path is on the root mount (not a separate persistent mount).
// For testing, mountinfoPath can be overridden.
func isMountWarning(path string, mountinfoPath string) (bool, error) {
	data, err := os.ReadFile(mountinfoPath)
	if err != nil {
		return false, err
	}

	lines := strings.Split(string(data), "\n")

	// Parse mountinfo to find if path is on a separate mount
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) < 5 {
			continue
		}
		mountpoint := parts[4]
		// Check if the identity path starts with a non-root mountpoint
		if mountpoint != "/" && strings.HasPrefix(filepath.Clean(path), filepath.Clean(mountpoint)) {
			return false, nil
		}
	}

	// If we get here, the path is on the root mount
	return true, nil
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
	for _, name := range []string{"pg_stat_statements", "pg_buffercache"} {
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

func formatSkipReasons(reasons map[string]int) string {
	var parts []string
	for reason, count := range reasons {
		parts = append(parts, fmt.Sprintf("%d %s", count, reason))
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
