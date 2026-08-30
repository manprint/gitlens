package server

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type relationAPIResponse struct {
	InstanceID           string           `json:"instance_id"`
	Items                []map[string]any `json:"items"`
	Truncated            bool             `json:"truncated"`
	RelationsNotReported *int             `json:"relations_not_reported"`
}

func (a *API) registerRelationRoutes(r chi.Router) {
	r.Get("/api/v1/instances/{id}/tables", a.handleTables)
	r.Get("/api/v1/instances/{id}/indexes", a.handleIndexes)
	r.Get("/api/v1/instances/{id}/bloat", a.handleBloat)
}
func relationID(r *http.Request) (uuid.UUID, error) { return uuid.Parse(chi.URLParam(r, "id")) }
func relationLimit(r *http.Request) int {
	n, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if n <= 0 {
		n = 50
	}
	if n > 500 {
		n = 500
	}
	return n
}
func (a *API) handleTables(w http.ResponseWriter, r *http.Request) {
	id, e := relationID(r)
	if e != nil {
		writeError(w, 400, "invalid instance_id", e.Error())
		return
	}
	order := r.URL.Query().Get("order_by")
	if order == "" {
		order = r.URL.Query().Get("order")
	}
	if order != "" && order != "dead_tup" && order != "dead_ratio" && order != "size" && order != "seq_scan" {
		writeError(w, 400, "invalid order_by", "allowed values: dead_tup, dead_ratio, size, seq_scan")
		return
	}
	resp := relationAPIResponse{InstanceID: id.String(), Items: []map[string]any{}}
	if a.pool != nil {
		orderExpr := "n_dead_tup DESC"
		switch order {
		case "dead_ratio":
			orderExpr = "COALESCE(dead_tuple_ratio, n_dead_tup::double precision / NULLIF(n_live_tup, 0)) DESC NULLS LAST"
		case "size":
			orderExpr = "total_bytes DESC NULLS LAST"
		case "seq_scan":
			orderExpr = "seq_scan DESC NULLS LAST"
		}
		rows, e := a.pool.Query(r.Context(), "SELECT schemaname,relname,COALESCE(n_live_tup,0),COALESCE(n_dead_tup,0),dead_tuple_ratio,last_vacuum_age_seconds,COALESCE(total_bytes,0),COALESCE(seq_scan,0),ts FROM metrics_tables WHERE instance_id=$1 AND ts >= now()-interval '15 minutes' ORDER BY "+orderExpr+" LIMIT $2", id, relationLimit(r))
		if e == nil {
			defer rows.Close()
			for rows.Next() {
				var schema, name string
				var live, dead, bytes, seq int64
				var ratio, vacuumAge *float64
				var ts time.Time
				if rows.Scan(&schema, &name, &live, &dead, &ratio, &vacuumAge, &bytes, &seq, &ts) == nil {
					item := map[string]any{"schemaname": schema, "relname": name, "n_live_tup": live, "n_dead_tup": dead, "total_bytes": bytes, "seq_scan": seq, "ts": ts}
					if ratio != nil {
						item["dead_ratio"] = *ratio
					} else if live > 0 {
						item["dead_ratio"] = float64(dead) / float64(live)
					}
					if vacuumAge != nil {
						item["last_vacuum_age_seconds"] = *vacuumAge
					}
					resp.Items = append(resp.Items, item)
				}
			}
		}
		setRelationMetadata(r.Context(), a.pool, &resp, id, "table_stats")
	}
	writeJSON(w, 200, resp)
}
func (a *API) handleIndexes(w http.ResponseWriter, r *http.Request) {
	id, e := relationID(r)
	if e != nil {
		writeError(w, 400, "invalid instance_id", e.Error())
		return
	}
	resp := relationAPIResponse{InstanceID: id.String(), Items: []map[string]any{}}
	if a.pool != nil {
		rows, e := a.pool.Query(r.Context(), "SELECT schemaname,relname,indexrelname,idx_scan,index_bytes,is_unique,is_primary,is_valid,ts FROM metrics_indexes WHERE instance_id=$1 ORDER BY index_bytes DESC LIMIT $2", id, relationLimit(r))
		if e == nil {
			defer rows.Close()
			for rows.Next() {
				var s, n, i string
				var scan, bytes int64
				var u, p, v bool
				var ts time.Time
				if rows.Scan(&s, &n, &i, &scan, &bytes, &u, &p, &v, &ts) == nil {
					resp.Items = append(resp.Items, map[string]any{"schemaname": s, "relname": n, "indexrelname": i, "idx_scan": scan, "index_bytes": bytes, "is_unique": u, "is_primary": p, "is_valid": v, "ts": ts})
				}
			}
		}
		setRelationMetadata(r.Context(), a.pool, &resp, id, "index_stats")
	}
	writeJSON(w, 200, resp)
}
func (a *API) handleBloat(w http.ResponseWriter, r *http.Request) {
	id, e := relationID(r)
	if e != nil {
		writeError(w, 400, "invalid instance_id", e.Error())
		return
	}
	resp := relationAPIResponse{InstanceID: id.String(), Items: []map[string]any{}}
	if a.pool != nil {
		method := r.URL.Query().Get("method")
		if method != "" && method != "estimate" && method != "pgstattuple" {
			writeError(w, http.StatusBadRequest, "invalid method", "allowed values: estimate, pgstattuple")
			return
		}
		query := "SELECT schemaname,relname,indexrelname,object_kind,real_bytes,expected_bytes,bloat_bytes,bloat_ratio,method,ts FROM metrics_bloat WHERE instance_id=$1"
		args := []any{id}
		if method != "" {
			query += " AND method=$2"
			args = append(args, method)
		}
		query += " ORDER BY bloat_bytes DESC LIMIT $" + strconv.Itoa(len(args)+1)
		args = append(args, relationLimit(r))
		rows, e := a.pool.Query(r.Context(), query, args...)
		if e == nil {
			defer rows.Close()
			for rows.Next() {
				var s, n, i, k, m string
				var real, expected, bloat int64
				var ratio float64
				var ts time.Time
				if rows.Scan(&s, &n, &i, &k, &real, &expected, &bloat, &ratio, &m, &ts) == nil {
					resp.Items = append(resp.Items, map[string]any{"schemaname": s, "relname": n, "indexrelname": i, "object_kind": k, "real_bytes": real, "expected_bytes": expected, "bloat_bytes": bloat, "bloat_ratio": ratio, "method": m, "ts": ts})
				}
			}
		}
		setRelationMetadata(r.Context(), a.pool, &resp, id, "bloat_estimate")
	}
	writeJSON(w, 200, resp)
}

// setRelationMetadata mirrors the typed relation budgets in the read API. A
// positive count is intentionally a lower bound: the event contract records
// that at least one candidate was omitted, without pretending the selector's
// transient candidate count is durable state.
func setRelationMetadata(ctx context.Context, pool dbPool, resp *relationAPIResponse, id uuid.UUID, check string) {
	var truncated bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM events
		WHERE tenant_id='default' AND instance_id=$1 AND type='cardinality_truncated'
		  AND payload->>'check'=$2 AND ts >= now()-interval '15 minutes')`, id, check).Scan(&truncated); err != nil || !truncated {
		return
	}
	resp.Truncated = true
	n := 1
	resp.RelationsNotReported = &n
}
