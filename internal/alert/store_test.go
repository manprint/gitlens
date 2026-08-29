package alert

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"
)

func mockStore(t *testing.T) (pgxmock.PgxPoolIface, *pgStore) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() { mock.Close() })
	return mock, &pgStore{pool: mock}
}

func TestPgStore_RulesAndSilences(t *testing.T) {
	mock, s := mockStore(t)
	mock.ExpectQuery("SELECT rule_id").WillReturnRows(pgxmock.NewRows([]string{"rule_id", "enabled", "severity", "scope", "metric", "comparator", "threshold", "for_seconds", "event_type", "summary"}).AddRow("r", true, "warning", "instance", "x", "gt", 2.0, 5, nil, "summary"))
	rules, err := s.Rules(context.Background())
	require.NoError(t, err)
	require.Len(t, rules, 1)
	require.Equal(t, 5*time.Second, rules[0].For)
	id := uuid.New()
	raw := []byte(`[{"name":"rule_id","value":"r"}]`)
	mock.ExpectQuery("SELECT silence_id").WithArgs(pgxmock.AnyArg()).WillReturnRows(pgxmock.NewRows([]string{"silence_id", "matchers", "reason", "starts_at", "ends_at"}).AddRow(id, raw, "maintenance", time.Unix(0, 0), time.Unix(10, 0)))
	silences, err := s.Silences(context.Background(), time.Unix(1, 0))
	require.NoError(t, err)
	require.Len(t, silences, 1)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPgStore_UpsertClaimMark(t *testing.T) {
	mock, s := mockStore(t)
	now := time.Unix(1, 0)
	a := Alert{Key: "k", RuleID: "r", Severity: SeverityWarning, State: StateFiring, Summary: "s", StartedAt: now, LastEvalAt: now, Labels: map[string]string{"x": "y"}}
	mock.ExpectExec("INSERT INTO alerts").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	require.NoError(t, s.Upsert(context.Background(), a))
	mock.ExpectQuery("INSERT INTO notifications").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnRows(pgxmock.NewRows([]string{"notification_id"}).AddRow(int64(1)))
	won, err := s.ClaimNotification(context.Background(), "k", "slack", "fire", a)
	require.NoError(t, err)
	require.True(t, won)
	mock.ExpectQuery("INSERT INTO notifications").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnError(errors.New("duplicate"))
	_, err = s.ClaimNotification(context.Background(), "k", "slack", "fire", a)
	require.Error(t, err)
	mock.ExpectExec("UPDATE notifications").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	require.NoError(t, s.MarkNotification(context.Background(), "k", "slack", "fire", true, 1, nil))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPgStore_Active(t *testing.T) {
	mock, s := mockStore(t)
	raw := []byte(`{"x":"y"}`)
	now := time.Unix(1, 0)
	mock.ExpectQuery("SELECT alert_key").WithArgs(pgxmock.AnyArg()).WillReturnRows(pgxmock.NewRows([]string{"alert_key", "rule_id", "severity", "state", "cluster_id", "instance_id", "datname", "labels", "value", "summary", "started_at", "last_eval_at", "resolved_at"}).AddRow("k", "r", "warning", "firing", nil, nil, "db", raw, 3.0, "s", now, now, nil))
	got, err := s.Active(context.Background(), Filter{RuleID: "r"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "k", got[0].Key)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPgStore_ErrorPaths(t *testing.T) {
	mock, s := mockStore(t)
	mock.ExpectQuery("SELECT rule_id").WillReturnError(errors.New("rules unavailable"))
	_, err := s.Rules(context.Background())
	require.Error(t, err)
	a := Alert{Key: "k", StartedAt: time.Unix(1, 0)}
	mock.ExpectQuery("INSERT INTO notifications").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnError(pgx.ErrNoRows)
	won, err := s.ClaimNotification(context.Background(), "k", "slack", "fire", a)
	require.False(t, won)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
