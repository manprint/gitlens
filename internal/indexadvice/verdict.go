package indexadvice

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"strings"
)

// Verdicts, from a documented heuristic: read/write ratio ≥ 10 strong, ≥ 1
// moderate, below 1 write_heavy.
const (
	VerdictStrong     = "strong"
	VerdictModerate   = "moderate"
	VerdictWriteHeavy = "write_heavy"

	strongRatio   = 10.0
	moderateRatio = 1.0
	minWriteRate  = 0.001
)

// readWriteRatio divides the statements' call rate by the row changes that
// must touch an index: inserts, non-HOT updates and deletes.
func readWriteRatio(calls float64, w Writes) float64 {
	return calls / math.Max(indexWriteRate(w), minWriteRate)
}

// indexWriteRate is the row changes per second that must touch an index.
func indexWriteRate(w Writes) float64 {
	return w.Ins + (w.Upd - w.HotUpd) + w.Del
}

// noIndexWrites is true when the table's writes are unknown or so rare that
// an index costs nothing: the ratio then says nothing against the index.
func noIndexWrites(w Writes, writesKnown bool) bool {
	return !writesKnown || indexWriteRate(w) < minWriteRate
}

// verdictFor maps the ratio to a verdict; a low-selectivity first column
// lowers it one step.
func verdictFor(ratio float64, lowSelectivity bool) string {
	level := 0
	switch {
	case ratio >= strongRatio:
		level = 2
	case ratio >= moderateRatio:
		level = 1
	}
	if lowSelectivity && level > 0 {
		level--
	}
	return [...]string{VerdictWriteHeavy, VerdictModerate, VerdictStrong}[level]
}

// selectivityOf labels the first key column: high ≥ 1000 distinct values or a
// fraction ≤ -0.1, low ≤ 20 distinct values, medium otherwise, unknown without
// statistics.
func selectivityOf(meta TableMeta, hasMeta bool, column string) Selectivity {
	sel := Selectivity{Column: column, Label: "unknown"}
	if !hasMeta {
		return sel
	}
	col, ok := meta.Columns[column]
	if !ok || col.NDistinct == nil || *col.NDistinct == 0 {
		return sel
	}
	nd := *col.NDistinct
	sel.NDistinct = &nd
	distinct := nd
	if nd < 0 {
		rows := meta.RelTuples
		if rows <= 0 {
			rows = meta.LiveTuples
		}
		distinct = -nd * rows
	}
	switch {
	case nd <= -0.1 || (nd > 0 && nd >= 1000) || (nd < 0 && distinct >= 1000):
		sel.Label = "high"
	case distinct <= 20 && (nd > 0 || meta.RelTuples > 0 || meta.LiveTuples > 0):
		sel.Label = "low"
	default:
		sel.Label = "medium"
	}
	return sel
}

// reservedWords are the keywords of pg_get_keywords() with category R or T
// of the monitored server: they cannot be used as an unquoted name.
var reservedWords = map[string]bool{
	"all": true, "analyse": true, "analyze": true, "and": true, "any": true, "array": true, "as": true,
	"asc": true, "asymmetric": true, "authorization": true, "binary": true, "both": true, "case": true,
	"cast": true, "check": true, "collate": true, "collation": true, "column": true, "concurrently": true,
	"constraint": true, "create": true, "cross": true, "current_catalog": true, "current_date": true,
	"current_role": true, "current_schema": true, "current_time": true, "current_timestamp": true,
	"current_user": true, "default": true, "deferrable": true, "desc": true, "distinct": true, "do": true,
	"else": true, "end": true, "except": true, "false": true, "fetch": true, "for": true, "foreign": true,
	"freeze": true, "from": true, "full": true, "grant": true, "group": true, "having": true, "ilike": true,
	"in": true, "initially": true, "inner": true, "intersect": true, "into": true, "is": true, "isnull": true,
	"join": true, "lateral": true, "leading": true, "left": true, "like": true, "limit": true,
	"localtime": true, "localtimestamp": true, "natural": true, "not": true, "notnull": true, "null": true,
	"offset": true, "on": true, "only": true, "or": true, "order": true, "outer": true, "overlaps": true,
	"placing": true, "primary": true, "references": true, "returning": true, "right": true, "select": true,
	"session_user": true, "similar": true, "some": true, "symmetric": true, "system_user": true, "table": true,
	"tablesample": true, "then": true, "to": true, "trailing": true, "true": true, "union": true,
	"unique": true, "user": true, "using": true, "variadic": true, "verbose": true, "when": true, "where": true,
	"window": true, "with": true,
}

// quoteIdent quotes an identifier when it is not a plain lower-case name or is
// a reserved word, doubling any embedded quote.
func quoteIdent(name string) string {
	plain := name != ""
	for i := 0; plain && i < len(name); i++ {
		c := name[i]
		plain = c == '_' || (c >= 'a' && c <= 'z') || (i > 0 && c >= '0' && c <= '9')
	}
	if plain && !reservedWords[name] {
		return name
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// indexName is idx_<table>_<cols>; a name over 63 bytes is cut on a rune
// boundary and ends with "_" and 6 hex digits of the full name's hash.
func indexName(table string, cols []KeyColumn) string {
	parts := []string{"idx", table}
	for _, c := range cols {
		parts = append(parts, c.Name)
	}
	full := strings.Join(parts, "_")
	if len(full) <= maxNameBytes {
		return full
	}
	sum := sha256.Sum256([]byte(full))
	cut := maxNameBytes - 7
	for cut > 0 && !isRuneStart(full[cut]) {
		cut--
	}
	return full[:cut] + "_" + hex.EncodeToString(sum[:])[:6]
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

func createStatement(key Key, cols []KeyColumn) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = quoteIdent(c.Name)
		if c.Desc {
			parts[i] += " DESC"
		}
	}
	return "CREATE INDEX CONCURRENTLY " + quoteIdent(indexName(key.Table, cols)) + " ON " +
		quoteIdent(key.Schema) + "." + quoteIdent(key.Table) + " (" + strings.Join(parts, ", ") + ");"
}
