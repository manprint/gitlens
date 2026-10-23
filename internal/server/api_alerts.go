package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/manprint/pglens/internal/alert"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/store"
)

// AlertAPI exposes the alert catalogue and the operator-managed alert state.
type AlertAPI struct {
	pool  dbPool
	store alert.Store
}

func NewAlertAPI(pool *pgxpool.Pool, store alert.Store) *AlertAPI {
	return &AlertAPI{pool: asDBPool(pool), store: store}
}

func newAlertAPIForTest(pool dbPool, store alert.Store) *AlertAPI {
	return &AlertAPI{pool: pool, store: store}
}

func (a *AlertAPI) RegisterRoutes(r chi.Router) {
	r.Get("/api/v1/alerts", a.listAlerts)
	r.Get("/api/v1/alerts/{alert_key}", a.alertDetail)
	r.Get("/api/v1/alert-rules", a.listRules)
	r.Put("/api/v1/alert-rules/{rule_id}", a.updateRule)
	r.Get("/api/v1/silences", a.listSilences)
	r.Post("/api/v1/silences", a.createSilence)
	r.Delete("/api/v1/silences/{id}", a.deleteSilence)
}

type alertJSON struct {
	Key        string            `json:"alert_key"`
	RuleID     string            `json:"rule_id"`
	Severity   alert.Severity    `json:"severity"`
	State      alert.State       `json:"state"`
	ClusterID  *string           `json:"cluster_id"`
	InstanceID *string           `json:"instance_id"`
	Datname    string            `json:"datname"`
	Labels     map[string]string `json:"labels"`
	Value      float64           `json:"value"`
	Summary    string            `json:"summary"`
	StartedAt  time.Time         `json:"started_at"`
	LastEvalAt time.Time         `json:"last_eval_at"`
	ResolvedAt *time.Time        `json:"resolved_at"`
	Suppressed bool              `json:"suppressed"`
}

type alertRuleJSON struct {
	ID         string         `json:"id"`
	Severity   alert.Severity `json:"severity"`
	Scope      alert.Scope    `json:"scope"`
	Needs      []string       `json:"needs"`
	MinTier    string         `json:"min_tier"`
	Enabled    bool           `json:"enabled"`
	Threshold  float64        `json:"threshold"`
	ForSeconds int            `json:"for_seconds"`
}

func renderAlertRule(v alert.Rule) alertRuleJSON {
	needs := make([]string, 0, 1)
	if v.Metric != "" {
		needs = append(needs, v.Metric)
	}
	if v.EventType != "" {
		needs = append(needs, v.EventType)
	}
	return alertRuleJSON{
		ID:         v.ID,
		Severity:   v.Severity,
		Scope:      v.Scope,
		Needs:      needs,
		MinTier:    "T" + strconv.Itoa(int(v.Tier)),
		Enabled:    v.Enabled,
		Threshold:  v.Threshold,
		ForSeconds: int(v.For / time.Second),
	}
}

func renderAlert(v alert.Alert) alertJSON {
	o := alertJSON{Key: v.Key, RuleID: v.RuleID, Severity: v.Severity, State: v.State, Datname: v.Datname, Labels: v.Labels, Value: v.Value, Summary: v.Summary, StartedAt: v.StartedAt, LastEvalAt: v.LastEvalAt, ResolvedAt: v.ResolvedAt, Suppressed: v.Suppressed}
	// The spec makes labels a required object, and a nil map encodes as null.
	// Rows written while the store still stored a nil map come back as nil, so
	// the empty object is filled in here rather than only on write.
	if o.Labels == nil {
		o.Labels = map[string]string{}
	}
	// The column holds the uint64 system identifier's bit pattern: render it
	// unsigned, like every other endpoint (ClusterId in the spec). It was
	// rendered signed, so an identifier above 2^63 did not match its cluster.
	if v.ClusterID != nil {
		s := store.FromDB(*v.ClusterID).String()
		o.ClusterID = &s
	}
	if v.InstanceID != nil {
		s := v.InstanceID.String()
		o.InstanceID = &s
	}
	return o
}

