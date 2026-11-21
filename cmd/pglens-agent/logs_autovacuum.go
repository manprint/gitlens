package main

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/manprint/pglens/internal/pglog"
	"github.com/manprint/pglens/internal/wire"
)

const (
	autovacuumRunKind        = "autovacuum_run"
	autovacuumFactsPerScrape = 500
)

// autovacuumFact turns an autovacuum.run log line into an autovacuum_run fact
// (plan 007 phase 8); false when the message is not a run header. The value
// holds the numbers the message had, named as the server's columns.
func autovacuumFact(e pglog.Entry) (wire.Fact, bool) {
	r, ok := pglog.ParseAutovacuum(e.Message)
	if !ok || r.Database == "" || r.Schema == "" || r.Relation == "" {
		return wire.Fact{}, false
	}
	value := map[string]any{}
	for k, v := range map[string]*int64{
		"index_scans": r.IndexScans, "pages_removed": r.PagesRemoved, "pages_remain": r.PagesRemain,
		"pages_scanned": r.PagesScanned, "tuples_removed": r.TuplesRemoved, "tuples_remain": r.TuplesRemain,
		"tuples_dead_not_removable": r.TuplesDeadNotRemovable, "buffer_hits": r.BufferHits,
		"buffer_reads": r.BufferReads, "buffer_dirtied": r.BufferDirtied, "wal_records": r.WALRecords,
		"wal_fpi": r.WALFullPageImages, "wal_bytes": r.WALBytes,
	} {
		if v != nil {
			value[k] = *v
		}
	}
	for k, v := range map[string]*float64{
		"cpu_user_seconds": r.CPUUserSeconds, "cpu_system_seconds": r.CPUSystemSeconds, "elapsed_seconds": r.ElapsedSeconds,
	} {
		if v != nil {
			value[k] = *v
		}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return wire.Fact{}, false
	}
	return wire.Fact{
		Kind: autovacuumRunKind,
		Key:  e.SessionID + "/" + strconv.FormatInt(e.LineNum, 10),
		Labels: map[string]string{
			"ts":         e.Time.UTC().Format(time.RFC3339Nano),
			"datname":    r.Database,
			"schemaname": r.Schema,
			"relname":    r.Relation,
			"kind":       r.Kind,
			"aggressive": strconv.FormatBool(r.Aggressive),
			"wraparound": strconv.FormatBool(r.Wraparound),
		},
		ValueJSON: raw,
	}, true
}
