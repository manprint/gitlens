package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/command"
	"github.com/manprint/pglens/internal/indexadvice"
)

// recordingCommandTx records the statements of a command result.
type recordingCommandTx struct {
	commandFakeTx
	sql     []string
	args    [][]any
	rowSQL  []string
	rowArgs [][]any
}

func (t *recordingCommandTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.execCalls++
	t.sql = append(t.sql, sql)
	t.args = append(t.args, args)
	return pgconn.NewCommandTag("OK"), nil
}

func (t *recordingCommandTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	t.rowSQL = append(t.rowSQL, sql)
	t.rowArgs = append(t.rowArgs, args)
	return t.commandFakeTx.QueryRow(ctx, sql, args...)
}

const indexCheckArgsJSON = `{"queryid":"77","datname":"app","index_schema":"public","index_relation":"orders","index_columns":["customer_id","created_at"]}`

func postIndexCheckResult(t *testing.T, body string) *recordingCommandTx {
	t.Helper()
	instanceID := uuid.New()
	tx := &recordingCommandTx{commandFakeTx: commandFakeTx{row: &mockRow{vals: []any{"default", []byte(instanceID.String()), int64(3), command.KindIndexCheck, []byte(indexCheckArgsJSON), "api"}}}}
	s := NewCommandService(&commandFakePool{tx: tx}, NewAuth("secret"))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/commands/"+uuid.NewString()+"/result", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	commandRouter(s).ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.True(t, tx.committed)
	return tx
}

func validationWrites(tx *recordingCommandTx) [][]any {
	var out [][]any
	for i, sql := range tx.sql {
		if strings.Contains(sql, "INSERT INTO index_validations") {
			out = append(out, tx.args[i])
		}
	}
	return out
}

// TestIDXV009: a successful index_check result becomes one index_validations
// row; a failed command stores none.
func TestIDXV009(t *testing.T) {
	claim := uuid.NewString()
	tx := postIndexCheckResult(t, `{"claim_token":"`+claim+`","outcome":"ok","result":{"datname":"app","index_name":"<1>btree_orders","index_size_bytes":8192,"cost_before":1693,"cost_after":8.3,"index_used":true,"plan_after":[{"Plan":{"Node Type":"Index Scan"}}]}}`)
	writes := validationWrites(tx)
	require.Len(t, writes, 1)
	args := writes[0]
	require.Equal(t, "default", args[0])
	require.Equal(t, int64(3), args[2])
	require.Equal(t, "app", args[3])
	require.Equal(t, int64(77), args[4])
	require.Equal(t, "public", args[5])
	require.Equal(t, "orders", args[6])
	require.Equal(t, []string{"customer_id", "created_at"}, args[7])
	require.Equal(t, []string{}, args[8])
	require.Equal(t, true, args[9])
	require.InDelta(t, 1693.0, args[10], 1e-9)
	require.InDelta(t, 8.3, args[11], 1e-9)
	require.Equal(t, int64(8192), *(args[12].(*int64)))
	require.JSONEq(t, `[{"Plan":{"Node Type":"Index Scan"}}]`, string(args[13].(json.RawMessage)))

	tx = postIndexCheckResult(t, `{"claim_token":"`+claim+`","outcome":"rejected","error":"column_not_found: table \"public\".\"orders\" has no column \"x\""}`)
	require.Empty(t, validationWrites(tx))
}

// TestIDXV006: an agent that predates index_check answers with an error; the
// command is stored as failed with that text and nothing is validated.
func TestIDXV006(t *testing.T) {
	const oldAgent = `unknown command kind "index_check"`
	tx := postIndexCheckResult(t, `{"claim_token":"`+uuid.NewString()+`","outcome":"rejected","error":"unknown command kind \"index_check\""}`)
	require.Empty(t, validationWrites(tx))
	var update []any
	for i, sql := range tx.rowSQL {
		if strings.HasPrefix(sql, "UPDATE commands SET state=") {
			update = tx.rowArgs[i]
		}
	}
	require.NotNil(t, update)
	require.Equal(t, "failed", update[2])
	require.Equal(t, oldAgent, update[4])

	s := indexadvice.Suggestion{Database: "app", Schema: "public", Table: "orders", Columns: []indexadvice.KeyColumn{{Name: "customer_id"}}}
	require.Nil(t, suggestionValidation(s, nil))
}

// TestIDXV010: a suggestion takes the newest validation of the same database,
// table and key columns in the same order; descending columns make it
// approximate.
func TestIDXV010(t *testing.T) {
	size := int64(8192)
	t0 := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	vs := []storedValidation{
		{Datname: "app", Schema: "public", Relname: "orders", Columns: []string{"created_at", "customer_id"}, QueryID: 1, IndexUsed: true, CostBefore: 100, CostAfter: 1, ValidatedAt: t0.Add(3 * time.Hour)},
		{Datname: "app", Schema: "public", Relname: "orders", Columns: []string{"customer_id", "created_at"}, QueryID: 2, IndexUsed: false, CostBefore: 100, CostAfter: 100, ValidatedAt: t0},
		{Datname: "app", Schema: "public", Relname: "orders", Columns: []string{"customer_id", "created_at"}, QueryID: 3, IndexUsed: true, CostBefore: 200, CostAfter: 50, IndexSizeBytes: &size, ValidatedAt: t0.Add(time.Hour)},
		{Datname: "app", Schema: "public", Relname: "orders", Columns: []string{"customer_id", "created_at"}, Include: []string{"total"}, QueryID: 4, IndexUsed: true, CostBefore: 1, CostAfter: 1, ValidatedAt: t0.Add(5 * time.Hour)},
		{Datname: "other", Schema: "public", Relname: "orders", Columns: []string{"customer_id", "created_at"}, QueryID: 5, ValidatedAt: t0.Add(6 * time.Hour)},
	}
	s := indexadvice.Suggestion{Database: "app", Schema: "public", Table: "orders", Columns: []indexadvice.KeyColumn{{Name: "customer_id"}, {Name: "created_at"}}}
	got := suggestionValidation(s, vs)
	require.NotNil(t, got)
	require.Equal(t, int64(3), got.QueryID)
	require.True(t, got.IndexUsed)
	require.InDelta(t, 0.25, *got.CostRatio, 1e-9)
	require.Equal(t, &size, got.IndexSizeBytes)
	require.Equal(t, t0.Add(time.Hour), got.ValidatedAt)
	require.False(t, got.Approximate)

	s.Columns[1].Desc = true
	require.True(t, suggestionValidation(s, vs).Approximate)

	s.Columns = []indexadvice.KeyColumn{{Name: "customer_id"}}
	require.Nil(t, suggestionValidation(s, vs))

	zero := []storedValidation{{Datname: "app", Schema: "public", Relname: "orders", Columns: []string{"customer_id"}, CostBefore: 0, CostAfter: 0, ValidatedAt: t0}}
	got = suggestionValidation(s, zero)
	require.NotNil(t, got)
	require.Nil(t, got.CostRatio)

	body, err := json.Marshal(indexSuggestionJSON{Suggestion: s, Validation: got})
	require.NoError(t, err)
	require.Contains(t, string(body), `"validation":{"queryid":"0","index_used":false,"cost_before":0,"cost_after":0,"cost_ratio":null,"index_size_bytes":null,"validated_at":"2026-11-21T10:00:00Z","approximate":false}`)
}
