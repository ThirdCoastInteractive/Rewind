package shownote_api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"github.com/reearth/ygo/crdt"
	ygowebsocket "github.com/reearth/ygo/provider/websocket"

	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	roomhub "thirdcoast.systems/rewind/cmd/web/internal/shownote"
	"thirdcoast.systems/rewind/internal/db"
	workspace "thirdcoast.systems/rewind/internal/shownote"
)

type workspaceState struct {
	Note       *db.ShowNote                         `json:"note"`
	Document   *db.ShowNoteDocument                 `json:"document"`
	References []*db.ShowNoteReference              `json:"references"`
	Messages   []*db.ShowNoteRoomMessage            `json:"messages"`
	Reviews    []*db.ShowNoteReviewThread           `json:"reviews"`
	Replies    map[string][]*db.ShowNoteReviewReply `json:"replies"`
	Agents     []*db.ShowNoteAgentLease             `json:"agents"`
	Cursor     int64                                `json:"cursor"`
	ReadOnly   bool                                 `json:"read_only"`
}

func requireWorkspaceAccess(c echo.Context, sm *auth.SessionManager, dbc *db.DatabaseConnection) (pgtype.UUID, pgtype.UUID, string, workspace.Access, error) {
	userID, username, err := common.RequireSessionUser(c, sm)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, "", workspace.Access{}, err
	}
	noteID, err := common.RequireUUIDParam(c, "id")
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, "", workspace.Access{}, err
	}
	access := workspace.UserAccess(c.Request().Context(), dbc, noteID, userID)
	if !access.Allowed {
		return pgtype.UUID{}, pgtype.UUID{}, "", workspace.Access{}, echo.NewHTTPError(403, "forbidden")
	}
	return noteID, userID, username, access, nil
}

// HandleWorkspaceState returns the current SQL projection used by panels and MCP-like clients.
func HandleWorkspaceState(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		noteID, _, _, access, err := requireWorkspaceAccess(c, sm, dbc)
		if err != nil {
			return err
		}
		ctx := c.Request().Context()
		if err := workspace.EnsureWorkspaceDocument(ctx, dbc, noteID); err != nil {
			return echo.NewHTTPError(500, "workspace migration failed").SetInternal(err)
		}
		q := dbc.Queries(ctx)
		note, err := q.GetShowNote(ctx, noteID)
		if err != nil {
			return err
		}
		document, err := q.GetShowNoteDocument(ctx, noteID)
		if err != nil {
			return err
		}
		references, err := q.ListShowNoteReferences(ctx, noteID)
		if err != nil {
			return err
		}
		messages, err := q.ListShowNoteRoomMessages(ctx, &db.ListShowNoteRoomMessagesParams{ShowNoteID: noteID, ResultLimit: 100})
		if err != nil {
			return err
		}
		reviews, err := q.ListShowNoteReviewThreads(ctx, noteID)
		if err != nil {
			return err
		}
		replies := make(map[string][]*db.ShowNoteReviewReply, len(reviews))
		for _, review := range reviews {
			items, err := q.ListShowNoteReviewReplies(ctx, review.ID)
			if err != nil {
				return err
			}
			replies[review.ID.String()] = items
		}
		agents, err := q.ListActiveShowNoteAgentLeases(ctx, noteID)
		if err != nil {
			return err
		}
		cursor, err := q.GetShowNoteRoomCursor(ctx, noteID)
		if err != nil {
			return err
		}
		return c.JSON(200, workspaceState{
			Note: note, Document: document, References: references, Messages: messages,
			Reviews: reviews, Replies: replies, Agents: agents, Cursor: cursor, ReadOnly: access.ReadOnly,
		})
	}
}

