package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Full search (D20). A search term is matched case-insensitively as a
// substring with ILIKE ... ESCAPE '\'. The term only ever reaches the database
// as a bound parameter (I-3): searchPattern escapes the LIKE wildcards, so a
// '%' or '_' the user typed matches itself.

const (
	searchMinLength = 2
	searchMaxLength = 200
	// searchDetail is the detail of the 400 answer to a term out of bounds.
	searchDetail = "q must be 2–200 characters"
	// searchKindLimit is the most hits a group of searchInstance returns; its
	// total still counts every match.
	searchKindLimit = 50
	// searchSnippetLength bounds a snippet, the cut marks included.
	searchSnippetLength = 160
	// searchSnippetLead is how much text a snippet keeps before the first
	// match when the text is longer than a snippet.
	searchSnippetLead = 40
	// searchDefaultWindow is the range searchInstance reads without from.
	searchDefaultWindow = 24 * time.Hour
)

var (
	// ErrSearchTooShort is a search term under two characters once trimmed.
	ErrSearchTooShort = errors.New("search term is shorter than 2 characters")
	// ErrSearchTooLong is a search term over 200 characters once trimmed.
	ErrSearchTooLong = errors.New("search term is longer than 200 characters")
)

// searchPattern trims q and turns it into an ILIKE pattern for a substring
// match: '\', '%' and '_' are escaped with a backslash and the result is
// wrapped in '%'. The length is counted in characters, not bytes.
func searchPattern(q string) (string, error) {
	term := strings.TrimSpace(q)
	n := utf8.RuneCountInString(term)
	if n < searchMinLength {
		return "", ErrSearchTooShort
	}
	if n > searchMaxLength {
		return "", ErrSearchTooLong
	}
	var sb strings.Builder
	sb.Grow(len(term) + 4)
	sb.WriteByte('%')
	for _, r := range term {
		if r == '\\' || r == '%' || r == '_' {
			sb.WriteByte('\\')
		}
		sb.WriteRune(r)
	}
	sb.WriteByte('%')
	return sb.String(), nil
}

// searchParam reads the q parameter of a request. An absent or empty q is no
// search (ok false), and the handler answers as it did before search existed.
// Otherwise the trimmed term and its pattern are returned, or the 400 answer
// was written and err is set.
func searchParam(w http.ResponseWriter, r *http.Request) (term, pattern string, ok bool, err error) {
	raw := r.URL.Query().Get("q")
	if raw == "" {
		return "", "", false, nil
	}
	pattern, err = searchPattern(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid q", searchDetail)
		return "", "", false, err
	}
	return strings.TrimSpace(raw), pattern, true, nil
}

// The kinds searchInstance searches, in the order of its groups.
const (
	searchKindStatements = "statements"
	searchKindIndexes    = "indexes"
	searchKindTables     = "tables"
	searchKindColumns    = "columns"
)

var searchKinds = []string{searchKindStatements, searchKindIndexes, searchKindTables, searchKindColumns}

// parseSearchKinds reads the comma-separated kinds parameter. Empty means
// every kind; a repeated kind counts once; an unknown or empty element is an
// error. The kinds come back in group order, whatever order they were asked in.
func parseSearchKinds(s string) ([]string, error) {
	if strings.TrimSpace(s) == "" {
		return append([]string(nil), searchKinds...), nil
	}
	asked := map[string]bool{}
	for _, part := range strings.Split(s, ",") {
		kind := strings.TrimSpace(part)
		known := false
		for _, k := range searchKinds {
			if k == kind {
				known = true
				break
			}
		}
		if !known {
			return nil, fmt.Errorf("unknown kind %q: allowed values are %s", kind, strings.Join(searchKinds, ", "))
		}
		asked[kind] = true
	}
	out := make([]string, 0, len(asked))
	for _, k := range searchKinds {
		if asked[k] {
			out = append(out, k)
		}
	}
	return out, nil
}

// searchSnippetPart is a run of snippet text; Match marks a run that matches
// the search term, so the client renders it highlighted and adds nothing.
type searchSnippetPart struct {
	Text  string `json:"text"`
	Match bool   `json:"match"`
}

