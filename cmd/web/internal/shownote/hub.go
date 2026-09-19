// Package shownote provides the in-process real-time collaboration hub and the
// block-tree assembly used by the Show Notes editor. It mirrors the SSE
// broadcast model of cmd/web/internal/producer but is keyed by show-note id and
// carries change events (not raw scene bytes).
package shownote

import (
	"encoding/json"
	"sync"
)

// maxSubsPerNote caps concurrent host streams on a single show note so a host
// opening many tabs can't exhaust goroutines.
const maxSubsPerNote = 32

// Event describes a change to a show note, fanned out to every connected host.
type Event struct {
	// Kind is "tree" (structure changed — re-render the whole outline) or
	// "block" (one block's content changed — re-render just that block).
	Kind string `json:"kind"`
	// BlockID identifies the changed block for Kind=="block".
	BlockID string `json:"block_id,omitempty"`
	// Origin is the editing client's id, used to suppress echoing a change back
	// to the client that made it (which already rendered it locally).
	Origin string `json:"origin,omitempty"`
}

type session struct {
	subs map[chan []byte]struct{}
}

// Hub fans out show-note change events to all subscribed host streams.
type Hub struct {
	mu       sync.Mutex
	sessions map[string]*session
}

// NewHub creates an empty show-note hub.
func NewHub() *Hub {
	return &Hub{sessions: make(map[string]*session)}
}

func (h *Hub) getOrCreate(noteID string) *session {
	if s, ok := h.sessions[noteID]; ok {
		return s
	}
	s := &session{subs: make(map[chan []byte]struct{})}
	h.sessions[noteID] = s
	return s
}

// Subscribe registers a host stream for a show note. It returns a receive
// channel of marshaled Events and an unsubscribe function.
func (h *Hub) Subscribe(noteID string) (<-chan []byte, func()) {
	ch := make(chan []byte, 16)

	h.mu.Lock()
	s := h.getOrCreate(noteID)
	if len(s.subs) >= maxSubsPerNote {
		h.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	s.subs[ch] = struct{}{}
	h.mu.Unlock()

	unsubscribe := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		s, ok := h.sessions[noteID]
		if !ok {
			return
		}
		if _, ok := s.subs[ch]; ok {
			delete(s.subs, ch)
			close(ch)
		}
		if len(s.subs) == 0 {
			delete(h.sessions, noteID)
		}
	}
	return ch, unsubscribe
}

// Broadcast marshals an event and delivers it to all subscribers of a show note.
// Delivery is best-effort: a full subscriber buffer drops the event for that
// subscriber rather than blocking.
func (h *Hub) Broadcast(noteID string, evt Event) {
	data, err := json.Marshal(evt)
	if err != nil {
		return
	}

	h.mu.Lock()
	s, ok := h.sessions[noteID]
	if !ok {
		h.mu.Unlock()
		return
	}
	subs := make([]chan []byte, 0, len(s.subs))
	for ch := range s.subs {
		subs = append(subs, ch)
	}
	h.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- data:
		default:
		}
	}
}
