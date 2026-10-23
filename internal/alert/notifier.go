package alert

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/manprint/pglens/internal/alert/notify"
)

// maxNotifyAttempts bounds the sends of one delivery, the first included. A
// channel that is down for a while still gets the transition once it is back,
// and one that stays down does not keep its messages queued forever: after the
// last send the claim row records the failure. A failed claim uses up the same
// attempts, and a delivery that runs out of them while unclaimed has no row to
// record it, so keepUnclaimed reports it instead.
const maxNotifyAttempts = 5

// undelivered counts the unclaimed deliveries this process gave up on. It is
// read through UndeliveredNotifications.
var undelivered atomic.Int64

// errUndelivered wraps the error of each delivery given up on.
var errUndelivered = errors.New("notification not delivered")

// UndeliveredNotifications reports how many deliveries this process gave up on
// without a claim on record. Such a delivery is never sent, and no row shows it.
func UndeliveredNotifications() int64 { return undelivered.Load() }

type channelNotifier struct {
	store    Store
	channels []notify.Channel

	mu      sync.Mutex
	passes  int               // completed Retry passes
	pending []pendingDelivery // failed deliveries waiting for a later pass
}

// pendingDelivery is a message that a channel did not accept. It is sent again
// on the first pass after the one in which it failed. The queue lives in
// memory: a restart or a change of leader drops it. A failed send stays on
// record in the claim row; an unclaimed delivery has no row, so its drop is
// reported instead.
type pendingDelivery struct {
	dedupID string
	channel notify.Channel
	phase   string
	message notify.Message
	// unclaimed marks a delivery whose claim failed. It is not on record yet,
	// so the next pass claims it before it sends, and it sends only if it wins.
	unclaimed bool
	alert     Alert
	attempts  int
	failedIn  int
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
		d := pendingDelivery{dedupID: DedupID(a), channel: ch, phase: phase, alert: a}
		won, err := n.claim(ctx, &d)
		if err != nil {
			// The transition is already persisted, so without a claim it would
			// never be delivered. The claim is made again on a later pass.
			errs = append(errs, err)
			d.unclaimed = true
			if err := n.keepUnclaimed(d); err != nil {
				errs = append(errs, err)
			}
			continue
		}
		if !won {
			continue
		}
		d.message = newMessage(a, phase)
		if err := n.deliver(ctx, d); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// claim makes the claim row of d and reports whether this pass won it. A claim
// that the cycle's deadline cut short is not counted as an attempt: the store
// was never given the chance to answer, as with a send that did not begin.
func (n *channelNotifier) claim(ctx context.Context, d *pendingDelivery) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	won, err := n.store.ClaimNotification(ctx, d.dedupID, d.channel.Name(), d.phase, d.alert)
	if err != nil {
		if ctx.Err() == nil {
			d.attempts++
		}
		return false, err
	}
	return won, nil
}

// newMessage builds the message a channel receives for one transition.
func newMessage(a Alert, phase string) notify.Message {
	m := notify.Message{DedupID: DedupID(a), RuleID: a.RuleID, Severity: string(a.Severity), Phase: phase, Summary: a.Summary, Database: a.Datname, Value: a.Value, At: a.LastEvalAt, Labels: a.Labels}
	if a.ClusterID != nil {
		m.Cluster = strconv.FormatInt(*a.ClusterID, 10)
	}
	if a.InstanceID != nil {
		m.Instance = uuid.MustParse(a.InstanceID.String()).String()
	}
	return m
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
		if d.unclaimed {
			won, err := n.claim(ctx, &d)
			if err != nil {
				errs = append(errs, err)
				if err := n.keepUnclaimed(d); err != nil {
					errs = append(errs, err)
				}
				continue
			}
			if !won {
				// This process claims a delivery only before it sends it, so a claim
				// on record that it did not win was made by an earlier attempt whose
				// reply was lost, or by another leader. Nothing here can tell whether
				// that message went out, so the delivery is given up and reported.
				errs = append(errs, n.giveUp(d, "its claim is already on record and this pass did not make it"))
				continue
			}
			d.unclaimed = false
			d.message = newMessage(d.alert, d.phase)
		}
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
	if err := ctx.Err(); err != nil {
		// The cycle ran out of time before the send began, so the channel was
		// never asked and the attempt says nothing about it. It is kept for a
		// later pass without being counted, or the time the rules took would use
		// up the attempts. A send that did begin counts, even when the cycle ends
		// while it waits: a channel that never answers is failing.
		n.keep(d)
		return fmt.Errorf("channel %s: %w", d.channel.Name(), err)
	}
	sendErr := d.channel.Send(ctx, d.message)
	d.attempts++
	markErr := n.store.MarkNotification(ctx, d.dedupID, d.channel.Name(), d.phase, sendErr == nil, d.attempts, sendErr)
	if sendErr != nil {
		n.keep(d)
		return fmt.Errorf("channel %s: %w", d.channel.Name(), sendErr)
	}
	return markErr
}

// keep queues a delivery for the next pass while it has attempts left.
func (n *channelNotifier) keep(d pendingDelivery) {
	if d.attempts >= maxNotifyAttempts {
		return
	}
	n.mu.Lock()
	d.failedIn = n.passes
	n.pending = append(n.pending, d)
	n.mu.Unlock()
}

// keepUnclaimed queues a delivery whose claim failed. When its attempts are used
// up it is given up, since no claim row records a failure for it.
func (n *channelNotifier) keepUnclaimed(d pendingDelivery) error {
	if d.attempts < maxNotifyAttempts {
		n.keep(d)
		return nil
	}
	return n.giveUp(d, "its claim failed on every attempt")
}

// giveUp reports an unclaimed delivery that this process will not send. The
// drop is counted, logged and returned, so that it is not silent.
func (n *channelNotifier) giveUp(d pendingDelivery, reason string) error {
	undelivered.Add(1)
	log.Printf("alert notifier: %s on %s (%s) not sent: %s", d.dedupID, d.channel.Name(), d.phase, reason)
	return fmt.Errorf("%w: channel %s, %s of %s: %s", errUndelivered, d.channel.Name(), d.phase, d.dedupID, reason)
}
