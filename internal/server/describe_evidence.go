package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/manprint/pglens/internal/alert"
	"github.com/manprint/pglens/internal/alert/notify"
	"github.com/manprint/pglens/internal/i18n"
	"github.com/manprint/pglens/internal/watch"
)

// Bounds that keep a notification inside a chat message.
const (
	maxEvidenceRows    = 5
	maxIncidentSession = 6
	maxStatementRunes  = 240
	// a notification field shows the first statements of a capture, shorter
	maxFieldStatements     = 2
	maxFieldStatementRunes = 160
	maxEventLines          = 12
)

// evidence is the data a notification shows, and the statements it suggests.
type evidence struct {
	sections []notify.Section
	code     []string
}

func (e *evidence) add(s notify.Section) {
	if len(s.Lines) > 0 {
		e.sections = append(e.sections, s)
	}
}

// incidentTypesFor lists the captures of the agent's watcher that explain a
// rule, the most telling first, from its metric or event, so a rule a user
// writes on the same metric gets them too.
func incidentTypesFor(r alert.Rule) []string {
	switch r.Metric {
	case "pg_deadlocks_total":
		return []string{watch.TypeDeadlock, watch.TypeLockWait}
	case "pg_max_xact_age_seconds":
		return []string{watch.TypeLongXact, watch.TypeIdleInXact, watch.TypeLongQuery}
	case "pg_max_idle_in_transaction_seconds":
		return []string{watch.TypeIdleInXact, watch.TypeLongXact}
	}
	for typ, ev := range incidentEventTypes {
		if r.EventType == ev {
			return []string{typ}
		}
	}
	return nil
}

func isReplicationRule(r alert.Rule) bool {
	switch r.Metric {
	case "pg_replication_lag_seconds", "pg_sync_standby_count", "pg_missing_standby_count":
		return true
	}
	return false
}

func (d *AlertDescriber) evidence(ctx context.Context, a alert.Alert, r alert.Rule, tg target, incs []watch.Incident, incErr error) evidence {
	var ev evidence
	switch types := incidentTypesFor(r); {
	case types != nil:
		if incErr == nil {
			d.incidentEvidence(&ev, incs)
		}
	case r.Metric == "pg_max_datfrozenxid_age":
		d.tableEvidence(ctx, &ev, a, true)
	case r.Metric == "host_disk_free_ratio":
		d.tableEvidence(ctx, &ev, a, false)
	case isReplicationRule(r):
		d.replicationEvidence(ctx, &ev, a)
	case r.Metric == "pg_connections_used_ratio":
		d.connectionEvidence(ctx, &ev, a)
	case r.Metric == "pglens_check_error_rate":
		d.checkEvidence(ctx, &ev, a)
	case r.Metric == "up":
		d.agentEvidence(&ev, tg)
	case r.Metric == "pg_archiver_failed_ratio":
		d.archiverEvidence(ctx, &ev, a)
	case r.EventType != "":
		d.eventEvidence(ctx, &ev, a, r)
	}
	return ev
}

// actions is the rule's advice, then the statements the evidence suggests.
func (d *AlertDescriber) actions(r alert.Rule, code []string) *notify.Section {
	s := notify.Section{Title: d.t("section.actions", nil), Code: code}
	if key := "rule." + r.ID + ".actions"; i18n.Has(key) {
		s.Lines = strings.Split(d.t(key, nil), "\n")
	}
	if len(s.Lines) == 0 && len(s.Code) == 0 {
		return nil
	}
	return &s
}

// incidentWindow is how far back a capture may explain the alert: the
// condition's own length (the threshold of a duration metric, the rule's
// hold time) and a margin for the push and evaluation delays.
func incidentWindow(r alert.Rule) time.Duration {
	w := r.For + 5*time.Minute
	if strings.HasSuffix(r.Metric, "_seconds") && r.Threshold > 0 {
		w += time.Duration(r.Threshold * float64(time.Second))
	}
	return w
}

