package main

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/check"
	"github.com/manprint/pglens/internal/pgtype"
)

// stubCheck is a check with the requirements a test sets, so the status of a
// mandatory check can be driven by the server it is run against.
type stubCheck struct {
	name string
	req  check.Requirements
}

func (c stubCheck) Name() string                   { return c.name }
func (c stubCheck) Requires() check.Requirements   { return c.req }
func (c stubCheck) DefaultInterval() time.Duration { return time.Minute }
func (c stubCheck) Timeout() time.Duration         { return time.Second }

func (c stubCheck) Scrape(context.Context, check.Target) (check.Result, error) {
	return check.Result{}, nil
}

// TestPrintChecks_DisabledMandatoryCheckIsCounted: the exit code of `check` is
// decided from the mandatory checks that are enabled. A mandatory check that
// the server version or the role rules out must still be counted, or it can
// never fail the exit code.
func TestPrintChecks_DisabledMandatoryCheckIsCounted(t *testing.T) {
	checks := []check.Check{
		stubCheck{name: "instance_info"},
		stubCheck{name: "activity", req: check.Requirements{MinPG: pgtype.PGVersion(190000)}},
		stubCheck{name: "database_stats", req: check.Requirements{Roles: []pgtype.Role{pgtype.RoleStandby}}},
		stubCheck{name: "wal", req: check.Requirements{MinPG: pgtype.PGVersion(190000)}},
	}

	var out bytes.Buffer
	mandatory, enabled := printChecks(&out, checks, pgtype.RolePrimary, pgtype.PGVersion(180002), pgtype.TierReadOnly, nil)

	require.Equal(t, 3, mandatory, "instance_info, activity and database_stats are mandatory")
	require.Equal(t, 1, enabled, "only instance_info can run on a PG 18 primary")
	require.Contains(t, out.String(), "requires PG >= 19.0")
}

// TestCheckExitCode: `check` exits 0 only when every mandatory check is
// enabled, the server version is supported and the monitoring role cannot
// write, which is the contract README documents for the exit code.
func TestCheckExitCode(t *testing.T) {
	supported := pgtype.PGVersion(180002)
	readOnly := check.RolePosture{Role: "pglens", ReadOnlyDefault: "on"}
	couldWrite := check.RolePosture{Role: "pglens", ReadOnlyDefault: "on", Problems: []string{"has SUPERUSER"}}

	tests := []struct {
		name       string
		mandatory  int
		enabled    int
		version    pgtype.PGVersion
		posture    check.RolePosture
		postureErr error
		want       int
	}{
		{name: "all mandatory checks enabled", mandatory: 3, enabled: 3, version: supported, posture: readOnly, want: 0},
		{name: "a mandatory check disabled", mandatory: 3, enabled: 2, version: supported, posture: readOnly, want: 1},
		{name: "server version outside 15-19", mandatory: 3, enabled: 3, version: pgtype.PGVersion(140000), posture: readOnly, want: 1},
		{name: "role could write", mandatory: 3, enabled: 3, version: supported, posture: couldWrite, want: 1},
		{name: "role posture unknown", mandatory: 3, enabled: 3, version: supported, postureErr: errors.New("query"), want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, checkExitCode(tt.mandatory, tt.enabled, tt.version, tt.postureErr, tt.posture))
		})
	}
}
