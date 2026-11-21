//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/alert"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/wire"
)

// recordingDB keeps every query and scan error the describer would swallow.
type recordingDB struct {
	db   describeDB
	mu   sync.Mutex
	errs []string
}

func (r *recordingDB) note(sql string, err error) {
	if err == nil || errors.Is(err, pgx.ErrNoRows) {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errs = append(r.errs, err.Error()+" in: "+strings.Join(strings.Fields(sql), " "))
}

func (r *recordingDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	rows, err := r.db.Query(ctx, sql, args...)
	r.note(sql, err)
	if err != nil {
		return nil, err
	}
	return &recordingRows{Rows: rows, r: r, sql: sql}, nil
}

func (r *recordingDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return &recordingRow{row: r.db.QueryRow(ctx, sql, args...), r: r, sql: sql}
}

type recordingRows struct {
	pgx.Rows
	r   *recordingDB
	sql string
}

func (w *recordingRows) Scan(dest ...any) error {
	err := w.Rows.Scan(dest...)
	w.r.note(w.sql, err)
	return err
}

func (w *recordingRows) Err() error {
	err := w.Rows.Err()
	w.r.note(w.sql, err)
	return err
}

type recordingRow struct {
	row pgx.Row
	r   *recordingDB
	sql string
}

func (w *recordingRow) Scan(dest ...any) error {
	err := w.row.Scan(dest...)
	w.r.note(w.sql, err)
	return err
}

// INT-DESCRIBE-001: every builtin and seeded rule is described against the
// real schema with no query or scan error, and the deadlock notification
// names the table, the tuple and the sessions the agent captured.
func TestINTDESCRIBE001_EveryRuleDescribedAgainstTheSchema(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	router := NewRouter(UIConfig{}, nil, nil, NewAuth("test-token"), NewInventory(pool), NewPipeline(pool, nil), NewAPI(pool), NewTopologyAPI(pool), NewAshAPI(pool))

	iid := uuid.New()
	now := time.Now().UTC().Truncate(time.Microsecond)
	deadlock := []byte(`{"id":"deadlock-1-101","type":"deadlock","captured_at":"` + now.Add(-time.Minute).Format(time.RFC3339Nano) + `","database":"app","relation":"public.orders","ctid":"(3,7)","lock_mode":"ShareLock","wait_seconds":1.5,
		"sessions":[{"pid":101,"usename":"odoo","application_name":"web","state":"active","query":"UPDATE orders SET qty = 1 WHERE id = 7","blocked_by":[102],"wait":{"locktype":"transactionid","mode":"ShareLock","transactionid":"900","wait_seconds":1.5},"row":{"locktype":"tuple","mode":"ExclusiveLock","relation":"public.orders","page":3,"tuple":7}},
		{"pid":102,"usename":"odoo","application_name":"cron","state":"active","query":"UPDATE orders SET qty = 2 WHERE id = 7","blocked_by":[101]}]}`)
	env := wire.Envelope{
		ProtocolVersion: wire.ProtocolVersion,
		AgentID:         uuid.NewString(),
		SentAt:          now,
		Instances: []wire.Instance{{
			InstanceID: iid.String(), ClusterID: pgtype.ClusterID(4242).String(), ClusterIDSource: string(pgtype.IDSourceManual),
			Addr: "10.0.0.7", Port: 5432, Role: "primary", PGVersion: 180001, PermTier: "T0",
			Results: []wire.Result{{Check: "ash", TS: now, Facts: []wire.Fact{{Kind: "incident", Key: "deadlock-1-101", Labels: map[string]string{"type": "deadlock"}, ValueJSON: deadlock}}}},
		}},
	}
	body, err := json.Marshal(env)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/push", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())

	var cid int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT cluster_id FROM instances WHERE instance_id = $1`, iid).Scan(&cid))
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO metrics_tables (ts, cluster_id, instance_id, datname, schemaname, relname, n_dead_tup, total_bytes, table_bytes, toast_bytes, relfrozenxid_age, last_autovacuum)
			VALUES ($1,$2,$3,'app','public','orders',12,2097152,1048576,1048576,1200000000,$1)`, []any{now, cid, iid}},
		{`INSERT INTO metrics_replication (ts, tenant_id, cluster_id, instance_id, slot_name, edge_type, slot_active, sync_state, replay_lag_sec, replay_lag_bytes)
			VALUES ($1,'default',$2,$3,'pg2_slot','streaming',true,'async',45,1048576)`, []any{now, cid, iid}},
		{`INSERT INTO check_results (instance_id, check_name, datname, first_result_at, last_result_at, last_failed) VALUES ($1,'locks','',$2,$2,true)`, []any{iid, now}},
		{`INSERT INTO object_facts (tenant_id, cluster_id, instance_id, datname, kind, key, labels, value_text, first_seen, last_seen, changed_at)
			VALUES ('default',$1,$2,'','check_skip','stat_statements','{}','missing extension',$3,$3,$3)`, []any{cid, iid, now}},
		{`INSERT INTO metrics (ts, tenant_id, cluster_id, instance_id, datname, metric, labels, series_id, value) VALUES
			($1,'default',$2,$3,'','pg_connections_used','{}',9001,92),
			($1,'default',$2,$3,'','pg_connections_limit','{}',9002,100),
			($1,'default',$2,$3,'','pg_connections_by_application','{"application_name":"web"}',9003,60),
			($1,'default',$2,$3,'','pg_archiver_last_failed_age_seconds','{}',9004,30)`, []any{now, cid, iid}},
		{`INSERT INTO events (tenant_id, ts, type, cluster_id, instance_id, payload) VALUES ('default',$1,'slot_inactive',$2,$3,'{"slot_name":"old","retained_bytes":1048576}')`, []any{now, cid, iid}},
	} {
		_, err := pool.Exec(ctx, q.sql, q.args...)
		require.NoError(t, err, q.sql)
	}

	rows, err := pool.Query(ctx, `SELECT DISTINCT rule_id FROM alert_rules`)
	require.NoError(t, err)
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	require.NoError(t, err)
	for _, b := range alert.Builtin() {
		ids = append(ids, b.ID)
	}
	require.Contains(t, ids, "db.deadlock_rate")

	db := &recordingDB{db: pool}
	for _, locale := range []string{"en", "it"} {
		d := NewAlertDescriber(db, locale)
		for _, id := range append(ids, "custom.unknown") {
			for _, phase := range []string{"fire", "resolve"} {
				a := alert.Alert{Key: id, RuleID: id, Severity: alert.SeverityWarning, ClusterID: &cid, InstanceID: &iid, Datname: "app", Value: 1, StartedAt: now.Add(-2 * time.Minute), LastEvalAt: now}
				det := d.Describe(ctx, a, phase)
				require.NotEmpty(t, det.Title, id)
				require.NotEmpty(t, det.Fields, id)
			}
		}
	}
	require.Empty(t, db.errs)

	det := NewAlertDescriber(db, "en").Describe(ctx, alert.Alert{RuleID: "db.deadlock_rate", Severity: alert.SeverityWarning, ClusterID: &cid, InstanceID: &iid, Datname: "app", Value: 1, StartedAt: now}, "fire")
	text := allLines(det)
	require.Contains(t, det.Title, "Deadlock · 10.0.0.7:5432 / app")
	require.Contains(t, fieldMap(det)["Server"], "PostgreSQL 18.1")
	require.Contains(t, text, "Table public.orders, row ctid (3,7) · lock mode ShareLock")
	require.Contains(t, text, "• pid 101 · user odoo · application web")
	require.Contains(t, text, "SELECT ctid, * FROM public.orders WHERE ctid = '(3,7)';")
	require.Empty(t, db.errs)
}
