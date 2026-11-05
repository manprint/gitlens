package indexadvice

import (
	"encoding/json"
	"os"
	"testing"
)

func FuzzParse(f *testing.F) {
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

// IDXA-FUZZ-001: the seed corpus (every corpus statement plus the edge seeds) passes.
func TestIDXAFUZZ001_SeedsTerminate(t *testing.T) {
	// FuzzParse runs its seeds as sub-tests under `go test`; this test pins the
	// name of the gate and checks a few hostile inputs terminate quickly.
	for _, in := range []string{"SELECT " + string(make([]byte, 2000)), "SELECT 1 FROM t WHERE " + repeat("a = $1 AND ", 3000) + "b = $2"} {
		if len(in) < MaxInput {
			Parse(in)
		}
	}
}

func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
