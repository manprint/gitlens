package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/manprint/pglens/internal/redact"
	"github.com/manprint/pglens/internal/sessionorigin"
	"github.com/manprint/pglens/internal/wire"
)

type lockAPIResponse struct {
	InstanceID string            `json:"instance_id"`
	SampledAt  *time.Time        `json:"sampled_at"`
	Stale      bool              `json:"stale"`
	Nodes      []json.RawMessage `json:"nodes"`
	Sessions   []json.RawMessage `json:"sessions"`
	// Connections summarises the sampled sessions. It is absent when no lock
	// tree was ever sampled.
	Connections *connectionsAPI `json:"connections,omitempty"`
}

type activityAPIResponse struct {
	InstanceID string                 `json:"instance_id"`
	Stale      bool                   `json:"stale"`
	Metrics    map[string][]apiMetric `json:"metrics"`
	// XminHorizon is the oldest holder of the xmin horizon in the latest
	// activity sample. It is null when no holder is known from a recent sample.
	XminHorizon *xminHorizonAPI `json:"xmin_horizon"`
}

// xminHorizonAPI is the oldest xmin holder. The identity is a pid, a slot
// name, a standby's application_name or a prepared transaction's gid. It is
// never query text.
type xminHorizonAPI struct {
	HolderKind string    `json:"holder_kind"`
	Identity   string    `json:"identity"`
	AgeXIDs    int64     `json:"age_xids"`
	AgeSeconds *float64  `json:"age_seconds"`
	SampledAt  time.Time `json:"sampled_at"`
}

type apiMetric struct {
	TS     time.Time         `json:"ts"`
	Value  float64           `json:"value"`
	Labels map[string]string `json:"labels"`
}

type databaseAPIResponse struct {
	InstanceID        string             `json:"instance_id"`
	Databases         []databaseAPIEntry `json:"databases"`
	NotMonitoredCount int                `json:"not_monitored_count"`
}

type databaseAPIEntry struct {
	Datname    string  `json:"datname"`
	Monitored  bool    `json:"monitored"`
	SkipReason *string `json:"skip_reason,omitempty"`
}

func contentionStale(sampledAt, now time.Time) bool {
	return now.Sub(sampledAt) > 30*time.Second
}

func (a *API) registerContentionRoutes(r chi.Router) {
	r.Get("/api/v1/locks", a.handleLocks)
	r.Get("/api/v1/instances/{id}/activity", a.handleActivity)
	r.Get("/api/v1/instances/{id}/databases", a.handleDatabases)
}

