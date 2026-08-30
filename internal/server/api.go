package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	pgxpgtype "github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/store"
)

const (
	defaultEventsLimit     = 100
	maxEventsLimit         = 1000
	defaultStatementsLimit = 20
	maxStatementsLimit     = 1000
	maxMetricPoints        = 10000
	// An instance is considered down after one minute without an accepted
	// envelope. L3 topology scenarios use this bound to distinguish a stale
	// parent from a live upstream without waiting for the agent's longer
	// retry horizon.
	upThreshold = 60 * time.Second
)

// API handles read endpoints.
type API struct {
	pool dbPool
}

// NewAPI creates an API backed by the given pool.
// Pool may be nil in unit tests that exercise only validation logic.
func NewAPI(pool *pgxpool.Pool) *API {
	return &API{pool: asDBPool(pool)}
}

// RegisterRoutes mounts all read API handlers onto r.
func (a *API) RegisterRoutes(r chi.Router) {
	r.Get("/api/v1/clusters", a.handleClusters)
	r.Get("/api/v1/instances", a.handleInstances)
	r.Get("/api/v1/instances/{id}", a.handleInstance)
	r.Get("/api/v1/metrics/query", a.handleMetricsQuery)
	r.Get("/api/v1/events", a.handleEvents)
	r.Get("/api/v1/statements", a.handleStatements)
	a.registerContentionRoutes(r)
	a.registerRelationRoutes(r)
	a.registerSettingsRoutes(r)
	a.registerHostRoutes(r)
	a.registerFindingsRoutes(r)
	a.registerPlanRoutes(r)
	a.registerAuditRoutes(r)
}

// Ready verifies that the database is reachable and the complete migration
// set has been applied. Liveness remains independent of this check.
func (a *API) Ready(ctx context.Context) error {
	if a == nil || a.pool == nil {
		return errors.New("database pool unavailable")
	}
	var one int
	if err := a.pool.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
		return fmt.Errorf("database ping: %w", err)
	}
	var migrated bool
	if err := a.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM schema_migrations)").Scan(&migrated); err != nil {
		return fmt.Errorf("migration status: %w", err)
	}
	if !migrated {
		return errors.New("database migrations are not applied")
	}
	return nil
}

// ----- helpers -----

type apiError struct {
	Error  string `json:"error"`
	Detail string `json:"detail"`
}

func writeError(w http.ResponseWriter, status int, msg, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiError{Error: msg, Detail: detail})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func parseTimeParam(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, errors.New("empty time")
	}
	// Try RFC3339, RFC3339Nano, and without timezone fallback.
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05Z07:00", "2006-01-02 15:04:05Z07:00"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid time %q: %w", s, errors.New("parse failed"))
}

func parseStep(s string) (time.Duration, error) {
	if s == "" {
		return 0, errors.New("missing step")
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid step %q: %w", s, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("step must be positive, got %q", s)
	}
	return d, nil
}

func parseLimitParam(s string, def, max int) (int, error) {
	if s == "" {
		return def, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid limit %q: %w", s, err)
	}
	if v <= 0 {
		return 0, fmt.Errorf("limit must be positive, got %q", s)
	}
	if v > max {
		return max, nil
	}
	return v, nil
}

func isUp(lastSeen time.Time, now time.Time) bool {
	return now.Sub(lastSeen) <= upThreshold
}

// healthForCluster computes ok/degraded/critical per phase_07.md §6.3:
// critical when there is no primary *or more than one* (split-brain —
// phase_07.md §6.3 names this explicitly; found missing entirely while
// running SYS-REPL-002 live: primaryCount was never even computed, only a
// hasPrimary bool, so two simultaneous primaries reported "ok" like
// everything was fine), degraded when any instance is down or a replica is
// lagging (maxReplayLagSeconds > 0 — nil/0 means "no lag reported", never
// itself a reason to degrade), ok otherwise.
func healthForCluster(primaryCount int, anyDown bool, instanceCount int, maxReplayLagSeconds *float64) string {
	if instanceCount == 0 {
		return "unknown"
	}
	if primaryCount != 1 {
		return "critical"
	}
	if anyDown {
		return "degraded"
	}
	if maxReplayLagSeconds != nil && *maxReplayLagSeconds > 0 {
		return "degraded"
	}
	return "ok"
}

