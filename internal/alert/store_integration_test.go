//go:build integration

package alert

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

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

// TestINTALERT013_ActiveStateFilters — the default listing keeps resolved
// alerts out, while an explicit resolved state and the all-states flag return
// them. The default used to be combined with every explicit state, so
// state=resolved always came back empty and state=all never showed history.
func TestINTALERT013_ActiveStateFilters(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		pool := pg.Pool(t, "app", pgtest.RoleSuperuser)
		ctx := context.Background()
		_, err := pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS alerts (alert_key text NOT NULL, tenant_id text NOT NULL DEFAULT 'default', rule_id text NOT NULL, severity text NOT NULL, state text NOT NULL, cluster_id bigint, instance_id uuid, datname text, labels jsonb NOT NULL, value double precision, summary text NOT NULL, started_at timestamptz NOT NULL, last_eval_at timestamptz NOT NULL, resolved_at timestamptz, PRIMARY KEY (tenant_id, alert_key, started_at));
TRUNCATE alerts RESTART IDENTITY`)
		require.NoError(t, err)

		st := NewPgStore(pool)
		now := time.Now().UTC().Truncate(time.Microsecond)
		resolvedAt := now
		for i, state := range []State{StatePending, StateFiring, StateResolved} {
			a := Alert{Key: "k/" + string(state), RuleID: "r", Severity: SeverityWarning, State: state, Summary: "s", StartedAt: now.Add(time.Duration(i) * time.Minute), LastEvalAt: now.Add(time.Duration(i) * time.Minute), Labels: map[string]string{}}
			if state == StateResolved {
				a.ResolvedAt = &resolvedAt
			}
			require.NoError(t, st.Upsert(ctx, a))
		}
		keys := func(rows []Alert) []string {
			out := make([]string, 0, len(rows))
			for _, r := range rows {
				out = append(out, r.Key)
			}
			return out
		}

		got, err := st.Active(ctx, Filter{})
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"k/pending", "k/firing"}, keys(got))

		got, err = st.Active(ctx, Filter{State: StateResolved})
		require.NoError(t, err)
		require.Equal(t, []string{"k/resolved"}, keys(got))

		got, err = st.Active(ctx, Filter{State: StateFiring})
		require.NoError(t, err)
		require.Equal(t, []string{"k/firing"}, keys(got))

		got, err = st.Active(ctx, Filter{IncludeResolved: true})
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"k/pending", "k/firing", "k/resolved"}, keys(got))

		// The cap keeps the newest rows, so a long history is bounded in SQL.
		got, err = st.Active(ctx, Filter{IncludeResolved: true, Limit: 2})
		require.NoError(t, err)
		require.Equal(t, []string{"k/resolved", "k/firing"}, keys(got))
	})
}

// historyPurgeSchema is the alert schema of the migration, as the purge
// tests need it, emptied before each test.
const historyPurgeSchema = `
CREATE TABLE IF NOT EXISTS alerts (alert_key text NOT NULL, tenant_id text NOT NULL DEFAULT 'default', rule_id text NOT NULL, severity text NOT NULL, state text NOT NULL, cluster_id bigint, instance_id uuid, datname text, labels jsonb NOT NULL, value double precision, summary text NOT NULL, started_at timestamptz NOT NULL, last_eval_at timestamptz NOT NULL, resolved_at timestamptz, PRIMARY KEY (tenant_id, alert_key, started_at));
CREATE TABLE IF NOT EXISTS notifications (notification_id bigserial PRIMARY KEY, tenant_id text NOT NULL DEFAULT 'default', alert_key text NOT NULL, started_at timestamptz NOT NULL, channel text NOT NULL, phase text NOT NULL, sent_at timestamptz NOT NULL DEFAULT now(), ok boolean NOT NULL, attempts integer NOT NULL, error text);
CREATE UNIQUE INDEX IF NOT EXISTS alert_store_notifications_once ON notifications (tenant_id, alert_key, started_at, channel, phase);
CREATE TABLE IF NOT EXISTS silences (silence_id uuid PRIMARY KEY, tenant_id text NOT NULL DEFAULT 'default', matchers jsonb NOT NULL, reason text NOT NULL, starts_at timestamptz NOT NULL, ends_at timestamptz NOT NULL);
TRUNCATE alerts, notifications, silences RESTART IDENTITY`

// TestINTALERT016_HistoryPurgeKeepsOpenEpisodes: the purge removes resolved
// alerts, deliveries and silences older than the cutoff, and keeps a firing
// episode with its deliveries however old it is, since its dedup depends on them.
func TestINTALERT016_HistoryPurgeKeepsOpenEpisodes(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		pool := pg.Pool(t, "app", pgtest.RoleSuperuser)
		ctx := context.Background()
		_, err := pool.Exec(ctx, historyPurgeSchema)
		require.NoError(t, err)

		now := time.Now().UTC().Truncate(time.Microsecond)
		old := now.Add(-100 * 24 * time.Hour)
		recent := now.Add(-24 * time.Hour)
		ancient := now.Add(-200 * 24 * time.Hour)
		cutoff := now.Add(-historyRetention)
		// Each episode has a start, a state and, when resolved, the time it resolved.
		episodes := []struct {
			key       string
			started   time.Time
			resolved  *time.Time
			sentAt    time.Time
			wantAlert bool
		}{
			{"old", old.Add(-time.Hour), &old, old, false},
			{"recent", recent.Add(-time.Hour), &recent, recent, true},
			{"open", ancient, nil, ancient, true},
		}
		st := NewPgStore(pool)
		for _, ep := range episodes {
			state := StateResolved
			if ep.resolved == nil {
				state = StateFiring
			}
			a := Alert{Key: ep.key, RuleID: "r", Severity: SeverityWarning, State: state, Summary: "s", StartedAt: ep.started, LastEvalAt: ep.started, ResolvedAt: ep.resolved, Labels: map[string]string{}}
			require.NoError(t, st.Upsert(ctx, a))
			_, err := pool.Exec(ctx, `INSERT INTO notifications (alert_key, started_at, channel, phase, sent_at, ok, attempts) VALUES ($1,$2,'slack','fire',$3,true,1)`, ep.key+"/"+ep.started.Format(time.RFC3339Nano), ep.started, ep.sentAt)
			require.NoError(t, err)
		}
		_, err = pool.Exec(ctx, `INSERT INTO silences (silence_id, matchers, reason, starts_at, ends_at) VALUES
			(gen_random_uuid(), '[]', 'expired long ago', $1, $2),
			(gen_random_uuid(), '[]', 'expired recently', $3, $4),
			(gen_random_uuid(), '[]', 'still active', $3, $5)`, old.Add(-time.Hour), old, recent.Add(-time.Hour), recent, now.Add(time.Hour))
		require.NoError(t, err)

		require.NoError(t, st.(purger).Purge(ctx, cutoff))

		var keys []string
		rows, err := pool.Query(ctx, `SELECT alert_key FROM alerts ORDER BY alert_key`)
		require.NoError(t, err)
		for rows.Next() {
			var k string
			require.NoError(t, rows.Scan(&k))
			keys = append(keys, k)
		}
		require.NoError(t, rows.Err())
		rows.Close()
		require.Equal(t, []string{"open", "recent"}, keys, "the resolved alert past the cutoff goes; the firing one never does")

		var deliveries []string
		rows, err = pool.Query(ctx, `SELECT alert_key FROM notifications ORDER BY alert_key`)
		require.NoError(t, err)
		for rows.Next() {
			var k string
			require.NoError(t, rows.Scan(&k))
			deliveries = append(deliveries, k)
		}
		require.NoError(t, rows.Err())
		rows.Close()
		require.Len(t, deliveries, 2, "the delivery of the open episode and of the recent one stay")
		require.True(t, strings.HasPrefix(deliveries[0], "open/") && strings.HasPrefix(deliveries[1], "recent/"), deliveries)

		var silences int
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM silences`).Scan(&silences))
		require.Equal(t, 2, silences, "the silence that expired long ago goes; the recent and the active one stay")
	})
}

