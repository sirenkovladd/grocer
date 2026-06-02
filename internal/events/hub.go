package events

import (
	"sync"

	"code.sirenko.ca/grocer/internal/receipt"
)

// Hub manages SSE subscribers per proposal.
type Hub struct {
	mu          sync.RWMutex
	subscribers map[uint64][]chan receipt.ParseEvent
}

func NewHub() *Hub {
	return &Hub{
		subscribers: make(map[uint64][]chan receipt.ParseEvent),
	}
}

// Subscribe returns a channel that receives events for the given proposal.
func (h *Hub) Subscribe(proposalID uint64) <-chan receipt.ParseEvent {
	ch := make(chan receipt.ParseEvent, 32)
	h.mu.Lock()
	h.subscribers[proposalID] = append(h.subscribers[proposalID], ch)
	h.mu.Unlock()
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

// Publish sends an event to all subscribers of a proposal.
func (h *Hub) Publish(proposalID uint64, event receipt.ParseEvent) {
	h.mu.RLock()
	subs := h.subscribers[proposalID]
	h.mu.RUnlock()

	for _, ch := range subs {
		select {
		case ch <- event:
		default:
			// Skip slow subscribers
		}
	}
}