// buildSnippet cuts text to at most searchSnippetLength characters around the
// first case-insensitive match of term and marks every match in the cut, left
// to right and without overlap. Runs of white space are folded into one space,
// in the text and in the term, so a multi-line statement reads as one line. A
// cut is shown with '…' on its side, inside the length bound. Text without a
// match (the hit matched another field) comes back from its start, unmarked.
func buildSnippet(text, term string) []searchSnippetPart {
	runes := []rune(foldSpace(text))
	needle := []rune(strings.ToLower(foldSpace(strings.TrimSpace(term))))
	for i, r := range needle {
		needle[i] = unicode.ToLower(r)
	}
	lower := make([]rune, len(runes))
	for i, r := range runes {
		lower[i] = unicode.ToLower(r)
	}
	var matches [][2]int
	if len(needle) > 0 {
		for i := 0; i+len(needle) <= len(lower); {
			if runesEqual(lower[i:i+len(needle)], needle) {
				matches = append(matches, [2]int{i, i + len(needle)})
				i += len(needle)
				continue
			}
			i++
		}
	}

	start, end := 0, len(runes)
	if len(runes) > searchSnippetLength {
		first := 0
		if len(matches) > 0 {
			first = matches[0][0]
		}
		start = max(first-searchSnippetLead, 0)
		end = start + searchSnippetLength
		if end > len(runes) {
			end = len(runes)
			start = end - searchSnippetLength
		}
		// The cut marks take a character each from the window.
		if start > 0 {
			start++
		}
		if end < len(runes) {
			end--
		}
	}

	var parts []searchSnippetPart
	add := func(s string, match bool) {
		if s == "" {
			return
		}
		if n := len(parts); n > 0 && parts[n-1].Match == match {
			parts[n-1].Text += s
			return
		}
		parts = append(parts, searchSnippetPart{Text: s, Match: match})
	}
	if start > 0 {
		add("…", false)
	}
	pos := start
	for _, m := range matches {
		from, to := max(m[0], start), min(m[1], end)
		if from >= to {
			continue
		}
		add(string(runes[pos:from]), false)
		add(string(runes[from:to]), true)
		pos = to
	}
	add(string(runes[pos:end]), false)
	if end < len(runes) {
		add("…", false)
	}
	if parts == nil {
		parts = []searchSnippetPart{}
	}
	return parts
}

func runesEqual(a, b []rune) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// foldSpace replaces every run of white space with one space.
func foldSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// searchHit is one match of searchInstance. Link is a path of the console
// that shows the matched object.
type searchHit struct {
	Key      string              `json:"key"`
	Title    string              `json:"title"`
	Subtitle string              `json:"subtitle"`
	Snippet  []searchSnippetPart `json:"snippet"`
	Link     string              `json:"link"`
}

// searchGroup holds the hits of one kind. Total counts every match, and
// Truncated says the hits are fewer than the total.
type searchGroup struct {
	Kind      string      `json:"kind"`
	Total     int64       `json:"total"`
	Truncated bool        `json:"truncated"`
	Hits      []searchHit `json:"hits"`
}

type searchResponse struct {
	Q      string        `json:"q"`
	Groups []searchGroup `json:"groups"`
}

// searchRequest is a validated searchInstance request.
type searchRequest struct {
	instanceID uuid.UUID
	term       string
	pattern    string
	kinds      []string
	database   string
	from, to   time.Time
	limit      int
	// windowed says the caller named the range (from or to), which the
	// statement links then carry.
	windowed bool
}

