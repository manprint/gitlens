//go:build integration

package alert

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/manprint/pglens/internal/alert/notify"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

type integrationSource struct {
	mu    sync.Mutex
	value float64
	ts    time.Time
	iid   uuid.UUID
}

func (s *integrationSource) Kind() string { return "metric" }
func (s *integrationSource) Samples(context.Context, Rule, time.Time) ([]Sample, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ts.IsZero() {
		return nil, nil
	}
	iid := s.iid
	return []Sample{{InstanceID: &iid, Value: s.value, TS: s.ts}}, nil
}
func (s *integrationSource) set(v float64, ts time.Time) {
	s.mu.Lock()
	s.value = v
	s.ts = ts
	s.mu.Unlock()
}

func setupEngineIntegration(t *testing.T, pg *pgtest.PG) (context.Context, *pgxpool.Pool, *integrationSource, *clock.Fake, *Engine, *Engine, <-chan notify.Message) {
	pg.Lock(t)
	pool := pg.Pool(t, "app", pgtest.RoleSuperuser)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS alert_rules (rule_id text NOT NULL,tenant_id text NOT NULL DEFAULT 'default',enabled boolean NOT NULL,severity text NOT NULL,scope text NOT NULL,metric text,comparator text,threshold double precision,for_seconds integer NOT NULL,event_type text,summary text NOT NULL,PRIMARY KEY(tenant_id,rule_id)); CREATE TABLE IF NOT EXISTS alerts (alert_key text NOT NULL,tenant_id text NOT NULL DEFAULT 'default',rule_id text NOT NULL,severity text NOT NULL,state text NOT NULL,cluster_id bigint,instance_id uuid,datname text,labels jsonb NOT NULL,value double precision,summary text NOT NULL,started_at timestamptz NOT NULL,last_eval_at timestamptz NOT NULL,resolved_at timestamptz,PRIMARY KEY(tenant_id,alert_key,started_at)); CREATE TABLE IF NOT EXISTS notifications (notification_id bigserial PRIMARY KEY,tenant_id text NOT NULL DEFAULT 'default',alert_key text NOT NULL,started_at timestamptz NOT NULL,channel text NOT NULL,phase text NOT NULL,sent_at timestamptz NOT NULL DEFAULT now(),ok boolean NOT NULL,attempts integer NOT NULL,error text); CREATE UNIQUE INDEX IF NOT EXISTS engine_notifications_once ON notifications(tenant_id,alert_key,started_at,channel,phase); CREATE TABLE IF NOT EXISTS silences (silence_id uuid PRIMARY KEY,tenant_id text NOT NULL DEFAULT 'default',matchers jsonb NOT NULL,reason text NOT NULL,starts_at timestamptz NOT NULL,ends_at timestamptz NOT NULL); TRUNCATE alert_rules,alerts,notifications,silences RESTART IDENTITY`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO alert_rules(rule_id,enabled,severity,scope,metric,comparator,threshold,for_seconds,summary) VALUES('integration.rule',true,'warning','instance','integration.metric','gt',10,30,'integration alert')`)
	require.NoError(t, err)
	src := &integrationSource{iid: uuid.New()}
	now := time.Now().UTC().Truncate(time.Microsecond)
	src.set(5, now)
	received := make(chan notify.Message, 10)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m notify.Message
		_ = json.NewDecoder(r.Body).Decode(&m)
		received <- m
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(receiver.Close)
	clk := clock.NewFake(now)
	st := NewPgStore(pool)
	n := NewChannelNotifier(st, notify.NewWebhook(receiver.URL, nil))
	e := NewEngineWithInterval(pool, clk, 30*time.Second, st, n, []Source{src})
	e2 := NewEngineWithInterval(pool, clk, 30*time.Second, st, n, []Source{src})
	return ctx, pool, src, clk, e, e2, received
}

