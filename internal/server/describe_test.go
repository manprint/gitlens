package server

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/alert"
	"github.com/manprint/pglens/internal/alert/notify"
	"github.com/manprint/pglens/internal/watch"
)

// sqlFake answers each statement by the first key it contains.
type sqlFake struct {
	rows map[string][][]any
	errs map[string]error
	seen []string
}

func (f *sqlFake) match(sql string) string {
	keys := make([]string, 0, len(f.rows)+len(f.errs))
	for k := range f.rows {
		keys = append(keys, k)
	}
	for k := range f.errs {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, k := range keys {
		if strings.Contains(sql, k) {
			f.seen = append(f.seen, k)
			return k
		}
	}
	return ""
}

func (f *sqlFake) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	k := f.match(sql)
	if err := f.errs[k]; err != nil {
		return nil, err
	}
	return &mockRows{rows: f.rows[k]}, nil
}

func (f *sqlFake) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	k := f.match(sql)
	if err := f.errs[k]; err != nil {
		return &mockRow{err: err}
	}
	if len(f.rows[k]) == 0 {
		return &mockRow{err: pgx.ErrNoRows}
	}
	return &mockRow{vals: f.rows[k][0]}
}

var (
	descNow  = time.Date(2026, 11, 20, 15, 0, 0, 0, time.UTC)
	descInst = uuid.MustParse("11111111-2222-3333-4444-555555555555")
	descCID  = int64(42)
)

func newTestDescriber(locale string, f *sqlFake) *AlertDescriber {
	d := NewAlertDescriber(f, locale)
	d.now = func() time.Time { return descNow }
	return d
}

func baseFake() *sqlFake {
	return &sqlFake{
		rows: map[string][][]any{
			"FROM clusters":  {{"odoo"}},
			"FROM instances": {{"pg1", "10.0.0.1", 5432, 180001, "primary", descNow.Add(-2 * time.Minute), descNow.Add(-time.Hour)}},
		},
		errs: map[string]error{},
	}
}

func seededRule(f *sqlFake, sev, scope, metric, cmp string, thr float64, secs int, event, summary string) {
	var m, c, e any
	if metric != "" {
		m, c = metric, cmp
	}
	if event != "" {
		e = event
	}
	f.rows["FROM alert_rules"] = [][]any{{alert.Severity(sev), alert.Scope(scope), m, c, thr, secs, e, summary}}
}

func ptrTime(t time.Time) *time.Time { return &t }
func ptrI64(v int64) *int64          { return &v }

func incidentJSON(t *testing.T, inc watch.Incident) []byte {
	t.Helper()
	b, err := json.Marshal(inc)
	require.NoError(t, err)
	return b
}

func deadlockIncident() watch.Incident {
	page, tuple := int64(3), int64(7)
	row := &watch.LockRef{LockType: "tuple", Mode: "ExclusiveLock", Relation: "public.orders", Page: &page, Tuple: &tuple}
	return watch.Incident{
		ID: "deadlock-1", Type: watch.TypeDeadlock, CapturedAt: descNow.Add(-90 * time.Second), Database: "app",
		Relation: "public.orders", CTID: "(3,7)", LockMode: "ShareLock", WaitSeconds: 1.2,
		Sessions: []watch.Session{
			{PID: 101, Username: "odoo", ApplicationName: "web", ClientAddr: "10.1.1.5", State: "active", XactSeconds: 4, QuerySeconds: 1.5,
				Query: "UPDATE orders\n   SET qty = 1 WHERE id = 7", BlockedBy: []int32{102},
				Wait: &watch.LockRef{LockType: "transactionid", Mode: "ShareLock", TransactionID: "900", WaitSeconds: 1.2}, Row: row},
			{PID: 102, Username: "odoo", ApplicationName: "cron", State: "active", XactSeconds: 3, BlockedBy: []int32{101},
				Wait: &watch.LockRef{LockType: "tuple", Mode: "ExclusiveLock", Relation: "public.orders", Page: &page, Tuple: &tuple}, Query: "UPDATE orders SET qty = 2 WHERE id = 7"},
		},
		Suppressed: 2,
	}
}

func fieldValues(det *notify.Detail, label string) []string {
	var out []string
	for _, f := range det.Fields {
		if f.Label == label {
			out = append(out, f.Value)
		}
	}
	return out
}