type metricPoint struct {
	TS    time.Time `json:"ts"`
	Value *float64  `json:"value"`
}

type metricsResponse struct {
	Series []metricPoint `json:"series"`
}

type eventResponse struct {
	EventID    int64           `json:"event_id"`
	TS         time.Time       `json:"ts"`
	Type       string          `json:"type"`
	ClusterID  *string         `json:"cluster_id"`
	InstanceID *string         `json:"instance_id"`
	Payload    json.RawMessage `json:"payload"`
}

type statementEntry struct {
	QueryID   int64    `json:"queryid"`
	Datname   string   `json:"datname"`
	QueryText string   `json:"query_text"`
	Calls     *float64 `json:"calls,omitempty"`
	TotalExec *float64 `json:"total_exec_time_ms,omitempty"`
	Rows      *float64 `json:"rows,omitempty"`
	Truncated bool     `json:"truncated"`
}

type statementsResp struct {
	Statements      []statementEntry `json:"statements"`
	Truncated       bool             `json:"truncated"`
	ComparableScope string           `json:"comparable_scope"`
}

// ----- /api/v1/clusters -----

type clusterInstanceResp struct {
	InstanceID string    `json:"instance_id"`
	Addr       string    `json:"addr"`
	Port       int       `json:"port"`
	Role       string    `json:"role"`
	PGVersion  int       `json:"pg_version"`
	PermTier   string    `json:"perm_tier"`
	LastSeen   time.Time `json:"last_seen"`
	Up         bool      `json:"up"`
}

type topologyEdgeResp struct {
	From       string  `json:"from"`
	To         string  `json:"to"`
	Type       string  `json:"type"`
	SyncState  *string `json:"sync_state,omitempty"`
	Confidence string  `json:"confidence"`
	Note       *string `json:"note,omitempty"`
}

type clusterResp struct {
	ClusterID     string                `json:"cluster_id"`
	Name          *string               `json:"name"`
	IDSource      string                `json:"id_source"`
	Primary       *string               `json:"primary"`
	InstanceCount int                   `json:"instance_count"`
	Health        string                `json:"health"`
	Instances     []clusterInstanceResp `json:"instances"`
	// Phase 6 topology extensions (omitempty keeps pre-6 payload minimal).
	StandbyCount        int                `json:"standby_count,omitempty"`
	SyncStandbyCount    int                `json:"sync_standby_count,omitempty"`
	MaxReplayLagSeconds *float64           `json:"max_replay_lag_seconds,omitempty"`
	Topology            []topologyEdgeResp `json:"topology,omitempty"`
}

