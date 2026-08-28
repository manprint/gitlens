//go:build e2e

package scenario

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

func init() {
	Register(Scenario{
		ID:       "SYS-ASH-001",
		Title:    "ASH conservation end to end: idle instance, then 10 known sessions",
		Topology: TopologyStandalone,
		Covers:   []string{"phase_08.md#7.2", "phase_08.md#7.4"},
		Expect: Expectations{
			Invariants: []string{"I-2", "I-3", "I-4"},
		},
		Run: func(ctx context.Context, e *Env) error {
			var instanceID string
			resolveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := poll(resolveCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				err := e.DB.QueryRow(ctx, `SELECT instance_id::text FROM instances LIMIT 1`).Scan(&instanceID)
				return err == nil, nil
			}); err != nil {
				return fmt.Errorf("resolve instance: %w", err)
			}

			pg := e.PG("pg")
			connCfg := pg.Config().ConnConfig

			// 10 known, concurrent, long-running sessions — each its own
			// dedicated raw connection (not the shared harness pool, whose
			// capacity is sized for ordinary test queries, not 10
			// simultaneously held ones).
			const sessions = 10
			const sleepSeconds = 35 // > 30s asked for, to comfortably span three full 10s windows
			startTS := time.Now().UTC()
			var wg sync.WaitGroup
			for i := 0; i < sessions; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					conn, err := pgx.ConnectConfig(ctx, connCfg)
					if err != nil {
						return
					}
					defer conn.Close(ctx)
					_, _ = conn.Exec(ctx, fmt.Sprintf("SELECT pg_sleep(%d)", sleepSeconds))
				}()
			}

			// Wait for all 10 to actually be active before trusting the
			// window that follows — otherwise the first window could
			// undercount purely from connection-establishment latency, not
			// a real conservation violation.
			estCtx, cancelEst := context.WithTimeout(ctx, 15*time.Second)
			defer cancelEst()
			if err := poll(estCtx, 1*time.Second, func(ctx context.Context) (bool, error) {
				var n int
				err := pg.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE state='active' AND query LIKE 'SELECT pg_sleep%'`).Scan(&n)
				return err == nil && n >= sessions, nil
			}); err != nil {
				return fmt.Errorf("the %d known sessions never all became active: %w", sessions, err)
			}
			allActiveTS := time.Now().UTC()

			// Let the sleeps finish, then give the last window a chance to
			// close and reach the server (window 10s + push_interval 5s +
			// margin).
			wg.Wait()
			select {
			case <-time.After(20 * time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}

			// Only trust the middle of the sleep period, not its edges: the
			// first attempt at this scenario queried from startTS-2s (before
			// any session had even connected) through startTS+45s, diluting
			// the average with ramp-up/ramp-down windows that never had all
			// 10 sessions active — observed live as 7.06 avg instead of ~10.
			// windowStart is comfortably after allActiveTS (all 10 confirmed
			// running); windowEnd stays comfortably before the earliest
			// possible completion (sessions sleep the same duration from
			// their own individually-staggered start, so the first one
			// finishes at startTS+sleepSeconds at the earliest).
			windowStart := allActiveTS.Add(2 * time.Second)
			windowEnd := startTS.Add(time.Duration(sleepSeconds)*time.Second - 5*time.Second)
			if !windowStart.Before(windowEnd) {
				return fmt.Errorf("no safe measurement window: sessions became active too close to their own end (allActiveTS=%s, deadline=%s)", allActiveTS, windowEnd)
			}

			// Sum samples and ticks per window (metrics_ash stores one row
			// per distinct wait-event bucket, so summing raw window_ticks
			// across bucket rows would over-count by however many buckets
			// exist that window — group by ts first).
			rows, err := e.DB.Query(ctx, `
				SELECT SUM(samples) AS total_samples, MAX(window_ticks) AS ticks
				FROM metrics_ash
				WHERE instance_id=$1::uuid AND ts >= $2 AND ts <= $3
				GROUP BY ts`,
				instanceID, windowStart, windowEnd)
			if err != nil {
				return fmt.Errorf("query ASH windows: %w", err)
			}
			defer rows.Close()

			var totalSamples, totalTicks int
			maxConnections := 0
			if err := pg.QueryRow(ctx, `SELECT current_setting('max_connections')::int`).Scan(&maxConnections); err != nil {
				return fmt.Errorf("query max_connections: %w", err)
			}
			for rows.Next() {
				var s, t int
				if err := rows.Scan(&s, &t); err != nil {
					return fmt.Errorf("scan ASH window: %w", err)
				}
				if t > 0 && s > t*maxConnections {
					return fmt.Errorf("window has samples (%d) > ticks (%d) x max_connections (%d) — arithmetically impossible, indicates double counting", s, t, maxConnections)
				}
				totalSamples += s
				totalTicks += t
			}
			if err := rows.Err(); err != nil {
				return fmt.Errorf("iterate ASH windows: %w", err)
			}
			if totalTicks == 0 {
				return fmt.Errorf("no ASH windows observed during the known-session period")
			}

			observed := float64(totalSamples) / float64(totalTicks)
			expected := float64(sessions)
			if diff := math.Abs(observed-expected) / expected; diff > 0.10 {
				return fmt.Errorf("avg active sessions %.2f not within 10%% of the %d known sessions (samples=%d ticks=%d)", observed, sessions, totalSamples, totalTicks)
			}

			e.AssertInvariants(e.T)
			return nil
		},
	})

	Register(Scenario{
		ID:       "SYS-ASH-002",
		Title:    "ASH disabled in configuration",
		Topology: TopologyStandalone,
		Covers:   []string{"phase_08.md#7.4"},
		Expect: Expectations{
			Invariants: []string{"I-2", "I-3", "I-4"},
		},
		Run: func(ctx context.Context, e *Env) error {
			var instanceID string
			resolveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := poll(resolveCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				err := e.DB.QueryRow(ctx, `SELECT instance_id::text FROM instances LIMIT 1`).Scan(&instanceID)
				return err == nil, nil
			}); err != nil {
				return fmt.Errorf("resolve instance: %w", err)
			}

			// Give the agent several push cycles' worth of time to have
			// pushed *something* ASH-related if it were going to.
			select {
			case <-time.After(20 * time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}

			var ashRows int
			if err := e.DB.QueryRow(ctx, `SELECT count(*) FROM metrics_ash WHERE instance_id=$1::uuid`, instanceID).Scan(&ashRows); err != nil {
				return fmt.Errorf("query metrics_ash: %w", err)
			}
			if ashRows != 0 {
				return fmt.Errorf("expected no metrics_ash rows with ASH disabled, got %d", ashRows)
			}

			clusters, err := e.API.Clusters()
			if err != nil {
				return fmt.Errorf("query clusters (checking the instance stayed healthy, no error): %w", err)
			}
			if len(clusters) == 0 {
				return fmt.Errorf("expected the cluster to still exist and be healthy with ASH disabled")
			}

			resp, err := e.API.Get(fmt.Sprintf("/api/v1/ash?instance_id=%s", instanceID))
			if err != nil {
				return fmt.Errorf("GET /api/v1/ash: %w", err)
			}
			enabled, ok := resp["enabled"]
			if !ok {
				return fmt.Errorf("expected an explicit \"enabled\" field in the response, got none: %v", resp)
			}
			if enabled != false {
				return fmt.Errorf("expected enabled=false, got %v", enabled)
			}

			e.AssertInvariants(e.T)
			return nil
		},
	})

	Register(Scenario{
		ID:       "SYS-LOAD-003",
		Title:    "workloadctl slow-query --sleep 30s --count 3: ASH and pg_stat_statements agree",
		Topology: TopologyStandalone,
		Covers:   []string{"phase_08.md#7.4"},
		Smoke:    true,
		Expect: Expectations{
			Invariants: []string{"I-2", "I-3", "I-4"},
		},
		Run: func(ctx context.Context, e *Env) error {
			var instanceID string
			resolveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := poll(resolveCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				err := e.DB.QueryRow(ctx, `SELECT instance_id::text FROM instances LIMIT 1`).Scan(&instanceID)
				return err == nil, nil
			}); err != nil {
				return fmt.Errorf("resolve instance: %w", err)
			}

			pg := e.PG("pg")
			cfg := pg.Config().ConnConfig
			dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
				cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database)

			const sessions = 3
			// > 30s asked for (same correction SYS-ASH-001 already made,
			// for the same reason): ASH aggregates in fixed 10s windows
			// (cmd/pglens-agent/ash.go's ashWindow, ticking since agent
			// startup, not synchronized to this scenario) plus push_interval
			// latency before a window lands in the DB at all — 30s left no
			// margin for even one complete aggregated window to arrive
			// within the measurement range (observed live: "no ASH samples
			// landed during the slow-query window").
			const sleepSeconds = 45
			startTS := time.Now().UTC()
			workloadErr := make(chan error, 1)
			go func() {
				_, err := e.Workload("slow-query", "--dsn", dsn, "--sleep", fmt.Sprintf("%ds", sleepSeconds), "--count", fmt.Sprintf("%d", sessions))
				workloadErr <- err
			}()

			// Same rationale as SYS-ASH-001: don't trust the window's edges,
			// only the middle, once all 3 sessions are confirmed active.
			estCtx, cancelEst := context.WithTimeout(ctx, 15*time.Second)
			defer cancelEst()
			if err := poll(estCtx, 1*time.Second, func(ctx context.Context) (bool, error) {
				var n int
				err := pg.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE state='active' AND query LIKE 'SELECT pg_sleep%'`).Scan(&n)
				return err == nil && n >= sessions, nil
			}); err != nil {
				return fmt.Errorf("the %d slow-query sessions never all became active: %w", sessions, err)
			}
			allActiveTS := time.Now().UTC()
			windowStart := allActiveTS.Add(2 * time.Second)
			windowEnd := startTS.Add(time.Duration(sleepSeconds)*time.Second - 5*time.Second)
			if !windowStart.Before(windowEnd) {
				return fmt.Errorf("no safe measurement window: sessions became active too close to their own end")
			}

			// Wall-clock time must actually REACH windowEnd (plus a grace
			// margin for ASH's 10s aggregation + push_interval latency)
			// before querying — querying immediately after merely computing
			// the window found nothing, since almost none of the window had
			// elapsed yet in real time.
			graceDeadline := windowEnd.Add(15 * time.Second)
			if d := time.Until(graceDeadline); d > 0 {
				select {
				case <-time.After(d):
				case <-ctx.Done():
					return ctx.Err()
				}
			}

			// Dominant wait_event_type/wait_event during the window.
			var dominantType, dominantEvent string
			var dominantWaitSamples int
			rows, err := e.DB.Query(ctx, `
				SELECT wait_event_type, wait_event, SUM(samples) AS s
				FROM metrics_ash
				WHERE instance_id=$1::uuid AND ts >= $2 AND ts <= $3
				GROUP BY wait_event_type, wait_event
				ORDER BY s DESC`,
				instanceID, windowStart, windowEnd)
			if err != nil {
				return fmt.Errorf("query ASH wait events: %w", err)
			}
			for rows.Next() {
				var wet, we string
				var s int
				if err := rows.Scan(&wet, &we, &s); err != nil {
					rows.Close()
					return fmt.Errorf("scan ASH wait event: %w", err)
				}
				if dominantType == "" {
					dominantType, dominantEvent, dominantWaitSamples = wet, we, s
				}
			}
			if err := rows.Err(); err != nil {
				return fmt.Errorf("iterate ASH wait events: %w", err)
			}
			rows.Close()
			if dominantType == "" {
				return fmt.Errorf("no ASH samples landed during the slow-query window")
			}
			if dominantType != "Timeout" || dominantEvent != "PgSleep" {
				return fmt.Errorf("dominant wait event was %s/%s (samples=%d), want Timeout/PgSleep", dominantType, dominantEvent, dominantWaitSamples)
			}

			// Roughly `sessions` average active sessions — a loose bound,
			// not SYS-ASH-001's strict 10%: that scenario's own history
			// (STATE.md) shows this class of average is measurably
			// timing-sensitive even with a tightened window, and the plan's
			// own wording here only asks for "roughly 3."
			var totalSamples, totalTicks int
			if err := e.DB.QueryRow(ctx, `
				SELECT COALESCE(SUM(s), 0), COALESCE(SUM(t), 0) FROM (
					SELECT SUM(samples) AS s, MAX(window_ticks) AS t
					FROM metrics_ash
					WHERE instance_id=$1::uuid AND ts >= $2 AND ts <= $3
					GROUP BY ts
				) windows`, instanceID, windowStart, windowEnd).Scan(&totalSamples, &totalTicks); err != nil {
				return fmt.Errorf("query ASH conservation totals: %w", err)
			}
			if totalTicks == 0 {
				return fmt.Errorf("no ASH windows observed during the slow-query period")
			}
			avgActive := float64(totalSamples) / float64(totalTicks)
			if avgActive < float64(sessions)*0.5 || avgActive > float64(sessions)*1.5 {
				return fmt.Errorf("avg active sessions %.2f not roughly %d (samples=%d ticks=%d)", avgActive, sessions, totalSamples, totalTicks)
			}

			// Dominant queryid during the window, attributed via group_by=queryid.
			var dominantQueryID int64
			var haveQueryID bool
			qidRows, err := e.DB.Query(ctx, `
				SELECT queryid, SUM(samples) AS s
				FROM metrics_ash
				WHERE instance_id=$1::uuid AND ts >= $2 AND ts <= $3 AND queryid IS NOT NULL
				GROUP BY queryid
				ORDER BY s DESC`,
				instanceID, windowStart, windowEnd)
			if err != nil {
				return fmt.Errorf("query ASH queryids: %w", err)
			}
			for qidRows.Next() {
				var qid int64
				var s int
				if err := qidRows.Scan(&qid, &s); err != nil {
					qidRows.Close()
					return fmt.Errorf("scan ASH queryid: %w", err)
				}
				if !haveQueryID {
					dominantQueryID, haveQueryID = qid, true
				}
			}
			if err := qidRows.Err(); err != nil {
				return fmt.Errorf("iterate ASH queryids: %w", err)
			}
			qidRows.Close()
			if !haveQueryID {
				return fmt.Errorf("no ASH sample carried a non-null queryid during the slow-query window (compute_query_id may not have been discovered yet)")
			}

			if err := <-workloadErr; err != nil {
				return fmt.Errorf("workloadctl slow-query: %w", err)
			}

			// The cross-check: pg_stat_statements (via stat_statements's own
			// 60s-plus-jitter interval, INT-STMT-002/SYS-RESET-002
			// precedent) must eventually report the SAME queryid ASH
			// attributed the sleeping statement to.
			crossCtx, cancelCross := context.WithTimeout(ctx, 200*time.Second)
			defer cancelCross()
			if err := poll(crossCtx, 3*time.Second, func(ctx context.Context) (bool, error) {
				var n int
				err := e.DB.QueryRow(ctx,
					`SELECT count(*) FROM metrics_statements WHERE instance_id=$1::uuid AND queryid=$2`,
					instanceID, dominantQueryID).Scan(&n)
				return err == nil && n > 0, err
			}); err != nil {
				return fmt.Errorf("queryid %d (ASH's dominant sleeping statement) never appeared in metrics_statements: %w", dominantQueryID, err)
			}

			e.AssertInvariants(e.T)
			return nil
		},
	})

	Register(Scenario{
		ID:       "SYS-LOAD-005",
		Title:    "workloadctl race --workers 20 --rows 100: transactionid contention",
		Topology: TopologyStandalone,
		Covers:   []string{"phase_08.md#7.4"},
		Expect: Expectations{
			Invariants: []string{"I-2", "I-3", "I-4"},
		},
		Run: func(ctx context.Context, e *Env) error {
			// `race` did not exist as a test/workload subcommand — only
			// deadlock, lock-storm, slow-query, idle-in-txn,
			// distinct-queries, oltp did. lock-storm's SELECT...FOR UPDATE
			// produces wait_event=tuple, not the transactionid contention
			// this scenario specifically needs (two sessions UPDATEing the
			// SAME row while one's transaction is still open, which is a
			// materially different PostgreSQL wait condition) — added a
			// real new `race` subcommand (test/workload/race.go) rather
			// than fake the signal or misuse an existing command.
			var instanceID string
			resolveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := poll(resolveCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				err := e.DB.QueryRow(ctx, `SELECT instance_id::text FROM instances LIMIT 1`).Scan(&instanceID)
				return err == nil, nil
			}); err != nil {
				return fmt.Errorf("resolve instance: %w", err)
			}

			cfg := e.PG("pg").Config().ConnConfig
			dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
				cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database)

			const raceDuration = 25 * time.Second
			startTS := time.Now().UTC()
			workloadErr := make(chan error, 1)
			go func() {
				_, err := e.Workload("race", "--dsn", dsn, "--workers", "20", "--rows", "100", "--duration", raceDuration.String())
				workloadErr <- err
			}()

			if err := <-workloadErr; err != nil {
				return fmt.Errorf("workloadctl race: %w", err)
			}

			// Same lesson as SYS-LOAD-003: wait for ASH's 10s aggregation +
			// push_interval latency to actually deliver the run's windows
			// before querying, not just for the workload process to exit.
			select {
			case <-time.After(15 * time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}
			windowEnd := time.Now().UTC()

			var dominantType, dominantEvent string
			var dominantSamples int
			rows, err := e.DB.Query(ctx, `
				SELECT wait_event_type, wait_event, SUM(samples) AS s
				FROM metrics_ash
				WHERE instance_id=$1::uuid AND ts >= $2 AND ts <= $3
				GROUP BY wait_event_type, wait_event
				ORDER BY s DESC`,
				instanceID, startTS, windowEnd)
			if err != nil {
				return fmt.Errorf("query ASH wait events: %w", err)
			}
			for rows.Next() {
				var wet, we string
				var s int
				if err := rows.Scan(&wet, &we, &s); err != nil {
					rows.Close()
					return fmt.Errorf("scan ASH wait event: %w", err)
				}
				if dominantType == "" {
					dominantType, dominantEvent, dominantSamples = wet, we, s
				}
			}
			if err := rows.Err(); err != nil {
				return fmt.Errorf("iterate ASH wait events: %w", err)
			}
			rows.Close()
			if dominantType == "" {
				return fmt.Errorf("no ASH samples landed during the race window")
			}
			if dominantType != "Lock" || dominantEvent != "transactionid" {
				return fmt.Errorf("dominant wait event was %s/%s (samples=%d), want Lock/transactionid", dominantType, dominantEvent, dominantSamples)
			}

			var totalSamples, totalTicks int
			if err := e.DB.QueryRow(ctx, `
				SELECT COALESCE(SUM(s), 0), COALESCE(SUM(t), 0) FROM (
					SELECT SUM(samples) AS s, MAX(window_ticks) AS t
					FROM metrics_ash
					WHERE instance_id=$1::uuid AND ts >= $2 AND ts <= $3
					GROUP BY ts
				) windows`, instanceID, startTS, windowEnd).Scan(&totalSamples, &totalTicks); err != nil {
				return fmt.Errorf("query ASH conservation totals: %w", err)
			}
			if totalTicks == 0 {
				return fmt.Errorf("no ASH windows observed during the race period")
			}
			avgActive := float64(totalSamples) / float64(totalTicks)
			if avgActive <= 5 {
				return fmt.Errorf("avg active sessions %.2f not above 5 (samples=%d ticks=%d)", avgActive, totalSamples, totalTicks)
			}

			e.AssertInvariants(e.T)
			return nil
		},
	})
}
