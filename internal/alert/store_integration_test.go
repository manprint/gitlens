//go:build integration

package alert

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

func setupAlertStoreTables(t *testing.T, ctx context.Context, pool interface {
	Exec(context.Context, string, ...any) (any, error)
}) {
	// Kept as a separate test-local schema so the persistence contract is tested
	// against PostgreSQL without depending on unrelated Timescale fixtures.
	t.Helper()
}

func TestINTALERT003_Through007_AlertStorePersistence(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		pool := pg.Pool(t, "app", pgtest.RoleSuperuser)
		ctx := context.Background()
		_, err := pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS alert_rules (rule_id text NOT NULL, tenant_id text NOT NULL DEFAULT 'default', enabled boolean NOT NULL, severity text NOT NULL, scope text NOT NULL, metric text, comparator text, threshold double precision, for_seconds integer NOT NULL, event_type text, summary text NOT NULL, PRIMARY KEY (tenant_id, rule_id));
CREATE TABLE IF NOT EXISTS alerts (alert_key text NOT NULL, tenant_id text NOT NULL DEFAULT 'default', rule_id text NOT NULL, severity text NOT NULL, state text NOT NULL, cluster_id bigint, instance_id uuid, datname text, labels jsonb NOT NULL, value double precision, summary text NOT NULL, started_at timestamptz NOT NULL, last_eval_at timestamptz NOT NULL, resolved_at timestamptz, PRIMARY KEY (tenant_id, alert_key, started_at));
CREATE TABLE IF NOT EXISTS notifications (notification_id bigserial PRIMARY KEY, tenant_id text NOT NULL DEFAULT 'default', alert_key text NOT NULL, started_at timestamptz NOT NULL, channel text NOT NULL, phase text NOT NULL, sent_at timestamptz NOT NULL DEFAULT now(), ok boolean NOT NULL, attempts integer NOT NULL, error text);
CREATE UNIQUE INDEX IF NOT EXISTS alert_store_notifications_once ON notifications (tenant_id, alert_key, started_at, channel, phase);
CREATE TABLE IF NOT EXISTS silences (silence_id uuid PRIMARY KEY, tenant_id text NOT NULL DEFAULT 'default', matchers jsonb NOT NULL, reason text NOT NULL, starts_at timestamptz NOT NULL, ends_at timestamptz NOT NULL);
TRUNCATE alert_rules, alerts, notifications, silences RESTART IDENTITY`)
		require.NoError(t, err)

		st := NewPgStore(pool)
		// INT-ALERT-006: the ten enabled Tier 1 rows are returned; disabled rows are not.
		for i := 0; i < 10; i++ {
			_, err = pool.Exec(ctx, `INSERT INTO alert_rules (rule_id,enabled,severity,scope,metric,comparator,threshold,for_seconds,summary) VALUES ($1,true,'warning','instance','m','gt',1,1,'s')`, "tier1."+string(rune('a'+i)))
			require.NoError(t, err)
		}
		_, err = pool.Exec(ctx, `INSERT INTO alert_rules (rule_id,enabled,severity,scope,metric,comparator,threshold,for_seconds,summary) VALUES ('disabled',false,'warning','instance','m','gt',1,1,'s')`)
		require.NoError(t, err)
		rules, err := st.Rules(ctx)
		require.NoError(t, err)
		require.Len(t, rules, 10)

		now := time.Now().UTC().Truncate(time.Microsecond)
		a := Alert{Key: "refire", RuleID: "tier1.a", Severity: SeverityWarning, State: StateFiring, Summary: "s", StartedAt: now, LastEvalAt: now, Labels: map[string]string{"x": "y"}}
		// INT-ALERT-003 and 004: idempotent update and a new started_at create two rows.
		require.NoError(t, st.Upsert(ctx, a))
		a.Value = 2
		require.NoError(t, st.Upsert(ctx, a))
		a.StartedAt = now.Add(time.Minute)
		require.NoError(t, st.Upsert(ctx, a))
		var count int
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM alerts WHERE alert_key='refire'`).Scan(&count))
		require.Equal(t, 2, count)

		// INT-ALERT-005: the unique key arbitrates concurrent delivery claims.
		var wg sync.WaitGroup
		wins := make(chan bool, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				won, e := st.ClaimNotification(ctx, "refire", "webhook", "fire", a)
				require.NoError(t, e)
				wins <- won
			}()
		}
		wg.Wait()
		close(wins)
		wonCount := 0
		for won := range wins {
			if won {
				wonCount++
			}
		}
		require.Equal(t, 1, wonCount)
		require.NoError(t, st.MarkNotification(ctx, "refire", "webhook", "fire", true, 1, nil))

		// INT-ALERT-007: only the active silence is returned at the requested time.
		activeID, expiredID := uuid.New(), uuid.New()
		_, err = pool.Exec(ctx, `INSERT INTO silences (silence_id,matchers,reason,starts_at,ends_at) VALUES ($1,'[]','active',$3,$4),($2,'[]','expired',$3 - interval '2 hours',$3 - interval '1 hour')`, activeID, expiredID, now, now.Add(time.Hour))
		require.NoError(t, err)
		silences, err := st.Silences(ctx, now)
		require.NoError(t, err)
		require.Len(t, silences, 1)
		require.Equal(t, activeID, silences[0].ID)
	})
}
