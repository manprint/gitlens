package alert

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/manprint/pglens/internal/alert/notify"
)

// maxNotifyAttempts bounds the sends and the claims of one delivery that the
// store was asked for, the first included. A channel that is down for a while
// still gets the transition once it is back, and one that stays down does not
// keep its messages queued forever: after the last send the claim row records
// the failure. A claim that fails, its own deadline included, uses up the same
// attempts. A claim that never started, because the cycle had already ended,
// uses none. A delivery that runs out of attempts while unclaimed has no row to
// record it, so keepUnclaimed reports it instead.
const maxNotifyAttempts = 5

// claimTimeout bounds one claim of the store. A claim is not tied to the cycle's
// context: a claim the store has started may already have written its row, and
// it must be allowed to finish, or the delivery it won would be reported
// undelivered instead of sent. The claim's own deadline keeps a store that never
// answers from holding up the pass.
var claimTimeout = 2 * time.Second

// undelivered counts the unclaimed deliveries this process gave up on. It is
// read through UndeliveredNotifications.
var undelivered atomic.Int64

// errUndelivered wraps the error of each delivery given up on.
var errUndelivered = errors.New("notification not delivered")

// UndeliveredNotifications reports how many unclaimed deliveries this process
// gave up on. It never sent them, and no row records the loss.
func UndeliveredNotifications() int64 { return undelivered.Load() }

type channelNotifier struct {
	store    Store
	channels []notify.Channel

	mu      sync.Mutex
	passes  int               // completed Retry passes
	pending []pendingDelivery // failed deliveries waiting for a later pass
}

// pendingDelivery is a message that a channel did not accept. It is sent again
// on the first pass after the one in which it failed, unless Notify or Settle
// drops it first because its alert has moved on. A drop is logged, not counted,
// and not reported as undelivered. The queue lives in memory: a restart drops
// it, while a change of leader in the same process keeps the entries that the
// table still owes, and those are sent late. A failed send stays on record in
// the claim row; an unclaimed delivery has no row, so when it runs out of
// attempts giveUp reports it.
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
	// A transition is newer than every delivery still pending for its alert, so
	// those are dropped before this one goes out. Otherwise a channel that was
	// refused a FIRING would get it after the RESOLVED that follows it. The cost
	// is that a dropped RESOLVED of an older episode is never sent. The dedup_id
	// of each message is per episode, so a consumer that tracks incidents by it
	// keeps that older incident open.
	n.dropPending(func(d pendingDelivery) bool { return d.alert.Key == a.Key })
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

// claim makes the claim row of d and reports whether this pass won it. The
// cycle's deadline is checked before the store is asked, and a claim that never
// starts is not counted. Once the store has been asked, every failure counts,
// since the store had its chance to answer, and the cycle ending during the call
// does not change that. The claim is not given the cycle's context, so it can
// run past the cycle's end. That overrun is bounded by claimTimeout, and by the
// check above, which lets no second claim start after the cycle has ended.
func (n *channelNotifier) claim(ctx context.Context, d *pendingDelivery) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	claimCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), claimTimeout)
	defer cancel()
	won, err := n.store.ClaimNotification(claimCtx, d.dedupID, d.channel.Name(), d.phase, d.alert)
	if err != nil {
		d.attempts++
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
// not hit twice in the same pass. A pass that starts after the cycle has ended
// makes no claim and no send, since the cycle's context is already done. Those
// deliveries are kept without being counted, so a delivery can stay pending
// while such passes repeat, for example when the refreshes of the rules,
// endUnsampled or purgeHistory use up the rest of the cycle. Settle still runs
// on those passes, since it needs no store, so an obsolete delivery is dropped
// even then.
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

// Settle drops the pending deliveries that their alerts have moved past. firing
// returns the episodes that are firing now, and it is called only when a
// delivery is queued. A FIRING is still owed only while its own episode is
// firing, and a firing episode of the same alert that started later makes an
// older delivery of it obsolete. This covers a RESOLVED that this process sent,
// a RESOLVED that another replica sent while this one was not leader, and a
// RESOLVED that a silence kept from the channels. The evaluator follows all of
// them, because it is restored from the table when a term begins.
func (n *channelNotifier) Settle(firing func() []Alert) {
	n.mu.Lock()
	queued := len(n.pending) > 0
	n.mu.Unlock()
	if !queued {
		return
	}
	// The episodes are grouped by key once, so that each delivery is checked
	// against the episodes of its own alert and not against all of them.
	byKey := make(map[string][]Alert)
	for _, a := range firing() {
		byKey[a.Key] = append(byKey[a.Key], a)
	}
	n.dropPending(func(d pendingDelivery) bool {
		owed := false
		for _, a := range byKey[d.alert.Key] {
			if startOf(a).After(startOf(d.alert)) {
				return true
			}
			owed = owed || startOf(a).Equal(startOf(d.alert))
		}
		return d.phase == "fire" && !owed
	})
}

// startOf is the start of an alert's episode at the precision the table keeps.
// The table stores microseconds, so an episode read back from it starts up to
// a microsecond before the copy this process queued.
func startOf(a Alert) time.Time { return a.StartedAt.Truncate(time.Microsecond) }

// dropPending removes the pending deliveries that drop selects. A dropped
// delivery is logged, not counted as an attempt and not reported as
// undelivered, since nothing failed: it is obsolete.
func (n *channelNotifier) dropPending(drop func(pendingDelivery) bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	kept := n.pending[:0]
	for _, d := range n.pending {
		if drop(d) {
			log.Printf("alert notifier: %s on %s (%s) dropped: its alert has moved on", d.dedupID, d.channel.Name(), d.phase)
			continue
		}
		kept = append(kept, d)
	}
	n.pending = kept
}

// deliver makes one attempt at a delivery and records it. A failed attempt is
// kept for a later pass while attempts remain.
func (n *channelNotifier) deliver(ctx context.Context, d pendingDelivery) error {
	if err := ctx.Err(); err != nil {
		// The cycle ran out of time before the send began, so the channel was
		// never asked and the attempt says nothing about it. It is kept for a
		// later pass without being counted, or the time the rules took would use
		// up the attempts. A send that did begin counts, even when the cycle ends
		// while it waits: a channel that never answers is failing. A slow channel
		// that needs more time than the cycle has left is cut short the same way.
		// That is the price of bounding a hung one, since the cycle is 10 seconds
		// and the retries run last.
		n.keep(d)
		return fmt.Errorf("channel %s: %w", d.channel.Name(), err)
	}
	sendErr := d.channel.Send(ctx, d.message)
	d.attempts++
	// The outcome is recorded even when the cycle ended during the send. The
	// cycle's deadline may already be spent, so the write gets a short one of its
	// own.
	markCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	markErr := n.store.MarkNotification(markCtx, d.dedupID, d.channel.Name(), d.phase, sendErr == nil, d.attempts, sendErr)
	cancel()
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
