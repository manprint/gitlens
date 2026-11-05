package indexadvice

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Limits of Suggest.
const (
	DefaultLimit   = 20
	MaxLimit       = 100
	MaxKeyColumns  = 4
	SmallTableRows = 1000
	maxTextChars   = 400
	maxNameBytes   = 63
)

// Notes carried by a suggestion or counted in Coverage.Skipped.
const (
	NoteSmallTable         = "small_table"
	NoteNotTable           = "not_table"
	NoteIndexUnparsed      = "index_unparsed"
	NoteColumnUnknown      = "column_unknown"
	NoteTypeNotBtree       = "type_not_btree"
	NoteSizeUnknown        = "size_unknown"
	NoteWidthAssumed       = "width_assumed"
	NoteLowSelectivity     = "low_selectivity"
	NoteSelectivityUnknown = "selectivity_unknown"
	NoteWritesUnknown      = "writes_unknown"
)

// Key identifies a table of one monitored database.
type Key struct {
	Database string
	Schema   string
	Table    string
}

// Stmt is one normalised statement with its rates over the window.
type Stmt struct {
	Database   string
	QueryID    int64
	Text       string
	CallsRate  float64
	ExecMsRate float64
}

// Column is the table_columns fact of one column. NDistinct is nil (or zero)
// when the statistics are unreadable or absent; AvgWidth zero means absent.
type Column struct {
	Type      string
	TypLen    int
	AvgWidth  float64
	NDistinct *float64
}

// Writes are the per-second row-change rates of a table.
type Writes struct {
	Ins, Upd, Del, HotUpd float64
}

// TableMeta is what the server knows about a table. A table without Columns
// is treated as having unknown columns: none is dropped. HasCounts says
// RelTuples/LiveTuples come from metrics_tables, HasWrites says Writes does;
// without them the table is neither suppressed as small nor sized, and its
// verdict is capped at moderate.
type TableMeta struct {
	RelTuples  float64
	LiveTuples float64
	HasCounts  bool
	Columns    map[string]Column
	Writes     Writes
	HasWrites  bool
	// IndexesKnown says the table's index definitions are the complete list
	// (the table_indexes fact), not only the indexes the agent selected.
	IndexesKnown bool
	// IndexesUnparsed says an index definition of the table could not be read:
	// its list is incomplete, so no suggestion is made for the table.
	IndexesUnparsed bool
}

// Input of Suggest.
type Input struct {
	Window          time.Duration
	Statements      []Stmt
	TotalExecMsRate map[string]float64
	Tables          map[Key]TableMeta
	Indexes         map[Key][]IndexDef
	// IndexesComplete marks the databases whose tables report every index
	// (TableMeta.IndexesKnown). There, a relation that is not such a table (a
	// view, a foreign table, a table past the inventory limit) is skipped as
	// not_table: no complete index list could rule out an existing index.
	IndexesComplete map[string]bool
	Limit           int
}

// KeyColumn is one column of a suggested index.
type KeyColumn struct {
	Name string `json:"name"`
	Desc bool   `json:"desc"`
}

// StmtShare is a statement that would use the suggested index. SharePct is its
// share of the suggestion's read cost.
type StmtShare struct {
	QueryID   int64   `json:"queryid,string"`
	SharePct  float64 `json:"share_pct"`
	QueryText string  `json:"query_text"`
}

// Selectivity of the first key column.
type Selectivity struct {
	Column    string   `json:"column"`
	NDistinct *float64 `json:"n_distinct"`
	Label     string   `json:"label"`
}

// WriteRates of the table the index would be added to.
type WriteRates struct {
	InsertRate     float64 `json:"insert_rate"`
	UpdateRate     float64 `json:"update_rate"`
	DeleteRate     float64 `json:"delete_rate"`
	HotUpdateRatio float64 `json:"hot_update_ratio"`
}

