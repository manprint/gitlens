package advisor

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type findingDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// Store is the durable boundary used by the advisor engine.
type Store interface {
	ActiveInstances(context.Context, time.Time) ([]uuid.UUID, error)
	UpsertFinding(context.Context, Finding, time.Time) error
	ResolveAbsent(context.Context, uuid.UUID, []string, []string, time.Time) error
	PurgeResolved(context.Context, time.Time) error
}

type PgStore struct{ db findingDB }

func NewPgStore(pool *pgxpool.Pool) Store { return &PgStore{db: pool} }

func (s *PgStore) ActiveInstances(ctx context.Context, since time.Time) ([]uuid.UUID, error) {
	rows, err := s.db.Query(ctx, `SELECT instance_id FROM instances WHERE tenant_id='default' AND last_seen >= $1 ORDER BY instance_id`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *PgStore) UpsertFinding(ctx context.Context, f Finding, now time.Time) error {
	evidence, err := json.Marshal(f.Evidence)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `INSERT INTO findings (finding_id,rule_id,severity,state,scope,cluster_id,instance_id,datname,object_name,title,detail,remediation,evidence,degraded_reason,first_seen,last_seen,resolved_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$15,NULL) ON CONFLICT (tenant_id,finding_id) DO UPDATE SET rule_id=EXCLUDED.rule_id,severity=EXCLUDED.severity,scope=EXCLUDED.scope,cluster_id=EXCLUDED.cluster_id,instance_id=EXCLUDED.instance_id,datname=EXCLUDED.datname,object_name=EXCLUDED.object_name,title=EXCLUDED.title,detail=EXCLUDED.detail,remediation=EXCLUDED.remediation,evidence=EXCLUDED.evidence,degraded_reason=EXCLUDED.degraded_reason,last_seen=EXCLUDED.last_seen,resolved_at=NULL,state=CASE WHEN findings.muted_until IS NOT NULL AND findings.muted_until > EXCLUDED.last_seen THEN 'muted' ELSE EXCLUDED.state END`, f.ID(), f.RuleID, f.Severity, f.State, f.Scope, f.ClusterID, f.InstanceID, f.Datname, f.ObjectName, f.Title, f.Detail, f.Remediation, evidence, nullIfEmpty(f.DegradedReason), now)
	return err
}

func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func (s *PgStore) ResolveAbsent(ctx context.Context, instanceID uuid.UUID, ranRules, seen []string, now time.Time) error {
	if len(ranRules) == 0 {
		return nil
	}
	_, err := s.db.Exec(ctx, `UPDATE findings SET state='resolved',resolved_at=$1 WHERE tenant_id='default' AND instance_id=$2 AND state IN ('open','degraded') AND rule_id = ANY($3) AND NOT (finding_id = ANY($4))`, now, instanceID, ranRules, seen)
	return err
}

func (s *PgStore) PurgeResolved(ctx context.Context, before time.Time) error {
	_, err := s.db.Exec(ctx, `DELETE FROM findings WHERE tenant_id='default' AND state='resolved' AND resolved_at < $1`, before)
	return err
}

func (s *PgStore) String() string { return fmt.Sprintf("%T", s) }
