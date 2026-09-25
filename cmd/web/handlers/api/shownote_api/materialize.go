package shownote_api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"thirdcoast.systems/rewind/internal/events"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"github.com/reearth/ygo/crdt"
	ygowebsocket "github.com/reearth/ygo/provider/websocket"

	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/archival"
	"thirdcoast.systems/rewind/internal/db"
	workspace "thirdcoast.systems/rewind/internal/shownote"
	"thirdcoast.systems/rewind/pkg/plugin"
)

// HandleMaterializeReference explicitly resolves, archives, or materializes one reference.
func HandleMaterializeReference(sm *auth.SessionManager, dbc *db.DatabaseConnection, collab *ygowebsocket.Server) echo.HandlerFunc {
	return func(c echo.Context) error {
		noteID, userID, err := requireEditor(c, sm, dbc)
		if err != nil {
			return err
		}
		referenceID, err := common.RequireUUIDParam(c, "referenceId")
		if err != nil {
			return err
		}
		var request struct {
			Action  string      `json:"action"`
			VideoID pgtype.UUID `json:"video_id"`
		}
		if err := json.NewDecoder(c.Request().Body).Decode(&request); err != nil {
			return echo.NewHTTPError(400, "invalid body")
		}
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		ref, err := q.GetShowNoteReference(ctx, &db.GetShowNoteReferenceParams{ID: referenceID, ShowNoteID: noteID})
		if err != nil {
			return echo.NewHTTPError(404, "reference not found")
		}
		if externalArchiveBlocked() && ref.Kind == "external" {
			return echo.NewHTTPError(409, "external references cannot be materialized in Live; use an owned recording")
		}

		videoID := request.VideoID
		switch request.Action {
		case "refresh_time":
			var bounds workspace.Bounds
			if ref.ClipID.Valid {
				clip, err := q.GetClip(ctx, ref.ClipID)
				if err != nil {
					return echo.NewHTTPError(409, "saved clip is unavailable").SetInternal(err)
				}
				bounds = workspace.Bounds{Start: clip.StartTs, End: clip.EndTs, HasTime: true, IsRange: true}
			} else if ref.MarkerID.Valid {
				marker, err := q.GetMarker(ctx, ref.MarkerID)
				if err != nil {
					return echo.NewHTTPError(409, "saved marker is unavailable").SetInternal(err)
				}
				bounds = workspace.Bounds{Start: marker.Timestamp, HasTime: true}
			} else {
				return echo.NewHTTPError(409, "reference has no saved clip or marker")
			}
			if err := rewriteReferenceBounds(ctx, collab, noteID, ref, bounds); err != nil {
				return err
			}
			if err := emitReferenceEvent(ctx, q, noteID, userID, "reference_time_refreshed", map[string]any{"reference_id": ref.ID.String()}); err != nil {
				return err
			}
			return c.JSON(200, map[string]any{"bounds": bounds})
		case "archive":
			if ref.Kind != "external" {
				return echo.NewHTTPError(409, "only external references can be archived")
			}
			if externalArchiveBlocked() {
				return echo.NewHTTPError(409, "external source archival is unavailable in Live; use an owned recording")
			}
			enqueued, err := archival.EnqueueURL(ctx, q, ref.SourceUri, userID)
			if err != nil {
				return err
			}
			job := enqueued.Job
			updated, err := q.MarkShowNoteReferenceResolving(ctx, &db.MarkShowNoteReferenceResolvingParams{
				DownloadJobID: job.ID, ID: ref.ID, ShowNoteID: noteID,
			})
			if err != nil {
				return err
			}
			if err := emitReferenceEvent(ctx, q, noteID, userID, "reference_resolving", updated); err != nil {
				return err
			}
			return c.JSON(202, map[string]any{"reference": updated, "download_job": job})
		case "settle_archive":
			job, err := q.GetShowNoteReferenceDownloadJob(ctx, &db.GetShowNoteReferenceDownloadJobParams{ID: ref.ID, ShowNoteID: noteID})
			if err != nil {
				return echo.NewHTTPError(409, "reference has no archive job")
			}
			if !job.VideoID.Valid {
				return c.JSON(202, map[string]any{"reference": ref, "download_job": job})
			}
			videoID = job.VideoID
		case "use_match", "create_marker", "create_clip":
			if !videoID.Valid && ref.VideoID.Valid {
				videoID = ref.VideoID
			}
			if !videoID.Valid && ref.Kind == "external" {
				video, findErr := q.FindVideoForShowNoteSource(ctx, ref.SourceUri)
				if findErr == nil {
					videoID = video.ID
				} else if findErr != pgx.ErrNoRows {
					return findErr
				}
			}
			if !videoID.Valid && ref.ClipID.Valid {
				clip, clipErr := q.GetClip(ctx, ref.ClipID)
				if clipErr != nil {
					return clipErr
				}
				videoID = clip.VideoID
			}
			if !videoID.Valid && ref.MarkerID.Valid {
				marker, markerErr := q.GetMarker(ctx, ref.MarkerID)
				if markerErr != nil {
					return markerErr
				}
				videoID = marker.VideoID
			}
		default:
			return echo.NewHTTPError(400, "unknown materialization action")
		}
		if !videoID.Valid {
			return echo.NewHTTPError(409, "no matching archived video; archive the source first")
		}
		result, err := materializeResolvedReference(ctx, q, collab, noteID, userID, ref, videoID, request.Action)
		if err != nil {
			return err
		}
		return c.JSON(200, result)
	}
}