func TestDescribeIncidentFieldsInBothPhases(t *testing.T) {
	f := baseFake()
	seededRule(f, "warning", "instance", "pg_deadlocks_total", "gt", 0, 0, "", "Deadlocks are occurring")
	inc := deadlockIncident()
	inc.Sessions = append(inc.Sessions, watch.Session{PID: 103, Query: "UPDATE   orders SET qty = 1\nWHERE id = 7"}, watch.Session{PID: 104, Query: "SELECT 1"})
	f.rows["FROM events WHERE instance_id"] = [][]any{{incidentJSON(t, inc)}}
	d := newTestDescriber("en", f)
	a := alert.Alert{Key: "k", RuleID: "db.deadlock_rate", Severity: alert.SeverityWarning, State: alert.StateResolved, ClusterID: &descCID, InstanceID: &descInst, Datname: "app",
		StartedAt: descNow.Add(-time.Minute), LastEvalAt: descNow}
	for _, phase := range []string{"fire", "resolve"} {
		det := d.Describe(context.Background(), a, phase)
		require.Equal(t, []string{"public.orders, row ctid (3,7)"}, fieldValues(det, "Table"), phase)
		require.Equal(t, []string{"pid 101: UPDATE orders SET qty = 1 WHERE id = 7", "pid 102: UPDATE orders SET qty = 2 WHERE id = 7"}, fieldValues(det, "Query"), phase)
		require.Equal(t, phase == "fire", strings.Contains(allLines(det), "Captured sessions") || strings.Contains(allLines(det), "## Deadlock"), phase)
	}

	inc.CTID, inc.Sessions[0].Query = "", strings.Repeat("x", 300)
	f.rows["FROM events WHERE instance_id"] = [][]any{{incidentJSON(t, inc)}}
	det := d.Describe(context.Background(), a, "resolve")
	require.Equal(t, []string{"public.orders"}, fieldValues(det, "Table"))
	q := fieldValues(det, "Query")
	require.Len(t, q, 2)
	require.Equal(t, "pid 101: "+strings.Repeat("x", 159)+"…", q[0])

	f.rows["FROM events WHERE instance_id"] = [][]any{{[]byte(`{"type":"deadlock","database":"app","sessions":[{"pid":5}]}`)}}
	det = d.Describe(context.Background(), a, "resolve")
	require.Empty(t, fieldValues(det, "Table"))
	require.Empty(t, fieldValues(det, "Query"))
}

func TestDescribeRebuiltDeadlockInRome(t *testing.T) {
	f := baseFake()
	seededRule(f, "warning", "instance", "pg_deadlocks_total", "gt", 0, 0, "", "Deadlocks are occurring")
	inc := deadlockIncident()
	inc.CTID, inc.Rebuilt, inc.Deadlocks, inc.Since = "", true, 2, ptrTime(descNow.Add(-2*time.Minute))
	f.rows["FROM events WHERE instance_id"] = [][]any{{incidentJSON(t, inc)}}
	rome, err := time.LoadLocation("Europe/Rome")
	require.NoError(t, err)
	a := alert.Alert{Key: "k", RuleID: "db.deadlock_rate", Severity: alert.SeverityWarning, State: alert.StateFiring, ClusterID: &descCID, InstanceID: &descInst, Datname: "app",
		StartedAt: descNow.Add(-time.Minute), LastEvalAt: descNow}

	text := allLines(newTestDescriber("en", f).InLocation(rome).Describe(context.Background(), a, "fire"))
	require.Contains(t, text, "## Deadlock captured at 2026-11-20 15:58:30 CET, database app\n")
	require.Contains(t, text, "PostgreSQL counted 2 deadlocks and broke them within deadlock_timeout, before the agent saw the cycle: listed are the sessions that waited on a lock, or blocked one, in this database since 2026-11-20 15:58:00 CET.")
	require.NotContains(t, text, "wait for each other in a cycle")

	inc.Deadlocks, inc.Since = 0, nil
	f.rows["FROM events WHERE instance_id"] = [][]any{{incidentJSON(t, inc)}}
	text = allLines(newTestDescriber("it", f).Describe(context.Background(), a, "fire"))
	require.Contains(t, text, "PostgreSQL ha contato un deadlock e lo ha sciolto entro deadlock_timeout, prima che l'agent vedesse il ciclo: sono elencate le sessioni che dal 2026-11-20 14:58:30 UTC hanno atteso")
}

func fieldMap(det *notify.Detail) map[string]string {
	out := map[string]string{}
	for _, f := range det.Fields {
		out[f.Label] = f.Value
	}
	return out
}

func allLines(det *notify.Detail) string {
	var b strings.Builder
	for _, s := range det.Sections {
		b.WriteString("## " + s.Title + "\n" + strings.Join(s.Lines, "\n") + "\n" + strings.Join(s.Code, "\n") + "\n")
	}
	return b.String()
}