// Suggestion is one proposed index. Every number is an estimate (I-5).
type Suggestion struct {
	ID                 string      `json:"id"`
	Database           string      `json:"database"`
	Schema             string      `json:"schema"`
	Table              string      `json:"table"`
	Columns            []KeyColumn `json:"columns"`
	CreateStatement    string      `json:"create_statement"`
	WeightPct          float64     `json:"weight_pct"`
	ReadCostMsPerS     float64     `json:"read_cost_ms_per_s"`
	CallsRate          float64     `json:"calls_rate"`
	Statements         []StmtShare `json:"statements"`
	OverlappingIndexes []string    `json:"overlapping_indexes"`
	Selectivity        Selectivity `json:"selectivity"`
	EstSizeBytes       *int64      `json:"est_size_bytes"`
	Write              WriteRates  `json:"write"`
	ReadWriteRatio     float64     `json:"read_write_ratio"`
	Verdict            string      `json:"verdict"`
	Confidence         string      `json:"confidence"`
	Notes              []string    `json:"notes"`
}

// Coverage tells how much of the workload the extractor could place: a
// statement is analysed when at least one of its predicate groups became a
// candidate. Skipped counts statements per reason (extractor reasons plus
// small_table); a statement can have several reasons and an analysed statement
// can also have skipped groups, so the counts do not add up to
// considered − analysed.
type Coverage struct {
	StatementsConsidered int            `json:"statements_considered"`
	StatementsAnalysed   int            `json:"statements_analysed"`
	Skipped              map[string]int `json:"skipped"`
}

// Output of Suggest.
type Output struct {
	WindowSeconds int          `json:"window_seconds"`
	Suggestions   []Suggestion `json:"suggestions"`
	Coverage      Coverage     `json:"coverage"`
}

type candidate struct {
	key   Key
	cols  []KeyColumn
	notes map[string]bool
	stmts []int
	scope int
	// eqLead counts the key columns that came from equality predicates;
	// strict holds the columns with an eq, range or like_prefix predicate.
	eqLead int
	strict map[string]bool
}

func (c *candidate) sig() string {
	names := make([]string, len(c.cols))
	for i, col := range c.cols {
		names[i] = col.Name
	}
	return c.key.Schema + "\x00" + c.key.Table + "\x00" + strings.Join(names, "\x00")
}

// Suggest proposes indexes for the statements. It is pure and deterministic.
func Suggest(in Input) Output {
	out := Output{
		WindowSeconds: int(in.Window / time.Second),
		Suggestions:   []Suggestion{},
		Coverage:      Coverage{Skipped: map[string]int{}},
	}
	stmts := append([]Stmt(nil), in.Statements...)
	sort.SliceStable(stmts, func(i, j int) bool {
		if stmts[i].Database != stmts[j].Database {
			return stmts[i].Database < stmts[j].Database
		}
		return stmts[i].QueryID < stmts[j].QueryID
	})
	out.Coverage.StatementsConsidered = len(stmts)

	var cands []*candidate
	for si, st := range stmts {
		an := Parse(st.Text)
		skipped := map[string]bool{}
		for _, reason := range an.Skipped {
			skipped[string(reason)] = true
		}
		placed := false
		for _, group := range groupPredicates(an.Predicates) {
			c, why := buildCandidate(in, st.Database, group)
			if why != "" {
				skipped[why] = true
			}
			if c == nil {
				continue
			}
			placed = true
			c.stmts = []int{si}
			cands = append(cands, c)
		}
		if placed {
			out.Coverage.StatementsAnalysed++
		}
		for reason := range skipped {
			out.Coverage.Skipped[reason]++
		}
	}

	kept := cands[:0]
	var overlapping = map[*candidate][]string{}
	for _, c := range cands {
		covered, overlap := coverage(c, in.Indexes[c.key], stmts[c.stmts[0]].Text)
		if covered {
			continue
		}
		overlapping[c] = overlap
		kept = append(kept, c)
	}
	merged := mergeCandidates(kept)

	for _, c := range merged {
		out.Suggestions = append(out.Suggestions, score(in, stmts, c, overlapping))
	}
	sort.SliceStable(out.Suggestions, func(i, j int) bool {
		a, b := out.Suggestions[i], out.Suggestions[j]
		if a.WeightPct != b.WeightPct {
			return a.WeightPct > b.WeightPct
		}
		if a.Database != b.Database {
			return a.Database < b.Database
		}
		if a.Schema != b.Schema {
			return a.Schema < b.Schema
		}
		if a.Table != b.Table {
			return a.Table < b.Table
		}
		return columnsText(a.Columns) < columnsText(b.Columns)
	})
	limit := in.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	if len(out.Suggestions) > limit {
		out.Suggestions = out.Suggestions[:limit]
	}
	return out
}

type predGroup struct {
	schema, table string
	scope         int
	preds         []Predicate
}

