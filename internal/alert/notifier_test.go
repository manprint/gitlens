package alert

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/manprint/pglens/internal/alert/notify"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"
)

type testChannel struct {
	name string
	err  error
	sent int
}

func (c *testChannel) Name() string                               { return c.name }
func (c *testChannel) Send(context.Context, notify.Message) error { c.sent++; return c.err }
func TestChannelNotifier_ClaimsMarksAndDeduplicates(t *testing.T) {
	mock, s := mockStore(t)
	a := Alert{Key: "k", RuleID: "r", Severity: SeverityWarning, State: StateFiring, StartedAt: time.Unix(1, 0), LastEvalAt: time.Unix(1, 0)}
	mock.ExpectQuery("INSERT INTO notifications").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnRows(pgxmock.NewRows([]string{"notification_id"}).AddRow(int64(1)))
	mock.ExpectExec("UPDATE notifications").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	ch := &testChannel{name: "test"}
	require.NoError(t, NewChannelNotifier(s, ch).Notify(context.Background(), a))
	require.Equal(t, 1, ch.sent)
	require.NoError(t, mock.ExpectationsWereMet())
}
func TestChannelNotifier_ErrorAndAlreadyClaimed(t *testing.T) {
	mock, s := mockStore(t)
	a := Alert{Key: "k", StartedAt: time.Unix(1, 0), LastEvalAt: time.Unix(1, 0)}
	mock.ExpectQuery("INSERT INTO notifications").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnError(errors.New("db"))
	require.Error(t, NewChannelNotifier(s, &testChannel{name: "test"}).Notify(context.Background(), a))
	require.NoError(t, mock.ExpectationsWereMet())
	mock.ExpectQuery("INSERT INTO notifications").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnRows(pgxmock.NewRows([]string{"notification_id"}))
	ch := &testChannel{name: "test"}
	require.NoError(t, NewChannelNotifier(s, ch).Notify(context.Background(), a))
	require.Zero(t, ch.sent)
	require.NoError(t, mock.ExpectationsWereMet())
}
