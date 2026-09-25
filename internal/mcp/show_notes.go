package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/reearth/ygo/crdt"

	"thirdcoast.systems/rewind/internal/db"
	eventhub "thirdcoast.systems/rewind/internal/events"
	"thirdcoast.systems/rewind/internal/shownote"
	"thirdcoast.systems/rewind/pkg/plugin"
)

const showNoteAgentLeaseDuration = 2 * time.Minute

type showNoteIDArgs struct {
	ShowNoteID string `json:"show_note_id" jsonschema:"Show-note UUID"`
}

type joinShowNoteArgs struct {
	ShowNoteID string `json:"show_note_id" jsonschema:"Show-note UUID"`
	AgentName  string `json:"agent_name,omitempty" jsonschema:"Display name for this agent presence"`
}

type waitShowNoteArgs struct {
	ShowNoteID string `json:"show_note_id"`
	LeaseID    string `json:"lease_id"`
	Cursor     int64  `json:"cursor"`
	TimeoutMS  int32  `json:"timeout_ms,omitempty" jsonschema:"Bounded wait in milliseconds, maximum 30000"`
}

type leaveShowNoteArgs struct {
	LeaseID string `json:"lease_id"`
}

type postShowNoteMessageArgs struct {
	ShowNoteID string `json:"show_note_id"`
	LeaseID    string `json:"lease_id"`
	Body       string `json:"body"`
}

type addShowNoteCommentArgs struct {
	ShowNoteID   string `json:"show_note_id"`
	LeaseID      string `json:"lease_id"`
	BaseRevision int64  `json:"base_revision"`
	StartLine    int32  `json:"start_line"`
	StartColumn  int32  `json:"start_column"`
	EndLine      int32  `json:"end_line"`
	EndColumn    int32  `json:"end_column"`
	ExpectedText string `json:"expected_selected_text"`
	Body         string `json:"body"`
}

type proposeShowNotePatchArgs struct {
	ShowNoteID   string `json:"show_note_id"`
	LeaseID      string `json:"lease_id"`
	BaseRevision int64  `json:"base_revision"`
	Summary      string `json:"summary"`
	Comment      string `json:"comment,omitempty"`
	Patch        string `json:"patch" jsonschema:"A validated single-document unified diff"`
}

func authorizeShowNoteSubscription(ctx context.Context, dbc *db.DatabaseConnection, uri string) error {
	const prefix = "rewind://show-note/"
	if !strings.HasPrefix(uri, prefix) {
		return fmt.Errorf("unsupported subscribable resource")
	}
	_, _, _, err := requireShowNoteAccess(ctx, dbc, strings.TrimPrefix(uri, prefix), false)
	return err
}

func registerShowNoteMCP(srv *mcpsdk.Server, dbc *db.DatabaseConnection) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "list_show_notes", Description: "List collaborative show notes available to the token's owning user. Requires mcp:read."}, listShowNotesMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "get_show_note", Description: "Read a show note's Markdown, revision, parsed references, review items, and room cursor. Requires mcp:read."}, getShowNoteMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "join_show_note_room", Description: "Join a show-note room as an external agent and receive a renewable presence lease. Requires mcp:write."}, joinShowNoteRoomMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "wait_show_note_events", Description: "Wait up to 30 seconds for room events and renew the agent presence lease. Requires mcp:write."}, waitShowNoteEventsMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "leave_show_note_room", Description: "Release an agent presence lease. Requires mcp:write."}, leaveShowNoteRoomMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "post_show_note_message", Description: "Post a chronological room message as the connected agent. Requires mcp:write."}, postShowNoteMessageMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "add_show_note_comment", Description: "Add a Yjs-anchored review comment using a revision and 1-based Markdown range. Requires mcp:write."}, addShowNoteCommentMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "propose_show_note_patch", Description: "Propose, but do not approve, a validated single-document unified diff. Requires mcp:write."}, proposeShowNotePatchMCP(dbc))
	srv.AddResourceTemplate(&mcpsdk.ResourceTemplate{
		Name: "show-note", Title: "Rewind show note", MIMEType: "application/json",
		Description: "Collaborative Markdown show-note workspace state.",
		URITemplate: "rewind://show-note/{id}",
	}, showNoteResource(dbc))
}

func requireShowNoteScope(ctx context.Context, scope string) error {
	tok := tokenFrom(ctx)
	if tok != nil {
		for _, candidate := range tok.Scopes {
			if candidate == scope {
				return nil
			}
		}
	}
	return fmt.Errorf("%s scope required", scope)
}