func groupPredicates(preds []Predicate) []predGroup {
	var groups []predGroup
	index := map[string]int{}
	for _, p := range preds {
		id := p.Schema + "\x00" + p.Table + "\x00" + strconv.Itoa(p.Scope)
		i, ok := index[id]
		if !ok {
			i = len(groups)
			index[id] = i
			groups = append(groups, predGroup{schema: p.Schema, table: p.Table, scope: p.Scope})
		}
		groups[i].preds = append(groups[i].preds, p)
	}
	return groups
}

// resolve finds the table's key: an explicit schema is taken as is; otherwise
// the unique known schema, else public.
func resolve(in Input, db, schema, table string) Key {
	if schema != "" {
		return Key{db, schema, table}
	}
	var found []string
	for k := range in.Tables {
		if k.Database == db && k.Table == table {
			found = append(found, k.Schema)
		}
	}
	for k := range in.Indexes {
		if k.Database == db && k.Table == table {
			found = append(found, k.Schema)
		}
	}
	sort.Strings(found)
	for _, s := range found {
		if s == "public" {
			return Key{db, "public", table}
		}
	}
	if len(found) > 0 {
		return Key{db, found[0], table}
	}
	return Key{db, "public", table}
}

// buildCandidate applies the key rules to the predicates of one table in one
// query scope. The second result is the skip reason (small_table, not_table) or
// empty.
func buildCandidate(in Input, db string, g predGroup) (*candidate, string) {
	key := resolve(in, db, g.schema, g.table)
	meta, hasMeta := in.Tables[key]
	if in.IndexesComplete[db] {
		switch {
		case hasMeta && meta.IndexesUnparsed:
			return nil, NoteIndexUnparsed
		case !hasMeta || !meta.IndexesKnown:
			return nil, NoteNotTable
		}
	}
	// n_live_tup is 0 after a statistics reset or restore while reltuples (planner
	// estimate) is right: the small-table rule reads reltuples first, as the size does.
	rows := meta.RelTuples
	if rows <= 0 {
		rows = meta.LiveTuples
	}
	if hasMeta && meta.HasCounts && rows < SmallTableRows {
		return nil, NoteSmallTable
	}
	c := &candidate{key: key, notes: map[string]bool{}, scope: g.scope, strict: map[string]bool{}}
	usable := func(name string) bool {
		if !hasMeta || len(meta.Columns) == 0 {
			return true
		}
		col, ok := meta.Columns[name]
		if !ok {
			c.notes[NoteColumnUnknown] = true
			return false
		}
		if !btreeType(col.Type) {
			c.notes[NoteTypeNotBtree] = true
			return false
		}
		return true
	}
	var eq []string
	seen := map[string]bool{}
	var rangeCol, likeCol, orderCol string
	orderDesc := false
	for _, p := range g.preds {
		if p.Kind != KindOrder && p.Kind != KindIsNull {
			c.strict[p.Column] = true
		}
		switch p.Kind {
		case KindEq:
			if !seen[p.Column] && usable(p.Column) {
				seen[p.Column] = true
				eq = append(eq, p.Column)
			}
		case KindRange:
			if rangeCol == "" && usable(p.Column) {
				rangeCol = p.Column
			}
		case KindLikePrefix:
			if likeCol == "" && usable(p.Column) {
				likeCol = p.Column
			}
		case KindOrder:
			if orderCol == "" && p.Limit && usable(p.Column) {
				orderCol, orderDesc = p.Column, p.Dir == "desc"
			}
		}
	}
	sort.SliceStable(eq, func(i, j int) bool {
		return selectivityRank(meta, eq[i]) > selectivityRank(meta, eq[j])
	})
	var cols []KeyColumn
	for _, name := range eq {
		cols = append(cols, KeyColumn{Name: name})
	}
	if len(cols) > MaxKeyColumns {
		cols = cols[:MaxKeyColumns]
	}
	if len(cols) < MaxKeyColumns {
		trail, desc := "", false
		switch {
		case rangeCol != "":
			trail = rangeCol
		case likeCol != "":
			trail = likeCol
		case orderCol != "":
			trail, desc = orderCol, orderDesc
		}
		if trail != "" && !seen[trail] {
			cols = append(cols, KeyColumn{Name: trail, Desc: desc})
		}
	}
	if len(cols) == 0 {
		return nil, ""
	}
	c.cols = cols
	c.eqLead = min(len(eq), len(cols))
	return c, ""
}

