package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/store"
)

// TopologyAPI handles topology-related read extensions.
// Phase 6 extends it with /clusters/{id}/topology and /clusters/{id}/replication endpoints.
type TopologyAPI struct {
	pool dbPool
}

// NewTopologyAPI creates a TopologyAPI backed by the given pool.
// Pool may be nil in unit tests.
func NewTopologyAPI(pool *pgxpool.Pool) *TopologyAPI {
	return &TopologyAPI{pool: asDBPool(pool)}
}

// RegisterRoutes mounts topology-specific read handlers onto r.
func (t *TopologyAPI) RegisterRoutes(r chi.Router) {
	r.Get("/api/v1/clusters/{id}/topology", t.handleTopology)
	r.Get("/api/v1/clusters/{id}/replication", t.handleReplication)
	r.Get("/api/v1/clusters/{id}/slots", t.handleSlots)
}

// ----- /api/v1/clusters/{id}/topology -----

type topologyResp struct {
	ClusterID string             `json:"cluster_id"`
	Topology  []topologyEdgeResp `json:"topology"`
	Events    []eventResponse    `json:"events"`
}

func (t *TopologyAPI) handleTopology(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := chi.URLParam(r, "id")
	if idStr == "" {
		writeError(w, http.StatusBadRequest, "missing cluster_id", "path param {id} required")
		return
	}

	cid, err := pgtype.ParseClusterID(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid cluster_id", err.Error())
		return
	}

	if t.pool == nil {
		writeJSON(w, http.StatusOK, topologyResp{
			ClusterID: cid.String(),
			Topology:  []topologyEdgeResp{},
			Events:    []eventResponse{},
		})
		return
	}

	cidDB := store.ToDB(cid)

	// Fetch topology edges for this cluster
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	erows, err := t.pool.Query(ctx,
		`SELECT from_instance, to_instance, edge_type, sync_state, confidence FROM topology_edges WHERE tenant_id='default' AND cluster_id=$1 ORDER BY from_instance, to_instance`,
		cidDB)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query edges failed", err.Error())
		return
	}
	defer erows.Close()

	edges := []topologyEdgeResp{}
	for erows.Next() {
		var from, to uuid.UUID
		var edgeType, confidence string
		var syncState sql.NullString
		if scanErr := erows.Scan(&from, &to, &edgeType, &syncState, &confidence); scanErr != nil {
			writeError(w, http.StatusInternalServerError, "scan edge failed", scanErr.Error())
			return
		}
		edge := topologyEdgeResp{
			From:       from.String(),
			To:         to.String(),
			Type:       edgeType,
			Confidence: confidence,
		}
		if syncState.Valid {
			edge.SyncState = &syncState.String
		}
		if confidence == "low" {
			// A low-confidence edge is the standby's last-known upstream while
			// its WAL receiver is not streaming (see wire.Edge.Confidence). The
			// upstream is still a known instance: the pipeline drops an edge it
			// cannot resolve.
			note := "The standby is not streaming from this upstream right now; check connectivity"
			edge.Note = &note
		}
		edges = append(edges, edge)
	}
	if err := erows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "edges rows error", err.Error())
		return
	}
	if edges == nil {
		edges = []topologyEdgeResp{}
	}
	syncStates, _, _, err := currentSyncStates(ctx, t.pool, cidDB)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query sync state failed", err.Error())
		return
	}
	for i := range edges {
		if edges[i].SyncState != nil {
			continue
		}
		if from, perr := uuid.Parse(edges[i].From); perr == nil {
			if state, ok := syncStates[from]; ok {
				edges[i].SyncState = &state
			}
		}
	}

	// Fetch failover events for this cluster
	arows, err := t.pool.Query(ctx,
		`SELECT event_id, ts, type, instance_id, payload FROM events WHERE tenant_id='default' AND cluster_id=$1 AND type='failover_detected' ORDER BY ts DESC`,
		cidDB)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query events failed", err.Error())
		return
	}
	defer arows.Close()

	events := []eventResponse{}
	for arows.Next() {
		var eid int64
		var ts time.Time
		var typ string
		var instID sql.NullString
		var payload []byte
		if scanErr := arows.Scan(&eid, &ts, &typ, &instID, &payload); scanErr != nil {
			writeError(w, http.StatusInternalServerError, "scan event failed", scanErr.Error())
			return
		}
		var iStr *string
		if instID.Valid {
			s := instID.String
			iStr = &s
		}
		cidStr := cid.String()
		raw := make(map[string]interface{})
		if len(payload) > 0 {
			// Try to unmarshal payload, but don't fail if it's not JSON
			_ = json.Unmarshal(payload, &raw)
		}
		rawMsg, _ := json.Marshal(raw)
		events = append(events, eventResponse{
			EventID:    eid,
			TS:         ts,
			Type:       typ,
			ClusterID:  &cidStr,
			InstanceID: iStr,
			Payload:    rawMsg,
		})
	}
	if err := arows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "events rows error", err.Error())
		return
	}
	if events == nil {
		events = []eventResponse{}
	}

	resp := topologyResp{
		ClusterID: cid.String(),
		Topology:  edges,
		Events:    events,
	}
	writeJSON(w, http.StatusOK, resp)
}

