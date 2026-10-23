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

func (s *markStore) MarkNotification(ctx context.Context, _, _, phase string, ok bool, attempts int, _ error) error {
	// Like the store, it does not write on a context that is done.
	if err := ctx.Err(); err != nil {
		return err
	}
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

// claimStore fails the first claims it is asked for and records the rest as
// won, so a transition can be persisted with no claim on record. Like pgx, it
// does not write on a context that is done.
type claimStore struct {
	markStore
	failClaims int
	calls      int
}

func (s *claimStore) ClaimNotification(ctx context.Context, _, _, _ string, _ Alert) (bool, error) {
	s.calls++
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s.failClaims > 0 {
		s.failClaims--
		return false, errors.New("claim failed")
	}
	return true, nil
}

// TestChannelNotifier_DeliversATransitionWhoseClaimFailed — a transition that is
// persisted but whose claim failed is not dropped. The next pass claims it and
// delivers it, and the failed claim counts as one of its attempts.
func TestChannelNotifier_DeliversATransitionWhoseClaimFailed(t *testing.T) {
	a := Alert{Key: "k", RuleID: "r", Severity: SeverityWarning, State: StateFiring, StartedAt: time.Unix(1, 0), LastEvalAt: time.Unix(1, 0)}
	store := &claimStore{failClaims: 1}
	slack := &testChannel{name: "slack"}
	n := NewChannelNotifier(store, slack)
	retrier, ok := n.(interface{ Retry(context.Context) error })
	require.True(t, ok, "the channel notifier must retry failed deliveries")

	require.ErrorContains(t, n.Notify(context.Background(), a), "claim failed")
	require.Zero(t, slack.sent, "nothing is sent before the claim is on record")
	require.NoError(t, retrier.Retry(context.Background()))
	require.Zero(t, slack.sent, "a claim is not made again in the pass that failed it")
	require.NoError(t, retrier.Retry(context.Background()))
	require.Equal(t, 1, slack.sent)
	require.Equal(t, []notificationMark{{phase: "fire", ok: true, attempts: 2}}, store.marks)
}

// TestChannelNotifier_ReportsAnUnclaimedDeliveryItGivesUp — a claim that fails
// on every pass is not dropped silently. When its attempts are used up the pass
// returns the drop, and the counter records it. No send happens.
func TestChannelNotifier_ReportsAnUnclaimedDeliveryItGivesUp(t *testing.T) {
	a := Alert{Key: "k", RuleID: "r", Severity: SeverityWarning, State: StateFiring, StartedAt: time.Unix(1, 0), LastEvalAt: time.Unix(1, 0)}
	store := &claimStore{failClaims: maxNotifyAttempts}
	slack := &testChannel{name: "slack"}
	n := NewChannelNotifier(store, slack)
	retrier := n.(interface{ Retry(context.Context) error })
	before := UndeliveredNotifications()

	require.ErrorContains(t, n.Notify(context.Background(), a), "claim failed")
	require.NoError(t, retrier.Retry(context.Background()), "a claim is not made again in the pass that failed it")
	for i := 2; i < maxNotifyAttempts; i++ {
		require.ErrorContains(t, retrier.Retry(context.Background()), "claim failed")
	}
	require.ErrorIs(t, retrier.Retry(context.Background()), errUndelivered)
	require.Equal(t, before+1, UndeliveredNotifications())
	require.NoError(t, retrier.Retry(context.Background()), "a given-up delivery is not retried")
	require.Zero(t, slack.sent)
	require.Empty(t, store.marks)
}

// lostReplyStore commits the first claim and loses its reply, as a connection
// that drops after the commit does. Every later claim finds the row on record.
type lostReplyStore struct {
	markStore
	claims int
}

func (s *lostReplyStore) ClaimNotification(context.Context, string, string, string, Alert) (bool, error) {
	s.claims++
	if s.claims == 1 {
		return false, errors.New("connection reset")
	}
	return false, nil
}

// TestChannelNotifier_ReportsAClaimWhoseReplyWasLost — this process never won
// the claim of a delivery whose reply it lost, so it does not send it. The pass
// reports it instead of skipping it as delivered by someone else.
func TestChannelNotifier_ReportsAClaimWhoseReplyWasLost(t *testing.T) {
	a := Alert{Key: "k", RuleID: "r", Severity: SeverityWarning, State: StateFiring, StartedAt: time.Unix(1, 0), LastEvalAt: time.Unix(1, 0)}
	store := &lostReplyStore{}
	slack := &testChannel{name: "slack"}
	n := NewChannelNotifier(store, slack)
	retrier := n.(interface{ Retry(context.Context) error })
	before := UndeliveredNotifications()

	require.ErrorContains(t, n.Notify(context.Background(), a), "connection reset")
	require.NoError(t, retrier.Retry(context.Background()), "a claim is not made again in the pass that failed it")
	require.ErrorIs(t, retrier.Retry(context.Background()), errUndelivered)
	require.Equal(t, before+1, UndeliveredNotifications())
	require.Zero(t, slack.sent)
	require.Empty(t, store.marks)
}

// TestChannelNotifier_AnExpiredClaimDoesNotUseUpAnAttempt pins the cycle check
// that comes before the store is asked: a claim that an expired cycle would start
// makes no store call, so it uses no attempt. The delivery keeps all of its
// attempts and goes out on the first pass whose cycle is live. Without that check
// the claim would reach the store, count as an attempt, and use them up.
func TestChannelNotifier_AnExpiredClaimDoesNotUseUpAnAttempt(t *testing.T) {
	a := Alert{Key: "k", RuleID: "r", Severity: SeverityWarning, State: StateFiring, StartedAt: time.Unix(1, 0), LastEvalAt: time.Unix(1, 0)}
	store := &claimStore{failClaims: 1}
	slack := &testChannel{name: "slack"}
	n := NewChannelNotifier(store, slack)
	retrier := n.(interface{ Retry(context.Context) error })
	require.Error(t, n.Notify(context.Background(), a))
	calls := store.calls

	expired, cancel := context.WithCancel(context.Background())
	cancel()
	for i := 0; i < maxNotifyAttempts+2; i++ {
		_ = retrier.Retry(expired)
	}
	require.Equal(t, calls, store.calls, "an expired cycle asks the store for no claim")
	require.Zero(t, slack.sent)

	require.NoError(t, retrier.Retry(context.Background()))
	require.Equal(t, 1, slack.sent, "the delivery is still pending after the expired passes")
	require.Equal(t, []notificationMark{{phase: "fire", ok: true, attempts: 2}}, store.marks)
}

// ctxChannel refuses to send once its context is done, as the HTTP channel does.
type ctxChannel struct {
	name string
	err  error
	sent int
}

func (c *ctxChannel) Name() string { return c.name }
func (c *ctxChannel) Send(ctx context.Context, _ notify.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.err != nil {
		return c.err
	}
	c.sent++
	return nil
}

// TestChannelNotifier_AnExpiredCycleDoesNotUseUpAnAttempt — when the cycle has
// run out of time before a retry, the channel is not blamed for the failure, so
// the delivery keeps all of its attempts.
func TestChannelNotifier_AnExpiredCycleDoesNotUseUpAnAttempt(t *testing.T) {
	a := Alert{Key: "k", RuleID: "r", Severity: SeverityWarning, State: StateFiring, StartedAt: time.Unix(1, 0), LastEvalAt: time.Unix(1, 0)}
	store := &markStore{}
	slack := &ctxChannel{name: "slack", err: errors.New("HTTP 503")}
	n := NewChannelNotifier(store, slack)
	retrier := n.(interface{ Retry(context.Context) error })
	require.Error(t, n.Notify(context.Background(), a))

	expired, cancel := context.WithCancel(context.Background())
	cancel()
	for i := 0; i < maxNotifyAttempts+2; i++ {
		_ = retrier.Retry(expired)
	}
	require.Zero(t, slack.sent)

	slack.err = nil
	require.NoError(t, retrier.Retry(context.Background()))
	require.Equal(t, 1, slack.sent, "the delivery is still pending after the expired passes")
	require.Equal(t, []notificationMark{{phase: "fire", ok: false, attempts: 1}, {phase: "fire", ok: true, attempts: 2}}, store.marks)
}

// hangingChannel accepts a send and then never answers it. The cycle ends while
// the send waits, as it does when the cycle's deadline arrives first, so the
// test ends the cycle from inside the send instead of waiting for a deadline.
type hangingChannel struct {
	name   string
	hang   bool
	calls  int
	cancel context.CancelFunc
}

func (c *hangingChannel) Name() string { return c.name }
func (c *hangingChannel) Send(ctx context.Context, _ notify.Message) error {
	c.calls++
	if !c.hang {
		return errors.New("HTTP 503")
	}
	c.cancel()
	<-ctx.Done()
	return ctx.Err()
}

// TestChannelNotifier_AHungChannelUsesUpItsAttempts — a send that starts and
// then runs into the end of the cycle counts as an attempt. Otherwise a channel
// that never answers would be kept for every later pass and never marked failed.
// Its outcome is still recorded, although the cycle has ended.
func TestChannelNotifier_AHungChannelUsesUpItsAttempts(t *testing.T) {
	a := Alert{Key: "k", RuleID: "r", Severity: SeverityWarning, State: StateFiring, StartedAt: time.Unix(1, 0), LastEvalAt: time.Unix(1, 0)}
	store := &markStore{}
	hung := &hangingChannel{name: "webhook"}
	n := NewChannelNotifier(store, hung)
	retrier := n.(interface{ Retry(context.Context) error })
	require.Error(t, n.Notify(context.Background(), a))

	hung.hang = true
	for i := 0; i < maxNotifyAttempts+1; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		hung.cancel = cancel
		_ = retrier.Retry(ctx)
		cancel()
	}
	require.Equal(t, maxNotifyAttempts, hung.calls, "the sends stop once the attempts are used up")
	last := store.marks[len(store.marks)-1]
	require.False(t, last.ok)
	require.Equal(t, maxNotifyAttempts, last.attempts)

	require.NoError(t, retrier.Retry(context.Background()))
	require.Equal(t, maxNotifyAttempts, hung.calls, "a delivery out of attempts is not sent again")
}

