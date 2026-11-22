package pgtype

import "fmt"

// Role is the replication role of a PostgreSQL instance.
type Role string

const (
	RolePrimary Role = "primary"
	RoleStandby Role = "standby"
	RoleUnknown Role = "unknown"
)

// RoleFromRecovery maps pg_is_in_recovery() to a Role.
func RoleFromRecovery(inRecovery bool) Role {
	if inRecovery {
		return RoleStandby
	}
	return RolePrimary
}

// ReadAllDataStandIn is the group role deploy/sql/monitoring_user.sql creates
// on PostgreSQL 13, which has no pg_read_all_data: USAGE on every schema and
// SELECT on every table and sequence, for objects that exist when the script
// runs and, through default privileges, for those their owners create later.
const ReadAllDataStandIn = "pglens_read_all_data"

// ReadAllDataRoleSQL is the oid of the tier-1 role: pg_read_all_data where the
// server has it (PostgreSQL 14 and later), else ReadAllDataStandIn, else NULL.
// pg_has_role on a NULL oid is NULL, so wrap it in COALESCE(..., false).
const ReadAllDataRoleSQL = `COALESCE(pg_catalog.to_regrole('pg_read_all_data'), pg_catalog.to_regrole('` + ReadAllDataStandIn + `'))`

// PermTier is the permission tier required to run a check.
type PermTier int

const (
	TierReadOnly  PermTier = iota // T0: pg_monitor + pg_control_system()
	TierExplain                   // T1: + pg_read_all_data (ReadAllDataStandIn on PostgreSQL 13)
	TierSignal                    // T2: + pg_signal_backend
	TierExtension                 // T3: + CREATE EXTENSION
)

// String returns T0..T3.
func (t PermTier) String() string {
	switch t {
	case TierReadOnly:
		return "T0"
	case TierExplain:
		return "T1"
	case TierSignal:
		return "T2"
	case TierExtension:
		return "T3"
	default:
		return fmt.Sprintf("Tier%d", int(t))
	}
}

// ParsePermTier parses T0..T3.
func ParsePermTier(s string) (PermTier, error) {
	switch s {
	case "T0":
		return TierReadOnly, nil
	case "T1":
		return TierExplain, nil
	case "T2":
		return TierSignal, nil
	case "T3":
		return TierExtension, nil
	default:
		return 0, fmt.Errorf("unknown perm tier %q", s)
	}
}
