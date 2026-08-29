package alert

import (
	"context"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/manprint/pglens/internal/alert/notify"
)

type channelNotifier struct {
	store    Store
	channels []notify.Channel
}

func NewChannelNotifier(store Store, channels ...notify.Channel) Notifier {
	return &channelNotifier{store: store, channels: channels}
}

func (n *channelNotifier) Notify(ctx context.Context, a Alert) error {
	phase := "fire"
	if a.State == StateResolved {
		phase = "resolve"
	}
	for _, ch := range n.channels {
		won, err := n.store.ClaimNotification(ctx, DedupID(a), ch.Name(), phase, a)
		if err != nil {
			return err
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
		err = ch.Send(ctx, m)
		if markErr := n.store.MarkNotification(ctx, DedupID(a), ch.Name(), phase, err == nil, 1, err); markErr != nil && err == nil {
			err = markErr
		}
		if err != nil {
			return fmt.Errorf("channel %s: %w", ch.Name(), err)
		}
	}
	return nil
}