func rewriteReferenceBounds(
	ctx context.Context,
	collab *ygowebsocket.Server,
	noteID pgtype.UUID,
	ref *db.ShowNoteReference,
	bounds workspace.Bounds,
) error {
	var rewriteErr error
	if err := collab.Apply(ctx, noteID.String(), func(doc *crdt.Doc, transact func(func(*crdt.Transaction))) {
		text := doc.GetText("markdown")
		updatedMarkdown, err := workspace.RewriteReferenceBounds(text.ToString(), int(ref.LineStart), ref.SourceUri, bounds)
		if err != nil {
			rewriteErr = err
			return
		}
		transact(func(txn *crdt.Transaction) {
			text.Delete(txn, 0, text.Len())
			text.Insert(txn, 0, updatedMarkdown, nil)
		})
	}); rewriteErr == nil && err != nil {
		return err
	}
	if rewriteErr != nil {
		return echo.NewHTTPError(409, rewriteErr.Error())
	}
	return nil
}

func materializeResolvedReference(
	ctx context.Context,
	q *db.Queries,
	collab *ygowebsocket.Server,
	noteID, userID pgtype.UUID,
	ref *db.ShowNoteReference,
	videoID pgtype.UUID,
	action string,
	idempotency ...string,
) (map[string]any, error) {
	kind := "video"
	objectID := videoID
	sourceRef := fmt.Sprintf("show-note:%s:%s", noteID.String(), ref.OccurrenceKey)
	if len(idempotency) > 0 {
		sourceRef = idempotency[0] + ":" + ref.OccurrenceKey
	}
	materializePoint := action == "create_marker" || (action != "create_clip" && ref.StartSeconds != nil && ref.EndSeconds == nil)
	materializeRange := action == "create_clip" || (ref.StartSeconds != nil && ref.EndSeconds != nil)
	if materializeRange {
		if ref.StartSeconds == nil || ref.EndSeconds == nil || *ref.EndSeconds <= *ref.StartSeconds {
			return nil, echo.NewHTTPError(409, "a clip requires a valid timestamp range")
		}
		clip, err := q.CreateShowNoteClip(ctx, &db.CreateShowNoteClipParams{
			VideoID: videoID, StartTs: *ref.StartSeconds, EndTs: *ref.EndSeconds,
			CreatedBy: userID, Title: ref.Label, Description: ref.Context, SourceRef: sourceRef,
		})
		if err != nil {
			return nil, err
		}
		kind, objectID = "clip", clip.ID
	} else if materializePoint {
		if ref.StartSeconds == nil {
			return nil, echo.NewHTTPError(409, "a marker requires a timestamp")
		}
		marker, err := q.CreateShowNoteMarker(ctx, &db.CreateShowNoteMarkerParams{
			VideoID: videoID, Timestamp: *ref.StartSeconds, Title: ref.Label,
			Description: ref.Context, CreatedBy: userID, SourceRef: sourceRef,
		})
		if err != nil {
			return nil, err
		}
		kind, objectID = "marker", marker.ID
	}
	newURI := fmt.Sprintf("rewind://%s/%s", kind, objectID.String())
	var rewriteErr error
	var currentMarkdown string
	if err := collab.Apply(ctx, noteID.String(), func(doc *crdt.Doc, transact func(func(*crdt.Transaction))) {
		text := doc.GetText("markdown")
		currentMarkdown = text.ToString()
		updatedMarkdown, err := workspace.RewriteReferenceURI(currentMarkdown, int(ref.LineStart), ref.SourceUri, newURI)
		if err != nil {
			rewriteErr = err
			return
		}
		transact(func(txn *crdt.Transaction) {
			text.Delete(txn, 0, text.Len())
			text.Insert(txn, 0, updatedMarkdown, nil)
		})
	}); rewriteErr == nil && err != nil {
		return nil, err
	}
	if rewriteErr != nil {
		return nil, echo.NewHTTPError(409, rewriteErr.Error()).SetInternal(fmt.Errorf("current markdown: %s", currentMarkdown))
	}
	if err := emitReferenceEvent(ctx, q, noteID, userID, "reference_ready", map[string]any{"reference_id": ref.ID.String(), "uri": newURI, "kind": kind}); err != nil {
		return nil, err
	}
	return map[string]any{"uri": newURI, "kind": kind, "id": objectID.String()}, nil
}

