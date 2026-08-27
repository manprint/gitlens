package server

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/delta"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/store"
	"github.com/manprint/pglens/internal/wire"
)

// Pipeline handles delta conversion and routing.
type Pipeline struct {
	pool      dbPool
	delta     *delta.Engine
	clock     clock.Clock
	mu        sync.Mutex
	lastEvict time.Time
}

// PipelineResult reports per-instance acceptance.
type PipelineResult struct {
	Accepted int
	Rejected int
	Errors   map[string]error
}

// NewPipeline creates a Pipeline backed by the given pool and clock.
// Clock may be nil, in which case the system clock is used.
func NewPipeline(pool *pgxpool.Pool, clk clock.Clock) *Pipeline {
	if clk == nil {
		clk = clock.System()
	}
	return &Pipeline{
		pool:      asDBPool(pool),
		delta:     delta.New(delta.Options{}),
		clock:     clk,
		lastEvict: clk.Now(),
	}
}

func (p *Pipeline) maybeEvict() {
	now := p.clock.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	if now.Sub(p.lastEvict) < 10*time.Minute {
		return
	}
	cutoff := now.Add(-1 * time.Hour)
	p.delta.Evict(cutoff)
	p.lastEvict = now
}

// destinationTable routes a check name to its storage table.
func destinationTable(check string) string {
	switch {
	case check == "stat_statements":
		return "metrics_statements"
	case check == "ash":
		return "metrics_ash"
	case strings.HasPrefix(check, "replication"):
		return "metrics_replication"
	default:
		return "metrics"
	}
}