func showNoteUUID(raw string) (pgtype.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("invalid show-note UUID: %w", err)
	}
	return pgtype.UUID{Bytes: id, Valid: true}, nil
}

func requireShowNoteAccess(ctx context.Context, dbc *db.DatabaseConnection, rawID string, write bool) (pgtype.UUID, *db.APIToken, shownote.Access, error) {
	scope := "mcp:read"
	if write {
		scope = "mcp:write"
	}
	if err := requireShowNoteScope(ctx, scope); err != nil {
		return pgtype.UUID{}, nil, shownote.Access{}, err
	}
	tok := tokenFrom(ctx)
	noteID, err := showNoteUUID(rawID)
	if err != nil {
		return pgtype.UUID{}, nil, shownote.Access{}, err
	}
	access := shownote.UserAccess(ctx, dbc, noteID, tok.UserID)
	if !access.Allowed || write && access.ReadOnly {
		return pgtype.UUID{}, nil, shownote.Access{}, fmt.Errorf("show note is not available to this token's owning user")
	}
	if err := shownote.EnsureWorkspaceDocument(ctx, dbc, noteID); err != nil {
		return pgtype.UUID{}, nil, shownote.Access{}, err
	}
	return noteID, tok, access, nil
}

func listShowNotesMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *struct{}) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ *struct{}) (*mcpsdk.CallToolResult, any, error) {
		if err := requireShowNoteScope(ctx, "mcp:read"); err != nil {
			return nil, nil, err
		}
		rows, err := dbc.Queries(ctx).ListShowNotesForUser(ctx, tokenFrom(ctx).UserID)
		if err != nil {
			return nil, nil, err
		}
		if plugin.LiveIngest() != nil {
			tenant, terr := shownote.TenantForContext(ctx)
			if terr != nil {
				return nil, nil, terr
			}
			filtered := rows[:0]
			for _, row := range rows {
				if row.TenantID == tenant {
					filtered = append(filtered, row)
				}
			}
			rows = filtered
		}
		return jsonResult(rows)
	}
}

func getShowNoteMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *showNoteIDArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *showNoteIDArgs) (*mcpsdk.CallToolResult, any, error) {
		noteID, _, access, err := requireShowNoteAccess(ctx, dbc, args.ShowNoteID, false)
		if err != nil {
			return nil, nil, err
		}
		state, err := showNoteResourceState(ctx, dbc, noteID)
		if err != nil {
			return nil, nil, err
		}
		state["access_role"] = access.Role
		return jsonResult(state)
	}
}

func showNoteResource(dbc *db.DatabaseConnection) mcpsdk.ResourceHandler {
	return func(ctx context.Context, request *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
		const prefix = "rewind://show-note/"
		if request.Params == nil {
			return nil, mcpsdk.ResourceNotFoundError("")
		}
		if !strings.HasPrefix(request.Params.URI, prefix) {
			return nil, mcpsdk.ResourceNotFoundError(request.Params.URI)
		}
		rawID := strings.TrimPrefix(request.Params.URI, prefix)
		noteID, _, _, err := requireShowNoteAccess(ctx, dbc, rawID, false)
		if err != nil {
			return nil, mcpsdk.ResourceNotFoundError(request.Params.URI)
		}
		state, err := showNoteResourceState(ctx, dbc, noteID)
		if err != nil {
			return nil, err
		}
		body, err := json.MarshalIndent(state, "", "  ")
		if err != nil {
			return nil, err
		}
		return &mcpsdk.ReadResourceResult{Contents: []*mcpsdk.ResourceContents{{
			URI: request.Params.URI, MIMEType: "application/json", Text: string(body),
		}}}, nil
	}
}

func showNoteResourceState(ctx context.Context, dbc *db.DatabaseConnection, noteID pgtype.UUID) (map[string]any, error) {
	q := dbc.Queries(ctx)
	note, err := shownote.RequireTenant(ctx, dbc, noteID)
	if err != nil {
		return nil, err
	}
	document, err := q.GetShowNoteDocument(ctx, noteID)
	if err != nil {
		return nil, err
	}
	refs, err := q.ListShowNoteReferences(ctx, noteID)
	if err != nil {
		return nil, err
	}
	reviews, err := q.ListShowNoteReviewThreads(ctx, noteID)
	if err != nil {
		return nil, err
	}
	open := make([]*db.ShowNoteReviewThread, 0, len(reviews))
	for _, review := range reviews {
		if review.Status == "open" || review.Status == "stale" {
			open = append(open, review)
		}
	}
	cursor, err := q.GetShowNoteRoomCursor(ctx, noteID)
	if err != nil {
		return nil, err
	}
	agents, err := q.ListActiveShowNoteAgentLeases(ctx, noteID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id": uuidString(note.ID), "title": note.Title, "markdown": document.Markdown,
		"revision": document.Revision, "parsed_references": refs,
		"open_review_items": open, "room_cursor": cursor, "active_agents": agents,
	}, nil
}

func joinShowNoteRoomMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *joinShowNoteArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *joinShowNoteArgs) (*mcpsdk.CallToolResult, any, error) {
		noteID, tok, _, err := requireShowNoteAccess(ctx, dbc, args.ShowNoteID, true)
		if err != nil {
			return nil, nil, err
		}
		name := strings.TrimSpace(args.AgentName)
		if name == "" {
			name = tok.Name
		}
		cursor, err := dbc.Queries(ctx).GetShowNoteRoomCursor(ctx, noteID)
		if err != nil {
			return nil, nil, err
		}
		lease, err := dbc.Queries(ctx).CreateShowNoteAgentLease(ctx, &db.CreateShowNoteAgentLeaseParams{
			ShowNoteID: noteID, APITokenID: tok.ID, UserID: tok.UserID, AgentName: name,
			ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(showNoteAgentLeaseDuration), Valid: true}, LastCursor: cursor,
		})
		if err != nil {
			return nil, nil, err
		}
		payload, _ := json.Marshal(map[string]any{"lease_id": uuidString(lease.ID), "expires_at": lease.ExpiresAt})
		event, err := dbc.Queries(ctx).CreateShowNoteRoomEvent(ctx, &db.CreateShowNoteRoomEventParams{
			ShowNoteID: noteID, EventType: "agent_joined", ActorKind: "agent", ActorUserID: tok.UserID,
			ActorTokenID: tok.ID, ActorName: name, Payload: payload,
		})
		if err != nil {
			return nil, nil, err
		}
		state, err := showNoteResourceState(ctx, dbc, noteID)
		if err != nil {
			return nil, nil, err
		}
		state["lease_id"] = uuidString(lease.ID)
		state["lease_expires_at"] = lease.ExpiresAt
		state["room_cursor"] = event.Cursor
		return jsonResult(state)
	}
}

func requireAgentLease(ctx context.Context, dbc *db.DatabaseConnection, tok *db.APIToken, rawLease string, noteID pgtype.UUID) (*db.ShowNoteAgentLease, error) {
	leaseID, err := showNoteUUID(rawLease)
	if err != nil {
		return nil, fmt.Errorf("invalid lease UUID: %w", err)
	}
	lease, err := dbc.Queries(ctx).GetShowNoteAgentLease(ctx, &db.GetShowNoteAgentLeaseParams{ID: leaseID, APITokenID: tok.ID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("agent lease is missing or expired; join the room again")
		}
		return nil, err
	}
	if noteID.Valid && lease.ShowNoteID != noteID {
		return nil, fmt.Errorf("agent lease belongs to a different show note")
	}
	return lease, nil
}

func waitShowNoteEventsMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *waitShowNoteArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *waitShowNoteArgs) (*mcpsdk.CallToolResult, any, error) {
		noteID, tok, _, err := requireShowNoteAccess(ctx, dbc, args.ShowNoteID, true)
		if err != nil {
			return nil, nil, err
		}
		if _, err := requireAgentLease(ctx, dbc, tok, args.LeaseID, noteID); err != nil {
			return nil, nil, err
		}
		timeout := time.Duration(args.TimeoutMS) * time.Millisecond
		if timeout <= 0 || timeout > 30*time.Second {
			timeout = 30 * time.Second
		}
		deadline := time.NewTimer(timeout)
		defer deadline.Stop()
		wake, unsubscribe := eventhub.Default.Subscribe()
		defer unsubscribe()
		var events []*db.ShowNoteRoomEvent
		for {
			events, err = dbc.Queries(ctx).ListShowNoteRoomEventsAfter(ctx, &db.ListShowNoteRoomEventsAfterParams{ShowNoteID: noteID, Cursor: args.Cursor, ResultLimit: 100})
			if err != nil || len(events) > 0 {
				break
			}
			select {
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			case <-deadline.C:
				goto done
			case <-wake:
			}
		}
	done:
		if err != nil {
			return nil, nil, err
		}
		cursor := args.Cursor
		if len(events) > 0 {
			cursor = events[len(events)-1].Cursor
		}
		leaseID, _ := showNoteUUID(args.LeaseID)
		lease, err := dbc.Queries(ctx).RenewShowNoteAgentLease(ctx, &db.RenewShowNoteAgentLeaseParams{
			ID: leaseID, APITokenID: tok.ID, ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(showNoteAgentLeaseDuration), Valid: true}, LastCursor: cursor,
		})
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{"events": roomEventPayloads(events), "cursor": cursor, "lease_expires_at": lease.ExpiresAt})
	}
}

func leaveShowNoteRoomMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *leaveShowNoteArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *leaveShowNoteArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		leaseID, err := showNoteUUID(args.LeaseID)
		if err != nil {
			return nil, nil, err
		}
		tok := tokenFrom(ctx)
		lease, err := dbc.Queries(ctx).GetShowNoteAgentLease(ctx, &db.GetShowNoteAgentLeaseParams{ID: leaseID, APITokenID: tok.ID})
		if err != nil {
			return nil, nil, err
		}
		err = dbc.Queries(ctx).DeleteShowNoteAgentLease(ctx, &db.DeleteShowNoteAgentLeaseParams{ID: leaseID, APITokenID: tok.ID})
		if err != nil {
			return nil, nil, err
		}
		payload, _ := json.Marshal(map[string]any{"lease_id": args.LeaseID})
		if _, err := dbc.Queries(ctx).CreateShowNoteRoomEvent(ctx, &db.CreateShowNoteRoomEventParams{
			ShowNoteID: lease.ShowNoteID, EventType: "agent_left", ActorKind: "agent", ActorUserID: tok.UserID,
			ActorTokenID: tok.ID, ActorName: lease.AgentName, Payload: payload,
		}); err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{"left": true})
	}
}

func postShowNoteMessageMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *postShowNoteMessageArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *postShowNoteMessageArgs) (*mcpsdk.CallToolResult, any, error) {
		noteID, tok, _, err := requireShowNoteAccess(ctx, dbc, args.ShowNoteID, true)
		if err != nil {
			return nil, nil, err
		}
		lease, err := requireAgentLease(ctx, dbc, tok, args.LeaseID, noteID)
		if err != nil {
			return nil, nil, err
		}
		body := strings.TrimSpace(args.Body)
		if body == "" {
			return nil, nil, fmt.Errorf("message body is required")
		}
		txq, tx, err := dbc.NewWithTX(ctx)
		if err != nil {
			return nil, nil, err
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		message, err := txq.CreateShowNoteRoomMessage(ctx, &db.CreateShowNoteRoomMessageParams{
			ShowNoteID: noteID, ActorKind: "agent", ActorUserID: tok.UserID, ActorTokenID: tok.ID,
			ActorName: lease.AgentName, Body: body,
		})
		if err != nil {
			return nil, nil, err
		}
		payload, _ := json.Marshal(map[string]any{"message_id": uuidString(message.ID), "body": body})
		event, err := txq.CreateShowNoteRoomEvent(ctx, &db.CreateShowNoteRoomEventParams{
			ShowNoteID: noteID, EventType: "message", ActorKind: "agent", ActorUserID: tok.UserID,
			ActorTokenID: tok.ID, ActorName: lease.AgentName, Payload: payload,
		})
		if err != nil {
			return nil, nil, err
		}
		if err := txq.SetShowNoteRoomMessageEventCursor(ctx, &db.SetShowNoteRoomMessageEventCursorParams{ID: message.ID, EventCursor: &event.Cursor}); err != nil {
			return nil, nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, nil, err
		}
		message.EventCursor = &event.Cursor
		return jsonResult(message)
	}
}

func addShowNoteCommentMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *addShowNoteCommentArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *addShowNoteCommentArgs) (*mcpsdk.CallToolResult, any, error) {
		noteID, tok, _, err := requireShowNoteAccess(ctx, dbc, args.ShowNoteID, true)
		if err != nil {
			return nil, nil, err
		}
		lease, err := requireAgentLease(ctx, dbc, tok, args.LeaseID, noteID)
		if err != nil {
			return nil, nil, err
		}
		document, err := dbc.Queries(ctx).GetShowNoteDocument(ctx, noteID)
		if err != nil {
			return nil, nil, err
		}
		if document.Revision != args.BaseRevision {
			return staleShowNoteResult(document)
		}
		selected, startOffset, endOffset, err := shownote.MarkdownRange(document.Markdown, int(args.StartLine), int(args.StartColumn), int(args.EndLine), int(args.EndColumn))
		if err != nil {
			return nil, nil, err
		}
		if selected != args.ExpectedText {
			return staleShowNoteResult(document)
		}
		return createAgentReview(ctx, dbc, noteID, tok, lease.AgentName, "comment", strings.TrimSpace(args.Body), "", document.Revision, document.Markdown, selected, "", startOffset, endOffset, args.StartLine, args.StartColumn, args.EndLine, args.EndColumn)
	}
}

func proposeShowNotePatchMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *proposeShowNotePatchArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *proposeShowNotePatchArgs) (*mcpsdk.CallToolResult, any, error) {
		noteID, tok, _, err := requireShowNoteAccess(ctx, dbc, args.ShowNoteID, true)
		if err != nil {
			return nil, nil, err
		}
		lease, err := requireAgentLease(ctx, dbc, tok, args.LeaseID, noteID)
		if err != nil {
			return nil, nil, err
		}
		document, err := dbc.Queries(ctx).GetShowNoteDocument(ctx, noteID)
		if err != nil {
			return nil, nil, err
		}
		if document.Revision != args.BaseRevision {
			return staleShowNoteResult(document)
		}
		updated, err := shownote.ApplyUnifiedDiff(document.Markdown, args.Patch)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid patch: %w", err)
		}
		change := shownote.ChangedTextRange(document.Markdown, updated)
		return createAgentReview(ctx, dbc, noteID, tok, lease.AgentName, "suggestion", strings.TrimSpace(args.Comment), strings.TrimSpace(args.Summary), document.Revision, document.Markdown, change.Before, args.Patch, change.StartUTF16, change.EndUTF16, int32(change.StartLine), int32(change.StartCol), int32(change.EndLine), int32(change.EndCol))
	}
}

func createAgentReview(ctx context.Context, dbc *db.DatabaseConnection, noteID pgtype.UUID, tok *db.APIToken, actorName, kind, body, summary string, revision int64, baseMarkdown, expected, patch string, startOffset, endOffset int, startLine, startColumn, endLine, endColumn int32) (*mcpsdk.CallToolResult, any, error) {
	state, err := shownote.NewPostgresPersistence(dbc).LoadDoc(uuidString(noteID))
	if err != nil {
		return nil, nil, err
	}
	doc := crdt.New()
	if len(state) > 0 {
		if err := crdt.ApplyUpdateV1(doc, state, nil); err != nil {
			return nil, nil, err
		}
	}
	text := doc.GetText("markdown")
	anchorStart := crdt.EncodeRelativePosition(crdt.CreateRelativePositionFromIndex(text, startOffset, 0))
	anchorEnd := crdt.EncodeRelativePosition(crdt.CreateRelativePositionFromIndex(text, endOffset, 0))
	txq, tx, err := dbc.NewWithTX(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	thread, err := txq.CreateShowNoteReviewThread(ctx, &db.CreateShowNoteReviewThreadParams{
		ShowNoteID: noteID, Kind: kind, ActorKind: "agent", ActorUserID: tok.UserID,
		ActorTokenID: tok.ID, ActorName: actorName, Body: body, Summary: summary,
		BaseRevision: revision, BaseMarkdown: baseMarkdown, ExpectedText: expected, Patch: patch,
		AnchorStart: anchorStart, AnchorEnd: anchorEnd, StartLine: startLine,
		StartColumn: startColumn, EndLine: endLine, EndColumn: endColumn,
	})
	if err != nil {
		return nil, nil, err
	}
	payload, _ := json.Marshal(map[string]any{"thread_id": uuidString(thread.ID), "kind": kind, "summary": summary})
	if _, err := txq.CreateShowNoteRoomEvent(ctx, &db.CreateShowNoteRoomEventParams{
		ShowNoteID: noteID, EventType: kind, ActorKind: "agent", ActorUserID: tok.UserID,
		ActorTokenID: tok.ID, ActorName: actorName, Payload: payload,
	}); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	return jsonResult(thread)
}

func staleShowNoteResult(document *db.ShowNoteDocument) (*mcpsdk.CallToolResult, any, error) {
	return jsonResult(map[string]any{"stale": true, "current_revision": document.Revision, "markdown": document.Markdown})
}

func roomEventPayloads(events []*db.ShowNoteRoomEvent) []map[string]any {
	out := make([]map[string]any, 0, len(events))
	for _, event := range events {
		var payload any
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		out = append(out, map[string]any{
			"cursor": event.Cursor, "type": event.EventType, "actor_kind": event.ActorKind,
			"actor_name": event.ActorName, "payload": payload, "created_at": event.CreatedAt,
		})
	}
	return out
}