// incidents are the watcher's captures that explain a, the most telling type
// first and the newest first within a type, in a's database; nil for a rule no
// capture explains.
func (d *AlertDescriber) incidents(ctx context.Context, a alert.Alert, r alert.Rule) ([]watch.Incident, error) {
	types := incidentTypesFor(r)
	if types == nil || a.InstanceID == nil {
		return nil, errNoIncidents
	}
	eventTypes := make([]string, len(types))
	for i, t := range types {
		eventTypes[i] = incidentEventTypes[t]
	}
	from, to := a.StartedAt.Add(-incidentWindow(r)), d.now().Add(time.Minute)
	rows, err := d.db.Query(ctx, `SELECT payload FROM events WHERE instance_id = $1 AND type = ANY($2) AND ts BETWEEN $3 AND $4 ORDER BY array_position($2, type), ts DESC LIMIT 50`, *a.InstanceID, eventTypes, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var found []watch.Incident
	for rows.Next() {
		var raw []byte
		if rows.Scan(&raw) != nil {
			continue
		}
		var inc watch.Incident
		if json.Unmarshal(raw, &inc) != nil {
			continue
		}
		if a.Datname != "" && inc.Database != "" && inc.Database != a.Datname {
			continue
		}
		found = append(found, inc)
	}
	return found, rows.Err()
}

// errNoIncidents marks an alert with no capture to look for.
var errNoIncidents = errors.New("no incident evidence")

func (d *AlertDescriber) incidentEvidence(ev *evidence, found []watch.Incident) {
	if len(found) == 0 {
		ev.add(notify.Section{Title: d.t("incident.section", nil), Lines: []string{d.t("incident.none", nil)}})
		return
	}
	sec, code := d.incidentSection(found[0], len(found)-1)
	ev.add(sec)
	ev.code = append(ev.code, code...)
}

// incidentSection renders one capture: what, where (table and row), every
// session with its user, application, client, transaction, wait and
// statement, and the statements that inspect the row or free the lock.
func (d *AlertDescriber) incidentSection(inc watch.Incident, more int) (notify.Section, []string) {
	typ := d.t("incident.type."+inc.Type, nil)
	at := d.fmtTime(inc.CapturedAt)
	s := notify.Section{Title: d.t("incident.heading", i18n.Args{"type": typ, "at": at, "database": inc.Database})}
	if inc.Database == "" {
		s.Title = d.t("incident.heading_nodb", i18n.Args{"type": typ, "at": at})
	}
	if inc.Relation != "" {
		parts := []string{d.t("incident.object_table", i18n.Args{"relation": inc.Relation})}
		if inc.CTID != "" {
			parts[0] = d.t("incident.object_row", i18n.Args{"relation": inc.Relation, "ctid": inc.CTID})
		}
		if inc.LockMode != "" {
			parts = append(parts, d.t("incident.lock_mode", i18n.Args{"mode": inc.LockMode}))
		}
		if inc.WaitSeconds > 0 {
			parts = append(parts, d.t("incident.longest_wait", i18n.Args{"duration": d.fmtDuration(seconds(inc.WaitSeconds))}))
		}
		s.Lines = append(s.Lines, strings.Join(parts, " · "))
	}
	switch {
	case inc.Type == watch.TypeDeadlock && inc.Rebuilt:
		since := inc.CapturedAt
		if inc.Since != nil {
			since = *inc.Since
		}
		s.Lines = append(s.Lines, d.t("incident.rebuilt", i18n.Args{"n": max(inc.Deadlocks, 1), "since": d.fmtTime(since)}))
	case inc.Type == watch.TypeDeadlock:
		s.Lines = append(s.Lines, d.t("incident.cycle", nil))
	}
	sessions := inc.Sessions
	omitted := inc.SessionsOmitted
	if len(sessions) > maxIncidentSession {
		omitted += len(sessions) - maxIncidentSession
		sessions = sessions[:maxIncidentSession]
	}
	for _, ss := range sessions {
		s.Lines = append(s.Lines, "• "+d.sessionLine(ss))
		if ss.Query != "" {
			s.Lines = append(s.Lines, "   "+d.t("session.query", i18n.Args{"query": clipText(oneLine(ss.Query), maxStatementRunes)}))
		}
	}
	if omitted > 0 {
		s.Lines = append(s.Lines, d.t("incident.omitted", i18n.Args{"n": omitted}))
	}
	if inc.Suppressed > 0 {
		s.Lines = append(s.Lines, d.t("incident.suppressed", i18n.Args{"n": inc.Suppressed}))
	}
	if more > 0 {
		s.Lines = append(s.Lines, d.t("incident.more", i18n.Args{"n": more}))
	}
	return s, d.incidentCode(inc)
}

// incidentObject names the table, and the row when there is one, a capture is
// about; empty when it names none.
func (d *AlertDescriber) incidentObject(inc watch.Incident) string {
	switch {
	case inc.Relation == "":
		return ""
	case inc.CTID != "":
		return d.t("table.row", i18n.Args{"relation": inc.Relation, "ctid": inc.CTID})
	}
	return inc.Relation
}

// incidentStatements are the first maxFieldStatements distinct statements of a
// capture, longest wait first, each clipped to maxFieldStatementRunes with its pid.
func (d *AlertDescriber) incidentStatements(inc watch.Incident) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range inc.Sessions {
		q := oneLine(s.Query)
		if q == "" || seen[q] {
			continue
		}
		seen[q] = true
		out = append(out, d.t("statement.value", i18n.Args{"pid": strconv.Itoa(int(s.PID)), "query": clipText(q, maxFieldStatementRunes)}))
		if len(out) == maxFieldStatements {
			break
		}
	}
	return out
}