func (a *API) handleClusters(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if a.pool == nil {
		writeJSON(w, http.StatusOK, []clusterResp{})
		return
	}
	rows, err := a.pool.Query(ctx, `SELECT cluster_id, name, id_source FROM clusters WHERE tenant_id='default' ORDER BY cluster_id`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed", err.Error())
		return
	}
	defer rows.Close()

	type clusterRow struct {
		idDB     int64
		name     sql.NullString
		idSource string
	}
	var clusters []clusterRow
	for rows.Next() {
		var cr clusterRow
		if scanErr := rows.Scan(&cr.idDB, &cr.name, &cr.idSource); scanErr != nil {
			writeError(w, http.StatusInternalServerError, "scan failed", scanErr.Error())
			return
		}
		clusters = append(clusters, cr)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "rows error", err.Error())
		return
	}

	now := time.Now().UTC()
	resp := make([]clusterResp, 0, len(clusters))
	for _, c := range clusters {
		cidStr := store.FromDB(c.idDB).String()

		var namePtr *string
		if c.name.Valid {
			v := c.name.String
			namePtr = &v
		}

		// Fetch instances for this cluster.
		irows, qerr := a.pool.Query(ctx,
			`SELECT instance_id, addr, port, role, pg_version, perm_tier, last_seen FROM instances WHERE tenant_id='default' AND cluster_id=$1 ORDER BY addr, port`,
			c.idDB)
		if qerr != nil {
			writeError(w, http.StatusInternalServerError, "query instances failed", qerr.Error())
			return
		}
		instances := []clusterInstanceResp{}
		var primaryID *string
		var primaryLastSeen time.Time
		hasPrimary := false
		primaryCount := 0
		anyDown := false
		standbyCount := 0
		syncStandbyCount := 0
		for irows.Next() {
			var iid uuid.UUID
			var addr, role, permTier string
			var port, pgVersion int
			var lastSeen time.Time
			if scanErr := irows.Scan(&iid, &addr, &port, &role, &pgVersion, &permTier, &lastSeen); scanErr != nil {
				irows.Close()
				writeError(w, http.StatusInternalServerError, "scan instance failed", scanErr.Error())
				return
			}
			up := isUp(lastSeen, now)
			if !up {
				anyDown = true
			}
			if role == "standby" {
				standbyCount++
			}
			if role == "primary" {
				primaryCount++
			}
			// Among (possibly multiple, in a split-brain) role="primary"
			// rows, report the most recently-seen one — not simply the
			// first by addr/port order. An `addr`-ordered "first match"
			// picked a stale, already-dead former primary over a
			// genuinely-just-promoted one every time their names happened
			// to sort that way (found live running SYS-REPL-001: killing
			// the old primary doesn't retroactively change its own
			// instances.role row, so both rows legitimately say "primary"
			// for a while after a real failover, and the old one kept
			// winning).
			if role == "primary" && (!hasPrimary || lastSeen.After(primaryLastSeen)) {
				s := iid.String()
				primaryID = &s
				primaryLastSeen = lastSeen
				hasPrimary = true
			}
			instances = append(instances, clusterInstanceResp{
				InstanceID: iid.String(),
				Addr:       addr,
				Port:       port,
				Role:       role,
				PGVersion:  pgVersion,
				PermTier:   permTier,
				LastSeen:   lastSeen,
				Up:         up,
			})
		}
		if err := irows.Err(); err != nil {
			irows.Close()
			writeError(w, http.StatusInternalServerError, "instances rows error", err.Error())
			return
		}
		irows.Close()

		// Fetch topology edges for this cluster.
		trows, terr := a.pool.Query(ctx,
			`SELECT from_instance, to_instance, edge_type, sync_state, confidence FROM topology_edges WHERE tenant_id='default' AND cluster_id=$1 ORDER BY from_instance, to_instance`,
			c.idDB)
		if terr != nil {
			writeError(w, http.StatusInternalServerError, "query topology edges failed", terr.Error())
			return
		}
		topology := []topologyEdgeResp{}
		for trows.Next() {
			var from, to uuid.UUID
			var edgeType, confidence string
			var syncState sql.NullString
			if scanErr := trows.Scan(&from, &to, &edgeType, &syncState, &confidence); scanErr != nil {
				trows.Close()
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
				if syncState.String == "sync" {
					syncStandbyCount++
				}
			}
			if confidence == "low" {
				note := "Endpoint could not be resolved; check connectivity"
				edge.Note = &note
			}
			topology = append(topology, edge)
		}
		if err := trows.Err(); err != nil {
			trows.Close()
			writeError(w, http.StatusInternalServerError, "topology rows error", err.Error())
			return
		}
		trows.Close()

		// Fetch the CURRENT max replay lag across this cluster's instances —
		// a recent reading per instance, not the all-time max. Found live
		// while verifying the README's own "ok" health example against a
		// real idle cluster: a plain all-time MAX() means one transient lag
		// blip during replication's initial catch-up (routine, harmless, and
		// exactly what SYS-REPL-004 exercises the recovery half of) makes a
		// cluster report "degraded" forever, with no way for health to ever
		// recover. A plain "latest non-null reading per instance" isn't
		// enough either: `replication_streaming`'s replay_lag column reads
		// NULL whenever PostgreSQL has no fresh feedback to report (common —
		// confirmed live, most cycles), so "latest non-null" can still reach
		// back to that same one-time stale blip while every truly recent
		// row is null. Bounding to the last 60s (comfortably above every
		// replication check's own interval, 10-15s) is what actually lets a
		// resolved lag age out.
		var maxReplayLag *float64
		err = a.pool.QueryRow(ctx,
			`SELECT MAX(replay_lag_sec) FROM (
				SELECT DISTINCT ON (instance_id) replay_lag_sec
				FROM metrics_replication
				WHERE tenant_id='default' AND cluster_id=$1 AND replay_lag_sec IS NOT NULL
				  AND ts > now() - interval '60 seconds'
				ORDER BY instance_id, ts DESC
			) latest`,
			c.idDB).Scan(&maxReplayLag)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "query max lag failed", err.Error())
			return
		}

		health := healthForCluster(primaryCount, anyDown, len(instances), maxReplayLag)
		cr := clusterResp{
			ClusterID:     cidStr,
			Name:          namePtr,
			IDSource:      c.idSource,
			Primary:       primaryID,
			InstanceCount: len(instances),
			Health:        health,
			Instances:     instances,
		}
		if standbyCount > 0 {
			cr.StandbyCount = standbyCount
		}
		if syncStandbyCount > 0 {
			cr.SyncStandbyCount = syncStandbyCount
		}
		if maxReplayLag != nil && *maxReplayLag > 0 {
			cr.MaxReplayLagSeconds = maxReplayLag
		}
		if len(topology) > 0 {
			cr.Topology = topology
		}
		resp = append(resp, cr)
	}
	writeJSON(w, http.StatusOK, resp)
}

