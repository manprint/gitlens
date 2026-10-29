package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/manprint/pglens/internal/wire"
)

type relationAPIResponse struct {
	InstanceID           string           `json:"instance_id"`
	Items                []map[string]any `json:"items"`
	Truncated            bool             `json:"truncated"`
	RelationsNotReported *int             `json:"relations_not_reported"`
}

func (a *API) registerRelationRoutes(r chi.Router) {
	r.Get("/api/v1/instances/{id}/tables", a.handleTables)
	r.Get("/api/v1/instances/{id}/tables/history", a.handleTableHistory)
	r.Get("/api/v1/instances/{id}/indexes", a.handleIndexes)
	r.Get("/api/v1/instances/{id}/bloat", a.handleBloat)
}
func relationID(r *http.Request) (uuid.UUID, error) { return uuid.Parse(chi.URLParam(r, "id")) }
func relationLimit(r *http.Request) int {
	n, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if n <= 0 {
		n = 50
	}
	if n > 500 {
		n = 500
	}
	return n
}

// The relation tables hold one row per relation per scrape. Every handler
// reads the newest row of each relation (DISTINCT ON), inside the window its
// check refreshes it in. The handlers used to select raw rows: the tables
// endpoint listed each relation up to three times (one per scrape in its
// 15-minute window) and the index and bloat endpoints walked their whole
// retention. Their scans also dropped rows silently: seq_scan and idx_scan
// are per-second rates, and scanning 0.25 into an int64 fails, as does any
// NULL, so the row was skipped without a word. Rows are now scanned into
// nullable types and a failed query is an error, not an empty list.
func (a *API) handleTables(w http.ResponseWriter, r *http.Request) {
	id, e := relationID(r)
	if e != nil {
		writeError(w, 400, "invalid instance_id", e.Error())
		return
	}
	order := r.URL.Query().Get("order_by")
	if order == "" {
		order = r.URL.Query().Get("order")
	}
	if order != "" && order != "dead_tup" && order != "dead_ratio" && order != "size" && order != "seq_scan" {
		writeError(w, 400, "invalid order_by", "allowed values: dead_tup, dead_ratio, size, seq_scan")
		return
	}
	resp := relationAPIResponse{InstanceID: id.String(), Items: []map[string]any{}}
	if a.pool == nil {
		writeJSON(w, 200, resp)
		return
	}
	orderExpr := "n_dead_tup DESC NULLS LAST"
	switch order {
	case "dead_ratio":
		orderExpr = "COALESCE(dead_tuple_ratio, n_dead_tup::double precision / NULLIF(n_live_tup, 0)) DESC NULLS LAST"
	case "size":
		orderExpr = "total_bytes DESC NULLS LAST"
	case "seq_scan":
		orderExpr = "seq_scan DESC NULLS LAST"
	}
	rows, e := a.pool.Query(r.Context(), `SELECT datname, schemaname, relname, n_live_tup, n_dead_tup, dead_tuple_ratio,
			last_vacuum_age_seconds, total_bytes, seq_scan, idx_scan, ts,
			last_vacuum, last_autovacuum, last_analyze, last_autoanalyze,
			seq_tup_read, n_tup_ins, n_tup_upd, n_tup_del, n_tup_hot_upd, n_mod_since_analyze,
			autovacuum_count, autoanalyze_count, table_bytes, toast_bytes, relfrozenxid_age
		FROM (SELECT DISTINCT ON (datname, schemaname, relname) *
		        FROM metrics_tables
		       WHERE tenant_id='default' AND instance_id=$1 AND ts >= now() - interval '15 minutes'
		       ORDER BY datname, schemaname, relname, ts DESC) latest
		ORDER BY `+orderExpr+` LIMIT $2`, id, relationLimit(r))
	if e != nil {
		writeError(w, http.StatusInternalServerError, "query failed", e.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var datname, schema, name string
		var live, dead, bytes, modSinceAnalyze, tableBytes, toastBytes, frozenAge *int64
		var ratio, vacuumAge, seqScan, idxScan *float64
		var seqTupRead, tupIns, tupUpd, tupDel, tupHotUpd, autovacuums, autoanalyzes *float64
		var ts time.Time
		var lastVacuum, lastAutovacuum, lastAnalyze, lastAutoanalyze *time.Time
		if e := rows.Scan(&datname, &schema, &name, &live, &dead, &ratio, &vacuumAge, &bytes, &seqScan, &idxScan, &ts,
			&lastVacuum, &lastAutovacuum, &lastAnalyze, &lastAutoanalyze,
			&seqTupRead, &tupIns, &tupUpd, &tupDel, &tupHotUpd, &modSinceAnalyze,
			&autovacuums, &autoanalyzes, &tableBytes, &toastBytes, &frozenAge); e != nil {
			writeError(w, http.StatusInternalServerError, "scan failed", e.Error())
			return
		}
		item := map[string]any{"datname": datname, "schemaname": schema, "relname": name, "ts": ts}
		// last_vacuum_age_seconds is the manual VACUUM only; a table that
		// only autovacuum maintains carries its time in last_autovacuum_at.
		for key, at := range map[string]*time.Time{"last_vacuum_at": lastVacuum, "last_autovacuum_at": lastAutovacuum, "last_analyze_at": lastAnalyze, "last_autoanalyze_at": lastAutoanalyze} {
			if at != nil {
				item[key] = *at
			}
		}
		putNumber(item, "n_live_tup", live)
		putNumber(item, "n_dead_tup", dead)
		putNumber(item, "total_bytes", bytes)
		putNumber(item, "seq_scan_rate", seqScan)
		putNumber(item, "idx_scan_rate", idxScan)
		putNumber(item, "last_vacuum_age_seconds", vacuumAge)
		// The write, scan and vacuum figures are per-second rates, like the scan
		// rates above. A table that has not been updated has no HOT ratio, not a
		// perfect one.
		putNumber(item, "seq_tup_read_rate", seqTupRead)
		putNumber(item, "n_tup_ins_rate", tupIns)
		putNumber(item, "n_tup_upd_rate", tupUpd)
		putNumber(item, "n_tup_del_rate", tupDel)
		putNumber(item, "n_tup_hot_upd_rate", tupHotUpd)
		putNumber(item, "autovacuum_rate", autovacuums)
		putNumber(item, "autoanalyze_rate", autoanalyzes)
		putNumber(item, "n_mod_since_analyze", modSinceAnalyze)
		putNumber(item, "table_bytes", tableBytes)
		putNumber(item, "toast_bytes", toastBytes)
		putNumber(item, "relfrozenxid_age", frozenAge)
		if hot := hotUpdateRatio(tupUpd, tupHotUpd); hot != nil {
			item["hot_update_ratio"] = *hot
		}
		switch {
		case ratio != nil:
			item["dead_ratio"] = *ratio
		case live != nil && dead != nil && *live > 0:
			item["dead_ratio"] = float64(*dead) / float64(*live)
		}
		resp.Items = append(resp.Items, item)
	}
	if e := rows.Err(); e != nil {
		writeError(w, http.StatusInternalServerError, "rows error", e.Error())
		return
	}
	if e := setRelationMetadata(r.Context(), a.pool, &resp, id, "table_stats"); e != nil {
		writeError(w, http.StatusInternalServerError, "query failed", e.Error())
		return
	}
	writeJSON(w, 200, resp)
}

// tableHistoryPoint is one bucket of a table's dead and live tuple counts.
type tableHistoryPoint struct {
	TS       time.Time `json:"ts"`
	NDeadTup *int64    `json:"n_dead_tup"`
	NLiveTup *int64    `json:"n_live_tup"`
}

// tableHistorySeries is the history of one table, oldest bucket first.
type tableHistorySeries struct {
	Datname    string              `json:"datname"`
	Schemaname string              `json:"schemaname"`
	Relname    string              `json:"relname"`
	Points     []tableHistoryPoint `json:"points"`
}

type tableHistoryResponse struct {
	InstanceID string               `json:"instance_id"`
	From       time.Time            `json:"from"`
	To         time.Time            `json:"to"`
	StepSecs   int                  `json:"step_seconds"`
	Tables     []tableHistorySeries `json:"tables"`
}

const (
	tableHistoryDefaultLimit = 10
	tableHistoryMaxLimit     = 25
	// tableHistoryBuckets is how many points a series is cut to, at most.
	tableHistoryBuckets = 60
	tableHistoryMinStep = 60
)

// handleTableHistory returns the dead and live tuple counts over time of the
// tables with the most dead tuples now: the trend behind the dead-rows list. A
// counter is a reading, not a rate, so each bucket keeps its last reading.
func (a *API) handleTableHistory(w http.ResponseWriter, r *http.Request) {
	id, e := relationID(r)
	if e != nil {
		writeError(w, 400, "invalid instance_id", e.Error())
		return
	}
	q := r.URL.Query()
	limit := tableHistoryDefaultLimit
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > tableHistoryMaxLimit {
			writeError(w, 400, "invalid limit", "allowed range: 1 to "+strconv.Itoa(tableHistoryMaxLimit))
			return
		}
		limit = n
	}
	to := time.Now().UTC()
	if raw := q.Get("to"); raw != "" {
		t, err := parseTimeParam(raw)
		if err != nil {
			writeError(w, 400, "invalid to", err.Error())
			return
		}
		to = t
	}
	from := to.Add(-time.Hour)
	if raw := q.Get("from"); raw != "" {
		t, err := parseTimeParam(raw)
		if err != nil {
			writeError(w, 400, "invalid from", err.Error())
			return
		}
		from = t
	}
	if !from.Before(to) {
		writeError(w, 400, "invalid range", "from must be before to")
		return
	}
	step := int(to.Sub(from).Seconds()) / tableHistoryBuckets
	if step < tableHistoryMinStep {
		step = tableHistoryMinStep
	}
	resp := tableHistoryResponse{InstanceID: id.String(), From: from, To: to, StepSecs: step, Tables: []tableHistorySeries{}}
	if a.pool == nil {
		writeJSON(w, 200, resp)
		return
	}
	args := []any{id, limit, from, to, float64(step)}
	database := ""
	if database = q.Get("database"); database != "" {
		args = append(args, database)
	}
	filter := ""
	if database != "" {
		filter = " AND datname=$6"
	}
	// The tables are those with the most dead tuples in the newest reading,
	// as the tables endpoint ranks them; the history is read for exactly those.
	rows, e := a.pool.Query(r.Context(), `WITH picked AS (
			SELECT datname, schemaname, relname, row_number() OVER (ORDER BY n_dead_tup DESC NULLS LAST, datname, schemaname, relname) AS rank
			FROM (SELECT DISTINCT ON (datname, schemaname, relname) datname, schemaname, relname, n_dead_tup
			        FROM metrics_tables
			       WHERE tenant_id='default' AND instance_id=$1 AND ts >= now() - interval '15 minutes'`+filter+`
			       ORDER BY datname, schemaname, relname, ts DESC) latest
			ORDER BY n_dead_tup DESC NULLS LAST, datname, schemaname, relname LIMIT $2)
		SELECT p.rank, m.datname, m.schemaname, m.relname,
		       time_bucket(make_interval(secs => $5), m.ts) AS bucket,
		       last(m.n_dead_tup, m.ts), last(m.n_live_tup, m.ts)
		FROM metrics_tables m
		JOIN picked p ON p.datname = m.datname AND p.schemaname = m.schemaname AND p.relname = m.relname
		WHERE m.tenant_id='default' AND m.instance_id=$1 AND m.ts >= $3 AND m.ts < $4
		GROUP BY p.rank, m.datname, m.schemaname, m.relname, bucket
		ORDER BY p.rank, bucket`, args...)
	if e != nil {
		writeError(w, http.StatusInternalServerError, "query failed", e.Error())
		return
	}
	defer rows.Close()
	index := map[int64]int{}
	for rows.Next() {
		var rank int64
		var datname, schema, name string
		var bucket time.Time
		var dead, live *int64
		if e := rows.Scan(&rank, &datname, &schema, &name, &bucket, &dead, &live); e != nil {
			writeError(w, http.StatusInternalServerError, "scan failed", e.Error())
			return
		}
		at, seen := index[rank]
		if !seen {
			resp.Tables = append(resp.Tables, tableHistorySeries{Datname: datname, Schemaname: schema, Relname: name, Points: []tableHistoryPoint{}})
			at = len(resp.Tables) - 1
			index[rank] = at
		}
		resp.Tables[at].Points = append(resp.Tables[at].Points, tableHistoryPoint{TS: bucket.UTC(), NDeadTup: dead, NLiveTup: live})
	}
	if e := rows.Err(); e != nil {
		writeError(w, http.StatusInternalServerError, "rows error", e.Error())
		return
	}
	writeJSON(w, 200, resp)
}

