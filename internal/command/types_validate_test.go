package command

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// backend_start is an optional guard for cancel and terminate. The other
// kinds reject it, as they reject every argument they do not use.
func TestArgsValidateBackendStart(t *testing.T) {
	started := time.Date(2026, 10, 23, 7, 0, 0, 123456000, time.UTC)
	pid := 42
	queryID := int64(7)
	tests := []struct {
		name    string
		kind    Kind
		args    Args
		wantErr string
	}{
		{name: "cancel with pid and start", kind: KindCancel, args: Args{PID: &pid, BackendStart: &started}},
		{name: "terminate with pid and start", kind: KindTerminate, args: Args{PID: &pid, BackendStart: &started}},
		{name: "cancel with pid alone", kind: KindCancel, args: Args{PID: &pid}},
		{name: "cancel start without pid", kind: KindCancel, args: Args{BackendStart: &started}, wantErr: "cancel requires pid"},
		{name: "explain rejects start", kind: KindExplain, args: Args{QueryID: &queryID, BackendStart: &started}, wantErr: "explain rejects backend_start"},
		{name: "pgstattuple rejects start", kind: KindPgstattuple, args: Args{Schema: "public", Relation: "t", BackendStart: &started}, wantErr: "pgstattuple rejects unrelated arguments"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.args.Validate(tc.kind)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, tc.wantErr)
		})
	}
}
