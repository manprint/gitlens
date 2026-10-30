package notify

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type recordingChannel struct {
	name string
	mu   sync.Mutex
	got  []Message
}

func (c *recordingChannel) Name() string { return c.name }

func (c *recordingChannel) Send(_ context.Context, m Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, m)
	return nil
}

// NOTI-010 (slot): a Slot keeps its kind's name, sends through the channel it
// holds at the time of the send, and an empty Slot is not configured and sends
// nothing.
func TestNOTI010_SlotSendsThroughItsCurrentChannel(t *testing.T) {
	first := &recordingChannel{name: "google_chat"}
	second := &recordingChannel{name: "google_chat"}
	slot := NewSlot("google_chat", nil)
	require.Equal(t, "google_chat", slot.Name())
	require.False(t, slot.Configured())
	require.False(t, Configured(slot))
	require.True(t, errors.Is(slot.Send(context.Background(), Message{Summary: "none"}), ErrNotConfigured))

	slot.Store(first)
	require.True(t, Configured(slot))
	require.NoError(t, slot.Send(context.Background(), Message{Summary: "one"}))
	slot.Store(second)
	require.NoError(t, slot.Send(context.Background(), Message{Summary: "two"}))
	slot.Store(nil)
	require.ErrorIs(t, slot.Send(context.Background(), Message{Summary: "three"}), ErrNotConfigured)

	require.Len(t, first.got, 1)
	require.Equal(t, "one", first.got[0].Summary)
	require.Len(t, second.got, 1)
	require.Equal(t, "two", second.got[0].Summary)
	// A plain channel is always configured.
	require.True(t, Configured(first))
}

// The swap is safe while sends run (go test -race).
func TestNOTI010_SlotSwapUnderConcurrentSends(t *testing.T) {
	slot := NewSlot("webhook", &recordingChannel{name: "webhook"})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = slot.Send(context.Background(), Message{})
		}()
		go func() {
			defer wg.Done()
			slot.Store(&recordingChannel{name: "webhook"})
		}()
	}
	wg.Wait()
	require.True(t, slot.Configured())
}