// handleSearch is searchInstance: every statement, index, table and column of
// an instance that matches q, from what pglens collected in the range. Each
// kind is one query that returns its first hits and the count of all of them.
func (a *API) handleSearch(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", err.Error())
		return
	}
	term, pattern, ok, err := searchParam(w, r)
	if err != nil {
		return
	}
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid q", searchDetail)
		return
	}
	query := r.URL.Query()
	kinds, err := parseSearchKinds(query.Get("kinds"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid kinds", err.Error())
		return
	}
	limit, err := parseLimitParam(query.Get("limit"), searchKindLimit, searchKindLimit)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid limit", err.Error())
		return
	}
	to := time.Now()
	if s := query.Get("to"); s != "" {
		if to, err = parseTimeParam(s); err != nil {
			writeError(w, http.StatusBadRequest, "invalid to", err.Error())
			return
		}
	}
	from := to.Add(-searchDefaultWindow)
	if s := query.Get("from"); s != "" {
		if from, err = parseTimeParam(s); err != nil {
			writeError(w, http.StatusBadRequest, "invalid from", err.Error())
			return
		}
	}
	if !from.Before(to) {
		writeError(w, http.StatusBadRequest, "invalid range", "from must be before to")
		return
	}
	if a.pool == nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable", "the server has no database pool")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var exists bool
	if err := a.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM instances WHERE tenant_id = 'default' AND instance_id = $1)`, id).Scan(&exists); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed", err.Error())
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "instance not found", fmt.Sprintf("instance %s not found", idStr))
		return
	}

	req := searchRequest{instanceID: id, term: term, pattern: pattern, kinds: kinds,
		database: query.Get("database"), from: from, to: to, limit: limit,
		windowed: query.Get("from") != "" || query.Get("to") != ""}
	resp := searchResponse{Q: term, Groups: make([]searchGroup, 0, len(kinds))}
	for _, kind := range kinds {
		group, err := a.searchKind(ctx, req, kind)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "search failed", err.Error())
			return
		}
		resp.Groups = append(resp.Groups, group)
	}
	writeJSON(w, http.StatusOK, resp)
}

// searchKindQueries is the query of each kind. $1 is the instance, $2 and $3
// the range, $4 the pattern, $5 the limit and $6, when present, the database.
// Every query counts its matches with count(*) OVER (), which is evaluated
// over the rows the WHERE clause keeps, before the LIMIT, and returns the
// match's database, its key parts and the text the snippet is cut from.
//
// Statements are those with samples in the range, ranked by their execution
// time in it, as the statements list ranks them; the text matched is the one
// the list shows, the newest of the queryid. Indexes and tables are matched
// on their facts (index_def, table_columns) seen in the range: an index on its
// qualified name, its table or its definition, a table on its qualified name,
// a column on its qualified name (schema.table.column) or its type, so a
// table's name finds every column of it.
var searchKindQueries = map[string]string{
	searchKindStatements: `
		WITH agg AS (
		SELECT s.cluster_id, s.queryid, s.datname,
		       SUM(s.exec_time_rate_ms * COALESCE(s.sample_seconds, 60)) AS total_exec_time
		FROM metrics_statements s
		WHERE s.tenant_id='default' AND s.instance_id=$1 AND s.ts >= $2 AND s.ts < $3 AND ($6::text IS NULL OR s.datname=$6)
		GROUP BY s.cluster_id, s.queryid, s.datname
		), texted AS (
		SELECT agg.queryid, agg.datname, agg.total_exec_time,
		       (SELECT qt.query_text FROM query_texts qt
		         WHERE qt.tenant_id='default' AND qt.cluster_id=agg.cluster_id
		           AND qt.datname=agg.datname AND qt.queryid=agg.queryid
		         ORDER BY qt.last_seen DESC LIMIT 1) AS qtext
		FROM agg
		)
		SELECT datname, queryid::text, '', COALESCE(qtext, ''), count(*) OVER ()
		FROM texted
		WHERE qtext ILIKE $4 ESCAPE '\' OR queryid::text ILIKE $4 ESCAPE '\' OR datname ILIKE $4 ESCAPE '\'
		ORDER BY texted.total_exec_time DESC NULLS LAST, texted.datname, texted.queryid
		LIMIT $5`,
	searchKindIndexes: `
		SELECT datname, COALESCE(labels->>'schemaname', split_part(key, '.', 1)),
		       COALESCE(labels->>'indexrelname', substr(key, strpos(key, '.') + 1)),
		       COALESCE(labels->>'relname', '') || E'\n' || COALESCE(value_text, ''), count(*) OVER ()
		FROM object_facts
		WHERE tenant_id='default' AND instance_id=$1 AND kind='index_def'
		  AND last_seen >= $2 AND first_seen < $3 AND ($6::text IS NULL OR datname=$6)
		  AND (key ILIKE $4 ESCAPE '\' OR labels->>'relname' ILIKE $4 ESCAPE '\' OR value_text ILIKE $4 ESCAPE '\')
		ORDER BY datname, key
		LIMIT $5`,
	searchKindTables: `
		SELECT datname, COALESCE(labels->>'schemaname', ''), COALESCE(labels->>'relname', ''), '', count(*) OVER ()
		FROM object_facts
		WHERE tenant_id='default' AND instance_id=$1 AND kind='table_columns'
		  AND last_seen >= $2 AND first_seen < $3 AND ($6::text IS NULL OR datname=$6)
		  AND ((labels->>'schemaname') || '.' || (labels->>'relname')) ILIKE $4 ESCAPE '\'
		ORDER BY datname, labels->>'schemaname', labels->>'relname'
		LIMIT $5`,
	searchKindColumns: `
		SELECT f.datname, COALESCE(f.labels->>'schemaname', ''), COALESCE(f.labels->>'relname', ''),
		       COALESCE(c->>'name', '') || E'\n' || COALESCE(c->>'type', ''), count(*) OVER ()
		FROM object_facts f
		CROSS JOIN LATERAL jsonb_array_elements(
		       CASE WHEN jsonb_typeof(f.value_json) = 'array' THEN f.value_json ELSE '[]'::jsonb END) c
		WHERE f.tenant_id='default' AND f.instance_id=$1 AND f.kind='table_columns'
		  AND f.last_seen >= $2 AND f.first_seen < $3 AND ($6::text IS NULL OR f.datname=$6)
		  AND (COALESCE(f.labels->>'schemaname', '') || '.' || COALESCE(f.labels->>'relname', '') || '.' || (c->>'name') ILIKE $4 ESCAPE '\'
		       OR c->>'type' ILIKE $4 ESCAPE '\')
		ORDER BY f.datname, f.labels->>'schemaname', f.labels->>'relname', c->>'name'
		LIMIT $5`,
}

// searchKind runs the query of one kind and builds its group.
func (a *API) searchKind(ctx context.Context, req searchRequest, kind string) (searchGroup, error) {
	var database *string
	if req.database != "" {
		database = &req.database
	}
	rows, err := a.pool.Query(ctx, searchKindQueries[kind], req.instanceID, req.from, req.to, req.pattern, req.limit, database)
	if err != nil {
		return searchGroup{}, fmt.Errorf("%s: %w", kind, err)
	}
	defer rows.Close()
	group := searchGroup{Kind: kind, Hits: []searchHit{}}
	for rows.Next() {
		var datname, first, second, text string
		var total int64
		if err := rows.Scan(&datname, &first, &second, &text, &total); err != nil {
			return searchGroup{}, fmt.Errorf("%s: %w", kind, err)
		}
		group.Total = total
		group.Hits = append(group.Hits, searchHitFor(req, kind, datname, first, second, text))
	}
	if err := rows.Err(); err != nil {
		return searchGroup{}, fmt.Errorf("%s: %w", kind, err)
	}
	group.Truncated = group.Total > int64(len(group.Hits))
	return group, nil
}

// searchHitFor builds the hit of one row. The keys of indexes and tables are
// the row keys of the Index analysis tables (database.schema.name), so a link
// focuses the row it found.
func searchHitFor(req searchRequest, kind, datname, first, second, text string) searchHit {
	instance := url.QueryEscape(req.instanceID.String())
	indexesLink := func(tab, key string) string {
		return "/indexes?instance=" + instance + "&tab=" + tab + "&focus=" + url.QueryEscape(key)
	}
	switch kind {
	case searchKindStatements:
		// first is the queryid; the snippet is cut from the query text.
		return searchHit{
			Key: datname + "." + first, Title: first, Subtitle: datname,
			Snippet: buildSnippet(text, req.term),
			Link:    statementsLink(req),
		}
	case searchKindIndexes:
		// first and second are the schema and the index; text is the table,
		// a line break and the definition.
		table, definition, _ := strings.Cut(text, "\n")
		qualified := first + "." + second
		subtitle := datname
		if table != "" {
			subtitle = datname + " · " + first + "." + table
		}
		if definition == "" {
			definition = qualified
		}
		key := datname + "." + qualified
		return searchHit{Key: key, Title: qualified, Subtitle: subtitle,
			Snippet: buildSnippet(definition, req.term), Link: indexesLink("indexes", key)}
	case searchKindTables:
		qualified := first + "." + second
		key := datname + "." + qualified
		return searchHit{Key: key, Title: qualified, Subtitle: datname,
			Snippet: buildSnippet(qualified, req.term), Link: indexesLink("dead-rows", key)}
	default:
		// columns: text is the column name, a line break and its type.
		column, typ, _ := strings.Cut(text, "\n")
		table := datname + "." + first + "." + second
		qualified := first + "." + second + "." + column
		subtitle := datname
		if typ != "" {
			subtitle = typ + " · " + datname
		}
		return searchHit{Key: datname + "." + qualified, Title: qualified, Subtitle: subtitle,
			Snippet: buildSnippet(qualified, req.term), Link: indexesLink("dead-rows", table)}
	}
}

// statementsLink is the Slowest queries page searching the term. When the
// caller named the range, the link carries it as the page reads it: from and
// to, both, RFC 3339 in UTC (fractional seconds kept).
func statementsLink(req searchRequest) string {
	link := "/queries?instance=" + url.QueryEscape(req.instanceID.String()) + "&q=" + url.QueryEscape(req.term)
	if req.windowed {
		link += "&from=" + url.QueryEscape(req.from.UTC().Format(time.RFC3339Nano)) +
			"&to=" + url.QueryEscape(req.to.UTC().Format(time.RFC3339Nano))
	}
	return link
}