// phaseRecorder keeps the phase of each message the channels accepted, in the
// order they were accepted, and refuses every send while down is set. A test
// shares one recorder between its channels, so the order is the order in which
// the channel received them.
type phaseRecorder struct {
	down bool
	got  []string
}

// recordingChannel reports each accepted message to a phaseRecorder.
type recordingChannel struct{ rec *phaseRecorder }

func (c recordingChannel) Name() string { return "slack" }
func (c recordingChannel) Send(_ context.Context, m notify.Message) error {
	if c.rec.down {
		return errors.New("HTTP 503")
	}
	c.rec.got = append(c.rec.got, m.Phase)
	return nil
}

// stepSource answers the rule custom with the samples the test set for the
// pass. The builtin rules get no samples, so only custom changes state.
type stepSource struct{ samples []Sample }

func (s *stepSource) Kind() string { return "metric" }
func (s *stepSource) Samples(_ context.Context, r Rule, _ time.Time) ([]Sample, error) {
	if r.ID != "custom" {
		return nil, nil
	}
	return s.samples, nil
}

// customRule fires as soon as a sample is above 1, with no For to wait for.
func customRule() Rule {
	return Rule{ID: "custom", Enabled: true, Severity: SeverityWarning, Scope: ScopeInstance, Metric: "x", Comparator: GT, Threshold: 1, Summary: "x"}
}