func (d *AlertDescriber) sessionLine(s watch.Session) string {
	parts := []string{d.t("session.pid", i18n.Args{"pid": strconv.Itoa(int(s.PID))})}
	add := func(key, name, value string) {
		if value != "" {
			parts = append(parts, d.t(key, i18n.Args{name: value}))
		}
	}
	add("session.user", "user", s.Username)
	add("session.app", "app", s.ApplicationName)
	add("session.client", "addr", s.ClientAddr)
	add("session.state", "state", s.State)
	if s.XactSeconds > 0 {
		add("session.xact", "duration", d.fmtDuration(seconds(s.XactSeconds)))
	}
	switch {
	case strings.HasPrefix(s.State, "idle in transaction") && s.StateSeconds > 0:
		add("session.idle", "duration", d.fmtDuration(seconds(s.StateSeconds)))
	case s.State == "active" && s.QuerySeconds >= 1:
		add("session.query_age", "duration", d.fmtDuration(seconds(s.QuerySeconds)))
	}
	if w := s.Wait; w != nil {
		object := w.Relation
		if ctid := w.CTID(); ctid != "" && object != "" {
			object += " " + ctid
		}
		var line string
		switch {
		case w.LockType == "transactionid" && object == "":
			line = d.t("session.waits_xid", i18n.Args{"mode": w.Mode, "xid": w.TransactionID})
		default:
			if object == "" {
				object = w.LockType
			}
			line = d.t("session.waits", i18n.Args{"mode": w.Mode, "object": object})
		}
		if w.WaitSeconds > 0 {
			line += " (" + d.fmtDuration(seconds(w.WaitSeconds)) + ")"
		}
		parts = append(parts, line)
	}
	if len(s.BlockedBy) > 0 {
		pids := make([]string, len(s.BlockedBy))
		for i, p := range s.BlockedBy {
			pids[i] = strconv.Itoa(int(p))
		}
		parts = append(parts, d.t("session.blocked_by", i18n.Args{"pids": strings.Join(pids, ", ")}))
	}
	if r := s.Row; r != nil && r.Relation != "" && r.CTID() != "" {
		parts = append(parts, d.t("session.holds_row", i18n.Args{"ctid": r.CTID(), "relation": r.Relation}))
	}
	if n := len(s.HeldRelations); n > 0 {
		held := s.HeldRelations
		if n > 3 {
			held = append(append([]string{}, held[:3]...), "…")
		}
		parts = append(parts, d.t("session.holds_tables", i18n.Args{"tables": strings.Join(held, ", ")}))
	}
	if s.XminAge > 0 {
		parts = append(parts, d.t("session.xmin", i18n.Args{"n": d.fmtCount(float64(s.XminAge))}))
	}
	return strings.Join(parts, " · ")
}

// incidentCode suggests the statements for one capture: read the contested
// row, then end what holds the others. A deadlock is already broken by
// PostgreSQL, so it gets no cancel.
func (d *AlertDescriber) incidentCode(inc watch.Incident) []string {
	var code []string
	if inc.Relation != "" && inc.CTID != "" {
		code = append(code, "-- "+d.t("action.row", i18n.Args{"relation": inc.Relation, "database": inc.Database}),
			fmt.Sprintf("SELECT ctid, * FROM %s WHERE ctid = '%s';", inc.Relation, inc.CTID))
	}
	switch inc.Type {
	case watch.TypeDeadlock:
		return code
	case watch.TypeLockWait:
		for _, s := range rootBlockers(inc.Sessions) {
			code = append(code, d.endSession(s)...)
		}
	default:
		for _, s := range inc.Sessions {
			code = append(code, d.endSession(s)...)
		}
	}
	return code
}

