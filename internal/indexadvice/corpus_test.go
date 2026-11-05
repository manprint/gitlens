package indexadvice

import (
	"encoding/json"
	"flag"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite the expected analyses of testdata/odoo_corpus.json from Parse (review the diff)")

type corpusEntry struct {
	ID       string   `json:"id"`
	Source   string   `json:"source"`
	SQL      string   `json:"sql"`
	Expected Analysis `json:"expected"`
}

type corpusFile struct {
	Entries []corpusEntry `json:"entries"`
}

const corpusPath = "testdata/odoo_corpus.json"

func TestIDXACORPUS001_Entries(t *testing.T) {
	raw, err := os.ReadFile(corpusPath)
	require.NoError(t, err)
	var file corpusFile
	require.NoError(t, json.Unmarshal(raw, &file))
	require.GreaterOrEqual(t, len(file.Entries), 60, "the corpus holds at least 60 statements")
	if *update {
		for i := range file.Entries {
			file.Entries[i].Expected = Parse(file.Entries[i].SQL)
		}
		out, err := json.MarshalIndent(file, "", " ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(corpusPath, append(out, '\n'), 0o644))
		return
	}
	seen := map[string]bool{}
	for _, entry := range file.Entries {
		require.False(t, seen[entry.SQL], "duplicate statement %s", entry.ID)
		seen[entry.SQL] = true
		t.Run(entry.ID, func(t *testing.T) {
			require.Equal(t, entry.Expected, Parse(entry.SQL))
		})
	}
}
