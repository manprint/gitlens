package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/command"
	"github.com/manprint/pglens/internal/store"
)

// memWorkbooks keeps workbooks in memory with the store's limits and reset
// rules, so the handlers can be driven without a database.
type memWorkbooks struct {
	books map[uuid.UUID]*store.Workbook
}

func newMemWorkbooks() *memWorkbooks { return &memWorkbooks{books: map[uuid.UUID]*store.Workbook{}} }

func (m *memWorkbooks) List(_ context.Context, instanceID uuid.UUID, queryID *int64) ([]store.Workbook, error) {
	var out []store.Workbook
	for _, b := range m.books {
		if b.InstanceID == instanceID && (queryID == nil || *queryID == b.QueryID) {
			out = append(out, *b)
		}
	}
	return out, nil
}

func (m *memWorkbooks) Create(_ context.Context, w store.Workbook) (store.Workbook, error) {
	n := 0
	for _, b := range m.books {
		if b.InstanceID == w.InstanceID {
			n++
		}
	}
	if n >= store.MaxWorkbooksPerInstance {
		return store.Workbook{}, store.ErrLimitReached
	}
	w.ID = uuid.New()
	w.Variants = []store.WorkbookVariant{{ID: uuid.New(), WorkbookID: w.ID, Name: store.BaselineVariantName, Settings: map[string]string{}, Status: "never"}}
	m.books[w.ID] = &w
	return w, nil
}

func (m *memWorkbooks) Get(_ context.Context, id uuid.UUID) (store.Workbook, error) {
	b, ok := m.books[id]
	if !ok {
		return store.Workbook{}, store.ErrNotFound
	}
	return *b, nil
}

func (m *memWorkbooks) Update(_ context.Context, id uuid.UUID, name, notes *string) (store.Workbook, error) {
	b, ok := m.books[id]
	if !ok {
		return store.Workbook{}, store.ErrNotFound
	}
	if name != nil {
		b.Name = *name
	}
	if notes != nil {
		b.Notes = *notes
	}
	return *b, nil
}

func (m *memWorkbooks) Delete(_ context.Context, id uuid.UUID) error {
	if _, ok := m.books[id]; !ok {
		return store.ErrNotFound
	}
	delete(m.books, id)
	return nil
}

func (m *memWorkbooks) variant(wid, vid uuid.UUID) (*store.WorkbookVariant, error) {
	b, ok := m.books[wid]
	if !ok {
		return nil, store.ErrNotFound
	}
	for i := range b.Variants {
		if b.Variants[i].ID == vid {
			return &b.Variants[i], nil
		}
	}
	return nil, store.ErrNotFound
}

func (m *memWorkbooks) AddVariant(_ context.Context, wid uuid.UUID, v store.WorkbookVariant) (store.WorkbookVariant, error) {
	b, ok := m.books[wid]
	if !ok {
		return store.WorkbookVariant{}, store.ErrNotFound
	}
	if len(b.Variants) >= store.MaxVariantsPerWorkbook {
		return store.WorkbookVariant{}, store.ErrLimitReached
	}
	v.ID, v.WorkbookID, v.Position, v.Status = uuid.New(), wid, len(b.Variants), "never"
	b.Variants = append(b.Variants, v)
	return v, nil
}

func (m *memWorkbooks) GetVariant(_ context.Context, wid, vid uuid.UUID) (store.WorkbookVariant, error) {
	v, err := m.variant(wid, vid)
	if err != nil {
		return store.WorkbookVariant{}, err
	}
	return *v, nil
}

func (m *memWorkbooks) UpdateVariant(_ context.Context, wid, vid uuid.UUID, nv store.WorkbookVariant) (store.WorkbookVariant, error) {
	v, err := m.variant(wid, vid)
	if err != nil {
		return store.WorkbookVariant{}, err
	}
	*v = store.WorkbookVariant{ID: v.ID, WorkbookID: wid, Position: v.Position, Name: nv.Name, Settings: nv.Settings, Hints: nv.Hints, Params: nv.Params, Analyze: nv.Analyze, Status: "never"}
	return *v, nil
}