// TestINTALERT017_PurgeRunsInBatches: a backlog larger than one batch is removed
// in full, and the batches leave the open episode and its delivery alone.
func TestINTALERT017_PurgeRunsInBatches(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		pool := pg.Pool(t, "app", pgtest.RoleSuperuser)
		ctx := context.Background()
		_, err := pool.Exec(ctx, historyPurgeSchema)
		require.NoError(t, err)

		now := time.Now().UTC().Truncate(time.Microsecond)
		old := now.Add(-100 * 24 * time.Hour)
		st := &pgStore{pool: pool, purgeBatch: 2}
		const backlog = 5
		for i := range backlog {
			// Not old itself: the open episode below starts at old, and the guard matches on the start.
			at := old.Add(-time.Hour + time.Duration(i)*time.Minute)
			key := fmt.Sprintf("done-%d", i)
			a := Alert{Key: key, RuleID: "r", Severity: SeverityWarning, State: StateResolved, Summary: "s", StartedAt: at, LastEvalAt: at, ResolvedAt: &at, Labels: map[string]string{}}
			require.NoError(t, st.Upsert(ctx, a))
			_, err := pool.Exec(ctx, `INSERT INTO notifications (alert_key, started_at, channel, phase, sent_at, ok, attempts) VALUES ($1,$2,'slack','fire',$3,true,1)`, key+"/"+at.Format(time.RFC3339Nano), at, at)
			require.NoError(t, err)
		}
		open := Alert{Key: "open", RuleID: "r", Severity: SeverityWarning, State: StateFiring, Summary: "s", StartedAt: old, LastEvalAt: old, Labels: map[string]string{}}
		require.NoError(t, st.Upsert(ctx, open))
		_, err = pool.Exec(ctx, `INSERT INTO notifications (alert_key, started_at, channel, phase, sent_at, ok, attempts) VALUES ('open/x',$1,'slack','fire',$1,true,1)`, old)
		require.NoError(t, err)

		require.NoError(t, st.Purge(ctx, now.Add(-historyRetention)))

		var alerts, deliveries int
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM alerts`).Scan(&alerts))
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM notifications`).Scan(&deliveries))
		require.Equal(t, 1, alerts, "every resolved alert of the backlog goes, in batches of two")
		require.Equal(t, 1, deliveries, "every delivery of the backlog goes, and the open episode's stays")
	})
}

