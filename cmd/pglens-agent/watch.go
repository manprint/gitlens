package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/internal/ash"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/watch"
	"github.com/manprint/pglens/internal/wire"
)

// startWatch hangs the incident watcher on the ASH sampler of one target: it
// runs after each successful tick, on the sampler's connection, and queues
// what it captures straight away, so an incident reaches the server with the
// next push rather than with the next ASH window. checks.watch.enabled: false
// turns it off.
func startWatch(ctx context.Context, sampler *ash.Sampler, q watch.Querier, mgr *agent.Manager, targetName string, cfg agent.CheckConfig, clk clock.Clock, mu *sync.Mutex, pending map[string][]wire.Result) bool {
	if cfg.Enabled != nil && !*cfg.Enabled {
		return false
	}
	emit := func(incidents []watch.Incident) {
		res, ok := incidentResult(incidents, clk.Now())
		if !ok {
			return
		}
		mu.Lock()
		pending[targetName] = append(pending[targetName], res)
		mu.Unlock()
	}
	w := watch.New(q, clk, watch.DefaultOptions(), emit, relationResolver(mgr))
	failing := false
	sampler.SetBatchCallback(func(rows []ash.SampleRow) {
		w.Tick(ctx, lockWaiting(rows))
		// One line when probing starts failing and one when it recovers.
		if err := w.LastError(); err != nil && !failing {
			failing = true
			fmt.Fprintf(os.Stderr, "warning: watch: probe failed for %s: %v\n", targetName, err)
		} else if err == nil && failing {
			failing = false
			fmt.Fprintf(os.Stderr, "watch: probe recovered for %s\n", targetName)
		}
	})
	return true
}

// lockWaiting reports whether a tick sampled a session waiting on a lock.
func lockWaiting(rows []ash.SampleRow) bool {
	for _, r := range rows {
		if r.WaitEventType == "Lock" {
			return true
		}
	}
	return false
}

// incidentResult wraps incidents as facts of one result. The result is filed
// under the ASH check, whose sampler captured them, so the server needs no
// new check name and coverage reads as it did.
func incidentResult(incidents []watch.Incident, now time.Time) (wire.Result, bool) {
	facts := make([]wire.Fact, 0, len(incidents))
	for _, inc := range incidents {
		raw, err := json.Marshal(inc)
		if err != nil {
			continue
		}
		facts = append(facts, wire.Fact{Kind: watch.FactKind, Key: inc.ID, Labels: map[string]string{"type": inc.Type}, ValueJSON: raw})
	}
	if len(facts) == 0 {
		return wire.Result{}, false
	}
	return wire.Result{Check: "ash", TS: now, Facts: facts}, true
}

// relationNamesQuery names relations by OID; an index is named after its
// table, as the lock probe names it in its own database.
const relationNamesQuery = `SELECT c.oid::bigint, quote_ident(n.nspname) || '.' || quote_ident(t.relname)
  FROM pg_class c
  LEFT JOIN pg_index i ON i.indexrelid = c.oid
  JOIN pg_class t ON t.oid = COALESCE(i.indrelid, c.oid)
  JOIN pg_namespace n ON n.oid = t.relnamespace
 WHERE c.oid::bigint = ANY($1::bigint[])`

// relationResolver names relations of another database through the agent's
// pooled connection to it. A database the agent does not monitor, or any
// failure, leaves the relation named by its OID.
func relationResolver(mgr *agent.Manager) watch.Resolver {
	return func(ctx context.Context, datname string, oids []uint32) map[uint32]string {
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		conn, err := mgr.ForDatabase(ctx, datname)
		if err != nil {
			return nil
		}
		defer conn.Release()
		ids := make([]int64, len(oids))
		for i, oid := range oids {
			ids[i] = int64(oid)
		}
		rows, err := conn.Query(ctx, relationNamesQuery, ids)
		if err != nil {
			return nil
		}
		defer rows.Close()
		out := make(map[uint32]string, len(oids))
		for rows.Next() {
			var oid int64
			var name string
			if rows.Scan(&oid, &name) == nil {
				out[uint32(oid)] = name //nolint:gosec // an OID fits uint32
			}
		}
		return out
	}
}
