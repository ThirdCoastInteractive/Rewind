package producer

import "sync"

const (
	// MaxSceneSubsPerSession limits the number of viewer subscriptions per producer session.
	MaxSceneSubsPerSession = 50
)

// SceneHub manages producer sessions and broadcasts scene updates to viewers.
type SceneHub struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

// NewSceneHub creates a new scene hub.
func NewSceneHub() *SceneHub {
	return &SceneHub{
		sessions: make(map[string]*Session),
	}
}

// getOrCreateSession retrieves or creates a session for the given code.
func (h *SceneHub) getOrCreateSession(code string) *Session {
	s, ok := h.sessions[code]
	if ok {
		return s
	}
	s = &Session{
		Code: code,
		subs: make(map[chan []byte]struct{}),
	}
	h.sessions[code] = s
	return s
}

// Subscribe subscribes a viewer to scene updates for a producer session.
// Returns a channel for scene updates and an unsubscribe function.
func (h *SceneHub) Subscribe(code string) (<-chan []byte, func()) {
	ch := make(chan []byte, 8)

	h.mu.Lock()
	s := h.getOrCreateSession(code)
	if len(s.subs) >= MaxSceneSubsPerSession {
		h.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	s.subs[ch] = struct{}{}
	h.mu.Unlock()

	unsubscribe := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		s, ok := h.sessions[code]
		if !ok {
			return
		}
		if _, ok := s.subs[ch]; ok {
			delete(s.subs, ch)
			close(ch)
		}
	}

	return ch, unsubscribe
}

// Broadcast sends scene data to all viewers subscribed to a producer session.
func (h *SceneHub) Broadcast(sessionCode string, data []byte) {
	h.mu.Lock()
	s, ok := h.sessions[sessionCode]
	if !ok {
		h.mu.Unlock()
		return
	}

	// Snapshot subs under lock, then publish without holding the lock.
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
