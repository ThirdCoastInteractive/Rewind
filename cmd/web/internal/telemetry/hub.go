package telemetry

import (
	"sync"
	"time"
)

const (
	// If a remote stops reporting for this long, treat it as gone.
	// Remotes are removed immediately on SSE disconnect; this is a fallback for
	// cases where disconnect isn't observed (network drops, process kill, etc.).
	RemoteStaleAfter = 60 * time.Second

	// Hard caps to keep the web process responsive even if someone opens
	// a silly number of tabs. These can be revisited later.
	maxProducerStreamsPerSession = 5
	maxPlayerStreamsPerSession   = 50
	maxTotalStreams              = 200
)

// EventType distinguishes telemetry event kinds.
type EventType int

const (
	EventUpsertRemote EventType = iota
	EventRemoveRemote
)

// Event is a telemetry event broadcast to subscribers.
type Event struct {
	Typ       EventType
	Session   string
	RemoteKey string
	Telemetry RemoteTelemetry
}

// RemoteTelemetry holds live metrics for a connected remote player.
type RemoteTelemetry struct {
	RemoteKey  string
	RemoteID   string
	FirstSeen  time.Time
	LastSeen   time.Time
	RTTMs      int
	JitterMs   int
	OffsetMs   int
	Auth       string
	UserAgent  string
	RemoteIP   string
	Visibility string
}

// Hub manages per-session telemetry state and subscriber notifications.
type Hub struct {
	mu sync.Mutex

	sessions map[string]*session

	totalStreams int
}

type session struct {
	producerStreams int
	playerStreams   int

	remotes map[string]RemoteTelemetry
	subs    map[chan Event]struct{}
}

// NewHub creates a new telemetry hub.
func NewHub() *Hub {
	return &Hub{
		sessions: make(map[string]*session),
	}
}

func (h *Hub) getOrCreateSession(code string) *session {
	s, ok := h.sessions[code]
	if ok {
		return s
	}

	s = &session{
		remotes: make(map[string]RemoteTelemetry),
		subs:    make(map[chan Event]struct{}),
	}
	h.sessions[code] = s
	return s
}

// AcquireProducerStream attempts to reserve a producer SSE slot for the given session.
func (h *Hub) AcquireProducerStream(code string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.totalStreams >= maxTotalStreams {
		return false
	}

	s := h.getOrCreateSession(code)
	if s.producerStreams >= maxProducerStreamsPerSession {
		return false
	}

	s.producerStreams++
	h.totalStreams++
	return true
}

// ReleaseProducerStream frees a producer SSE slot for the given session.
func (h *Hub) ReleaseProducerStream(code string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	s, ok := h.sessions[code]
	if !ok {
		return
	}
	if s.producerStreams > 0 {
		s.producerStreams--
	}
	if h.totalStreams > 0 {
		h.totalStreams--
	}
}

// AcquirePlayerStream attempts to reserve a player SSE slot for the given session.
func (h *Hub) AcquirePlayerStream(code string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.totalStreams >= maxTotalStreams {
		return false
	}

	s := h.getOrCreateSession(code)
	if s.playerStreams >= maxPlayerStreamsPerSession {
		return false
	}

	s.playerStreams++
	h.totalStreams++
	return true
}

// ReleasePlayerStream frees a player SSE slot for the given session.
func (h *Hub) ReleasePlayerStream(code string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	s, ok := h.sessions[code]
	if !ok {
		return
	}
	if s.playerStreams > 0 {
		s.playerStreams--
	}
	if h.totalStreams > 0 {
		h.totalStreams--
	}
}

