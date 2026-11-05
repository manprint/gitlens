package indexadvice

import (
	"encoding/json"
	"os"
	"testing"
)

// IDXA-FUZZ-001: every corpus statement and the edge seeds parse without panic and deterministically.
func FuzzIDXAFUZZ001(f *testing.F) {
	raw, err := os.ReadFile(corpusPath)
	if err == nil {
		var file corpusFile
		if json.Unmarshal(raw, &file) == nil {
			for _, entry := range file.Entries {
				f.Add(entry.SQL)
			}
		}
	}
	for _, seed := range []string{"", "SELECT", "SELECT (", "WITH a AS (", "SELECT 'x", "$$", "/*", "SELECT 1 FROM t WHERE a BETWEEN", "UPDATE t SET", "((((((((((", "SELECT 1 FROM t JOIN u ON"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, sql string) {
		first := Parse(sql)
		second := Parse(sql)
		if len(first.Predicates) != len(second.Predicates) || len(first.Tables) != len(second.Tables) || len(first.Skipped) != len(second.Skipped) {
			t.Fatalf("Parse is not deterministic for %q", sql)
		}
		_, _ = ParseIndexDef(sql)
	})
}