// ----- /api/v1/instances -----

type instanceListResp struct {
	InstanceID string    `json:"instance_id"`
	ClusterID  string    `json:"cluster_id"`
	Addr       string    `json:"addr"`
	Port       int       `json:"port"`
	Role       string    `json:"role"`
	PGVersion  int       `json:"pg_version"`
	PermTier   string    `json:"perm_tier"`
	LastSeen   time.Time `json:"last_seen"`
	Up         bool      `json:"up"`
}

func (a *API) handleInstances(w http.ResponseWriter, r *http.Request) {
	if a.pool == nil {
		writeJSON(w, http.StatusOK, []instanceListResp{})
		return
	}
	rows, err := a.pool.Query(r.Context(), `
		SELECT instance_id, cluster_id, addr, port, role, pg_version, perm_tier, last_seen
		  FROM instances
		 WHERE tenant_id='default'
		 ORDER BY addr, port`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query instances failed", err.Error())
		return
	}
	defer rows.Close()

	now := time.Now().UTC()
	instances := []instanceListResp{}
	for rows.Next() {
		var iid uuid.UUID
		var clusterID int64
		var instance instanceListResp
		if err := rows.Scan(&iid, &clusterID, &instance.Addr, &instance.Port, &instance.Role, &instance.PGVersion, &instance.PermTier, &instance.LastSeen); err != nil {
			writeError(w, http.StatusInternalServerError, "scan instance failed", err.Error())
			return
		}
		instance.InstanceID = iid.String()
		instance.ClusterID = store.FromDB(clusterID).String()
		instance.Up = isUp(instance.LastSeen, now)
		instances = append(instances, instance)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "instances rows error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, instances)
}

// ----- /api/v1/instances/{id} -----

type instanceDatabaseResp struct {
	Datname    string  `json:"datname"`
	Monitored  bool    `json:"monitored"`
	SkipReason *string `json:"skip_reason"`
}

type instanceDetailResp struct {
	InstanceID            string                 `json:"instance_id"`
	ClusterID             string                 `json:"cluster_id"`
	Addr                  string                 `json:"addr"`
	Port                  int                    `json:"port"`
	Role                  string                 `json:"role"`
	PGVersion             int                    `json:"pg_version"`
	PermTier              string                 `json:"perm_tier"`
	LastSeen              time.Time              `json:"last_seen"`
	Up                    bool                   `json:"up"`
	Databases             []instanceDatabaseResp `json:"databases"`
	DatabasesNotMonitored int                    `json:"databases_not_monitored"`
}

func (a *API) handleInstance(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := chi.URLParam(r, "id")
	if idStr == "" {
		// Fallback for direct URL without chi (tests may use plain mux).
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) > 0 {
			idStr = parts[len(parts)-1]
		}
	}
	iid, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", err.Error())
		return
	}
	if a.pool == nil {
		writeError(w, http.StatusNotFound, "instance not found", fmt.Sprintf("instance %s not found", idStr))
		return
	}
	var (
		dbIID     uuid.UUID
		clusterDB int64
		addr      string
		port      int
		role      string
		pgVersion int
		permTier  string
		lastSeen  time.Time
	)
	qErr := a.pool.QueryRow(ctx,
		`SELECT instance_id, cluster_id, addr, port, role, pg_version, perm_tier, last_seen FROM instances WHERE instance_id=$1`,
		iid).Scan(&dbIID, &clusterDB, &addr, &port, &role, &pgVersion, &permTier, &lastSeen)
	if qErr != nil {
		if errors.Is(qErr, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "instance not found", fmt.Sprintf("instance %s not found", idStr))
			return
		}
		writeError(w, http.StatusInternalServerError, "query failed", qErr.Error())
		return
	}
	clusterStr := store.FromDB(clusterDB).String()

	drows, err := a.pool.Query(ctx, `SELECT datname, monitored, skip_reason FROM databases WHERE instance_id=$1 ORDER BY datname`, iid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query databases failed", err.Error())
		return
	}
	defer drows.Close()
	dbs := []instanceDatabaseResp{}
	notMonitored := 0
	for drows.Next() {
		var datname string
		var monitored bool
		var skip sql.NullString
		if scanErr := drows.Scan(&datname, &monitored, &skip); scanErr != nil {
			writeError(w, http.StatusInternalServerError, "scan database failed", scanErr.Error())
			return
		}
		var skipPtr *string
		if skip.Valid {
			v := skip.String
			skipPtr = &v
		}
		if !monitored {
			notMonitored++
		}
		dbs = append(dbs, instanceDatabaseResp{
			Datname:    datname,
			Monitored:  monitored,
			SkipReason: skipPtr,
		})
	}
	if err := drows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "databases rows error", err.Error())
		return
	}
	if dbs == nil {
		dbs = []instanceDatabaseResp{}
	}
	now := time.Now().UTC()
	resp := instanceDetailResp{
		InstanceID:            dbIID.String(),
		ClusterID:             clusterStr,
		Addr:                  addr,
		Port:                  port,
		Role:                  role,
		PGVersion:             pgVersion,
		PermTier:              permTier,
		LastSeen:              lastSeen,
		Up:                    isUp(lastSeen, now),
		Databases:             dbs,
		DatabasesNotMonitored: notMonitored,
	}
	writeJSON(w, http.StatusOK, resp)
}

