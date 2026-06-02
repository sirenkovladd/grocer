package events

import (
	"sync"
	"time"

	"code.sirenko.ca/grocer/internal/receipt"
)

const (
	// cleanupDelay is how long to keep events after done/error before purging.
	cleanupDelay = 5 * time.Minute
)

// Hub manages SSE subscribers per proposal with event replay.
type Hub struct {
	mu          sync.RWMutex
	subscribers map[uint64][]chan receipt.ParseEvent
	replay      map[uint64][]receipt.ParseEvent
}

func NewHub() *Hub {
	return &Hub{
		subscribers: make(map[uint64][]chan receipt.ParseEvent),
		replay:      make(map[uint64][]receipt.ParseEvent),
	}
}

// Subscribe returns a channel that receives events for the given proposal.
// If there are stored events (parsing in progress), they are replayed first.
func (h *Hub) Subscribe(proposalID uint64) <-chan receipt.ParseEvent {
	ch := make(chan receipt.ParseEvent, 32)

	h.mu.Lock()
	h.subscribers[proposalID] = append(h.subscribers[proposalID], ch)

	// Replay stored events to the new subscriber
	events := h.replay[proposalID]
	h.mu.Unlock()

	// Send replay events (outside lock to avoid blocking)
	for _, event := range events {
		select {
		case ch <- event:
		default:
			// Subscriber buffer full, skip
		}
	}

	return ch
}

// Unsubscribe removes a subscriber channel.
func (h *Hub) Unsubscribe(proposalID uint64, ch <-chan receipt.ParseEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	subs := h.subscribers[proposalID]
	for i, s := range subs {
		if s == ch {
			h.subscribers[proposalID] = append(subs[:i], subs[i+1:]...)
			close(s)
			break
		}
	}
	if len(h.subscribers[proposalID]) == 0 {
		delete(h.subscribers, proposalID)
	}
}

// Publish sends an event to all subscribers and stores it for replay.
func (h *Hub) Publish(proposalID uint64, event receipt.ParseEvent) {
	h.mu.Lock()
	// Store event for replay
	h.replay[proposalID] = append(h.replay[proposalID], event)

	// If terminal event, schedule cleanup
	if event.Type == "done" || event.Type == "error" {
		proposalID := proposalID // capture for goroutine
		go h.scheduleCleanup(proposalID)
	}

	// Get subscribers under lock
	subs := h.subscribers[proposalID]
	h.mu.Unlock()

	// Send to live subscribers
	for _, ch := range subs {
		select {
		case ch <- event:
		default:
			// Skip slow subscribers
		}
	}
}

// ClearReplay removes stored events for a proposal.
// Called after sending a snapshot that already includes persisted state.
func (h *Hub) ClearReplay(proposalID uint64) {
	h.mu.Lock()
	delete(h.replay, proposalID)
	h.mu.Unlock()
}

// scheduleCleanup removes stored events after a delay.
func (h *Hub) scheduleCleanup(proposalID uint64) {
	time.Sleep(cleanupDelay)
	h.mu.Lock()
	delete(h.replay, proposalID)
	h.mu.Unlock()
}