// selectivityRank orders columns by distinct count; unknown ranks last.
func selectivityRank(meta TableMeta, name string) float64 {
	col, ok := meta.Columns[name]
	if !ok || col.NDistinct == nil || *col.NDistinct == 0 {
		return -1
	}
	nd := *col.NDistinct
	if nd < 0 {
		rows := meta.RelTuples
		if rows <= 0 {
			rows = meta.LiveTuples
		}
		return -nd * rows
	}
	return nd
}

// coverage reports whether an existing index serves the candidate, and the
// names of the indexes that share its first key column without covering it.
// A btree index covers when the candidate's key is a prefix of its columns; a
// single-column hash index covers a single-column equality candidate. A
// partial index covers only when its predicate holds for the statement: the
// text contains it, or it is "col IS NOT NULL" and the statement compares col
// with a strict operator (the planner proves non-null from it).
func coverage(c *candidate, defs []IndexDef, text string) (bool, []string) {
	var overlap []string
	for _, def := range defs {
		if uniqueServes(def, c, text) {
			return true, nil
		}
	}
	for _, def := range defs {
		if len(def.Columns) == 0 || def.Columns[0].Expr || def.Columns[0].Name != c.cols[0].Name {
			continue
		}
		isHash := strings.EqualFold(def.Method, "hash")
		if def.Method != "" && !isHash && !strings.EqualFold(def.Method, "btree") {
			continue
		}
		served := partialHolds(def.Where, c, text)
		if served && isHash {
			served = len(def.Columns) == 1 && len(c.cols) == 1 && c.eqLead == 1
		} else if served {
			served = len(def.Columns) >= len(c.cols)
			for i := 0; served && i < len(c.cols); i++ {
				if def.Columns[i].Expr || def.Columns[i].Name != c.cols[i].Name {
					served = false
				}
			}
		}
		if served {
			return true, nil
		}
		overlap = append(overlap, def.Name)
	}
	sort.Strings(overlap)
	return false, overlap
}

// uniqueServes reports whether a unique btree index already resolves the
// candidate's equality predicates to at most one row: every column of the index
// is an equality column of the candidate. Extra key columns after it add
// nothing (id = $1 AND active = $2 is served by the primary key on id).
func uniqueServes(def IndexDef, c *candidate, text string) bool {
	if !def.Unique || len(def.Columns) == 0 || (def.Method != "" && !strings.EqualFold(def.Method, "btree")) {
		return false
	}
	eq := map[string]bool{}
	for _, col := range c.cols[:min(c.eqLead, len(c.cols))] {
		eq[col.Name] = true
	}
	for _, col := range def.Columns {
		if col.Expr || !eq[col.Name] {
			return false
		}
	}
	return partialHolds(def.Where, c, text)
}

func partialHolds(where string, c *candidate, text string) bool {
	if where == "" {
		return true
	}
	pred := squash(trimOuterParens(where))
	if col, ok := strings.CutSuffix(pred, " is not null"); ok && c.strict[strings.Trim(col, `"`)] {
		return true
	}
	return strings.Contains(squash(text), pred)
}

func squash(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

func trimOuterParens(s string) string {
	s = strings.TrimSpace(s)
	for len(s) >= 2 && s[0] == '(' && s[len(s)-1] == ')' {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
}

// mergeCandidates folds identical candidates together and a candidate whose
// key is a strict prefix of another's into the longest such one.
func mergeCandidates(cands []*candidate) []*candidate {
	sort.SliceStable(cands, func(i, j int) bool {
		if len(cands[i].cols) != len(cands[j].cols) {
			return len(cands[i].cols) > len(cands[j].cols)
		}
		return cands[i].sig() < cands[j].sig()
	})
	var result []*candidate
	for _, c := range cands {
		target := -1
		for i, r := range result {
			if r.key == c.key && isPrefix(c.cols, r.cols) {
				target = i
				break
			}
		}
		if target < 0 {
			result = append(result, c)
			continue
		}
		r := result[target]
		r.stmts = append(r.stmts, c.stmts...)
		for col := range c.strict {
			r.strict[col] = true
		}
		for note := range c.notes {
			r.notes[note] = true
		}
	}
	return result
}

func isPrefix(short, long []KeyColumn) bool {
	if len(short) > len(long) {
		return false
	}
	for i := range short {
		if short[i].Name != long[i].Name {
			return false
		}
	}
	return true
}

func columnsText(cols []KeyColumn) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = c.Name
		if c.Desc {
			parts[i] += " DESC"
		}
	}
	return strings.Join(parts, ",")
}