func TestDescribeDeadlockItalian(t *testing.T) {
	f := baseFake()
	seededRule(f, "warning", "instance", "pg_deadlocks_total", "gt", 0, 0, "", "Deadlocks are occurring")
	f.rows["FROM events WHERE instance_id"] = [][]any{{incidentJSON(t, deadlockIncident())}, {[]byte(`{"type":"lock_wait","database":"other","sessions":[]}`)}, {[]byte(`{bad`)}, {[]byte(`{"type":"lock_wait","database":"app","sessions":[]}`)}}
	d := newTestDescriber("it-IT", f)
	a := alert.Alert{Key: "k", RuleID: "db.deadlock_rate", Severity: alert.SeverityWarning, State: alert.StateFiring, ClusterID: &descCID, InstanceID: &descInst, Datname: "app",
		Value: 0.0333, StartedAt: descNow.Add(-time.Minute), LastEvalAt: descNow, Labels: map[string]string{"datname": "app"}}
	det := d.Describe(context.Background(), a, "fire")

	require.Equal(t, "it", det.Locale)
	require.Equal(t, "🟠 ATTENZIONE · Deadlock · pg1 / app", det.Title)
	fm := fieldMap(det)
	require.Equal(t, "PostgreSQL ha trovato transazioni in attesa l'una dell'altra e ne ha annullata una.", fm["Problema"])
	require.Equal(t, "odoo", fm["Cluster"])
	require.Equal(t, "pg1 (10.0.0.1:5432) · primario · PostgreSQL 18.1", fm["Server"])
	require.Equal(t, "app", fm["Database"])
	require.Equal(t, "2 al minuto (soglia > 0)", fm["Valore misurato"])
	require.Equal(t, "public.orders, tupla ctid (3,7)", fm["Tabella"])
	require.Equal(t, "2026-11-20 14:59:00 UTC · da 1 min", fm["Dal"])
	require.Equal(t, "datname=app", fm["Dettagli"])
	require.Equal(t, "Locks and activity › pg1 › Captured incidents", fm["Nella console"])

	text := allLines(det)
	require.Contains(t, text, "## Deadlock, rilevazione delle 2026-11-20 14:58:30 UTC, database app\n")
	require.Contains(t, text, "Tabella public.orders, tupla ctid (3,7) · lock ShareLock · attesa più lunga 1,2 s\n")
	require.Contains(t, text, "\nLe sessioni si aspettano a vicenda in un ciclo")
	require.Contains(t, text, "• pid 101 · utente odoo · applicazione web · da 10.1.1.5 · stato active · transazione aperta da 4 s · statement in esecuzione da 1,5 s · attende ShareLock sulla transazione 900 (1,2 s) · bloccata dal pid 102 · tiene la tupla (3,7) di public.orders\n")
	require.Contains(t, text, "   statement: UPDATE orders SET qty = 1 WHERE id = 7\n")
	require.Contains(t, text, "attende ExclusiveLock su public.orders (3,7)")
	require.Contains(t, text, "2 rilevazioni scartate dal limite")
	require.Contains(t, text, "1 altra rilevazione nella stessa finestra")
	require.Contains(t, text, "## Cosa fare\nLeggi le sessioni rilevate qui sopra")
	require.Contains(t, text, "SELECT ctid, * FROM public.orders WHERE ctid = '(3,7)';")
	require.NotContains(t, text, "pg_cancel_backend", "a deadlock is already broken")
	require.Equal(t, "pglens · Regola db.deadlock_rate", det.Footer)
}

func TestDescribeResolvedIsShort(t *testing.T) {
	f := baseFake()
	seededRule(f, "warning", "instance", "pg_max_xact_age_seconds", "gt", 900, 120, "", "")
	d := newTestDescriber("en", f)
	a := alert.Alert{RuleID: "txn.long_running", Severity: alert.SeverityWarning, InstanceID: &descInst, StartedAt: descNow.Add(-75 * time.Minute), LastEvalAt: descNow, ResolvedAt: ptrTime(descNow.Add(-5 * time.Minute))}
	det := d.Describe(context.Background(), a, "resolve")
	require.Equal(t, "✅ RESOLVED · Long transaction · pg1", det.Title)
	require.Empty(t, det.Sections)
	fm := fieldMap(det)
	require.Equal(t, "2026-11-20 13:45:00 UTC · lasted 1 h 10 min", fm["Since"])
	require.Equal(t, "A transaction has been open for over 15 min: it holds its locks and stops vacuum from removing dead rows.", fm["Problem"])
	require.NotContains(t, fm, "Measured")
	require.NotContains(t, fm, "Cluster", "no cluster id, no cluster")

	a.ResolvedAt = nil
	require.Equal(t, "2026-11-20 13:45:00 UTC · lasted 1 h 15 min", fieldMap(d.Describe(context.Background(), a, "resolve"))["Since"])
}