// confirmAcceptedAgentReferences turns accepting an agent suggestion into the
// explicit confirmation boundary for external sources introduced by its patch.
// Known sources are materialized immediately; unknown ones enter the existing
// archival pipeline and are completed by the pushed download-status event.
func confirmAcceptedAgentReferences(
	ctx context.Context,
	q *db.Queries,
	collab *ygowebsocket.Server,
	noteID, userID pgtype.UUID,
	baseMarkdown, proposedMarkdown, currentMarkdown string,
	idempotency string,
) []string {
	added := workspace.AddedExternalReferences(baseMarkdown, proposedMarkdown)
	if len(added) == 0 {
		return nil
	}

	current := workspace.ParseMarkdown(currentMarkdown).References
	used := make(map[string]bool)
	occurrences := make([]string, 0, len(added))
	for _, candidate := range added {
		for _, ref := range current {
			if used[ref.OccurrenceKey] || ref.Kind != workspace.ReferenceExternal || ref.URI != candidate.URI {
				continue
			}
			if candidate.Label != "" && ref.Label != candidate.Label {
				continue
			}
			used[ref.OccurrenceKey] = true
			occurrences = append(occurrences, ref.OccurrenceKey)
			break
		}
	}

	projection := make(map[string]*db.ShowNoteReference, len(occurrences))
	for _, occurrence := range occurrences {
		ref, err := q.GetShowNoteReferenceByOccurrence(ctx, &db.GetShowNoteReferenceByOccurrenceParams{ShowNoteID: noteID, OccurrenceKey: occurrence})
		if err != nil {
			return []string{err.Error()}
		}
		projection[occurrence] = ref
	}

	errors := make([]string, 0)
	for _, occurrence := range occurrences {
		ref := projection[occurrence]
		if externalArchiveBlocked() {
			errors = append(errors, "external source archival is unavailable in Live")
			continue
		}
		video, err := q.FindVideoForShowNoteSource(ctx, ref.SourceUri)
		if err == nil {
			if _, err := materializeResolvedReference(ctx, q, collab, noteID, userID, ref, video.ID, "use_match", idempotency); err != nil {
				errors = append(errors, err.Error())
			}
			continue
		}
		if err != pgx.ErrNoRows {
			errors = append(errors, err.Error())
			continue
		}
		if ref.DownloadJobID.Valid {
			job, err := q.GetDownloadJobByID(ctx, ref.DownloadJobID)
			if err != nil {
				errors = append(errors, err.Error())
				continue
			}
			if job.Status == db.JobStatusFailed {
				errors = append(errors, "download failed; retry from reference controls")
				continue
			}
			errors = append(errors, "waiting_media")
			continue
		}
		enqueued, err := archival.EnqueueURL(ctx, q, ref.SourceUri, userID)
		if err != nil {
			errors = append(errors, err.Error())
			continue
		}
		updated, err := q.MarkShowNoteReferenceResolving(ctx, &db.MarkShowNoteReferenceResolvingParams{
			DownloadJobID: enqueued.Job.ID, ID: ref.ID, ShowNoteID: noteID,
		})
		if err != nil {
			errors = append(errors, err.Error())
			continue
		}
		errors = append(errors, "waiting_media")
		if err := emitReferenceEvent(ctx, q, noteID, userID, "reference_resolving", updated); err != nil {
			errors = append(errors, err.Error())
		}
	}
	return errors
}