// TestTick_ARetryDoesNotSendAFiringAfterItsResolve — a FIRING that the channel
// refused is not sent after the RESOLVED that the same replica sends on a later
// pass. The table shows the alert resolved, so a late FIRING would leave the
// channel showing it firing for good.
func TestTick_ARetryDoesNotSendAFiringAfterItsResolve(t *testing.T) {
	now := time.Unix(100, 0)
	store := &engineStore{rules: []Rule{customRule()}}
	rec := &phaseRecorder{down: true}
	src := &stepSource{samples: []Sample{{Value: 2, TS: now}}}
	e := NewEngine(nil, clock.NewFake(now), store, NewChannelNotifier(store, recordingChannel{rec}), []Source{src})
	require.ErrorContains(t, e.Tick(context.Background()), "channel slack")

	rec.down = false
	src.samples = []Sample{{Value: 0, TS: now}}
	require.NoError(t, e.Tick(context.Background()))
	require.Equal(t, []string{"resolve"}, rec.got)
	require.NoError(t, e.Tick(context.Background()))
	require.Equal(t, []string{"resolve"}, rec.got, "the refused FIRING is never sent")
}

// TestTick_ARefireDoesNotSendAPendingResolveAfterIt — a RESOLVED that the channel
// refused is not sent once a newer episode of the alert is firing. The channel
// is already told the newer state, and the late RESOLVED would take it back.
func TestTick_ARefireDoesNotSendAPendingResolveAfterIt(t *testing.T) {
	t0 := time.Unix(100, 0)
	store := &engineStore{rules: []Rule{customRule()}}
	rec := &phaseRecorder{}
	src := &stepSource{samples: []Sample{{Value: 2, TS: t0}}}
	e := NewEngine(nil, clock.NewFake(t0), store, NewChannelNotifier(store, recordingChannel{rec}), []Source{src})
	require.NoError(t, e.Tick(context.Background()))

	rec.down = true
	src.samples = []Sample{{Value: 0, TS: t0.Add(time.Second)}}
	require.ErrorContains(t, e.Tick(context.Background()), "channel slack")

	rec.down = false
	src.samples = []Sample{{Value: 2, TS: t0.Add(2 * time.Second)}}
	require.NoError(t, e.Tick(context.Background()))
	require.Equal(t, []string{"fire", "fire"}, rec.got, "the pending RESOLVED of the first episode is dropped, not sent after the second FIRING")
}