func TestDescribeLongTransactionSuggestsEnd(t *testing.T) {
	f := baseFake()
	seededRule(f, "warning", "instance", "pg_max_xact_age_seconds", "gt", 900, 120, "", "")
	xs := descNow.Add(-20 * time.Minute)
	inc := watch.Incident{Type: watch.TypeLongXact, CapturedAt: descNow.Add(-10 * time.Minute), Database: "app", Relation: "public.stock",
		Sessions: []watch.Session{{PID: 300, Username: "etl", State: "idle in transaction", XactStart: &xs, XactSeconds: 1200, StateSeconds: 3,
			HeldRelations: []string{"public.stock", "public.a", "public.b", "public.c"}, XminAge: 2500000}}}
	f.rows["FROM events WHERE instance_id"] = [][]any{{incidentJSON(t, inc)}}
	det := newTestDescriber("en", f).Describe(context.Background(), alert.Alert{RuleID: "txn.long_running", Severity: alert.SeverityWarning, InstanceID: &descInst, Value: 1500, StartedAt: descNow.Add(-time.Minute)}, "fire")
	text := allLines(det)
	require.Equal(t, "25 min (threshold > 15 min, held for 2 min)", fieldMap(det)["Measured"])
	require.Contains(t, text, "Table public.stock\n")
	require.Contains(t, text, "idle for 3 s · holds locks on public.stock, public.a, public.b, … · holds back vacuum by 2.5 million transactions")
	require.Contains(t, text, "-- End the session of pid 300")
	require.Contains(t, text, "SELECT pg_terminate_backend(300);")
}

func TestDescribeNoCaptureSaysSo(t *testing.T) {
	f := baseFake()
	seededRule(f, "warning", "instance", "pg_max_idle_in_transaction_seconds", "gt", 300, 120, "", "")
	det := newTestDescriber("en", f).Describe(context.Background(), alert.Alert{RuleID: "conn.idle_in_transaction", InstanceID: &descInst, StartedAt: descNow}, "fire")
	require.Contains(t, allLines(det), "The agent captured no session for this alert")

	f.errs["FROM events WHERE instance_id"] = errors.New("down")
	det = newTestDescriber("en", f).Describe(context.Background(), alert.Alert{RuleID: "conn.idle_in_transaction", InstanceID: &descInst, StartedAt: descNow}, "fire")
	require.NotContains(t, allLines(det), "captured no session")

	det = newTestDescriber("en", f).Describe(context.Background(), alert.Alert{RuleID: "conn.idle_in_transaction", StartedAt: descNow}, "fire")
	require.Equal(t, "Alerts", fieldMap(det)["In the console"])
}

func TestIncidentCodeFreesTheRootBlocker(t *testing.T) {
	d := newTestDescriber("en", baseFake())
	inc := watch.Incident{Type: watch.TypeLockWait, Sessions: []watch.Session{
		{PID: 1, State: "idle in transaction"},
		{PID: 2, State: "active", BlockedBy: []int32{1}},
		{PID: 3, State: "active", BlockedBy: []int32{2}},
		{PID: 4, State: "active"},
	}}
	code := strings.Join(d.incidentCode(inc), "\n")
	require.Contains(t, code, "pg_terminate_backend(1)")
	require.NotContains(t, code, "(2)")
	require.NotContains(t, code, "(4)")
	inc.Type = watch.TypeLongQuery
	inc.Sessions = []watch.Session{{PID: 9, State: "active"}}
	require.Equal(t, []string{"-- Cancel the statement of pid 9, if it must not finish (the session stays):", "SELECT pg_cancel_backend(9);"}, d.incidentCode(inc))
}

func TestIncidentSectionBounds(t *testing.T) {
	d := newTestDescriber("en", baseFake())
	inc := watch.Incident{Type: watch.TypeLockWait, CapturedAt: descNow, SessionsOmitted: 1, Relation: "public.t"}
	for i := 0; i < 9; i++ {
		inc.Sessions = append(inc.Sessions, watch.Session{PID: int32(i), Query: strings.Repeat("x", 500), Wait: &watch.LockRef{LockType: "advisory", Mode: "ExclusiveLock"}})
	}
	s, _ := d.incidentSection(inc, 0)
	require.Equal(t, "Lock wait captured at 2026-11-20 15:00:00 UTC", s.Title)
	require.Contains(t, s.Lines, "4 more sessions not listed")
	require.Contains(t, strings.Join(s.Lines, "\n"), "waits for ExclusiveLock on advisory")
	for _, l := range s.Lines {
		require.LessOrEqual(t, len([]rune(l)), maxStatementRunes+20)
	}
}

