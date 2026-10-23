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
//
// payload_v2_pg19.json is synthetic. It is payload_v2_pg18.json with only
// pg_version changed to 190000, not a capture from a PostgreSQL 19 agent, so
// the pg19 case proves the server accepts that shape at PG19's version number.
// It does not prove what a real PG19 agent sends. Replace it with a captured
// envelope once one exists.
func TestINTGOLDEN002_ProtocolV2Facts(t *testing.T) {
	for _, major := range []int{15, 18, 19} {
		t.Run(fmt.Sprintf("pg%d", major), func(t *testing.T) {
			path := filepath.Join("testdata", fmt.Sprintf("payload_v2_pg%d.json", major))
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			var env wire.Envelope
			require.NoError(t, json.Unmarshal(data, &env))
			require.Equal(t, 2, env.ProtocolVersion)
			require.LessOrEqual(t, env.ProtocolVersion, wire.ProtocolVersionCurrent, "a v2 payload is still accepted")
			require.Len(t, env.Instances, 1)
			expectedVersion := 150014
			switch major {
			case 18:
				expectedVersion = 180002
			case 19:
				expectedVersion = 190000
			}
			require.Equal(t, expectedVersion, env.Instances[0].PGVersion)
			require.Len(t, env.Instances[0].Results, 1)
			require.Len(t, env.Instances[0].Results[0].Facts, 1)
			require.NoError(t, env.Instances[0].Results[0].Facts[0].Validate())
		})
	}
}