// HandlePostReviewReply appends an auditable human reply to a review thread.
func HandlePostReviewReply(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		noteID, userID, username, access, err := requireWorkspaceAccess(c, sm, dbc)
		if err != nil {
			return err
		}
		if access.ReadOnly {
			return echo.NewHTTPError(403, "viewer access is read-only")
		}
		threadID, err := common.RequireUUIDParam(c, "threadId")
		if err != nil {
			return err
		}
		var request struct {
			Body string `json:"body"`
		}
		if err := json.NewDecoder(c.Request().Body).Decode(&request); err != nil {
			return echo.NewHTTPError(400, "invalid body")
		}
		request.Body = strings.TrimSpace(request.Body)
		if request.Body == "" || len(request.Body) > 20_000 {
			return echo.NewHTTPError(400, "reply body is required and must be under 20,000 characters")
		}

		ctx := c.Request().Context()
		txq, tx, err := dbc.NewWithTX(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		thread, err := txq.GetShowNoteReviewThread(ctx, threadID)
		if err != nil || thread.ShowNoteID != noteID {
			return echo.NewHTTPError(404, "review not found")
		}
		reply, err := txq.CreateShowNoteReviewReply(ctx, &db.CreateShowNoteReviewReplyParams{
			ThreadID: threadID, ActorKind: "human", ActorUserID: userID,
			ActorName: username, Body: request.Body,
		})
		if err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"thread_id": threadID.String(), "reply_id": reply.ID.String(), "body": reply.Body})
		if _, err := txq.CreateShowNoteRoomEvent(ctx, &db.CreateShowNoteRoomEventParams{
			ShowNoteID: noteID, EventType: "review_reply", ActorKind: "human",
			ActorUserID: userID, ActorName: username, Payload: payload,
		}); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return c.JSON(201, reply)
	}
}

// HandlePostRoomMessage appends a human message and its chronological room event atomically.
func HandlePostRoomMessage(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		noteID, userID, username, _, err := requireWorkspaceAccess(c, sm, dbc)
		if err != nil {
			return err
		}
		var request struct {
			Body    string      `json:"body"`
			ReplyTo pgtype.UUID `json:"reply_to"`
		}
		if err := json.NewDecoder(c.Request().Body).Decode(&request); err != nil {
			return echo.NewHTTPError(400, "invalid body")
		}
		request.Body = strings.TrimSpace(request.Body)
		if request.Body == "" || len(request.Body) > 20_000 {
			return echo.NewHTTPError(400, "message must be between 1 and 20000 characters")
		}
		ctx := c.Request().Context()
		q, tx, err := dbc.NewWithTX(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		message, err := q.CreateShowNoteRoomMessage(ctx, &db.CreateShowNoteRoomMessageParams{
			ShowNoteID: noteID, ActorKind: "human", ActorUserID: userID,
			ActorName: username, Body: request.Body, ReplyTo: request.ReplyTo,
		})
		if err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"message_id": message.ID.String(), "body": message.Body})
		event, err := q.CreateShowNoteRoomEvent(ctx, &db.CreateShowNoteRoomEventParams{
			ShowNoteID: noteID, EventType: "message", ActorKind: "human",
			ActorUserID: userID, ActorName: username, Payload: payload,
		})
		if err != nil {
			return err
		}
		if err := q.SetShowNoteRoomMessageEventCursor(ctx, &db.SetShowNoteRoomMessageEventCursorParams{
			EventCursor: &event.Cursor, ID: message.ID,
		}); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		message.EventCursor = &event.Cursor
		return c.JSON(201, message)
	}
}