func (a *API) handleLocks(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.URL.Query().Get("instance_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", "query param instance_id is required and must be a UUID")
		return
	}
	resp := lockAPIResponse{
		InstanceID: id.String(),
		Stale:      true,
		Nodes:      []json.RawMessage{},
		Sessions:   []json.RawMessage{},
	}
	if a.pool != nil {
		var ts time.Time
		var raw []byte
		err = a.pool.QueryRow(r.Context(), `SELECT ts, tree FROM lock_snapshots WHERE tenant_id='default' AND instance_id=$1`, id).Scan(&ts, &raw)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "query failed", err.Error())
			return
		}
		if err == nil {
			resp.SampledAt = &ts
			resp.Stale = contentionStale(ts, time.Now())
			var tree struct {
				Nodes             []json.RawMessage `json:"nodes"`
				Sessions          []json.RawMessage `json:"sessions"`
				SessionsTruncated bool              `json:"sessions_truncated"`
			}
			// A tree that does not decode is not an empty tree: answering it as a
			// fresh, empty result would show no contention as current data.
			if err := json.Unmarshal(raw, &tree); err != nil {
				log.Printf("decode lock tree of instance %s: %v", id, err)
				writeError(w, http.StatusInternalServerError, "lock tree unreadable", err.Error())
				return
			}
			// A tree that omits a list, or stores null, keeps the empty list the
			// response promises instead of encoding null.
			if tree.Nodes != nil {
				resp.Nodes = tree.Nodes
			}
			if tree.Sessions != nil {
				resp.Sessions = tree.Sessions
			}
			if err := a.describeSessions(r.Context(), id, &resp, tree.SessionsTruncated); err != nil {
				log.Printf("describe sessions of instance %s: %v", id, err)
				writeError(w, http.StatusInternalServerError, "query failed", err.Error())
				return
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) handleActivity(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "invalid instance_id", err.Error())
		return
	}
	resp := activityAPIResponse{InstanceID: id.String(), Stale: true, Metrics: map[string][]apiMetric{}}
	if a.pool == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	// The connection totals, the saturation ratio and the oldest open and
	// idle-in-transaction ages were missing from this list, so the Activity
	// section showed them as never reported although the check sends them on
	// every scrape. pg_deadlocks_total (a per-second rate once stored) comes
	// from database_stats, every 30 seconds: its window is wider, or half the
	// reads missed it. A failed query was an empty, stale-looking answer.
	rows, err := a.pool.Query(r.Context(), `SELECT ts, metric, labels, value FROM metrics
		WHERE tenant_id='default' AND instance_id=$1
		  AND ((ts >= now() - interval '30 seconds' AND metric IN (
		         'pg_connections_used','pg_connections_limit','pg_connections_used_ratio',
		         'pg_connections_by_database','pg_connections_by_application','pg_backends',
		         'pg_connections_by_origin','pg_connections_by_user','pg_connections_by_client',
		         'pg_max_xact_age_seconds','pg_max_idle_in_transaction_seconds','pg_max_state_age_seconds',
		         'pg_prepared_xacts','pg_oldest_prepared_xact_seconds','pg_max_datfrozenxid_age',
		         'pg_xmin_horizon_age_xids'))
		    OR (ts >= now() - interval '90 seconds' AND metric = 'pg_deadlocks_total'))
		ORDER BY ts DESC`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed", err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var m apiMetric
		var name string
		var labels []byte
		if err := rows.Scan(&m.TS, &name, &labels, &m.Value); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed", err.Error())
			return
		}
		if err := json.Unmarshal(labels, &m.Labels); err != nil {
			log.Printf("decode labels of metric %s for instance %s: %v", name, id, err)
		}
		resp.Metrics[name] = append(resp.Metrics[name], m)
		resp.Stale = false
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "rows error", err.Error())
		return
	}
	holder, err := a.oldestXminHolder(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed", err.Error())
		return
	}
	resp.XminHorizon = holder
	writeJSON(w, http.StatusOK, resp)
}

// oldestXminHolder reads the xmin_horizon fact that the activity check writes
// with every sample. It answers nil when no holder fact is fresh: the fact is
// replaced by each sample, so one older than the activity window is no longer
// the instance's holder. A fact that does not decode is logged and left out of
// the answer, which the stored value of a fact the server wrote cannot cause.
func (a *API) oldestXminHolder(ctx context.Context, id uuid.UUID) (*xminHorizonAPI, error) {
	var raw []byte
	var seen time.Time
	err := a.pool.QueryRow(ctx, `SELECT value_json, last_seen FROM object_facts
		WHERE tenant_id='default' AND instance_id=$1 AND kind='xmin_horizon' AND key='oldest'
		  AND last_seen >= now() - interval '30 seconds'`, id).Scan(&raw, &seen)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var holder xminHorizonAPI
	if err := json.Unmarshal(raw, &holder); err != nil {
		log.Printf("decode xmin horizon of instance %s: %v", id, err)
		return nil, nil
	}
	holder.SampledAt = seen
	return &holder, nil
}

func (a *API) handleDatabases(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "invalid instance_id", err.Error())
		return
	}
	resp := databaseAPIResponse{InstanceID: id.String(), Databases: []databaseAPIEntry{}}
	if a.pool == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	rows, err := a.pool.Query(r.Context(), `SELECT datname, monitored, skip_reason FROM databases WHERE instance_id=$1 ORDER BY datname`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed", err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var d databaseAPIEntry
		if err := rows.Scan(&d.Datname, &d.Monitored, &d.SkipReason); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed", err.Error())
			return
		}
		resp.Databases = append(resp.Databases, d)
		if !d.Monitored {
			resp.NotMonitoredCount++
		}
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "rows error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