// Process converts the envelope's counters to rates, routes metrics to their
// tables, and writes them in one transaction per envelope. Instances are
// processed independently; a per-instance failure increments Rejected without
// aborting the envelope.
func (p *Pipeline) Process(ctx context.Context, env wire.Envelope) (*PipelineResult, error) {
	res := &PipelineResult{Errors: make(map[string]error)}
	p.maybeEvict()

	if p.pool == nil {
		for _, inst := range env.Instances {
			if err := p.processInstanceNoDB(ctx, inst); err != nil {
				res.Rejected++
				res.Errors[inst.InstanceID] = err
				continue
			}
			res.Accepted++
		}
		return res, nil
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var genericRows []store.MetricRow
	var statementRows []store.StatementRow
	var ashRows []store.ASHRow
	var replicationRows []store.ReplicationRow
	var queryTextRows []store.QueryTextRow
	var eventRows []store.EventRow

	for _, inst := range env.Instances {
		cid, err := pgtype.ParseClusterID(inst.ClusterID)
		if err != nil {
			res.Rejected++
			res.Errors[inst.InstanceID] = fmt.Errorf("invalid cluster_id %q: %w", inst.ClusterID, err)
			continue
		}
		cidDB := store.ToDB(cid)
		instUUID, err := uuid.Parse(inst.InstanceID)
		if err != nil {
			res.Rejected++
			res.Errors[inst.InstanceID] = fmt.Errorf("invalid instance_id %q: %w", inst.InstanceID, err)
			continue
		}
		tenantID := "default"
		// Per-instance buffers
		var instGeneric []store.MetricRow
		var instASH []store.ASHRow
		var instQueryTexts []store.QueryTextRow
		stmtMap := make(map[statementKey]*store.StatementRow)
		replMap := make(map[replicationKey]*store.ReplicationRow)
		var instEvents []store.EventRow
		var instErr error

		for _, r := range inst.Results {
			if r.Truncated {
				evCluster := cidDB
				evInst := instUUID
				instEvents = append(instEvents, store.EventRow{
					TenantID:   tenantID,
					TS:         r.TS,
					Type:       "cardinality_truncated",
					ClusterID:  &evCluster,
					InstanceID: &evInst,
					Payload:    map[string]any{"check": r.Check, "database": r.Database},
				})
			}
			// QueryTexts upsert
			for qidStr, text := range r.QueryTexts {
				qid, err := strconv.ParseInt(qidStr, 10, 64)
				if err != nil {
					continue
				}
				pgMajor := 0
				if inst.PGVersion != 0 {
					pgMajor = int(pgtype.PGVersion(inst.PGVersion).Major())
				}
				datname := r.Database
				instQueryTexts = append(instQueryTexts, store.QueryTextRow{
					TenantID:  tenantID,
					ClusterID: cidDB,
					Datname:   datname,
					QueryID:   qid,
					PGMajor:   pgMajor,
					QueryText: text,
					LastSeen:  r.TS,
				})
			}
			for _, m := range r.Metrics {
				dest := destinationTable(r.Check)
				// ASH buckets are counts, never through delta, regardless of Kind.
				if dest == "metrics_ash" {
					kind := pgtype.MetricKind(m.Kind)
					if kind != pgtype.KindGauge && kind != pgtype.KindCounter {
						instErr = fmt.Errorf("unknown metric kind %q for metric %q", m.Kind, m.Name)
						break
					}
					row := buildASHRow(r.TS, tenantID, cidDB, instUUID, r.Database, m, r.Check)
					instASH = append(instASH, row)
					continue
				}
				kind := pgtype.MetricKind(m.Kind)
				var value float64
				var skip bool
				switch kind {
				case pgtype.KindGauge:
					value = m.Value
				case pgtype.KindCounter:
					canonical := pgtype.CanonicalLabels(m.Labels)
					key := pgtype.SeriesKey{
						Metric:   m.Name,
						Instance: instUUID,
						Database: r.Database,
						Labels:   canonical,
					}
					obs := delta.Observation{
						Key:        key,
						TS:         r.TS,
						Value:      m.Value,
						StatsReset: r.StatsReset,
					}
					pt, ev := p.delta.Observe(obs)
					if ev != nil {
						payload := map[string]any{
							"metric": m.Name,
							"reason": string(ev.Reason),
							"labels": m.Labels,
						}
						evCluster := cidDB
						evInst := instUUID
						instEvents = append(instEvents, store.EventRow{
							TenantID:   tenantID,
							TS:         ev.TS,
							Type:       "counter_reset_detected",
							ClusterID:  &evCluster,
							InstanceID: &evInst,
							Payload:    payload,
						})
						skip = true
					} else if pt == nil {
						skip = true
					} else {
						value = pt.Rate
					}
				default:
					instErr = fmt.Errorf("unknown metric kind %q for metric %q", m.Kind, m.Name)
				}
				if instErr != nil {
					break
				}
				if skip {
					continue
				}
				switch dest {
				case "metrics":
					canonical := pgtype.CanonicalLabels(m.Labels)
					key := pgtype.SeriesKey{
						Metric:   m.Name,
						Instance: instUUID,
						Database: r.Database,
						Labels:   canonical,
					}
					sid := store.SeriesID(key)
					labelsJSON := m.Labels
					// Ensure labels is at least empty map for consistent jsonb
					if labelsJSON == nil {
						labelsJSON = map[string]string{}
					}
					instGeneric = append(instGeneric, store.MetricRow{
						TS:         r.TS,
						TenantID:   tenantID,
						ClusterID:  cidDB,
						InstanceID: instUUID,
						Datname:    r.Database,
						Metric:     m.Name,
						Labels:     labelsJSON,
						SeriesID:   sid,
						Value:      value,
					})
				case "metrics_statements":
					qidStr := m.Labels["queryid"]
					if qidStr == "" {
						qidStr = m.Labels["query_id"]
					}
					if qidStr == "" {
						// Try to find any label that looks like queryid
						for k, v := range m.Labels {
							if strings.EqualFold(k, "queryid") {
								qidStr = v
								break
							}
						}
					}
					if qidStr == "" {
						// No queryid label: skip this metric, cannot map to typed table
						continue
					}
					qid, err := strconv.ParseInt(qidStr, 10, 64)
					if err != nil {
						continue
					}
					sk := statementKey{ts: r.TS, datname: r.Database, queryid: qid}
					row, ok := stmtMap[sk]
					if !ok {
						row = &store.StatementRow{
							TS:         r.TS,
							TenantID:   tenantID,
							ClusterID:  cidDB,
							InstanceID: instUUID,
							Datname:    r.Database,
							QueryID:    qid,
						}
						stmtMap[sk] = row
					}
					applyStatementMetric(row, m.Name, value)
				case "metrics_replication":
					rk := replicationKey{ts: r.TS, slot: replicationSlot(m.Labels), datname: r.Database}
					row, ok := replMap[rk]
					if !ok {
						row = &store.ReplicationRow{
							TS:         r.TS,
							TenantID:   tenantID,
							ClusterID:  cidDB,
							InstanceID: instUUID,
							SlotName:   rk.slot,
							EdgeType:   "streaming",
						}
						// Fill upstream_id if present
						if s, ok := m.Labels["upstream_id"]; ok {
							if uid, err := uuid.Parse(s); err == nil {
								row.UpstreamID = &uid
							}
						}
						if s, ok := m.Labels["sync_state"]; ok {
							v := s
							row.SyncState = &v
						}
						replMap[rk] = row
					}
					applyReplicationMetric(row, m.Name, value, m.Labels)
				}
			}
			if instErr != nil {
				break
			}
		}
		if instErr != nil {
			res.Rejected++
			res.Errors[inst.InstanceID] = instErr
			continue
		}
		// Append per-instance buffers to envelope buffers
		genericRows = append(genericRows, instGeneric...)
		ashRows = append(ashRows, instASH...)
		// Flatten statement map
		for _, row := range stmtMap {
			statementRows = append(statementRows, *row)
		}
		for _, row := range replMap {
			replicationRows = append(replicationRows, *row)
		}
		queryTextRows = append(queryTextRows, instQueryTexts...)
		eventRows = append(eventRows, instEvents...)
		res.Accepted++
	}

	if err := store.WriteMetrics(ctx, tx, genericRows); err != nil {
		return nil, fmt.Errorf("write metrics: %w", err)
	}
	if err := store.WriteStatements(ctx, tx, statementRows); err != nil {
		return nil, fmt.Errorf("write statements: %w", err)
	}
	if err := store.WriteASH(ctx, tx, ashRows); err != nil {
		return nil, fmt.Errorf("write ash: %w", err)
	}
	if err := store.WriteReplication(ctx, tx, replicationRows); err != nil {
		return nil, fmt.Errorf("write replication: %w", err)
	}
	if err := store.WriteQueryTexts(ctx, tx, queryTextRows); err != nil {
		return nil, fmt.Errorf("write query texts: %w", err)
	}
	if err := store.WriteEvents(ctx, tx, eventRows); err != nil {
		return nil, fmt.Errorf("write events: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return res, nil
}

func (p *Pipeline) processInstanceNoDB(_ context.Context, inst wire.Instance) error {
	cid, err := pgtype.ParseClusterID(inst.ClusterID)
	if err != nil {
		return fmt.Errorf("invalid cluster_id %q: %w", inst.ClusterID, err)
	}
	_ = store.ToDB(cid)
	if _, err := uuid.Parse(inst.InstanceID); err != nil {
		return fmt.Errorf("invalid instance_id %q: %w", inst.InstanceID, err)
	}
	instUUID, _ := uuid.Parse(inst.InstanceID)
	for _, r := range inst.Results {
		for _, m := range r.Metrics {
			dest := destinationTable(r.Check)
			if dest == "metrics_ash" {
				kind := pgtype.MetricKind(m.Kind)
				if kind != pgtype.KindGauge && kind != pgtype.KindCounter {
					return fmt.Errorf("unknown metric kind %q for metric %q", m.Kind, m.Name)
				}
				continue
			}
			kind := pgtype.MetricKind(m.Kind)
			switch kind {
			case pgtype.KindGauge:
				continue
			case pgtype.KindCounter:
				canonical := pgtype.CanonicalLabels(m.Labels)
				key := pgtype.SeriesKey{
					Metric:   m.Name,
					Instance: instUUID,
					Database: r.Database,
					Labels:   canonical,
				}
				obs := delta.Observation{
					Key:        key,
					TS:         r.TS,
					Value:      m.Value,
					StatsReset: r.StatsReset,
				}
				_, _ = p.delta.Observe(obs)
			default:
				return fmt.Errorf("unknown metric kind %q for metric %q", m.Kind, m.Name)
			}
		}
	}
	return nil
}

type statementKey struct {
	ts      time.Time
	datname string
	queryid int64
}

type replicationKey struct {
	ts      time.Time
	slot    string
	datname string
}

func replicationSlot(labels map[string]string) string {
	if v, ok := labels["slot_name"]; ok && v != "" {
		return v
	}
	if v, ok := labels["slot"]; ok && v != "" {
		return v
	}
	if v, ok := labels["application_name"]; ok && v != "" {
		return v
	}
	return ""
}

func applyStatementMetric(row *store.StatementRow, name string, value float64) {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "shared_blks_hit"):
		v := value
		row.SharedBlksHitRate = &v
	case strings.Contains(lower, "shared_blks_read"):
		v := value
		row.SharedBlksReadRate = &v
	case strings.Contains(lower, "wal_bytes"):
		v := value
		row.WALBytesRate = &v
	case strings.Contains(lower, "exec_time") || strings.Contains(lower, "total_exec"):
		v := value
		row.ExecTimeRateMs = &v
	case strings.Contains(lower, "calls"):
		v := value
		row.CallsRate = &v
	case strings.Contains(lower, "rows"):
		// Ensure not double-counting blks rows already handled
		if !strings.Contains(lower, "blks") {
			v := value
			row.RowsRate = &v
		}
	default:
		// Unknown statement metric: map to calls_rate as fallback so row is not empty
		if row.CallsRate == nil {
			v := value
			row.CallsRate = &v
		}
	}
}

func applyReplicationMetric(row *store.ReplicationRow, name string, value float64, labels map[string]string) {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "write_lag_bytes"):
		v := int64(value)
		row.WriteLagBytes = &v
	case strings.Contains(lower, "flush_lag_bytes"):
		v := int64(value)
		row.FlushLagBytes = &v
	case strings.Contains(lower, "replay_lag_bytes"):
		v := int64(value)
		row.ReplayLagBytes = &v
	case strings.Contains(lower, "write_lag") && strings.Contains(lower, "sec"):
		v := value
		row.WriteLagSec = &v
	case strings.Contains(lower, "flush_lag") && strings.Contains(lower, "sec"):
		v := value
		row.FlushLagSec = &v
	case strings.Contains(lower, "replay_lag") && strings.Contains(lower, "sec"):
		v := value
		row.ReplayLagSec = &v
	case strings.Contains(lower, "slot_active"):
		b := value != 0
		row.SlotActive = &b
	case strings.Contains(lower, "slot_retained") || strings.Contains(lower, "retained_bytes"):
		v := int64(value)
		row.SlotRetainedBytes = &v
	case strings.Contains(lower, "slot_wal_status"):
		s := fmt.Sprintf("%v", value)
		if v, ok := labels["slot_wal_status"]; ok {
			s = v
		} else if v, ok := labels["wal_status"]; ok {
			s = v
		}
		row.SlotWalStatus = &s
	case strings.Contains(lower, "sync_state"):
		s := fmt.Sprintf("%v", value)
		if v, ok := labels["sync_state"]; ok {
			s = v
		}
		row.SyncState = &s
	default:
		// Fallback: treat unknown replication metric as write_lag_bytes if none set
		if row.WriteLagBytes == nil && row.WriteLagSec == nil && row.ReplayLagBytes == nil {
			// Use generic mapping based on whether name suggests bytes vs sec
			if strings.Contains(lower, "bytes") {
				v := int64(value)
				row.WriteLagBytes = &v
			} else if strings.Contains(lower, "lag") {
				v := value
				row.WriteLagSec = &v
			}
		}
	}
}

