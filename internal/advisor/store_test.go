package advisor

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"
)

func TestPgStore_PersistsAndQueriesFindings(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)

	store := &PgStore{db: mock}
	ctx := context.Background()
	now := time.Unix(100, 0).UTC()
	instanceID := uuid.New()
	clusterID := int64(42)

	mock.ExpectQuery("SELECT instance_id FROM instances").WithArgs(now).WillReturnRows(
		pgxmock.NewRows([]string{"instance_id"}).AddRow(instanceID),
	)
	ids, err := store.ActiveInstances(ctx, now)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{instanceID}, ids)

	finding := Finding{
		RuleID: "txn.long_running", Severity: SeverityWarning, State: StateOpen, Scope: ScopeInstance,
		ClusterID: &clusterID, InstanceID: &instanceID, Title: "long transaction", Detail: "too old",
		Evidence: map[string]any{"age": 12},
	}
	mock.ExpectExec("INSERT INTO findings").WithArgs(
		pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
		pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
		pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	require.NoError(t, store.UpsertFinding(ctx, finding, now))

	mock.ExpectExec("UPDATE findings").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	require.NoError(t, store.ResolveAbsent(ctx, instanceID, []string{"txn.long_running"}, []string{finding.ID()}, now))

	mock.ExpectExec("DELETE FROM findings").WithArgs(pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("DELETE", 1))
	require.NoError(t, store.PurgeResolved(ctx, now))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPgStore_NewAndNullHelpers(t *testing.T) {
	require.NotNil(t, NewPgStore(nil))
	require.Nil(t, nullIfEmpty(""))
	require.Equal(t, "value", nullIfEmpty("value"))
}