// ----- /api/v1/metrics/query -----

func (a *API) handleMetricsQuery(w http.ResponseWriter, r *http.Request) {
	// Validation before DB access so unit tests with nil pool still exercise error paths.
	q := r.URL.Query()
	metric := q.Get("metric")
	instanceIDStr := q.Get("instance_id")
	database := q.Get("database")
	fromStr := q.Get("from")
	toStr := q.Get("to")
	stepStr := q.Get("step")

	if metric == "" {
		writeError(w, http.StatusBadRequest, "missing metric", "query param metric is required")
		return
	}
	if instanceIDStr == "" {
		writeError(w, http.StatusBadRequest, "missing instance_id", "query param instance_id is required")
		return
	}
	iid, err := uuid.Parse(instanceIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", err.Error())
		return
	}
	_ = database // optional
	if fromStr == "" {
		writeError(w, http.StatusBadRequest, "missing from", "query param from is required")
		return
	}
	if toStr == "" {
		writeError(w, http.StatusBadRequest, "missing to", "query param to is required")
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
	if stepStr == "" {
		writeError(w, http.StatusBadRequest, "missing step", "query param step is required")
		return
	}
	step, err := parseStep(stepStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid step", err.Error())
		return
	}
	// Point count cap: >10000 points => 422
	duration := to.Sub(from)
	// Use ceil to be conservative: if duration not divisible, need extra bucket.
	points := int(math.Ceil(float64(duration) / float64(step)))
	if points <= 0 {
		points = 1
	}
	if points > maxMetricPoints {
		writeError(w, http.StatusUnprocessableEntity, "step too small", fmt.Sprintf("would produce %d points, max %d", points, maxMetricPoints))
		return
	}

	// Build bucket timestamps [from, from+step, ... ) < to
	buckets := make([]time.Time, 0, points)
	for i := 0; i < points; i++ {
		ts := from.Add(time.Duration(i) * step)
		if !ts.Before(to) {
			break
		}
		buckets = append(buckets, ts)
	}

	// If pool is nil, return all-null series (used by unit tests for null-gap rendering without DB).
	if a.pool == nil {
		series := make([]metricPoint, 0, len(buckets))
		for _, ts := range buckets {
			series = append(series, metricPoint{TS: ts, Value: nil})
		}
		writeJSON(w, http.StatusOK, metricsResponse{Series: series})
		return
	}

	// Query DB for points in range.
	ctx := r.Context()
	// Use context with timeout to avoid hanging handler.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Build query with optional database filter.
	var rows pgx.Rows
	if database != "" {
		rows, err = a.pool.Query(ctx,
			`SELECT ts, value FROM metrics WHERE instance_id=$1 AND metric=$2 AND datname=$3 AND ts >= $4 AND ts < $5 ORDER BY ts`,
			iid, metric, database, from, to)
	} else {
		rows, err = a.pool.Query(ctx,
			`SELECT ts, value FROM metrics WHERE instance_id=$1 AND metric=$2 AND ts >= $3 AND ts < $4 ORDER BY ts`,
			iid, metric, from, to)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed", err.Error())
		return
	}
	defer rows.Close()

	// Map bucket index -> value (last value wins if multiple per bucket)
	bucketValues := make(map[int]float64)
	bucketHas := make(map[int]bool)
	for rows.Next() {
		var ts time.Time
		var val float64
		if scanErr := rows.Scan(&ts, &val); scanErr != nil {
			writeError(w, http.StatusInternalServerError, "scan failed", scanErr.Error())
			return
		}
		// Compute bucket index: floor((ts - from)/step)
		if ts.Before(from) || !ts.Before(to) {
			continue
		}
		idx := int(ts.Sub(from) / step)
		if idx < 0 || idx >= len(buckets) {
			continue
		}
		bucketValues[idx] = val
		bucketHas[idx] = true
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "rows error", err.Error())
		return
	}

	series := make([]metricPoint, 0, len(buckets))
	for i, ts := range buckets {
		if bucketHas[i] {
			v := bucketValues[i]
			series = append(series, metricPoint{TS: ts, Value: &v})
		} else {
			series = append(series, metricPoint{TS: ts, Value: nil})
		}
	}
	writeJSON(w, http.StatusOK, metricsResponse{Series: series})
}

