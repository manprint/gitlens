package indexadvice

import "strings"

// PredKind is the access shape of one predicate.
type PredKind string

const (
	KindEq         PredKind = "eq"
	KindRange      PredKind = "range"
	KindLikePrefix PredKind = "like_prefix"
	KindIsNull     PredKind = "isnull"
	KindOrder      PredKind = "order"
)

// SkipReason says why a construct was not analysed.
type SkipReason string

const (
	SkipOrGroup              SkipReason = "or_group"
	SkipExpression           SkipReason = "expression"
	SkipUnresolvedAlias      SkipReason = "unresolved_alias"
	SkipParseError           SkipReason = "parse_error"
	SkipUnsupportedStatement SkipReason = "unsupported_statement"
	SkipCatalog              SkipReason = "catalog"
)

// MaxInput is the largest statement Parse analyses; a longer one is a parse_error.
const MaxInput = 64 << 10

const maxDepth = 48

// TableRef is a relation named in FROM, JOIN, UPDATE or DELETE FROM.
type TableRef struct {
	Schema string `json:"schema,omitempty"`
	Name   string `json:"name"`
	Alias  string `json:"alias,omitempty"`
	Scope  int    `json:"scope"`
}

// Predicate is a condition (or ORDER BY column) on one column of one table.
// Dir is "asc" or "desc" for KindOrder only; Limit is set on KindOrder when
// the same query level carries LIMIT.
type Predicate struct {
	Schema string   `json:"schema,omitempty"`
	Table  string   `json:"table"`
	Column string   `json:"column"`
	Kind   PredKind `json:"kind"`
	Dir    string   `json:"dir,omitempty"`
	Limit  bool     `json:"limit,omitempty"`
	Scope  int      `json:"scope"`
}

// Analysis is the result of Parse. Statement is the first statement's verb in
// lower case ("select", "update", ...), empty for empty input.
type Analysis struct {
	Statement  string       `json:"statement"`
	Tables     []TableRef   `json:"tables"`
	Predicates []Predicate  `json:"predicates"`
	Skipped    []SkipReason `json:"skipped"`
}

// SkipCounts counts the skipped constructs by reason.
func (a Analysis) SkipCounts() map[SkipReason]int {
	counts := map[SkipReason]int{}
	for _, r := range a.Skipped {
		counts[r]++
	}
	return counts
}

type parser struct {
	out    Analysis
	scopes int
	ctes   []string
	depth  int
	failed bool
}

// Parse analyses sql. It is total: any input returns, and input that cannot
// be tokenised or is longer than MaxInput yields one parse_error skip.
func Parse(sql string) Analysis {
	p := &parser{out: Analysis{Tables: []TableRef{}, Predicates: []Predicate{}, Skipped: []SkipReason{}}}
	if len(sql) > MaxInput {
		p.out.Skipped = append(p.out.Skipped, SkipParseError)
		return p.out
	}
	toks, err := lex(sql)
	if err != nil || !balanced(toks) {
		p.out.Skipped = append(p.out.Skipped, SkipParseError)
		return p.out
	}
	first := true
	for _, stmt := range splitStatements(toks) {
		if len(stmt) == 0 {
			continue
		}
		p.statement(stmt, first)
		first = false
		if p.failed {
			return Analysis{Tables: []TableRef{}, Predicates: []Predicate{}, Skipped: []SkipReason{SkipParseError}}
		}
	}
	return p.out
}

