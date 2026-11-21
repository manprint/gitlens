package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/pglog"
	"github.com/manprint/pglens/internal/wire"
)

func logFact(labels map[string]string, body map[string]string) wire.Fact {
	raw, _ := json.Marshal(body)
	return wire.Fact{Kind: logEventFactKind, Key: "6b01761c.2e/17", Labels: labels, ValueJSON: raw}
}

func logLabels() map[string]string {
	return map[string]string{
		"ts": "2026-11-21T10:00:00.123456Z", "class": pglog.ClassErrorStatement, "severity": "ERROR",
		"sqlstate": "23505", "datname": "app", "usename": "u", "application_name": "psql",
		"pid": "42", "queryid": "-7",
	}
}

// PGL-022
func TestPGL022LogEventRow(t *testing.T) {
	inst := uuid.New()
	row, ok := logEventRow(logFact(logLabels(), map[string]string{"message": "duplicate key", "statement": "INSERT INTO t VALUES (1)"}), "default", 3, inst)
	require.True(t, ok)
	require.Equal(t, time.Date(2026, 11, 21, 10, 0, 0, 123456000, time.UTC), row.TS)
	require.Equal(t, "6b01761c.2e", row.SessionID)
	require.Equal(t, int64(17), row.LineNum)
	require.Equal(t, int32(42), *row.PID)
	require.Equal(t, int64(-7), *row.QueryID)
	require.Equal(t, "duplicate key", row.Message)
	require.Equal(t, inst, row.InstanceID)
	require.Equal(t, int64(3), row.ClusterID)
	require.JSONEq(t, `{"statement":"INSERT INTO t VALUES (1)"}`, string(row.Extra))

	plain, ok := logEventRow(logFact(logLabels(), map[string]string{"message": "m"}), "default", 3, inst)
	require.True(t, ok)
	require.Nil(t, plain.Extra)

	for name, mutate := range map[string]func(map[string]string){
		"unknown class": func(l map[string]string) { l["class"] = "not.a.class" },
		"missing ts":    func(l map[string]string) { delete(l, "ts") },
		"bad ts":        func(l map[string]string) { l["ts"] = "yesterday" },
		"no severity":   func(l map[string]string) { delete(l, "severity") },
		"no class":      func(l map[string]string) { delete(l, "class") },
	} {
		l := logLabels()
		mutate(l)
		_, ok := logEventRow(logFact(l, map[string]string{"message": "m"}), "default", 3, inst)
		require.False(t, ok, name)
	}
}

// PGL-023
func TestPGL023ServerWithholds(t *testing.T) {
	row, ok := logEventRow(logFact(logLabels(), map[string]string{
		"message":   "statement: ALTER ROLE x PASSWORD 'hunter2'",
		"statement": "CREATE USER y PASSWORD 'hunter3'",
		"hint":      "plain hint",
	}), "default", 1, uuid.New())
	require.True(t, ok)
	require.Equal(t, logWithheld, row.Message)
	var extra map[string]string
	require.NoError(t, json.Unmarshal(row.Extra, &extra))
	require.Equal(t, logWithheld, extra["statement"])
	require.Equal(t, "plain hint", extra["hint"])
	require.False(t, strings.Contains(string(row.Extra), "hunter"))
}

// PGL-024
func TestPGL024RetentionFamily(t *testing.T) {
	f, ok := lookupRetentionFamily("log_events")
	require.True(t, ok)
	require.Equal(t, "log_events", f.Hypertable)
	require.Equal(t, 14, f.DefaultDays)
	require.False(t, f.Rollup)
}
