package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/store"
)

// maxNameLength is the longest cluster name or instance display name, in
// characters after trimming.
const maxNameLength = 64

// errNameInUse is the error a rename answers when another cluster, or another
// instance, already has the name. Names are compared without case.
const errNameInUse = "name already in use"

// validateName checks a cluster name or an instance display name and returns
// the value to store. Surrounding white space is trimmed. A null, an empty or an
// all-blank name clears the name, which is stored as NULL so the interface falls
// back to its default label. A name may hold any Unicode text up to 64
// characters, but no control character, no invisible format character (zero
// width space, byte order mark, bidirectional overrides) and no line or
// paragraph separator: they would not show in a label, would make two names
// that look alike differ, or could reverse how a label reads.
func validateName(in *string) (*string, error) {
	if in == nil {
		return nil, nil
	}
	name := strings.TrimSpace(*in)
	if name == "" {
		return nil, nil
	}
	if !utf8.ValidString(name) {
		return nil, errors.New("the name must be valid UTF-8")
	}
	if utf8.RuneCountInString(name) > maxNameLength {
		return nil, errors.New("the name may not exceed 64 characters")
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			return nil, errors.New("the name may not contain control or invisible characters")
		}
	}
	return &name, nil
}

// nameField reads the name field of a rename body. The field is required: a
// body without it is refused rather than read as a request to clear the name.
// JSON null clears the name; any other value must be a string.
func nameField(raw json.RawMessage, field string) (*string, error) {
	if len(raw) == 0 {
		return nil, errors.New(field + " is required")
	}
	var name *string
	if err := json.Unmarshal(raw, &name); err != nil {
		return nil, errors.New(field + " must be a string or null")
	}
	return validateName(name)
}

// isUniqueViolation reports whether err is PostgreSQL's unique_violation.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

type clusterRenameBody struct {
	Name json.RawMessage `json:"name"`
}

// handleRenameCluster sets or clears the name of a cluster. The name is written
// by one statement, so two renames race only on the unique index, which turns
// the loser into a 409. A cluster with no instance is not listed, so it cannot
// be renamed either. The answer is the cluster as the list shows it.
func (a *API) handleRenameCluster(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	cid, err := pgtype.ParseClusterID(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid cluster_id", err.Error())
		return
	}
	var in clusterRenameBody
	if !decodeStrictJSONBody(w, r, &in) {
		return
	}
	name, err := nameField(in.Name, "name")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid name", err.Error())
		return
	}
	if a.pool == nil {
		writeError(w, http.StatusServiceUnavailable, "store unavailable", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var row clusterListRow
	err = a.pool.QueryRow(ctx, `UPDATE clusters SET name=$1
		WHERE tenant_id='default' AND cluster_id=$2
		  AND EXISTS (SELECT 1 FROM instances i WHERE i.tenant_id=clusters.tenant_id AND i.cluster_id=clusters.cluster_id)
		RETURNING cluster_id, name, id_source`, name, store.ToDB(cid)).Scan(&row.idDB, &row.name, &row.idSource)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			writeError(w, http.StatusNotFound, "cluster not found", idStr)
		case isUniqueViolation(err):
			writeError(w, http.StatusConflict, errNameInUse, "another cluster has this name; names are compared without case")
		default:
			writeError(w, http.StatusInternalServerError, "cluster rename failed", err.Error())
		}
		return
	}
	resp, err := a.assembleClusters(ctx, []clusterListRow{row})
	if err != nil {
		writeClusterQueryError(w, err)
		return
	}
	if len(resp) == 0 {
		// The cluster's last instance went away after the update.
		writeError(w, http.StatusNotFound, "cluster not found", idStr)
		return
	}
	writeJSON(w, http.StatusOK, resp[0])
}

type instanceRenameBody struct {
	DisplayName json.RawMessage `json:"display_name"`
}

// handleRenameInstance sets or clears the display name of an instance. Like a
// cluster rename it is one statement, and the unique index decides a race.
// The answer is the instance as the instance list shows it.
func (a *API) handleRenameInstance(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	iid, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", err.Error())
		return
	}
	var in instanceRenameBody
	if !decodeStrictJSONBody(w, r, &in) {
		return
	}
	name, err := nameField(in.DisplayName, "display_name")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid name", err.Error())
		return
	}
	if a.pool == nil {
		writeError(w, http.StatusServiceUnavailable, "store unavailable", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var (
		dbIID     uuid.UUID
		clusterDB int64
		resp      instanceListResp
	)
	err = a.pool.QueryRow(ctx, `UPDATE instances SET display_name=$1
		WHERE tenant_id='default' AND instance_id=$2
		RETURNING instance_id, cluster_id, addr, port, role, pg_version, perm_tier, last_seen, display_name`, name, iid).
		Scan(&dbIID, &clusterDB, &resp.Addr, &resp.Port, &resp.Role, &resp.PGVersion, &resp.PermTier, &resp.LastSeen, &resp.DisplayName)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			writeError(w, http.StatusNotFound, "instance not found", idStr)
		case isUniqueViolation(err):
			writeError(w, http.StatusConflict, errNameInUse, "another instance has this name; names are compared without case")
		default:
			writeError(w, http.StatusInternalServerError, "instance rename failed", err.Error())
		}
		return
	}
	resp.InstanceID = dbIID.String()
	resp.ClusterID = store.FromDB(clusterDB).String()
	resp.Up = isUp(resp.LastSeen, time.Now().UTC())
	writeJSON(w, http.StatusOK, resp)
}
