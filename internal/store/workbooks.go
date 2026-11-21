package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Queryer is what the workbook functions need from a pool or a transaction.
type Queryer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Workbook limits (plan 007 phase 13).
const (
	MaxWorkbooksPerInstance = 100
	MaxVariantsPerWorkbook  = 20
	MaxWorkbookNameRunes    = 120
	MaxWorkbookNotesRunes   = 4000
	BaselineVariantName     = "Baseline"
)

var (
	// ErrNotFound is returned for a workbook or variant that does not exist
	// in the tenant.
	ErrNotFound = errors.New("not found")
	// ErrLimitReached is returned when a workbook or variant limit is full.
	ErrLimitReached = errors.New("limit reached")
)

// Workbook is one query's set of explain variants.
type Workbook struct {
	ID         uuid.UUID
	TenantID   string
	InstanceID uuid.UUID
	Datname    string
	QueryID    int64
	Name       string
	Notes      string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Variants   []WorkbookVariant
}

// WorkbookVariant is one explain variant and the result of its latest run.
type WorkbookVariant struct {
	ID          uuid.UUID
	WorkbookID  uuid.UUID
	Position    int
	Name        string
	Settings    map[string]string
	Hints       string
	Params      []*string
	Analyze     bool
	Status      string
	CommandID   *uuid.UUID
	Error       string
	Plan        json.RawMessage
	PlanHash    *string
	TotalCost   *float64
	PlanningMS  *float64
	ExecutionMS *float64
	RanAt       *time.Time
}

// VariantResult is what a finished command leaves on its variant.
type VariantResult struct {
	Status      string
	Error       string
	Plan        json.RawMessage
	PlanHash    *string
	TotalCost   *float64
	PlanningMS  *float64
	ExecutionMS *float64
	RanAt       time.Time
}

const workbookColumns = `w.workbook_id, w.tenant_id, w.instance_id, w.datname, w.queryid, w.name, w.notes, w.created_at, w.updated_at`

func scanWorkbook(row pgx.Row) (Workbook, error) {
	var w Workbook
	err := row.Scan(&w.ID, &w.TenantID, &w.InstanceID, &w.Datname, &w.QueryID, &w.Name, &w.Notes, &w.CreatedAt, &w.UpdatedAt)
	return w, err
}

// variantColumns lists a variant's columns; withPlan false leaves the plan
// out (the list route does not ship plans).
func variantColumns(withPlan bool) string {
	plan := "NULL::jsonb"
	if withPlan {
		plan = "v.plan"
	}
	return `v.variant_id, v.workbook_id, v.position, v.name, v.settings, v.hints, v.params, v."analyze", v.status, v.command_id, v.error, ` + plan + `, v.plan_hash, v.total_cost, v.planning_ms, v.execution_ms, v.ran_at`
}

func scanVariant(row pgx.Row) (WorkbookVariant, error) {
	var v WorkbookVariant
	var settings, params, plan []byte
	err := row.Scan(&v.ID, &v.WorkbookID, &v.Position, &v.Name, &settings, &v.Hints, &params, &v.Analyze, &v.Status, &v.CommandID, &v.Error, &plan, &v.PlanHash, &v.TotalCost, &v.PlanningMS, &v.ExecutionMS, &v.RanAt)
	if err != nil {
		return v, err
	}
	v.Settings = map[string]string{}
	if err := json.Unmarshal(settings, &v.Settings); err != nil {
		return v, fmt.Errorf("decode variant settings: %w", err)
	}
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &v.Params); err != nil {
			return v, fmt.Errorf("decode variant params: %w", err)
		}
	}
	if len(plan) > 0 {
		v.Plan = plan
	}
	return v, nil
}

