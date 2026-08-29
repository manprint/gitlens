package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHostLocal_InferredFromLocalhostDSN(t *testing.T) {
	require.True(t, TargetIsLocal(TargetConfig{DSN: "postgres://localhost/db"}))
}
func TestHostLocal_InferredFromUnixSocketDSN(t *testing.T) {
	require.True(t, TargetIsLocal(TargetConfig{DSN: "postgres:///db?host=%2Fvar%2Frun%2Fpostgresql"}))
}
func TestHostLocal_ExplicitFalseWins(t *testing.T) {
	f := false
	require.False(t, TargetIsLocal(TargetConfig{DSN: "postgres://localhost/db", HostLocal: &f}))
}

func TestINTHOST002_LocalTargetsUseHostConfiguration(t *testing.T) {
	local := TargetConfig{Name: "local", DSN: "postgres://localhost/db"}
	remote := TargetConfig{Name: "remote", DSN: "postgres://db.internal/db"}
	if !TargetIsLocal(local) || TargetIsLocal(remote) {
		t.Fatalf("host metrics must fan out only to local targets")
	}
}