// externalArchiveBlocked prevents external URL references from entering the
// downloader path when the Live product plugin is installed. Existing owned
// recordings continue through the local materialization path.
func externalArchiveBlocked() bool { return plugin.LiveIngest() != nil }

func emitReferenceEvent(ctx context.Context, q *db.Queries, noteID, userID pgtype.UUID, eventType string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = q.CreateShowNoteRoomEvent(ctx, &db.CreateShowNoteRoomEventParams{
		ShowNoteID: noteID, EventType: eventType, ActorKind: "human", ActorUserID: userID, Payload: payload,
	})
	return err
}

// RunMaterializations resumes accepted references without requiring an open browser.
func RunMaterializations(ctx context.Context, dbc *db.DatabaseConnection, collab *ygowebsocket.Server) {
	wake, unsubscribe := events.Default.Subscribe("show_note_room_events", "show_note_documents")
	defer unsubscribe()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		for i := 0; i < 100; i++ {
			q := dbc.Queries(ctx)
			job, err := q.ClaimNoteMaterialization(ctx)
			if errors.Is(err, pgx.ErrNoRows) {
				break
			}
			if err != nil {
				slog.Error("claim note materialization", "error", err)
				break
			}
			status, lastError := "pending", ""
			// Replaying the durable CRDT update also repairs a failed post-commit broadcast.
			update, err := workspace.NewPostgresPersistence(dbc).LoadDoc(job.ShowNoteID.String())
			if err == nil {
				err = collab.BroadcastUpdate(ctx, job.ShowNoteID.String(), update)
			}
			if err != nil {
				lastError = err.Error()
			} else {
				doc, err := q.GetShowNoteDocument(ctx, job.ShowNoteID)
				if err != nil {
					lastError = err.Error()
				} else {
					problems := confirmAcceptedAgentReferences(ctx, q, collab, job.ShowNoteID, job.UserID, job.BaseMarkdown, job.ProposedMarkdown, doc.Markdown, fmt.Sprintf("accepted:%s:%d", job.ThreadID.String(), job.AcceptedRevision))
					if len(problems) == 0 {
						persisted, readErr := q.GetShowNoteDocument(ctx, job.ShowNoteID)
						if readErr != nil {
							lastError = readErr.Error()
						} else if len(occurrencesStillPresent(job.BaseMarkdown, job.ProposedMarkdown, persisted.Markdown)) > 0 {
							lastError = "waiting for durable reference projection"
						} else {
							status = "complete"
						}
					} else {
						lastError = strings.Join(problems, "; ")
					}
				}
			}
			if err = q.FinishNoteMaterialization(ctx, &db.FinishNoteMaterializationParams{ThreadID: job.ThreadID, LeaseToken: job.LeaseToken, Status: status, LastError: lastError}); err != nil {
				slog.Error("finish note materialization", "error", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-wake:
		case <-ticker.C:
		}
	}
}

func occurrencesStillPresent(base, proposed, current string) []string {
	var found []string
	for _, added := range workspace.AddedExternalReferences(base, proposed) {
		for _, ref := range workspace.ParseMarkdown(current).References {
			if ref.Kind == workspace.ReferenceExternal && ref.URI == added.URI {
				found = append(found, ref.OccurrenceKey)
			}
		}
	}
	return found
}
