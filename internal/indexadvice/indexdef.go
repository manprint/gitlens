package indexadvice

import (
	"errors"
	"strings"
)

// IndexColumn is one key column of an index. For an expression Name holds the
// expression text and Expr is true.
type IndexColumn struct {
	Name string `json:"name"`
	Desc bool   `json:"desc,omitempty"`
	Expr bool   `json:"expr,omitempty"`
}

// IndexDef is a parsed pg_get_indexdef text.
type IndexDef struct {
	Schema  string        `json:"schema,omitempty"`
	Table   string        `json:"table"`
	Name    string        `json:"name"`
	Unique  bool          `json:"unique,omitempty"`
	Method  string        `json:"method"`
	Columns []IndexColumn `json:"columns"`
	Include []string      `json:"include,omitempty"`
	Where   string        `json:"where,omitempty"`
}

var errIndexDef = errors.New("indexadvice: not an index definition")

// ParseIndexDef reads `CREATE [UNIQUE] INDEX name ON [ONLY] [schema.]table
// USING method (cols) [INCLUDE (cols)] [WITH (...)] [TABLESPACE t] [WHERE pred]`.
func ParseIndexDef(def string) (IndexDef, error) {
	var d IndexDef
	if len(def) > MaxInput {
		return d, errIndexDef
	}
	toks, err := lex(def)
	if err != nil || !balanced(toks) {
		return d, errIndexDef
	}
	i := 0
	expect := func(words ...string) bool {
		for _, w := range words {
			if i >= len(toks) || !toks[i].is(w) {
				return false
			}
			i++
		}
		return true
	}
	if !expect("create") {
		return d, errIndexDef
	}
	if expect("unique") {
		d.Unique = true
	}
	if !expect("index") {
		return d, errIndexDef
	}
	expect("concurrently")
	expect("if", "not", "exists")
	if i >= len(toks) || toks[i].kind != tIdent {
		return d, errIndexDef
	}
	d.Name = toks[i].text
	i++
	if !expect("on") {
		return d, errIndexDef
	}
	expect("only")
	var parts []string
	for i < len(toks) && toks[i].kind == tIdent {
		parts = append(parts, toks[i].text)
		i++
		if i+1 < len(toks) && toks[i].kind == tDot && toks[i+1].kind == tIdent {
			i++
			continue
		}
		break
	}
	if len(parts) == 0 {
		return d, errIndexDef
	}
	d.Table = parts[len(parts)-1]
	if len(parts) >= 2 {
		d.Schema = parts[len(parts)-2]
	}
	d.Method = "btree"
	if expect("using") {
		if i >= len(toks) || toks[i].kind != tIdent {
			return d, errIndexDef
		}
		d.Method = toks[i].text
		i++
	}
	if i >= len(toks) || toks[i].kind != tLParen {
		return d, errIndexDef
	}
	end := matching(toks, i)
	for _, item := range splitTop(toks[i+1:end], func(k int) bool { return toks[i+1+k].kind == tComma }) {
		if len(item) == 0 {
			return d, errIndexDef
		}
		d.Columns = append(d.Columns, indexColumn(def, item))
	}
	i = end + 1
	if expect("include") {
		if i >= len(toks) || toks[i].kind != tLParen {
			return d, errIndexDef
		}
		end = matching(toks, i)
		for _, t := range toks[i+1 : end] {
			if t.kind == tIdent {
				d.Include = append(d.Include, t.text)
			}
		}
		i = end + 1
	}
	if expect("with") && i < len(toks) && toks[i].kind == tLParen {
		i = matching(toks, i) + 1
	}
	if expect("tablespace") && i < len(toks) {
		i++
	}
	if expect("where") && i < len(toks) {
		d.Where = strings.TrimSpace(def[toks[i].pos:])
	}
	return d, nil
}

// indexColumn classifies one key item: a plain (possibly quoted) column name
// optionally followed by COLLATE, an operator class, ASC/DESC and NULLS
// FIRST/LAST is a column; anything else is an expression kept as text.
func indexColumn(def string, item []token) IndexColumn {
	if item[0].kind == tIdent {
		if len(item) == 1 || item[1].kind == tIdent {
			col := IndexColumn{Name: item[0].text}
			plain := true
			for _, t := range item[1:] {
				if t.kind != tIdent {
					plain = false
				}
				if t.is("desc") {
					col.Desc = true
				}
			}
			if plain {
				return col
			}
		}
	}
	text := strings.TrimSpace(def[item[0].pos:item[len(item)-1].end])
	col := IndexColumn{Name: text, Expr: true}
	last := item[len(item)-1]
	if last.is("desc") || (len(item) > 2 && item[len(item)-2].is("desc")) || (len(item) > 3 && item[len(item)-3].is("desc")) {
		col.Desc = true
	}
	return col
}