// TestTick_ASilencedResolveDoesNotSendAFiringItsChannelRefused — a FIRING that the
// channel refused is not sent once a silence covers the alert and the alert
// resolves. The silence keeps the RESOLVED from the channels, so nothing but Settle
// drops the refused FIRING: the alert is no longer firing, so the FIRING is obsolete.
func TestTick_ASilencedResolveDoesNotSendAFiringItsChannelRefused(t *testing.T) {
	t0 := time.Unix(100, 0)
	store := &engineStore{rules: []Rule{customRule()}}
	rec := &phaseRecorder{down: true}
	src := &stepSource{samples: []Sample{{Value: 2, TS: t0}}}
	e := NewEngine(nil, clock.NewFake(t0), store, NewChannelNotifier(store, recordingChannel{rec}), []Source{src})
	require.Error(t, e.Tick(context.Background()))

	rec.down = false
	store.silences = []Silence{{Matchers: []Matcher{{Name: "rule_id", Value: "custom"}}, StartsAt: t0.Add(-time.Second), EndsAt: t0.Add(time.Hour)}}
	src.samples = []Sample{{Value: 0, TS: t0.Add(time.Second)}}
	require.NoError(t, e.Tick(context.Background()))
	last := store.alerts[len(store.alerts)-1]
	require.Equal(t, StateResolved, last.State)
	require.True(t, last.Suppressed, "the silence covers the RESOLVED, so no channel is told of it")
	require.Empty(t, rec.got, "the refused FIRING is obsolete once the alert has resolved")
	require.NoError(t, e.Tick(context.Background()))
	require.Empty(t, rec.got, "the refused FIRING is not sent on a later pass")
}

// tableStore is the alerts table as the engine reads it across a change of
// leader. An upsert replaces the row of its episode, the start is kept to the
// microsecond as the table keeps it, and Active lists the episodes not resolved.
type tableStore struct {
	engineStore
	rows map[episodeRow]Alert
}

type episodeRow struct {
	key     string
	started int64
}

func newTableStore(rules ...Rule) *tableStore {
	return &tableStore{engineStore: engineStore{rules: rules}, rows: make(map[episodeRow]Alert)}
}

func (s *tableStore) Upsert(_ context.Context, a Alert) error {
	a.StartedAt = a.StartedAt.Truncate(time.Microsecond)
	s.rows[episodeRow{key: a.Key, started: a.StartedAt.UnixMicro()}] = a
	return nil
}

