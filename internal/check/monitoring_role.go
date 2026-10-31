package check

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/pgtype"
)

func init() { Register(&monitoringRoleCheck{}) }

// The monitoring_role check reports one fact on every scrape. Its value lists
// what lets the role pglens connects as write, or is monitoringRoleReadOnly
// when nothing does, so the advisor (internal/advisor/rules_security.go) can
// tell a role that was fixed from one no longer observed. A fact needs a
// value: wire.Fact.Validate drops one without.
const (
	monitoringRoleFactKind = "monitoring_role"
	monitoringRoleFactKey  = "self"
	monitoringRoleReadOnly = "read-only"
)

// readOnlyMemberships are the roles deploy/sql/monitoring_user.sql lets the
// monitoring role belong to; pg_database_owner only stands for owning the
// current database, which the ownership count reports. Whether the role is a
// member of pg_monitor and of pg_read_all_stats, directly or through another
// role (a superuser is a member of every role), is read apart: the monitoring
// readiness view warns when either is missing.
const monitoringRoleQuery = `SELECT r.rolname,
       r.rolsuper, r.rolcreatedb, r.rolcreaterole, r.rolreplication, r.rolbypassrls,
       CASE WHEN r.rolsuper THEN '{}'::pg_catalog.text[] ELSE ARRAY(
         SELECT g.rolname::pg_catalog.text
           FROM pg_catalog.pg_roles g
          WHERE g.oid <> r.oid
            AND pg_catalog.pg_has_role(r.oid, g.oid, 'MEMBER')
            AND g.rolname NOT IN ('pg_monitor', 'pg_read_all_settings', 'pg_read_all_stats',
                                  'pg_stat_scan_tables', 'pg_read_all_data', 'pg_signal_backend',
                                  'pg_database_owner')
          ORDER BY 1) END,
       (SELECT pg_catalog.count(*)
          FROM pg_catalog.pg_shdepend d
         WHERE d.refclassid = 'pg_catalog.pg_authid'::pg_catalog.regclass
           AND d.refobjid = r.oid
           AND d.deptype = 'o')::pg_catalog.int8,
       (SELECT s.reset_val FROM pg_catalog.pg_settings s
         WHERE s.name = 'default_transaction_read_only'),
       pg_catalog.pg_has_role(r.oid, 'pg_monitor', 'MEMBER'),
       pg_catalog.pg_has_role(r.oid, 'pg_read_all_stats', 'MEMBER')
  FROM pg_catalog.pg_roles r
 WHERE r.rolname = CURRENT_USER`

// monitoringRoleCheck reports what would let the role pglens connects as
// change data if a session of it left read-only mode. pglens never does,
// but the role script only checks once: a grant made later shows up here.
// Ownership counts objects in every database (pg_shdepend is shared);
// object privileges are per database and not read here.
type monitoringRoleCheck struct{}

func (c *monitoringRoleCheck) Name() string { return "monitoring_role" }
func (c *monitoringRoleCheck) Requires() Requirements {
	return Requirements{Scope: ScopeInstance, PermTier: pgtype.TierReadOnly}
}
func (c *monitoringRoleCheck) DefaultInterval() time.Duration { return 10 * time.Minute }
func (c *monitoringRoleCheck) Timeout() time.Duration         { return 5 * time.Second }

func (c *monitoringRoleCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.Conn(ctx)
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()
	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}
	posture, err := ReadRolePosture(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	value := monitoringRoleReadOnly
	if len(posture.Problems) > 0 {
		value = strings.Join(posture.Problems, "; ")
	}
	return Result{Facts: []Fact{{
		Kind: monitoringRoleFactKind, Key: monitoringRoleFactKey,
		ValueText: value,
		Labels: map[string]string{
			"role": posture.Role, "read_only_default": posture.ReadOnlyDefault,
			"has_pg_monitor":     strconv.FormatBool(posture.HasPgMonitor),
			"has_read_all_stats": strconv.FormatBool(posture.HasReadAllStats),
		},
	}}}, nil
}

// RolePosture is what the role a connection logs in as could do beyond
// reading.
type RolePosture struct {
	Role string
	// Problems lists what would let the role write, in words ("has
	// SUPERUSER", "is a member of pg_write_all_data", "owns 3 object(s)").
	Problems []string
	// ReadOnlyDefault is default_transaction_read_only as the role gets it
	// from its own, its database's and the server's settings: a session's
	// SET does not hide it.
	ReadOnlyDefault string
	// HasPgMonitor and HasReadAllStats say whether the role is a member of
	// pg_monitor and of pg_read_all_stats. Without them it cannot see other
	// users' sessions and statements, nor every setting.
	HasPgMonitor, HasReadAllStats bool
}

// ReadRolePosture reads the RolePosture of the connection's current user.
// The agent's monitoring_role check and pglens-agent check share it.
func ReadRolePosture(ctx context.Context, conn interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}) (RolePosture, error) {
	var (
		posture                                             RolePosture
		super, createDB, createRole, replication, bypassRLS bool
		memberships                                         []string
		owned                                               int64
		readOnlyDefault                                     *string
	)
	if err := conn.QueryRow(ctx, monitoringRoleQuery).Scan(&posture.Role, &super, &createDB, &createRole,
		&replication, &bypassRLS, &memberships, &owned, &readOnlyDefault, &posture.HasPgMonitor, &posture.HasReadAllStats); err != nil {
		return RolePosture{}, fmt.Errorf("read monitoring role: %w", err)
	}
	for _, attr := range []struct {
		set  bool
		name string
	}{
		{super, "SUPERUSER"}, {createDB, "CREATEDB"}, {createRole, "CREATEROLE"},
		{replication, "REPLICATION"}, {bypassRLS, "BYPASSRLS"},
	} {
		if attr.set {
			posture.Problems = append(posture.Problems, "has "+attr.name)
		}
	}
	for _, m := range memberships {
		posture.Problems = append(posture.Problems, "is a member of "+m)
	}
	if owned > 0 {
		posture.Problems = append(posture.Problems, fmt.Sprintf("owns %d object(s)", owned))
	}
	if readOnlyDefault != nil {
		posture.ReadOnlyDefault = *readOnlyDefault
	}
	return posture, nil
}
