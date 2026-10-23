package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/internal/ash"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/wire"
)

const ashWindow = 10 * time.Second
const ashMaxKeys = 100

// startASH wires internal/ash's real sampler+aggregator pipeline for one
// target: a dedicated connection (phase 7.1 — never the shared one), a
// sampler ticking every 1s, an aggregator folding those into 10s windows,
// and a flush loop that turns each closed window into a wire.Result under
// the "ash" check name — the same shape internal/server/pipeline.go's
// buildASHRow already expects, unchanged since it was built ahead of this
// wiring. Returns a stop func (always non-nil, even when ASH is disabled —
// then a no-op) and whether ASH is enabled for this target, which the
// caller reports on wire.Instance.ASHEnabled every push (SYS-ASH-002).
func startASH(ctx context.Context, mgr *agent.Manager, targetName string, cfg agent.CheckConfig, clk clock.Clock, mu *sync.Mutex, pending map[string][]wire.Result) (stop func(), enabled bool) {
	if cfg.Enabled != nil && !*cfg.Enabled {
		return func() {}, false
	}

	conn := &ashConn{mgr: mgr, base: ctx, clk: clk}

	agg := ash.New(clk, ashWindow, ashMaxKeys)
	sampler := ash.NewSampler(conn, clk, func(r ash.SampleRow) {
		agg.Add(r.Datname, r.State, r.WaitEventType, r.WaitEvent, r.QueryID)
	})
	if cfg.Interval != "" {
		if d, err := time.ParseDuration(cfg.Interval); err == nil {
			sampler.SetInterval(d)
		} else {
			fmt.Fprintf(os.Stderr, "warning: ash: invalid checks.ash.interval %q for %s, using the 1s default: %v\n", cfg.Interval, targetName, err)
		}
	}
	sampler.SetTickCallback(func(success bool) {
		if success {
			agg.TickSucceeded()
		} else {
			agg.TickFailed()
		}
	})

	if err := sampler.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "warning: ash: start sampler for %s: %v (ASH disabled for this target)\n", targetName, err)
		conn.Close()
		return func() {}, false
	}

	flushCtx, cancel := context.WithCancel(ctx)
	flushTicker := clk.NewTicker(ashWindow)
	go func() {
		defer flushTicker.Stop()
		// failing is only for logging: a line when sampling stops succeeding
		// and another when it resumes, not one per failed tick.
		failing := false
		for {
			select {
			case <-flushCtx.Done():
				return
			case <-flushTicker.C():
				now := clk.Now()
				w := agg.Flush(now)
				if w.Ticks == 0 && w.FailedTicks > 0 {
					if !failing {
						fmt.Fprintf(os.Stderr, "warning: ash: sampling failed for %s: %v\n", targetName, sampler.LastError())
					}
					failing = true
				} else if w.Ticks > 0 && failing {
					fmt.Fprintf(os.Stderr, "ash: sampling recovered for %s\n", targetName)
					failing = false
				}
				res, ok := ashWindowResult(w, now, sampler.LastError())
				if !ok {
					continue
				}
				mu.Lock()
				pending[targetName] = append(pending[targetName], res)
				mu.Unlock()
			}
		}
	}()

	// sync.Once, not a bare closure: this stop func has two callers (the
	// deferred shutdown sweep, and any future caller that stops one target's
	// collection independently, as the revocation path very nearly did), and
	// stopping the sampler twice must stay a no-op.
	var stopOnce sync.Once
	stop = func() {
		stopOnce.Do(func() {
			cancel()
			sampler.Stop()
			conn.Close()
		})
	}
	return stop, true
}

// ashReconnectBackoff spaces reconnection attempts to a target that is down,
// so a 1-second sampler does not dial it every tick.
const ashReconnectBackoff = 5 * time.Second

// ashStatementTimeout stays below the sampler's 500ms tick deadline: the
// server cancels a slow sample itself and the connection survives, whereas an
// expired client context makes pgx close the connection.
const ashStatementTimeout = "SET statement_timeout = 400"

var errASHReconnectPending = errors.New("ash: dedicated connection unavailable, waiting to reconnect")

// ashConn is the sampler's dedicated connection, replaced whenever it dies.
// It used to be acquired once for the life of the process, so after a target
// restart, a network blip or one tick that overran its deadline every later
// sample failed and ASH stayed dark until the agent was restarted. A target
// that was down when the agent started no longer disables ASH for good
// either: the first successful tick connects.
type ashConn struct {
	mgr  *agent.Manager
	base context.Context
	clk  clock.Clock

	mu           sync.Mutex
	conn         *pgxpool.Conn
	retryAfter   time.Time
	warnedHidden bool // the hidden-sessions warning has been printed for this target
}

