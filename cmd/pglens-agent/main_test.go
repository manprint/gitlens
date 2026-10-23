package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
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

// TestRunAgentHealthcheck_ProbesTheListenAddress: an agent bound to one address
// does not answer on loopback, so the probe must dial the address it bound.
func TestRunAgentHealthcheck_ProbesTheListenAddress(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.2:0")
	if err != nil {
		t.Skipf("no 127.0.0.2 loopback address on this host: %v", err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	t.Setenv("PGLENS_HEALTHZ_LISTEN", ln.Addr().String())

	require.Equal(t, 0, runAgentHealthcheck())
}

// TestRunAgentHealthcheck_FailsWhenHealthzReports503: /healthz answers 503 for a
// revoked, unauthorized or incompatible agent. Docker reads only the probe's
// exit status, so a 503 must fail the probe, or the container shows healthy
// while the agent ships nothing.
func TestRunAgentHealthcheck_FailsWhenHealthzReports503(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"state":"revoked"}`))
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	t.Setenv("PGLENS_HEALTHZ_LISTEN", ln.Addr().String())

	require.Equal(t, 1, runAgentHealthcheck())
}

func TestHealthcheckURL(t *testing.T) {
	tests := []struct{ listen, want string }{
		{":9187", "http://127.0.0.1:9187/healthz"},
		{"0.0.0.0:9187", "http://127.0.0.1:9187/healthz"},
		{"[::]:9187", "http://127.0.0.1:9187/healthz"},
		{"127.0.0.1:9187", "http://127.0.0.1:9187/healthz"},
		{"10.0.0.5:9187", "http://10.0.0.5:9187/healthz"},
		{"[::1]:9187", "http://[::1]:9187/healthz"},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, healthcheckURL(tt.listen, "/healthz"), "listen %s", tt.listen)
	}
}

// TestIsMountWarning: the identity path warns unless the mount that holds it
// survives a restart. That mount is the longest mountpoint that contains the
// path, matched by whole path components, and a memory filesystem or the
// container's overlay root does not survive a restart.
func TestIsMountWarning(t *testing.T) {
	const (
		containerRoot = "36 35 0:31 / / rw,relatime - overlay overlay rw\n"
		hostRoot      = "36 35 8:1 / / rw,relatime - ext4 /dev/sda1 rw\n"
		identity      = "/var/lib/pglens/identity.json"
	)
	tests := []struct {
		name      string
		mountinfo string
		path      string
		want      bool
	}{
		{
			name:      "container root holds the identity",
			mountinfo: containerRoot,
			path:      identity,
			want:      true,
		},
		{
			name:      "initial rootfs holds the identity",
			mountinfo: "1 0 0:1 / / rw,relatime - rootfs rootfs rw\n",
			path:      "/var/lib/pglens",
			want:      true,
		},
		{
			name:      "persistent root filesystem in binary mode",
			mountinfo: hostRoot,
			path:      identity,
			want:      false,
		},
		{
			name:      "separate ext4 volume at the identity directory",
			mountinfo: containerRoot + "40 36 8:2 / /var/lib/pglens rw,relatime shared:2 - ext4 /dev/sdb1 rw\n",
			path:      identity,
			want:      false,
		},
		{
			name:      "volume at a sibling prefix does not hold the identity",
			mountinfo: containerRoot + "41 36 8:2 / /var/lib/pg rw,relatime - ext4 /dev/sdb1 rw\n",
			path:      identity,
			want:      true,
		},
		{
			name:      "tmpfs at the identity directory",
			mountinfo: containerRoot + "42 36 0:40 / /var/lib/pglens rw,nosuid - tmpfs tmpfs rw\n",
			path:      identity,
			want:      true,
		},
		{
			name:      "tmpfs nested in a volume holds the identity",
			mountinfo: containerRoot + "43 36 8:2 / /var/lib/pglens rw - ext4 /dev/sdb1 rw\n44 43 0:41 / /var/lib/pglens/run rw - tmpfs tmpfs rw\n",
			path:      "/var/lib/pglens/run/identity.json",
			want:      true,
		},
		{
			name:      "volume stacked over a tmpfs at the same mountpoint",
			mountinfo: containerRoot + "45 36 0:42 / /var/lib/pglens rw - tmpfs tmpfs rw\n46 36 8:2 / /var/lib/pglens rw - ext4 /dev/sdb1 rw\n",
			path:      identity,
			want:      false,
		},
		{
			name:      "escaped space in the mountpoint",
			mountinfo: containerRoot + `47 36 8:3 / /srv/pg\040data rw - ext4 /dev/sdc1 rw` + "\n",
			path:      "/srv/pg data/identity.json",
			want:      false,
		},
		{
			name:      "optional fields before the separator",
			mountinfo: containerRoot + "48 36 8:2 / /var/lib/pglens rw shared:5 master:3 - ext4 /dev/sdb1 rw\n",
			path:      identity,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpfile := filepath.Join(t.TempDir(), "mountinfo")
			require.NoError(t, os.WriteFile(tmpfile, []byte(tt.mountinfo), 0600))

			warning, err := isMountWarning(tt.path, tmpfile)
			require.NoError(t, err)
			require.Equal(t, tt.want, warning)
		})
	}
}