func TestDescribeUnknownRuleAndTarget(t *testing.T) {
	f := &sqlFake{rows: map[string][][]any{}, errs: map[string]error{"FROM instances": errors.New("gone"), "FROM clusters": errors.New("gone")}}
	d := newTestDescriber("en", f)
	a := alert.Alert{RuleID: "custom.x", Severity: alert.SeverityCritical, Summary: "Custom thing", ClusterID: &descCID, InstanceID: &descInst, StartedAt: descNow}
	det := d.Describe(context.Background(), a, "fire")
	require.Equal(t, "🔴 CRITICAL · Custom thing · "+descInst.String(), det.Title)
	fm := fieldMap(det)
	require.Equal(t, "Custom thing", fm["Problem"])
	require.Equal(t, "Cluster 42", fm["Cluster"])
	require.Equal(t, descInst.String(), fm["Server"])
	require.Nil(t, d.actions(alert.Rule{ID: "custom.x"}, nil))

	a.InstanceID, a.ClusterID, a.Summary = nil, nil, ""
	require.Equal(t, "🔴 CRITICAL · custom.x", d.Describe(context.Background(), a, "fire").Title)
}

func TestDescribeCustomMetricRule(t *testing.T) {
	f := baseFake()
	f.rows["FROM instances"] = [][]any{{"", "db1", 5432, 160004, "standby", descNow, nil}}
	seededRule(f, "info", "database", "pg_table_bloat_bytes", "ge", 1<<30, 0, "", "")
	det := newTestDescriber("en", f).Describe(context.Background(), alert.Alert{RuleID: "custom.bloat", Severity: alert.SeverityInfo, InstanceID: &descInst, Value: 3 << 30, StartedAt: descNow}, "fire")
	require.Equal(t, "🔵 INFO · custom.bloat · db1:5432", det.Title)
	fm := fieldMap(det)
	require.Equal(t, "pg_table_bloat_bytes is ≥ 1 GiB", fm["Problem"])
	require.Equal(t, "db1:5432 · standby · PostgreSQL 16.4", fm["Server"])
	require.Equal(t, "3 GiB (threshold ≥ 1 GiB)", fm["Measured"])
	require.Equal(t, "Instance overview › db1:5432", fm["In the console"])
}

func TestDescribeEventRuleTitleAndProblem(t *testing.T) {
	f := baseFake()
	seededRule(f, "warning", "instance", "", "", 0, 0, "lock_wait_captured", "")
	f.rows["FROM events WHERE instance_id"] = [][]any{{incidentJSON(t, watch.Incident{Type: watch.TypeLockWait, CapturedAt: descNow, Sessions: []watch.Session{{PID: 5}}})}}
	det := newTestDescriber("en", f).Describe(context.Background(), alert.Alert{RuleID: "custom.waits", Severity: alert.SeverityWarning, InstanceID: &descInst, StartedAt: descNow}, "fire")
	require.Equal(t, "🟠 WARNING · Lock wait captured · pg1", det.Title)
	require.Equal(t, "Event lock_wait_captured recorded", fieldMap(det)["Problem"])
	require.Contains(t, allLines(det), "• pid 5\n")

	f.rows["FROM alert_rules"] = [][]any{{alert.SeverityWarning, alert.ScopeInstance, nil, nil, 0.0, 0, "odd_event", ""}}
	f.rows["FROM events WHERE type"] = [][]any{{[]byte(`{"id":"x","b":true}`)}}
	det = newTestDescriber("en", f).Describe(context.Background(), alert.Alert{RuleID: "custom.odd", Severity: alert.SeverityWarning, InstanceID: &descInst, StartedAt: descNow}, "fire")
	require.Equal(t, "Event odd_event recorded", fieldMap(det)["Problem"])
	require.Contains(t, allLines(det), "## Latest event\nb: true\n")
}