// Subscribe returns a channel that receives telemetry events for the given session, and an unsubscribe function.
func (h *Hub) Subscribe(code string) (<-chan Event, func()) {
	ch := make(chan Event, 32)

	h.mu.Lock()
	s := h.getOrCreateSession(code)
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

// ListRemotes returns a snapshot of all known remotes for the given session.
func (h *Hub) ListRemotes(code string) []RemoteTelemetry {
	h.mu.Lock()
	defer h.mu.Unlock()

	s, ok := h.sessions[code]
	if !ok {
		return nil
	}

	out := make([]RemoteTelemetry, 0, len(s.remotes))
	for _, rt := range s.remotes {
		out = append(out, rt)
	}
	return out
}

// UpsertRemote adds or updates a remote and notifies subscribers.
func (h *Hub) UpsertRemote(code string, t RemoteTelemetry) {
	h.mu.Lock()
	s := h.getOrCreateSession(code)

	if t.RemoteKey == "" {
		t.RemoteKey = t.RemoteID
	}

	prev, had := s.remotes[t.RemoteKey]
	if !had {
		t.FirstSeen = t.LastSeen
	} else {
		t.FirstSeen = prev.FirstSeen
	}

	s.remotes[t.RemoteKey] = t

	evt := Event{
		Typ:       EventUpsertRemote,
		Session:   code,
		RemoteKey: t.RemoteKey,
		Telemetry: t,
	}

	for sub := range s.subs {
		select {
		case sub <- evt:
		default:
			// Drop rather than block the webserver.
		}
	}
	h.mu.Unlock()
}

// RemoveRemote deletes a remote and notifies subscribers.
func (h *Hub) RemoveRemote(code string, remoteKey string) {
	h.mu.Lock()
	s, ok := h.sessions[code]
	if !ok {
		h.mu.Unlock()
		return
	}

	if _, ok := s.remotes[remoteKey]; !ok {
		h.mu.Unlock()
		return
	}

	delete(s.remotes, remoteKey)

	evt := Event{
		Typ:       EventRemoveRemote,
		Session:   code,
		RemoteKey: remoteKey,
	}
	for sub := range s.subs {
		select {
		case sub <- evt:
		default:
			// Drop rather than block the webserver.
		}
	}

	h.mu.Unlock()
}

// TouchRemote updates the LastSeen timestamp for a remote and notifies subscribers.
func (h *Hub) TouchRemote(code string, remoteKey string, now time.Time) bool {
	h.mu.Lock()
	s, ok := h.sessions[code]
	if !ok {
		h.mu.Unlock()
		return false
	}

	rt, ok := s.remotes[remoteKey]
	if !ok {
		h.mu.Unlock()
		return false
	}

	rt.LastSeen = now
	s.remotes[remoteKey] = rt

	evt := Event{
		Typ:       EventUpsertRemote,
		Session:   code,
		RemoteKey: remoteKey,
		Telemetry: rt,
	}
	for sub := range s.subs {
		select {
		case sub <- evt:
		default:
			// Drop rather than block the webserver.
		}
	}

	h.mu.Unlock()
	return true
}

// UpdateRemoteFromTelemetry updates metrics for an existing remote and notifies subscribers.
func (h *Hub) UpdateRemoteFromTelemetry(code string, remoteKey string, rttMs int, jitterMs int, offsetMs int, visibility string, now time.Time) bool {
	h.mu.Lock()
	s, ok := h.sessions[code]
	if !ok {
		h.mu.Unlock()
		return false
	}

	rt, ok := s.remotes[remoteKey]
	if !ok {
		h.mu.Unlock()
		return false
	}

	rt.LastSeen = now
	rt.RTTMs = rttMs
	rt.JitterMs = jitterMs
	rt.OffsetMs = offsetMs
	rt.Visibility = visibility
	s.remotes[remoteKey] = rt

	evt := Event{
		Typ:       EventUpsertRemote,
		Session:   code,
		RemoteKey: remoteKey,
		Telemetry: rt,
	}
	for sub := range s.subs {
		select {
		case sub <- evt:
		default:
			// Drop rather than block the webserver.
		}
	}

	h.mu.Unlock()
	return true
}

// PruneStale removes remotes that haven't reported within the stale threshold.
func (h *Hub) PruneStale(code string, now time.Time) int {
	h.mu.Lock()
	s, ok := h.sessions[code]
	if !ok {
		h.mu.Unlock()
		return 0
	}

	removed := 0
	for remoteKey, rt := range s.remotes {
		if now.Sub(rt.LastSeen) <= RemoteStaleAfter {
			continue
		}

		delete(s.remotes, remoteKey)
		removed++

		evt := Event{
			Typ:       EventRemoveRemote,
			Session:   code,
			RemoteKey: remoteKey,
		}
		for sub := range s.subs {
			select {
			case sub <- evt:
			default:
				// Drop rather than block the webserver.
			}
		}
	}

	h.mu.Unlock()
	return removed
}
