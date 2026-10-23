package alert

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/manprint/pglens/internal/alert/notify"
	"github.com/manprint/pglens/internal/clock"
	"github.com/pashagolub/pgxmock/v5"
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

// markStore keeps the outcome and attempt count the notifier records for each
// delivery, in order.
type markStore struct {
	engineStore
	marks []notificationMark
}

type notificationMark struct {
	phase    string
	ok       bool
	attempts int
}

func (s *markStore) MarkNotification(_ context.Context, _, _, phase string, ok bool, attempts int, _ error) error {
	s.marks = append(s.marks, notificationMark{phase: phase, ok: ok, attempts: attempts})
	return nil
}

// TestChannelNotifier_AttemptsEveryChannel — a channel that fails must not keep
// the channels after it from delivering the same transition.
func TestChannelNotifier_AttemptsEveryChannel(t *testing.T) {
	a := Alert{Key: "k", RuleID: "r", Severity: SeverityWarning, State: StateFiring, StartedAt: time.Unix(1, 0), LastEvalAt: time.Unix(1, 0)}
	store := &markStore{}
	slack := &testChannel{name: "slack", err: errors.New("HTTP 503")}
	webhook := &testChannel{name: "webhook"}
	err := NewChannelNotifier(store, slack, webhook).Notify(context.Background(), a)
	require.ErrorContains(t, err, "channel slack")
	require.Equal(t, 1, webhook.sent, "the healthy channel must still deliver")
}

// TestChannelNotifier_RetriesAFailedDeliveryOnALaterPass — a delivery that failed
// is made again on a later pass, not in the pass that failed, and once it has
// been delivered it is not sent again.
func TestChannelNotifier_RetriesAFailedDeliveryOnALaterPass(t *testing.T) {
	a := Alert{Key: "k", RuleID: "r", Severity: SeverityWarning, State: StateFiring, StartedAt: time.Unix(1, 0), LastEvalAt: time.Unix(1, 0)}
	store := &markStore{}
	slack := &testChannel{name: "slack", err: errors.New("HTTP 503")}
	n := NewChannelNotifier(store, slack)
	retrier, ok := n.(interface{ Retry(context.Context) error })
	require.True(t, ok, "the channel notifier must retry failed deliveries")

	require.Error(t, n.Notify(context.Background(), a))
	require.NoError(t, retrier.Retry(context.Background()))
	require.Equal(t, 1, slack.sent, "a failure is not retried in the pass that made it")

	slack.err = nil
	require.NoError(t, retrier.Retry(context.Background()))
	require.Equal(t, 2, slack.sent)
	require.NoError(t, retrier.Retry(context.Background()))
	require.Equal(t, 2, slack.sent, "a delivered message is not sent again")
	require.Equal(t, []notificationMark{{phase: "fire", ok: false, attempts: 1}, {phase: "fire", ok: true, attempts: 2}}, store.marks)
}

// TestChannelNotifier_GivesUpAfterItsAttempts — a channel that stays down is not
// hit forever.
func TestChannelNotifier_GivesUpAfterItsAttempts(t *testing.T) {
	a := Alert{Key: "k", RuleID: "r", Severity: SeverityWarning, State: StateFiring, StartedAt: time.Unix(1, 0), LastEvalAt: time.Unix(1, 0)}
	slack := &testChannel{name: "slack", err: errors.New("HTTP 503")}
	n := NewChannelNotifier(&markStore{}, slack)
	retrier, ok := n.(interface{ Retry(context.Context) error })
	require.True(t, ok, "the channel notifier must retry failed deliveries")
	require.Error(t, n.Notify(context.Background(), a))
	for i := 0; i < 3*maxNotifyAttempts; i++ {
		_ = retrier.Retry(context.Background())
	}
	require.Equal(t, maxNotifyAttempts, slack.sent)
}

// TestTick_RetriesAFailedNotificationOnALaterPass — the engine makes the
// deliveries that failed on an earlier pass again, without a new transition.
func TestTick_RetriesAFailedNotificationOnALaterPass(t *testing.T) {
	now := time.Unix(100, 0)
	rule := Rule{ID: "custom", Enabled: true, Severity: SeverityWarning, Scope: ScopeInstance, Metric: "x", Comparator: GT, Threshold: 1, Summary: "x"}
	store := &engineStore{rules: []Rule{rule}}
	slack := &testChannel{name: "slack", err: errors.New("HTTP 503")}
	e := NewEngine(nil, clock.NewFake(now), store, NewChannelNotifier(store, slack), []Source{engineSource{kind: "metric", samples: []Sample{{Value: 2, TS: now}}}})
	require.ErrorContains(t, e.Tick(context.Background()), "channel slack")

	slack.err = nil
	require.NoError(t, e.Tick(context.Background()))
	require.Equal(t, 2, slack.sent)
}
