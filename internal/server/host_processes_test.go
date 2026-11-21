package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/wire"
)

func processFactOf(key string, labels map[string]string, value string) wire.Fact {
	return wire.Fact{Kind: wire.HostProcessFact, Key: key, Labels: labels, ValueJSON: json.RawMessage(value)}
}

func TestPROC010_FactToRowAndRejects(t *testing.T) {
	ts := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	inst := uuid.New()
	labels := map[string]string{"pid": "42", "backend_type": "client backend", "datname": "app", "usename": "u", "application_name": "web", "state": "active", "queryid": "-7"}
	row, ok := processRow(processFactOf("42", labels, `{"cpu_ratio":0.25,"rss_bytes":1048576,"read_bytes_per_s":10,"write_bytes_per_s":0}`), ts, "default", 9, inst)
	require.True(t, ok)
	require.Equal(t, int32(42), row.PID)
	require.Equal(t, ts, row.TS)
	require.Equal(t, inst, row.InstanceID)
	require.Equal(t, int64(9), row.ClusterID)
	require.Equal(t, "client backend", row.BackendType)
	require.Equal(t, "web", row.ApplicationName)
	require.Equal(t, int64(-7), *row.QueryID)
	require.Equal(t, 0.25, row.CPURatio)
	require.Equal(t, int64(1048576), row.RSSBytes)
	require.Equal(t, 10.0, *row.ReadBytesPerS)
	require.Equal(t, 0.0, *row.WriteBytesPerS)

	noIO, ok := processRow(processFactOf("7", map[string]string{"pid": "7"}, `{"cpu_ratio":0,"rss_bytes":0}`), ts, "default", 9, inst)
	require.True(t, ok)
	require.Nil(t, noIO.ReadBytesPerS)
	require.Nil(t, noIO.QueryID)
	require.Equal(t, "", noIO.BackendType)

	for name, f := range map[string]wire.Fact{
		"pid zero":          processFactOf("0", map[string]string{"pid": "0"}, `{"cpu_ratio":0,"rss_bytes":0}`),
		"pid negative":      processFactOf("-1", map[string]string{"pid": "-1"}, `{"cpu_ratio":0,"rss_bytes":0}`),
		"pid not a number":  processFactOf("x", map[string]string{"pid": "x"}, `{"cpu_ratio":0,"rss_bytes":0}`),
		"key differs":       processFactOf("8", map[string]string{"pid": "7"}, `{"cpu_ratio":0,"rss_bytes":0}`),
		"cpu negative":      processFactOf("7", map[string]string{"pid": "7"}, `{"cpu_ratio":-0.1,"rss_bytes":0}`),
		"cpu missing":       processFactOf("7", map[string]string{"pid": "7"}, `{"rss_bytes":0}`),
		"rss negative":      processFactOf("7", map[string]string{"pid": "7"}, `{"cpu_ratio":0,"rss_bytes":-1}`),
		"rss too large":     processFactOf("7", map[string]string{"pid": "7"}, `{"cpu_ratio":0,"rss_bytes":1e300}`),
		"read negative":     processFactOf("7", map[string]string{"pid": "7"}, `{"cpu_ratio":0,"rss_bytes":0,"read_bytes_per_s":-1}`),
		"write negative":    processFactOf("7", map[string]string{"pid": "7"}, `{"cpu_ratio":0,"rss_bytes":0,"write_bytes_per_s":-5}`),
		"queryid not a num": processFactOf("7", map[string]string{"pid": "7", "queryid": "q"}, `{"cpu_ratio":0,"rss_bytes":0}`),
		"value not json":    processFactOf("7", map[string]string{"pid": "7"}, `nope`),
	} {
		_, ok := processRow(f, ts, "default", 9, inst)
		require.False(t, ok, name)
	}
}

func TestPROC011_HandlerParameters(t *testing.T) {
	id := uuid.NewString()
	base := "/api/v1/instances/" + id + "/host/processes"
	for _, tc := range []struct {
		url  string
		code int
	}{
		{base, http.StatusOK},
		{base + "?order_by=rss&limit=200&at=2026-11-21T10:00:00Z", http.StatusOK},
		{base + "?order_by=name", http.StatusBadRequest},
		{base + "?limit=0", http.StatusBadRequest},
		{base + "?limit=201", http.StatusBadRequest},
		{base + "?limit=x", http.StatusBadRequest},
		{base + "?at=yesterday", http.StatusBadRequest},
		{"/api/v1/instances/nope/host/processes", http.StatusBadRequest},
		{base + "/top", http.StatusOK},
		{base + "/top?group_by=datname&from=2026-11-21T09:00:00Z&to=2026-11-21T10:00:00Z&limit=5", http.StatusOK},
		{base + "/top?group_by=query", http.StatusBadRequest},
		{base + "/top?from=2026-11-21T10:00:00Z&to=2026-11-21T10:00:00Z", http.StatusBadRequest},
		{base + "/top?from=bad", http.StatusBadRequest},
		{base + "/top?to=bad", http.StatusBadRequest},
		{base + "/top?limit=500", http.StatusBadRequest},
	} {
		rec := httptest.NewRecorder()
		hostTestRouter(NewAPI(nil)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.url, nil))
		require.Equal(t, tc.code, rec.Code, "%s: %s", tc.url, rec.Body.String())
	}
	rec := httptest.NewRecorder()
	hostTestRouter(NewAPI(nil)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, base, nil))
	require.JSONEq(t, `{"ts":null,"processes":[]}`, rec.Body.String())
}

func TestPROC012_GroupFold(t *testing.T) {
	sums := []processGroupSums{
		{Group: "autovacuum worker", CPU: 0.5, MaxRSS: 10, Read: 0, ReadKnown: 0, Write: 0, WriteKnown: 0},
		{Group: "client backend", CPU: 3, MaxRSS: 300, Read: 400, ReadKnown: 3, Write: 100, WriteKnown: 1},
		{Group: "checkpointer", CPU: 0.5, MaxRSS: 50, Read: 0, ReadKnown: 2, Write: 40, WriteKnown: 2},
	}
	got := foldProcessGroups(sums, 2, 10)
	require.Len(t, got, 3)
	require.Equal(t, "client backend", got[0].Group)
	require.Equal(t, 1.5, got[0].AvgCPURatio)
	require.Equal(t, int64(300), got[0].MaxRSSBytes)
	require.Equal(t, 200.0, *got[0].AvgReadBytesPerS)
	require.Equal(t, 50.0, *got[0].AvgWriteBytesPerS)
	// Equal CPU: ordered by name.
	require.Equal(t, "autovacuum worker", got[1].Group)
	require.Nil(t, got[1].AvgReadBytesPerS)
	require.Nil(t, got[1].AvgWriteBytesPerS)
	require.Equal(t, "checkpointer", got[2].Group)
	require.Equal(t, 0.0, *got[2].AvgReadBytesPerS)
	require.Equal(t, 20.0, *got[2].AvgWriteBytesPerS)

	require.Len(t, foldProcessGroups(sums, 2, 1), 1)
	require.Empty(t, foldProcessGroups(sums, 0, 10))
}
