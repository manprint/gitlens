package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/manprint/pglens/internal/command"
	"github.com/manprint/pglens/internal/store"
)

// workbookStore is the storage the workbook routes use; the server keeps it
// in TimescaleDB (pgWorkbookStore), tests in memory.
type workbookStore interface {
	List(ctx context.Context, instanceID uuid.UUID, queryID *int64) ([]store.Workbook, error)
	Create(ctx context.Context, w store.Workbook) (store.Workbook, error)
	Get(ctx context.Context, id uuid.UUID) (store.Workbook, error)
	Update(ctx context.Context, id uuid.UUID, name, notes *string) (store.Workbook, error)
	Delete(ctx context.Context, id uuid.UUID) error
	AddVariant(ctx context.Context, workbookID uuid.UUID, v store.WorkbookVariant) (store.WorkbookVariant, error)
	GetVariant(ctx context.Context, workbookID, variantID uuid.UUID) (store.WorkbookVariant, error)
	UpdateVariant(ctx context.Context, workbookID, variantID uuid.UUID, v store.WorkbookVariant) (store.WorkbookVariant, error)
	DeleteVariant(ctx context.Context, workbookID, variantID uuid.UUID) error
	MarkPending(ctx context.Context, workbookID, variantID, commandID uuid.UUID) (bool, error)
}

type pgWorkbookStore struct {
	q      store.Queryer
	tenant string
}

func (p pgWorkbookStore) List(ctx context.Context, instanceID uuid.UUID, queryID *int64) ([]store.Workbook, error) {
	return store.ListWorkbooks(ctx, p.q, p.tenant, instanceID, queryID)
}
func (p pgWorkbookStore) Create(ctx context.Context, w store.Workbook) (store.Workbook, error) {
	w.TenantID = p.tenant
	return store.CreateWorkbook(ctx, p.q, w)
}
func (p pgWorkbookStore) Get(ctx context.Context, id uuid.UUID) (store.Workbook, error) {
	return store.GetWorkbook(ctx, p.q, p.tenant, id)
}
func (p pgWorkbookStore) Update(ctx context.Context, id uuid.UUID, name, notes *string) (store.Workbook, error) {
	return store.UpdateWorkbook(ctx, p.q, p.tenant, id, name, notes)
}
func (p pgWorkbookStore) Delete(ctx context.Context, id uuid.UUID) error {
	return store.DeleteWorkbook(ctx, p.q, p.tenant, id)
}
func (p pgWorkbookStore) AddVariant(ctx context.Context, wid uuid.UUID, v store.WorkbookVariant) (store.WorkbookVariant, error) {
	return store.AddVariant(ctx, p.q, p.tenant, wid, v)
}
func (p pgWorkbookStore) GetVariant(ctx context.Context, wid, vid uuid.UUID) (store.WorkbookVariant, error) {
	return store.GetVariant(ctx, p.q, p.tenant, wid, vid)
}
func (p pgWorkbookStore) UpdateVariant(ctx context.Context, wid, vid uuid.UUID, v store.WorkbookVariant) (store.WorkbookVariant, error) {
	return store.UpdateVariant(ctx, p.q, p.tenant, wid, vid, v)
}
func (p pgWorkbookStore) DeleteVariant(ctx context.Context, wid, vid uuid.UUID) error {
	return store.DeleteVariant(ctx, p.q, p.tenant, wid, vid)
}
func (p pgWorkbookStore) MarkPending(ctx context.Context, wid, vid, cid uuid.UUID) (bool, error) {
	return store.MarkVariantPending(ctx, p.q, p.tenant, wid, vid, cid)
}

// workbookService serves the tuning workbooks. A variant runs as an ordinary
// explain command queued by enqueue, the function the commands route uses.
type workbookService struct {
	store   workbookStore
	enqueue func(r *http.Request, instanceID uuid.UUID, kind command.Kind, args command.Args) (uuid.UUID, *apiFailure)
}

func (s *CommandService) registerWorkbookRoutes(r chi.Router) {
	ws := &workbookService{enqueue: s.insertCommand}
	if s.pool != nil {
		ws.store = pgWorkbookStore{q: s.pool, tenant: commandTenant}
	}
	ws.register(r)
}

func (ws *workbookService) register(r chi.Router) {
	r.Get("/api/v1/workbooks", ws.list)
	r.Post("/api/v1/workbooks", ws.create)
	r.Get("/api/v1/workbooks/{id}", ws.get)
	r.Patch("/api/v1/workbooks/{id}", ws.update)
	r.Delete("/api/v1/workbooks/{id}", ws.remove)
	r.Post("/api/v1/workbooks/{id}/variants", ws.addVariant)
	r.Put("/api/v1/workbooks/{id}/variants/{variant_id}", ws.updateVariant)
	r.Delete("/api/v1/workbooks/{id}/variants/{variant_id}", ws.deleteVariant)
	r.Post("/api/v1/workbooks/{id}/variants/{variant_id}/run", ws.run)
}