func balanced(toks []token) bool {
	depth := 0
	for _, t := range toks {
		switch t.kind {
		case tLParen:
			depth++
		case tRParen:
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	return depth == 0
}

func splitStatements(toks []token) [][]token {
	var out [][]token
	start, depth := 0, 0
	for i, t := range toks {
		switch t.kind {
		case tLParen:
			depth++
		case tRParen:
			depth--
		case tSemi:
			if depth == 0 {
				out = append(out, toks[start:i])
				start = i + 1
			}
		}
	}
	return append(out, toks[start:])
}

func (t token) is(word string) bool { return t.kind == tIdent && !t.quoted && t.text == word }

func (p *parser) skip(r SkipReason) { p.out.Skipped = append(p.out.Skipped, r) }

func (p *parser) statement(toks []token, first bool) {
	verb := "other"
	if toks[0].kind == tIdent && !toks[0].quoted {
		verb = toks[0].text
	} else if toks[0].kind == tLParen {
		verb = "select"
	}
	if first {
		p.out.Statement = verb
	}
	switch verb {
	case "select", "with", "values", "table":
		p.query(toks)
	case "update":
		p.update(toks)
	case "delete":
		p.deleteFrom(toks)
	case "insert":
		p.insert(toks)
	default:
		p.skip(SkipUnsupportedStatement)
	}
}

func (p *parser) enter() bool {
	p.depth++
	if p.depth > maxDepth {
		p.failed = true
		return false
	}
	return true
}

func (p *parser) leave() { p.depth-- }

// matching returns the index of the parenthesis closing the one at toks[open].
func matching(toks []token, open int) int {
	depth := 0
	for i := open; i < len(toks); i++ {
		switch toks[i].kind {
		case tLParen:
			depth++
		case tRParen:
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return len(toks) - 1
}

// query analyses a SELECT-like statement: optional WITH, set operations,
// parenthesised queries.
func (p *parser) query(toks []token) {
	if !p.enter() {
		return
	}
	defer p.leave()
	mark := len(p.ctes)
	defer func() { p.ctes = p.ctes[:mark] }()
	if len(toks) > 0 && toks[0].is("with") {
		toks = p.withClause(toks[1:])
	}
	for _, part := range splitSetOps(toks) {
		for len(part) >= 2 && part[0].kind == tLParen && matching(part, 0) == len(part)-1 {
			part = part[1 : len(part)-1]
		}
		if len(part) == 0 {
			continue
		}
		switch {
		case part[0].is("select"):
			p.selectScope(part)
		case part[0].is("with"), part[0].kind == tLParen:
			p.query(part)
		}
	}
}

// withClause registers the CTE names, analyses each CTE body as its own scope
// and returns the tokens of the statement that follows the CTE list.
func (p *parser) withClause(toks []token) []token {
	i := 0
	if i < len(toks) && toks[i].is("recursive") {
		i++
	}
	for i < len(toks) {
		if toks[i].kind != tIdent {
			return toks[i:]
		}
		p.ctes = append(p.ctes, toks[i].text)
		i++
		if i < len(toks) && toks[i].kind == tLParen {
			i = matching(toks, i) + 1
		}
		if i < len(toks) && toks[i].is("as") {
			i++
		}
		if i < len(toks) && toks[i].is("not") {
			i++
		}
		if i < len(toks) && toks[i].is("materialized") {
			i++
		}
		if i >= len(toks) || toks[i].kind != tLParen {
			return toks[min(i, len(toks)):]
		}
		end := matching(toks, i)
		p.query(toks[i+1 : end])
		i = end + 1
		if i < len(toks) && toks[i].kind == tComma {
			i++
			continue
		}
		break
	}
	return toks[min(i, len(toks)):]
}

func splitSetOps(toks []token) [][]token {
	var out [][]token
	start, depth := 0, 0
	for i, t := range toks {
		switch t.kind {
		case tLParen:
			depth++
		case tRParen:
			depth--
		case tIdent:
			if depth == 0 && !t.quoted && (t.text == "union" || t.text == "intersect" || t.text == "except") {
				out = append(out, toks[start:i])
				start = i + 1
				for start < len(toks) && (toks[start].is("all") || toks[start].is("distinct")) {
					start++
				}
			}
		}
	}
	return append(out, toks[min(start, len(toks)):])
}

// clauses cuts a statement into its depth-0 clauses by leading keyword.
func clauses(toks []token, keywords map[string]bool) map[string][]token {
	out := map[string][]token{}
	cur := ""
	start := 0
	depth := 0
	flush := func(end int) {
		if cur != "" && start <= end {
			out[cur] = toks[start:end]
		}
	}
	for i, t := range toks {
		switch t.kind {
		case tLParen:
			depth++
			continue
		case tRParen:
			depth--
			continue
		}
		if depth != 0 || t.kind != tIdent || t.quoted || !keywords[t.text] {
			continue
		}
		if t.text == "from" && i > 0 && toks[i-1].is("distinct") {
			continue
		}
		name := t.text
		next := i + 1
		if (name == "group" || name == "order") && next < len(toks) && toks[next].is("by") {
			next++
		}
		if i == 0 && name != "select" && name != "update" && name != "delete" {
			continue
		}
		flush(i)
		cur, start = name, next
		if name == "order" {
			cur = "order"
		}
	}
	flush(len(toks))
	return out
}

var selectKeywords = map[string]bool{"select": true, "from": true, "where": true, "group": true, "having": true, "window": true, "order": true, "limit": true, "offset": true, "fetch": true, "for": true}

type relScope struct {
	id      int
	tables  []int // indexes into parser.out.Tables
	virtual int
	catalog int
	aliases map[string]int // alias or bare name → index in out.Tables, -1 for virtual and catalog
}

func (p *parser) newScope() *relScope {
	p.scopes++
	return &relScope{id: p.scopes, aliases: map[string]int{}}
}

func (p *parser) selectScope(toks []token) {
	sc := p.newScope()
	cl := clauses(toks, selectKeywords)
	if from, ok := cl["from"]; ok {
		p.fromClause(sc, from)
	}
	if where, ok := cl["where"]; ok {
		p.conditions(sc, where, 0)
	}
	_, hasLimit := cl["limit"]
	if !hasLimit {
		_, hasLimit = cl["fetch"]
	}
	if order, ok := cl["order"]; ok {
		p.orderBy(sc, order, hasLimit)
	}
	p.finishScope(sc)
	for _, name := range []string{"select", "from", "where", "group", "having", "order", "window", "limit", "offset"} {
		p.children(cl[name])
	}
}

// finishScope records the catalog-only statement skip.
func (p *parser) finishScope(sc *relScope) {
	if sc.catalog > 0 && len(sc.tables) == 0 && sc.virtual == 0 {
		p.skip(SkipCatalog)
	}
}

// children analyses every outermost parenthesised sub-select found in toks.
func (p *parser) children(toks []token) {
	for i := 0; i < len(toks); i++ {
		if toks[i].kind != tLParen {
			continue
		}
		end := matching(toks, i)
		if i+1 < end && (toks[i+1].is("select") || toks[i+1].is("with") || toks[i+1].is("values")) {
			p.query(toks[i+1 : end])
			i = end
		}
	}
}

func isCatalog(schema, name string) bool {
	// A pg_-prefixed relation in any schema is a catalog or an extension view such as
	// pg_stat_statements installed in public: no index can be added to it.
	return schema == "pg_catalog" || schema == "information_schema" || strings.HasPrefix(name, "pg_")
}

var joinWords = map[string]bool{"join": true, "inner": true, "left": true, "right": true, "full": true, "cross": true, "natural": true}

var aliasStop = map[string]bool{"on": true, "using": true, "join": true, "inner": true, "left": true, "right": true, "full": true, "cross": true, "natural": true, "tablesample": true, "where": true, "lateral": true}

func (p *parser) fromClause(sc *relScope, toks []token) {
	i := 0
	for i < len(toks) {
		i = p.fromItem(sc, toks, i)
		for i < len(toks) {
			t := toks[i]
			switch {
			case t.kind == tComma:
				i++
				goto next
			case t.is("on"):
				j := i + 1
				depth := 0
				for j < len(toks) {
					u := toks[j]
					if u.kind == tLParen {
						depth++
					} else if u.kind == tRParen {
						depth--
					} else if depth == 0 && (u.kind == tComma || u.is("join") || (u.kind == tIdent && !u.quoted && joinWords[u.text] && (j+1 >= len(toks) || toks[j+1].kind != tLParen))) {
						break
					}
					j++
				}
				p.conditions(sc, toks[i+1:j], 0)
				i = j
			case t.is("using"):
				i++
				if i < len(toks) && toks[i].kind == tLParen {
					i = matching(toks, i)
				}
				i++
			case t.is("join"):
				i++
				goto next
			case t.kind == tIdent && !t.quoted && (joinWords[t.text] || t.text == "outer" || t.text == "lateral"):
				i++
			case t.kind == tLParen:
				i = matching(toks, i) + 1
			default:
				i++
			}
		}
	next:
	}
}

// fromItem reads one FROM item starting at toks[i] and returns the index after it.
func (p *parser) fromItem(sc *relScope, toks []token, i int) int {
	for i < len(toks) && (toks[i].is("lateral") || toks[i].is("only")) {
		i++
	}
	if i >= len(toks) {
		return i
	}
	registerVirtual := func(i int) int {
		if i+1 < len(toks) && toks[i].is("with") && toks[i+1].is("ordinality") {
			i += 2
		}
		if i < len(toks) && toks[i].is("as") {
			i++
		}
		if i < len(toks) && toks[i].kind == tIdent && (toks[i].quoted || !aliasStop[toks[i].text]) {
			sc.aliases[toks[i].text] = -1
			sc.virtual++
			i++
			if i < len(toks) && toks[i].kind == tLParen {
				i = matching(toks, i) + 1
			}
		} else {
			sc.virtual++
		}
		return i
	}
	if toks[i].kind == tLParen {
		return registerVirtual(matching(toks, i) + 1)
	}
	if toks[i].kind != tIdent {
		return i + 1
	}
	if i+1 < len(toks) && toks[i+1].kind == tLParen {
		return registerVirtual(matching(toks, i+1) + 1)
	}
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
	if i < len(toks) && toks[i].kind == tLParen {
		return registerVirtual(matching(toks, i) + 1)
	}
	name := parts[len(parts)-1]
	schema := ""
	if len(parts) >= 2 {
		schema = parts[len(parts)-2]
	}
	alias := ""
	j := i
	if j < len(toks) && toks[j].is("as") {
		j++
	}
	if j < len(toks) && toks[j].kind == tIdent && (toks[j].quoted || !aliasStop[toks[j].text]) {
		alias = toks[j].text
		i = j + 1
		if i < len(toks) && toks[i].kind == tLParen {
			i = matching(toks, i) + 1
		}
	}
	if schema == "" && p.isCTE(name) {
		sc.virtual++
		if alias != "" {
			sc.aliases[alias] = -1
		}
		sc.aliases[name] = -1
		return i
	}
	if isCatalog(schema, name) {
		sc.catalog++
		if alias != "" {
			sc.aliases[alias] = -1
		}
		sc.aliases[name] = -1
		return i
	}
	p.out.Tables = append(p.out.Tables, TableRef{Schema: schema, Name: name, Alias: alias, Scope: sc.id})
	idx := len(p.out.Tables) - 1
	sc.tables = append(sc.tables, idx)
	if alias != "" {
		sc.aliases[alias] = idx
	} else {
		sc.aliases[name] = idx
	}
	return i
}

func (p *parser) isCTE(name string) bool {
	for _, c := range p.ctes {
		if c == name {
			return true
		}
	}
	return false
}

type resolution int

const (
	resOK resolution = iota
	resDrop
	resUnresolved
)

// resolve maps a column reference (qualifier parts and column) to its table.
func (p *parser) resolve(sc *relScope, qual []string) (TableRef, resolution) {
	switch len(qual) {
	case 0:
		if len(sc.tables) == 1 && sc.virtual == 0 && sc.catalog == 0 {
			return p.out.Tables[sc.tables[0]], resOK
		}
		if len(sc.tables) == 0 && sc.virtual+sc.catalog > 0 {
			return TableRef{}, resDrop
		}
		return TableRef{}, resUnresolved
	case 1:
		idx, ok := sc.aliases[qual[0]]
		switch {
		case !ok:
			return TableRef{}, resUnresolved
		case idx < 0:
			return TableRef{}, resDrop
		}
		return p.out.Tables[idx], resOK
	default:
		schema, name := qual[len(qual)-2], qual[len(qual)-1]
		for _, idx := range sc.tables {
			if t := p.out.Tables[idx]; t.Schema == schema && t.Name == name {
				return t, resOK
			}
		}
		if isCatalog(schema, name) {
			return TableRef{}, resDrop
		}
		return TableRef{}, resUnresolved
	}
}

func (p *parser) emit(sc *relScope, qual []string, column string, kind PredKind, dir string, limit bool) {
	t, res := p.resolve(sc, qual)
	switch res {
	case resUnresolved:
		p.skip(SkipUnresolvedAlias)
	case resOK:
		p.out.Predicates = append(p.out.Predicates, Predicate{Schema: t.Schema, Table: t.Name, Column: column, Kind: kind, Dir: dir, Limit: limit, Scope: sc.id})
	}
}

// splitTop splits toks at depth-0 tokens for which sep reports true.
func splitTop(toks []token, sep func(i int) bool) [][]token {
	var out [][]token
	start, depth := 0, 0
	for i, t := range toks {
		switch t.kind {
		case tLParen:
			depth++
		case tRParen:
			depth--
		}
		if depth == 0 && sep(i) {
			out = append(out, toks[start:i])
			start = i + 1
		}
	}
	return append(out, toks[start:])
}

// conditions extracts predicates from a WHERE / ON expression.
func (p *parser) conditions(sc *relScope, toks []token, level int) {
	if len(toks) == 0 {
		return
	}
	if !p.enter() {
		return
	}
	defer p.leave()
	var parts [][]token
	hasOr := false
	between := 0
	depth := 0
	start := 0
	for i, t := range toks {
		switch t.kind {
		case tLParen:
			depth++
		case tRParen:
			depth--
		case tIdent:
			if depth != 0 || t.quoted {
				continue
			}
			switch t.text {
			case "or":
				hasOr = true
			case "between":
				between++
			case "and":
				if between > 0 {
					between--
					continue
				}
				parts = append(parts, toks[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, toks[start:])
	if hasOr {
		p.skip(SkipOrGroup)
		return
	}
	for _, c := range parts {
		p.conjunct(sc, c, level)
	}
}

type colRef struct {
	qual   []string
	column string
	next   int // index of the first token after the reference
}

// columnAt reads [qual.]*column at toks[i]; ok is false when toks[i] does not
// start a plain column reference (a function call is not one).
func columnAt(toks []token, i int) (colRef, bool) {
	var parts []string
	j := i
	for j < len(toks) && toks[j].kind == tIdent {
		if !toks[j].quoted && reserved[toks[j].text] {
			return colRef{}, false
		}
		parts = append(parts, toks[j].text)
		j++
		if j+1 < len(toks) && toks[j].kind == tDot && toks[j+1].kind == tIdent {
			j++
			continue
		}
		break
	}
	if len(parts) == 0 || len(parts) > 4 {
		return colRef{}, false
	}
	if j < len(toks) && toks[j].kind == tLParen {
		return colRef{}, false
	}
	return colRef{qual: parts[:len(parts)-1], column: parts[len(parts)-1], next: j}, true
}

var reserved = map[string]bool{"not": true, "exists": true, "case": true, "null": true, "true": true, "false": true, "interval": true, "date": true, "timestamp": true, "array": true, "row": true, "any": true, "all": true, "some": true, "cast": true, "current_date": true, "current_timestamp": true, "select": true, "extract": true}

func stripParens(toks []token) []token {
	for len(toks) >= 2 && toks[0].kind == tLParen && matching(toks, 0) == len(toks)-1 {
		toks = toks[1 : len(toks)-1]
	}
	return toks
}

func (p *parser) conjunct(sc *relScope, toks []token, level int) {
	if len(toks) == 0 {
		return
	}
	inner := stripParens(toks)
	if len(inner) != len(toks) {
		p.conditions(sc, inner, level+1)
		return
	}
	if toks[0].is("not") {
		p.skip(SkipExpression)
		return
	}
	if toks[0].is("exists") {
		return
	}
	left, okLeft := columnAt(toks, 0)
	if !okLeft {
		if p.reversed(sc, toks) {
			return
		}
		p.skip(SkipExpression)
		return
	}
	rest := toks[left.next:]
	if len(rest) == 0 {
		p.skip(SkipExpression)
		return
	}
	op := rest[0]
	after := rest[1:]
	switch {
	case op.kind == tOp && op.text == "::":
		p.skip(SkipExpression)
	case op.kind == tOp && op.text == "=":
		if len(after) > 0 && (after[0].is("all")) {
			p.skip(SkipExpression)
			return
		}
		p.emit(sc, left.qual, left.column, KindEq, "", false)
		p.joinSide(sc, after)
	case op.kind == tOp && (op.text == "<" || op.text == "<=" || op.text == ">" || op.text == ">="):
		p.emit(sc, left.qual, left.column, KindRange, "", false)
		p.joinSide(sc, after)
	case op.kind == tOp && op.text == "~~", op.is("like"):
		p.likeOperand(sc, left, after)
	case op.is("in"):
		p.emit(sc, left.qual, left.column, KindEq, "", false)
	case op.is("between"):
		p.emit(sc, left.qual, left.column, KindRange, "", false)
	case op.is("is"):
		switch {
		case len(after) == 1 && after[0].is("null"):
			p.emit(sc, left.qual, left.column, KindIsNull, "", false)
		case len(after) == 2 && after[0].is("not") && after[1].is("null"):
		default:
			p.skip(SkipExpression)
		}
	default:
		p.skip(SkipExpression)
	}
}

func (p *parser) likeOperand(sc *relScope, left colRef, after []token) {
	if len(after) > 0 && after[0].kind == tString && after[0].text != "" && after[0].text[0] != '%' && after[0].text[0] != '_' {
		p.emit(sc, left.qual, left.column, KindLikePrefix, "", false)
	}
}

// joinSide emits the right-hand column of an equality or range comparison
// between two columns (a join condition): both sides can use an index.
func (p *parser) joinSide(sc *relScope, after []token) {
	if right, ok := columnAt(after, 0); ok && right.next == len(after) {
		p.emit(sc, right.qual, right.column, KindEq, "", false)
	}
}

// reversed handles `$1 = col` and `'x' < col`: a constant on the left of a
// comparison with a plain column on the right.
func (p *parser) reversed(sc *relScope, toks []token) bool {
	if len(toks) < 3 {
		return false
	}
	if k := toks[0].kind; k != tParam && k != tString && k != tNumber {
		return false
	}
	op := toks[1]
	right, ok := columnAt(toks, 2)
	if !ok || right.next != len(toks) || op.kind != tOp {
		return false
	}
	switch op.text {
	case "=":
		p.emit(sc, right.qual, right.column, KindEq, "", false)
	case "<", "<=", ">", ">=":
		p.emit(sc, right.qual, right.column, KindRange, "", false)
	default:
		return false
	}
	return true
}

func (p *parser) orderBy(sc *relScope, toks []token, limit bool) {
	for _, item := range splitTop(toks, func(i int) bool { return toks[i].kind == tComma }) {
		if len(item) == 0 {
			continue
		}
		if item[0].kind == tNumber {
			continue
		}
		col, ok := columnAt(item, 0)
		if !ok {
			p.skip(SkipExpression)
			continue
		}
		rest := item[col.next:]
		dir := "asc"
		if len(rest) > 0 && rest[0].is("desc") {
			dir = "desc"
			rest = rest[1:]
		} else if len(rest) > 0 && rest[0].is("asc") {
			rest = rest[1:]
		}
		if len(rest) >= 1 && rest[0].is("nulls") {
			rest = rest[min(2, len(rest)):]
		}
		if len(rest) > 0 {
			p.skip(SkipExpression)
			continue
		}
		p.emit(sc, col.qual, col.column, KindOrder, dir, limit)
	}
}

func (p *parser) update(toks []token) {
	if !p.enter() {
		return
	}
	defer p.leave()
	sc := p.newScope()
	cl := clauses(toks, map[string]bool{"update": true, "set": true, "from": true, "where": true, "returning": true})
	if head, ok := cl["update"]; ok {
		p.fromItem(sc, head, 0)
	}
	if from, ok := cl["from"]; ok {
		p.fromClause(sc, from)
	}
	if where, ok := cl["where"]; ok {
		p.conditions(sc, where, 0)
	}
	p.finishScope(sc)
	for _, name := range []string{"set", "from", "where", "returning"} {
		p.children(cl[name])
	}
}

func (p *parser) deleteFrom(toks []token) {
	if !p.enter() {
		return
	}
	defer p.leave()
	sc := p.newScope()
	cl := clauses(toks, map[string]bool{"delete": true, "from": true, "using": true, "where": true, "returning": true})
	if head, ok := cl["from"]; ok {
		p.fromItem(sc, head, 0)
	}
	if using, ok := cl["using"]; ok {
		p.fromClause(sc, using)
	}
	if where, ok := cl["where"]; ok {
		p.conditions(sc, where, 0)
	}
	p.finishScope(sc)
	for _, name := range []string{"using", "where", "returning"} {
		p.children(cl[name])
	}
}

// insert analyses the query of INSERT ... SELECT and the sub-selects of VALUES;
// the target table of an INSERT is never an access path.
func (p *parser) insert(toks []token) {
	if !p.enter() {
		return
	}
	defer p.leave()
	for i := 0; i < len(toks); i++ {
		if toks[i].kind == tLParen {
			end := matching(toks, i)
			if i+1 < end && (toks[i+1].is("select") || toks[i+1].is("with") || toks[i+1].is("values")) {
				p.query(toks[i+1 : end])
			}
			i = end
			continue
		}
		if toks[i].is("select") || toks[i].is("with") {
			end := len(toks)
			for j := i; j < len(toks); j++ {
				if toks[j].is("on") && j+1 < len(toks) && toks[j+1].is("conflict") {
					end = j
					break
				}
			}
			p.query(toks[i:end])
			return
		}
	}
}
