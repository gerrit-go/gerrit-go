package events

import (
	"sync"
	"time"

	"gerrit-go/internal/store"
)

// StreamEvent is one change event published to stream-events subscribers. The
// Change/PatchSet/Actor are read-only snapshots taken at publish time.
type StreamEvent struct {
	Type      string // change-created, patchset-created, comment-added, ...
	Change    *store.Change
	PatchSet  *store.PatchSet
	Actor     *store.Account
	Extra     map[string]any
	CreatedOn time.Time
}

// Subscriber is one stream-events consumer. Ch is buffered; the broker drops
// events for a subscriber whose buffer is full rather than blocking publishers.
type Subscriber struct {
	ID      uint64
	Acct    *store.Account
	Ch      chan StreamEvent
	Dropped int
}

// Broker is an in-process pub/sub registry for stream-events. It is
// permission-agnostic; per-subscriber read filtering happens in the consumer.
type Broker struct {
	mu   sync.Mutex
	subs map[uint64]*Subscriber
	next uint64
}

func NewBroker() *Broker {
	return &Broker{subs: map[uint64]*Subscriber{}}
}

// Subscribe registers a subscriber with the given channel buffer and returns
// it. Unsubscribe must be called to release it.
func (b *Broker) Subscribe(acct *store.Account, buffer int) *Subscriber {
	if buffer <= 0 {
		buffer = 64
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.next++
	sub := &Subscriber{ID: b.next, Acct: acct, Ch: make(chan StreamEvent, buffer)}
	b.subs[sub.ID] = sub
	return sub
}

// Unsubscribe removes the subscriber and closes its channel.
func (b *Broker) Unsubscribe(sub *Subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.subs[sub.ID]; ok {
		delete(b.subs, sub.ID)
		close(sub.Ch)
	}
}

// Publish fans an event out to all subscribers with a non-blocking send; a
// subscriber whose buffer is full has the event dropped (counted in Dropped).
func (b *Broker) Publish(ev StreamEvent) {
	if ev.CreatedOn.IsZero() {
		ev.CreatedOn = time.Now()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, sub := range b.subs {
		select {
		case sub.Ch <- ev:
		default:
			sub.Dropped++
		}
	}
}

// Count returns the number of active subscribers.
func (b *Broker) Count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}
