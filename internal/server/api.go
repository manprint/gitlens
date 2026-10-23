package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"mime"
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
	a.registerCatalogRoutes(r)
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

// maxJSONBodyBytes caps the body of a JSON write. Rule edits, silences and
// mutes are a few hundred bytes, and the decoder buffers each value in full, so
// a larger body is refused before it is read.
const maxJSONBodyBytes = 64 << 10

// decodeJSONBody decodes the JSON body of a write request into dst. It writes
// the error response and returns false when the body is too large or malformed.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	// The body must declare itself JSON. A form or text body is what a page on
	// another site can send without a preflight, so it is refused unread.
	if mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported media type", "the body must be application/json")
		return false
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)).Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "payload too large", fmt.Sprintf("the body may not exceed %d bytes", maxJSONBodyBytes))
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid body", err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	// The body is encoded before the status is written. Once the header is
	// out, a payload that fails to encode, such as a NaN, leaves the client a
	// 200 with no body and leaves nothing in the log.
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(v); err != nil {
		log.Printf("encode response: %v", err)
		writeError(w, http.StatusInternalServerError, "encode failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body.Bytes())
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

// healthLagThresholdSeconds is the replay lag a cluster tolerates before it
// is degraded: the threshold of the seeded replica.lag_high alert rule. Any
// lag above zero used to degrade the cluster, and a busy primary's standbys
// are always a few milliseconds behind, so busy clusters never read "ok".
const healthLagThresholdSeconds = 30

// healthForCluster computes ok/degraded/critical per phase_07.md §6.3:
// critical when there is no primary *or more than one* (split-brain —
// phase_07.md §6.3 names this explicitly; found missing entirely while
// running SYS-REPL-002 live: primaryCount was never even computed, only a
// hasPrimary bool, so two simultaneous primaries reported "ok" like
// everything was fine), degraded when any instance is down or a replica
// lags more than healthLagThresholdSeconds (nil means "no lag reported",
// never itself a reason to degrade), ok otherwise.
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
	if maxReplayLagSeconds != nil && *maxReplayLagSeconds > healthLagThresholdSeconds {
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
	// PostgreSQL query IDs are int64 values. They must cross the JSON
	// boundary as strings so browsers cannot round them past Number.MAX_SAFE_INTEGER.
	QueryID   string `json:"queryid"`
	Datname   string `json:"datname"`
	QueryText string `json:"query_text"`
	// Totals over the requested window. The shared-block, WAL and
	// temporary-block totals and the mean were computed and then dropped,
	// which left the UI's cache-hit ratio and temp bytes permanently empty.
	Calls           *float64 `json:"calls,omitempty"`
	TotalExec       *float64 `json:"total_exec_time_ms,omitempty"`
	Rows            *float64 `json:"rows,omitempty"`
	MeanExec        *float64 `json:"mean_exec_time,omitempty"`
	SharedBlksHit   *float64 `json:"shared_blks_hit,omitempty"`
	SharedBlksRead  *float64 `json:"shared_blks_read,omitempty"`
	WALBytes        *float64 `json:"wal_bytes,omitempty"`
	TempBlksWritten *float64 `json:"temp_blks_written,omitempty"`
	Truncated       bool     `json:"truncated"`
}

type statementsResp struct {
	Statements      []statementEntry `json:"statements"`
	Truncated       bool             `json:"truncated"`
	ComparableScope string           `json:"comparable_scope"`
	// Disabled and DisabledReason report that the agent skips the
	// stat_statements check on this instance (no readable pg_stat_statements,
	// or a tier too low). The interface looked for them and the server never
	// sent them, so a skipped check read as an instance without statements.
	Disabled       bool    `json:"disabled,omitempty"`
	DisabledReason *string `json:"disabled_reason,omitempty"`
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

// clusterInstanceRow is one instances row of the cluster list.
type clusterInstanceRow struct {
	id        uuid.UUID
	addr      string
	port      int
	role      string
	pgVersion int
	permTier  string
	lastSeen  time.Time
}

// clusterEndpoint is the host and port an instance answers on. An instance
// that is rebuilt or re-registered under a new id keeps its endpoint.
type clusterEndpoint struct {
	addr string
	port int
}

// clusterEdgeRow is one topology_edges row of the cluster list.
type clusterEdgeRow struct {
	from, to   uuid.UUID
	edgeType   string
	syncState  sql.NullString
	confidence string
}

func (a *API) handleClusters(w http.ResponseWriter, r *http.Request) {
	if a.pool == nil {
		writeJSON(w, http.StatusOK, []clusterResp{})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
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
	if len(clusters) == 0 {
		writeJSON(w, http.StatusOK, []clusterResp{})
		return
	}

	// Each per-cluster read is issued once for all the clusters and grouped
	// here, so the list costs the same round trips however many clusters
	// there are. It used to be a query per cluster, on the endpoint the
	// dashboard polls most.
	cids := make([]int64, 0, len(clusters))
	for _, c := range clusters {
		cids = append(cids, c.idDB)
	}

	irows, qerr := a.pool.Query(ctx,
		`SELECT cluster_id, instance_id, addr, port, role, pg_version, perm_tier, last_seen FROM instances WHERE tenant_id='default' AND cluster_id = ANY($1) ORDER BY cluster_id, addr, port`,
		cids)
	if qerr != nil {
		writeError(w, http.StatusInternalServerError, "query instances failed", qerr.Error())
		return
	}
	instanceRows := make(map[int64][]clusterInstanceRow)
	for irows.Next() {
		var cid int64
		var row clusterInstanceRow
		if scanErr := irows.Scan(&cid, &row.id, &row.addr, &row.port, &row.role, &row.pgVersion, &row.permTier, &row.lastSeen); scanErr != nil {
			irows.Close()
			writeError(w, http.StatusInternalServerError, "scan instance failed", scanErr.Error())
			return
		}
		instanceRows[cid] = append(instanceRows[cid], row)
	}
	if err := irows.Err(); err != nil {
		irows.Close()
		writeError(w, http.StatusInternalServerError, "instances rows error", err.Error())
		return
	}
	irows.Close()

	trows, terr := a.pool.Query(ctx,
		`SELECT cluster_id, from_instance, to_instance, edge_type, sync_state, confidence FROM topology_edges WHERE tenant_id='default' AND cluster_id = ANY($1) ORDER BY cluster_id, from_instance, to_instance`,
		cids)
	if terr != nil {
		writeError(w, http.StatusInternalServerError, "query topology edges failed", terr.Error())
		return
	}
	edgeRows := make(map[int64][]clusterEdgeRow)
	for trows.Next() {
		var cid int64
		var edge clusterEdgeRow
		if scanErr := trows.Scan(&cid, &edge.from, &edge.to, &edge.edgeType, &edge.syncState, &edge.confidence); scanErr != nil {
			trows.Close()
			writeError(w, http.StatusInternalServerError, "scan edge failed", scanErr.Error())
			return
		}
		edgeRows[cid] = append(edgeRows[cid], edge)
	}
	if err := trows.Err(); err != nil {
		trows.Close()
		writeError(w, http.StatusInternalServerError, "topology rows error", err.Error())
		return
	}
	trows.Close()

	resolvers, err := loadStandbyResolvers(ctx, a.pool, cids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query instances failed", err.Error())
		return
	}
	syncStates, err := loadSyncStates(ctx, a.pool, cids, resolvers)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query sync state failed", err.Error())
		return
	}

	// The CURRENT max replay lag across each cluster's instances: a recent
	// reading per instance, not the all-time max. A plain all-time MAX() means
	// one transient lag blip during replication's initial catch-up makes a
	// cluster report "degraded" forever. A plain "latest non-null reading per
	// instance" is not enough either, because replication_streaming's
	// replay_lag column reads NULL whenever PostgreSQL has no fresh feedback,
	// so "latest non-null" can still reach back to that stale blip. Bounding to
	// the last 60s is what lets a resolved lag age out.
	maxLags, err := loadMaxReplayLags(ctx, a.pool, cids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query max lag failed", err.Error())
		return
	}

	now := time.Now().UTC()
	resp := make([]clusterResp, 0, len(clusters))
	for _, c := range clusters {
		// A cluster with no instance behind it is left by a registration that
		// failed after its cluster row was written. healthForCluster reads it
		// as "unknown", which the spec does not allow, and the fleet summary
		// cannot count it, so it is left out of the list.
		if len(instanceRows[c.idDB]) == 0 {
			continue
		}
		cidStr := store.FromDB(c.idDB).String()

		var namePtr *string
		if c.name.Valid {
			v := c.name.String
			namePtr = &v
		}

		instances := []clusterInstanceResp{}
		var primaryID *string
		var primaryLastSeen time.Time
		hasPrimary := false
		primaryCount := 0
		livePrimaryCount := 0
		anyDown := false
		standbyCount := 0
		edgeSyncCount := 0
		// The newest sighting at each endpoint. A silent row that a later row
		// has taken over is a predecessor of that instance: it is still listed,
		// but it counts neither as down nor as the primary or a standby.
		newestSeen := map[clusterEndpoint]time.Time{}
		for _, row := range instanceRows[c.idDB] {
			ep := clusterEndpoint{row.addr, row.port}
			if row.lastSeen.After(newestSeen[ep]) {
				newestSeen[ep] = row.lastSeen
			}
		}
		for _, row := range instanceRows[c.idDB] {
			up := isUp(row.lastSeen, now)
			superseded := !up && row.lastSeen.Before(newestSeen[clusterEndpoint{row.addr, row.port}])
			if !up && !superseded {
				anyDown = true
			}
			if row.role == "standby" && !superseded {
				standbyCount++
			}
			if row.role == "primary" && !superseded {
				primaryCount++
				if up {
					livePrimaryCount++
				}
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
			if row.role == "primary" && !superseded && (!hasPrimary || row.lastSeen.After(primaryLastSeen)) {
				s := row.id.String()
				primaryID = &s
				primaryLastSeen = row.lastSeen
				hasPrimary = true
			}
			instances = append(instances, clusterInstanceResp{
				InstanceID: row.id.String(),
				Addr:       row.addr,
				Port:       row.port,
				Role:       row.role,
				PGVersion:  row.pgVersion,
				PermTier:   row.permTier,
				LastSeen:   row.lastSeen,
				Up:         up,
			})
		}

		topology := []topologyEdgeResp{}
		for _, e := range edgeRows[c.idDB] {
			edge := topologyEdgeResp{
				From:       e.from.String(),
				To:         e.to.String(),
				Type:       e.edgeType,
				Confidence: e.confidence,
			}
			if e.syncState.Valid {
				syncState := e.syncState.String
				edge.SyncState = &syncState
				if syncState == "sync" {
					edgeSyncCount++
				}
			}
			if e.confidence == "low" {
				// A low-confidence edge is the standby's last-known upstream while
				// its WAL receiver is not streaming (see wire.Edge.Confidence). The
				// upstream is still a known instance: the pipeline drops an edge it
				// cannot resolve.
				note := "The standby is not streaming from this upstream right now; check connectivity"
				edge.Note = &note
			}
			topology = append(topology, edge)
		}
		summary := syncStates[c.idDB]
		for i := range topology {
			if topology[i].SyncState != nil {
				continue
			}
			if from, perr := uuid.Parse(topology[i].From); perr == nil {
				if state, ok := summary.states[from]; ok {
					topology[i].SyncState = &state
				}
			}
		}
		syncStandbyCount := edgeSyncCount
		if summary.seen {
			syncStandbyCount = summary.synchronous
		}

		var maxReplayLag *float64
		if lag, ok := maxLags[c.idDB]; ok {
			maxReplayLag = &lag
		}

		// Once a live primary exists, primary rows that have gone silent are not
		// counted. A failover leaves the old row role='primary' indefinitely, and
		// counting it kept a failed-over cluster critical. The fallback to every
		// stored row keeps a cluster whose only primary is down degraded, as it
		// has always read. The rule is not strict live-only counting: staleness
		// waits three expected intervals before it stops counting a primary, and
		// a card that gave up after the shorter up threshold would go critical
		// first.
		if livePrimaryCount > 0 {
			primaryCount = livePrimaryCount
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
		// A measured zero is kept: a caught-up standby reports exactly 0, and
		// the spec reads a missing value as "not measured", never as zero.
		if maxReplayLag != nil {
			cr.MaxReplayLagSeconds = maxReplayLag
		}
		if len(topology) > 0 {
			cr.Topology = topology
		}
		resp = append(resp, cr)
	}
	writeJSON(w, http.StatusOK, resp)
}

// loadMaxReplayLags returns, for each cluster in cids that has a reading in the
// last 60 seconds, the largest replay lag of its instances' latest readings.
// Clusters without one are absent from the map.
func loadMaxReplayLags(ctx context.Context, pool dbPool, cids []int64) (map[int64]float64, error) {
	rows, err := pool.Query(ctx, `SELECT cluster_id, MAX(replay_lag_sec) FROM (
			SELECT DISTINCT ON (cluster_id, instance_id, slot_name) cluster_id, replay_lag_sec
			FROM metrics_replication
			WHERE tenant_id='default' AND cluster_id = ANY($1) AND replay_lag_sec IS NOT NULL
			  AND ts > now() - interval '60 seconds'
			ORDER BY cluster_id, instance_id, slot_name, ts DESC
		) latest
		GROUP BY cluster_id`, cids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lags := make(map[int64]float64)
	for rows.Next() {
		var cid int64
		var lag *float64
		if err := rows.Scan(&cid, &lag); err != nil {
			return nil, err
		}
		if lag != nil {
			lags[cid] = *lag
		}
	}
	return lags, rows.Err()
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
	// Point count cap: >10000 points => 422. to.Sub saturates at about 292
	// years, so a longer range is refused rather than cut short. The count is
	// compared as a float: at a 1ns step it is past int range, and converting
	// it first wrapped to a value under the cap.
	duration := to.Sub(from)
	if !from.Add(duration).Equal(to) {
		writeError(w, http.StatusUnprocessableEntity, "range too long", "a range over 292 years cannot be expressed")
		return
	}
	// Use ceil to be conservative: if duration not divisible, need extra bucket.
	pointCount := math.Ceil(float64(duration) / float64(step))
	if pointCount > maxMetricPoints {
		writeError(w, http.StatusUnprocessableEntity, "step too small", fmt.Sprintf("would produce %.0f points, max %d", pointCount, maxMetricPoints))
		return
	}
	points := int(pointCount)
	if points <= 0 {
		points = 1
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
			`SELECT ts, value, series_id FROM metrics WHERE instance_id=$1 AND metric=$2 AND datname=$3 AND ts >= $4 AND ts < $5 ORDER BY ts`,
			iid, metric, database, from, to)
	} else {
		rows, err = a.pool.Query(ctx,
			`SELECT ts, value, series_id FROM metrics WHERE instance_id=$1 AND metric=$2 AND ts >= $3 AND ts < $4 ORDER BY ts`,
			iid, metric, from, to)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed", err.Error())
		return
	}
	defer rows.Close()

	// Map bucket index -> sum and count of its samples. A stored value is the
	// rate since the previous scrape, so a bucket answers with the mean of its
	// samples: the overview multiplies that mean by the bucket width, and keeping
	// only the last scrape counted one interval for the whole bucket.
	bucketSums := make(map[int]float64)
	bucketCounts := make(map[int]int)
	// A mean is only meaningful within one series. Rows of different label
	// sets (pg_io_* by object, context and backend_type, pg_backends by state)
	// share timestamps, so averaging them returned an arbitrary series.
	// This endpoint cannot select a series yet, so a range holding several is
	// refused rather than answered with one of them.
	var firstSeries int64
	haveSeries := false
	for rows.Next() {
		var ts time.Time
		var val float64
		var sid int64
		if scanErr := rows.Scan(&ts, &val, &sid); scanErr != nil {
			writeError(w, http.StatusInternalServerError, "scan failed", scanErr.Error())
			return
		}
		if !haveSeries {
			firstSeries, haveSeries = sid, true
		} else if sid != firstSeries {
			writeError(w, http.StatusBadRequest, "ambiguous metric query", fmt.Sprintf("metric %q has several series in this range, and this endpoint cannot select one", metric))
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
		bucketSums[idx] += val
		bucketCounts[idx]++
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "rows error", err.Error())
		return
	}

	series := make([]metricPoint, 0, len(buckets))
	for i, ts := range buckets {
		if bucketCounts[i] > 0 {
			v := bucketSums[i] / float64(bucketCounts[i])
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
	// Normalize order_by. NULLS LAST: in descending order PostgreSQL puts
	// NULLs first, so statements with no calls in the window (NULL mean)
	// headed the "slowest" list.
	orderClause := "total_exec_time DESC NULLS LAST"
	if orderBy != "" {
		switch strings.ToLower(orderBy) {
		case "calls", "calls_rate", "total_calls":
			orderClause = "total_calls DESC NULLS LAST"
		case "total_exec_time", "total_exec_time_ms", "exec_time", "total_time":
			orderClause = "total_exec_time DESC NULLS LAST"
		case "rows", "rows_rate", "total_rows":
			orderClause = "total_rows DESC NULLS LAST"
		case "shared_blks_hit", "shared_blks_hit_rate":
			orderClause = "total_hit DESC NULLS LAST"
		case "shared_blks_read", "shared_blks_read_rate":
			orderClause = "total_read DESC NULLS LAST"
		case "wal_bytes", "wal_bytes_rate":
			orderClause = "total_wal DESC NULLS LAST"
		case "temp_blks_written", "temp_blks_written_rate":
			orderClause = "total_temp DESC NULLS LAST"
		case "mean_exec_time", "mean_time":
			orderClause = "mean_exec DESC NULLS LAST"
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

	// Without from, the answer covers the hour before to, or before now: the
	// window the UI asks for by default. Summing the statements over the whole
	// retained history cost every such request a scan of all of it.
	if fromPtr == nil {
		start := time.Now()
		if toPtr != nil {
			start = *toPtr
		}
		start = start.Add(-time.Hour)
		fromPtr = &start
	}

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

	// Each row holds per-second rates over sample_seconds, so rate *
	// sample_seconds is what that interval observed and the window's total
	// is their sum. The rates used to be summed as they were, which is off
	// by the scrape interval (60x by default). Rows written before
	// migration 0014 carry no interval; the default stat_statements
	// interval stands in for it.
	//
	// The query text is looked up after aggregating: query_texts keeps one
	// row per PostgreSQL major version, and joining it before the GROUP BY
	// counted every sample once per major version the queryid was ever
	// seen under.
	const seconds = "COALESCE(s.sample_seconds, 60)"
	var sb strings.Builder
	args := []any{}
	sb.WriteString(`
		WITH agg AS (
		SELECT s.cluster_id, s.queryid, s.datname,
		       SUM(s.calls_rate * ` + seconds + `) AS total_calls,
		       SUM(s.exec_time_rate_ms * ` + seconds + `) AS total_exec_time,
		       SUM(s.rows_rate * ` + seconds + `) AS total_rows,
		       SUM(s.shared_blks_hit_rate * ` + seconds + `) AS total_hit,
		       SUM(s.shared_blks_read_rate * ` + seconds + `) AS total_read,
		       SUM(s.wal_bytes_rate * ` + seconds + `) AS total_wal,
		       SUM(s.temp_blks_written_rate * ` + seconds + `) AS total_temp
		FROM metrics_statements s
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
	sb.WriteString(` GROUP BY s.cluster_id, s.queryid, s.datname
		), ranked AS (
		SELECT agg.*,
		       CASE WHEN total_calls > 0 THEN total_exec_time / total_calls END AS mean_exec
		FROM agg
		ORDER BY ` + orderClause)
	fmt.Fprintf(&sb, " LIMIT $%d", argIdx)
	args = append(args, limit)
	sb.WriteString(`
		)
		SELECT r.queryid, r.datname,
		       (SELECT qt.query_text FROM query_texts qt
		         WHERE qt.tenant_id='default' AND qt.cluster_id=r.cluster_id
		           AND qt.datname=r.datname AND qt.queryid=r.queryid
		         ORDER BY qt.last_seen DESC LIMIT 1) AS qtext,
		       r.total_calls, r.total_exec_time, r.total_rows, r.total_hit, r.total_read,
		       r.total_wal, r.total_temp, r.mean_exec
		FROM ranked r
		ORDER BY ` + orderClause)

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
		var totalCalls, totalExec, totalRows, totalHit, totalRead, totalWAL, totalTemp, meanExec *float64
		if scanErr := rows.Scan(&qid, &datname, &qtext, &totalCalls, &totalExec, &totalRows, &totalHit, &totalRead, &totalWAL, &totalTemp, &meanExec); scanErr != nil {
			writeError(w, http.StatusInternalServerError, "scan failed", scanErr.Error())
			return
		}
		text := ""
		if qtext.Valid {
			text = qtext.String
		}
		e := statementEntry{
			QueryID:         strconv.FormatInt(qid, 10),
			Datname:         datname,
			QueryText:       text,
			Calls:           totalCalls,
			TotalExec:       totalExec,
			Rows:            totalRows,
			MeanExec:        meanExec,
			SharedBlksHit:   totalHit,
			SharedBlksRead:  totalRead,
			WALBytes:        totalWAL,
			TempBlksWritten: totalTemp,
			Truncated:       false,
		}
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
	skipReason, err := a.statementsSkipReason(ctx, iid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed", err.Error())
		return
	}
	resp := statementsResp{
		Statements:      entries,
		Truncated:       truncated,
		ComparableScope: "cluster",
		Disabled:        skipReason != nil,
		DisabledReason:  skipReason,
	}
	writeJSON(w, http.StatusOK, resp)
}

// statementsSkipReason returns why the agent skips the stat_statements check
// on this instance, or nil when it does not. The agent reports a skip when
// it schedules its checks, and the first successful result deletes the fact,
// so a present row is the current state.
func (a *API) statementsSkipReason(ctx context.Context, instanceID uuid.UUID) (*string, error) {
	var reason string
	err := a.pool.QueryRow(ctx, `SELECT COALESCE(value_text, '') FROM object_facts
		WHERE tenant_id='default' AND instance_id=$1 AND kind='check_skip' AND key='stat_statements'
		ORDER BY last_seen DESC LIMIT 1`, instanceID).Scan(&reason)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &reason, nil
}

// instanceTruncated reports whether the statements for this instance may be
// incomplete: the agent's hard cardinality cap cut the list during the
// requested window (a "cardinality_truncated" event), or a pg_stat_statements
// result held only the top statements (a check_partial fact). The fact holds
// the current partial episode, from first_seen to last_seen, so it counts only
// when that episode overlaps the window. The flag is instance-wide, not
// per database: the fact is keyed by the target database, but the statements
// are read from one instance-wide view.
func (a *API) instanceTruncated(ctx context.Context, instanceID uuid.UUID, from, to *time.Time) (bool, error) {
	var sb strings.Builder
	args := []any{instanceID}
	sb.WriteString(`SELECT EXISTS(SELECT 1 FROM events WHERE tenant_id='default' AND instance_id=$1 AND type='cardinality_truncated' AND payload->>'check'='stat_statements'`)
	argIdx := 2
	if from != nil {
		// A marker stands for the cap until the next one, which is written up to
		// truncationEventWindow later, so a window that starts after a marker
		// still reads the cap from it.
		fmt.Fprintf(&sb, " AND ts >= $%d", argIdx)
		args = append(args, from.Add(-truncationEventWindow))
		argIdx++
	}
	if to != nil {
		fmt.Fprintf(&sb, " AND ts < $%d", argIdx)
		args = append(args, *to)
		argIdx++
	}
	sb.WriteString(`) OR EXISTS(SELECT 1 FROM object_facts WHERE tenant_id='default' AND instance_id=$1 AND kind='check_partial' AND key='stat_statements'`)
	if from != nil {
		fmt.Fprintf(&sb, " AND last_seen >= $%d", argIdx)
		args = append(args, *from)
		argIdx++
	} else {
		sb.WriteString(" AND last_seen >= now() - interval '1 day'")
	}
	if to != nil {
		fmt.Fprintf(&sb, " AND first_seen < $%d", argIdx)
		args = append(args, *to)
	}
	sb.WriteString(")")
	var truncated bool
	if err := a.pool.QueryRow(ctx, sb.String(), args...).Scan(&truncated); err != nil {
		return false, err
	}
	return truncated, nil
}
