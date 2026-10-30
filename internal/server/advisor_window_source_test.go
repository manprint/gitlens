package server

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/advisor"
)

// WINDOW-009 (13.2): the windows the advisor engine judges with come through
// the same loader as the API (ADVW-002), which keeps a stored value only
// between one hour and 90 days: anything else is the default. The source keeps
// its last answer when a read fails.
func TestWINDOW009_SourceEnforcesBounds(t *testing.T) {
	t.Parallel()
	require.Nil(t, AdvisorWindowsSource(nil), "without a pool the engine keeps its defaults")

	pool := &retentionRecPool{policyRows: [][]any{
		{"window.unused_index", []byte("3599")},
		{"window.backup_history", []byte("7200")},
	}}
	src := advisorWindowsSource(pgAdvisorSettingsStore{pool: pool})
	require.Equal(t, advisor.Windows{UnusedIndex: 7 * 24 * time.Hour, BackupHistory: 2 * time.Hour}, src(context.Background()))

	pool.policyRows = [][]any{{"window.unused_index", []byte("7776001")}, {"window.backup_history", []byte("3600")}}
	require.Equal(t, advisor.Windows{UnusedIndex: 7 * 24 * time.Hour, BackupHistory: time.Hour}, src(context.Background()))

	pool.policyRows = [][]any{{"window.unused_index", []byte("7776000")}}
	require.Equal(t, advisor.Windows{UnusedIndex: 90 * 24 * time.Hour, BackupHistory: 7 * 24 * time.Hour}, src(context.Background()))

	// A stub store that answers a value out of bounds is not trusted either.
	require.Equal(t, advisor.DefaultWindows(), advisorWindowsOf(map[string]int64{"window.unused_index": 60, "window.backup_history": 0}))

	// A failed read keeps the previous answer.
	stub := newStubAdvisorSettings()
	stub.windows["window.unused_index"] = 3600
	stubSrc := advisorWindowsSource(stub)
	require.Equal(t, time.Hour, stubSrc(context.Background()).UnusedIndex)
	stub.windowsErr = errors.New("connection refused")
	require.Equal(t, time.Hour, stubSrc(context.Background()).UnusedIndex)
}

// WINDOW-010 (13.2): an unknown window.* key is refused with a 400 (as
// ADVW-004 shows for the API), and the source reads only the two window keys.
func TestWINDOW010_OnlyTheTwoKeys(t *testing.T) {
	t.Parallel()
	stub := newStubAdvisorSettings()
	api := &API{advisorSettings: stub}
	rec := retentionRequest(t, api, http.MethodPut, "/api/v1/settings/advisor-windows", `{"window.observation":3600}`)
	requireAPIError(t, rec, http.StatusBadRequest, "unknown key")
	require.Zero(t, stub.windowSets)

	pool := &retentionRecPool{policyRows: [][]any{{"window.unused_index", []byte("3600")}}}
	advisorWindowsSource(pgAdvisorSettingsStore{pool: pool})(context.Background())
	require.Equal(t, []recordedExec{{sql: advisorWindowsSQL, args: []any{[]string{"window.unused_index", "window.backup_history"}}}}, pool.calls)
}
