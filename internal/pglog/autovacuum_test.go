package pglog

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func vacuumFixture(t *testing.T, name string) VacuumRun {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)
	r, ok := ParseAutovacuum(strings.TrimRight(string(raw), "\n"))
	require.True(t, ok, name)
	return r
}

func i64(v int64) *int64     { return &v }
func f64(v float64) *float64 { return &v }

// AVH-001: PG 15 vacuum, every field from the text; reads come from misses.
func TestAVH001(t *testing.T) {
	r := vacuumFixture(t, "autovacuum_pg15_vacuum.txt")
	require.Equal(t, VacuumRun{
		Kind: "vacuum", Database: "postgres", Schema: "public", Relation: "avh",
		IndexScans: i64(0), PagesRemoved: i64(205), PagesRemain: i64(2), PagesScanned: i64(207),
		TuplesRemoved: i64(19900), TuplesRemain: i64(100), TuplesDeadNotRemovable: i64(0),
		BufferHits: i64(649), BufferReads: i64(2), BufferDirtied: i64(4),
		WALRecords: i64(622), WALFullPageImages: i64(1), WALBytes: i64(121298),
		CPUUserSeconds: f64(0), CPUSystemSeconds: f64(0), ElapsedSeconds: f64(0),
	}, r)
}

// AVH-002: PG 18 vacuum: "reads", and the extra "eagerly scanned" and
// "buffers full" text does not break the match.
func TestAVH002(t *testing.T) {
	r := vacuumFixture(t, "autovacuum_pg18_vacuum.txt")
	require.Equal(t, "vacuum", r.Kind)
	require.Equal(t, int64(207), *r.PagesScanned)
	require.Equal(t, int64(19900), *r.TuplesRemoved)
	require.Equal(t, int64(654), *r.BufferHits)
	require.Equal(t, int64(0), *r.BufferReads)
	require.Equal(t, int64(421), *r.WALRecords)
	require.Equal(t, int64(6), *r.WALFullPageImages)
	require.Equal(t, int64(112061), *r.WALBytes)
	require.NotNil(t, r.ElapsedSeconds)
}

// AVH-003: analyze runs: kind analyze, no page or tuple fields, elapsed set.
func TestAVH003(t *testing.T) {
	for _, name := range []string{"autovacuum_pg15_analyze.txt", "autovacuum_pg18_analyze.txt"} {
		r := vacuumFixture(t, name)
		require.Equal(t, "analyze", r.Kind, name)
		require.Equal(t, "avh", r.Relation, name)
		require.Nil(t, r.IndexScans, name)
		require.Nil(t, r.PagesRemoved, name)
		require.Nil(t, r.PagesScanned, name)
		require.Nil(t, r.TuplesRemoved, name)
		require.NotNil(t, r.ElapsedSeconds, name)
		require.NotNil(t, r.BufferHits, name)
	}
	require.Equal(t, int64(24), *vacuumFixture(t, "autovacuum_pg15_analyze.txt").BufferReads)
}

// AVH-004: aggressive vacuum to prevent wraparound sets both flags.
func TestAVH004(t *testing.T) {
	r, ok := ParseAutovacuum(`automatic aggressive vacuum to prevent wraparound of table "d.s.r": index scans: 1`)
	require.True(t, ok)
	require.True(t, r.Aggressive)
	require.True(t, r.Wraparound)
	require.Equal(t, []string{"d", "s", "r"}, []string{r.Database, r.Schema, r.Relation})
	require.Equal(t, int64(1), *r.IndexScans)

	r, ok = ParseAutovacuum(`automatic aggressive vacuum of table "d.s.r.x": index scans: 0`)
	require.True(t, ok)
	require.True(t, r.Aggressive)
	require.False(t, r.Wraparound)
	require.Equal(t, "r.x", r.Relation)

	_, ok = ParseAutovacuum("checkpoint complete: wrote 3 buffers")
	require.False(t, ok)
}

// AVH-005: a header alone gives a run with every number nil.
func TestAVH005(t *testing.T) {
	r, ok := ParseAutovacuum("automatic vacuum of table \"d.s.r\"\npages: garbled")
	require.True(t, ok)
	require.Equal(t, VacuumRun{Kind: "vacuum", Database: "d", Schema: "s", Relation: "r"}, r)
}
