package alert

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type storeDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}
type pgStore struct {
	pool       storeDB
	purgeBatch int // rows per DELETE of a purge; defaultPurgeBatch when zero
}

// defaultPurgeBatch bounds the rows that one DELETE of a purge removes.
const defaultPurgeBatch = 1000

func NewPgStore(pool *pgxpool.Pool) Store { return &pgStore{pool: pool, purgeBatch: defaultPurgeBatch} }

func (s *pgStore) Rules(ctx context.Context) ([]Rule, error) {
	rows, err := s.pool.Query(ctx, `SELECT rule_id, enabled, severity, scope, metric, comparator, threshold, for_seconds, event_type, summary FROM alert_rules WHERE enabled ORDER BY rule_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		var r Rule
		var metric, comparator, eventType pgtype.Text
		var threshold pgtype.Float8
		var seconds int
		if err := rows.Scan(&r.ID, &r.Enabled, &r.Severity, &r.Scope, &metric, &comparator, &threshold, &seconds, &eventType, &r.Summary); err != nil {
			return nil, err
		}
		if metric.Valid {
			r.Metric = metric.String
		}
		if comparator.Valid {
			r.Comparator = Comparator(comparator.String)
		}
		if threshold.Valid {
			r.Threshold = threshold.Float64
		}
		if eventType.Valid {
			r.EventType = eventType.String
		}
		r.For = time.Duration(seconds) * time.Second
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *pgStore) Silences(ctx context.Context, now time.Time) ([]Silence, error) {
	rows, err := s.pool.Query(ctx, `SELECT silence_id, matchers, reason, starts_at, ends_at FROM silences WHERE tenant_id='default' AND starts_at <= $1 AND ends_at > $1 ORDER BY ends_at`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Silence
	for rows.Next() {
		var z Silence
		var raw []byte
		if err := rows.Scan(&z.ID, &raw, &z.Reason, &z.StartsAt, &z.EndsAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &z.Matchers); err != nil {
			return nil, err
		}
		out = append(out, z)
	}
	return out, rows.Err()
}

func (s *pgStore) Upsert(ctx context.Context, a Alert) error {
	// A nil map marshals to null, and the column would keep that literal. The
	// column's default is the empty object, so that is what a nil map writes.
	labels := a.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	raw, err := json.Marshal(labels)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO alerts (alert_key, rule_id, severity, state, cluster_id, instance_id, datname, labels, value, summary, started_at, last_eval_at, resolved_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) ON CONFLICT (tenant_id, alert_key, started_at) DO UPDATE SET state=EXCLUDED.state, value=EXCLUDED.value, last_eval_at=EXCLUDED.last_eval_at, resolved_at=EXCLUDED.resolved_at`, a.Key, a.RuleID, a.Severity, a.State, a.ClusterID, a.InstanceID, a.Datname, raw, a.Value, a.Summary, a.StartedAt, a.LastEvalAt, a.ResolvedAt)
	return err
}

func (s *pgStore) ClaimNotification(ctx context.Context, dedupID, channel, phase string, a Alert) (bool, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `INSERT INTO notifications (alert_key, started_at, channel, phase, ok, attempts) VALUES ($1,$2,$3,$4,false,0) ON CONFLICT (tenant_id, alert_key, started_at, channel, phase) DO NOTHING RETURNING notification_id`, dedupID, a.StartedAt, channel, phase).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *pgStore) MarkNotification(ctx context.Context, dedupID, channel, phase string, ok bool, attempts int, sendErr error) error {
	var msg any
	if sendErr != nil {
		msg = sendErr.Error()
	}
	_, err := s.pool.Exec(ctx, `UPDATE notifications SET ok=$1, attempts=$2, error=$3, sent_at=now() WHERE tenant_id='default' AND alert_key=$4 AND channel=$5 AND phase=$6`, ok, attempts, msg, dedupID, channel, phase)
	return err
}

// Purge removes the history that is older than before: resolved alerts, the
// deliveries of episodes that no longer have an open alert, and silences that
// ended before it. A pending or firing alert keeps its row, and so does every
// delivery of an episode still open, because the dedup of that episode is
// the claim those rows hold. The rows go in batches, each committed by itself,
// so a purge that the pass's deadline cuts short keeps what it removed and the
// next attempt carries on.
func (s *pgStore) Purge(ctx context.Context, before time.Time) error {
	batch := s.purgeBatch
	if batch <= 0 {
		batch = defaultPurgeBatch
	}
	if err := purgeBatches(ctx, s.pool, batch, `DELETE FROM alerts WHERE (tenant_id, alert_key, started_at) IN (SELECT tenant_id, alert_key, started_at FROM alerts WHERE state='resolved' AND resolved_at < $1 LIMIT $2)`, before); err != nil {
		return err
	}
	if err := purgeBatches(ctx, s.pool, batch, `DELETE FROM notifications WHERE notification_id IN (SELECT n.notification_id FROM notifications n WHERE n.sent_at < $1 AND NOT EXISTS (SELECT 1 FROM alerts a WHERE a.started_at = n.started_at AND a.state <> 'resolved') LIMIT $2)`, before); err != nil {
		return err
	}
	return purgeBatches(ctx, s.pool, batch, `DELETE FROM silences WHERE silence_id IN (SELECT silence_id FROM silences WHERE ends_at < $1 LIMIT $2)`, before)
}

// purgeBatches runs query, a DELETE of at most batch rows that takes the cutoff
// and the batch size, until a run removes fewer rows than the batch.
func purgeBatches(ctx context.Context, db storeDB, batch int, query string, before time.Time) error {
	for {
		tag, err := db.Exec(ctx, query, before, batch)
		if err != nil {
			return err
		}
		if tag.RowsAffected() < int64(batch) {
			return nil
		}
	}
}

func (s *pgStore) Active(ctx context.Context, f Filter) ([]Alert, error) {
	query := `SELECT alert_key, rule_id, severity, state, cluster_id, instance_id, datname, labels, value, summary, started_at, last_eval_at, resolved_at FROM alerts WHERE tenant_id='default'`
	// An explicit resolved state must not be combined with the default
	// exclusion, which would empty the result.
	if !f.IncludeResolved && f.State != StateResolved {
		query += " AND state <> 'resolved'"
	}
	args := []any{}
	n := 1
	if f.RuleID != "" {
		query += fmt.Sprintf(" AND rule_id=$%d", n)
		args = append(args, f.RuleID)
		n++
	}
	if f.State != "" {
		query += fmt.Sprintf(" AND state=$%d", n)
		args = append(args, f.State)
		n++
	}
	if f.Severity != "" {
		query += fmt.Sprintf(" AND severity=$%d", n)
		args = append(args, f.Severity)
		n++
	}
	if f.ClusterID != nil {
		query += fmt.Sprintf(" AND cluster_id=$%d", n)
		args = append(args, *f.ClusterID)
		n++
	}
	if f.InstanceID != "" {
		query += fmt.Sprintf(" AND instance_id=$%d", n)
		args = append(args, f.InstanceID)
		n++
	}
	query += " ORDER BY last_eval_at DESC"
	if f.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", n)
		args = append(args, f.Limit)
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Alert
	for rows.Next() {
		var a Alert
		var raw []byte
		if err := rows.Scan(&a.Key, &a.RuleID, &a.Severity, &a.State, &a.ClusterID, &a.InstanceID, &a.Datname, &raw, &a.Value, &a.Summary, &a.StartedAt, &a.LastEvalAt, &a.ResolvedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &a.Labels); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