func TestDescribeWraparoundListsTablesAndVacuum(t *testing.T) {
	f := baseFake()
	f.rows["FROM metrics_tables"] = [][]any{
		{"app", "public", "orders", int64(1_400_000_000), descNow.Add(-48 * time.Hour), int64(0), int64(0), int64(0), int64(0)},
		{"app", "Sales", "order", int64(1_100_000_000), nil, int64(0), int64(0), int64(0), int64(0)},
		{"crm", "public", "leads", int64(900_000_000), nil, int64(0), int64(0), int64(0), int64(0)},
		{"bad"},
	}
	seededRule(f, "critical", "instance", "pg_max_datfrozenxid_age", "gt", 1e9, 300, "", "")
	det := newTestDescriber("en", f).Describe(context.Background(), alert.Alert{RuleID: "txn.wraparound_risk", Severity: alert.SeverityCritical, InstanceID: &descInst, Value: 1.4e9, StartedAt: descNow}, "fire")
	text := allLines(det)
	require.Equal(t, "1.4 billion (threshold > 1 billion, held for 5 min)", fieldMap(det)["Measured"])
	require.Contains(t, text, "• public.orders (database app): age 1.4 billion transactions, last vacuum 2026-11-18 15:00:00 UTC\n")
	require.Contains(t, text, `• "Sales"."order" (database app): age 1.1 billion transactions, last vacuum never`)
	require.Contains(t, text, "-- Database: app\nVACUUM (FREEZE, VERBOSE) public.orders;\nVACUUM (FREEZE, VERBOSE) \"Sales\".\"order\";\n-- Database: crm\nVACUUM (FREEZE, VERBOSE) public.leads;")
}

func TestDescribeDiskListsLargestTables(t *testing.T) {
	f := baseFake()
	f.rows["FROM metrics_tables"] = [][]any{{"app", "public", "logs", int64(0), nil, int64(50 << 30), int64(40 << 30), int64(10 << 30), int64(1200)}}
	seededRule(f, "critical", "instance", "host_disk_free_ratio", "lt", 0.15, 300, "", "")
	det := newTestDescriber("en", f).Describe(context.Background(), alert.Alert{RuleID: "disk.free_low", Severity: alert.SeverityCritical, InstanceID: &descInst, Value: 0.083, StartedAt: descNow}, "fire")
	require.Equal(t, "Less than 15% of the data filesystem is free: when it fills up, PostgreSQL stops.", fieldMap(det)["Problem"])
	require.Equal(t, "8.3% (threshold < 15%, held for 5 min)", fieldMap(det)["Measured"])
	require.Contains(t, allLines(det), "• public.logs (database app): 50 GiB (table 40 GiB, TOAST 10 GiB), 1200 dead rows")
	require.NotContains(t, allLines(det), "VACUUM")

	f.errs["FROM metrics_tables"] = errors.New("x")
	require.NotContains(t, allLines(newTestDescriber("en", f).Describe(context.Background(), alert.Alert{RuleID: "disk.free_low", InstanceID: &descInst}, "fire")), "Largest")
}

func TestDescribeReplication(t *testing.T) {
	f := baseFake()
	f.rows["FROM metrics_replication"] = [][]any{{"pg2", "pg2_slot", "async", 45.0, int64(64 << 20)}, {"pg3", "", "", nil, nil}}
	seededRule(f, "critical", "cluster", "pg_replication_lag_seconds", "gt", 30, 120, "", "")
	det := newTestDescriber("en", f).Describe(context.Background(), alert.Alert{RuleID: "replica.all_standbys_lagging", Severity: alert.SeverityCritical, ClusterID: &descCID, Value: 45, StartedAt: descNow}, "fire")
	require.Equal(t, "🔴 CRITICAL · Every standby lagging · odoo", det.Title)
	require.Equal(t, "Cluster › odoo", fieldMap(det)["In the console"])
	text := allLines(det)
	require.Contains(t, text, "• pg2: replay lag 45 s, 64 MiB of WAL behind, async, slot pg2_slot\n")
	require.Contains(t, text, "• pg3: replay lag ?, ? of WAL behind, \n")
}

func TestDescribeConnections(t *testing.T) {
	f := baseFake()
	f.rows["DISTINCT ON (metric, labels)"] = [][]any{
		{"pg_connections_used", map[string]string{}, 92.0},
		{"pg_connections_limit", map[string]string{}, 100.0},
		{"pg_connections_by_application", map[string]string{"application_name": "web"}, 60.0},
		{"pg_connections_by_application", map[string]string{"application_name": ""}, 2.0},
		{"pg_connections_by_application", map[string]string{"application_name": "cron"}, 30.0},
		{"pg_connections_by_user", map[string]string{"usename": "odoo"}, 90.0},
	}
	seededRule(f, "warning", "instance", "pg_connections_used_ratio", "gt", 0.8, 120, "", "")
	det := newTestDescriber("it", f).Describe(context.Background(), alert.Alert{RuleID: "conn.near_max", Severity: alert.SeverityWarning, InstanceID: &descInst, Value: 0.92, StartedAt: descNow}, "fire")
	text := allLines(det)
	require.Equal(t, "Le connessioni superano il 80% di max_connections: a breve le nuove sessioni verranno rifiutate.", fieldMap(det)["Problema"])
	require.Contains(t, text, "92 connessioni in uso su 100\n")
	require.Contains(t, text, "• per applicazione: web 60, cron 30, — 2\n")
	require.Contains(t, text, "• per utente: odoo 90")
}

