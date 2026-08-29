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