const (
	// queryTextNormalised is the source of a session text resolved from
	// query_texts by query_id; queryTextRaw is the text the agent sent.
	queryTextNormalised = "normalised"
	queryTextRaw        = "raw"
	// maxSessionTextLookups bounds the (database, query id) pairs one request
	// resolves; the agent's session sample is capped at 100.
	maxSessionTextLookups = 100
)

// connectionCount is one row of a connection summary.
type connectionCount struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

// connectionGroups holds the six groupings of the summary, each sorted by
// count descending then key.
type connectionGroups struct {
	ByOrigin      []connectionCount `json:"by_origin"`
	ByApplication []connectionCount `json:"by_application"`
	ByUser        []connectionCount `json:"by_user"`
	ByClient      []connectionCount `json:"by_client"`
	ByDatabase    []connectionCount `json:"by_database"`
	ByState       []connectionCount `json:"by_state"`
}

// connectionsAPI is the connection summary of the lock sample. The groups are
// counted from the very sessions the response lists, so the tables and the
// summary cannot disagree. When the sample was truncated, Totals carries the
// counts of the latest activity scrape, which cover every client backend.
type connectionsAPI struct {
	ByApplicationEnabled bool `json:"by_application_enabled"`
	Truncated            bool `json:"truncated"`
	connectionGroups
	Totals *connectionGroups `json:"totals,omitempty"`
}

// sessionFacts are the fields of a sampled session the server reads.
type sessionFacts struct {
	fields        map[string]json.RawMessage
	app, user, db string
	client, state string
	rawText       string
	queryID       int64
	hasQueryID    bool
}

func rawString(fields map[string]json.RawMessage, key string) string {
	var s string
	if raw, ok := fields[key]; ok && json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// describeSessions adds origin, query_text and query_text_source to every
// sampled session and the connection summary to the response. Fields it owns
// are removed from the stored session first, so an agent cannot set them. A
// session that is not a JSON object is kept as it came and left out of the
// summary. Fields an older agent does not send are simply omitted.
func (a *API) describeSessions(ctx context.Context, id uuid.UUID, resp *lockAPIResponse, truncated bool) error {
	facts := make([]*sessionFacts, len(resp.Sessions))
	for i, raw := range resp.Sessions {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
			log.Printf("session %d of instance %s is not an object", i, id)
			continue
		}
		delete(fields, "origin")
		delete(fields, "query_text")
		delete(fields, "query_text_source")
		f := &sessionFacts{fields: fields}
		f.app = rawString(fields, "application_name")
		f.user = rawString(fields, "usename")
		f.db = rawString(fields, "datname")
		f.client = rawString(fields, "client_addr")
		f.state = rawString(fields, "state")
		f.rawText = rawString(fields, "query")
		if text := rawString(fields, "query_id"); text != "" && f.rawText == "" {
			if v, err := strconv.ParseInt(text, 10, 64); err == nil {
				f.queryID, f.hasQueryID = v, true
			}
		}
		facts[i] = f
	}

	role := ""
	if len(resp.Sessions) > 0 || len(resp.Nodes) > 0 {
		var err error
		if role, err = a.monitoringRole(ctx, id); err != nil {
			return err
		}
	}
	texts, err := a.normalisedTexts(ctx, id, facts)
	if err != nil {
		return err
	}
	// Blocking-tree nodes get the same origin, so the UI never derives it.
	for i, raw := range resp.Nodes {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
			continue
		}
		delete(fields, "origin")
		fields["origin"] = mustMarshal(sessionorigin.Origin(rawString(fields, "application_name"), rawString(fields, "usename"), role))
		out, err := json.Marshal(fields)
		if err != nil {
			return err
		}
		resp.Nodes[i] = out
	}

	groups := map[string]map[string]int64{}
	count := func(group, key string) {
		if groups[group] == nil {
			groups[group] = map[string]int64{}
		}
		groups[group][key]++
	}
	// Both origins are always listed, zero included.
	groups["origin"] = map[string]int64{sessionorigin.Application: 0, sessionorigin.Pglens: 0}
	for i, f := range facts {
		if f == nil {
			continue
		}
		origin := sessionorigin.Origin(f.app, f.user, role)
		f.fields["origin"] = mustMarshal(origin)
		switch {
		case f.rawText != "":
			f.fields["query_text"] = mustMarshal(f.rawText)
			f.fields["query_text_source"] = mustMarshal(queryTextRaw)
		case f.hasQueryID:
			if text, ok := texts[textKey{f.db, f.queryID}]; ok {
				f.fields["query_text"] = mustMarshal(text)
				f.fields["query_text_source"] = mustMarshal(queryTextNormalised)
			}
		}
		out, err := json.Marshal(f.fields)
		if err != nil {
			return err
		}
		resp.Sessions[i] = out

		count("origin", origin)
		count("application", f.app)
		count("user", f.user)
		client := f.client
		if client == "" {
			client = "local"
		}
		count("client", client)
		count("database", f.db)
		state := f.state
		if state == "" {
			state = "unknown"
		}
		count("state", state)
	}

	enabled, err := a.byApplicationEnabled(ctx, id)
	if err != nil {
		return err
	}
	conn := &connectionsAPI{ByApplicationEnabled: enabled, Truncated: truncated}
	conn.ByOrigin = sortedCounts(groups["origin"])
	conn.ByApplication = sortedCounts(groups["application"])
	conn.ByUser = sortedCounts(groups["user"])
	conn.ByClient = sortedCounts(groups["client"])
	conn.ByDatabase = sortedCounts(groups["database"])
	conn.ByState = sortedCounts(groups["state"])
	if truncated {
		totals, err := a.connectionTotals(ctx, id)
		if err != nil {
			return err
		}
		conn.Totals = totals
	}
	resp.Connections = conn
	return nil
}

