package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

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

	conn, err := mgr.DedicatedConn(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: ash: acquire dedicated connection for %s: %v (ASH disabled for this target)\n", targetName, err)
		return func() {}, false
	}

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
		conn.Release()
		return func() {}, false
	}

	flushCtx, cancel := context.WithCancel(ctx)
	flushTicker := clk.NewTicker(ashWindow)
	go func() {
		defer flushTicker.Stop()
		for {
			select {
			case <-flushCtx.Done():
				return
			case <-flushTicker.C():
				w := agg.Flush(clk.Now())
				metrics := ashWindowToMetrics(w)
				if len(metrics) == 0 {
					continue
				}
				mu.Lock()
				pending[targetName] = append(pending[targetName], wire.Result{
					Check:   "ash",
					TS:      clk.Now(),
					Metrics: metrics,
				})
				mu.Unlock()
			}
		}
	}()

	stop = func() {
		cancel()
		sampler.Stop()
		conn.Release()
	}
	return stop, true
}

// ashWindowToMetrics converts one closed ash.Window into the wire.Metric
// shape internal/server/pipeline.go's buildASHRow already parses: one
// "ash_samples" gauge per bucket, labeled with the bucket's key plus the
// window's own ticks/seconds for the honest samples/ticks average.
func ashWindowToMetrics(w ash.Window) []wire.Metric {
	if len(w.Buckets) == 0 {
		return nil
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
