package wire_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/manprint/pglens/internal/wire"
	"github.com/stretchr/testify/require"
)

// INT-GOLDEN-002: protocol-v2 fixtures are frozen independently from the
// legacy v1 payloads used by INT-GOLDEN-001.
func TestINTGOLDEN002_ProtocolV2Facts(t *testing.T) {
	for _, major := range []int{15, 18} {
		t.Run(fmt.Sprintf("pg%d", major), func(t *testing.T) {
			path := filepath.Join("testdata", fmt.Sprintf("payload_v2_pg%d.json", major))
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			var env wire.Envelope
			require.NoError(t, json.Unmarshal(data, &env))
			require.Equal(t, wire.ProtocolVersionCurrent, env.ProtocolVersion)
			require.Len(t, env.Instances, 1)
			expectedVersion := 150014
			if major == 18 {
				expectedVersion = 180002
			}
			require.Equal(t, expectedVersion, env.Instances[0].PGVersion)
			require.Len(t, env.Instances[0].Results, 1)
			require.Len(t, env.Instances[0].Results[0].Facts, 1)
			require.NoError(t, env.Instances[0].Results[0].Facts[0].Validate())
		})
	}
}