func (s *tableStore) Active(context.Context, Filter) ([]Alert, error) {
	var out []Alert
	for _, a := range s.rows {
		if a.State != StateResolved {
			out = append(out, a)
		}
	}
	return out, nil
}

// regainLeadership starts a new term on a, as ensureLeader does when it acquires
// the lock again: the evaluator is reset and the next pass reloads the table.
func regainLeadership(a *Engine) {
	a.eval.Reset()
	a.stateLoaded = false
}

// TestTick_ALeaderChangeDoesNotSendAFiringOverruledByAResolve — replica A fails to
// send a FIRING and stops leading. Replica B, a separate process with its own
// notifier, sends the RESOLVED once the channel is back. When A leads again its
// queue still holds the FIRING, which the table no longer owes, so it is dropped.
func TestTick_ALeaderChangeDoesNotSendAFiringOverruledByAResolve(t *testing.T) {
	t0 := time.Unix(100, 123456789) // nanoseconds, as a live clock gives them
	clk := clock.NewFake(t0)
	store := newTableStore(customRule())
	rec := &phaseRecorder{down: true}
	srcA := &stepSource{samples: []Sample{{Value: 2, TS: t0}}}
	srcB := &stepSource{samples: []Sample{{Value: 2, TS: t0}}}
	a := NewEngine(nil, clk, store, NewChannelNotifier(store, recordingChannel{rec}), []Source{srcA})
	b := NewEngine(nil, clk, store, NewChannelNotifier(store, recordingChannel{rec}), []Source{srcB})
	require.Error(t, a.Tick(context.Background()))

	rec.down = false
	srcB.samples = []Sample{{Value: 0, TS: t0}}
	require.NoError(t, b.Tick(context.Background()))
	require.Equal(t, []string{"resolve"}, rec.got)

	regainLeadership(a)
	srcA.samples = []Sample{{Value: 0, TS: t0}}
	require.NoError(t, a.Tick(context.Background()))
	require.Equal(t, []string{"resolve"}, rec.got, "the FIRING A failed to send is obsolete and is not sent after the RESOLVED")
}

// TestTick_ALeaderChangeSendsAFiringThatIsStillOwed — the same handover, but the
// alert is still firing when A leads again. The table still owes the FIRING, so A
// sends it late. The start the table returns is cut to the microsecond, and the
// queued copy is not, so this also pins that the two starts compare equal.
func TestTick_ALeaderChangeSendsAFiringThatIsStillOwed(t *testing.T) {
	t0 := time.Unix(100, 123456789)
	clk := clock.NewFake(t0)
	store := newTableStore(customRule())
	rec := &phaseRecorder{down: true}
	srcA := &stepSource{samples: []Sample{{Value: 2, TS: t0}}}
	srcB := &stepSource{samples: []Sample{{Value: 2, TS: t0}}}
	a := NewEngine(nil, clk, store, NewChannelNotifier(store, recordingChannel{rec}), []Source{srcA})
	b := NewEngine(nil, clk, store, NewChannelNotifier(store, recordingChannel{rec}), []Source{srcB})
	require.Error(t, a.Tick(context.Background()))

	rec.down = false
	require.NoError(t, b.Tick(context.Background()))
	require.Empty(t, rec.got)

	regainLeadership(a)
	require.NoError(t, a.Tick(context.Background()))
	require.Equal(t, []string{"fire"}, rec.got)
}