// TestINTALERT020_StoreWritesEmptyLabelsAndReadsOneTenant: a nil label map is
// stored as the empty object the column defaults to, never as JSON null. The
// silence and active reads take the default tenant only, so a row written
// under another tenant stays out of both.
func TestINTALERT020_StoreWritesEmptyLabelsAndReadsOneTenant(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		pool := pg.Pool(t, "app", pgtest.RoleSuperuser)
		ctx := context.Background()
		_, err := pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS alerts (alert_key text NOT NULL, tenant_id text NOT NULL DEFAULT 'default', rule_id text NOT NULL, severity text NOT NULL, state text NOT NULL, cluster_id bigint, instance_id uuid, datname text, labels jsonb NOT NULL, value double precision, summary text NOT NULL, started_at timestamptz NOT NULL, last_eval_at timestamptz NOT NULL, resolved_at timestamptz, PRIMARY KEY (tenant_id, alert_key, started_at));
CREATE TABLE IF NOT EXISTS silences (silence_id uuid PRIMARY KEY, tenant_id text NOT NULL DEFAULT 'default', matchers jsonb NOT NULL, reason text NOT NULL, starts_at timestamptz NOT NULL, ends_at timestamptz NOT NULL);
TRUNCATE alerts, silences RESTART IDENTITY`)
		require.NoError(t, err)

		st := NewPgStore(pool)
		now := time.Now().UTC().Truncate(time.Microsecond)
		require.NoError(t, st.Upsert(ctx, Alert{Key: "nil-labels", RuleID: "r", Severity: SeverityWarning, State: StateFiring, Summary: "s", StartedAt: now, LastEvalAt: now}))
		var stored string
		require.NoError(t, pool.QueryRow(ctx, `SELECT labels::text FROM alerts WHERE alert_key='nil-labels'`).Scan(&stored))
		require.Equal(t, "{}", stored)

		_, err = pool.Exec(ctx, `INSERT INTO alerts (alert_key, tenant_id, rule_id, severity, state, datname, value, labels, summary, started_at, last_eval_at) VALUES ('other-tenant','other','r','warning','firing','db',0,'{}','s',$1,$1)`, now)
		require.NoError(t, err)
		active, err := st.Active(ctx, Filter{})
		require.NoError(t, err)
		require.Len(t, active, 1, "the default tenant's alert only")
		require.Equal(t, "nil-labels", active[0].Key)

		defaultSilence, otherSilence := uuid.New(), uuid.New()
		_, err = pool.Exec(ctx, `INSERT INTO silences (silence_id, tenant_id, matchers, reason, starts_at, ends_at) VALUES ($1,'default','[]','d',$3,$4), ($2,'other','[]','o',$3,$4)`,
			defaultSilence, otherSilence, now.Add(-time.Hour), now.Add(time.Hour))
		require.NoError(t, err)
		silences, err := st.Silences(ctx, now)
		require.NoError(t, err)
		require.Len(t, silences, 1, "the default tenant's silence only")
		require.Equal(t, defaultSilence, silences[0].ID)
	})
}
