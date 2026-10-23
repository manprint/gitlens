package alert

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"

	"github.com/google/uuid"
	"github.com/manprint/pglens/internal/alert/notify"
)

// maxNotifyAttempts bounds the sends of one delivery, the first included. A
// channel that is down for a while still gets the transition once it is back,
// and one that stays down does not keep its messages queued forever: after the
// last attempt the claim row records the failure.
const maxNotifyAttempts = 5

type channelNotifier struct {
	store    Store
	channels []notify.Channel

	mu      sync.Mutex
	passes  int               // completed Retry passes
	pending []pendingDelivery // failed deliveries waiting for a later pass
}

// pendingDelivery is a message that a channel did not accept. It is sent again
// on the first pass after the one in which it failed. The queue lives in
// memory: a restart or a change of leader drops it, and the claim row keeps the
// failure on record.
type pendingDelivery struct {
	dedupID  string
	channel  notify.Channel
	phase    string
	message  notify.Message
	attempts int
	failedIn int
}

func NewChannelNotifier(store Store, channels ...notify.Channel) Notifier {
	return &channelNotifier{store: store, channels: channels}
}

func (n *channelNotifier) Notify(ctx context.Context, a Alert) error {
	phase := "fire"
	if a.State == StateResolved {
		phase = "resolve"
	}
	// Every channel gets its attempt. One channel that is down must not keep
	// the others from delivering the same transition.
	var errs []error
	for _, ch := range n.channels {
		won, err := n.store.ClaimNotification(ctx, DedupID(a), ch.Name(), phase, a)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !won {
			continue
		}
		m := notify.Message{DedupID: DedupID(a), RuleID: a.RuleID, Severity: string(a.Severity), Phase: phase, Summary: a.Summary, Database: a.Datname, Value: a.Value, At: a.LastEvalAt, Labels: a.Labels}
		if a.ClusterID != nil {
			m.Cluster = strconv.FormatInt(*a.ClusterID, 10)
		}
		if a.InstanceID != nil {
			m.Instance = uuid.MustParse(a.InstanceID.String()).String()
		}
		if err := n.deliver(ctx, pendingDelivery{dedupID: DedupID(a), channel: ch, phase: phase, message: m}); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Retry makes again the deliveries that failed on an earlier pass. A delivery
// that failed in the current pass waits for the next one, so that a channel is
// not hit twice in the same pass.
func (n *channelNotifier) Retry(ctx context.Context) error {
	n.mu.Lock()
	var due []pendingDelivery
	kept := n.pending[:0]
	for _, d := range n.pending {
		if d.failedIn < n.passes {
			due = append(due, d)
		} else {
			kept = append(kept, d)
		}
	}
	n.pending = kept
	n.mu.Unlock()
	var errs []error
	for _, d := range due {
		if err := n.deliver(ctx, d); err != nil {
			errs = append(errs, err)
		}
	}
	n.mu.Lock()
	n.passes++
	n.mu.Unlock()
	return errors.Join(errs...)
}

// deliver makes one attempt at a delivery and records it. A failed attempt is
// kept for a later pass while attempts remain.
func (n *channelNotifier) deliver(ctx context.Context, d pendingDelivery) error {
	d.attempts++
	sendErr := d.channel.Send(ctx, d.message)
	markErr := n.store.MarkNotification(ctx, d.dedupID, d.channel.Name(), d.phase, sendErr == nil, d.attempts, sendErr)
	if sendErr != nil {
		if d.attempts < maxNotifyAttempts {
			n.mu.Lock()
			d.failedIn = n.passes
			n.pending = append(n.pending, d)
			n.mu.Unlock()
		}
		return fmt.Errorf("channel %s: %w", d.channel.Name(), sendErr)
	}
	return markErr
}
