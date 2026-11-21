package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/manprint/pglens/internal/check"
)

// The code the EXPLAIN guard reads. A function the database defines is not
// refused for being user-defined: the guard reads it. A function in SQL or
// PL/pgSQL is admitted when its body cannot act outside the read-only
// transaction: it builds no SQL at run time (EXECUTE, DO), runs no COPY, and
// every function it calls and every view, foreign table or table with row
// security policies it names is admitted in turn. Writes need no check of
// their own: the READ ONLY transaction refuses them inside PostgreSQL, and
// what the transaction does not contain is a call (dblink, pg_notify,
// pg_terminate_backend, an advisory lock, a procedural language that reaches
// other systems), which the call check finds.
//
// The check is conservative where it cannot be exact. An unqualified call
// matches the functions of that name in every schema, and every overload: one
// that is not admitted makes the name refused. A word in a body that names a
// view, a foreign table or a table with policies counts as reading it, even
// when the word is a column or a variable of the same name. A function in any
// other language, C included, is refused: pglens cannot read its code.

// codeLimit bounds how many names the guard reads before it gives up.
const codeLimit = 20000

// codeFunction is one pg_proc row with what the guard needs of its code.
type codeFunction struct {
	catalogFunction
	Kind     string  `json:"kind"`
	Language string  `json:"language"`
	Body     *string `json:"body"`
}

