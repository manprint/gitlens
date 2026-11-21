package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// period is one of the two time ranges a compare endpoint reads, [From, To).
type period struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

func (p period) seconds() float64 { return p.To.Sub(p.From).Seconds() }

const (
	compareMaxPeriod    = 31 * 24 * time.Hour
	compareFutureSlack  = time.Minute
	defaultCompareLimit = 50
	maxCompareLimit     = 200
	compareTimeout      = 10 * time.Second
	compareNormalized   = "per_second"
)

// compareParamError names the query parameter a compare request got wrong.
type compareParamError struct {
	field, detail string
}

func (e *compareParamError) Error() string { return e.field + ": " + e.detail }

// parseComparePeriods reads periods A and B of a compare request (D7): both
// required, each from before its to, at most 31 days long and ending at most
// one minute after now. The periods may overlap.
func parseComparePeriods(q url.Values, now time.Time) (a, b period, err error) {
	read := func(name string) (time.Time, error) {
		v := q.Get(name)
		if v == "" {
			return time.Time{}, &compareParamError{name, "query param " + name + " is required"}
		}
		t, perr := parseTimeParam(v)
		if perr != nil {
			return time.Time{}, &compareParamError{name, perr.Error()}
		}
		return t, nil
	}
	one := func(label string) (period, error) {
		from, err := read(label + "_from")
		if err != nil {
			return period{}, err
		}
		to, err := read(label + "_to")
		if err != nil {
			return period{}, err
		}
		switch {
		case !from.Before(to):
			return period{}, &compareParamError{label + "_to", label + "_from must be before " + label + "_to"}
		case to.Sub(from) > compareMaxPeriod:
			return period{}, &compareParamError{label + "_to", "a period may span at most 31 days"}
		case to.After(now.Add(compareFutureSlack)):
			return period{}, &compareParamError{label + "_to", label + "_to is more than one minute in the future"}
		}
		return period{From: from.UTC(), To: to.UTC()}, nil
	}
	if a, err = one("a"); err != nil {
		return period{}, period{}, err
	}
	if b, err = one("b"); err != nil {
		return period{}, period{}, err
	}
	return a, b, nil
}

// writeCompareError answers a compare request whose parameters failed.
func writeCompareError(w http.ResponseWriter, err error) {
	var pe *compareParamError
	if errors.As(err, &pe) {
		writeError(w, http.StatusBadRequest, "invalid "+pe.field, pe.detail)
		return
	}
	writeError(w, http.StatusBadRequest, "invalid request", err.Error())
}

// compareInstance parses instance_id and, with a pool, answers 404 for an
// instance the server does not know.
func compareInstance(ctx context.Context, w http.ResponseWriter, pool dbPool, s string) (uuid.UUID, bool) {
	if s == "" {
		writeError(w, http.StatusBadRequest, "missing instance_id", "query param instance_id is required")
		return uuid.Nil, false
	}
	iid, err := uuid.Parse(s)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", err.Error())
		return uuid.Nil, false
	}
	if pool == nil {
		return iid, true
	}
	var one int
	err = pool.QueryRow(ctx, `SELECT 1 FROM instances WHERE instance_id=$1`, iid).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "instance not found", fmt.Sprintf("instance %s not found", s))
		return uuid.Nil, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed", err.Error())
		return uuid.Nil, false
	}
	return iid, true
}

// compareStatus says which periods a row was seen in.
func compareStatus(inA, inB bool) string {
	switch {
	case inA && inB:
		return "both"
	case inB:
		return "new"
	default:
		return "gone"
	}
}

// compareDelta is b − a with a missing side counted as 0.
func compareDelta(a, b *float64) *float64 {
	if a == nil && b == nil {
		return nil
	}
	d := deref0(b) - deref0(a)
	return &d
}

// compareRatio is b / a, nil when a is 0 or missing.
func compareRatio(a, b *float64) *float64 {
	if a == nil || *a == 0 {
		return nil
	}
	r := deref0(b) / *a
	return &r
}

