package sfu

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"

	"thirdcoast.systems/rewind/internal/db"
)

// directorManager maintains the producer_connections table: live presence plus
// the single "director" (playback controller) per show note, with automatic
// election on join and handoff when the director disconnects.
type directorManager struct {
	dbc *db.DatabaseConnection
}

// onJoin registers a host connection and elects it director if none is active.
func (dm *directorManager) onJoin(ctx context.Context, room, user pgtype.UUID) {
	q := dm.dbc.Queries(ctx)
	if _, err := q.UpsertProducerConnection(ctx, &db.UpsertProducerConnectionParams{ShowNoteID: room, UserID: user}); err != nil {
		slog.Error("sfu: upsert producer connection", "error", err)
		return
	}
	if _, err := q.GetDirector(ctx, room); err != nil {
		// No active director — make the joining host the director.
		if err := q.SetDirector(ctx, &db.SetDirectorParams{ShowNoteID: room, UserID: user}); err != nil {
			slog.Error("sfu: elect director", "error", err)
		}
	}
}

// ping refreshes a connection's liveness.
func (dm *directorManager) ping(ctx context.Context, room, user pgtype.UUID) {
	_ = dm.dbc.Queries(ctx).UpdateConnectionPing(ctx, &db.UpdateConnectionPingParams{ShowNoteID: room, UserID: user})
}

// onLeave removes a host connection and, if it was the director, hands off to
// the oldest remaining active host.
func (dm *directorManager) onLeave(ctx context.Context, room, user pgtype.UUID) {
	q := dm.dbc.Queries(ctx)
	conn, err := q.GetProducerConnection(ctx, &db.GetProducerConnectionParams{ShowNoteID: room, UserID: user})
	wasDirector := err == nil && conn.IsDirector

	if err := q.DeleteProducerConnection(ctx, &db.DeleteProducerConnectionParams{ShowNoteID: room, UserID: user}); err != nil {
		slog.Error("sfu: delete producer connection", "error", err)
	}
	if !wasDirector {
		return
	}
	actives, err := q.ListActiveConnections(ctx, room)
	if err != nil || len(actives) == 0 {
		return
	}
	_ = q.ClearDirector(ctx, room)
	if err := q.SetDirector(ctx, &db.SetDirectorParams{ShowNoteID: room, UserID: actives[0].UserID}); err != nil {
		slog.Error("sfu: hand off director", "error", err)
	}
}
