package shownote

import (
	"context"

	"github.com/jackc/pgx/v5/pgconn"
	"thirdcoast.systems/rewind/internal/db"
)

// StartRoomEventNotifications forwards PostgreSQL room notifications into the
// per-note SSE hub until ctx is cancelled.
func StartRoomEventNotifications(ctx context.Context, dbc *db.DatabaseConnection, hub *Hub) {
	go db.RunListenLoop(ctx, dbc, []string{"show_note_room_events"}, func(n *pgconn.Notification) {
		hub.Broadcast(n.Payload, Event{Kind: "room"})
	})
}
