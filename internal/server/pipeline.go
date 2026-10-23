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
	"github.com/manprint/pglens/internal/topology"
	"github.com/manprint/pglens/internal/wire"
)

// Pipeline handles delta conversion and routing.
type Pipeline struct {
	pool      dbPool
	delta     *delta.Engine
	topo      *topology.Engine
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
		topo:      topology.NewEngine(clk),
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
	// The topology engine accumulates exactly the same way the delta engine
	// does — one entry per instance, forever — and was never swept.
	p.topo.Evict(cutoff)
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
	case check == "table_stats":
		return "metrics_tables"
	case check == "index_stats":
		return "metrics_indexes"
	case check == "bloat_estimate":
		return "metrics_bloat"
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
	var objectFactRows []store.ObjectFactRow
	var tableRows []store.TableStatRow
	var indexRows []store.IndexStatRow
	var bloatRows []store.BloatRow
	var lockSnapshotRows []store.LockSnapshotRow
	var relationPrunes []store.RelationStatsPrune
	currentSeries := make(map[uuid.UUID]map[pgtype.SeriesKey]struct{}, len(env.Instances))

	// Resolves a wire.Edge.To (an upstream's addr, e.g. "pg-primary") to an
	// instance_id. Tries the current envelope first (fast path, no query).
	// Falls back to the instances table for an upstream that isn't in this
	// envelope at all — found live via SYS-REPL-003: once a dead instance
	// stops being pushed (it no longer has a live signal to report at all,
	// see flushEnvelope's own comment on this), its addr would otherwise
	// never resolve again, permanently breaking a standby's low-confidence
	// edge back to it — exactly the edge orphan-standby detection depends on
	// existing at all (topology.Engine's own TestEngine_OrphanStandby: an
	// edge object with a resolvable target is required to start its clock).
	addrToInstance := make(map[string]uuid.UUID, len(env.Instances)*2)
	targetNameToInstance := make(map[string]uuid.UUID, len(env.Instances))
	endpointKey := func(addr string, port int) string {
		if port > 0 {
			return addr + ":" + strconv.Itoa(port)
		}
		return addr
	}
	var primaryID uuid.UUID
	primaryCount := 0
	for _, inst := range env.Instances {
		if inst.Addr == "" {
		} else if id, err := uuid.Parse(inst.InstanceID); err == nil {
			addrToInstance[inst.Addr] = id
			if inst.Port > 0 {
				addrToInstance[endpointKey(inst.Addr, inst.Port)] = id
			}
		}
		if inst.TargetName != "" {
			if id, err := uuid.Parse(inst.InstanceID); err == nil {
				targetNameToInstance[inst.TargetName] = id
			}
		}
		if inst.Role == "primary" {
			if id, err := uuid.Parse(inst.InstanceID); err == nil {
				primaryID = id
				primaryCount++
			}
		}
	}
	dbAddrCache := make(map[string]uuid.UUID)
	resolveAddr := func(ctx context.Context, cidDB int64, addr string, port int) (uuid.UUID, bool) {
		// PostgreSQL may report the upstream's logical/container hostname
		// while the agent connects through localhost plus a published port.
		// TargetName is unambiguous within an agent envelope and must win over
		// the single-primary fallback below, otherwise a cascading standby is
		// incorrectly attached directly to the primary.
		if id, ok := targetNameToInstance[addr]; ok {
			return id, true
		}
		key := endpointKey(addr, port)
		if id, ok := addrToInstance[key]; ok {
			return id, true
		}
		if id, ok := dbAddrCache[key]; ok {
			return id, true
		}
		var id uuid.UUID
		if addr != "" {
			if err := tx.QueryRow(ctx, `SELECT instance_id FROM instances WHERE cluster_id=$1 AND target_name=$2 LIMIT 1`, cidDB, addr).Scan(&id); err == nil {
				dbAddrCache[key] = id
				return id, true
			}
		}
		query := `SELECT instance_id FROM instances WHERE cluster_id=$1 AND addr=$2 LIMIT 1`
		args := []any{cidDB, addr}
		if port > 0 {
			query = `SELECT instance_id FROM instances WHERE cluster_id=$1 AND addr=$2 AND port=$3 LIMIT 1`
			args = append(args, port)
		}
		if err := tx.QueryRow(ctx, query, args...).Scan(&id); err != nil {
			return uuid.UUID{}, false
		}
		dbAddrCache[key] = id
		return id, true
	}

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
		var instFacts []store.ObjectFactRow
		var clearCheckSkips []string
		partialState := make(map[partialKey]partialObservation)
		var instLockSnapshot *store.LockSnapshotRow
		tableMap := make(map[tableStatKey]*store.TableStatRow)
		indexMap := make(map[indexStatKey]*store.IndexStatRow)
		bloatMap := make(map[bloatKey]*store.BloatRow)
		var instRelationPrunes []store.RelationStatsPrune
		var instErr error
		instSeries := make(map[pgtype.SeriesKey]struct{})

		// internal/topology.Engine (6.2, unit-tested at 90.9% coverage) was
		// never actually called from anywhere in the real server — found
		// live while building SYS-REPL-001 (the plan's own acceptance
		// scenario), which needs a real `failover_detected` event and
		// found none, ever, no matter how correctly a real promote
		// happened.
		var topoEdges []topology.Edge
		for _, we := range inst.TopologyEdges {
			toID, ok := resolveAddr(ctx, cidDB, we.To, we.Port)
			if !ok && inst.Role == "standby" && primaryCount == 1 {
				// A receiver may report the upstream's logical/container hostname
				// while the agent reaches both databases through a shared address
				// (for example localhost plus distinct published ports). In that
				// case the address lookup is intentionally incomplete, but a single
				// primary in this envelope is an unambiguous upstream for a
				// standby. Keep the edge instead of silently dropping topology.
				toID, ok = primaryID, true
			}
			if !ok {
				continue // unresolvable — no instance in this cluster has ever reported this addr
			}
			topoEdges = append(topoEdges, topology.Edge{
				From:       instUUID,
				To:         toID,
				Type:       we.Type,
				Confidence: we.Confidence,
			})
		}
		{
			edgeRows := make([]store.TopologyEdgeRow, 0, len(topoEdges))
			for _, te := range topoEdges {
				// SyncState is left unset: it comes from the primary's own
				// replication_streaming check (pg_stat_replication), not
				// from the standby-side edge this loop resolves.
				edgeRows = append(edgeRows, store.TopologyEdgeRow{
					TenantID:     tenantID,
					ClusterID:    cidDB,
					FromInstance: instUUID,
					ToInstance:   te.To,
					EdgeType:     te.Type,
					Confidence:   te.Confidence,
				})
			}
			// A DB write failure here is the same class of problem as
			// store.WriteMetrics/WriteReplication/etc failing below — a
			// real transaction-level error (e.g. disk full), not a
			// per-instance data problem — so it aborts and rolls back the
			// whole envelope the same way, rather than silently rejecting
			// just this one instance.
			if err := store.WriteTopologyEdges(ctx, tx, tenantID, cidDB, instUUID, edgeRows); err != nil {
				return nil, fmt.Errorf("write topology edges: %w", err)
			}
		}
		for _, ev := range p.topo.Apply(topology.Observation{
			InstanceID: instUUID,
			ClusterID:  cid,
			Role:       pgtype.Role(inst.Role),
			Edges:      topoEdges,
		}) {
			evCluster := cidDB
			evInst := instUUID
			instEvents = append(instEvents, store.EventRow{
				TenantID:   tenantID,
				TS:         ev.Timestamp,
				Type:       string(ev.Type),
				ClusterID:  &evCluster,
				InstanceID: &evInst,
				Payload:    ev.Payload,
			})
		}

		for _, r := range inst.Results {
			if r.SkipReason != "" {
				reason := r.SkipReason
				instFacts = append(instFacts, store.ObjectFactRow{
					TenantID: tenantID, ClusterID: cidDB, InstanceID: instUUID,
					Kind: "check_skip", Key: r.Check, ValueText: &reason,
					FirstSeen: r.TS, LastSeen: r.TS, ChangedAt: r.TS,
				})
			} else if r.Error == "" {
				clearCheckSkips = append(clearCheckSkips, r.Check)
			}
			// Only a complete sample says which relations exist: a partial
			// one (the agent's top-N) or a truncated one (its hard cap)
			// leaves out relations that are still there, and a skipped or
			// failed one says nothing at all.
			complete := r.Error == "" && r.SkipReason == "" && !r.Truncated && !r.Partial
			if complete {
				switch dest := destinationTable(r.Check); dest {
				case "metrics_tables", "metrics_indexes", "metrics_bloat":
					instRelationPrunes = append(instRelationPrunes, store.RelationStatsPrune{TableName: dest, TenantID: tenantID, InstanceID: instUUID, Datname: r.Database, Before: r.TS, Present: presentRelations(r.Metrics)})
				}
			}
			if r.Error == "" && r.SkipReason == "" {
				pk := partialKey{check: r.Check, datname: r.Database}
				if latest, seen := partialState[pk]; !seen || !r.TS.Before(latest.ts) {
					partialState[pk] = partialObservation{ts: r.TS, partial: r.Partial, truncated: r.Truncated}
				}
			}
			if r.Error != "" {
				IncCheckError(r.Check)
			} else {
				IncCheckOK(r.Check)
			}
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
			for _, f := range r.Facts {
				if err := f.Validate(); err != nil {
					IncIngestRejected("invalid_fact")
					continue
				}
				if f.Kind == "lock_tree" {
					instLockSnapshot = &store.LockSnapshotRow{TenantID: tenantID, InstanceID: instUUID, ClusterID: cidDB, TS: r.TS, Tree: append([]byte(nil), f.ValueJSON...)}
					continue
				}
				if f.Kind == "plan" {
					// phase 8: plans are routed to their dedicated history table.
					continue
				}
				var text *string
				if f.ValueText != "" || f.EmptySetting() {
					v := f.ValueText
					text = &v
				}
				var raw []byte
				if len(f.ValueJSON) > 0 {
					raw = append([]byte(nil), f.ValueJSON...)
				}
				instFacts = append(instFacts, store.ObjectFactRow{TenantID: tenantID, ClusterID: cidDB, InstanceID: instUUID, Datname: r.Database, Kind: f.Kind, Key: f.Key, Labels: f.Labels, ValueText: text, ValueJSON: raw, FirstSeen: r.TS, LastSeen: r.TS, ChangedAt: r.TS})
			}
			// A base backup is visible only while it streams. The last time
			// one was seen is kept as a fact, so the advisor can tell "no
			// backup this week" from "no backup right now" without scanning
			// a month of samples.
			if jobs := runningBasebackups(r); jobs > 0 {
				value := strconv.Itoa(jobs)
				instFacts = append(instFacts, store.ObjectFactRow{TenantID: tenantID, ClusterID: cidDB, InstanceID: instUUID, Kind: "backup", Key: "basebackup_running", ValueText: &value, FirstSeen: r.TS, LastSeen: r.TS, ChangedAt: r.TS})
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
				metricDatname := r.Database
				// Database-scoped checks such as database_stats emit one result
				// containing labelled rows. Preserve that database identity in the
				// generic metric row when the envelope result itself has no
				// Database field, so database-scoped alert samples retain datname.
				if metricDatname == "" {
					metricDatname = m.Labels["database"]
				}
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
				var value, sampleSeconds float64
				var skip bool
				switch kind {
				case pgtype.KindGauge:
					value = m.Value
				case pgtype.KindCounter:
					canonical := pgtype.CanonicalLabels(m.Labels)
					key := pgtype.SeriesKey{
						Metric:   m.Name,
						Instance: instUUID,
						Database: metricDatname,
						Labels:   canonical,
					}
					instSeries[key] = struct{}{}
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
						sampleSeconds = pt.Seconds
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
						Database: metricDatname,
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
						Datname:    metricDatname,
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
					if sampleSeconds > 0 && row.SampleSeconds == nil {
						v := sampleSeconds
						row.SampleSeconds = &v
					}
				case "metrics_replication":
					source := replicationSource(r.Check)
					rk := replicationKey{ts: r.TS, slot: replicationSlot(m.Labels), datname: r.Database, source: source}
					row, ok := replMap[rk]
					if !ok {
						row = &store.ReplicationRow{
							TS:         r.TS,
							TenantID:   tenantID,
							ClusterID:  cidDB,
							InstanceID: instUUID,
							SlotName:   rk.slot,
							EdgeType:   "streaming",
							Source:     source,
						}
						// No check has ever set an "upstream_id" label (checked:
						// replication_receiver.go/replication_streaming.go's
						// own label sets) — GET /api/v1/clusters/{id}/replication
						// (internal/server/api_topology.go) skips every row
						// with a null upstream_id outright, so this endpoint
						// always returned an empty edge list, found live
						// verifying phase 6.5's README example. Falls back to
						// the same resolved edge topoEdges already computed
						// above for this instance (at most one, in every
						// topology this plan tests).
						//
						// Only a receiver row describes this instance's own
						// upstream. A sender row describes one of its
						// standbys and a slot row one of its slots: giving
						// them this instance's upstream (a cascading standby
						// has one) filed them under the wrong edge.
						if s, ok := m.Labels["upstream_id"]; ok {
							if uid, err := uuid.Parse(s); err == nil {
								row.UpstreamID = &uid
							}
						} else if source == "receiver" && len(topoEdges) > 0 {
							to := topoEdges[0].To
							row.UpstreamID = &to
						}
						// "sync_mode" is the name older agents used for this
						// label; accept both so a fleet mid-upgrade still
						// records sync_state.
						if s, ok := m.Labels["sync_state"]; ok {
							v := s
							row.SyncState = &v
						} else if s, ok := m.Labels["sync_mode"]; ok {
							v := s
							row.SyncState = &v
						}
						replMap[rk] = row
					}
					applyReplicationMetric(row, m.Name, value, m.Labels)
				case "metrics_tables":
					key := tableStatKey{ts: r.TS, datname: r.Database, schema: m.Labels["schemaname"], relname: m.Labels["relname"]}
					row := tableMap[key]
					if row == nil {
						row = &store.TableStatRow{TS: r.TS, TenantID: tenantID, ClusterID: cidDB, InstanceID: instUUID, Datname: r.Database, Schemaname: key.schema, Relname: key.relname}
						tableMap[key] = row
					}
					applyTableMetric(row, strings.TrimPrefix(m.Name, "pg_table_"), value)
				case "metrics_indexes":
					key := indexStatKey{ts: r.TS, datname: r.Database, schema: m.Labels["schemaname"], indexrelname: m.Labels["indexrelname"]}
					row := indexMap[key]
					if row == nil {
						row = &store.IndexStatRow{TS: r.TS, TenantID: tenantID, ClusterID: cidDB, InstanceID: instUUID, Datname: r.Database, Schemaname: key.schema, Relname: m.Labels["relname"], IndexRelname: key.indexrelname}
						indexMap[key] = row
					}
					applyIndexMetric(row, strings.TrimPrefix(m.Name, "pg_index_"), value, m.Labels)
				case "metrics_bloat":
					key := bloatKey{ts: r.TS, datname: r.Database, schema: m.Labels["schemaname"], relname: m.Labels["relname"], indexrelname: m.Labels["indexrelname"], method: m.Labels["method"]}
					row := bloatMap[key]
					if row == nil {
						row = &store.BloatRow{TS: r.TS, TenantID: tenantID, ClusterID: cidDB, InstanceID: instUUID, Datname: r.Database, Schemaname: key.schema, Relname: key.relname, IndexRelname: key.indexrelname, ObjectKind: m.Labels["object_kind"], Method: key.method}
						bloatMap[key] = row
					}
					applyBloatMetric(row, strings.TrimPrefix(m.Name, "pg_bloat_"), value)
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
		instGeneric = append(instGeneric, selfMetricRows(env, inst, tenantID, cidDB, instUUID, p.clock.Now())...)
		currentSeries[instUUID] = instSeries
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
		for _, row := range tableMap {
			tableRows = append(tableRows, *row)
		}
		for _, row := range indexMap {
			indexRows = append(indexRows, *row)
		}
		for _, row := range bloatMap {
			bloatRows = append(bloatRows, *row)
		}
		queryTextRows = append(queryTextRows, instQueryTexts...)
		eventRows = append(eventRows, instEvents...)
		objectFactRows = append(objectFactRows, instFacts...)
		for _, checkName := range uniqueStrings(clearCheckSkips) {
			if err := store.DeleteCheckSkipFacts(ctx, tx, tenantID, instUUID, checkName); err != nil {
				return nil, err
			}
		}
		// A check whose latest result left something out keeps a
		// check_partial fact, which the read APIs turn into their
		// "truncated" flag; a complete result removes it again.
		for pk, obs := range partialState {
			if !obs.partial && !obs.truncated {
				if err := store.DeleteCheckPartialFact(ctx, tx, tenantID, instUUID, pk.datname, pk.check); err != nil {
					return nil, err
				}
				continue
			}
			value := "partial"
			if obs.truncated {
				value = "truncated"
			}
			objectFactRows = append(objectFactRows, store.ObjectFactRow{
				TenantID: tenantID, ClusterID: cidDB, InstanceID: instUUID, Datname: pk.datname,
				Kind: "check_partial", Key: pk.check, ValueText: &value,
				FirstSeen: obs.ts, LastSeen: obs.ts, ChangedAt: obs.ts,
			})
		}
		if instLockSnapshot != nil {
			lockSnapshotRows = append(lockSnapshotRows, *instLockSnapshot)
		}
		relationPrunes = append(relationPrunes, instRelationPrunes...)
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
	if err := store.WriteObjectFacts(ctx, tx, objectFactRows); err != nil {
		return nil, fmt.Errorf("write object facts: %w", err)
	}
	if err := store.WriteLockSnapshots(ctx, tx, lockSnapshotRows); err != nil {
		return nil, fmt.Errorf("write lock snapshots: %w", err)
	}
	for _, prune := range relationPrunes {
		if err := store.PruneRelationStats(ctx, tx, prune); err != nil {
			return nil, err
		}
	}
	if err := store.WriteTableStats(ctx, tx, tableRows); err != nil {
		return nil, fmt.Errorf("write table stats: %w", err)
	}
	if err := store.WriteIndexStats(ctx, tx, indexRows); err != nil {
		return nil, fmt.Errorf("write index stats: %w", err)
	}
	if err := store.WriteBloat(ctx, tx, bloatRows); err != nil {
		return nil, fmt.Errorf("write bloat: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	// I-8's own real number: how many distinct counter series each instance is
	// reporting in this envelope. The delta engine intentionally retains older
	// observations for rate continuity, so its full state is not the current
	// cardinality budget and must not be exposed as one.
	for instID, series := range currentSeries {
		SetSeriesTotal(instID.String(), float64(len(series)))
	}
	return res, nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// selfMetricRows derives the ingest self-monitoring series for one instance of
// one envelope.
//
// Three Tier 0 alert rules watch the collector itself — check_failing,
// cardinality_budget_exceeded and clock_skew — by comparing metrics from the
// `metrics` table, and nothing had ever written them: no check emits a
// pglens_* metric, so all three were structurally unable to fire whatever
// happened in the field, while looking perfectly configured in the API and
// the docs. The envelope is the only place these are knowable.
//
// They are gauges, written on every push, so the rules also receive the false
// sample that resolves them once the problem clears. `now` is the server's
// receive time, used only to measure the agent's clock skew.
func selfMetricRows(env wire.Envelope, inst wire.Instance, tenantID string, clusterID int64, instanceID uuid.UUID, now time.Time) []store.MetricRow {
	if len(inst.Results) == 0 {
		return nil
	}
	var failed, truncated int
	var ts time.Time
	for _, r := range inst.Results {
		if r.Error != "" {
			failed++
		}
		if r.Truncated {
			truncated++
		}
		if r.TS.After(ts) {
			ts = r.TS
		}
	}
	if ts.IsZero() {
		ts = env.SentAt
	}
	if ts.IsZero() {
		ts = now
	}
	total := float64(len(inst.Results))
	values := []struct {
		name  string
		value float64
	}{
		{"pglens_check_error_rate", float64(failed) / total},
		{"pglens_cardinality_truncated_rate", float64(truncated) / total},
	}
	// Skew is measured against the agent's own clock at transmission: a
	// drifting agent clock corrupts every ts in the envelope, so it has to be
	// durable and shared, not only a process-local gauge on whichever server
	// replica happened to receive this one push. SentAt is only the fallback
	// for agents that do not send the transmission time: it is when the
	// envelope was queued, so a replay after an outage read as hours of skew.
	sent := env.TransmittedAt
	if sent.IsZero() {
		sent = env.SentAt
	}
	if !sent.IsZero() {
		skew := now.Sub(sent).Seconds()
		if skew < 0 {
			skew = -skew
		}
		values = append(values, struct {
			name  string
			value float64
		}{"pglens_agent_clock_skew_seconds", skew})
	}
	rows := make([]store.MetricRow, 0, len(values))
	for _, v := range values {
		key := pgtype.SeriesKey{Metric: v.name, Instance: instanceID}
		rows = append(rows, store.MetricRow{
			TS:         ts,
			TenantID:   tenantID,
			ClusterID:  clusterID,
			InstanceID: instanceID,
			Metric:     v.name,
			Labels:     map[string]string{},
			SeriesID:   store.SeriesID(key),
			Value:      v.value,
		})
	}
	return rows
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
		if r.Error != "" {
			IncCheckError(r.Check)
		} else {
			IncCheckOK(r.Check)
		}
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
	source  string
}

// partialKey and partialObservation track, per check and database, whether
// the newest result in the envelope was complete.
type partialKey struct {
	check, datname string
}

type partialObservation struct {
	ts                 time.Time
	partial, truncated bool
}

// runningBasebackups is the number of base backups a result saw streaming.
func runningBasebackups(r wire.Result) int {
	if r.Error != "" || r.SkipReason != "" {
		return 0
	}
	for _, m := range r.Metrics {
		if m.Name == "pg_basebackup_jobs_running" && len(m.Labels) == 0 {
			return int(m.Value)
		}
	}
	return 0
}

// replicationSource names the view of replication a check reports.
func replicationSource(check string) string {
	switch check {
	case "replication_receiver":
		return "receiver"
	case "replication_streaming":
		return "sender"
	case "replication_slots":
		return "slot"
	default:
		return ""
	}
}

// presentRelations lists the relations a relation check's metrics describe.
func presentRelations(metrics []wire.Metric) []store.RelationKey {
	seen := make(map[store.RelationKey]struct{})
	var keys []store.RelationKey
	for _, m := range metrics {
		k := store.RelationKey{Schema: m.Labels["schemaname"], Relation: m.Labels["relname"], Index: m.Labels["indexrelname"]}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	return keys
}

type tableStatKey struct {
	ts                       time.Time
	datname, schema, relname string
}
type indexStatKey struct {
	ts                            time.Time
	datname, schema, indexrelname string
}
type bloatKey struct {
	ts                                             time.Time
	datname, schema, relname, indexrelname, method string
}

func applyTableMetric(row *store.TableStatRow, name string, value float64) {
	v := value
	switch strings.ToLower(name) {
	case "seq_scan":
		row.SeqScan = &v
	case "seq_tup_read":
		row.SeqTupRead = &v
	case "idx_scan":
		row.IdxScan = &v
	case "idx_tup_fetch":
		row.IdxTupFetch = &v
	case "n_tup_ins":
		row.NTupIns = &v
	case "n_tup_upd":
		row.NTupUpd = &v
	case "n_tup_del":
		row.NTupDel = &v
	case "n_tup_hot_upd":
		row.NTupHotUpd = &v
	case "autovacuum_count":
		row.AutovacuumCount = &v
	case "autoanalyze_count":
		row.AutoanalyzeCount = &v
	case "n_live_tup":
		x := int64(value)
		row.NLiveTup = &x
	case "n_dead_tup":
		x := int64(value)
		row.NDeadTup = &x
	case "n_mod_since_analyze":
		x := int64(value)
		row.NModSinceAnalyze = &x
	case "dead_tuple_ratio":
		x := value
		row.DeadTupleRatio = &x
	case "last_vacuum_age_seconds":
		x := value
		row.LastVacuumAgeSeconds = &x
		row.LastVacuum = ageToTime(row.TS, value)
	// The agent reports each timestamp as an age at the scrape. Only the
	// manual vacuum's was kept: last_vacuum, last_autovacuum, last_analyze
	// and last_autoanalyze stayed NULL on every row, so every large table
	// read as never vacuumed to the advisor, and a table vacuumed only by
	// autovacuum showed no vacuum at all.
	case "last_autovacuum_age_seconds":
		row.LastAutovacuum = ageToTime(row.TS, value)
	case "last_analyze_age_seconds":
		row.LastAnalyze = ageToTime(row.TS, value)
	case "last_autoanalyze_age_seconds":
		row.LastAutoanalyze = ageToTime(row.TS, value)
	case "relpages":
		x := int64(value)
		row.Relpages = &x
	case "reltuples":
		row.RelTuples = &v
	case "relfrozenxid_age":
		x := int64(value)
		row.RelfrozenXIDAge = &x
	case "total_bytes":
		x := int64(value)
		row.TotalBytes = &x
	case "table_bytes":
		x := int64(value)
		row.TableBytes = &x
	case "toast_bytes":
		x := int64(value)
		row.ToastBytes = &x
	}
}

// ageToTime turns an age in seconds at a scrape back into the timestamp it
// was measured from, at the microsecond precision of timestamptz.
func ageToTime(ts time.Time, ageSeconds float64) *time.Time {
	t := ts.Add(-time.Duration(ageSeconds * float64(time.Second))).Truncate(time.Microsecond)
	return &t
}

func applyIndexMetric(row *store.IndexStatRow, name string, value float64, labels map[string]string) {
	v := value
	switch strings.ToLower(name) {
	case "idx_scan":
		row.IdxScan = &v
	case "idx_tup_read":
		row.IdxTupRead = &v
	case "idx_tup_fetch":
		row.IdxTupFetch = &v
	case "idx_blks_read":
		row.IdxBlksRead = &v
	case "idx_blks_hit":
		row.IdxBlksHit = &v
	case "index_bytes":
		x := int64(value)
		row.IndexBytes = &x
	case "is_unique":
		x := value != 0
		row.IsUnique = &x
	case "is_primary":
		x := value != 0
		row.IsPrimary = &x
	case "is_valid":
		x := value != 0
		row.IsValid = &x
	case "def_hash":
		if s := labels["def_hash"]; s != "" {
			row.DefHash = &s
		}
	}
}

func applyBloatMetric(row *store.BloatRow, name string, value float64) {
	switch strings.ToLower(name) {
	case "real_bytes", "bloat_real_bytes":
		x := int64(value)
		row.RealBytes = &x
	case "expected_bytes", "bloat_expected_bytes":
		x := int64(value)
		row.ExpectedBytes = &x
	case "bytes", "bloat_bytes":
		x := int64(value)
		row.BloatBytes = &x
	case "ratio", "bloat_ratio":
		x := value
		row.BloatRatio = &x
	}
}

// replicationSlot names the row a replication metric belongs to: the slot,
// or for a primary's view of a standby the standby's name. Agents before
// protocol v3 sent that name as "standby", which nothing read, so all the
// standbys of a primary collapsed into one row.
func replicationSlot(labels map[string]string) string {
	for _, label := range []string{"slot_name", "slot", "application_name", "standby"} {
		if v := labels[label]; v != "" {
			return v
		}
	}
	return ""
}

func applyStatementMetric(row *store.StatementRow, name string, value float64) {
	lower := strings.ToLower(name)
	v := value
	switch {
	case strings.Contains(lower, "temp_blks_written"):
		row.TempBlksWrittenRate = &v
	case strings.Contains(lower, "shared_blks_hit"):
		row.SharedBlksHitRate = &v
	case strings.Contains(lower, "shared_blks_read"):
		row.SharedBlksReadRate = &v
	case strings.Contains(lower, "wal_bytes"):
		row.WALBytesRate = &v
	case strings.Contains(lower, "exec_time") || strings.Contains(lower, "total_exec"):
		row.ExecTimeRateMs = &v
	case strings.Contains(lower, "calls"):
		row.CallsRate = &v
	case strings.Contains(lower, "rows") && !strings.Contains(lower, "blks"):
		row.RowsRate = &v
	}
	// Anything else is not stored. Unknown metrics used to be written as
	// calls_rate, which then ranked statements by a number that was not
	// their call rate.
}

// applyReplicationMetric fills the column a replication metric names. The
// mapping is explicit: an unrecognised metric used to be stored as write lag
// (replication_slot_safe_wal_size_bytes and a slot's active_pid both ended up
// there), and a slot's WAL status, which arrives as the "status" label, was
// never stored at all.
func applyReplicationMetric(row *store.ReplicationRow, name string, value float64, labels map[string]string) {
	lower := strings.ToLower(name)
	if row.Source == "slot" || strings.Contains(lower, "slot_") {
		if row.SlotWalStatus == nil {
			for _, label := range []string{"slot_wal_status", "wal_status", "status"} {
				if v := labels[label]; v != "" {
					s := v
					row.SlotWalStatus = &s
					break
				}
			}
		}
		if row.SlotType == nil {
			if v := labels["slot_type"]; v != "" {
				s := v
				row.SlotType = &s
			}
		}
	}
	switch {
	case strings.Contains(lower, "slot_active_pid"):
		// Not stored: a process id is not a measurement.
	case strings.Contains(lower, "slot_safe_wal_size"):
		v := int64(value)
		row.SlotSafeWALSizeBytes = &v
	case strings.Contains(lower, "slot_active"):
		b := value != 0
		row.SlotActive = &b
	case strings.Contains(lower, "slot_retained") || strings.Contains(lower, "retained_bytes"):
		v := int64(value)
		row.SlotRetainedBytes = &v
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
	case strings.Contains(lower, "sync_state"):
		if v, ok := labels["sync_state"]; ok {
			s := v
			row.SyncState = &s
		} else if v, ok := labels["sync_mode"]; ok {
			s := v
			row.SyncState = &s
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