func deref0(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

// perSecond divides a total by the period length.
func perSecond(v *float64, secs float64) *float64 {
	if v == nil || secs <= 0 {
		return nil
	}
	r := *v / secs
	return &r
}

// ----- /api/v1/statements/compare -----

// statementMetrics are the metrics of one statement in one period, named as
// /api/v1/statements names them; totals are per second of the period.
type statementMetrics struct {
	Calls           *float64 `json:"calls"`
	TotalExec       *float64 `json:"total_exec_time_ms"`
	Rows            *float64 `json:"rows"`
	MeanExec        *float64 `json:"mean_exec_time"`
	SharedBlksHit   *float64 `json:"shared_blks_hit"`
	SharedBlksRead  *float64 `json:"shared_blks_read"`
	WALBytes        *float64 `json:"wal_bytes"`
	TempBlksWritten *float64 `json:"temp_blks_written"`
}

type statementCompareRow struct {
	Datname   string            `json:"datname"`
	QueryID   string            `json:"queryid"`
	QueryText *string           `json:"query_text"`
	Status    string            `json:"status"`
	A         *statementMetrics `json:"a"`
	B         *statementMetrics `json:"b"`
	Delta     statementMetrics  `json:"delta"`
	Ratio     statementMetrics  `json:"ratio"`
}

type statementCompareResp struct {
	A          period                `json:"a"`
	B          period                `json:"b"`
	OrderBy    string                `json:"order_by"`
	Normalized string                `json:"normalized"`
	Rows       []statementCompareRow `json:"rows"`
}

// statementTotals are one side of a joined row as the SQL returns it: window
// totals, not yet per second.
type statementTotals struct {
	calls, exec, rows, hit, read, wal, temp, mean *float64
}

// statementOrders maps each order_by value /api/v1/statements accepts to the
// metric compare rows are ordered by, the aggregate column first.
var statementOrders = map[string]struct {
	column string
	metric func(*statementMetrics) *float64
}{}

func init() {
	add := func(column string, metric func(*statementMetrics) *float64, names ...string) {
		for _, n := range names {
			statementOrders[n] = struct {
				column string
				metric func(*statementMetrics) *float64
			}{column, metric}
		}
	}
	add("total_calls", func(m *statementMetrics) *float64 { return m.Calls }, "calls", "calls_rate", "total_calls")
	add("total_exec_time", func(m *statementMetrics) *float64 { return m.TotalExec }, "total_exec_time", "total_exec_time_ms", "exec_time", "total_time")
	add("total_rows", func(m *statementMetrics) *float64 { return m.Rows }, "rows", "rows_rate", "total_rows")
	add("total_hit", func(m *statementMetrics) *float64 { return m.SharedBlksHit }, "shared_blks_hit", "shared_blks_hit_rate")
	add("total_read", func(m *statementMetrics) *float64 { return m.SharedBlksRead }, "shared_blks_read", "shared_blks_read_rate")
	add("total_wal", func(m *statementMetrics) *float64 { return m.WALBytes }, "wal_bytes", "wal_bytes_rate")
	add("total_temp", func(m *statementMetrics) *float64 { return m.TempBlksWritten }, "temp_blks_written", "temp_blks_written_rate")
	add("mean_exec", func(m *statementMetrics) *float64 { return m.MeanExec }, "mean_exec_time", "mean_time")
}

// metricsOf turns window totals into per-second rates; the mean is per call
// and stays as it is.
func metricsOf(t *statementTotals, secs float64) *statementMetrics {
	if t == nil {
		return nil
	}
	return &statementMetrics{
		Calls: perSecond(t.calls, secs), TotalExec: perSecond(t.exec, secs), Rows: perSecond(t.rows, secs),
		MeanExec: t.mean, SharedBlksHit: perSecond(t.hit, secs), SharedBlksRead: perSecond(t.read, secs),
		WALBytes: perSecond(t.wal, secs), TempBlksWritten: perSecond(t.temp, secs),
	}
}

func metricsPair(a, b *statementMetrics, f func(x, y *float64) *float64) statementMetrics {
	var x, y statementMetrics
	if a != nil {
		x = *a
	}
	if b != nil {
		y = *b
	}
	return statementMetrics{
		Calls: f(x.Calls, y.Calls), TotalExec: f(x.TotalExec, y.TotalExec), Rows: f(x.Rows, y.Rows),
		MeanExec: f(x.MeanExec, y.MeanExec), SharedBlksHit: f(x.SharedBlksHit, y.SharedBlksHit),
		SharedBlksRead: f(x.SharedBlksRead, y.SharedBlksRead), WALBytes: f(x.WALBytes, y.WALBytes),
		TempBlksWritten: f(x.TempBlksWritten, y.TempBlksWritten),
	}
}

// statementCompareRowOf builds a response row from the totals of each side
// (nil when the statement was not seen in that period).
func statementCompareRowOf(datname string, qid int64, text *string, a, b *statementTotals, pa, pb period) statementCompareRow {
	ma, mb := metricsOf(a, pa.seconds()), metricsOf(b, pb.seconds())
	return statementCompareRow{
		Datname: datname, QueryID: strconv.FormatInt(qid, 10), QueryText: text,
		Status: compareStatus(a != nil, b != nil), A: ma, B: mb,
		Delta: metricsPair(ma, mb, compareDelta), Ratio: metricsPair(ma, mb, compareRatio),
	}
}

// sortStatementCompare orders rows by the absolute change of metric, largest
// first, then by queryid.
func sortStatementCompare(rows []statementCompareRow, metric func(*statementMetrics) *float64) {
	sort.SliceStable(rows, func(i, j int) bool {
		di, dj := math.Abs(deref0(metric(&rows[i].Delta))), math.Abs(deref0(metric(&rows[j].Delta)))
		if di != dj {
			return di > dj
		}
		return rows[i].QueryID < rows[j].QueryID
	})
}

// statementPeriodSQL is the aggregate of /api/v1/statements for one period
// (handleStatements), grouped by (datname, queryid) so the two periods join
// one row per statement.
func statementPeriodSQL(name, database string, from, to int) string {
	const seconds = "COALESCE(s.sample_seconds, 60)"
	db := ""
	if database != "" {
		db = " AND s.datname=$2"
	}
	return fmt.Sprintf(`%s AS (
	SELECT MIN(s.cluster_id) AS cluster_id, s.queryid, s.datname,
		SUM(s.calls_rate * %[2]s) AS total_calls,
		SUM(s.exec_time_rate_ms * %[2]s) AS total_exec_time,
		SUM(s.rows_rate * %[2]s) AS total_rows,
		SUM(s.shared_blks_hit_rate * %[2]s) AS total_hit,
		SUM(s.shared_blks_read_rate * %[2]s) AS total_read,
		SUM(s.wal_bytes_rate * %[2]s) AS total_wal,
		SUM(s.temp_blks_written_rate * %[2]s) AS total_temp
	FROM metrics_statements s
	WHERE s.tenant_id='default' AND s.instance_id=$1%[3]s AND s.ts >= $%[4]d AND s.ts < $%[5]d
	GROUP BY s.queryid, s.datname
)`, name, seconds, db, from, to)
}

func (a *API) handleStatementsCompare(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	pa, pb, err := parseComparePeriods(q, time.Now())
	if err != nil {
		writeCompareError(w, err)
		return
	}
	limit, err := parseLimitParam(q.Get("limit"), defaultCompareLimit, maxCompareLimit)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid limit", err.Error())
		return
	}
	orderBy := strings.ToLower(q.Get("order_by"))
	if orderBy == "" {
		orderBy = "total_exec_time"
	}
	order, ok := statementOrders[orderBy]
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid order_by", fmt.Sprintf("unknown order_by %q", q.Get("order_by")))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), compareTimeout)
	defer cancel()
	iid, ok := compareInstance(ctx, w, a.pool, q.Get("instance_id"))
	if !ok {
		return
	}
	resp := statementCompareResp{A: pa, B: pb, OrderBy: orderBy, Normalized: compareNormalized, Rows: []statementCompareRow{}}
	if a.pool == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	database := q.Get("database")
	args := []any{iid}
	if database != "" {
		args = append(args, database)
	}
	n := len(args)
	args = append(args, pa.From, pa.To, pb.From, pb.To, pa.seconds(), pb.seconds(), limit)
	// The mean is per call and compared as it is; every other metric is a
	// window total compared per second of its period.
	key := func(side string, secs int) string {
		if order.column == "mean_exec" {
			return fmt.Sprintf("CASE WHEN %[1]s.total_calls > 0 THEN %[1]s.total_exec_time / %[1]s.total_calls END", side)
		}
		return fmt.Sprintf("%s.%s / $%d", side, order.column, secs)
	}
	query := `WITH ` + statementPeriodSQL("pa", database, n+1, n+2) + `, ` + statementPeriodSQL("pb", database, n+3, n+4) + fmt.Sprintf(`, joined AS (
	SELECT COALESCE(pb.cluster_id, pa.cluster_id) AS cluster_id,
		COALESCE(pb.datname, pa.datname) AS datname, COALESCE(pb.queryid, pa.queryid) AS queryid,
		pa.queryid IS NOT NULL AS in_a, pb.queryid IS NOT NULL AS in_b,
		pa.total_calls AS a_calls, pa.total_exec_time AS a_exec, pa.total_rows AS a_rows, pa.total_hit AS a_hit,
		pa.total_read AS a_read, pa.total_wal AS a_wal, pa.total_temp AS a_temp,
		CASE WHEN pa.total_calls > 0 THEN pa.total_exec_time / pa.total_calls END AS a_mean,
		pb.total_calls AS b_calls, pb.total_exec_time AS b_exec, pb.total_rows AS b_rows, pb.total_hit AS b_hit,
		pb.total_read AS b_read, pb.total_wal AS b_wal, pb.total_temp AS b_temp,
		CASE WHEN pb.total_calls > 0 THEN pb.total_exec_time / pb.total_calls END AS b_mean,
		abs(COALESCE(%s, 0) - COALESCE(%s, 0)) AS change
	FROM pa FULL OUTER JOIN pb ON pa.datname = pb.datname AND pa.queryid = pb.queryid
), top AS (
	SELECT * FROM joined ORDER BY change DESC, queryid::text COLLATE "C" LIMIT $%d
)
SELECT top.datname, top.queryid,
	(SELECT qt.query_text FROM query_texts qt
		WHERE qt.tenant_id='default' AND qt.cluster_id=top.cluster_id
		  AND qt.datname=top.datname AND qt.queryid=top.queryid
		ORDER BY qt.last_seen DESC LIMIT 1) AS qtext,
	top.in_a, top.in_b,
	top.a_calls, top.a_exec, top.a_rows, top.a_hit, top.a_read, top.a_wal, top.a_temp, top.a_mean,
	top.b_calls, top.b_exec, top.b_rows, top.b_hit, top.b_read, top.b_wal, top.b_temp, top.b_mean
FROM top`, key("pb", n+6), key("pa", n+5), n+7)
	rows, err := a.pool.Query(ctx, query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed", err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var (
			datname  string
			qid      int64
			text     sql.NullString
			inA, inB bool
			ta, tb   statementTotals
		)
		if err := rows.Scan(&datname, &qid, &text, &inA, &inB,
			&ta.calls, &ta.exec, &ta.rows, &ta.hit, &ta.read, &ta.wal, &ta.temp, &ta.mean,
			&tb.calls, &tb.exec, &tb.rows, &tb.hit, &tb.read, &tb.wal, &tb.temp, &tb.mean); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed", err.Error())
			return
		}
		var sa, sb *statementTotals
		if inA {
			sa = &ta
		}
		if inB {
			sb = &tb
		}
		var tp *string
		if text.Valid {
			tp = &text.String
		}
		resp.Rows = append(resp.Rows, statementCompareRowOf(datname, qid, tp, sa, sb, pa, pb))
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "rows error", err.Error())
		return
	}
	sortStatementCompare(resp.Rows, order.metric)
	writeJSON(w, http.StatusOK, resp)
}
