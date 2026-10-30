package notify

import (
	"context"
	"errors"
	"sync/atomic"
)

// ErrNotConfigured is the error of a send through a Slot that holds no
// channel.
var ErrNotConfigured = errors.New("notification channel not configured")

// Slot is the delivery channel of one kind whose address the console can
// replace while the server runs (11.5b, D3). The alert notifier and the
// console's test send hold the same Slot and read its current channel on every
// send, so a change reaches the next delivery without a restart. An empty Slot
// is a channel that is not configured.
type Slot struct {
	name    string
	current atomic.Pointer[slotEntry]
}

// slotEntry boxes the channel, since an atomic.Pointer holds a pointer and not
// an interface value.
type slotEntry struct {
	channel Channel
}

// NewSlot returns the Slot of the kind name holding initial, which may be nil.
func NewSlot(name string, initial Channel) *Slot {
	s := &Slot{name: name}
	s.Store(initial)
	return s
}

// Name is the kind of the Slot, whatever channel it holds.
func (s *Slot) Name() string { return s.name }

// Channel is the channel the Slot holds now, nil when it holds none.
func (s *Slot) Channel() Channel {
	if e := s.current.Load(); e != nil {
		return e.channel
	}
	return nil
}

// Store replaces the channel; nil empties the Slot. A send already running
// finishes on the channel it started with.
func (s *Slot) Store(c Channel) {
	if c == nil {
		s.current.Store(nil)
		return
	}
	s.current.Store(&slotEntry{channel: c})
}

// Configured reports whether the Slot holds a channel.
func (s *Slot) Configured() bool { return s.Channel() != nil }

// Send sends m through the channel the Slot holds now.
func (s *Slot) Send(ctx context.Context, m Message) error {
	c := s.Channel()
	if c == nil {
		return ErrNotConfigured
	}
	return c.Send(ctx, m)
}

// Configured reports whether c has an address to deliver to. Only a channel
// that says otherwise, such as an empty Slot, is not configured.
func Configured(c Channel) bool {
	if s, ok := c.(interface{ Configured() bool }); ok {
		return s.Configured()
	}
	return true
}
