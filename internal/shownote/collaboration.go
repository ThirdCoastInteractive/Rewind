package shownote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/reearth/ygo/crdt"

	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/plugin"
)

// PostgresPersistence stores Yjs updates, snapshots, Markdown projections, and
// semantic reference projections in one transaction per committed update.
type PostgresPersistence struct {
	dbc   *db.DatabaseConnection
	locks sync.Map
}

// NewPostgresPersistence creates a durable collaboration store.
func NewPostgresPersistence(dbc *db.DatabaseConnection) *PostgresPersistence {
	return &PostgresPersistence{dbc: dbc}
}

// LoadDoc returns the materialized V1 Yjs state for a show-note room.
func (p *PostgresPersistence) LoadDoc(room string) ([]byte, error) {
	return p.loadDoc(context.Background(), room)
}

func (p *PostgresPersistence) loadDoc(ctx context.Context, room string) ([]byte, error) {
	noteID, err := parseRoomID(room)
	if err != nil {
		return nil, err
	}
	ctx, err = collaborationTenantContext(ctx, p.dbc, noteID)
	if err != nil {
		return nil, err
	}
	q := p.dbc.Queries(ctx)
	document, err := q.GetShowNoteDocument(ctx, noteID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load show-note snapshot: %w", err)
	}
	updates, err := q.ListShowNoteDocumentUpdatesAfter(ctx, &db.ListShowNoteDocumentUpdatesAfterParams{
		ShowNoteID: noteID,
		Revision:   document.SnapshotRevision,
	})
	if err != nil {
		return nil, fmt.Errorf("load show-note updates: %w", err)
	}
	doc, err := materializeDocument(document.Snapshot, updates)
	if err != nil {
		return nil, err
	}
	state := crdt.EncodeStateAsUpdateV1(doc, nil)
	if len(state) == 0 {
		return nil, nil
	}
	return state, nil
}

// StoreUpdate persists an incremental update before returning to the provider.
func (p *PostgresPersistence) StoreUpdate(room string, update []byte) error {
	return p.StoreUpdateContext(context.Background(), room, update)
}