// rootBlockers are the sessions of a lock wait that block others and wait for
// no one: ending them frees the chain.
func rootBlockers(sessions []watch.Session) []watch.Session {
	blocking := map[int32]bool{}
	for _, s := range sessions {
		for _, p := range s.BlockedBy {
			blocking[p] = true
		}
	}
	var out []watch.Session
	for _, s := range sessions {
		if blocking[s.PID] && len(s.BlockedBy) == 0 {
			out = append(out, s)
		}
	}
	return out
}

// endSession is the statement that frees what s holds: an idle transaction can
// only be ended with its session; a running statement can be cancelled first.
func (d *AlertDescriber) endSession(s watch.Session) []string {
	pid := strconv.Itoa(int(s.PID))
	args := i18n.Args{"pid": pid}
	if s.State == "active" {
		return []string{"-- " + d.t("action.cancel", args), "SELECT pg_cancel_backend(" + pid + ");"}
	}
	return []string{"-- " + d.t("action.terminate", args), "SELECT pg_terminate_backend(" + pid + ");"}
}

// tableEvidence lists the tables with the oldest relfrozenxid (frozen) or the
// largest ones, from the latest table statistics of the instance.
func (d *AlertDescriber) tableEvidence(ctx context.Context, ev *evidence, a alert.Alert, frozen bool) {
	if a.InstanceID == nil {
		return
	}
	order := "total_bytes"
	if frozen {
		order = "relfrozenxid_age"
	}
	rows, err := d.db.Query(ctx, `SELECT datname, schemaname, relname, COALESCE(relfrozenxid_age, 0), GREATEST(last_vacuum, last_autovacuum),
	       COALESCE(total_bytes, 0), COALESCE(table_bytes, 0), COALESCE(toast_bytes, 0), COALESCE(n_dead_tup, 0)
	  FROM (SELECT DISTINCT ON (datname, schemaname, relname) *
	          FROM metrics_tables WHERE instance_id = $1 AND ts > $2
	         ORDER BY datname, schemaname, relname, ts DESC) t
	 WHERE `+order+` IS NOT NULL
	 ORDER BY `+order+` DESC LIMIT $3`, *a.InstanceID, d.now().Add(-2*time.Hour), maxEvidenceRows)
	if err != nil {
		return
	}
	defer rows.Close()
	key := "size"
	if frozen {
		key = "frozen"
	}
	s := notify.Section{Title: d.t(key+".section", nil)}
	lastDB := ""
	for rows.Next() {
		var db, schema, rel string
		var age, total, table, toast, dead int64
		var vacuum pgtype.Timestamptz
		if rows.Scan(&db, &schema, &rel, &age, &vacuum, &total, &table, &toast, &dead) != nil {
			continue
		}
		name := quoteIdent(schema) + "." + quoteIdent(rel)
		vac := d.t("never", nil)
		if vacuum.Valid {
			vac = d.fmtTime(vacuum.Time)
		}
		s.Lines = append(s.Lines, "• "+d.t(key+".line", i18n.Args{"relation": name, "database": db, "age": d.fmtCount(float64(age)), "vacuum": vac,
			"total": d.fmtBytes(float64(total)), "table": d.fmtBytes(float64(table)), "toast": d.fmtBytes(float64(toast)), "dead": d.fmtCount(float64(dead))}))
		if frozen {
			if db != lastDB {
				ev.code = append(ev.code, "-- "+d.t("field.database", nil)+": "+db)
				lastDB = db
			}
			ev.code = append(ev.code, "VACUUM (FREEZE, VERBOSE) "+name+";")
		}
	}
	ev.add(s)
}