// codeRelation is a relation that runs code when it is read: a view, a
// foreign table, or a table with row security policies (definition holds
// their expressions).
type codeRelation struct {
	Schema     string `json:"schema"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Definition string `json:"definition"`
}

// codeFunctionLookup reads every function named in $1, with the body of the
// SQL and PL/pgSQL functions the database defines itself. A SQL-standard body
// (BEGIN ATOMIC, PostgreSQL 14+) has no source text and is deparsed.
func codeFunctionLookup(major int) string {
	body := "p.prosrc"
	if major >= 14 {
		body = "CASE WHEN p.prosqlbody IS NULL THEN p.prosrc ELSE pg_catalog.pg_get_functiondef(p.oid) END"
	}
	return `/* pglens: explain guard, code */
SELECT COALESCE(pg_catalog.json_agg(pg_catalog.json_build_object(
		'schema', n.nspname,
		'name', p.proname,
		'volatility', p.provolatile,
		'extension', COALESCE(e.extname, ''),
		'kind', p.prokind,
		'language', l.lanname,
		'body', CASE WHEN e.extname IS NULL AND n.nspname NOT IN ('pg_catalog', 'information_schema')
		              AND l.lanname IN ('sql', 'plpgsql') THEN ` + body + ` END)), '[]')
FROM pg_catalog.pg_proc p
JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
JOIN pg_catalog.pg_language l ON l.oid = p.prolang
JOIN pg_catalog.pg_roles ow ON ow.oid = p.proowner
LEFT JOIN pg_catalog.pg_depend d
	ON d.classid = 'pg_catalog.pg_proc'::pg_catalog.regclass AND d.objid = p.oid AND d.deptype = 'e'
LEFT JOIN pg_catalog.pg_extension e ON e.oid = d.refobjid AND ow.rolsuper
WHERE p.proname = ANY ($1::pg_catalog.name[])`
}

// codeRelationLookup reads the relations named in $1 that run code when they
// are read. Built-in views only call built-in functions, which the plan check
// sees, so the catalog schemas are left out.
const codeRelationLookup = `/* pglens: explain guard, relations */
SELECT COALESCE(pg_catalog.json_agg(pg_catalog.json_build_object(
		'schema', n.nspname,
		'name', c.relname,
		'kind', c.relkind,
		'definition', CASE WHEN c.relkind = 'v' THEN pg_catalog.pg_get_viewdef(c.oid)
		                   ELSE (SELECT pg_catalog.string_agg(
		                                COALESCE(pg_catalog.pg_get_expr(p.polqual, p.polrelid), '') || ' ' ||
		                                COALESCE(pg_catalog.pg_get_expr(p.polwithcheck, p.polrelid), ''), ' ')
		                           FROM pg_catalog.pg_policy p WHERE p.polrelid = c.oid) END)), '[]')
FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE c.relname = ANY ($1::pg_catalog.name[])
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND (c.relkind IN ('v', 'f')
       OR (c.relrowsecurity AND c.relkind IN ('r', 'p')
           AND EXISTS (SELECT 1 FROM pg_catalog.pg_policy p WHERE p.polrelid = c.oid)))`

type codeEdge struct {
	key  string
	verb string
}

type codeNode struct {
	schema string
	label  string
	// reason is the node's own hazard; via is the edge its hazard comes
	// through when it has none of its own.
	reason string
	calls  []funcRef
	words  []string
	hazard bool
	via    *codeEdge
}

// codeGraph is the code one EXPLAIN may reach, read on demand and kept for
// the command: guardPlanning and guardAnalyze share it.
type codeGraph struct {
	conn      check.Conn
	major     int
	nodes     map[string]*codeNode
	functions map[string][]string
	relations map[string][]string
	seenFn    map[string]bool
	seenRel   map[string]bool
}

func newCodeGraph(conn check.Conn, major int) *codeGraph {
	return &codeGraph{
		conn: conn, major: major,
		nodes:     map[string]*codeNode{},
		functions: map[string][]string{},
		relations: map[string][]string{},
		seenFn:    map[string]bool{},
		seenRel:   map[string]bool{},
	}
}

func isUserFunction(f catalogFunction) bool {
	return f.Extension == "" && f.Schema != "pg_catalog" && f.Schema != "information_schema"
}

// hazard reports why f could act outside the transaction, or "". The
// function must have been read with ensure.
func (g *codeGraph) hazard(f catalogFunction) string {
	if !isUserFunction(f) {
		return functionHazard(f)
	}
	n := g.nodes["f:"+f.Schema+"."+f.Name]
	if n == nil {
		return "is a user-defined function that pglens could not read"
	}
	if !n.hazard {
		return ""
	}
	return g.describe(n)
}

// describe follows a hazard to where it starts: "calls app.f(), which reads
// view app.v, which calls dblink.dblink_exec(), which belongs to ...".
func (g *codeGraph) describe(n *codeNode) string {
	var b strings.Builder
	for hops := 0; n.reason == "" && n.via != nil; hops++ {
		next := g.nodes[n.via.key]
		if hops < 6 {
			fmt.Fprintf(&b, "%s %s, which ", n.via.verb, next.label)
		}
		n = next
	}
	b.WriteString(n.reason)
	return b.String()
}

// ensure reads the functions named in names and everything their code
// reaches, then decides which of them are admitted.
func (g *codeGraph) ensure(ctx context.Context, names []string) error {
	fnPending := fresh(names, g.seenFn)
	var relPending []string
	for len(fnPending) > 0 || len(relPending) > 0 {
		if len(g.seenFn)+len(g.seenRel) > codeLimit {
			return rejectedf("EXPLAIN refused: the functions and views the database may run are too many to check (more than %d names)", codeLimit)
		}
		var nextFn, nextRel []string
		if len(fnPending) > 0 {
			var functions []codeFunction
			if err := queryJSON(ctx, g.conn, codeFunctionLookup(g.major), &functions, fnPending); err != nil {
				return fmt.Errorf("read functions for the explain guard: %w", err)
			}
			for _, f := range functions {
				calls, words := g.addFunction(f)
				nextFn = append(nextFn, fresh(callNames(calls), g.seenFn)...)
				nextRel = append(nextRel, fresh(words, g.seenRel)...)
			}
		}
		if len(relPending) > 0 {
			var relations []codeRelation
			if err := queryJSON(ctx, g.conn, codeRelationLookup, &relations, relPending); err != nil {
				return fmt.Errorf("read views and policies for the explain guard: %w", err)
			}
			for _, r := range relations {
				calls, words := g.addRelation(r)
				nextFn = append(nextFn, fresh(callNames(calls), g.seenFn)...)
				nextRel = append(nextRel, fresh(words, g.seenRel)...)
			}
		}
		fnPending, relPending = nextFn, nextRel
	}
	g.solve()
	return nil
}

func (g *codeGraph) node(key, schema, label string, index map[string][]string, name string) *codeNode {
	n := g.nodes[key]
	if n == nil {
		n = &codeNode{schema: schema, label: label}
		g.nodes[key] = n
		index[name] = append(index[name], key)
	}
	return n
}

func (g *codeGraph) addFunction(f codeFunction) (calls []funcRef, words []string) {
	n := g.node("f:"+f.Schema+"."+f.Name, f.Schema, f.String(), g.functions, f.Name)
	var reason string
	switch {
	case !isUserFunction(f.catalogFunction):
		reason = functionHazard(f.catalogFunction)
	case f.Kind == "a":
		// An aggregate runs its support functions, which the hidden-call
		// check reads.
	case f.Language == "sql" || f.Language == "plpgsql":
		if f.Body == nil {
			reason = "has a body pglens cannot read"
			break
		}
		facts := scanCode(*f.Body, false)
		reason = dynamicReason(facts.dynamic)
		calls, words = facts.calls, facts.words
	default:
		reason = fmt.Sprintf("is written in %s, whose code pglens cannot read", f.Language)
	}
	if n.reason == "" {
		n.reason = reason
	}
	n.calls = append(n.calls, calls...)
	n.words = append(n.words, words...)
	return calls, words
}

func (g *codeGraph) addRelation(r codeRelation) (calls []funcRef, words []string) {
	label := "table " + r.Schema + "." + r.Name + " (row security policy)"
	switch r.Kind {
	case "v":
		label = "view " + r.Schema + "." + r.Name
	case "f":
		label = "foreign table " + r.Schema + "." + r.Name
	}
	n := g.node("r:"+r.Schema+"."+r.Name, r.Schema, label, g.relations, r.Name)
	if r.Kind == "f" {
		n.reason = "runs its foreign-data wrapper, which reaches another server"
		return nil, nil
	}
	facts := scanCode(r.Definition, true)
	n.calls = append(n.calls, facts.calls...)
	n.words = append(n.words, facts.words...)
	return facts.calls, facts.words
}

// solve marks every node that reaches a hazard, until nothing changes. Keys
// are visited in order, so the reason reported does not depend on map order.
func (g *codeGraph) solve() {
	keys := make([]string, 0, len(g.nodes))
	for key, n := range g.nodes {
		keys = append(keys, key)
		n.hazard, n.via = n.reason != "", nil
	}
	sort.Strings(keys)
	edges := make(map[string][]codeEdge, len(keys))
	for _, key := range keys {
		n := g.nodes[key]
		var out []codeEdge
		for _, call := range n.calls {
			for _, target := range g.functions[call.Name] {
				if call.Schema == "" || g.nodes[target].schema == call.Schema {
					out = append(out, codeEdge{key: target, verb: "calls"})
				}
			}
		}
		for _, word := range n.words {
			for _, target := range g.relations[word] {
				out = append(out, codeEdge{key: target, verb: "reads"})
			}
		}
		edges[key] = out
	}
	for changed := true; changed; {
		changed = false
		for _, key := range keys {
			n := g.nodes[key]
			if n.hazard {
				continue
			}
			for _, edge := range edges[key] {
				if g.nodes[edge.key].hazard {
					n.hazard, n.via = true, &edge
					changed = true
					break
				}
			}
		}
	}
}

func dynamicReason(keyword string) string {
	switch keyword {
	case "EXECUTE":
		return "runs SQL text it builds at run time (EXECUTE), which pglens cannot check"
	case "DO":
		return "runs an anonymous code block (DO), which pglens cannot check"
	case "COPY":
		return "runs COPY, which can read or write files and programs on the server"
	}
	return ""
}

// codeFacts is what the guard reads in a function body or a view definition.
type codeFacts struct {
	calls []funcRef
	// words are the identifiers, lower case, each once.
	words []string
	// dynamic is the first keyword that runs code the text does not show.
	dynamic string
}

// scanCode reads text as scanCalls does, and also collects its identifiers
// and the keywords that run code built at run time. deparsed is as for
// scanCalls.
func scanCode(text string, deparsed bool) codeFacts {
	facts := codeFacts{calls: scanCalls(text, deparsed)}
	seen := map[string]bool{}
	add := func(word string) {
		if !seen[word] {
			seen[word] = true
			facts.words = append(facts.words, word)
		}
	}
	flag := func(keyword string) {
		if facts.dynamic == "" {
			facts.dynamic = keyword
		}
	}
	prev := ""
	for i := 0; i < len(text); {
		c := text[i]
		switch {
		case c == '\'':
			if prev == "do" {
				flag("DO")
			}
			escaped := i > 0 && (text[i-1] == 'E' || text[i-1] == 'e') && (i < 2 || !identByte(text[i-2]))
			i = skipStringLiteral(text, i, escaped)
			prev = ""
		case c == '-' && i+1 < len(text) && text[i+1] == '-':
			i = skipLineComment(text, i)
		case c == '/' && i+1 < len(text) && text[i+1] == '*':
			i = skipBlockComment(text, i)
		case c == '$':
			if prev == "do" && i+1 < len(text) && (text[i+1] < '0' || text[i+1] > '9') {
				flag("DO")
			}
			i = skipDollar(text, i)
			prev = ""
		case c == ':' && i+1 < len(text) && text[i+1] == ':':
			i = skipTypeName(text, i+2)
			prev = ""
		case c >= '0' && c <= '9':
			for i < len(text) && (identByte(text[i]) || text[i] == '.') {
				i++
			}
			prev = ""
		case c == '"' || identStart(c):
			part, quoted, end, _ := readNamePart(text, i, deparsed)
			i = end
			if quoted {
				add(part)
				prev = ""
				continue
			}
			lower := strings.ToLower(part)
			switch {
			case lower == "execute":
				flag("EXECUTE")
			case lower == "copy":
				flag("COPY")
			case lower == "language" && prev == "do":
				flag("DO")
			}
			// Deparsed text prints keywords in upper case and names in
			// lower case.
			if !deparsed || part == lower {
				add(lower)
			}
			prev = lower
		default:
			if !isSpace(c) {
				prev = ""
			}
			i++
		}
	}
	return facts
}

func callNames(refs []funcRef) []string {
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		names = append(names, ref.Name)
	}
	return names
}

// fresh returns the names not seen yet, in order, and marks them seen.
func fresh(names []string, seen map[string]bool) []string {
	var out []string
	for _, name := range names {
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func queryJSON(ctx context.Context, conn check.Conn, sql string, out any, args ...any) error {
	var raw json.RawMessage
	if err := conn.QueryRow(ctx, sql, args...).Scan(&raw); err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}
