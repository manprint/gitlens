package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
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
			note := "Endpoint could not be resolved; check connectivity"
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
	From      string        `json:"from"`
	To        string        `json:"to"`
	Metric    string        `json:"metric"`
	SyncState *string       `json:"sync_state,omitempty"`
	Series    []metricPoint `json:"series"`
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

	// Query replication metrics per edge (from -> to)
	// The schema has: instance_id (the reporting instance), upstream_id (the edge destination)
	rows, err := t.pool.Query(ctx,
		`SELECT instance_id, upstream_id, sync_state, ts, write_lag_sec, flush_lag_sec, replay_lag_sec
		 FROM metrics_replication
		 WHERE tenant_id='default' AND cluster_id=$1 AND ts >= $2 AND ts < $3
		 ORDER BY instance_id, upstream_id, ts`,
		cidDB, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query metrics failed", err.Error())
		return
	}
	defer rows.Close()

	// Collect metrics grouped by edge and metric type
	type edgeMetrics struct {
		from         uuid.UUID
		to           uuid.UUID
		syncState    *string
		writePoints  []metricPoint
		flushPoints  []metricPoint
		replayPoints []metricPoint
	}
	edgeMap := make(map[string]*edgeMetrics) // key = "from:to"

	for rows.Next() {
		var instID uuid.UUID
		var upID sql.NullString
		var syncState sql.NullString
		var ts time.Time
		var wLag, fLag, rLag sql.NullFloat64

		if scanErr := rows.Scan(&instID, &upID, &syncState, &ts, &wLag, &fLag, &rLag); scanErr != nil {
			writeError(w, http.StatusInternalServerError, "scan metric failed", scanErr.Error())
			return
		}

		if !upID.Valid {
			continue // Skip entries without upstream
		}

		upID_uuid, err := uuid.Parse(upID.String)
		if err != nil {
			continue
		}

		key := fmt.Sprintf("%s:%s", instID.String(), upID_uuid.String())
		em, exists := edgeMap[key]
		if !exists {
			em = &edgeMetrics{
				from:         instID,
				to:           upID_uuid,
				writePoints:  []metricPoint{},
				flushPoints:  []metricPoint{},
				replayPoints: []metricPoint{},
			}
			if syncState.Valid {
				em.syncState = &syncState.String
			}
			edgeMap[key] = em
		}

		// Add metric points (with nil for missing values)
		var wVal *float64
		if wLag.Valid {
			wVal = &wLag.Float64
		}
		em.writePoints = append(em.writePoints, metricPoint{TS: ts, Value: wVal})

		var fVal *float64
		if fLag.Valid {
			fVal = &fLag.Float64
		}
		em.flushPoints = append(em.flushPoints, metricPoint{TS: ts, Value: fVal})

		var rVal *float64
		if rLag.Valid {
			rVal = &rLag.Float64
		}
		em.replayPoints = append(em.replayPoints, metricPoint{TS: ts, Value: rVal})
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "rows error", err.Error())
		return
	}

	// Convert to response format (one edge per series, per metric type)
	edgeSeries := []replicationSeriesResp{}
	for _, em := range edgeMap {
		// For each metric type, create a separate entry
		if len(em.writePoints) > 0 {
			edgeSeries = append(edgeSeries, replicationSeriesResp{
				From:      em.from.String(),
				To:        em.to.String(),
				Metric:    "write_lag_sec",
				SyncState: em.syncState,
				Series:    em.writePoints,
			})
		}
		if len(em.flushPoints) > 0 {
			edgeSeries = append(edgeSeries, replicationSeriesResp{
				From:      em.from.String(),
				To:        em.to.String(),
				Metric:    "flush_lag_sec",
				SyncState: em.syncState,
				Series:    em.flushPoints,
			})
		}
		if len(em.replayPoints) > 0 {
			edgeSeries = append(edgeSeries, replicationSeriesResp{
				From:      em.from.String(),
				To:        em.to.String(),
				Metric:    "replay_lag_sec",
				SyncState: em.syncState,
				Series:    em.replayPoints,
			})
		}
	}

	resp := replicationResp{
		ClusterID: cid.String(),
		Edges:     edgeSeries,
	}
	writeJSON(w, http.StatusOK, resp)
}