func (d *AlertDescriber) replicationEvidence(ctx context.Context, ev *evidence, a alert.Alert) {
	if a.ClusterID == nil {
		return
	}
	rows, err := d.db.Query(ctx, `SELECT label, slot_name, COALESCE(sync_state, ''), replay_lag_sec, replay_lag_bytes FROM (
	  SELECT DISTINCT ON (r.instance_id, r.slot_name) COALESCE(NULLIF(i.display_name, ''), i.addr || ':' || i.port) AS label,
	         r.slot_name, r.sync_state, r.replay_lag_sec, r.replay_lag_bytes
	    FROM metrics_replication r JOIN instances i ON i.instance_id = r.instance_id
	   WHERE r.cluster_id = $1 AND r.ts > $2
	   ORDER BY r.instance_id, r.slot_name, r.ts DESC) t
	 ORDER BY replay_lag_sec DESC NULLS LAST LIMIT 10`, *a.ClusterID, d.now().Add(-10*time.Minute))
	if err != nil {
		return
	}
	defer rows.Close()
	s := notify.Section{Title: d.t("repl.section", nil)}
	for rows.Next() {
		var label, slot, state string
		var lag pgtype.Float8
		var lagBytes pgtype.Int8
		if rows.Scan(&label, &slot, &state, &lag, &lagBytes) != nil {
			continue
		}
		lagText, bytesText := "?", "?"
		if lag.Valid {
			lagText = d.fmtDuration(seconds(lag.Float64))
		}
		if lagBytes.Valid {
			bytesText = d.fmtBytes(float64(lagBytes.Int64))
		}
		if slot != "" {
			state = strings.TrimPrefix(state+", "+d.t("repl.slot", i18n.Args{"slot": slot}), ", ")
		}
		s.Lines = append(s.Lines, "• "+d.t("repl.line", i18n.Args{"instance": label, "lag": lagText, "bytes": bytesText, "state": state}))
	}
	ev.add(s)
}

// connectionDims are the breakdowns of the connections, with the label each
// one is keyed by.
var connectionDims = []struct{ metric, label, key string }{
	{"pg_connections_by_application", "application_name", "application"},
	{"pg_connections_by_user", "usename", "user"},
	{"pg_connections_by_client", "client_addr", "client"},
	{"pg_connections_by_database", "datname", "database"},
}

// latestMetrics reads the latest value of each series of the metrics named,
// for one instance.
func (d *AlertDescriber) latestMetrics(ctx context.Context, instance uuid.UUID, names []string, since time.Duration) map[string][]labelledValue {
	rows, err := d.db.Query(ctx, `SELECT DISTINCT ON (metric, labels) metric, labels, value FROM metrics
	 WHERE instance_id = $1 AND metric = ANY($2) AND ts > $3 ORDER BY metric, labels, ts DESC`, instance, names, d.now().Add(-since))
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := map[string][]labelledValue{}
	for rows.Next() {
		var metric string
		var labels map[string]string
		var v float64
		if rows.Scan(&metric, &labels, &v) != nil {
			continue
		}
		out[metric] = append(out[metric], labelledValue{labels, v})
	}
	return out
}

type labelledValue struct {
	labels map[string]string
	value  float64
}

func (d *AlertDescriber) connectionEvidence(ctx context.Context, ev *evidence, a alert.Alert) {
	if a.InstanceID == nil {
		return
	}
	names := []string{"pg_connections_used", "pg_connections_limit"}
	for _, dim := range connectionDims {
		names = append(names, dim.metric)
	}
	m := d.latestMetrics(ctx, *a.InstanceID, names, 10*time.Minute)
	s := notify.Section{Title: d.t("conn.section", nil)}
	if used, limit := m["pg_connections_used"], m["pg_connections_limit"]; len(used) > 0 && len(limit) > 0 {
		s.Lines = append(s.Lines, d.t("conn.used", i18n.Args{"used": used[0].value, "limit": limit[0].value}))
	}
	for _, dim := range connectionDims {
		vals := m[dim.metric]
		sort.Slice(vals, func(i, j int) bool { return vals[i].value > vals[j].value })
		if len(vals) > maxEvidenceRows {
			vals = vals[:maxEvidenceRows]
		}
		list := make([]string, 0, len(vals))
		for _, v := range vals {
			name := v.labels[dim.label]
			if name == "" {
				name = "—"
			}
			list = append(list, name+" "+i18n.Number(d.locale, v.value))
		}
		if len(list) > 0 {
			s.Lines = append(s.Lines, "• "+d.t("conn.line", i18n.Args{"dimension": d.t("conn.dim."+dim.key, nil), "list": strings.Join(list, ", ")}))
		}
	}
	ev.add(s)
}

