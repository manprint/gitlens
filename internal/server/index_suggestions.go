package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/indexadvice"
	"github.com/manprint/pglens/internal/wire"
)

// Missing-index suggestions (phase 4, D8). The endpoint loads the statements of
// the window with their texts, the index definitions and the column inventory
// the agent reports, and the table sizes and write rates, and hands them to
// indexadvice.Suggest. Nothing here executes DDL (I-2).

const (
	indexSuggestionsBudget = 5 * time.Second
	// indexSuggestionsStatementCap bounds the statements analysed: the heaviest
	// of the window.
	indexSuggestionsStatementCap = 5000
	indexSuggestionsMinWindow    = time.Hour
	indexSuggestionsMaxWindow    = 7 * 24 * time.Hour
	indexSuggestionsDefaultLimit = 20
	indexSuggestionsMaxLimit     = 100
	// skipNoText counts statements whose text the server no longer holds.
	skipNoText = "no_text"

	indexSuggestionsNote = "Every figure is an estimate from the statistics of the window. Weight and read cost are an upper bound of the time the statements spend today: an index cannot remove more than all of it. Sizes come from the planner's row counts. A suggestion is a candidate to evaluate, never an instruction; pglens does not run it."
)

// indexSuggestionData is what the store loads for one request.
type indexSuggestionData struct {
	Statements      []indexadvice.Stmt
	NoText          int
	TotalExecMsRate map[string]float64
	Tables          map[indexadvice.Key]indexadvice.TableMeta
	Indexes         map[indexadvice.Key][]indexadvice.IndexDef
	// ColumnFacts is false when no table_columns fact was reported.
	ColumnFacts bool
	// IndexesComplete marks the databases that reported table_indexes facts:
	// their index list is complete. Others only know the index_def facts of
	// the indexes the agent selected, so an existing index may be missing.
	IndexesComplete map[string]bool
	// InventoryTruncated marks the databases that reported as many tables as the
	// agent's limit: tables beyond it have no table_indexes fact.
	InventoryTruncated map[string]bool
}

// indexSuggestionStore loads the data of one instance; found is false for an
// unknown instance. Unit tests stub it.
type indexSuggestionStore interface {
	IndexSuggestionData(ctx context.Context, id uuid.UUID, window time.Duration, database string) (data indexSuggestionData, found bool, err error)
}

type indexSuggestionsJSON struct {
	InstanceID    string                   `json:"instance_id"`
	GeneratedAt   time.Time                `json:"generated_at"`
	WindowSeconds int                      `json:"window_seconds"`
	EstimatesNote string                   `json:"estimates_note"`
	Coverage      indexadvice.Coverage     `json:"coverage"`
	Suggestions   []indexadvice.Suggestion `json:"suggestions"`
	Notes         []string                 `json:"notes"`
}

func (a *API) indexSuggestionStore() indexSuggestionStore {
	if a.indexSuggestions != nil {
		return a.indexSuggestions
	}
	if a.pool == nil {
		return nil
	}
	return pgIndexSuggestionStore{pool: a.pool}
}

func (a *API) registerIndexSuggestionRoutes(r chi.Router) {
	r.Get("/api/v1/instances/{id}/index-suggestions", a.handleIndexSuggestions)
}

// parseSuggestionWindow reads a Go duration or a number of days ("3d"),
// between 1h and 7d; empty is 24h.
func parseSuggestionWindow(raw string) (time.Duration, error) {
	if raw == "" {
		return 24 * time.Hour, nil
	}
	var d time.Duration
	if days, ok := strings.CutSuffix(raw, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil {
			return 0, fmt.Errorf("invalid window %q", raw)
		}
		d = time.Duration(n) * 24 * time.Hour
	} else {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return 0, fmt.Errorf("invalid window %q", raw)
		}
		d = parsed
	}
	if d < indexSuggestionsMinWindow || d > indexSuggestionsMaxWindow {
		return 0, fmt.Errorf("window must be between 1h and 7d, got %q", raw)
	}
	return d, nil
}

