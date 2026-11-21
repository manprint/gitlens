package server

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/wire"
)

func autovacuumFact(labels map[string]string, value string) wire.Fact {
	l := map[string]string{
		"ts": "2026-11-21T10:00:03.5Z", "datname": "app", "schemaname": "public", "relname": "t",
		"kind": "vacuum", "aggressive": "true", "wraparound": "false",
	}
	for k, v := range labels {
		if v == "" {
			delete(l, k)
			continue
		}
		l[k] = v
	}
	return wire.Fact{Kind: autovacuumRunFactKind, Key: "s1/7", Labels: l, ValueJSON: []byte(value)}
}

// AVH-007: a fact becomes one row; numbers the fact lacks stay NULL.
func TestAVH007(t *testing.T) {
	inst := uuid.New()
	row, ok := autovacuumRunRow(autovacuumFact(nil, `{"tuples_removed":19900,"wal_fpi":6,"elapsed_seconds":0.25}`), "default", 7, inst)
	require.True(t, ok)
	require.Equal(t, "2026-11-21T10:00:03.5Z", row.TS.Format("2006-01-02T15:04:05.999Z07:00"))
	require.Equal(t, inst, row.InstanceID)
	require.Equal(t, int64(7), row.ClusterID)
	require.Equal(t, []string{"app", "public", "t", "vacuum"}, []string{row.Datname, row.Schemaname, row.Relname, row.Kind})
	require.True(t, row.Aggressive)
	require.False(t, row.Wraparound)
	require.Equal(t, int64(19900), *row.TuplesRemoved)
	require.Equal(t, int64(6), *row.WALFPI)
	require.Equal(t, 0.25, *row.ElapsedSeconds)
	require.Nil(t, row.PagesScanned)
	require.Nil(t, row.BufferReads)
	require.Nil(t, row.CPUUserSeconds)

	row, ok = autovacuumRunRow(autovacuumFact(map[string]string{"kind": "analyze"}, `{}`), "default", 7, inst)
	require.True(t, ok)
	require.Equal(t, "analyze", row.Kind)
	require.Nil(t, row.TuplesRemoved)
}

// AVH-008: a bad kind, a missing name, a bad time or flag, or a negative
// number is rejected.
func TestAVH008(t *testing.T) {
	inst := uuid.New()
	for name, f := range map[string]wire.Fact{
		"kind":       autovacuumFact(map[string]string{"kind": "cluster"}, `{}`),
		"relation":   autovacuumFact(map[string]string{"relname": ""}, `{}`),
		"schema":     autovacuumFact(map[string]string{"schemaname": ""}, `{}`),
		"database":   autovacuumFact(map[string]string{"datname": ""}, `{}`),
		"ts":         autovacuumFact(map[string]string{"ts": "yesterday"}, `{}`),
		"flag":       autovacuumFact(map[string]string{"wraparound": "maybe"}, `{}`),
		"negative":   autovacuumFact(nil, `{"pages_removed":-1}`),
		"negative_s": autovacuumFact(nil, `{"elapsed_seconds":-0.5}`),
		"json":       autovacuumFact(nil, `[1]`),
	} {
		_, ok := autovacuumRunRow(f, "default", 7, inst)
		require.False(t, ok, name)
	}
}