func TestINTALERT008_Through012_EngineIntegration(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		ctx, pool, src, clk, e, e2, received := setupEngineIntegration(t, pg)
		t.Cleanup(e.Stop)
		t.Cleanup(e2.Stop)
		require.NoError(t, e.Tick(ctx))
		src.set(20, clk.Now())
		require.NoError(t, e.Tick(ctx))
		clk.Advance(31 * time.Second)
		require.NoError(t, e.Tick(ctx))
		var state State
		require.NoError(t, pool.QueryRow(ctx, `SELECT state FROM alerts WHERE rule_id='integration.rule' ORDER BY started_at DESC LIMIT 1`).Scan(&state))
		require.Equal(t, StateFiring, state)
		select {
		case <-received:
		default:
			t.Fatal("expected one webhook notification")
		}
		require.NoError(t, e2.Tick(ctx))
		var n int
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM notifications`).Scan(&n))
		require.Equal(t, 1, n)
		sid := uuid.New()
		_, err := pool.Exec(ctx, `INSERT INTO silences(silence_id,matchers,reason,starts_at,ends_at) VALUES($1,$2,'maintenance',$3,$4)`, sid, []byte(`[{"name":"rule_id","value":"integration.rule"}]`), clk.Now().Add(-time.Minute), clk.Now().Add(time.Minute))
		require.NoError(t, err)
		src.set(5, clk.Now())
		require.NoError(t, e.Tick(ctx))
		require.NoError(t, pool.QueryRow(ctx, `SELECT state FROM alerts WHERE rule_id='integration.rule' ORDER BY started_at DESC LIMIT 1`).Scan(&state))
		require.Equal(t, StateResolved, state)
		e.Stop()
		require.NoError(t, e2.Tick(ctx))
	})
}

// countRows returns how many rows of a query on the integration rule hold.
func countRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(ctx, query).Scan(&n))
	return n
}

// TestINTALERT015_NewTermStartsFromTheTable — the leader's session dies while an
// alert fires, another engine takes over and closes the episode, and the first
// engine leads again with the condition true. Its new term must start from the
// table: the closed episode stays closed, and the condition opens a new one that
// is recorded and notified. The firing state the first engine held from its
// earlier term must not be carried into the new term.
func TestINTALERT015_NewTermStartsFromTheTable(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		ctx, pool, src, clk, e, e2, _ := setupEngineIntegration(t, pg)
		t.Cleanup(e.Stop)
		t.Cleanup(e2.Stop)
		const rows = `SELECT count(*) FROM alerts WHERE rule_id='integration.rule'`
		const fires = `SELECT count(*) FROM notifications WHERE alert_key LIKE 'integration.rule/%' AND phase='fire'`
		require.NoError(t, e.Tick(ctx))
		src.set(20, clk.Now())
		require.NoError(t, e.Tick(ctx))
		clk.Advance(31 * time.Second)
		require.NoError(t, e.Tick(ctx))
		require.Equal(t, 1, countRows(t, ctx, pool, rows))
		require.Equal(t, 1, countRows(t, ctx, pool, fires))

		// The leader's session ends, which releases the advisory lock with it.
		e.mu.Lock()
		session := e.conn.Conn()
		e.mu.Unlock()
		require.NoError(t, session.Close(ctx))

		// The other engine leads, sees the condition clear and closes the episode.
		src.set(5, clk.Now())
		deadline := time.Now().Add(10 * time.Second)
		for {
			require.NoError(t, e2.Tick(ctx))
			e2.mu.Lock()
			led := e2.hasLock
			e2.mu.Unlock()
			if led {
				break
			}
			require.True(t, time.Now().Before(deadline), "the second engine never acquired the lock")
			time.Sleep(10 * time.Millisecond)
		}
		e2.Stop()

		// The condition holds again and the first engine leads again. Until the
		// new episode has held for its For, the table shows the closed one.
		clk.Advance(time.Minute)
		src.set(20, clk.Now())
		require.NoError(t, e.Tick(ctx))
		var state State
		require.NoError(t, pool.QueryRow(ctx, `SELECT state FROM alerts WHERE rule_id='integration.rule' ORDER BY started_at DESC LIMIT 1`).Scan(&state))
		require.Equal(t, StateResolved, state)

		clk.Advance(31 * time.Second)
		src.set(20, clk.Now())
		require.NoError(t, e.Tick(ctx))
		require.NoError(t, pool.QueryRow(ctx, `SELECT state FROM alerts WHERE rule_id='integration.rule' ORDER BY started_at DESC LIMIT 1`).Scan(&state))
		require.Equal(t, StateFiring, state)
		require.Equal(t, 2, countRows(t, ctx, pool, rows), "the new episode must be recorded as its own row")
		require.Equal(t, 2, countRows(t, ctx, pool, fires), "the new episode must notify")
	})
}

// ruleChannel reports the messages of one rule to a phaseRecorder. The integration
// source samples every metric rule, so the builtin rules fire too, and they are
// not what this test is about.
type ruleChannel struct {
	rule string
	rec  *phaseRecorder
}

func (c ruleChannel) Name() string { return "slack" }
func (c ruleChannel) Send(ctx context.Context, m notify.Message) error {
	if m.RuleID != c.rule {
		return nil
	}
	return recordingChannel{rec: c.rec}.Send(ctx, m)
}

// TestINTALERT019_ALeaderChangeDoesNotSendAnOverruledFiring — the first engine
// fails to send a FIRING and loses the lock. The second engine, with its own
// notifier, resolves the alert once the channel is back. When the first engine
// leads again its queue still holds the FIRING, but the table shows the alert
// resolved, so the FIRING is dropped and the channel's last message is RESOLVED.
func TestINTALERT019_ALeaderChangeDoesNotSendAnOverruledFiring(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		ctx, pool, src, clk, _, _, _ := setupEngineIntegration(t, pg)
		st := NewPgStore(pool)
		rec := &phaseRecorder{down: true}
		first := NewEngineWithInterval(pool, clk, 30*time.Second, st, NewChannelNotifier(st, ruleChannel{rule: "integration.rule", rec: rec}), []Source{src})
		second := NewEngineWithInterval(pool, clk, 30*time.Second, st, NewChannelNotifier(st, ruleChannel{rule: "integration.rule", rec: rec}), []Source{src})
		t.Cleanup(first.Stop)
		t.Cleanup(second.Stop)
		require.NoError(t, first.Tick(ctx))
		src.set(20, clk.Now())
		require.NoError(t, first.Tick(ctx))
		clk.Advance(31 * time.Second)
		require.Error(t, first.Tick(ctx), "the channel is down, so the FIRING is refused")
		require.Empty(t, rec.got)

		// The first engine's session ends, which releases the advisory lock with it.
		first.mu.Lock()
		session := first.conn.Conn()
		first.mu.Unlock()
		require.NoError(t, session.Close(ctx))

		// The channel is back. The second engine leads, sees the condition clear
		// and resolves the episode.
		rec.down = false
		src.set(5, clk.Now())
		deadline := time.Now().Add(10 * time.Second)
		for {
			require.NoError(t, second.Tick(ctx))
			second.mu.Lock()
			led := second.hasLock
			second.mu.Unlock()
			if led {
				break
			}
			require.True(t, time.Now().Before(deadline), "the second engine never acquired the lock")
			time.Sleep(10 * time.Millisecond)
		}
		second.Stop()
		require.Equal(t, []string{"resolve"}, rec.got)

		// The first engine leads again. Its queue still holds the FIRING.
		clk.Advance(time.Minute)
		src.set(5, clk.Now())
		require.NoError(t, first.Tick(ctx))
		// A pass that did not lead would send nothing, and the check below would
		// pass for no reason.
		first.mu.Lock()
		led := first.hasLock
		first.mu.Unlock()
		require.True(t, led, "the first engine leads again")
		require.Equal(t, []string{"resolve"}, rec.got, "the FIRING the first engine failed to send is obsolete")
	})
}

// TestINTALERT018_SpellClosesAfterARestartDuringTheOutage — the engine that
// opened an instance_unreachable spell is replaced while the instance is still
// down. The instance then reports again, and no instance_reachable event is
// ever written: the server that would have written it restarted too. The spell
// still closes, on the instance's own state.
func TestINTALERT018_SpellClosesAfterARestartDuringTheOutage(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		ctx, pool, _, _, _, _, _ := setupEngineIntegration(t, pg)
		_, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS events (event_id bigserial, ts timestamptz NOT NULL, type text NOT NULL, cluster_id bigint, instance_id uuid); TRUNCATE events`)
		require.NoError(t, err)
		createAlertTables(t, pool)
		agent, instance := uuid.New(), uuid.New()
		_, err = pool.Exec(ctx, `INSERT INTO agents (agent_id) VALUES ($1)`, agent)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `INSERT INTO instances (instance_id, cluster_id, agent_id, role, last_seen) VALUES ($1, 105, $2, 'standby', now() - interval '10 minutes')`, instance, agent)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `INSERT INTO events (ts, type, cluster_id, instance_id) VALUES (now(), 'instance_unreachable', 105, $1)`, instance)
		require.NoError(t, err)

		clk := clock.NewFake(time.Now())
		sources := []Source{NewEventSource(pool, 30*time.Second)}
		first := &engineNotifier{}
		e1 := NewEngineWithInterval(pool, clk, 30*time.Second, NewPgStore(pool), first, sources)
		t.Cleanup(e1.Stop)
		require.NoError(t, e1.Tick(ctx))
		require.Len(t, first.alerts, 1)
		require.Equal(t, StateFiring, first.alerts[0].State)
		e1.Stop()

		// The event leaves the lookback while the outage goes on. Then the
		// instance reports again.
		_, err = pool.Exec(ctx, `UPDATE events SET ts = now() - interval '5 minutes'`)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `UPDATE instances SET last_seen = now() WHERE instance_id = $1`, instance)
		require.NoError(t, err)

		second := &engineNotifier{}
		e2 := NewEngineWithInterval(pool, clk, 30*time.Second, NewPgStore(pool), second, sources)
		t.Cleanup(e2.Stop)
		for i := 0; i < 8; i++ { // four minutes: the restored episode gets its grace first
			clk.Advance(30 * time.Second)
			require.NoError(t, e2.Tick(ctx))
		}
		require.Len(t, second.alerts, 1)
		require.Equal(t, StateResolved, second.alerts[0].State)
		require.Zero(t, countRows(t, ctx, pool, `SELECT count(*) FROM events WHERE type = 'instance_reachable'`), "no reachable event was written, so the spell cleared on state")
	})
}
