package command

import (
	"encoding/json"
	"testing"

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
