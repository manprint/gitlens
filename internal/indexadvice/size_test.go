package indexadvice

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIDXS013_SizeFormula(t *testing.T) {
	meta := TableMeta{RelTuples: 1_000_000, Columns: map[string]Column{"a": {AvgWidth: 8}, "b": {TypLen: 16}, "c": {TypLen: -1}}}
	size, notes := estimateSize(meta, true, []KeyColumn{{Name: "a"}, {Name: "b"}})
	require.NotNil(t, size)
	require.Equal(t, int64(1_000_000*(8+16+8+4)*1.15), *size)
	require.Empty(t, notes)

	size, notes = estimateSize(meta, true, []KeyColumn{{Name: "a"}, {Name: "c"}, {Name: "missing"}})
	require.Equal(t, int64(1_000_000*(8+32+32+12)*1.15), *size)
	require.Equal(t, []string{NoteWidthAssumed}, notes)
}

func TestIDXS013_SizeUnknown(t *testing.T) {
	size, notes := estimateSize(TableMeta{}, false, []KeyColumn{{Name: "a"}})
	require.Nil(t, size)
	require.Equal(t, []string{NoteSizeUnknown}, notes)
	size, _ = estimateSize(TableMeta{RelTuples: -1}, true, []KeyColumn{{Name: "a"}})
	require.Nil(t, size)
	size, _ = estimateSize(TableMeta{RelTuples: -1, LiveTuples: 100, Columns: map[string]Column{"a": {AvgWidth: 4}}}, true, []KeyColumn{{Name: "a"}})
	require.Equal(t, int64(100*16*1.15), *size, "live tuples stand in for a never-analysed reltuples")
}

func TestIDXS013_BtreeType(t *testing.T) {
	for _, typ := range []string{"json", "xml", "point", "tsvector"} {
		require.False(t, btreeType(typ), typ)
	}
	for _, typ := range []string{"", "text", "jsonb", "integer", "uuid"} {
		require.True(t, btreeType(typ), typ)
	}
}