// ----- /api/v1/clusters/{id}/replication -----

type replicationSeriesResp struct {
	From string `json:"from"`
	To   string `json:"to"`
	// StandbyName is the application_name the primary reports the standby
	// under. When no monitored instance matches it, From carries it too.
	StandbyName *string       `json:"standby_name,omitempty"`
	Metric      string        `json:"metric"`
	SyncState   *string       `json:"sync_state,omitempty"`
	Series      []metricPoint `json:"series"`
}

type replicationResp struct {
	ClusterID string                  `json:"cluster_id"`
	Edges     []replicationSeriesResp `json:"edges"`
}

func (t *TopologyAPI) handleReplication(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := chi.URLParam(r, "id")
	if idStr == "" {
		writeError(w, http.StatusBadRequest, "missing cluster_id", "path param {id} required")
		return
	}

	cid, err := pgtype.ParseClusterID(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid cluster_id", err.Error())
		return
	}

	// Parse time range query params
	q := r.URL.Query()
	fromStr := q.Get("from")
	toStr := q.Get("to")

	if fromStr == "" || toStr == "" {
		writeError(w, http.StatusBadRequest, "missing time range", "query params from and to are required")
		return
	}

	from, err := parseTimeParam(fromStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid from", err.Error())
		return
	}
	to, err := parseTimeParam(toStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid to", err.Error())
		return
	}
	if !from.Before(to) {
		writeError(w, http.StatusBadRequest, "invalid range", "from must be before to")
		return
	}

	if t.pool == nil {
		writeJSON(w, http.StatusOK, replicationResp{
			ClusterID: cid.String(),
			Edges:     []replicationSeriesResp{},
		})
		return
	}

	cidDB := store.ToDB(cid)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resolver, err := loadStandbyResolver(ctx, t.pool, cidDB)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query instances failed", err.Error())
		return
	}

	// metrics_replication holds three views. A standby's receiver rows are
	// the edge standby -> upstream. A primary's sender rows (one per standby
	// in pg_stat_replication) are the same edge seen from the other end, and
	// the only source of write and flush lag and of sync_state: they used to
	// be dropped because they carry no upstream_id, so write/flush lag were
	// always empty and the sync state always Unknown. Slot rows are not
	// edges at all (see the slots endpoint). Rows written before migration
	// 0014 have no source and are told apart by their columns.
	rows, err := t.pool.Query(ctx,
		`SELECT instance_id, upstream_id, slot_name, `+store.ReplicationSourceSQL+` AS source,
		        sync_state, ts, write_lag_sec, flush_lag_sec, replay_lag_sec
		 FROM metrics_replication
		 WHERE tenant_id='default' AND cluster_id=$1 AND ts >= $2 AND ts < $3
		 ORDER BY ts`,
		cidDB, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query metrics failed", err.Error())
		return
	}
	defer rows.Close()

	type edgeMetrics struct {
		from, to     string
		standbyName  *string
		sender       bool
		syncState    *string
		writePoints  []metricPoint
		flushPoints  []metricPoint
		replayPoints []metricPoint
	}
	edgeMap := make(map[string]*edgeMetrics) // key = source:from:to
	var order []string

	for rows.Next() {
		var instID uuid.UUID
		var upID *uuid.UUID
		var slotName, source string
		var syncState *string
		var ts time.Time
		var wLag, fLag, rLag *float64
		if scanErr := rows.Scan(&instID, &upID, &slotName, &source, &syncState, &ts, &wLag, &fLag, &rLag); scanErr != nil {
			writeError(w, http.StatusInternalServerError, "scan metric failed", scanErr.Error())
			return
		}
		var fromID, toID string
		var standbyName *string
		switch source {
		case "receiver":
			if upID == nil {
				continue // the upstream could not be resolved
			}
			fromID, toID = instID.String(), upID.String()
		case "sender":
			if slotName == "" {
				continue
			}
			name := slotName
			standbyName = &name
			toID = instID.String()
			if standby, ok := resolver.resolve(instID, slotName); ok {
				fromID = standby.String()
			} else {
				fromID = slotName
			}
		default:
			continue
		}
		key := source + "\x00" + fromID + "\x00" + toID
		em, exists := edgeMap[key]
		if !exists {
			em = &edgeMetrics{from: fromID, to: toID, standbyName: standbyName, sender: source == "sender",
				writePoints: []metricPoint{}, flushPoints: []metricPoint{}, replayPoints: []metricPoint{}}
			edgeMap[key] = em
			order = append(order, key)
		}
		if syncState != nil {
			em.syncState = syncState // the newest reading wins
		}
		em.writePoints = append(em.writePoints, metricPoint{TS: ts, Value: wLag})
		em.flushPoints = append(em.flushPoints, metricPoint{TS: ts, Value: fLag})
		em.replayPoints = append(em.replayPoints, metricPoint{TS: ts, Value: rLag})
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "rows error", err.Error())
		return
	}

	// An edge both ends report is shown once, from the primary's side: it
	// measures write, flush and replay lag where the receiver only has an
	// estimate of replay lag.
	fromSender := make(map[string]bool)
	for _, em := range edgeMap {
		if em.sender {
			fromSender[em.from+"\x00"+em.to] = true
		}
	}
	edgeSeries := []replicationSeriesResp{}
	for _, key := range order {
		em := edgeMap[key]
		if !em.sender && fromSender[em.from+"\x00"+em.to] {
			continue
		}
		for _, series := range []struct {
			metric string
			points []metricPoint
		}{{"write_lag_sec", em.writePoints}, {"flush_lag_sec", em.flushPoints}, {"replay_lag_sec", em.replayPoints}} {
			edgeSeries = append(edgeSeries, replicationSeriesResp{
				From:        em.from,
				To:          em.to,
				StandbyName: em.standbyName,
				Metric:      series.metric,
				SyncState:   em.syncState,
				Series:      series.points,
			})
		}
	}

	resp := replicationResp{
		ClusterID: cid.String(),
		Edges:     edgeSeries,
	}
	writeJSON(w, http.StatusOK, resp)
}