func mustMarshal(v string) json.RawMessage {
	raw, _ := json.Marshal(v) // a string always marshals
	return raw
}

func sortedCounts(m map[string]int64) []connectionCount {
	out := make([]connectionCount, 0, len(m))
	for k, v := range m {
		out = append(out, connectionCount{Key: k, Count: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// monitoringRole is the role label of the instance's latest monitoring_role
// fact. Empty when the instance has none: only the application-name rule then
// classifies its sessions.
func (a *API) monitoringRole(ctx context.Context, id uuid.UUID) (string, error) {
	var role *string
	err := a.pool.QueryRow(ctx, `SELECT labels->>'role' FROM object_facts
		WHERE tenant_id='default' AND instance_id=$1 AND kind='monitoring_role' AND key=$2
		ORDER BY last_seen DESC LIMIT 1`, id, "self").Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if role == nil {
		return "", nil
	}
	return *role, nil
}

type textKey struct {
	datname string
	queryID int64
}

// normalisedTexts resolves the sessions that carry a query_id and no raw text
// with one query over their distinct (database, query id) pairs, at most
// maxSessionTextLookups. query_texts is keyed by cluster, database, query id and
// major version, so the newest last_seen wins when a statement was seen under
// two majors. A pair with no row has no text.
func (a *API) normalisedTexts(ctx context.Context, id uuid.UUID, facts []*sessionFacts) (map[textKey]string, error) {
	seen := map[textKey]bool{}
	var names []string
	var ids []int64
	for _, f := range facts {
		if f == nil || !f.hasQueryID {
			continue
		}
		k := textKey{f.db, f.queryID}
		if seen[k] || len(names) >= maxSessionTextLookups {
			continue
		}
		seen[k] = true
		names = append(names, f.db)
		ids = append(ids, f.queryID)
	}
	out := map[textKey]string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := a.pool.Query(ctx, `SELECT DISTINCT ON (qt.datname, qt.queryid) qt.datname, qt.queryid, qt.query_text
		FROM query_texts qt
		JOIN unnest($2::text[], $3::bigint[]) AS want(datname, queryid)
		  ON want.datname = qt.datname AND want.queryid = qt.queryid
		WHERE qt.tenant_id='default'
		  AND qt.cluster_id = (SELECT ls.cluster_id FROM lock_snapshots ls WHERE ls.tenant_id='default' AND ls.instance_id=$1)
		ORDER BY qt.datname, qt.queryid, qt.last_seen DESC`, id, names, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var datname, text string
		var queryID int64
		if err := rows.Scan(&datname, &queryID, &text); err != nil {
			return nil, err
		}
		if redact.MayHoldCredential(text) {
			text = redact.Withheld
		}
		out[textKey{datname, queryID}] = text
	}
	return out, rows.Err()
}

// byApplicationEnabled says whether per-application counts are collected: the
// agent_flags fact says so when it names the switch, otherwise the latest
// activity scrape does by carrying a pg_connections_by_application metric.
func (a *API) byApplicationEnabled(ctx context.Context, id uuid.UUID) (bool, error) {
	var raw []byte
	err := a.pool.QueryRow(ctx, `SELECT value_json FROM object_facts
		WHERE tenant_id='default' AND instance_id=$1 AND kind=$2 AND key=$3
		ORDER BY last_seen DESC LIMIT 1`, id, wire.AgentFlagsFact, wire.AgentFlagsKey).Scan(&raw)
	switch {
	case err == nil:
		var flags wire.AgentFlags
		if jsonErr := json.Unmarshal(raw, &flags); jsonErr != nil {
			log.Printf("decode agent flags of instance %s: %v", id, jsonErr)
		} else if flags.ByApplication != nil {
			return *flags.ByApplication, nil
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return false, err
	}
	var present bool
	err = a.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM metrics
		WHERE tenant_id='default' AND instance_id=$1 AND metric='pg_connections_by_application'
		  AND ts >= now() - interval '30 seconds')`, id).Scan(&present)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return present, err
}

// connectionTotalMetrics maps each pg_connections_* metric the totals read to
// its label and the group it fills.
var connectionTotalMetrics = map[string]struct{ label, group string }{
	"pg_connections_by_origin":      {"origin", "origin"},
	"pg_connections_by_application": {"application_name", "application"},
	"pg_connections_by_user":        {"usename", "user"},
	"pg_connections_by_client":      {"client_addr", "client"},
	"pg_connections_by_database":    {"datname", "database"},
	"pg_backends":                   {"state", "state"},
}

// connectionTotals reads the latest activity scrape: the counts over every
// client backend, which a truncated session sample cannot give. Each group
// keeps the bound the agent applies (top 10 and other).
func (a *API) connectionTotals(ctx context.Context, id uuid.UUID) (*connectionGroups, error) {
	names := make([]string, 0, len(connectionTotalMetrics))
	for name := range connectionTotalMetrics {
		names = append(names, name)
	}
	sort.Strings(names)
	rows, err := a.pool.Query(ctx, `SELECT DISTINCT ON (metric, labels) metric, labels, value FROM metrics
		WHERE tenant_id='default' AND instance_id=$1 AND metric = ANY($2)
		  AND ts >= now() - interval '30 seconds'
		ORDER BY metric, labels, ts DESC`, id, names)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := map[string]map[string]int64{}
	for rows.Next() {
		var name string
		var labels []byte
		var value float64
		if err := rows.Scan(&name, &labels, &value); err != nil {
			return nil, err
		}
		spec, ok := connectionTotalMetrics[name]
		if !ok {
			continue
		}
		var l map[string]string
		if err := json.Unmarshal(labels, &l); err != nil {
			log.Printf("decode labels of metric %s for instance %s: %v", name, id, err)
			continue
		}
		if groups[spec.group] == nil {
			groups[spec.group] = map[string]int64{}
		}
		groups[spec.group][l[spec.label]] += int64(value + 0.5)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &connectionGroups{
		ByOrigin:      sortedCounts(groups["origin"]),
		ByApplication: sortedCounts(groups["application"]),
		ByUser:        sortedCounts(groups["user"]),
		ByClient:      sortedCounts(groups["client"]),
		ByDatabase:    sortedCounts(groups["database"]),
		ByState:       sortedCounts(groups["state"]),
	}, nil
}