func (a *API) handleIndexes(w http.ResponseWriter, r *http.Request) {
	id, e := relationID(r)
	if e != nil {
		writeError(w, 400, "invalid instance_id", e.Error())
		return
	}
	resp := relationAPIResponse{InstanceID: id.String(), Items: []map[string]any{}}
	if a.pool == nil {
		writeJSON(w, 200, resp)
		return
	}
	// The last scan and the definition come from the definition fact the
	// index_stats check writes for the same index: its last_idx_scan label and
	// its value_text. The tuple and block figures are per-second rates, like
	// idx_scan.
	rows, e := a.pool.Query(r.Context(), `SELECT latest.datname, latest.schemaname, latest.relname, latest.indexrelname,
			latest.idx_scan, latest.index_bytes, latest.is_unique, latest.is_primary, latest.is_valid, latest.ts,
			f.labels->>'last_idx_scan', f.value_text,
			latest.idx_tup_read, latest.idx_tup_fetch, latest.idx_blks_read, latest.idx_blks_hit
		FROM (SELECT DISTINCT ON (datname, schemaname, indexrelname) *
		        FROM metrics_indexes
		       WHERE tenant_id='default' AND instance_id=$1 AND ts >= now() - interval '15 minutes'
		       ORDER BY datname, schemaname, indexrelname, ts DESC) latest
		LEFT JOIN object_facts f ON f.tenant_id='default' AND f.instance_id=$1 AND f.kind='index_def'
		     AND f.datname=latest.datname AND f.key=latest.schemaname || '.' || latest.indexrelname
		ORDER BY latest.index_bytes DESC NULLS LAST LIMIT $2`, id, relationLimit(r))
	if e != nil {
		writeError(w, http.StatusInternalServerError, "query failed", e.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var datname, schema, name, index string
		var scans, tupRead, tupFetch, blksRead, blksHit *float64
		var bytes *int64
		var unique, primary, valid *bool
		var ts time.Time
		var lastScan, definition *string
		if e := rows.Scan(&datname, &schema, &name, &index, &scans, &bytes, &unique, &primary, &valid, &ts, &lastScan, &definition,
			&tupRead, &tupFetch, &blksRead, &blksHit); e != nil {
			writeError(w, http.StatusInternalServerError, "scan failed", e.Error())
			return
		}
		item := map[string]any{"datname": datname, "schemaname": schema, "relname": name, "indexrelname": index, "ts": ts}
		putNumber(item, "idx_scan_rate", scans)
		putNumber(item, "index_bytes", bytes)
		putBool(item, "is_unique", unique)
		putBool(item, "is_primary", primary)
		putBool(item, "is_valid", valid)
		putNumber(item, "idx_tup_read_rate", tupRead)
		putNumber(item, "idx_tup_fetch_rate", tupFetch)
		if ratio := indexCacheHitRatio(blksRead, blksHit); ratio != nil {
			item["cache_hit_ratio"] = *ratio
		}
		if definition != nil && *definition != "" {
			item["definition"] = *definition
		}
		at, status := indexLastScan(lastScan)
		item["last_scan_at"] = at
		item["last_scan_status"] = status
		resp.Items = append(resp.Items, item)
	}
	if e := rows.Err(); e != nil {
		writeError(w, http.StatusInternalServerError, "rows error", e.Error())
		return
	}
	if e := setRelationMetadata(r.Context(), a.pool, &resp, id, "index_stats"); e != nil {
		writeError(w, http.StatusInternalServerError, "query failed", e.Error())
		return
	}
	writeJSON(w, 200, resp)
}

// bloatAPIResponse is the bloat endpoint's answer. skipped_tables counts the
// tables whose column statistics the monitoring role cannot read, so the
// estimate has no row for them; skipped_relations names the largest of them,
// up to a few per database. Both are read from the latest scrape of each
// database, within the estimate's window.
type bloatAPIResponse struct {
	relationAPIResponse
	SkippedTables    int64             `json:"skipped_tables"`
	SkippedRelations []skippedRelation `json:"skipped_relations"`
}

type skippedRelation struct {
	Datname    string `json:"datname"`
	Schemaname string `json:"schemaname"`
	Relname    string `json:"relname"`
}

// hotUpdateRatio is the share of a table's updates that were heap-only, from
// the per-second update rates. It is nil when either rate is missing or the
// table was not updated in the interval.
func hotUpdateRatio(updates, hot *float64) *float64 {
	if updates == nil || hot == nil || *updates <= 0 {
		return nil
	}
	ratio := *hot / *updates
	if ratio > 1 {
		ratio = 1
	}
	return &ratio
}

// indexCacheHitRatio is the share of an index's block reads served from the
// buffer cache, from the per-second block rates. It is nil when either rate is
// missing or the index read no block in the interval: an idle index has no
// ratio, not a perfect one.
func indexCacheHitRatio(read, hit *float64) *float64 {
	if read == nil || hit == nil || *read+*hit <= 0 {
		return nil
	}
	ratio := *hit / (*read + *hit)
	return &ratio
}

// indexLastScan reads the last_idx_scan label of an index's definition fact.
// An absent label means the server has no last scan for the index: the agent
// runs on PostgreSQL 15, which cannot report it, or has not reported it yet.
// An empty label means the index was not scanned since the statistics reset.
// The time is nil unless the index was scanned.
func indexLastScan(label *string) (at *time.Time, status string) {
	switch {
	case label == nil:
		return nil, "not_available"
	case *label == "":
		return nil, "never_since_reset"
	}
	parsed, err := time.Parse(time.RFC3339Nano, *label)
	if err != nil {
		return nil, "not_available"
	}
	return &parsed, "scanned"
}

func (a *API) handleBloat(w http.ResponseWriter, r *http.Request) {
	id, e := relationID(r)
	if e != nil {
		writeError(w, 400, "invalid instance_id", e.Error())
		return
	}
	method := r.URL.Query().Get("method")
	if method != "" && method != "estimate" && method != "pgstattuple" {
		writeError(w, http.StatusBadRequest, "invalid method", "allowed values: estimate, pgstattuple")
		return
	}
	kindFilter := r.URL.Query().Get("object_kind")
	if kindFilter != "" && kindFilter != "table" && kindFilter != "index" {
		writeError(w, http.StatusBadRequest, "invalid object_kind", "allowed values: table, index")
		return
	}
	resp := bloatAPIResponse{relationAPIResponse: relationAPIResponse{InstanceID: id.String(), Items: []map[string]any{}}, SkippedRelations: []skippedRelation{}}
	if a.pool == nil {
		writeJSON(w, 200, resp)
		return
	}
	// The estimate is refreshed every 6 hours; a pgstattuple measurement is
	// taken on request and stays informative for longer.
	query := `SELECT datname, schemaname, relname, indexrelname, object_kind, real_bytes, expected_bytes,
			bloat_bytes, bloat_ratio, method, ts
		FROM (SELECT DISTINCT ON (datname, schemaname, relname, indexrelname, method) *
		        FROM metrics_bloat
		       WHERE tenant_id='default' AND instance_id=$1
		         AND (ts >= now() - interval '13 hours' OR (method = 'pgstattuple' AND ts >= now() - interval '7 days'))`
	args := []any{id}
	if method != "" {
		query += " AND method=$" + strconv.Itoa(len(args)+1)
		args = append(args, method)
	}
	if kindFilter != "" {
		query += " AND object_kind=$" + strconv.Itoa(len(args)+1)
		args = append(args, kindFilter)
	}
	query += ` ORDER BY datname, schemaname, relname, indexrelname, method, ts DESC) latest
		ORDER BY bloat_bytes DESC NULLS LAST LIMIT $` + strconv.Itoa(len(args)+1)
	args = append(args, relationLimit(r))
	rows, e := a.pool.Query(r.Context(), query, args...)
	if e != nil {
		writeError(w, http.StatusInternalServerError, "query failed", e.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var datname, schema, name, index, kind, m string
		var real, expected, bloat *int64
		var ratio *float64
		var ts time.Time
		if e := rows.Scan(&datname, &schema, &name, &index, &kind, &real, &expected, &bloat, &ratio, &m, &ts); e != nil {
			writeError(w, http.StatusInternalServerError, "scan failed", e.Error())
			return
		}
		item := map[string]any{"datname": datname, "schemaname": schema, "relname": name, "indexrelname": index, "object_kind": kind, "method": m, "ts": ts}
		putNumber(item, "real_bytes", real)
		putNumber(item, "expected_bytes", expected)
		putNumber(item, "bloat_bytes", bloat)
		putNumber(item, "bloat_ratio", ratio)
		resp.Items = append(resp.Items, item)
	}
	if e := rows.Err(); e != nil {
		writeError(w, http.StatusInternalServerError, "rows error", e.Error())
		return
	}
	if e := setRelationMetadata(r.Context(), a.pool, &resp.relationAPIResponse, id, "bloat_estimate"); e != nil {
		writeError(w, http.StatusInternalServerError, "query failed", e.Error())
		return
	}
	if e := setSkippedTables(r.Context(), a.pool, &resp, id); e != nil {
		writeError(w, http.StatusInternalServerError, "query failed", e.Error())
		return
	}
	writeJSON(w, 200, resp)
}

// setSkippedTables reads the skipped-table fact of each database the instance
// reported in the estimate's window. A malformed fact is left out rather than
// failing the whole answer.
func setSkippedTables(ctx context.Context, pool dbPool, resp *bloatAPIResponse, id uuid.UUID) error {
	rows, err := pool.Query(ctx, `SELECT datname, value_json FROM object_facts
		WHERE tenant_id='default' AND instance_id=$1 AND kind=$2 AND last_seen >= now() - interval '13 hours'
		ORDER BY datname`, id, wire.BloatUnreadableFact)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var datname string
		var raw []byte
		if err := rows.Scan(&datname, &raw); err != nil {
			return err
		}
		var value wire.BloatSkippedValue
		if json.Unmarshal(raw, &value) != nil {
			continue
		}
		resp.SkippedTables += value.Count
		for _, t := range value.Tables {
			resp.SkippedRelations = append(resp.SkippedRelations, skippedRelation{Datname: datname, Schemaname: t.SchemaName, Relname: t.RelName})
		}
	}
	return rows.Err()
}

func putNumber[T int64 | float64](item map[string]any, key string, value *T) {
	if value != nil {
		item[key] = *value
	}
}

func putBool(item map[string]any, key string, value *bool) {
	if value != nil {
		item[key] = *value
	}
}

// setRelationMetadata mirrors the typed relation budgets in the read API. The
// list is incomplete when the agent's hard cardinality cap cut it (a
// cardinality_truncated event) or when its latest sample of the check held
// only the top relations (a check_partial fact, removed again by the next
// complete sample). A positive count is intentionally a lower bound: neither
// records how many candidates were left out.
func setRelationMetadata(ctx context.Context, pool dbPool, resp *relationAPIResponse, id uuid.UUID, check string) error {
	window := "15 minutes"
	if check == "bloat_estimate" {
		window = "13 hours"
	}
	var truncated bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM events
		WHERE tenant_id='default' AND instance_id=$1 AND type='cardinality_truncated'
		  AND payload->>'check'=$2 AND ts >= now() - $3::interval)
	OR EXISTS(
		SELECT 1 FROM object_facts
		WHERE tenant_id='default' AND instance_id=$1 AND kind='check_partial' AND key=$2
		  AND last_seen >= now() - interval '1 day')`, id, check, window).Scan(&truncated); err != nil {
		return err
	}
	if truncated {
		resp.Truncated = true
		n := 1
		resp.RelationsNotReported = &n
	}
	return nil
}