// standbyResolver matches the name a primary reports a standby under
// (pg_stat_replication.application_name) to the monitored standby instance.
type standbyResolver struct {
	byTargetName map[string]uuid.UUID
	// standbysOf lists the standbys whose own topology edge points at each
	// upstream.
	standbysOf map[uuid.UUID][]uuid.UUID
	// namesOf lists the distinct standby names each primary has reported.
	namesOf map[uuid.UUID]map[string]struct{}
}

// loadStandbyResolver builds the standby resolver of one cluster.
func loadStandbyResolver(ctx context.Context, pool dbPool, cidDB int64) (*standbyResolver, error) {
	resolvers, err := loadStandbyResolvers(ctx, pool, []int64{cidDB})
	if err != nil {
		return nil, err
	}
	return resolvers[cidDB], nil
}

// loadStandbyResolvers builds the standby resolver of every cluster in cids,
// with one query per table for all of them rather than three per cluster.
func loadStandbyResolvers(ctx context.Context, pool dbPool, cids []int64) (map[int64]*standbyResolver, error) {
	resolvers := make(map[int64]*standbyResolver, len(cids))
	for _, cid := range cids {
		resolvers[cid] = &standbyResolver{
			byTargetName: make(map[string]uuid.UUID),
			standbysOf:   make(map[uuid.UUID][]uuid.UUID),
			namesOf:      make(map[uuid.UUID]map[string]struct{}),
		}
	}
	rows, err := pool.Query(ctx, `SELECT cluster_id, instance_id, COALESCE(target_name, '') FROM instances WHERE tenant_id='default' AND cluster_id = ANY($1)`, cids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var cid int64
		var id uuid.UUID
		var name string
		if err := rows.Scan(&cid, &id, &name); err != nil {
			rows.Close()
			return nil, err
		}
		if name != "" {
			resolvers[cid].byTargetName[name] = id
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = pool.Query(ctx, `SELECT cluster_id, from_instance, to_instance FROM topology_edges WHERE tenant_id='default' AND cluster_id = ANY($1)`, cids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var cid int64
		var standby, upstream uuid.UUID
		if err := rows.Scan(&cid, &standby, &upstream); err != nil {
			rows.Close()
			return nil, err
		}
		resolvers[cid].standbysOf[upstream] = append(resolvers[cid].standbysOf[upstream], standby)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = pool.Query(ctx, `SELECT DISTINCT cluster_id, instance_id, slot_name FROM metrics_replication
		WHERE tenant_id='default' AND cluster_id = ANY($1) AND source='sender' AND ts >= now() - interval '1 hour'`, cids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int64
		var primary uuid.UUID
		var name string
		if err := rows.Scan(&cid, &primary, &name); err != nil {
			return nil, err
		}
		if resolvers[cid].namesOf[primary] == nil {
			resolvers[cid].namesOf[primary] = make(map[string]struct{})
		}
		resolvers[cid].namesOf[primary][name] = struct{}{}
	}
	return resolvers, rows.Err()
}

// syncStateSummary is what the sync states of one cluster say: the sync state
// of each standby that resolves to an instance, how many standbys are
// synchronous (sync or quorum), and whether any sender row was seen at all.
type syncStateSummary struct {
	states      map[uuid.UUID]string
	synchronous int
	seen        bool
}

// currentSyncStates reads each primary's latest pg_stat_replication rows: the
// sync_state of every standby it serves, keyed by the standby instance when
// the row resolves to one, and how many standbys are synchronous (sync or
// quorum). topology_edges.sync_state, which used to be the only source, is
// never filled: the standby that reports an edge cannot see its own state.
func currentSyncStates(ctx context.Context, pool dbPool, cidDB int64) (map[uuid.UUID]string, int, bool, error) {
	resolver, err := loadStandbyResolver(ctx, pool, cidDB)
	if err != nil {
		return nil, 0, false, err
	}
	summaries, err := loadSyncStates(ctx, pool, []int64{cidDB}, map[int64]*standbyResolver{cidDB: resolver})
	if err != nil {
		return nil, 0, false, err
	}
	summary := summaries[cidDB]
	return summary.states, summary.synchronous, summary.seen, nil
}

// loadSyncStates reads the latest sync state of every primary in cids, with
// one query for all of them, and resolves each standby with its cluster's
// resolver.
func loadSyncStates(ctx context.Context, pool dbPool, cids []int64, resolvers map[int64]*standbyResolver) (map[int64]*syncStateSummary, error) {
	rows, err := pool.Query(ctx, `SELECT DISTINCT ON (cluster_id, instance_id, slot_name) cluster_id, instance_id, slot_name, sync_state
		FROM metrics_replication
		WHERE tenant_id='default' AND cluster_id = ANY($1) AND sync_state IS NOT NULL AND slot_name <> ''
		  AND ts > now() - interval '60 seconds'
		ORDER BY cluster_id, instance_id, slot_name, ts DESC`, cids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	summaries := make(map[int64]*syncStateSummary, len(cids))
	for _, cid := range cids {
		summaries[cid] = &syncStateSummary{states: make(map[uuid.UUID]string)}
	}
	for rows.Next() {
		var cid int64
		var primary uuid.UUID
		var name, state string
		if err := rows.Scan(&cid, &primary, &name, &state); err != nil {
			return nil, err
		}
		summary := summaries[cid]
		summary.seen = true
		if state == "sync" || state == "quorum" {
			summary.synchronous++
		}
		if standby, ok := resolvers[cid].resolve(primary, name); ok {
			summary.states[standby] = state
		}
	}
	return summaries, rows.Err()
}

// resolve finds the standby a primary's row describes: the instance whose
// agent target name is the application_name (the agent qualifies a name two
// standbys share as name@address:port), or else the primary's only standby
// when it reports exactly one.
func (r *standbyResolver) resolve(primary uuid.UUID, name string) (uuid.UUID, bool) {
	base := name
	if i := strings.IndexByte(name, '@'); i >= 0 {
		base = name[:i]
	}
	if id, ok := r.byTargetName[base]; ok && id != primary {
		return id, true
	}
	if standbys := r.standbysOf[primary]; len(standbys) == 1 && len(r.namesOf[primary]) <= 1 {
		return standbys[0], true
	}
	return uuid.Nil, false
}

// ----- /api/v1/clusters/{id}/slots -----

type slotResp struct {
	InstanceID       string     `json:"instance_id"`
	SlotName         string     `json:"slot_name"`
	SlotType         *string    `json:"slot_type"`
	Active           *bool      `json:"active"`
	WALStatus        *string    `json:"wal_status"`
	RetainedBytes    *int64     `json:"retained_bytes"`
	SafeWALSizeBytes *int64     `json:"safe_wal_size_bytes"`
	LastActiveAt     *time.Time `json:"last_active_at"`
	TS               time.Time  `json:"ts"`
}

type slotsResp struct {
	ClusterID string     `json:"cluster_id"`
	Slots     []slotResp `json:"slots"`
}

// slotsSQL is the newest reading of each slot, with the last active reading of
// an inactive one. That lookup binds the instance, the slot and the activity,
// ordered by time, which metrics_replication_slot_active_idx serves. Without
// it, each inactive slot walked its history back to the last active row.
const slotsSQL = `
		SELECT l.instance_id, l.slot_name, l.slot_type, l.slot_active, l.slot_wal_status,
		       l.slot_retained_bytes, l.slot_safe_wal_size_bytes,
		       CASE WHEN l.slot_active THEN l.ts ELSE (
		         SELECT max(h.ts) FROM metrics_replication h
		          WHERE h.tenant_id='default' AND h.instance_id=l.instance_id
		            AND h.slot_name=l.slot_name AND h.slot_active) END,
		       l.ts
		FROM (SELECT DISTINCT ON (instance_id, slot_name) *
		        FROM metrics_replication
		       WHERE tenant_id='default' AND cluster_id=$1 AND slot_name <> ''
		         AND (source='slot' OR (source IS NULL AND slot_active IS NOT NULL))
		         AND ts >= now() - interval '5 minutes'
		       ORDER BY instance_id, slot_name, ts DESC) l
		ORDER BY l.slot_name, l.instance_id`

// handleSlots returns the newest state of every replication slot in the
// cluster. The cluster page had a slots section and nothing to fill it with:
// no endpoint served the rows the replication_slots check has always sent.
func (t *TopologyAPI) handleSlots(w http.ResponseWriter, r *http.Request) {
	cid, err := pgtype.ParseClusterID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid cluster_id", err.Error())
		return
	}
	resp := slotsResp{ClusterID: cid.String(), Slots: []slotResp{}}
	if t.pool == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	// A slot not reported for five minutes (the check runs every 15s) has
	// been dropped, or its instance is gone. last_active_at looks back over
	// the retained history for the slot's last active reading.
	rows, err := t.pool.Query(ctx, slotsSQL, store.ToDB(cid))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query slots failed", err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var slot slotResp
		var instance uuid.UUID
		if err := rows.Scan(&instance, &slot.SlotName, &slot.SlotType, &slot.Active, &slot.WALStatus,
			&slot.RetainedBytes, &slot.SafeWALSizeBytes, &slot.LastActiveAt, &slot.TS); err != nil {
			writeError(w, http.StatusInternalServerError, "scan slot failed", err.Error())
			return
		}
		slot.InstanceID = instance.String()
		resp.Slots = append(resp.Slots, slot)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "rows error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