func buildASHRow(ts time.Time, tenantID string, clusterID int64, instanceID uuid.UUID, datname string, m wire.Metric, _ string) store.ASHRow {
	waitEventType := m.Labels["wait_event_type"]
	if waitEventType == "" {
		waitEventType = "CPU"
	}
	waitEvent := m.Labels["wait_event"]
	if waitEvent == "" {
		waitEvent = "CPU"
	}
	state := m.Labels["state"]
	if state == "" {
		state = "active"
	}
	dn := m.Labels["datname"]
	if dn == "" {
		dn = datname
	}
	var qid *int64
	if s, ok := m.Labels["queryid"]; ok && s != "" {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			qid = &v
		}
	} else if s, ok := m.Labels["query_id"]; ok && s != "" {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			qid = &v
		}
	}
	// Also handle encoded queryid in labels map with different case
	if qid == nil {
		for k, v := range m.Labels {
			if strings.EqualFold(k, "queryid") && v != "" {
				if pv, err := strconv.ParseInt(v, 10, 64); err == nil {
					qid = &pv
					break
				}
			}
		}
	}
	samples := int(m.Value)
	windowSec := 10
	if v, ok := m.Labels["window_seconds"]; ok {
		if iv, err := strconv.Atoi(v); err == nil {
			windowSec = iv
		}
	} else if v, ok := m.Labels["window"]; ok {
		if iv, err := strconv.Atoi(v); err == nil {
			windowSec = iv
		}
	}
	windowTicks := 0
	if v, ok := m.Labels["window_ticks"]; ok {
		if iv, err := strconv.Atoi(v); err == nil {
			windowTicks = iv
		}
	} else if v, ok := m.Labels["ticks"]; ok {
		if iv, err := strconv.Atoi(v); err == nil {
			windowTicks = iv
		}
	}
	// Ensure datname default is empty string per schema, but use provided.
	return store.ASHRow{
		TS:            ts,
		TenantID:      tenantID,
		ClusterID:     clusterID,
		InstanceID:    instanceID,
		Datname:       dn,
		WaitEventType: waitEventType,
		WaitEvent:     waitEvent,
		State:         state,
		QueryID:       qid,
		Samples:       samples,
		WindowSeconds: windowSec,
		WindowTicks:   windowTicks,
	}
}