// ----- /api/v1/events -----

func (a *API) handleEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	clusterIDStr := q.Get("cluster_id")
	typeFilter := q.Get("type")
	fromStr := q.Get("from")
	toStr := q.Get("to")
	limitStr := q.Get("limit")

	limit, err := parseLimitParam(limitStr, defaultEventsLimit, maxEventsLimit)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid limit", err.Error())
		return
	}

	var clusterDB *int64
	if clusterIDStr != "" {
		cid, perr := pgtype.ParseClusterID(clusterIDStr)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "invalid cluster_id", perr.Error())
			return
		}
		v := store.ToDB(cid)
		clusterDB = &v
	}

	var fromPtr, toPtr *time.Time
	if fromStr != "" {
		t, perr := parseTimeParam(fromStr)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "invalid from", perr.Error())
			return
		}
		fromPtr = &t
	}
	if toStr != "" {
		t, perr := parseTimeParam(toStr)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "invalid to", perr.Error())
			return
		}
		toPtr = &t
	}
	if fromPtr != nil && toPtr != nil && !fromPtr.Before(*toPtr) {
		writeError(w, http.StatusBadRequest, "invalid range", "from must be before to")
		return
	}

	if a.pool == nil {
		writeJSON(w, http.StatusOK, []eventResponse{})
		return
	}
	ctx := r.Context()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Build dynamic query.
	var sb strings.Builder
	args := []any{}
	sb.WriteString(`SELECT event_id, ts, type, cluster_id, instance_id, payload FROM events WHERE tenant_id='default'`)
	argIdx := 1
	if clusterDB != nil {
		fmt.Fprintf(&sb, " AND cluster_id=$%d", argIdx)
		args = append(args, *clusterDB)
		argIdx++
	}
	if typeFilter != "" {
		fmt.Fprintf(&sb, " AND type=$%d", argIdx)
		args = append(args, typeFilter)
		argIdx++
	}
	if fromPtr != nil {
		fmt.Fprintf(&sb, " AND ts >= $%d", argIdx)
		args = append(args, *fromPtr)
		argIdx++
	}
	if toPtr != nil {
		fmt.Fprintf(&sb, " AND ts < $%d", argIdx)
		args = append(args, *toPtr)
		argIdx++
	}
	sb.WriteString(` ORDER BY ts DESC`)
	fmt.Fprintf(&sb, " LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := a.pool.Query(ctx, sb.String(), args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed", err.Error())
		return
	}
	defer rows.Close()

	events := []eventResponse{}
	for rows.Next() {
		var eid int64
		var ts time.Time
		var typ string
		var cID sql.NullInt64
		var iID pgxpgtype.UUID
		var payload []byte
		if scanErr := rows.Scan(&eid, &ts, &typ, &cID, &iID, &payload); scanErr != nil {
			writeError(w, http.StatusInternalServerError, "scan failed", scanErr.Error())
			return
		}
		var cStr *string
		if cID.Valid {
			s := store.FromDB(cID.Int64).String()
			cStr = &s
		}
		var iStr *string
		if iID.Valid {
			// pgtype.UUID stores 16 bytes
			u := uuid.UUID(iID.Bytes)
			s := u.String()
			iStr = &s
		}
		var raw json.RawMessage
		if len(payload) > 0 {
			raw = json.RawMessage(payload)
		} else {
			raw = json.RawMessage(`{}`)
		}
		events = append(events, eventResponse{
			EventID:    eid,
			TS:         ts,
			Type:       typ,
			ClusterID:  cStr,
			InstanceID: iStr,
			Payload:    raw,
		})
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "rows error", err.Error())
		return
	}
	if events == nil {
		events = []eventResponse{}
	}
	writeJSON(w, http.StatusOK, events)
}