type workbookJSON struct {
	WorkbookID uuid.UUID     `json:"workbook_id"`
	InstanceID uuid.UUID     `json:"instance_id"`
	Database   string        `json:"database"`
	QueryID    string        `json:"queryid"`
	Name       string        `json:"name"`
	Notes      string        `json:"notes"`
	CreatedAt  time.Time     `json:"created_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
	Variants   []variantJSON `json:"variants"`
}

type variantJSON struct {
	VariantID   uuid.UUID         `json:"variant_id"`
	Position    int               `json:"position"`
	Name        string            `json:"name"`
	Settings    map[string]string `json:"settings"`
	Hints       string            `json:"hints"`
	Params      []*string         `json:"params"`
	Analyze     bool              `json:"analyze"`
	Status      string            `json:"status"`
	CommandID   *uuid.UUID        `json:"command_id"`
	Error       string            `json:"error"`
	Plan        json.RawMessage   `json:"plan,omitempty"`
	PlanHash    *string           `json:"plan_hash"`
	TotalCost   *float64          `json:"total_cost"`
	PlanningMS  *float64          `json:"planning_ms"`
	ExecutionMS *float64          `json:"execution_ms"`
	RanAt       *time.Time        `json:"ran_at"`
}

func toVariantJSON(v store.WorkbookVariant) variantJSON {
	settings := v.Settings
	if settings == nil {
		settings = map[string]string{}
	}
	return variantJSON{VariantID: v.ID, Position: v.Position, Name: v.Name, Settings: settings, Hints: v.Hints, Params: v.Params, Analyze: v.Analyze, Status: v.Status, CommandID: v.CommandID, Error: v.Error, Plan: v.Plan, PlanHash: v.PlanHash, TotalCost: v.TotalCost, PlanningMS: v.PlanningMS, ExecutionMS: v.ExecutionMS, RanAt: v.RanAt}
}

func toWorkbookJSON(w store.Workbook) workbookJSON {
	out := workbookJSON{WorkbookID: w.ID, InstanceID: w.InstanceID, Database: w.Datname, QueryID: strconv.FormatInt(w.QueryID, 10), Name: w.Name, Notes: w.Notes, CreatedAt: w.CreatedAt.UTC(), UpdatedAt: w.UpdatedAt.UTC(), Variants: make([]variantJSON, 0, len(w.Variants))}
	for _, v := range w.Variants {
		out.Variants = append(out.Variants, toVariantJSON(v))
	}
	return out
}

func (ws *workbookService) ready(w http.ResponseWriter) bool {
	if ws.store == nil {
		writeError(w, http.StatusServiceUnavailable, "workbooks unavailable", "database pool is not configured")
		return false
	}
	return true
}

// storeFailure writes the response for a store error.
func workbookFailure(w http.ResponseWriter, err error, what string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, what+" not found", err.Error())
	case errors.Is(err, store.ErrLimitReached):
		writeError(w, http.StatusConflict, "limit_reached", fmt.Sprintf("at most %d workbooks per instance and %d variants per workbook", store.MaxWorkbooksPerInstance, store.MaxVariantsPerWorkbook))
	default:
		writeError(w, http.StatusInternalServerError, what+" storage failed", err.Error())
	}
}

func pathUUID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+name, err.Error())
		return uuid.Nil, false
	}
	return id, true
}

func validWorkbookName(field, name string) error {
	n := utf8.RuneCountInString(name)
	if strings.TrimSpace(name) == "" || n > store.MaxWorkbookNameRunes {
		return fmt.Errorf("%s: must be 1 to %d characters", field, store.MaxWorkbookNameRunes)
	}
	return nil
}

func validNotes(notes string) error {
	if utf8.RuneCountInString(notes) > store.MaxWorkbookNotesRunes {
		return fmt.Errorf("notes: at most %d characters", store.MaxWorkbookNotesRunes)
	}
	return nil
}

func (ws *workbookService) list(w http.ResponseWriter, r *http.Request) {
	if !ws.ready(w) {
		return
	}
	instanceID, err := uuid.Parse(r.URL.Query().Get("instance_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workbook query", "instance_id: "+err.Error())
		return
	}
	var queryID *int64
	if raw := r.URL.Query().Get("queryid"); raw != "" {
		q, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid workbook query", "queryid: must be a 64-bit integer")
			return
		}
		queryID = &q
	}
	books, err := ws.store.List(r.Context(), instanceID, queryID)
	if err != nil {
		workbookFailure(w, err, "workbook")
		return
	}
	out := make([]workbookJSON, 0, len(books))
	for _, b := range books {
		j := toWorkbookJSON(b)
		for i := range j.Variants {
			j.Variants[i].Plan = nil
		}
		out = append(out, j)
	}
	writeJSON(w, http.StatusOK, map[string]any{"workbooks": out})
}

type createWorkbookRequest struct {
	InstanceID string `json:"instance_id"`
	Database   string `json:"database"`
	QueryID    string `json:"queryid"`
	Name       string `json:"name"`
	Notes      string `json:"notes"`
}

func (ws *workbookService) create(w http.ResponseWriter, r *http.Request) {
	if !ws.ready(w) {
		return
	}
	var req createWorkbookRequest
	if !decodeJSON(w, r, &req, maxCommandBody) {
		return
	}
	instanceID, err := uuid.Parse(req.InstanceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workbook", "instance_id: "+err.Error())
		return
	}
	queryID, err := strconv.ParseInt(req.QueryID, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workbook", "queryid: must be a 64-bit integer as a string")
		return
	}
	if err := (command.Args{QueryID: &queryID, Datname: req.Database}).Validate(command.KindExplain); err != nil {
		writeError(w, http.StatusBadRequest, "invalid workbook", "database: "+err.Error())
		return
	}
	for _, err := range []error{validWorkbookName("name", req.Name), validNotes(req.Notes)} {
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid workbook", err.Error())
			return
		}
	}
	book, err := ws.store.Create(r.Context(), store.Workbook{InstanceID: instanceID, Datname: req.Database, QueryID: queryID, Name: req.Name, Notes: req.Notes})
	if err != nil {
		workbookFailure(w, err, "instance")
		return
	}
	writeJSON(w, http.StatusCreated, toWorkbookJSON(book))
}

func (ws *workbookService) get(w http.ResponseWriter, r *http.Request) {
	if !ws.ready(w) {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	book, err := ws.store.Get(r.Context(), id)
	if err != nil {
		workbookFailure(w, err, "workbook")
		return
	}
	writeJSON(w, http.StatusOK, toWorkbookJSON(book))
}

type updateWorkbookRequest struct {
	Name  *string `json:"name"`
	Notes *string `json:"notes"`
}

func (ws *workbookService) update(w http.ResponseWriter, r *http.Request) {
	if !ws.ready(w) {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var req updateWorkbookRequest
	if !decodeJSON(w, r, &req, maxCommandBody) {
		return
	}
	if req.Name != nil {
		if err := validWorkbookName("name", *req.Name); err != nil {
			writeError(w, http.StatusBadRequest, "invalid workbook", err.Error())
			return
		}
	}
	if req.Notes != nil {
		if err := validNotes(*req.Notes); err != nil {
			writeError(w, http.StatusBadRequest, "invalid workbook", err.Error())
			return
		}
	}
	book, err := ws.store.Update(r.Context(), id, req.Name, req.Notes)
	if err != nil {
		workbookFailure(w, err, "workbook")
		return
	}
	writeJSON(w, http.StatusOK, toWorkbookJSON(book))
}

func (ws *workbookService) remove(w http.ResponseWriter, r *http.Request) {
	if !ws.ready(w) {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	if err := ws.store.Delete(r.Context(), id); err != nil {
		workbookFailure(w, err, "workbook")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type variantRequest struct {
	Name     string            `json:"name"`
	Settings map[string]string `json:"settings"`
	Hints    string            `json:"hints"`
	Params   []*string         `json:"params"`
	Analyze  bool              `json:"analyze"`
}

// validateVariant checks a variant with the explain command's own rules; the
// error names the field.
func validateVariant(req variantRequest) error {
	if err := validWorkbookName("name", req.Name); err != nil {
		return err
	}
	if err := command.ValidateSettings(req.Settings); err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	if err := command.ValidateHints(req.Hints); err != nil {
		return fmt.Errorf("hints: %w", err)
	}
	if len(req.Params) > command.MaxParams {
		return fmt.Errorf("params: at most %d values", command.MaxParams)
	}
	for i, p := range req.Params {
		if p != nil && len(*p) > command.MaxParamValueBytes {
			return fmt.Errorf("params: $%d is longer than %d bytes", i+1, command.MaxParamValueBytes)
		}
	}
	return nil
}

func (ws *workbookService) decodeVariant(w http.ResponseWriter, r *http.Request) (store.WorkbookVariant, bool) {
	var req variantRequest
	if !decodeJSON(w, r, &req, maxCommandBody) {
		return store.WorkbookVariant{}, false
	}
	if err := validateVariant(req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid variant", err.Error())
		return store.WorkbookVariant{}, false
	}
	settings := req.Settings
	if settings == nil {
		settings = map[string]string{}
	}
	return store.WorkbookVariant{Name: req.Name, Settings: settings, Hints: req.Hints, Params: req.Params, Analyze: req.Analyze}, true
}

func (ws *workbookService) addVariant(w http.ResponseWriter, r *http.Request) {
	if !ws.ready(w) {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	v, ok := ws.decodeVariant(w, r)
	if !ok {
		return
	}
	created, err := ws.store.AddVariant(r.Context(), id, v)
	if err != nil {
		workbookFailure(w, err, "workbook")
		return
	}
	writeJSON(w, http.StatusCreated, toVariantJSON(created))
}

func (ws *workbookService) updateVariant(w http.ResponseWriter, r *http.Request) {
	if !ws.ready(w) {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	vid, ok := pathUUID(w, r, "variant_id")
	if !ok {
		return
	}
	v, ok := ws.decodeVariant(w, r)
	if !ok {
		return
	}
	updated, err := ws.store.UpdateVariant(r.Context(), id, vid, v)
	if err != nil {
		workbookFailure(w, err, "variant")
		return
	}
	writeJSON(w, http.StatusOK, toVariantJSON(updated))
}

func (ws *workbookService) deleteVariant(w http.ResponseWriter, r *http.Request) {
	if !ws.ready(w) {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	vid, ok := pathUUID(w, r, "variant_id")
	if !ok {
		return
	}
	if err := ws.store.DeleteVariant(r.Context(), id, vid); err != nil {
		workbookFailure(w, err, "variant")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// variantArgs is the explain command a variant runs: the workbook's query
// and database with the variant's settings, hints, values and ANALYZE flag.
// The Baseline (nothing set) is an explain exactly like the query page's.
func variantArgs(book store.Workbook, v store.WorkbookVariant) command.Args {
	queryID := book.QueryID
	args := command.Args{QueryID: &queryID, Datname: book.Datname, Analyze: v.Analyze, Hints: v.Hints}
	if len(v.Settings) > 0 {
		args.Settings = maps.Clone(v.Settings)
	}
	if len(v.Params) > 0 {
		args.Params = v.Params
	}
	return args
}

func variantPending(w http.ResponseWriter) {
	writeError(w, http.StatusConflict, "variant_pending", "the variant's previous run has not finished")
}

func (ws *workbookService) run(w http.ResponseWriter, r *http.Request) {
	if !ws.ready(w) {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	vid, ok := pathUUID(w, r, "variant_id")
	if !ok {
		return
	}
	ctx := r.Context()
	book, err := ws.store.Get(ctx, id)
	if err != nil {
		workbookFailure(w, err, "workbook")
		return
	}
	v, err := ws.store.GetVariant(ctx, id, vid)
	if err != nil {
		workbookFailure(w, err, "variant")
		return
	}
	if v.Status == "pending" {
		variantPending(w)
		return
	}
	commandID, failure := ws.enqueue(r, book.InstanceID, command.KindExplain, variantArgs(book, v))
	if failure != nil {
		failure.write(w)
		return
	}
	marked, err := ws.store.MarkPending(ctx, id, vid, commandID)
	if err != nil {
		workbookFailure(w, err, "variant")
		return
	}
	if !marked {
		// Another run marked the variant between the read and now; the
		// command just queued runs, and its result finds no variant.
		variantPending(w)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]uuid.UUID{"command_id": commandID})
}

// variantResultOf reads what a finished explain leaves on its variant: the
// plan, its hash, the root node's total cost and the planning and execution
// times (the latter only with ANALYZE).
func variantResultOf(state string, result json.RawMessage, errText string, at time.Time) store.VariantResult {
	out := store.VariantResult{Status: state, Error: errText, RanAt: at.UTC()}
	if state != "done" {
		if out.Error == "" {
			out.Error = "the command failed without an error text"
		}
		return out
	}
	var payload struct {
		Plan     json.RawMessage `json:"plan"`
		PlanHash string          `json:"plan_hash"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return out
	}
	out.Plan = payload.Plan
	if payload.PlanHash != "" {
		out.PlanHash = &payload.PlanHash
	}
	var plans []struct {
		Plan struct {
			TotalCost *float64 `json:"Total Cost"`
		} `json:"Plan"`
		PlanningTime  *float64 `json:"Planning Time"`
		ExecutionTime *float64 `json:"Execution Time"`
	}
	if err := json.Unmarshal(payload.Plan, &plans); err == nil && len(plans) > 0 {
		out.TotalCost = plans[0].Plan.TotalCost
		out.PlanningMS = plans[0].PlanningTime
		out.ExecutionMS = plans[0].ExecutionTime
	}
	return out
}