// StoreUpdateContext is the cancellable persistence implementation used by
// the collaboration server during normal operation and graceful shutdown.
func (p *PostgresPersistence) StoreUpdateContext(ctx context.Context, room string, update []byte) error {
	noteID, err := parseRoomID(room)
	if err != nil {
		return err
	}
	ctx, err = collaborationTenantContext(ctx, p.dbc, noteID)
	if err != nil {
		return err
	}
	lockValue, _ := p.locks.LoadOrStore(room, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	txq, tx, err := p.dbc.NewWithTX(ctx)
	if err != nil {
		return fmt.Errorf("begin show-note update: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	document, err := txq.LockShowNoteDocument(ctx, noteID)
	if err != nil {
		return fmt.Errorf("lock show-note document: %w", err)
	}
	updates, err := txq.ListShowNoteDocumentUpdatesAfter(ctx, &db.ListShowNoteDocumentUpdatesAfterParams{
		ShowNoteID: noteID,
		Revision:   document.SnapshotRevision,
	})
	if err != nil {
		return fmt.Errorf("load pending show-note updates: %w", err)
	}
	doc, err := materializeDocument(document.Snapshot, updates)
	if err != nil {
		return err
	}
	before := crdt.EncodeStateAsUpdateV1(doc, nil)
	if err := crdt.ApplyUpdateV1(doc, update, nil); err != nil {
		return fmt.Errorf("apply incoming show-note update: %w", err)
	}

	if bytes.Equal(before, crdt.EncodeStateAsUpdateV1(doc, nil)) {
		return nil
	}
	revision := document.Revision + 1
	markdown := doc.GetText("markdown").ToString()
	if err := txq.AppendShowNoteDocumentUpdate(ctx, &db.AppendShowNoteDocumentUpdateParams{
		ShowNoteID: noteID, Revision: revision, Update: update,
	}); err != nil {
		return fmt.Errorf("append show-note update: %w", err)
	}
	if err := txq.CommitShowNoteDocumentUpdateProjection(ctx, &db.CommitShowNoteDocumentUpdateProjectionParams{
		ShowNoteID: noteID, Markdown: markdown, Revision: revision,
	}); err != nil {
		return fmt.Errorf("update show-note projection: %w", err)
	}
	if err := replaceReferenceProjection(ctx, txq, noteID, revision, ParseMarkdown(markdown)); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"revision": revision})
	if _, err := txq.CreateShowNoteRoomEvent(ctx, &db.CreateShowNoteRoomEventParams{
		ShowNoteID: noteID, EventType: "document_revision", ActorKind: "system",
		ActorName: "Rewind", Payload: payload,
	}); err != nil {
		return fmt.Errorf("append document event: %w", err)
	}
	if err := txq.TouchShowNote(ctx, noteID); err != nil {
		return fmt.Errorf("touch show note: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit show-note update: %w", err)
	}
	return nil
}

// Compact stores a full snapshot before deleting only the updates covered by
// that snapshot.
func (p *PostgresPersistence) Compact(ctx context.Context, room string) error {
	noteID, err := parseRoomID(room)
	if err != nil {
		return err
	}
	ctx, err = collaborationTenantContext(ctx, p.dbc, noteID)
	if err != nil {
		return err
	}
	lockValue, _ := p.locks.LoadOrStore(room, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	txq, tx, err := p.dbc.NewWithTX(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	document, err := txq.LockShowNoteDocument(ctx, noteID)
	if err != nil {
		return err
	}
	updates, err := txq.ListShowNoteDocumentUpdatesAfter(ctx, &db.ListShowNoteDocumentUpdatesAfterParams{
		ShowNoteID: noteID, Revision: document.SnapshotRevision,
	})
	if err != nil {
		return err
	}
	doc, err := materializeDocument(document.Snapshot, updates)
	if err != nil {
		return err
	}
	if err := txq.StoreShowNoteDocumentSnapshot(ctx, &db.StoreShowNoteDocumentSnapshotParams{
		ShowNoteID: noteID, Snapshot: crdt.EncodeStateAsUpdateV1(doc, nil), SnapshotRevision: document.Revision,
	}); err != nil {
		return err
	}
	if err := txq.DeleteShowNoteDocumentUpdatesThrough(ctx, &db.DeleteShowNoteDocumentUpdatesThroughParams{
		ShowNoteID: noteID, Revision: document.Revision,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// collaborationTenantContext derives Live workspace scope from the note room
// for websocket/background callbacks that cannot carry the browser context.
// It never falls back to OSS rows in a Live process.
func collaborationTenantContext(ctx context.Context, dbc *db.DatabaseConnection, noteID pgtype.UUID) (context.Context, error) {
	if plugin.LiveIngest() == nil {
		return ctx, nil
	}
	if _, scoped := plugin.TenantScope(ctx); !scoped {
		note, err := dbc.Queries(ctx).GetShowNote(ctx, noteID)
		if err != nil {
			return ctx, err
		}
		if !note.TenantID.Valid || note.TenantID == (pgtype.UUID{}) {
			return ctx, ErrTenantRequired
		}
		ctx = plugin.WithTenantScope(ctx, note.TenantID.String(), true)
	}
	if _, err := RequireTenant(ctx, dbc, noteID); err != nil {
		return ctx, err
	}
	return ctx, nil
}

// EncodeInitialDocument creates the initial Y.Text named markdown and returns
// its full V1 state update.
func EncodeInitialDocument(markdown string) []byte {
	doc := crdt.New()
	text := doc.GetText("markdown")
	if markdown != "" {
		doc.Transact(func(txn *crdt.Transaction) {
			text.Insert(txn, 0, markdown, nil)
		})
	}
	return crdt.EncodeStateAsUpdateV1(doc, nil)
}

func materializeDocument(snapshot []byte, updates []*db.ShowNoteDocumentUpdate) (*crdt.Doc, error) {
	doc := crdt.New()
	if len(snapshot) > 0 {
		if err := crdt.ApplyUpdateV1(doc, snapshot, nil); err != nil {
			return nil, fmt.Errorf("apply stored show-note snapshot: %w", err)
		}
	}
	for _, update := range updates {
		if err := crdt.ApplyUpdateV1(doc, update.Update, nil); err != nil {
			return nil, fmt.Errorf("apply stored show-note update %d: %w", update.Revision, err)
		}
	}
	return doc, nil
}

func replaceReferenceProjection(ctx context.Context, q *db.Queries, noteID pgtype.UUID, revision int64, projection Document) error {
	for _, ref := range projection.References {
		status := "unresolved"
		if ref.Diagnostic != "" {
			status = "invalid"
		} else if ref.Kind == ReferenceVideo || ref.Kind == ReferenceClip || ref.Kind == ReferenceMarker {
			status = "ready"
		}
		var videoID, clipID, markerID pgtype.UUID
		parsed, parseErr := uuid.Parse(ref.ObjectID)
		if parseErr == nil {
			value := pgtype.UUID{Bytes: parsed, Valid: true}
			var exists bool
			switch ref.Kind {
			case ReferenceVideo:
				exists, parseErr = q.ShowNoteVideoObjectExists(ctx, value)
				if exists {
					videoID = value
				}
			case ReferenceClip:
				clip, err := q.GetClip(ctx, value)
				parseErr = err
				exists = err == nil
				if errors.Is(err, pgx.ErrNoRows) {
					parseErr = nil
				}
				if clip != nil {
					clipID = value
					if ref.Bounds.IsRange && (math.Abs(ref.Bounds.Start-clip.StartTs) > .001 || math.Abs(ref.Bounds.End-clip.EndTs) > .001) {
						ref.Diagnostic = fmt.Sprintf("displayed range differs from saved clip (%s–%s)", FormatTimestamp(clip.StartTs), FormatTimestamp(clip.EndTs))
					}
				}
			case ReferenceMarker:
				marker, err := q.GetMarker(ctx, value)
				parseErr = err
				exists = err == nil
				if errors.Is(err, pgx.ErrNoRows) {
					parseErr = nil
				}
				if marker != nil {
					markerID = value
					if ref.Bounds.HasTime && math.Abs(ref.Bounds.Start-marker.Timestamp) > .001 {
						ref.Diagnostic = fmt.Sprintf("displayed time differs from saved marker (%s)", FormatTimestamp(marker.Timestamp))
					}
				}
			}
			if parseErr != nil {
				return fmt.Errorf("validate show-note reference %q: %w", ref.URI, parseErr)
			}
			if !exists {
				status = "missing"
				if ref.Diagnostic == "" {
					ref.Diagnostic = "referenced Rewind object does not exist"
				}
			}
		} else if ref.ObjectID != "" {
			status = "missing"
		}
		var start, end *float64
		if ref.Bounds.HasTime {
			start = &ref.Bounds.Start
		}
		if ref.Bounds.IsRange {
			end = &ref.Bounds.End
		}
		if _, err := q.UpsertShowNoteReference(ctx, &db.UpsertShowNoteReferenceParams{
			ShowNoteID: noteID, OccurrenceKey: ref.OccurrenceKey, Ordinal: int32(ref.Ordinal),
			Kind: string(ref.Kind), SourceUri: ref.URI, Label: ref.Label, Context: ref.Context,
			SectionPath: ref.SectionPath, StartSeconds: start, EndSeconds: end, Status: status,
			VideoID: videoID, ClipID: clipID, MarkerID: markerID, LineStart: int32(ref.LineStart),
			LineEnd: int32(ref.LineEnd), ParsedRevision: revision, Diagnostic: ref.Diagnostic,
		}); err != nil {
			return fmt.Errorf("insert show-note reference projection: %w", err)
		}
	}
	if err := q.DeleteStaleShowNoteReferences(ctx, &db.DeleteStaleShowNoteReferencesParams{
		ShowNoteID: noteID, ParsedRevision: revision,
	}); err != nil {
		return fmt.Errorf("delete stale show-note references: %w", err)
	}
	return nil
}

func parseRoomID(room string) (pgtype.UUID, error) {
	noteID, err := uuid.Parse(room)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("invalid show-note room %q: %w", room, err)
	}
	return pgtype.UUID{Bytes: noteID, Valid: true}, nil
}