func parseSuggestionLimit(raw string) (int, error) {
	if raw == "" {
		return indexSuggestionsDefaultLimit, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > indexSuggestionsMaxLimit {
		return 0, fmt.Errorf("limit must be an integer between 1 and %d, got %q", indexSuggestionsMaxLimit, raw)
	}
	return n, nil
}

// handleIndexSuggestions answers getIndexSuggestions.
func (a *API) handleIndexSuggestions(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", err.Error())
		return
	}
	window, err := parseSuggestionWindow(r.URL.Query().Get("window"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid window", err.Error())
		return
	}
	limit, err := parseSuggestionLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid limit", err.Error())
		return
	}
	database := r.URL.Query().Get("database")
	st := a.indexSuggestionStore()
	if st == nil {
		writeError(w, http.StatusNotFound, "instance not found", fmt.Sprintf("instance %s not found", idStr))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), indexSuggestionsBudget)
	defer cancel()
	data, found, err := st.IndexSuggestionData(ctx, id, window, database)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			writeError(w, http.StatusServiceUnavailable, "analysis timed out", "the analysis did not finish in "+indexSuggestionsBudget.String())
			return
		}
		writeError(w, http.StatusInternalServerError, "index suggestions query failed", err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "instance not found", fmt.Sprintf("instance %s not found", idStr))
		return
	}
	writeJSON(w, http.StatusOK, buildIndexSuggestions(id, window, limit, data, time.Now()))
}

// buildIndexSuggestions runs the analysis and shapes the response.
func buildIndexSuggestions(id uuid.UUID, window time.Duration, limit int, data indexSuggestionData, now time.Time) indexSuggestionsJSON {
	out := indexadvice.Suggest(indexadvice.Input{
		Window: window, Statements: data.Statements, TotalExecMsRate: data.TotalExecMsRate,
		Tables: data.Tables, Indexes: data.Indexes, IndexesComplete: data.IndexesComplete, InventoryTruncated: data.InventoryTruncated, Limit: limit,
	})
	out.Coverage.StatementsConsidered += data.NoText
	if data.NoText > 0 {
		out.Coverage.Skipped[skipNoText] = data.NoText
	}
	notes := []string{}
	switch {
	case out.Coverage.StatementsConsidered == 0:
		notes = append(notes, "No statement data in the window: check that pg_stat_statements is installed and reported (see the readiness hint).")
	case !data.ColumnFacts:
		notes = append(notes, "The table_columns check has not reported: selectivity and sizes are unknown, so every suggestion has low confidence.")
	}
	if data.ColumnFacts && len(data.IndexesComplete) == 0 && out.Coverage.StatementsConsidered > 0 {
		notes = append(notes, "The agent does not report every index definition: an index that already exists may be suggested again.")
	}
	for _, truncated := range data.InventoryTruncated {
		if truncated && out.Coverage.StatementsConsidered > 0 {
			notes = append(notes, "The table list of a database reached the agent's limit of "+strconv.Itoa(wire.TableInventoryLimit)+" tables: a table beyond it may be suggested an index it already has.")
			break
		}
	}
	return indexSuggestionsJSON{
		InstanceID: id.String(), GeneratedAt: now.UTC(), WindowSeconds: int(window / time.Second),
		EstimatesNote: indexSuggestionsNote, Coverage: out.Coverage, Suggestions: out.Suggestions, Notes: notes,
	}
}

