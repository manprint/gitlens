package alert

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/alert/notify"
)

type stubDescriber struct {
	calls int
	panic bool
	block bool
}

func (s *stubDescriber) Describe(ctx context.Context, a Alert, phase string) *notify.Detail {
	s.calls++
	if s.panic {
		panic("boom")
	}
	if s.block {
		<-ctx.Done()
		return nil
	}
	return &notify.Detail{Title: phase + " " + a.Key}
}

type msgChannel struct {
	name string
	fail int
	got  []notify.Message
}

func (c *msgChannel) Name() string { return c.name }
func (c *msgChannel) Send(_ context.Context, m notify.Message) error {
	if c.fail > 0 {
		c.fail--
		return errors.New("down")
	}
	c.got = append(c.got, m)
	return nil
}

func describedAlert() Alert {
	return Alert{Key: "k", RuleID: "r", Severity: SeverityWarning, State: StateFiring, StartedAt: time.Unix(1, 0), LastEvalAt: time.Unix(1, 0)}
}

func TestDescriber_OnceForEveryChannel(t *testing.T) {
	d := &stubDescriber{}
	a, b := &msgChannel{name: "slack"}, &msgChannel{name: "webhook"}
	require.NoError(t, NewChannelNotifierWith(&claimStore{}, d, a, b).Notify(context.Background(), describedAlert()))
	require.Equal(t, 1, d.calls)
	require.Equal(t, "fire k", a.got[0].Detail.Title)
	require.Same(t, a.got[0].Detail, b.got[0].Detail)

	plain := &msgChannel{name: "slack"}
	require.NoError(t, NewChannelNotifier(&claimStore{}, plain).Notify(context.Background(), describedAlert()))
	require.Nil(t, plain.got[0].Detail)
}

func TestDescriber_PanicSendsTheSummary(t *testing.T) {
	ch := &msgChannel{name: "slack"}
	require.NoError(t, NewChannelNotifierWith(&claimStore{}, &stubDescriber{panic: true}, ch).Notify(context.Background(), describedAlert()))
	require.Len(t, ch.got, 1)
	require.Nil(t, ch.got[0].Detail)
}

func TestDescriber_UnclaimedDeliveryDescribedOnRetry(t *testing.T) {
	d := &stubDescriber{}
	ch := &msgChannel{name: "slack"}
	n := NewChannelNotifierWith(&claimStore{failClaims: 1}, d, ch).(*channelNotifier)
	require.Error(t, n.Notify(context.Background(), describedAlert()))
	require.Zero(t, d.calls, "no detail before a claim is won")
	require.NoError(t, n.Retry(context.Background()))
	require.NoError(t, n.Retry(context.Background()))
	require.Equal(t, 1, d.calls)
	require.Equal(t, "fire k", ch.got[0].Detail.Title)
}

func TestDescriber_TimedOutDetailTriedAgain(t *testing.T) {
	old := describeTimeout
	describeTimeout = 10 * time.Millisecond
	defer func() { describeTimeout = old }()
	d := &stubDescriber{block: true}
	ch := &msgChannel{name: "slack", fail: 1}
	n := NewChannelNotifierWith(&claimStore{}, d, ch).(*channelNotifier)
	require.Error(t, n.Notify(context.Background(), describedAlert()))
	d.block = false
	require.NoError(t, n.Retry(context.Background()))
	require.NoError(t, n.Retry(context.Background()))
	require.Equal(t, 2, d.calls)
	require.Equal(t, "fire k", ch.got[0].Detail.Title)
}

// localeDescriber writes the detail in the locale asked for; "" is the
// server default.
type localeDescriber struct{ asked []string }

func (d *localeDescriber) Describe(ctx context.Context, a Alert, phase string) *notify.Detail {
	return d.DescribeIn(ctx, a, phase, "")
}

func (d *localeDescriber) DescribeIn(_ context.Context, a Alert, phase, locale string) *notify.Detail {
	d.asked = append(d.asked, locale)
	return &notify.Detail{Locale: locale, Title: phase + " " + a.Key}
}

// NOTI-014 (plan 006 F7): each channel reads the detail in its own language,
// written once per language; a channel without one gets the server default.
func TestDescriber_DetailPerChannelLanguage(t *testing.T) {
	d := &localeDescriber{}
	slack, chat, hook := &msgChannel{name: "slack"}, &msgChannel{name: "google_chat"}, &msgChannel{name: "webhook"}
	itSlack, itChat := notify.NewSlot("slack", slack), notify.NewSlot("google_chat", chat)
	itSlack.SetLocale("it")
	itChat.SetLocale("it")
	require.NoError(t, NewChannelNotifierWith(&claimStore{}, d, itSlack, itChat, hook).Notify(context.Background(), describedAlert()))
	require.Equal(t, []string{"it", ""}, d.asked)
	require.Equal(t, "it", slack.got[0].Detail.Locale)
	require.Same(t, slack.got[0].Detail, chat.got[0].Detail)
	require.Equal(t, "", hook.got[0].Detail.Locale)

	// The retry of a failed send asks for the channel's language too.
	d.asked = nil
	failing := &msgChannel{name: "slack", fail: 1}
	slot := notify.NewSlot("slack", failing)
	slot.SetLocale("it")
	n := NewChannelNotifierWith(&claimStore{}, d, slot).(*channelNotifier)
	require.Error(t, n.Notify(context.Background(), describedAlert()))
	require.NoError(t, n.Retry(context.Background()))
	require.NoError(t, n.Retry(context.Background()))
	require.Equal(t, "it", failing.got[0].Detail.Locale)
}