// TestTick_ALeaderChangeDoesNotSendAResolveOverruledByANewerFiring — replica A
// fails to send the RESOLVED of an episode and stops leading. Replica B fires a
// newer episode of the same alert, and the channel accepts it. When A leads again
// its queue still holds the RESOLVED of the older episode. The table no longer
// owes that RESOLVED, so Settle drops it before a late send would leave the
// channel showing the alert resolved while it is firing.
func TestTick_ALeaderChangeDoesNotSendAResolveOverruledByANewerFiring(t *testing.T) {
	t0 := time.Unix(100, 123456789) // nanoseconds, as a live clock gives them
	clk := clock.NewFake(t0)
	store := newTableStore(customRule())
	rec := &phaseRecorder{}
	srcA := &stepSource{samples: []Sample{{Value: 2, TS: t0}}}
	srcB := &stepSource{samples: []Sample{{Value: 2, TS: t0}}}
	a := NewEngine(nil, clk, store, NewChannelNotifier(store, recordingChannel{rec}), []Source{srcA})
	b := NewEngine(nil, clk, store, NewChannelNotifier(store, recordingChannel{rec}), []Source{srcB})
	require.NoError(t, a.Tick(context.Background()))
	require.Equal(t, []string{"fire"}, rec.got)

	rec.down = true
	srcA.samples = []Sample{{Value: 0, TS: t0.Add(time.Second)}}
	require.Error(t, a.Tick(context.Background()))

	rec.down = false
	srcB.samples = []Sample{{Value: 2, TS: t0.Add(2 * time.Second)}}
	require.NoError(t, b.Tick(context.Background()))
	require.Equal(t, []string{"fire", "fire"}, rec.got)

	regainLeadership(a)
	srcA.samples = []Sample{{Value: 2, TS: t0.Add(2 * time.Second)}}
	require.NoError(t, a.Tick(context.Background()))
	require.Equal(t, []string{"fire", "fire"}, rec.got, "the RESOLVED of the first episode is not sent after the FIRING of the newer one")
}

// messageRecorder keeps every message its channel accepted.
type messageRecorder struct{ got []notify.Message }

func (c *messageRecorder) Name() string { return "slack" }
func (c *messageRecorder) Send(_ context.Context, m notify.Message) error {
	c.got = append(c.got, m)
	return nil
}

// requireOneEpisode checks that got is the FIRING and then the RESOLVED of one
// episode, under one dedup_id: a consumer that closes incidents by dedup_id must
// be able to close the incident the FIRING opened.
func requireOneEpisode(t *testing.T, got []notify.Message) {
	require.Len(t, got, 2)
	require.Equal(t, "fire", got[0].Phase)
	require.Equal(t, "resolve", got[1].Phase)
	require.Equal(t, got[0].DedupID, got[1].DedupID, "the RESOLVED names the episode its FIRING named")
}

// TestTick_ARegainedLeadershipResolvesUnderTheDedupIDOfItsFiring — the leader's
// session drops and comes back in the same process. The episode is read back from
// the table for the new term, and its RESOLVED keeps the dedup_id of the FIRING.
func TestTick_ARegainedLeadershipResolvesUnderTheDedupIDOfItsFiring(t *testing.T) {
	t0 := time.Unix(100, 123456789) // nanoseconds, as a live clock gives them
	store := newTableStore(customRule())
	rec := &messageRecorder{}
	src := &stepSource{samples: []Sample{{Value: 2, TS: t0}}}
	e := NewEngine(nil, clock.NewFake(t0), store, NewChannelNotifier(store, rec), []Source{src})
	require.NoError(t, e.Tick(context.Background()))
	regainLeadership(e)
	src.samples = []Sample{{Value: 0, TS: t0.Add(time.Second)}}
	require.NoError(t, e.Tick(context.Background()))
	requireOneEpisode(t, rec.got)
}

// TestTick_AHandoverResolvesUnderTheDedupIDOfItsFiring — the FIRING is sent by one
// replica and the RESOLVED by another, which starts its term from the table. The
// RESOLVED keeps the dedup_id of the FIRING.
func TestTick_AHandoverResolvesUnderTheDedupIDOfItsFiring(t *testing.T) {
	t0 := time.Unix(100, 123456789) // nanoseconds, as a live clock gives them
	clk := clock.NewFake(t0)
	store := newTableStore(customRule())
	rec := &messageRecorder{}
	srcA := &stepSource{samples: []Sample{{Value: 2, TS: t0}}}
	srcB := &stepSource{samples: []Sample{{Value: 0, TS: t0.Add(time.Second)}}}
	a := NewEngine(nil, clk, store, NewChannelNotifier(store, rec), []Source{srcA})
	b := NewEngine(nil, clk, store, NewChannelNotifier(store, rec), []Source{srcB})
	require.NoError(t, a.Tick(context.Background()))
	require.NoError(t, b.Tick(context.Background()))
	requireOneEpisode(t, rec.got)
}

// blockingClaimStore never answers a claim: it waits until the claim's context is
// done, as a hung connection pool does.
type blockingClaimStore struct {
	markStore
	calls int
}