// SQL of the store. The instance, window and database are bound parameters
// (I-3); the SQL text is constant.
const (
	indexSuggestionInstanceSQL = `SELECT cluster_id FROM instances WHERE tenant_id = 'default' AND instance_id = $1`

	// Rates are averaged over every scrape of the instance in the window, so a
	// statement absent from a scrape counts as zero for it. The totals are the
	// database's whole workload, taken before the cap. One scan of the instance's
	// rows feeds both the per-statement sums (grouping set 1) and the scrape
	// count (grouping set 6, one row per distinct ts): a separate count(DISTINCT
	// ts) would skip-scan the ts index and filter every other instance's rows.
	indexSuggestionStatementsSQL = `
WITH g AS (
  SELECT datname, queryid, ts, sum(calls_rate) AS calls, sum(exec_time_rate_ms) AS exec,
         GROUPING(datname, queryid, ts) AS gid
    FROM metrics_statements
   WHERE tenant_id = 'default' AND instance_id = $1 AND ts >= now() - make_interval(secs => $2)
   GROUP BY GROUPING SETS ((datname, queryid), (ts))
), win AS (
  SELECT count(*)::double precision AS n FROM g WHERE gid = 6
), agg AS (
  SELECT datname, queryid, calls, exec FROM g WHERE gid = 1 AND ($3 = '' OR datname = $3)
), total AS (
  SELECT agg.*, sum(agg.exec) OVER (PARTITION BY agg.datname) AS db_exec FROM agg
)
SELECT t.datname, t.queryid,
       COALESCE(t.calls / NULLIF(win.n, 0), 0), COALESCE(t.exec / NULLIF(win.n, 0), 0),
       COALESCE(t.db_exec / NULLIF(win.n, 0), 0),
       (SELECT qt.query_text FROM query_texts qt
         WHERE qt.tenant_id = 'default' AND qt.cluster_id = $4 AND qt.datname = t.datname AND qt.queryid = t.queryid
         ORDER BY qt.last_seen DESC LIMIT 1)
  FROM total t, win
 ORDER BY t.exec DESC NULLS LAST, t.datname, t.queryid
 LIMIT $5`

	indexSuggestionIndexesSQL = `
SELECT datname, COALESCE(labels->>'schemaname', ''), COALESCE(labels->>'relname', ''), COALESCE(labels->>'indexrelname', ''), COALESCE(value_text, '')
  FROM object_facts
 WHERE tenant_id = 'default' AND instance_id = $1 AND kind = 'index_def' AND ($2 = '' OR datname = $2)
 ORDER BY datname, key`

	indexSuggestionTableIndexesSQL = `
	SELECT datname, COALESCE(labels->>'schemaname', ''), COALESCE(labels->>'relname', ''), COALESCE(value_json::text, '[]')
	FROM object_facts
	WHERE tenant_id = 'default' AND instance_id = $1 AND kind = 'table_indexes' AND ($2 = '' OR datname = $2)
	ORDER BY datname, key`

	indexSuggestionColumnsSQL = `
SELECT datname, COALESCE(labels->>'schemaname', ''), COALESCE(labels->>'relname', ''), COALESCE(labels->>'reltuples', ''), COALESCE(value_json::text, '[]')
  FROM object_facts
 WHERE tenant_id = 'default' AND instance_id = $1 AND kind = 'table_columns' AND ($2 = '' OR datname = $2)
 ORDER BY datname, key`

	// The newest sizes of each table in the window and its mean write rates.
	indexSuggestionTablesSQL = `
SELECT datname, schemaname, relname,
       (array_agg(n_live_tup ORDER BY ts DESC))[1], (array_agg(reltuples ORDER BY ts DESC))[1],
       avg(n_tup_ins), avg(n_tup_upd), avg(n_tup_del), avg(n_tup_hot_upd)
  FROM metrics_tables
 WHERE tenant_id = 'default' AND instance_id = $1 AND ts >= now() - make_interval(secs => $2)
   AND ($3 = '' OR datname = $3)
 GROUP BY datname, schemaname, relname`
)

// pgIndexSuggestionStore is the indexSuggestionStore of a database.
type pgIndexSuggestionStore struct {
	pool dbPool
}

type columnFact struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	TypLen    int      `json:"typlen"`
	AvgWidth  *float64 `json:"avg_width"`
	NDistinct *float64 `json:"n_distinct"`
}