func variantsOf(ctx context.Context, q Queryer, withPlan bool, where string, args ...any) ([]WorkbookVariant, error) {
	rows, err := q.Query(ctx, `SELECT `+variantColumns(withPlan)+` FROM workbook_variants v JOIN workbooks w USING (workbook_id) WHERE `+where+` ORDER BY v.workbook_id, v.position`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkbookVariant
	for rows.Next() {
		v, err := scanVariant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ListWorkbooks returns an instance's workbooks, of one query when queryID is
// set, newest first, with their variants but without plans.
func ListWorkbooks(ctx context.Context, q Queryer, tenantID string, instanceID uuid.UUID, queryID *int64) ([]Workbook, error) {
	rows, err := q.Query(ctx, `SELECT `+workbookColumns+` FROM workbooks w
		WHERE w.tenant_id=$1 AND w.instance_id=$2 AND ($3::bigint IS NULL OR w.queryid=$3)
		ORDER BY w.updated_at DESC, w.workbook_id`, tenantID, instanceID, queryID)
	if err != nil {
		return nil, err
	}
	var out []Workbook
	index := map[uuid.UUID]int{}
	for rows.Next() {
		w, err := scanWorkbook(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		w.Variants = []WorkbookVariant{}
		index[w.ID] = len(out)
		out = append(out, w)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return []Workbook{}, nil
	}
	variants, err := variantsOf(ctx, q, false, `w.tenant_id=$1 AND w.instance_id=$2 AND ($3::bigint IS NULL OR w.queryid=$3)`, tenantID, instanceID, queryID)
	if err != nil {
		return nil, err
	}
	for _, v := range variants {
		if i, ok := index[v.WorkbookID]; ok {
			out[i].Variants = append(out[i].Variants, v)
		}
	}
	return out, nil
}

// GetWorkbook returns a workbook with its variants and their plans.
func GetWorkbook(ctx context.Context, q Queryer, tenantID string, id uuid.UUID) (Workbook, error) {
	w, err := scanWorkbook(q.QueryRow(ctx, `SELECT `+workbookColumns+` FROM workbooks w WHERE w.tenant_id=$1 AND w.workbook_id=$2`, tenantID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return w, ErrNotFound
	}
	if err != nil {
		return w, err
	}
	w.Variants, err = variantsOf(ctx, q, true, `w.tenant_id=$1 AND w.workbook_id=$2`, tenantID, id)
	if w.Variants == nil {
		w.Variants = []WorkbookVariant{}
	}
	return w, err
}

// CreateWorkbook stores a workbook and its Baseline variant (no settings, no
// hints, no parameters). The instance must exist in the tenant
// (ErrNotFound), and the instance's workbooks must be under the limit
// (ErrLimitReached).
func CreateWorkbook(ctx context.Context, q Queryer, w Workbook) (Workbook, error) {
	var exists bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM instances WHERE tenant_id=$1 AND instance_id=$2)`, w.TenantID, w.InstanceID).Scan(&exists); err != nil {
		return w, err
	}
	if !exists {
		return w, ErrNotFound
	}
	var id uuid.UUID
	err := q.QueryRow(ctx, `WITH w AS (
		INSERT INTO workbooks (tenant_id, instance_id, datname, queryid, name, notes)
		SELECT $1, $2, $3, $4, $5, $6
		 WHERE (SELECT count(*) FROM workbooks WHERE tenant_id=$1 AND instance_id=$2) < $7
		RETURNING workbook_id),
		v AS (INSERT INTO workbook_variants (workbook_id, position, name) SELECT workbook_id, 0, $8 FROM w)
		SELECT workbook_id FROM w`,
		w.TenantID, w.InstanceID, w.Datname, w.QueryID, w.Name, w.Notes, MaxWorkbooksPerInstance, BaselineVariantName).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return w, ErrLimitReached
	}
	if err != nil {
		return w, err
	}
	return GetWorkbook(ctx, q, w.TenantID, id)
}

// UpdateWorkbook changes the name and/or the notes.
func UpdateWorkbook(ctx context.Context, q Queryer, tenantID string, id uuid.UUID, name, notes *string) (Workbook, error) {
	tag, err := q.Exec(ctx, `UPDATE workbooks SET name=COALESCE($3, name), notes=COALESCE($4, notes), updated_at=now()
		WHERE tenant_id=$1 AND workbook_id=$2`, tenantID, id, name, notes)
	if err != nil {
		return Workbook{}, err
	}
	if tag.RowsAffected() == 0 {
		return Workbook{}, ErrNotFound
	}
	return GetWorkbook(ctx, q, tenantID, id)
}

// DeleteWorkbook removes a workbook; its variants go with it.
func DeleteWorkbook(ctx context.Context, q Queryer, tenantID string, id uuid.UUID) error {
	tag, err := q.Exec(ctx, `DELETE FROM workbooks WHERE tenant_id=$1 AND workbook_id=$2`, tenantID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func variantJSON(v WorkbookVariant) ([]byte, []byte, error) {
	settings := v.Settings
	if settings == nil {
		settings = map[string]string{}
	}
	s, err := json.Marshal(settings)
	if err != nil {
		return nil, nil, err
	}
	var p []byte
	if v.Params != nil {
		if p, err = json.Marshal(v.Params); err != nil {
			return nil, nil, err
		}
	}
	return s, p, nil
}

func getVariant(ctx context.Context, q Queryer, tenantID string, workbookID, variantID uuid.UUID) (WorkbookVariant, error) {
	v, err := scanVariant(q.QueryRow(ctx, `SELECT `+variantColumns(true)+` FROM workbook_variants v JOIN workbooks w USING (workbook_id)
		WHERE w.tenant_id=$1 AND v.workbook_id=$2 AND v.variant_id=$3`, tenantID, workbookID, variantID))
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	return v, err
}

// GetVariant returns one variant of a workbook.
func GetVariant(ctx context.Context, q Queryer, tenantID string, workbookID, variantID uuid.UUID) (WorkbookVariant, error) {
	return getVariant(ctx, q, tenantID, workbookID, variantID)
}

// AddVariant appends a variant to a workbook (ErrNotFound, ErrLimitReached).
func AddVariant(ctx context.Context, q Queryer, tenantID string, workbookID uuid.UUID, v WorkbookVariant) (WorkbookVariant, error) {
	settings, params, err := variantJSON(v)
	if err != nil {
		return v, err
	}
	var id uuid.UUID
	err = q.QueryRow(ctx, `INSERT INTO workbook_variants (workbook_id, position, name, settings, hints, params, "analyze")
		SELECT w.workbook_id,
		       COALESCE((SELECT max(position) + 1 FROM workbook_variants WHERE workbook_id = w.workbook_id), 0),
		       $3, $4, $5, $6, $7
		  FROM workbooks w
		 WHERE w.tenant_id=$1 AND w.workbook_id=$2
		   AND (SELECT count(*) FROM workbook_variants WHERE workbook_id = w.workbook_id) < $8
		RETURNING variant_id`, tenantID, workbookID, v.Name, settings, v.Hints, params, v.Analyze, MaxVariantsPerWorkbook).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, getErr := GetWorkbook(ctx, q, tenantID, workbookID); getErr != nil {
			return v, getErr
		}
		return v, ErrLimitReached
	}
	if err != nil {
		return v, err
	}
	if _, err := q.Exec(ctx, `UPDATE workbooks SET updated_at=now() WHERE workbook_id=$1`, workbookID); err != nil {
		return v, err
	}
	return getVariant(ctx, q, tenantID, workbookID, id)
}

// UpdateVariant replaces a variant's definition. Its status goes back to
// never and its result columns are cleared: the old plan no longer belongs
// to it.
func UpdateVariant(ctx context.Context, q Queryer, tenantID string, workbookID, variantID uuid.UUID, v WorkbookVariant) (WorkbookVariant, error) {
	settings, params, err := variantJSON(v)
	if err != nil {
		return v, err
	}
	tag, err := q.Exec(ctx, `UPDATE workbook_variants v
		   SET name=$4, settings=$5, hints=$6, params=$7, "analyze"=$8,
		       status='never', command_id=NULL, error='', plan=NULL, plan_hash=NULL,
		       total_cost=NULL, planning_ms=NULL, execution_ms=NULL, ran_at=NULL
		  FROM workbooks w
		 WHERE w.workbook_id = v.workbook_id AND w.tenant_id=$1 AND v.workbook_id=$2 AND v.variant_id=$3`,
		tenantID, workbookID, variantID, v.Name, settings, v.Hints, params, v.Analyze)
	if err != nil {
		return v, err
	}
	if tag.RowsAffected() == 0 {
		return v, ErrNotFound
	}
	if _, err := q.Exec(ctx, `UPDATE workbooks SET updated_at=now() WHERE workbook_id=$1`, workbookID); err != nil {
		return v, err
	}
	return getVariant(ctx, q, tenantID, workbookID, variantID)
}

// DeleteVariant removes one variant.
func DeleteVariant(ctx context.Context, q Queryer, tenantID string, workbookID, variantID uuid.UUID) error {
	tag, err := q.Exec(ctx, `DELETE FROM workbook_variants v USING workbooks w
		WHERE w.workbook_id = v.workbook_id AND w.tenant_id=$1 AND v.workbook_id=$2 AND v.variant_id=$3`, tenantID, workbookID, variantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkVariantPending links a queued command to its variant. It reports false
// when the variant is already pending (another run won the race).
func MarkVariantPending(ctx context.Context, q Queryer, tenantID string, workbookID, variantID, commandID uuid.UUID) (bool, error) {
	tag, err := q.Exec(ctx, `UPDATE workbook_variants v SET status='pending', command_id=$4, error=''
		  FROM workbooks w
		 WHERE w.workbook_id = v.workbook_id AND w.tenant_id=$1 AND v.workbook_id=$2 AND v.variant_id=$3 AND v.status <> 'pending'`,
		tenantID, workbookID, variantID, commandID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// LinkVariantResult files a finished command's result on the variant that
// queued it, if any. A variant edited since the run no longer carries the
// command id, so a late result does not overwrite the new definition.
func LinkVariantResult(ctx context.Context, q Queryer, commandID uuid.UUID, r VariantResult) error {
	_, err := q.Exec(ctx, `UPDATE workbook_variants
		   SET status=$2, error=$3, plan=$4, plan_hash=$5, total_cost=$6, planning_ms=$7, execution_ms=$8, ran_at=$9
		 WHERE command_id=$1 AND status='pending'`,
		commandID, r.Status, r.Error, nullableRaw(r.Plan), r.PlanHash, r.TotalCost, r.PlanningMS, r.ExecutionMS, r.RanAt.UTC())
	return err
}

func nullableRaw(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return []byte(raw)
}