func (m *memWorkbooks) DeleteVariant(_ context.Context, wid, vid uuid.UUID) error {
	b, ok := m.books[wid]
	if !ok {
		return store.ErrNotFound
	}
	for i := range b.Variants {
		if b.Variants[i].ID == vid {
			b.Variants = append(b.Variants[:i], b.Variants[i+1:]...)
			return nil
		}
	}
	return store.ErrNotFound
}

func (m *memWorkbooks) MarkPending(_ context.Context, wid, vid, cid uuid.UUID) (bool, error) {
	v, err := m.variant(wid, vid)
	if err != nil {
		return false, err
	}
	if v.Status == "pending" {
		return false, nil
	}
	v.Status, v.CommandID = "pending", &cid
	return true, nil
}

type enqueued struct {
	instanceID uuid.UUID
	kind       command.Kind
	args       command.Args
}

func workbookRouter(m *memWorkbooks, calls *[]enqueued, failure *apiFailure) *chi.Mux {
	ws := &workbookService{store: m, enqueue: func(_ *http.Request, instanceID uuid.UUID, kind command.Kind, args command.Args) (uuid.UUID, *apiFailure) {
		if failure != nil {
			return uuid.Nil, failure
		}
		if err := args.Validate(kind); err != nil {
			return uuid.Nil, &apiFailure{http.StatusBadRequest, "invalid command", err.Error()}
		}
		*calls = append(*calls, enqueued{instanceID, kind, args})
		return uuid.New(), nil
	}}
	r := chi.NewRouter()
	ws.register(r)
	return r
}