func TestDescribeChecksAgentArchiver(t *testing.T) {
	f := baseFake()
	f.rows["FROM check_results"] = [][]any{{"locks", "", descNow}, {"bloat", "app", descNow}, {"bad"}}
	f.rows["kind = 'check_skip'"] = [][]any{{"stat_statements", "missing extension pg_stat_statements"}, {1}}
	text := allLines(newTestDescriber("en", f).Describe(context.Background(), alert.Alert{RuleID: "check_failing", InstanceID: &descInst, StartedAt: descNow}, "fire"))
	require.Contains(t, text, "• locks: last result 2026-11-20 15:00:00 UTC, failed\n• bloat on database app")
	require.Contains(t, text, "• stat_statements skipped: missing extension pg_stat_statements")

	text = allLines(newTestDescriber("en", f).Describe(context.Background(), alert.Alert{RuleID: "agent_down", InstanceID: &descInst, StartedAt: descNow}, "fire"))
	require.Contains(t, text, "## Agent\nLast data received 2026-11-20 14:58:00 UTC (2 min ago)\nAgent started 2026-11-20 14:00:00 UTC\n")
	require.Contains(t, text, "## What to do\nCheck that the agent container")

	f.rows["DISTINCT ON (metric, labels)"] = [][]any{
		{"pg_archiver_last_failed_age_seconds", map[string]string{}, 30.0},
		{"pg_archiver_last_archived_age_seconds", map[string]string{}, 7200.0},
		{"pg_archiver_ready_files", map[string]string{}, 1.0},
	}
	seededRule(f, "critical", "instance", "pg_archiver_failed_ratio", "gt", 0, 300, "", "")
	text = allLines(newTestDescriber("en", f).Describe(context.Background(), alert.Alert{RuleID: "archive.failing", InstanceID: &descInst, StartedAt: descNow}, "fire"))
	require.Contains(t, text, "## WAL archiver\nLast failure 30 s ago\nLast WAL archived 2 h ago\n1 WAL file waiting to be archived\n")
}

func TestDescribeSlotInactiveSuggestsDrop(t *testing.T) {
	f := baseFake()
	f.rows["FROM events WHERE type"] = [][]any{{[]byte(`{"id":"e1","slot_name":"old's","retained_bytes":1048576,"detail":{"a":1},"none":null,"note":"line1\nline2"}`)}}
	det := newTestDescriber("en", f).Describe(context.Background(), alert.Alert{RuleID: "slot_inactive", Severity: alert.SeverityWarning, InstanceID: &descInst, ClusterID: &descCID, StartedAt: descNow}, "fire")
	text := allLines(det)
	require.Equal(t, "🟠 WARNING · Replication slot inactive · pg1", det.Title)
	require.Contains(t, text, "## Latest event\ndetail: {\"a\":1}\nnone: —\nnote: line1 line2\nretained_bytes: 1048576\nslot_name: old's\n")
	require.Contains(t, text, "SELECT pg_drop_replication_slot('old''s');")

	f.rows["FROM events WHERE type"] = [][]any{{[]byte(`[1]`)}}
	require.NotContains(t, allLines(newTestDescriber("en", f).Describe(context.Background(), alert.Alert{RuleID: "slot_inactive", InstanceID: &descInst}, "fire")), "Latest event")
	delete(f.rows, "FROM events WHERE type")
	require.NotContains(t, allLines(newTestDescriber("en", f).Describe(context.Background(), alert.Alert{RuleID: "slot_inactive", InstanceID: &descInst}, "fire")), "Latest event")
}

