package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/advisor"
)

type findingJSON struct {
	FindingID      string          `json:"finding_id"`
	RuleID         string          `json:"rule_id"`
	Severity       string          `json:"severity"`
	State          string          `json:"state"`
	Scope          string          `json:"scope"`
	ClusterID      *int64          `json:"cluster_id,omitempty"`
	InstanceID     *uuid.UUID      `json:"instance_id,omitempty"`
	Datname        string          `json:"datname"`
	ObjectName     string          `json:"object_name"`
	Title          string          `json:"title"`
	Detail         string          `json:"detail"`
	Remediation    string          `json:"remediation"`
	Evidence       json.RawMessage `json:"evidence"`
	DegradedReason *string         `json:"degraded_reason,omitempty"`
	FirstSeen      time.Time       `json:"first_seen"`
	LastSeen       time.Time       `json:"last_seen"`
	ResolvedAt     *time.Time      `json:"resolved_at,omitempty"`
	MutedUntil     *time.Time      `json:"muted_until,omitempty"`
	MuteReason     *string         `json:"mute_reason,omitempty"`
}

func (a *API) registerFindingsRoutes(r chi.Router) {
	r.Get("/api/v1/findings", a.listFindings)
	// Finding IDs are rule_id/subject and therefore may contain slashes. Chi's
	// catch-all route preserves that stable ID while allowing the mute action
	// to remain a suffix of the same resource path.
	r.Post("/api/v1/findings/*", a.muteFinding)
	r.Delete("/api/v1/findings/*", a.unmuteFinding)
	r.Get("/api/v1/findings/*", a.getFinding)
	r.Get("/api/v1/advisor/rules", a.listAdvisorRules)
}

func findingPathID(r *http.Request, action string) string {
	id := chi.URLParam(r, "id")
	if id == "" {
		id = strings.TrimPrefix(chi.URLParam(r, "*"), "/")
	}
	if action != "" {
		id = strings.TrimSuffix(id, "/"+action)
	}
	if decoded, err := url.PathUnescape(id); err == nil {
		id = decoded
	}
	return id
}

const findingSelect = `finding_id,rule_id,severity,state,scope,cluster_id,instance_id,datname,object_name,title,detail,remediation,evidence,degraded_reason,first_seen,last_seen,resolved_at,muted_until,mute_reason`

type findingScanner interface{ Scan(...any) error }

func scanFinding(row findingScanner) (findingJSON, error) {
	var f findingJSON
	var evidence []byte
	err := row.Scan(&f.FindingID, &f.RuleID, &f.Severity, &f.State, &f.Scope, &f.ClusterID, &f.InstanceID, &f.Datname, &f.ObjectName, &f.Title, &f.Detail, &f.Remediation, &evidence, &f.DegradedReason, &f.FirstSeen, &f.LastSeen, &f.ResolvedAt, &f.MutedUntil, &f.MuteReason)
	f.Evidence = json.RawMessage(evidence)
	if len(f.Evidence) == 0 {
		f.Evidence = json.RawMessage(`{}`)
	}
	return f, err
}