func doJSON(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

func createTestWorkbook(t *testing.T, h http.Handler, instanceID uuid.UUID) workbookJSON {
	t.Helper()
	rec := doJSON(t, h, http.MethodPost, "/api/v1/workbooks", `{"instance_id":"`+instanceID.String()+`","database":"app","queryid":"-77","name":"lookup"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var out workbookJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return out
}

func errorTitle(t *testing.T, rec *httptest.ResponseRecorder) (string, string) {
	t.Helper()
	var body struct{ Error, Detail string }
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body.Error, body.Detail
}

// TestWBK007: a new workbook starts with its Baseline; the 101st workbook of
// an instance and the 21st variant of a workbook are refused as limit_reached.
func TestWBK007_CreateGivesBaselineAndLimitsHold(t *testing.T) {
	m := newMemWorkbooks()
	h := workbookRouter(m, &[]enqueued{}, nil)
	instanceID := uuid.New()
	book := createTestWorkbook(t, h, instanceID)
	require.Equal(t, "-77", book.QueryID)
	require.Equal(t, "app", book.Database)
	require.Len(t, book.Variants, 1)
	base := book.Variants[0]
	require.Equal(t, store.BaselineVariantName, base.Name)
	require.Empty(t, base.Settings)
	require.Empty(t, base.Hints)
	require.Empty(t, base.Params)
	require.Equal(t, "never", base.Status)

	for i := 1; i < store.MaxWorkbooksPerInstance; i++ {
		createTestWorkbook(t, h, instanceID)
	}
	rec := doJSON(t, h, http.MethodPost, "/api/v1/workbooks", `{"instance_id":"`+instanceID.String()+`","database":"app","queryid":"1","name":"one too many"}`)
	require.Equal(t, http.StatusConflict, rec.Code)
	title, _ := errorTitle(t, rec)
	require.Equal(t, "limit_reached", title)

	for i := 1; i < store.MaxVariantsPerWorkbook; i++ {
		rec := doJSON(t, h, http.MethodPost, "/api/v1/workbooks/"+book.WorkbookID.String()+"/variants", `{"name":"v"}`)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	}
	rec = doJSON(t, h, http.MethodPost, "/api/v1/workbooks/"+book.WorkbookID.String()+"/variants", `{"name":"v21"}`)
	require.Equal(t, http.StatusConflict, rec.Code)
	title, _ = errorTitle(t, rec)
	require.Equal(t, "limit_reached", title)
}

// TestWBK008: a bad variant is refused with 400 naming the field.
func TestWBK008_BadVariantNamesTheField(t *testing.T) {
	m := newMemWorkbooks()
	h := workbookRouter(m, &[]enqueued{}, nil)
	book := createTestWorkbook(t, h, uuid.New())
	tooMany := `"` + strings.Repeat(`1","`, command.MaxParams) + `1"`
	for body, field := range map[string]string{
		`{"name":"v","settings":{"shared_buffers":"1GB"}}`:   "settings:",
		`{"name":"v","settings":{"enable_seqscan":"maybe"}}`: "settings:",
		`{"name":"v","hints":"SeqScan(t) */ DROP TABLE t"}`:  "hints:",
		`{"name":"v","hints":"Set(enable_seqscan off)"}`:     "hints:",
		`{"name":"v","params":[` + tooMany + `]}`:            "params:",
		`{"name":""}`: "name:",
	} {
		rec := doJSON(t, h, http.MethodPost, "/api/v1/workbooks/"+book.WorkbookID.String()+"/variants", body)
		require.Equal(t, http.StatusBadRequest, rec.Code, body)
		_, detail := errorTitle(t, rec)
		require.True(t, strings.HasPrefix(detail, field), "%s: %s", body, detail)
	}
	rec := doJSON(t, h, http.MethodPost, "/api/v1/workbooks/"+book.WorkbookID.String()+"/variants", `{"name":"v","sql":"DELETE FROM t"}`)
	require.Equal(t, http.StatusBadRequest, rec.Code, "unknown fields such as sql are refused")
	rec = doJSON(t, h, http.MethodPost, "/api/v1/workbooks", `{"instance_id":"`+uuid.NewString()+`","database":"app","queryid":"x","name":"n"}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	_, detail := errorTitle(t, rec)
	require.Contains(t, detail, "queryid")
}

// TestWBK009: run queues an explain with the variant's settings, hints,
// values and ANALYZE flag and the workbook's statement; a pending variant is
// refused, and a closed gate answers as the commands route does.
func TestWBK009_RunBuildsTheExplainArgs(t *testing.T) {
	m := newMemWorkbooks()
	var calls []enqueued
	h := workbookRouter(m, &calls, nil)
	instanceID := uuid.New()
	book := createTestWorkbook(t, h, instanceID)
	rec := doJSON(t, h, http.MethodPost, "/api/v1/workbooks/"+book.WorkbookID.String()+"/variants", `{"name":"no index","settings":{"enable_indexscan":"off","work_mem":"65536"},"hints":"SeqScan(t)","params":["42",null],"analyze":true}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var v variantJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &v))
	runPath := "/api/v1/workbooks/" + book.WorkbookID.String() + "/variants/" + v.VariantID.String() + "/run"

	rec = doJSON(t, h, http.MethodPost, runPath, "")
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	require.Len(t, calls, 1)
	got := calls[0]
	require.Equal(t, instanceID, got.instanceID)
	require.Equal(t, command.KindExplain, got.kind)
	require.Equal(t, int64(-77), *got.args.QueryID)
	require.Equal(t, "app", got.args.Datname)
	require.Equal(t, map[string]string{"enable_indexscan": "off", "work_mem": "65536"}, got.args.Settings)
	require.Equal(t, "SeqScan(t)", got.args.Hints)
	require.Len(t, got.args.Params, 2)
	require.Equal(t, "42", *got.args.Params[0])
	require.Nil(t, got.args.Params[1])
	require.True(t, got.args.Analyze)
	var accepted struct {
		CommandID uuid.UUID `json:"command_id"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &accepted))
	stored, _ := m.GetVariant(context.Background(), book.WorkbookID, v.VariantID)
	require.Equal(t, "pending", stored.Status)
	require.Equal(t, accepted.CommandID, *stored.CommandID)

	rec = doJSON(t, h, http.MethodPost, runPath, "")
	require.Equal(t, http.StatusConflict, rec.Code)
	title, _ := errorTitle(t, rec)
	require.Equal(t, "variant_pending", title)
	require.Len(t, calls, 1)

	// The Baseline is a plain explain, as the query page queues it.
	rec = doJSON(t, h, http.MethodPost, "/api/v1/workbooks/"+book.WorkbookID.String()+"/variants/"+book.Variants[0].VariantID.String()+"/run", "")
	require.Equal(t, http.StatusAccepted, rec.Code)
	require.Nil(t, calls[1].args.Settings)
	require.Empty(t, calls[1].args.Hints)
	require.Nil(t, calls[1].args.Params)

	closed := workbookRouter(m, &calls, &apiFailure{http.StatusTooManyRequests, "command queue full", "full"})
	other := createTestWorkbook(t, closed, instanceID)
	rec = doJSON(t, closed, http.MethodPost, "/api/v1/workbooks/"+other.WorkbookID.String()+"/variants/"+other.Variants[0].VariantID.String()+"/run", "")
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	title, _ = errorTitle(t, rec)
	require.Equal(t, "command queue full", title)

	rec = doJSON(t, h, http.MethodPost, "/api/v1/workbooks/"+uuid.NewString()+"/variants/"+v.VariantID.String()+"/run", "")
	require.Equal(t, http.StatusNotFound, rec.Code)
}

// TestWBK010: a finished explain fills the variant's result columns; a failed
// command leaves failed with its error.
func TestWBK010_ResultFillsTheVariant(t *testing.T) {
	at := time.Date(2026, 11, 21, 10, 0, 0, 0, time.FixedZone("CET", 3600))
	analyzed := json.RawMessage(`{"plan":[{"Plan":{"Node Type":"Seq Scan","Total Cost":1693.0},"Planning Time":0.12,"Execution Time":4.5}],"plan_hash":"ab12","datname":"app"}`)
	got := variantResultOf("done", analyzed, "", at)
	require.Equal(t, "done", got.Status)
	require.Equal(t, "ab12", *got.PlanHash)
	require.InDelta(t, 1693.0, *got.TotalCost, 1e-9)
	require.InDelta(t, 0.12, *got.PlanningMS, 1e-9)
	require.InDelta(t, 4.5, *got.ExecutionMS, 1e-9)
	require.Equal(t, at.UTC(), got.RanAt)
	require.JSONEq(t, `[{"Plan":{"Node Type":"Seq Scan","Total Cost":1693.0},"Planning Time":0.12,"Execution Time":4.5}]`, string(got.Plan))

	plain := variantResultOf("done", json.RawMessage(`{"plan":[{"Plan":{"Total Cost":8.3}}],"plan_hash":"cd34"}`), "", at)
	require.InDelta(t, 8.3, *plain.TotalCost, 1e-9)
	require.Nil(t, plain.ExecutionMS, "no ANALYZE, no execution time")

	failed := variantResultOf("failed", nil, "rejected: hint_plan_inactive: pg_hint_plan is not loaded", at)
	require.Equal(t, "failed", failed.Status)
	require.Equal(t, "rejected: hint_plan_inactive: pg_hint_plan is not loaded", failed.Error)
	require.Nil(t, failed.Plan)
	require.Nil(t, failed.TotalCost)

	require.NotEmpty(t, variantResultOf("failed", nil, "", at).Error)
}

type planWriteTx struct {
	pgx.Tx
	sql []string
}

func (t *planWriteTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	t.sql = append(t.sql, sql)
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

// TestWBK011: an explain made with workbook settings or hints stays off the
// plan history; a plain one is filed as before.
func TestWBK011_WorkbookPlansStayOffPlanHistory(t *testing.T) {
	queryID := int64(5)
	result := json.RawMessage(`{"plan":[{"Plan":{"Total Cost":1}}],"plan_hash":"h","datname":"app"}`)
	for name, tc := range map[string]struct {
		args   command.Args
		writes bool
	}{
		"plain":    {command.Args{QueryID: &queryID}, true},
		"settings": {command.Args{QueryID: &queryID, Settings: map[string]string{"enable_seqscan": "off"}}, false},
		"hints":    {command.Args{QueryID: &queryID, Hints: "SeqScan(t)"}, false},
	} {
		argsJSON, err := json.Marshal(tc.args)
		require.NoError(t, err)
		tx := &planWriteTx{}
		require.NoError(t, persistCommandArtifact(context.Background(), tx, "default", uuid.New(), 1, uuid.New(), command.KindExplain, argsJSON, result, time.Now()), name)
		wrote := len(tx.sql) == 1 && strings.Contains(tx.sql[0], "INSERT INTO query_plans")
		require.Equal(t, tc.writes, wrote, name)
		if !tc.writes {
			require.Empty(t, tx.sql, name)
		}
	}
}

// TestWBK012: replacing a variant clears its last result and status.
func TestWBK012_EditResetsTheVariant(t *testing.T) {
	m := newMemWorkbooks()
	var calls []enqueued
	h := workbookRouter(m, &calls, nil)
	book := createTestWorkbook(t, h, uuid.New())
	vid := book.Variants[0].VariantID
	base := "/api/v1/workbooks/" + book.WorkbookID.String() + "/variants/" + vid.String()
	require.Equal(t, http.StatusAccepted, doJSON(t, h, http.MethodPost, base+"/run", "").Code)

	rec := doJSON(t, h, http.MethodPut, base, `{"name":"Baseline","settings":{"enable_hashjoin":"off"}}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var v variantJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &v))
	require.Equal(t, "never", v.Status)
	require.Nil(t, v.CommandID)
	require.Nil(t, v.TotalCost)
	require.Equal(t, map[string]string{"enable_hashjoin": "off"}, v.Settings)
	require.Equal(t, http.StatusAccepted, doJSON(t, h, http.MethodPost, base+"/run", "").Code, "an edited variant runs again")

	require.Equal(t, http.StatusNoContent, doJSON(t, h, http.MethodDelete, base, "").Code)
	require.Equal(t, http.StatusNotFound, doJSON(t, h, http.MethodPut, base, `{"name":"x"}`).Code)
	require.Equal(t, http.StatusNoContent, doJSON(t, h, http.MethodDelete, "/api/v1/workbooks/"+book.WorkbookID.String(), "").Code)
	require.Equal(t, http.StatusNotFound, doJSON(t, h, http.MethodGet, "/api/v1/workbooks/"+book.WorkbookID.String(), "").Code)
}

func TestWorkbookRoutes_UnavailableWithoutPoolAndBadQueries(t *testing.T) {
	r := chi.NewRouter()
	(&CommandService{}).registerWorkbookRoutes(r)
	require.Equal(t, http.StatusServiceUnavailable, doJSON(t, r, http.MethodGet, "/api/v1/workbooks?instance_id="+uuid.NewString(), "").Code)

	m := newMemWorkbooks()
	h := workbookRouter(m, &[]enqueued{}, nil)
	instanceID := uuid.New()
	book := createTestWorkbook(t, h, instanceID)
	createTestWorkbook(t, h, uuid.New())
	require.Equal(t, http.StatusBadRequest, doJSON(t, h, http.MethodGet, "/api/v1/workbooks", "").Code)
	require.Equal(t, http.StatusBadRequest, doJSON(t, h, http.MethodGet, "/api/v1/workbooks?instance_id="+instanceID.String()+"&queryid=abc", "").Code)
	require.Equal(t, http.StatusBadRequest, doJSON(t, h, http.MethodGet, "/api/v1/workbooks/nope", "").Code)
	rec := doJSON(t, h, http.MethodGet, "/api/v1/workbooks?instance_id="+instanceID.String()+"&queryid=-77", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var list struct{ Workbooks []workbookJSON }
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list.Workbooks, 1)

	rec = doJSON(t, h, http.MethodPatch, "/api/v1/workbooks/"+book.WorkbookID.String(), `{"notes":"cost doubles"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "cost doubles")
	require.Equal(t, http.StatusBadRequest, doJSON(t, h, http.MethodPatch, "/api/v1/workbooks/"+book.WorkbookID.String(), `{"name":""}`).Code)
	require.Equal(t, http.StatusBadRequest, doJSON(t, h, http.MethodPatch, "/api/v1/workbooks/"+book.WorkbookID.String(), `{"notes":"`+strings.Repeat("x", store.MaxWorkbookNotesRunes+1)+`"}`).Code)
	require.Equal(t, http.StatusNotFound, doJSON(t, h, http.MethodPatch, "/api/v1/workbooks/"+uuid.NewString(), `{"notes":"x"}`).Code)
}
