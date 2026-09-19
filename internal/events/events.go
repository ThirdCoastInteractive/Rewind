// Package events fans database notifications out to live HTTP and MCP consumers.
package events

import (
	"context"
	"slices"
	"sync"

	"github.com/jackc/pgx/v5/pgconn"
	"thirdcoast.systems/rewind/internal/db"
)

// Hub coalesces notifications; callers always reread authoritative database state.
type Hub struct {
	mu        sync.Mutex
	next      int
	listeners map[int]subscription
	once      sync.Once
}

// Default is the application-wide database event fanout.
var Default = &Hub{listeners: map[int]subscription{}}

type subscription struct {
	wake     chan struct{}
	channels []string
}

// Subscribe returns a bounded wake channel and its cleanup function. Optional
// channel names filter ordinary events; listener reconnects wake every subscriber.
func (h *Hub) Subscribe(channels ...string) (<-chan struct{}, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.next++
	id := h.next
	ch := make(chan struct{}, 1)
	if h.listeners == nil {
		h.listeners = map[int]subscription{}
	}
	h.listeners[id] = subscription{wake: ch, channels: slices.Clone(channels)}
	return ch, func() { h.mu.Lock(); delete(h.listeners, id); h.mu.Unlock() }
}
func (h *Hub) notify(channel string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, listener := range h.listeners {
		if channel != "" && len(listener.channels) > 0 && !slices.Contains(listener.channels, channel) {
			continue
		}
		select {
		case listener.wake <- struct{}{}:
		default:
		}
	}
}

// Start maintains a single LISTEN connection, including reconnect wakeups.
func (h *Hub) Start(ctx context.Context, dbc *db.DatabaseConnection) {
	h.once.Do(func() {
		go db.RunListenLoop(ctx, dbc, []string{
			"visual_changed", "ml_jobs", "show_note_room_events", "show_note_documents",
			"context_windows_changed", "download_jobs", "stitch_jobs", "compilation_changed",
			"jobs_ui", "stitch_projects_changed",
		}, func(n *pgconn.Notification) { h.notify(n.Channel) }, func() { h.notify("") })
	})
}