// HandleRoomEventStream streams persisted room events after a monotonic cursor.
func HandleRoomEventStream(sm *auth.SessionManager, dbc *db.DatabaseConnection, hub *roomhub.Hub) echo.HandlerFunc {
	return func(c echo.Context) error {
		noteID, _, _, _, err := requireWorkspaceAccess(c, sm, dbc)
		if err != nil {
			return err
		}
		cursor, _ := strconv.ParseInt(c.QueryParam("cursor"), 10, 64)
		if resumed, err := strconv.ParseInt(c.Request().Header.Get("Last-Event-ID"), 10, 64); err == nil && resumed > cursor {
			cursor = resumed
		}
		ctx := c.Request().Context()
		response := c.Response()
		flusher, ok := response.Writer.(http.Flusher)
		if !ok {
			return echo.NewHTTPError(500, "streaming unsupported")
		}
		common.SetSSEHeaders(c)
		changes, unsubscribe := hub.Subscribe(noteID.String())
		defer unsubscribe()
		q := dbc.Queries(ctx)
		emit := func() error {
			events, err := q.ListShowNoteRoomEventsAfter(ctx, &db.ListShowNoteRoomEventsAfterParams{
				ShowNoteID: noteID, Cursor: cursor, ResultLimit: 100,
			})
			if err != nil {
				return err
			}
			for _, event := range events {
				payload := map[string]any{
					"cursor":  event.Cursor,
					"type":    event.EventType,
					"payload": json.RawMessage(event.Payload),
				}
				encoded, err := json.Marshal(payload)
				if err != nil {
					return err
				}
				if _, err := fmt.Fprintf(response, "id: %d\nevent: room\ndata: %s\n\n", event.Cursor, encoded); err != nil {
					return err
				}
				cursor = event.Cursor
			}
			flusher.Flush()
			return nil
		}
		if err := emit(); err != nil {
			return err
		}
		_, _ = fmt.Fprint(response, ": connected\n\n")
		flusher.Flush()
		keepalive := time.NewTicker(15 * time.Second)
		defer keepalive.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case data, ok := <-changes:
				if !ok {
					return nil
				}
				var notification roomhub.Event
				if json.Unmarshal(data, &notification) == nil && notification.Kind == "room" {
					if err := emit(); err != nil {
						return err
					}
				}
			case <-keepalive.C:
				_, _ = fmt.Fprint(response, ": keepalive\n\n")
				flusher.Flush()
			}
		}
	}
}

// HandleCreateReview creates an anchored comment or validated patch suggestion.
func HandleCreateReview(sm *auth.SessionManager, dbc *db.DatabaseConnection, collab *ygowebsocket.Server) echo.HandlerFunc {
	return func(c echo.Context) error {
		noteID, userID, username, access, err := requireWorkspaceAccess(c, sm, dbc)
		if err != nil {
			return err
		}
		if access.ReadOnly {
			return echo.NewHTTPError(403, "viewer access is read-only")
		}
		var request struct {
			Kind         string `json:"kind"`
			Body         string `json:"body"`
			Summary      string `json:"summary"`
			BaseRevision int64  `json:"base_revision"`
			ExpectedText string `json:"expected_text"`
			Patch        string `json:"patch"`
			StartLine    int    `json:"start_line"`
			StartColumn  int    `json:"start_column"`
			EndLine      int    `json:"end_line"`
			EndColumn    int    `json:"end_column"`
		}
		if err := json.NewDecoder(c.Request().Body).Decode(&request); err != nil {
			return echo.NewHTTPError(400, "invalid body")
		}
		if request.Kind != "comment" && request.Kind != "suggestion" {
			return echo.NewHTTPError(400, "kind must be comment or suggestion")
		}
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		document, err := q.GetShowNoteDocument(ctx, noteID)
		if err != nil {
			return err
		}
		if document.Revision != request.BaseRevision {
			return c.JSON(409, map[string]any{"error": "stale revision", "document": document})
		}
		selected, startOffset, endOffset, err := workspace.MarkdownRange(document.Markdown, request.StartLine, request.StartColumn, request.EndLine, request.EndColumn)
		if err != nil {
			return echo.NewHTTPError(400, err.Error())
		}
		if selected != request.ExpectedText {
			return c.JSON(409, map[string]any{"error": "selected text changed", "document": document})
		}
		if request.Kind == "suggestion" {
			proposed, err := workspace.ApplyUnifiedDiff(document.Markdown, request.Patch)
			if err != nil {
				return echo.NewHTTPError(400, fmt.Sprintf("invalid patch: %v", err))
			}
			change := workspace.ChangedTextRange(document.Markdown, proposed)
			if change.StartUTF16 != startOffset || change.EndUTF16 != endOffset || change.Before != selected {
				return echo.NewHTTPError(400, "suggestion selection must exactly cover the patch's changed text")
			}
		}
		var anchorStart, anchorEnd []byte
		if err := collab.Apply(ctx, noteID.String(), func(doc *crdt.Doc, _ func(func(*crdt.Transaction))) {
			text := doc.GetText("markdown")
			anchorStart = crdt.EncodeRelativePosition(crdt.CreateRelativePositionFromIndex(text, startOffset, 0))
			anchorEnd = crdt.EncodeRelativePosition(crdt.CreateRelativePositionFromIndex(text, endOffset, 0))
		}); err != nil {
			return err
		}
		txq, tx, err := dbc.NewWithTX(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		thread, err := txq.CreateShowNoteReviewThread(ctx, &db.CreateShowNoteReviewThreadParams{
			ShowNoteID: noteID, Kind: request.Kind, ActorKind: "human", ActorUserID: userID,
			ActorName: username, Body: request.Body, Summary: request.Summary,
			BaseRevision: request.BaseRevision, BaseMarkdown: document.Markdown, ExpectedText: request.ExpectedText, Patch: request.Patch,
			AnchorStart: anchorStart, AnchorEnd: anchorEnd, StartLine: int32(request.StartLine),
			StartColumn: int32(request.StartColumn), EndLine: int32(request.EndLine), EndColumn: int32(request.EndColumn),
		})
		if err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"thread_id": thread.ID.String(), "kind": thread.Kind, "summary": thread.Summary})
		if _, err := txq.CreateShowNoteRoomEvent(ctx, &db.CreateShowNoteRoomEventParams{
			ShowNoteID: noteID, EventType: thread.Kind, ActorKind: "human", ActorUserID: userID,
			ActorName: username, Payload: payload,
		}); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return c.JSON(201, thread)
	}
}

