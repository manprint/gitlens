package command

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestArgsUnmarshalJSONPreservesStringQueryID(t *testing.T) {
	var args Args
	require.NoError(t, json.Unmarshal([]byte(`{"queryid":"9223372036854775807"}`), &args))
	require.NotNil(t, args.QueryID)
	require.Equal(t, int64(9223372036854775807), *args.QueryID)
}

func TestArgsUnmarshalJSONAcceptsLegacyNumericQueryID(t *testing.T) {
	var args Args
	require.NoError(t, json.Unmarshal([]byte(`{"queryid":123}`), &args))
	require.NotNil(t, args.QueryID)
	require.Equal(t, int64(123), *args.QueryID)
}

func TestArgsUnmarshalJSONRejectsUnknownFields(t *testing.T) {
	var args Args
	require.Error(t, json.Unmarshal([]byte(`{"queryid":"123","query_text":"SELECT 1"}`), &args))
}

// The start time keeps its microseconds through the wire, and the agent
// compares it with the one PostgreSQL reports at that precision.
func TestArgsUnmarshalJSONKeepsBackendStartMicroseconds(t *testing.T) {
	var args Args
	require.NoError(t, json.Unmarshal([]byte(`{"pid":42,"backend_start":"2026-10-23T09:00:00.123456+02:00"}`), &args))
	require.NotNil(t, args.BackendStart)
	require.Equal(t, time.Date(2026, 10, 23, 7, 0, 0, 123456000, time.UTC).UnixMicro(), args.BackendStart.UnixMicro())

	encoded, err := json.Marshal(args)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"backend_start":"2026-10-23T09:00:00.123456+02:00"`)
}

func TestArgsUnmarshalJSONRejectsMalformedBackendStart(t *testing.T) {
	var args Args
	require.Error(t, json.Unmarshal([]byte(`{"pid":42,"backend_start":"yesterday"}`), &args))
}