func (s *blockingClaimStore) ClaimNotification(ctx context.Context, _, _, _ string, _ Alert) (bool, error) {
	s.calls++
	<-ctx.Done()
	return false, ctx.Err()
}

// TestChannelNotifier_AHungClaimUsesUpItsAttempts pins the claim's own deadline
// as an attempt. The cycle stays live, so each claim ends on its own deadline,
// and every such failure counts. After the attempts are used up the delivery is
// given up and reported, and nothing is sent. A rule that counted a claim only
// while the cycle was live would never count these, and the delivery would stay
// pending on every pass.
func TestChannelNotifier_AHungClaimUsesUpItsAttempts(t *testing.T) {
	restore := claimTimeout
	claimTimeout = 10 * time.Millisecond
	t.Cleanup(func() { claimTimeout = restore })
	a := Alert{Key: "k", RuleID: "r", Severity: SeverityWarning, State: StateFiring, StartedAt: time.Unix(1, 0), LastEvalAt: time.Unix(1, 0)}
	store := &blockingClaimStore{}
	slack := &testChannel{name: "slack"}
	n := NewChannelNotifier(store, slack)
	retrier := n.(interface{ Retry(context.Context) error })
	before := UndeliveredNotifications()

	cycle, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, n.Notify(cycle, a), context.DeadlineExceeded)
	require.NoError(t, retrier.Retry(cycle), "a claim is not made again in the pass that failed it")
	for i := 2; i < maxNotifyAttempts; i++ {
		require.ErrorIs(t, retrier.Retry(cycle), context.DeadlineExceeded)
	}
	require.ErrorIs(t, retrier.Retry(cycle), errUndelivered)
	require.Equal(t, before+1, UndeliveredNotifications())
	require.Equal(t, maxNotifyAttempts, store.calls)
	require.Zero(t, slack.sent)
}

// TestChannelNotifier_AClaimCutByTheCycleStillCounts pins that a claim which
// fails after the cycle has ended still counts. Every pass's cycle ends before
// the claim's own deadline, so each claim runs past the cycle, and its failure
// still counts. A rule that counted a claim only while the cycle was live would
// count none of them, and the delivery would never be given up. A claim that the
// store accepts after the cycle has ended is pinned by
// TestChannelNotifier_AClaimTheStoreAcceptedAfterTheCycleEndsIsDelivered.
func TestChannelNotifier_AClaimCutByTheCycleStillCounts(t *testing.T) {
	restore := claimTimeout
	claimTimeout = 60 * time.Millisecond
	t.Cleanup(func() { claimTimeout = restore })
	a := Alert{Key: "k", RuleID: "r", Severity: SeverityWarning, State: StateFiring, StartedAt: time.Unix(1, 0), LastEvalAt: time.Unix(1, 0)}
	store := &blockingClaimStore{}
	slack := &testChannel{name: "slack"}
	n := NewChannelNotifier(store, slack)
	retrier := n.(interface{ Retry(context.Context) error })
	before := UndeliveredNotifications()

	pass := func(run func(context.Context) error) error {
		cycle, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		return run(cycle)
	}
	require.ErrorIs(t, pass(func(ctx context.Context) error { return n.Notify(ctx, a) }), context.DeadlineExceeded)
	require.NoError(t, pass(retrier.Retry), "a claim is not made again in the pass that failed it")
	for i := 2; i < maxNotifyAttempts; i++ {
		require.ErrorIs(t, pass(retrier.Retry), context.DeadlineExceeded)
	}
	require.ErrorIs(t, pass(retrier.Retry), errUndelivered)
	require.Equal(t, before+1, UndeliveredNotifications())
	require.Equal(t, maxNotifyAttempts, store.calls)
	require.Zero(t, slack.sent)
}

// lateClaimStore records the claim row and ends the cycle while it does so, as a
// cycle that runs out during a store call does. It answers with the context it
// was given, so a store bound to the cycle reports that context's end although
// the row is already on record.
type lateClaimStore struct {
	markStore
	endCycle context.CancelFunc
	calls    int
	row      bool
}

