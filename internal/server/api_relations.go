package server

import (
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
	if order != "" && order != "dead_tup" && order != "size" && order != "seq_scan" {
		writeError(w, 400, "invalid order_by", "allowed values: dead_tup, size, seq_scan")
		return
	}
	resp := relationAPIResponse{InstanceID: id.String(), Items: []map[string]any{}}
	if a.pool != nil {
		rows, e := a.pool.Query(r.Context(), "SELECT schemaname,relname,n_dead_tup,total_bytes,seq_scan,ts FROM metrics_tables WHERE instance_id=$1 AND ts >= now()-interval '15 minutes' ORDER BY n_dead_tup DESC LIMIT $2", id, relationLimit(r))
		if e == nil {
			defer rows.Close()
			for rows.Next() {
				var schema, name string
				var dead, bytes, seq int64
				var ts time.Time
				if rows.Scan(&schema, &name, &dead, &bytes, &seq, &ts) == nil {
					resp.Items = append(resp.Items, map[string]any{"schemaname": schema, "relname": name, "n_dead_tup": dead, "total_bytes": bytes, "seq_scan": seq, "ts": ts})
				}
			}
		}
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
		rows, e := a.pool.Query(r.Context(), "SELECT schemaname,relname,indexrelname,object_kind,real_bytes,expected_bytes,bloat_bytes,bloat_ratio,method,ts FROM metrics_bloat WHERE instance_id=$1 ORDER BY bloat_bytes DESC LIMIT $2", id, relationLimit(r))
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
	}
	writeJSON(w, 200, resp)
}