func (s pgIndexSuggestionStore) IndexSuggestionData(ctx context.Context, id uuid.UUID, window time.Duration, database string) (indexSuggestionData, bool, error) {
	var clusterID int64
	if err := s.pool.QueryRow(ctx, indexSuggestionInstanceSQL, id).Scan(&clusterID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return indexSuggestionData{}, false, nil
		}
		return indexSuggestionData{}, false, err
	}
	data := indexSuggestionData{
		TotalExecMsRate:    map[string]float64{},
		Tables:             map[indexadvice.Key]indexadvice.TableMeta{},
		Indexes:            map[indexadvice.Key][]indexadvice.IndexDef{},
		IndexesComplete:    map[string]bool{},
		InventoryTruncated: map[string]bool{},
	}
	reported := map[string]int{}
	secs := window.Seconds()

	rows, err := s.pool.Query(ctx, indexSuggestionStatementsSQL, id, secs, database, clusterID, indexSuggestionsStatementCap)
	if err != nil {
		return data, false, err
	}
	for rows.Next() {
		var (
			db                  string
			queryID             int64
			calls, exec, dbExec float64
			text                *string
		)
		if err := rows.Scan(&db, &queryID, &calls, &exec, &dbExec, &text); err != nil {
			rows.Close()
			return data, false, err
		}
		data.TotalExecMsRate[db] = dbExec
		if text == nil || *text == "" {
			data.NoText++
			continue
		}
		data.Statements = append(data.Statements, indexadvice.Stmt{Database: db, QueryID: queryID, Text: *text, CallsRate: calls, ExecMsRate: exec})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return data, false, err
	}

	// table_indexes lists every index of a table: a table whose list is empty is
	// known to have none. Its databases ignore the partial index_def facts.
	known := map[indexadvice.Key]bool{}
	unparsed := map[indexadvice.Key]bool{}
	rows, err = s.pool.Query(ctx, indexSuggestionTableIndexesSQL, id, database)
	if err != nil {
		return data, false, err
	}
	for rows.Next() {
		var db, schema, table, defs string
		if err := rows.Scan(&db, &schema, &table, &defs); err != nil {
			rows.Close()
			return data, false, err
		}
		var list []string
		if json.Unmarshal([]byte(defs), &list) != nil {
			continue
		}
		key := indexadvice.Key{Database: db, Schema: schema, Table: table}
		data.IndexesComplete[db] = true
		reported[db]++
		if reported[db] >= wire.TableInventoryLimit {
			data.InventoryTruncated[db] = true
		}
		var parsedList []indexadvice.IndexDef
		for _, def := range list {
			parsed, perr := indexadvice.ParseIndexDef(def)
			if perr != nil {
				unparsed[key] = true
				continue
			}
			parsedList = append(parsedList, parsed)
		}
		known[key] = true
		data.Indexes[key] = parsedList
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return data, false, err
	}

	rows, err = s.pool.Query(ctx, indexSuggestionIndexesSQL, id, database)
	if err != nil {
		return data, false, err
	}
	for rows.Next() {
		var db, schema, table, name, def string
		if err := rows.Scan(&db, &schema, &table, &name, &def); err != nil {
			rows.Close()
			return data, false, err
		}
		if data.IndexesComplete[db] && !data.InventoryTruncated[db] {
			continue
		}
		parsed, perr := indexadvice.ParseIndexDef(def)
		if perr != nil {
			continue
		}
		if schema == "" {
			schema = parsed.Schema
		}
		if table == "" {
			table = parsed.Table
		}
		if name != "" {
			parsed.Name = name
		}
		key := indexadvice.Key{Database: db, Schema: schema, Table: table}
		if known[key] {
			continue
		}
		data.Indexes[key] = append(data.Indexes[key], parsed)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return data, false, err
	}

	rows, err = s.pool.Query(ctx, indexSuggestionColumnsSQL, id, database)
	if err != nil {
		return data, false, err
	}
	for rows.Next() {
		var db, schema, table, reltuples, columns string
		if err := rows.Scan(&db, &schema, &table, &reltuples, &columns); err != nil {
			rows.Close()
			return data, false, err
		}
		var facts []columnFact
		if json.Unmarshal([]byte(columns), &facts) != nil {
			continue
		}
		meta := indexadvice.TableMeta{
			Columns:         make(map[string]indexadvice.Column, len(facts)),
			IndexesKnown:    known[indexadvice.Key{Database: db, Schema: schema, Table: table}],
			IndexesUnparsed: unparsed[indexadvice.Key{Database: db, Schema: schema, Table: table}],
		}
		for _, f := range facts {
			col := indexadvice.Column{Type: f.Type, TypLen: f.TypLen, NDistinct: f.NDistinct}
			if f.AvgWidth != nil {
				col.AvgWidth = *f.AvgWidth
			}
			meta.Columns[f.Name] = col
		}
		if n, perr := strconv.ParseFloat(reltuples, 64); perr == nil && n >= 0 {
			meta.RelTuples, meta.LiveTuples, meta.HasCounts = n, n, true
		}
		data.Tables[indexadvice.Key{Database: db, Schema: schema, Table: table}] = meta
		data.ColumnFacts = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return data, false, err
	}

	rows, err = s.pool.Query(ctx, indexSuggestionTablesSQL, id, secs, database)
	if err != nil {
		return data, false, err
	}
	for rows.Next() {
		var db, schema, table string
		var live *int64
		var reltuples, ins, upd, del, hot *float64
		if err := rows.Scan(&db, &schema, &table, &live, &reltuples, &ins, &upd, &del, &hot); err != nil {
			rows.Close()
			return data, false, err
		}
		key := indexadvice.Key{Database: db, Schema: schema, Table: table}
		meta := data.Tables[key]
		if live != nil {
			meta.LiveTuples, meta.HasCounts = float64(*live), true
			if reltuples != nil && *reltuples > 0 {
				meta.RelTuples = *reltuples
			} else {
				meta.RelTuples = float64(*live)
			}
		}
		if ins != nil && upd != nil && del != nil {
			meta.Writes = indexadvice.Writes{Ins: *ins, Upd: *upd, Del: *del}
			if hot != nil {
				meta.Writes.HotUpd = *hot
			}
			meta.HasWrites = true
		}
		data.Tables[key] = meta
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return data, false, err
	}
	return data, true, nil
}
