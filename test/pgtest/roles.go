//go:build integration

package pgtest

// Role defines the permission tier for testing.
type Role int

const (
	RoleSuperuser   Role = iota // setup only, never used by a check
	RoleT0                      // pg_monitor + the two pg_control_* grants
	RoleT0NoControl             // pg_monitor only — used to prove R5
	RoleT1                      // T0 + pg_read_all_data
	RoleT2                      // T0 + pg_signal_backend
	RoleVictim                  // unprivileged victim for termination tests
)

func (r Role) String() string {
	switch r {
	case RoleSuperuser:
		return "superuser"
	case RoleT0:
		return "T0"
	case RoleT0NoControl:
		return "T0NoControl"
	case RoleT1:
		return "T1"
	case RoleT2:
		return "T2"
	default:
		return "unknown"
	}
}

// roleUser returns the postgres username for a role.
func roleUser(r Role) string {
	switch r {
	case RoleSuperuser:
		return "postgres"
	case RoleT0:
		return "pglens"
	case RoleT0NoControl:
		return "pglens_nocontrol"
	case RoleT1:
		return "pglens_t1"
	case RoleT2:
		return "pglens_t2"
	case RoleVictim:
		return "perm_victim"
	default:
		return "postgres"
	}
}

// rolePassword returns password for a role (all test roles use 'test' except superuser which also uses test).
func rolePassword(r Role) string {
	return "test"
}