// HandleReviewStatus resolves, reopens, rejects, or accepts a review thread.
func HandleReviewStatus(sm *auth.SessionManager, dbc *db.DatabaseConnection, collab *ygowebsocket.Server) echo.HandlerFunc {
	return func(c echo.Context) error {
		noteID, userID, err := requireEditor(c, sm, dbc)
		if err != nil {
			return err
		}
		threadID, err := common.RequireUUIDParam(c, "threadId")
		if err != nil {
			return err
		}
		var request struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(c.Request().Body).Decode(&request); err != nil {
			return echo.NewHTTPError(400, "invalid body")
		}
		if request.Status != "resolved" && request.Status != "open" && request.Status != "rejected" && request.Status != "accepted" {
			return echo.NewHTTPError(400, "invalid review status")
		}
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		thread, err := q.GetShowNoteReviewThread(ctx, threadID)
		if err != nil || thread.ShowNoteID != noteID {
			return echo.NewHTTPError(404, "review not found")
		}
		if request.Status == "accepted" {
			accepted, update, err := workspace.AcceptSuggestion(ctx, dbc, noteID, threadID, userID)
			if err != nil {
				return echo.NewHTTPError(409, err.Error())
			}
			// The database already owns the update. Broadcast failure cannot discard acceptance.
			if err = collab.BroadcastUpdate(ctx, noteID.String(), update); err != nil {
				return c.JSON(202, map[string]any{"review": accepted, "sync_pending": true})
			}
			return c.JSON(200, accepted)
		}
		updated, err := q.UpdateShowNoteReviewStatus(ctx, &db.UpdateShowNoteReviewStatusParams{
			ID: threadID, Status: request.Status, Detached: thread.Detached, ClosedByUserID: userID,
		})
		if err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"thread_id": threadID.String(), "status": updated.Status})
		if _, err := q.CreateShowNoteRoomEvent(ctx, &db.CreateShowNoteRoomEventParams{
			ShowNoteID: noteID, EventType: "review_status", ActorKind: "human", ActorUserID: userID,
			Payload: payload,
		}); err != nil {
			return err
		}
		return c.JSON(200, updated)
	}
}