func (a *API) listFindings(w http.ResponseWriter, r *http.Request) {
	if a.pool == nil {
		writeError(w, http.StatusServiceUnavailable, "findings unavailable", "database pool is not configured")
		return
	}
	args := []any{}
	where := []string{"tenant_id='default'"}
	state := r.URL.Query().Get("state")
	if state == "" {
		where = append(where, "state IN ('open','degraded')")
	} else if state != "all" {
		states := strings.Split(state, ",")
		for _, v := range states {
			if v != "open" && v != "degraded" && v != "muted" && v != "resolved" {
				writeError(w, 400, "invalid state", v)
				return
			}
		}
		where = append(where, "state = ANY($"+strconv.Itoa(len(args)+1)+")")
		args = append(args, states)
	}
	for _, filter := range []struct{ key, column string }{{"severity", "severity"}, {"rule_id", "rule_id"}, {"datname", "datname"}, {"scope", "scope"}, {"instance_id", "instance_id"}, {"cluster_id", "cluster_id"}} {
		if value := r.URL.Query().Get(filter.key); value != "" {
			var v any = value
			if filter.key == "instance_id" {
				id, err := uuid.Parse(value)
				if err != nil {
					writeError(w, 400, "invalid instance_id", err.Error())
					return
				}
				v = id
			}
			if filter.key == "cluster_id" {
				id, err := strconv.ParseInt(value, 10, 64)
				if err != nil {
					writeError(w, 400, "invalid cluster_id", err.Error())
					return
				}
				v = id
			}
			args = append(args, v)
			where = append(where, filter.column+"=$"+strconv.Itoa(len(args)))
		}
	}
	limit, err := parseLimitParam(r.URL.Query().Get("limit"), 100, 1000)
	if err != nil {
		writeError(w, 400, "invalid limit", err.Error())
		return
	}
	args = append(args, limit)
	q := "SELECT " + findingSelect + " FROM findings WHERE " + strings.Join(where, " AND ") + " ORDER BY CASE severity WHEN 'critical' THEN 0 WHEN 'warning' THEN 1 ELSE 2 END, last_seen DESC LIMIT $" + strconv.Itoa(len(args))
	rows, err := a.pool.Query(r.Context(), q, args...)
	if err != nil {
		writeError(w, 500, "finding query failed", err.Error())
		return
	}
	defer rows.Close()
	out := []findingJSON{}
	for rows.Next() {
		f, err := scanFinding(rows)
		if err != nil {
			writeError(w, 500, "finding scan failed", err.Error())
			return
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		writeError(w, 500, "finding query failed", err.Error())
		return
	}
	writeJSON(w, 200, out)
}

func (a *API) getFinding(w http.ResponseWriter, r *http.Request) {
	if a.pool == nil {
		writeError(w, 503, "findings unavailable", "database pool is not configured")
		return
	}
	id := findingPathID(r, "")
	f, err := scanFinding(a.pool.QueryRow(r.Context(), "SELECT "+findingSelect+" FROM findings WHERE tenant_id='default' AND finding_id=$1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "finding not found", id)
		return
	}
	if err != nil {
		writeError(w, 500, "finding query failed", err.Error())
		return
	}
	writeJSON(w, 200, f)
}

func (a *API) muteFinding(w http.ResponseWriter, r *http.Request) {
	if a.pool == nil {
		writeError(w, 503, "findings unavailable", "database pool is not configured")
		return
	}
	var in struct {
		Reason string `json:"reason"`
		Until  string `json:"until"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Reason) == "" || in.Until == "" {
		writeError(w, 400, "invalid mute", "reason and until are required")
		return
	}
	until, err := time.Parse(time.RFC3339, in.Until)
	if err != nil || !until.After(time.Now()) {
		writeError(w, 400, "invalid mute", "until must be a future RFC3339 timestamp")
		return
	}
	id := findingPathID(r, "mute")
	cmd, err := a.pool.Exec(r.Context(), "UPDATE findings SET state='muted',muted_until=$1,mute_reason=$2 WHERE tenant_id='default' AND finding_id=$3", until, in.Reason, id)
	if err != nil {
		writeError(w, 500, "finding mute failed", err.Error())
		return
	}
	if cmd.RowsAffected() == 0 {
		writeError(w, 404, "finding not found", id)
		return
	}
	writeJSON(w, 200, map[string]any{"finding_id": id, "state": "muted", "muted_until": until, "mute_reason": in.Reason})
}

func (a *API) unmuteFinding(w http.ResponseWriter, r *http.Request) {
	if a.pool == nil {
		writeError(w, 503, "findings unavailable", "database pool is not configured")
		return
	}
	id := findingPathID(r, "mute")
	cmd, err := a.pool.Exec(r.Context(), "UPDATE findings SET muted_until=NULL,mute_reason=NULL,state=CASE WHEN resolved_at IS NULL THEN 'open' ELSE 'resolved' END WHERE tenant_id='default' AND finding_id=$1", id)
	if err != nil {
		writeError(w, 500, "finding unmute failed", err.Error())
		return
	}
	if cmd.RowsAffected() == 0 {
		writeError(w, 404, "finding not found", id)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listAdvisorRules(w http.ResponseWriter, r *http.Request) {
	type ruleJSON struct {
		ID       string           `json:"id"`
		Severity advisor.Severity `json:"severity"`
		Scope    advisor.Scope    `json:"scope"`
		Needs    []string         `json:"needs"`
		MinTier  string           `json:"min_tier"`
	}
	out := make([]ruleJSON, 0, len(advisor.All()))
	for _, rule := range advisor.All() {
		out = append(out, ruleJSON{rule.ID(), rule.Severity(), rule.Scope(), rule.Needs(), rule.MinTier().String()})
	}
	writeJSON(w, 200, out)
}
