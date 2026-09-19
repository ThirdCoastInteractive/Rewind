package shownote

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/reearth/ygo/crdt"
	"thirdcoast.systems/rewind/internal/db"
)

// AcceptSuggestion commits the accepted Yjs update, review state, and materialization outbox together.
// Returned update bytes can be replayed safely to connected editors after commit or restart.
func AcceptSuggestion(ctx context.Context, dbc *db.DatabaseConnection, noteID, threadID, userID pgtype.UUID) (*db.ShowNoteReviewThread, []byte, error) {
	q, tx, err := dbc.NewWithTX(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx)
	document, err := q.LockShowNoteDocument(ctx, noteID)
	if err != nil {
		return nil, nil, err
	}
	thread, err := q.LockReviewThread(ctx, threadID)
	if err != nil {
		return nil, nil, err
	}
	if thread.ShowNoteID != noteID || thread.Kind != "suggestion" {
		return nil, nil, fmt.Errorf("suggestion not found")
	}
	updates, err := q.ListShowNoteDocumentUpdatesAfter(ctx, &db.ListShowNoteDocumentUpdatesAfterParams{ShowNoteID: noteID, Revision: document.SnapshotRevision})
	if err != nil {
		return nil, nil, err
	}
	doc, err := materializeDocument(document.Snapshot, updates)
	if err != nil {
		return nil, nil, err
	}
	if thread.Status == "accepted" {
		return thread, crdt.EncodeStateAsUpdateV1(doc, nil), nil
	}
	if thread.Status != "open" {
		return nil, nil, fmt.Errorf("suggestion is not open")
	}
	base := thread.BaseMarkdown
	if base == "" && document.Revision == thread.BaseRevision {
		base = document.Markdown
	}
	proposed, err := ApplyUnifiedDiff(base, thread.Patch)
	if err != nil {
		return nil, nil, err
	}
	change := ChangedTextRange(base, proposed)
	startRel, err := crdt.DecodeRelativePosition(thread.AnchorStart)
	if err != nil {
		return nil, nil, err
	}
	endRel, err := crdt.DecodeRelativePosition(thread.AnchorEnd)
	if err != nil {
		return nil, nil, err
	}
	start, ok1 := crdt.ToAbsolutePosition(doc, startRel)
	end, ok2 := crdt.ToAbsolutePosition(doc, endRel)
	if !ok1 || !ok2 || start.Name != "markdown" || end.Name != "markdown" || end.Index < start.Index {
		return nil, nil, fmt.Errorf("suggestion anchor is detached")
	}
	text := doc.GetText("markdown")
	selected, err := UTF16Slice(text.ToString(), start.Index, end.Index)
	if err != nil || selected != change.Before {
		return nil, nil, fmt.Errorf("suggestion overlaps a document edit")
	}
	doc.Transact(func(txn *crdt.Transaction) {
		if end.Index > start.Index {
			text.Delete(txn, start.Index, end.Index-start.Index)
		}
		if change.After != "" {
			text.Insert(txn, start.Index, change.After, nil)
		}
	})
	update := crdt.EncodeStateAsUpdateV1(doc, nil)
	revision := document.Revision + 1
	markdown := text.ToString()
	if err = q.AppendShowNoteDocumentUpdate(ctx, &db.AppendShowNoteDocumentUpdateParams{ShowNoteID: noteID, Revision: revision, Update: update}); err != nil {
		return nil, nil, err
	}
	if err = q.CommitShowNoteDocumentUpdateProjection(ctx, &db.CommitShowNoteDocumentUpdateProjectionParams{ShowNoteID: noteID, Revision: revision, Markdown: markdown}); err != nil {
		return nil, nil, err
	}
	if err = replaceReferenceProjection(ctx, q, noteID, revision, ParseMarkdown(markdown)); err != nil {
		return nil, nil, err
	}
	thread, err = q.UpdateShowNoteReviewStatus(ctx, &db.UpdateShowNoteReviewStatusParams{ID: threadID, Status: "accepted", ClosedByUserID: userID})
	if err != nil {
		return nil, nil, err
	}
	if thread.ActorKind == "agent" {
		if err = q.QueueNoteMaterialization(ctx, &db.QueueNoteMaterializationParams{ThreadID: threadID, ShowNoteID: noteID, UserID: userID, AcceptedRevision: revision, BaseMarkdown: base, ProposedMarkdown: proposed}); err != nil {
			return nil, nil, err
		}
	}
	payload, _ := json.Marshal(map[string]any{"thread_id": threadID.String(), "status": "accepted", "revision": revision})
	if _, err = q.CreateShowNoteRoomEvent(ctx, &db.CreateShowNoteRoomEventParams{ShowNoteID: noteID, EventType: "review_status", ActorKind: "human", ActorUserID: userID, Payload: payload}); err != nil {
		return nil, nil, err
	}
	if err = q.TouchShowNote(ctx, noteID); err != nil {
		return nil, nil, err
	}
	return thread, update, tx.Commit(ctx)
}