func (a *AlertAPI) activeSilences(ctx context.Context) ([]alert.Silence, error) {
	if a.pool == nil {
		return nil, nil
	}
	rows, err := a.pool.Query(ctx, `SELECT silence_id,matchers,reason,starts_at,ends_at FROM silences WHERE starts_at <= now() AND ends_at > now()`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []alert.Silence
	for rows.Next() {
		var s alert.Silence
		var raw []byte
		if err := rows.Scan(&s.ID, &raw, &s.Reason, &s.StartsAt, &s.EndsAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &s.Matchers); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (a *AlertAPI) listAlerts(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		writeError(w, http.StatusServiceUnavailable, "alerts unavailable", "alert store is not configured")
		return
	}
	f := alert.Filter{State: alert.State(r.URL.Query().Get("state")), Severity: alert.Severity(r.URL.Query().Get("severity")), RuleID: r.URL.Query().Get("rule_id"), InstanceID: r.URL.Query().Get("instance_id")}
	// Without a state the store answers every active alert, pending and firing.
	// A pending filter matched nothing, because the engine does not persist
	// pending episodes yet.
	if state := r.URL.Query().Get("state"); state == "all" {
		f.State = ""
	}
	if f.State != "" && f.State != alert.StatePending && f.State != alert.StateFiring && f.State != alert.StateResolved {
		writeError(w, http.StatusBadRequest, "invalid state", "state must be pending, firing, resolved, or all")
		return
	}
	if v := r.URL.Query().Get("cluster_id"); v != "" {
		cid, err := pgtype.ParseClusterID(v)
		if err != nil {
			writeError(w, 400, "invalid cluster_id", err.Error())
			return
		}
		n := store.ToDB(cid)
		f.ClusterID = &n
	}
	rows, err := a.store.Active(r.Context(), f)
	if err != nil {
		writeError(w, 500, "alert query failed", err.Error())
		return
	}
	silences, err := a.activeSilences(r.Context())
	if err != nil {
		writeError(w, 500, "silence query failed", err.Error())
		return
	}
	if len(rows) > 500 {
		rows = rows[:500]
	}
	out := make([]alertJSON, 0, len(rows))
	for _, v := range rows {
		v.Suppressed = alert.FirstMatch(silences, v, time.Now()) != nil
		out = append(out, renderAlert(v))
	}
	writeJSON(w, 200, out)
}

func (a *AlertAPI) listRules(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		writeError(w, 503, "alert store unavailable", "")
		return
	}
	stored, err := a.store.Rules(r.Context())
	if err != nil {
		writeError(w, 500, "rule query failed", err.Error())
		return
	}
	all := append(alert.Builtin(), stored...)
	out := make([]alertRuleJSON, 0, len(all))
	for _, v := range all {
		out = append(out, renderAlertRule(v))
	}
	writeJSON(w, 200, out)
}

type ruleUpdate struct {
	Enabled    *bool           `json:"enabled"`
	Severity   *alert.Severity `json:"severity"`
	Threshold  *float64        `json:"threshold"`
	ForSeconds *int            `json:"for_seconds"`
}

func (a *AlertAPI) updateRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "rule_id")
	for _, v := range alert.Builtin() {
		if v.ID == id {
			writeError(w, 409, "tier 0 rule cannot be changed", id)
			return
		}
	}
	var in ruleUpdate
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, 400, "invalid body", err.Error())
		return
	}
	if in.Severity != nil && *in.Severity != "critical" && *in.Severity != "warning" && *in.Severity != "info" {
		writeError(w, 400, "invalid rule", "unknown severity")
		return
	}
	if in.ForSeconds != nil && *in.ForSeconds < 0 {
		writeError(w, 400, "invalid rule", "for_seconds must be non-negative")
		return
	}
	if a.pool == nil {
		writeError(w, 503, "alert store unavailable", "")
		return
	}
	// One statement reads and writes the row. COALESCE keeps every column the
	// body left out, and a request that waits on the row lock re-evaluates the
	// SET list against the version the other edit committed. Reading the row
	// first and writing every column back let a concurrent edit to another
	// field revert.
	var enabled bool
	var severity alert.Severity
	var threshold *float64
	var seconds int
	row := a.pool.QueryRow(r.Context(), `UPDATE alert_rules SET enabled=COALESCE($1::boolean,enabled),severity=COALESCE($2::text,severity),threshold=COALESCE($3::double precision,threshold),for_seconds=COALESCE($4::integer,for_seconds),updated_at=now() WHERE rule_id=$5 RETURNING enabled,severity,threshold,for_seconds`, in.Enabled, in.Severity, in.Threshold, in.ForSeconds, id)
	if err := row.Scan(&enabled, &severity, &threshold, &seconds); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, 404, "rule not found", id)
		} else {
			writeError(w, 500, "rule update failed", err.Error())
		}
		return
	}
	writeJSON(w, 200, map[string]any{"rule_id": id, "enabled": enabled, "severity": severity, "threshold": threshold, "for_seconds": seconds})
}

type silenceJSON struct {
	ID       uuid.UUID       `json:"silence_id"`
	Matchers []alert.Matcher `json:"matchers"`
	Reason   string          `json:"reason"`
	StartsAt time.Time       `json:"starts_at"`
	EndsAt   time.Time       `json:"ends_at"`
}