func (d *AlertDescriber) checkEvidence(ctx context.Context, ev *evidence, a alert.Alert) {
	if a.InstanceID == nil {
		return
	}
	s := notify.Section{Title: d.t("checks.section", nil)}
	if rows, err := d.db.Query(ctx, `SELECT check_name, datname, last_result_at FROM check_results WHERE instance_id = $1 AND last_failed ORDER BY check_name, datname LIMIT 10`, *a.InstanceID); err == nil {
		for rows.Next() {
			var check, db string
			var at time.Time
			if rows.Scan(&check, &db, &at) != nil {
				continue
			}
			key := "checks.line"
			if db != "" {
				key = "checks.line_db"
			}
			s.Lines = append(s.Lines, "• "+d.t(key, i18n.Args{"check": check, "database": db, "at": d.fmtTime(at)}))
		}
		rows.Close()
	}
	if rows, err := d.db.Query(ctx, `SELECT key, COALESCE(value_text, '') FROM object_facts WHERE instance_id = $1 AND kind = 'check_skip' ORDER BY key LIMIT 10`, *a.InstanceID); err == nil {
		for rows.Next() {
			var check, reason string
			if rows.Scan(&check, &reason) != nil {
				continue
			}
			s.Lines = append(s.Lines, "• "+d.t("checks.skip", i18n.Args{"check": check, "reason": reason}))
		}
		rows.Close()
	}
	ev.add(s)
}

func (d *AlertDescriber) agentEvidence(ev *evidence, tg target) {
	s := notify.Section{Title: d.t("agent.section", nil)}
	if tg.lastSeen != nil {
		s.Lines = append(s.Lines, d.t("agent.last_seen", i18n.Args{"at": d.fmtTime(*tg.lastSeen), "ago": d.t("ago", i18n.Args{"duration": d.fmtDuration(d.now().Sub(*tg.lastSeen))})}))
	}
	if tg.started != nil {
		s.Lines = append(s.Lines, d.t("agent.started", i18n.Args{"at": d.fmtTime(*tg.started)}))
	}
	ev.add(s)
}

func (d *AlertDescriber) archiverEvidence(ctx context.Context, ev *evidence, a alert.Alert) {
	if a.InstanceID == nil {
		return
	}
	m := d.latestMetrics(ctx, *a.InstanceID, []string{"pg_archiver_last_failed_age_seconds", "pg_archiver_last_archived_age_seconds", "pg_archiver_ready_files"}, 15*time.Minute)
	s := notify.Section{Title: d.t("archiver.section", nil)}
	ago := func(metric, key string) {
		if v := m[metric]; len(v) > 0 {
			s.Lines = append(s.Lines, d.t(key, i18n.Args{"ago": d.t("ago", i18n.Args{"duration": d.fmtDuration(seconds(v[0].value))})}))
		}
	}
	ago("pg_archiver_last_failed_age_seconds", "archiver.last_failed")
	ago("pg_archiver_last_archived_age_seconds", "archiver.last_archived")
	if v := m["pg_archiver_ready_files"]; len(v) > 0 {
		s.Lines = append(s.Lines, d.t("archiver.ready", i18n.Args{"n": v[0].value}))
	}
	ev.add(s)
}

// eventEvidence shows the latest event of an event rule, field by field.
func (d *AlertDescriber) eventEvidence(ctx context.Context, ev *evidence, a alert.Alert, r alert.Rule) {
	var raw []byte
	err := d.db.QueryRow(ctx, `SELECT payload FROM events WHERE type = $1 AND ($2::bigint IS NULL OR cluster_id = $2) AND ($3::uuid IS NULL OR instance_id = $3) AND ts <= $4 ORDER BY ts DESC LIMIT 1`,
		r.EventType, a.ClusterID, a.InstanceID, d.now().Add(time.Minute)).Scan(&raw)
	if err != nil {
		return
	}
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return
	}
	keys := make([]string, 0, len(payload))
	for k := range payload {
		if k != "id" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	s := notify.Section{Title: d.t("event.section", nil)}
	for _, k := range keys {
		if len(s.Lines) == maxEventLines {
			break
		}
		s.Lines = append(s.Lines, d.t("event.line", i18n.Args{"key": k, "value": clipText(d.eventValue(payload[k]), 200)}))
	}
	ev.add(s)
	if slot, _ := payload["slot_name"].(string); slot != "" && r.EventType == "slot_inactive" {
		ev.code = append(ev.code, "-- "+d.t("action.slot_drop", i18n.Args{"slot": slot}), "SELECT pg_drop_replication_slot('"+strings.ReplaceAll(slot, "'", "''")+"');")
	}
}

func (d *AlertDescriber) eventValue(v any) string {
	switch x := v.(type) {
	case string:
		return oneLine(x)
	case float64:
		return i18n.Number(d.locale, x)
	case nil:
		return "—"
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func seconds(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }

// oneLine folds a statement's whitespace, so it reads on one line.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