func (c *ashConn) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil && c.conn.Conn().IsClosed() {
		c.conn.Release() // the pool destroys a closed connection
		c.conn = nil
	}
	if c.conn == nil {
		if c.clk.Now().Before(c.retryAfter) {
			return nil, errASHReconnectPending
		}
		if err := c.connectLocked(); err != nil { //nolint:contextcheck // dials on the sampler's lifetime context with its own budget: the 500ms tick deadline is too short for a TLS handshake to a distant target
			c.retryAfter = c.clk.Now().Add(ashReconnectBackoff)
			return nil, err
		}
	}
	return c.conn.Query(ctx, sql, args...) //nolint:sqlclosecheck // the caller, ash.Sampler, owns and closes the rows
}

func (c *ashConn) connectLocked() error {
	ctx, cancel := context.WithTimeout(c.base, 5*time.Second)
	defer cancel()
	conn, err := c.mgr.DedicatedConn(ctx)
	if err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, ashStatementTimeout); err != nil {
		conn.Release()
		return err
	}
	c.warnIfSessionsHidden(ctx, conn)
	c.conn = conn
	return nil
}

// warnIfSessionsHidden says once per target when its role cannot see other
// users' sessions. Their load is then missing from every sample, and the
// samples read as an idle instance; nothing else would report it.
func (c *ashConn) warnIfSessionsHidden(ctx context.Context, conn *pgxpool.Conn) {
	if c.warnedHidden {
		return
	}
	visible, err := ash.SessionsVisible(ctx, conn)
	if err != nil || visible {
		return
	}
	c.warnedHidden = true
	fmt.Fprintf(os.Stderr, "warning: ash: the role of %s cannot see other users' sessions (grant pg_monitor or pg_read_all_stats); their activity is not sampled\n", c.mgr.TargetName())
}

// Close returns the connection to the pool.
func (c *ashConn) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.Release()
		c.conn = nil
	}
}

// ashWindowResult turns one closed window into the wire.Result it adds to the
// envelope; ok is false when the window has nothing to report. A window where
// every tick failed has neither samples nor ticks. Left out, it would look like
// an idle instance, or like no agent at all, so it is reported as a check error
// that carries the error of the last tick.
func ashWindowResult(w ash.Window, now time.Time, lastErr error) (res wire.Result, ok bool) {
	if metrics := ashWindowToMetrics(w); len(metrics) > 0 {
		return wire.Result{Check: "ash", TS: now, Metrics: metrics}, true
	}
	if w.FailedTicks == 0 {
		return wire.Result{}, false
	}
	msg := fmt.Sprintf("ash: none of the %d sampling ticks in the window succeeded", w.FailedTicks)
	if lastErr != nil {
		msg += ": " + lastErr.Error()
	}
	return wire.Result{Check: "ash", TS: now, Error: msg}, true
}

// ashWindowToMetrics converts one closed ash.Window into the wire.Metric
// shape internal/server/pipeline.go's buildASHRow already parses: one
// "ash_samples" gauge per bucket, labeled with the bucket's key plus the
// window's own ticks/seconds for the honest samples/ticks average.
//
// A window that ticked with no sampled session still yields one zero-count
// record carrying its ticks. The server counts those ticks in the denominator,
// so an idle stretch reads as zero active sessions rather than as no data. A
// window where every tick failed yields nothing: it stays a gap.
func ashWindowToMetrics(w ash.Window) []wire.Metric {
	if len(w.Buckets) == 0 {
		if w.Ticks == 0 {
			return nil
		}
		return []wire.Metric{{
			Name:  "ash_samples",
			Value: 0,
			Kind:  string(pgtype.KindGauge),
			Labels: map[string]string{
				"window_seconds": strconv.Itoa(int(ashWindow / time.Second)),
				"window_ticks":   strconv.Itoa(w.Ticks),
			},
		}}
	}
	metrics := make([]wire.Metric, 0, len(w.Buckets))
	for _, b := range w.Buckets {
		labels := map[string]string{
			"datname":         b.Key.Datname,
			"wait_event_type": b.Key.WaitEventType,
			"wait_event":      b.Key.WaitEvent,
			"state":           b.Key.State,
			"window_seconds":  strconv.Itoa(int(ashWindow / time.Second)),
			"window_ticks":    strconv.Itoa(w.Ticks),
		}
		if b.Key.HasQueryID {
			labels["queryid"] = strconv.FormatInt(b.Key.QueryID, 10)
		}
		metrics = append(metrics, wire.Metric{
			Name:   "ash_samples",
			Value:  float64(b.Samples),
			Kind:   string(pgtype.KindGauge),
			Labels: labels,
		})
	}
	return metrics
}