func (a *AlertAPI) listSilences(w http.ResponseWriter, r *http.Request) {
	if a.pool == nil {
		writeError(w, 503, "alert store unavailable", "")
		return
	}
	q := `SELECT silence_id,matchers,reason,starts_at,ends_at FROM silences`
	if r.URL.Query().Get("all") != "true" {
		q += ` WHERE starts_at <= now() AND ends_at > now()`
	}
	q += ` ORDER BY ends_at`
	rows, err := a.pool.Query(r.Context(), q)
	if err != nil {
		writeError(w, 500, "silence query failed", err.Error())
		return
	}
	defer rows.Close()
	out := []silenceJSON{}
	for rows.Next() {
		var s silenceJSON
		var raw []byte
		if err := rows.Scan(&s.ID, &raw, &s.Reason, &s.StartsAt, &s.EndsAt); err != nil {
			writeError(w, 500, "silence decode failed", err.Error())
			return
		}
		if err := json.Unmarshal(raw, &s.Matchers); err != nil {
			writeError(w, 500, "silence decode failed", err.Error())
			return
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		writeError(w, 500, "silence query failed", err.Error())
		return
	}
	writeJSON(w, 200, out)
}
func (a *AlertAPI) createSilence(w http.ResponseWriter, r *http.Request) {
	var s silenceJSON
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		writeError(w, 400, "invalid body", err.Error())
		return
	}
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	z := alert.Silence{ID: s.ID, Matchers: s.Matchers, Reason: s.Reason, StartsAt: s.StartsAt, EndsAt: s.EndsAt}
	if err := z.Validate(); err != nil {
		writeError(w, 400, "invalid silence", err.Error())
		return
	}
	if a.pool == nil {
		writeError(w, 503, "alert store unavailable", "")
		return
	}
	raw, _ := json.Marshal(s.Matchers)
	if _, err := a.pool.Exec(r.Context(), `INSERT INTO silences (silence_id,matchers,reason,starts_at,ends_at) VALUES ($1,$2,$3,$4,$5)`, s.ID, raw, s.Reason, s.StartsAt, s.EndsAt); err != nil {
		writeError(w, 500, "silence create failed", err.Error())
		return
	}
	writeJSON(w, 201, s)
}
func (a *AlertAPI) deleteSilence(w http.ResponseWriter, r *http.Request) {
	if a.pool == nil {
		writeError(w, 503, "alert store unavailable", "")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "invalid silence id", err.Error())
		return
	}
	tag, err := a.pool.Exec(r.Context(), `UPDATE silences SET ends_at=now() WHERE silence_id=$1`, id)
	if err != nil {
		writeError(w, 500, "silence delete failed", err.Error())
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, 404, "silence not found", id.String())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *AlertAPI) alertDetail(w http.ResponseWriter, r *http.Request) {
	if a.pool == nil {
		writeError(w, 503, "alert store unavailable", "")
		return
	}
	key := chi.URLParam(r, "alert_key")
	// Keys contain '/' separators, so the client sends one percent-encoded path
	// segment. chi routes on the escaped path and hands the segment back still
	// encoded, which matched no row and 404ed every alert. Decode it once, and
	// only when the escaped path was routed: without RawPath chi already used
	// the decoded path, and decoding again would corrupt a key with a literal '%'.
	if r.URL.RawPath != "" {
		decoded, err := url.PathUnescape(key)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid alert key", err.Error())
			return
		}
		key = decoded
	}
	var v alert.Alert
	var raw []byte
	var cluster *int64
	var iid *uuid.UUID
	err := a.pool.QueryRow(r.Context(), `SELECT alert_key,rule_id,severity,state,cluster_id,instance_id,datname,labels,value,summary,started_at,last_eval_at,resolved_at FROM alerts WHERE alert_key=$1 ORDER BY started_at DESC LIMIT 1`, key).Scan(&v.Key, &v.RuleID, &v.Severity, &v.State, &cluster, &iid, &v.Datname, &raw, &v.Value, &v.Summary, &v.StartedAt, &v.LastEvalAt, &v.ResolvedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "alert not found", key)
		return
	}
	if err != nil {
		writeError(w, 500, "alert query failed", err.Error())
		return
	}
	v.ClusterID = cluster
	v.InstanceID = iid
	if err := json.Unmarshal(raw, &v.Labels); err != nil {
		writeError(w, 500, "alert decode failed", err.Error())
		return
	}
	silences, err := a.activeSilences(r.Context())
	if err != nil {
		writeError(w, 500, "silence query failed", err.Error())
		return
	}
	v.Suppressed = alert.FirstMatch(silences, v, time.Now()) != nil
	writeJSON(w, 200, renderAlert(v))
}