func (s *lateClaimStore) ClaimNotification(ctx context.Context, _, _, _ string, _ Alert) (bool, error) {
	s.calls++
	if s.row {
		return false, nil
	}
	s.row = true
	s.endCycle()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return true, nil
}

// TestChannelNotifier_AClaimTheStoreAcceptedAfterTheCycleEndsIsDelivered — a claim
// that the store accepts is not cut by the end of the cycle that asked for it, so
// its delivery is sent on the next pass. Cut, the claim would fail with its row on
// record, and the next pass would report the delivery undelivered.
func TestChannelNotifier_AClaimTheStoreAcceptedAfterTheCycleEndsIsDelivered(t *testing.T) {
	a := Alert{Key: "k", RuleID: "r", Severity: SeverityWarning, State: StateFiring, StartedAt: time.Unix(1, 0), LastEvalAt: time.Unix(1, 0)}
	cycle, endCycle := context.WithCancel(context.Background())
	defer endCycle()
	store := &lateClaimStore{endCycle: endCycle}
	slack := &testChannel{name: "slack"}
	n := NewChannelNotifier(store, slack)
	retrier := n.(interface{ Retry(context.Context) error })
	before := UndeliveredNotifications()

	require.ErrorIs(t, n.Notify(cycle, a), context.Canceled, "the send did not begin before the cycle ended")
	require.Zero(t, slack.sent)
	require.NoError(t, retrier.Retry(context.Background()), "a delivery is not sent again in the pass that failed it")
	require.Zero(t, slack.sent)
	require.NoError(t, retrier.Retry(context.Background()))
	require.Equal(t, 1, slack.sent)
	require.NoError(t, retrier.Retry(context.Background()))
	require.Equal(t, 1, slack.sent, "a delivery is sent once")
	require.Equal(t, 1, store.calls, "the claim is asked once")
	require.Equal(t, before, UndeliveredNotifications())
}

// TestChannelNotifier_ANewerTransitionDropsItsPendingDelivery — without an engine,
// a transition newer than a delivery still pending for its alert drops that
// delivery. The channel that refused the FIRING does not get it after the
// RESOLVED, even when no pass that would have checked the alert's state runs.
func TestChannelNotifier_ANewerTransitionDropsItsPendingDelivery(t *testing.T) {
	fire := Alert{Key: "k", RuleID: "r", Severity: SeverityWarning, State: StateFiring, StartedAt: time.Unix(1, 0), LastEvalAt: time.Unix(1, 0)}
	resolve := fire
	resolve.State = StateResolved
	rec := &phaseRecorder{down: true}
	n := NewChannelNotifier(&markStore{}, recordingChannel{rec})
	retrier := n.(interface{ Retry(context.Context) error })

	require.Error(t, n.Notify(context.Background(), fire))
	require.NoError(t, retrier.Retry(context.Background()), "a delivery is not sent again in the pass that failed it")
	rec.down = false
	require.NoError(t, n.Notify(context.Background(), resolve))
	require.NoError(t, retrier.Retry(context.Background()))
	require.Equal(t, []string{"resolve"}, rec.got, "the refused FIRING is not sent after the RESOLVED")
}

// TestChannelNotifier_SettleReadsTheEpisodesOnlyWhenADeliveryIsQueued — a pass
// with nothing queued has nothing to settle, so it does not read the firing
// episodes at all.
func TestChannelNotifier_SettleReadsTheEpisodesOnlyWhenADeliveryIsQueued(t *testing.T) {
	fire := Alert{Key: "k", RuleID: "r", Severity: SeverityWarning, State: StateFiring, StartedAt: time.Unix(1, 0), LastEvalAt: time.Unix(1, 0)}
	rec := &phaseRecorder{}
	n := NewChannelNotifier(&markStore{}, recordingChannel{rec})
	settle := n.(settler)
	reads := 0
	firing := func() []Alert {
		reads++
		return nil
	}
	settle.Settle(firing)
	require.Zero(t, reads, "an empty queue reads no episodes")

	rec.down = true
	require.Error(t, n.Notify(context.Background(), fire))
	settle.Settle(firing)
	require.Equal(t, 1, reads, "a queued delivery is settled against the episodes")
}