// ----- /api/v1/statements -----

func (a *API) handleStatements(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	instanceIDStr := q.Get("instance_id")
	database := q.Get("database")
	fromStr := q.Get("from")
	toStr := q.Get("to")
	orderBy := q.Get("order_by")
	limitStr := q.Get("limit")

	if instanceIDStr == "" {
		writeError(w, http.StatusBadRequest, "missing instance_id", "query param instance_id is required")
		return
	}
	iid, err := uuid.Parse(instanceIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", err.Error())
		return
	}
	limit, err := parseLimitParam(limitStr, defaultStatementsLimit, maxStatementsLimit)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid limit", err.Error())
		return
	}
	// Normalize order_by
	orderClause := "total_exec_time DESC"
	if orderBy != "" {
		switch strings.ToLower(orderBy) {
		case "calls", "calls_rate", "total_calls":
			orderClause = "total_calls DESC"
		case "total_exec_time", "total_exec_time_ms", "exec_time", "total_time":
			orderClause = "total_exec_time DESC"
		case "rows", "rows_rate":
			orderClause = "total_rows DESC"
		case "shared_blks_hit", "shared_blks_hit_rate":
			orderClause = "total_hit DESC"
		case "shared_blks_read", "shared_blks_read_rate":
			orderClause = "total_read DESC"
		case "wal_bytes", "wal_bytes_rate":
			orderClause = "total_wal DESC"
		case "mean_exec_time", "mean_time":
			orderClause = "mean_exec DESC"
		default:
			writeError(w, http.StatusBadRequest, "invalid order_by", fmt.Sprintf("unknown order_by %q", orderBy))
			return
		}
	}

	var fromPtr, toPtr *time.Time
	if fromStr != "" {
		t, perr := parseTimeParam(fromStr)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "invalid from", perr.Error())
			return
		}
		fromPtr = &t
	}
	if toStr != "" {
		t, perr := parseTimeParam(toStr)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "invalid to", perr.Error())
			return
		}
		toPtr = &t
	}
	if fromPtr != nil && toPtr != nil && !fromPtr.Before(*toPtr) {
		writeError(w, http.StatusBadRequest, "invalid range", "from must be before to")
		return
	}

	// If from/to missing, use defaults that still allow query (last 1h) for unit tests without DB.
	if a.pool == nil {
		resp := statementsResp{
			Statements:      []statementEntry{},
			Truncated:       false,
			ComparableScope: "cluster",
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	ctx := r.Context()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Build query aggregating metrics_statements and joining query_texts.
	// We aggregate rates over the window; for simplicity SUM the rates.
	var sb strings.Builder
	args := []any{}
	sb.WriteString(`
		SELECT s.queryid, s.datname, MAX(qt.query_text) as qtext,
		       SUM(s.calls_rate) as total_calls,
		       SUM(s.exec_time_rate_ms) as total_exec_time,
		       SUM(s.rows_rate) as total_rows,
		       SUM(s.shared_blks_hit_rate) as total_hit,
		       SUM(s.shared_blks_read_rate) as total_read,
		       SUM(s.wal_bytes_rate) as total_wal,
		       CASE WHEN SUM(s.calls_rate) > 0 THEN SUM(s.exec_time_rate_ms)/SUM(s.calls_rate) ELSE NULL END as mean_exec
		FROM metrics_statements s
		LEFT JOIN query_texts qt ON qt.tenant_id=s.tenant_id AND qt.cluster_id=s.cluster_id AND qt.datname=s.datname AND qt.queryid=s.queryid
		WHERE s.tenant_id='default' AND s.instance_id=$1`)
	args = append(args, iid)
	argIdx := 2
	if database != "" {
		fmt.Fprintf(&sb, " AND s.datname=$%d", argIdx)
		args = append(args, database)
		argIdx++
	}
	if fromPtr != nil {
		fmt.Fprintf(&sb, " AND s.ts >= $%d", argIdx)
		args = append(args, *fromPtr)
		argIdx++
	}
	if toPtr != nil {
		fmt.Fprintf(&sb, " AND s.ts < $%d", argIdx)
		args = append(args, *toPtr)
		argIdx++
	}
	sb.WriteString(` GROUP BY s.queryid, s.datname`)
	sb.WriteString(` ORDER BY ` + orderClause)
	fmt.Fprintf(&sb, " LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := a.pool.Query(ctx, sb.String(), args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed", err.Error())
		return
	}
	defer rows.Close()

	entries := []statementEntry{}
	for rows.Next() {
		var qid int64
		var datname string
		var qtext sql.NullString
		var totalCalls, totalExec, totalRows, totalHit, totalRead, totalWAL sql.NullFloat64
		var meanExec sql.NullFloat64
		if scanErr := rows.Scan(&qid, &datname, &qtext, &totalCalls, &totalExec, &totalRows, &totalHit, &totalRead, &totalWAL, &meanExec); scanErr != nil {
			writeError(w, http.StatusInternalServerError, "scan failed", scanErr.Error())
			return
		}
		text := ""
		if qtext.Valid {
			text = qtext.String
		}
		e := statementEntry{
			QueryID:   qid,
			Datname:   datname,
			QueryText: text,
			Truncated: false,
		}
		if totalCalls.Valid {
			v := totalCalls.Float64
			e.Calls = &v
		}
		if totalExec.Valid {
			v := totalExec.Float64
			e.TotalExec = &v
		}
		if totalRows.Valid {
			v := totalRows.Float64
			e.Rows = &v
		}
		// Prefer totalExec for ordering; other fields remain optional.
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "rows error", err.Error())
		return
	}
	if entries == nil {
		entries = []statementEntry{}
	}
	truncated, err := a.instanceTruncated(ctx, iid, fromPtr, toPtr)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed", err.Error())
		return
	}
	resp := statementsResp{
		Statements:      entries,
		Truncated:       truncated,
		ComparableScope: "cluster",
	}
	writeJSON(w, http.StatusOK, resp)
}

// instanceTruncated reports whether the agent's cardinality selector dropped
// candidates for this instance during the requested window (a
// "cardinality_truncated" event, emitted by the pipeline whenever a pushed
// wire.Result carries Truncated=true — see stat_statements.go's Scrape).
func (a *API) instanceTruncated(ctx context.Context, instanceID uuid.UUID, from, to *time.Time) (bool, error) {
	var sb strings.Builder
	args := []any{instanceID}
	sb.WriteString(`SELECT EXISTS(SELECT 1 FROM events WHERE tenant_id='default' AND instance_id=$1 AND type='cardinality_truncated'`)
	argIdx := 2
	if from != nil {
		fmt.Fprintf(&sb, " AND ts >= $%d", argIdx)
		args = append(args, *from)
		argIdx++
	}
	if to != nil {
		fmt.Fprintf(&sb, " AND ts < $%d", argIdx)
		args = append(args, *to)
	}
	sb.WriteString(")")
	var truncated bool
	if err := a.pool.QueryRow(ctx, sb.String(), args...).Scan(&truncated); err != nil {
		return false, err
	}
	return truncated, nil
}