func TestDescribeNeedsInstanceForInstanceEvidence(t *testing.T) {
	f := baseFake()
	d := newTestDescriber("en", f)
	for _, id := range []string{"txn.wraparound_risk", "conn.near_max", "check_failing", "archive.failing", "replica.lag_high"} {
		f.rows["FROM alert_rules"] = nil
		r := map[string]alert.Rule{
			"txn.wraparound_risk": {ID: id, Metric: "pg_max_datfrozenxid_age"}, "conn.near_max": {ID: id, Metric: "pg_connections_used_ratio"},
			"check_failing": {ID: id, Metric: "pglens_check_error_rate"}, "archive.failing": {ID: id, Metric: "pg_archiver_failed_ratio"},
			"replica.lag_high": {ID: id, Metric: "pg_replication_lag_seconds"},
		}[id]
		require.Empty(t, d.evidence(context.Background(), alert.Alert{RuleID: id}, r, target{}, nil, errNoIncidents).sections, id)
	}
}

func TestDescribeFormats(t *testing.T) {
	en, it := newTestDescriber("en", baseFake()), newTestDescriber("it", baseFake())
	for _, c := range []struct {
		d    *AlertDescriber
		v    time.Duration
		want string
	}{
		{en, -time.Second, "0 ms"}, {en, 250 * time.Millisecond, "250 ms"}, {it, 1500 * time.Millisecond, "1,5 s"},
		{en, 3*time.Minute + 12*time.Second, "3 min 12 s"}, {en, 5 * time.Minute, "5 min"},
		{en, 65 * time.Minute, "1 h 5 min"}, {it, 50 * time.Hour, "2 g 2 h"}, {en, 48 * time.Hour, "2 d"},
	} {
		require.Equal(t, c.want, c.d.fmtDuration(c.v))
	}
	require.Equal(t, "512 B", en.fmtBytes(512))
	require.Equal(t, "1,5 KiB", it.fmtBytes(1536))
	require.Equal(t, "42", en.fmtCount(42))
	require.Equal(t, "3.2 million", en.fmtCount(3.2e6))
	require.Equal(t, "7", en.fmtValue("plain", 7))
	require.Equal(t, "0", en.fmtValue("x_total", 0))
	require.Equal(t, "2.5 per second", en.fmtValue("x_total", 2.5))
	require.Equal(t, "3 al minuto", it.fmtValue("x_total", 0.05))
	require.Equal(t, "18.1", pgVersion(180001))
	require.Equal(t, "9.6.24", pgVersion(90624))
	require.Equal(t, "18", pgVersion(18))
	require.Equal(t, "orders", quoteIdent("orders"))
	require.Equal(t, `"user"`, quoteIdent("user"))
	require.Equal(t, `"My ""T"""`, quoteIdent(`My "T"`))
	for c, s := range map[alert.Comparator]string{alert.GT: ">", alert.GE: "≥", alert.LT: "<", alert.LE: "≤", alert.EQ: "=", alert.NE: "≠", "xx": "xx"} {
		require.Equal(t, s, cmpSymbol(c))
	}
	require.Equal(t, "ab…", clipText("abcd", 3))
	require.Equal(t, "a=1, b=2", labelList(map[string]string{"b": "2", "a": "1"}))
	require.Equal(t, "a b", oneLine(" a\n\t b "))
	require.Equal(t, 5*time.Minute+120*time.Second+900*time.Second, incidentWindow(alert.Rule{For: 2 * time.Minute, Metric: "x_seconds", Threshold: 900}))
	require.Nil(t, incidentTypesFor(alert.Rule{Metric: "up"}))
	require.Equal(t, []string{watch.TypeLongXact}, incidentTypesFor(alert.Rule{EventType: "long_transaction_captured"}))
	require.Equal(t, ptrI64(1), ptrI64(1))
}

// NOTI-014 (describer): a channel's language overrides the server's for that
// message only; the describer keeps its own language for the others.
func TestDescribeInChannelLanguage(t *testing.T) {
	f := baseFake()
	seededRule(f, "warning", "instance", "pg_deadlocks_total", "gt", 0, 0, "", "Deadlocks are occurring")
	d := newTestDescriber("en", f)
	a := alert.Alert{Key: "k", RuleID: "db.deadlock_rate", Severity: alert.SeverityWarning, State: alert.StateFiring, ClusterID: &descCID, InstanceID: &descInst, Datname: "app",
		Value: 0.0333, StartedAt: descNow.Add(-time.Minute), LastEvalAt: descNow}

	it := d.DescribeIn(context.Background(), a, "fire", "it")
	require.Equal(t, "it", it.Locale)
	require.Equal(t, "🟠 ATTENZIONE · Deadlock · pg1 / app", it.Title)
	require.Equal(t, "pglens · Regola db.deadlock_rate", it.Footer)

	en := d.Describe(context.Background(), a, "fire")
	require.Equal(t, "en", en.Locale)
	require.Equal(t, "pglens · Rule db.deadlock_rate", en.Footer)
}
