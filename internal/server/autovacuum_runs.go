package server

import (
	"encoding/json"
	"math"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/manprint/pglens/internal/store"
	"github.com/manprint/pglens/internal/wire"
)

const autovacuumRunFactKind = "autovacuum_run"

// autovacuumRunValue is the ValueJSON of an autovacuum_run fact; keys are the
// autovacuum_runs columns (plan 007 A27).
type autovacuumRunValue struct {
	IndexScans             *int64   `json:"index_scans"`
	PagesRemoved           *int64   `json:"pages_removed"`
	PagesRemain            *int64   `json:"pages_remain"`
	PagesScanned           *int64   `json:"pages_scanned"`
	TuplesRemoved          *int64   `json:"tuples_removed"`
	TuplesRemain           *int64   `json:"tuples_remain"`
	TuplesDeadNotRemovable *int64   `json:"tuples_dead_not_removable"`
	BufferHits             *int64   `json:"buffer_hits"`
	BufferReads            *int64   `json:"buffer_reads"`
	BufferDirtied          *int64   `json:"buffer_dirtied"`
	WALRecords             *int64   `json:"wal_records"`
	WALFPI                 *int64   `json:"wal_fpi"`
	WALBytes               *int64   `json:"wal_bytes"`
	CPUUserSeconds         *float64 `json:"cpu_user_seconds"`
	CPUSystemSeconds       *float64 `json:"cpu_system_seconds"`
	ElapsedSeconds         *float64 `json:"elapsed_seconds"`
}

// autovacuumRunRow maps an autovacuum_run fact to its row; false when a label
// is missing or invalid or a number is negative.
func autovacuumRunRow(f wire.Fact, tenantID string, clusterID int64, instanceID uuid.UUID) (store.AutovacuumRunRow, bool) {
	ts, err := time.Parse(time.RFC3339Nano, f.Labels["ts"])
	if err != nil {
		return store.AutovacuumRunRow{}, false
	}
	kind := f.Labels["kind"]
	datname, schema, relname := f.Labels["datname"], f.Labels["schemaname"], f.Labels["relname"]
	if (kind != "vacuum" && kind != "analyze") || datname == "" || schema == "" || relname == "" {
		return store.AutovacuumRunRow{}, false
	}
	aggressive, err1 := strconv.ParseBool(f.Labels["aggressive"])
	wraparound, err2 := strconv.ParseBool(f.Labels["wraparound"])
	if err1 != nil || err2 != nil {
		return store.AutovacuumRunRow{}, false
	}
	var v autovacuumRunValue
	if err := json.Unmarshal(f.ValueJSON, &v); err != nil {
		return store.AutovacuumRunRow{}, false
	}
	for _, n := range []*int64{v.IndexScans, v.PagesRemoved, v.PagesRemain, v.PagesScanned, v.TuplesRemoved, v.TuplesRemain,
		v.TuplesDeadNotRemovable, v.BufferHits, v.BufferReads, v.BufferDirtied, v.WALRecords, v.WALFPI, v.WALBytes} {
		if n != nil && *n < 0 {
			return store.AutovacuumRunRow{}, false
		}
	}
	for _, x := range []*float64{v.CPUUserSeconds, v.CPUSystemSeconds, v.ElapsedSeconds} {
		if x != nil && (*x < 0 || math.IsNaN(*x) || math.IsInf(*x, 0)) {
			return store.AutovacuumRunRow{}, false
		}
	}
	return store.AutovacuumRunRow{
		TS: ts.UTC(), TenantID: tenantID, InstanceID: instanceID, ClusterID: clusterID,
		Datname: datname, Schemaname: schema, Relname: relname, Kind: kind,
		Aggressive: aggressive, Wraparound: wraparound,
		IndexScans: v.IndexScans, PagesRemoved: v.PagesRemoved, PagesRemain: v.PagesRemain, PagesScanned: v.PagesScanned,
		TuplesRemoved: v.TuplesRemoved, TuplesRemain: v.TuplesRemain, TuplesDeadNotRemovable: v.TuplesDeadNotRemovable,
		BufferHits: v.BufferHits, BufferReads: v.BufferReads, BufferDirtied: v.BufferDirtied,
		WALRecords: v.WALRecords, WALFPI: v.WALFPI, WALBytes: v.WALBytes,
		CPUUserSeconds: v.CPUUserSeconds, CPUSystemSeconds: v.CPUSystemSeconds, ElapsedSeconds: v.ElapsedSeconds,
	}, true
}