func score(in Input, stmts []Stmt, c *candidate, overlapping map[*candidate][]string) Suggestion {
	meta, hasMeta := in.Tables[c.key]
	s := Suggestion{
		Database: c.key.Database, Schema: c.key.Schema, Table: c.key.Table,
		Columns:            c.cols,
		OverlappingIndexes: overlapping[c],
		Statements:         []StmtShare{},
	}
	if s.OverlappingIndexes == nil {
		s.OverlappingIndexes = []string{}
	}
	notes := map[string]bool{}
	for n := range c.notes {
		notes[n] = true
	}

	seen := map[int]bool{}
	var picked []int
	for _, si := range c.stmts {
		if !seen[si] {
			seen[si] = true
			picked = append(picked, si)
		}
	}
	for _, si := range picked {
		s.ReadCostMsPerS += stmts[si].ExecMsRate
		s.CallsRate += stmts[si].CallsRate
	}
	if total := in.TotalExecMsRate[c.key.Database]; total > 0 {
		s.WeightPct = s.ReadCostMsPerS / total * 100
	}
	sort.SliceStable(picked, func(i, j int) bool {
		a, b := stmts[picked[i]], stmts[picked[j]]
		if a.ExecMsRate != b.ExecMsRate {
			return a.ExecMsRate > b.ExecMsRate
		}
		return a.QueryID < b.QueryID
	})
	for _, si := range picked {
		share := 0.0
		if s.ReadCostMsPerS > 0 {
			share = stmts[si].ExecMsRate / s.ReadCostMsPerS * 100
		}
		s.Statements = append(s.Statements, StmtShare{QueryID: stmts[si].QueryID, SharePct: share, QueryText: truncateText(stmts[si].Text)})
	}

	s.Selectivity = selectivityOf(meta, hasMeta, c.cols[0].Name)
	if s.Selectivity.Label == "unknown" {
		notes[NoteSelectivityUnknown] = true
	}
	size, sizeNotes := estimateSize(meta, hasMeta, c.cols)
	s.EstSizeBytes = size
	for _, n := range sizeNotes {
		notes[n] = true
	}

	writesKnown := hasMeta && meta.HasWrites
	if !writesKnown {
		notes[NoteWritesUnknown] = true
	}
	w := meta.Writes
	s.Write = WriteRates{InsertRate: w.Ins, UpdateRate: w.Upd, DeleteRate: w.Del}
	if w.Upd > 0 {
		s.Write.HotUpdateRatio = w.HotUpd / w.Upd
	}
	s.ReadWriteRatio = readWriteRatio(s.CallsRate, w)
	s.Verdict = verdictFor(s.ReadWriteRatio, s.Selectivity.Label == "low")
	if !writesKnown && s.Verdict == VerdictStrong {
		s.Verdict = VerdictModerate
	}
	// Without writes to pay for, a low call rate never makes the index write-heavy.
	if s.Verdict == VerdictWriteHeavy && noIndexWrites(w, writesKnown) {
		s.Verdict = VerdictModerate
	}
	if s.Selectivity.Label == "low" {
		notes[NoteLowSelectivity] = true
	}
	switch s.Selectivity.Label {
	case "unknown":
		s.Confidence = "low"
	case "high":
		s.Confidence = "high"
	default:
		s.Confidence = "medium"
	}

	if !writesKnown && s.Confidence == "high" {
		s.Confidence = "medium"
	}
	s.Notes = make([]string, 0, len(notes))
	for n := range notes {
		s.Notes = append(s.Notes, n)
	}
	sort.Strings(s.Notes)
	s.CreateStatement = createStatement(c.key, c.cols)
	sum := sha256.Sum256([]byte(c.key.Database + "|" + c.key.Schema + "|" + c.key.Table + "|" + columnsText(c.cols)))
	s.ID = hex.EncodeToString(sum[:])[:12]
	return s
}

func truncateText(text string) string {
	if utf8.RuneCountInString(text) <= maxTextChars {
		return text
	}
	return string([]rune(text)[:maxTextChars])
}
