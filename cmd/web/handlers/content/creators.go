package content

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/analyze"
	"thirdcoast.systems/rewind/internal/channelid"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleCreatorsPage serves GET /creators, listing creators and pending grouping suggestions.
func HandleCreatorsPage(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, username, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		list, err := q.ListCreators(ctx)
		if err != nil {
			slog.Error("failed to list creators", "error", err)
			list = nil
		}
		suggestions, err := q.ListPendingCreatorSuggestions(ctx)
		if err != nil {
			slog.Error("failed to list creator suggestions", "error", err)
			suggestions = nil
		}
		return templates.Creators(list, suggestions, username).Render(ctx, c.Response())
	}
}

// HandleCreatorCreate serves POST /creators, creating a named creator.
func HandleCreatorCreate(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := common.RequireSessionUser(c, sm); err != nil {
			return c.Redirect(302, "/login")
		}
		name := strings.TrimSpace(c.FormValue("name"))
		if name == "" {
			return c.Redirect(302, "/creators/new")
		}
		notes := strings.TrimSpace(c.FormValue("notes"))
		var notesArg *string
		if notes != "" {
			notesArg = &notes
		}
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		created, err := q.CreateCreator(ctx, &db.CreateCreatorParams{
			Name:  name,
			Notes: notesArg,
		})
		if err != nil {
			slog.Error("failed to create creator", "error", err)
			return c.Redirect(302, "/creators")
		}
		if ids := parseChannelIDList(c.FormValue("channel_ids")); len(ids) > 0 {
			if err := q.LinkChannelsToCreator(ctx, &db.LinkChannelsToCreatorParams{
				CreatorID: created.ID,
				Ids:       ids,
			}); err != nil {
				slog.Error("failed to link wizard channels", "error", err, "creator_id", created.ID)
			}
		}
		return c.Redirect(302, "/creators/"+created.ID.String())
	}
}

// HandleCreatorViewPage serves GET /creators/:id, showing member channels and autopsy signals.
func HandleCreatorViewPage(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, username, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}
		id, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return c.Redirect(302, "/creators")
		}
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		creator, err := q.GetCreator(ctx, id)
		if err != nil {
			return c.Redirect(302, "/creators")
		}
		channels, err := q.ListChannelsByCreator(ctx, id)
		if err != nil {
			slog.Error("failed to list creator channels", "error", err, "creator_id", id)
			channels = nil
		}
		var edges []*db.ListChannelEdgesForCreatorRow
		if elist, err := q.ListChannelEdgesForCreator(ctx, id); err != nil {
			slog.Error("failed to list creator network edges", "error", err, "creator_id", id)
		} else {
			edges = elist
		}
		report := buildCreatorReport(ctx, q, channels)
		return templates.CreatorView(creator, channels, edges, report, username).Render(ctx, c.Response())
	}
}

// HandleCreatorWizardPage serves GET /creators/new, the name + channel picker flow.
func HandleCreatorWizardPage(sm *auth.SessionManager) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, username, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}
		return templates.CreatorWizard(username).Render(c.Request().Context(), c.Response())
	}
}

// HandleCreatorLinkChannel serves POST /creators/:id/link, assigning a channel to the creator.
func HandleCreatorLinkChannel(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := common.RequireSessionUser(c, sm); err != nil {
			return c.Redirect(302, "/login")
		}
		creatorID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		ids := parseChannelIDList(c.FormValue("channel_ids"))
		if len(ids) == 0 {
			var one pgtype.UUID
			if err := one.Scan(strings.TrimSpace(c.FormValue("channel_id"))); err == nil && one.Valid {
				ids = []pgtype.UUID{one}
			}
		}
		if len(ids) == 0 {
			return echo.NewHTTPError(400, "select at least one channel")
		}
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		if _, err := q.GetCreator(ctx, creatorID); err != nil {
			return c.Redirect(302, "/creators")
		}
		if err := q.LinkChannelsToCreator(ctx, &db.LinkChannelsToCreatorParams{
			CreatorID: creatorID,
			Ids:       ids,
		}); err != nil {
			slog.Error("failed to link channels to creator", "error", err, "creator_id", creatorID)
		}
		return c.Redirect(302, "/creators/"+creatorID.String())
	}
}

// HandleCreatorUnlinkChannel serves POST /creators/:id/unlink.
func HandleCreatorUnlinkChannel(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := common.RequireSessionUser(c, sm); err != nil {
			return c.Redirect(302, "/login")
		}
		creatorID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return c.Redirect(302, "/creators")
		}
		var channelID pgtype.UUID
		if err := channelID.Scan(strings.TrimSpace(c.FormValue("channel_id"))); err != nil || !channelID.Valid {
			return echo.NewHTTPError(400, "invalid channel_id")
		}
		ctx := c.Request().Context()
		if err := dbc.Queries(ctx).UnlinkChannelFromCreator(ctx, &db.UnlinkChannelFromCreatorParams{
			ID:        channelID,
			CreatorID: creatorID,
		}); err != nil {
			slog.Error("failed to unlink channel", "error", err, "channel_id", channelID, "creator_id", creatorID)
		}
		return c.Redirect(302, "/creators/"+creatorID.String())
	}
}

// HandleCreatorSuggestionAccept serves POST /creators/suggestions/:id/accept.
func HandleCreatorSuggestionAccept(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := common.RequireSessionUser(c, sm); err != nil {
			return c.Redirect(302, "/login")
		}
		id, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return c.Redirect(302, "/creators")
		}
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		sug, err := q.GetCreatorSuggestion(ctx, id)
		if err != nil || sug == nil || sug.Status != "pending" {
			return c.Redirect(302, "/creators")
		}
		members, err := q.ListCreatorSuggestionMembers(ctx, sug.ID)
		if err != nil {
			slog.Error("failed to list suggestion members", "error", err, "suggestion_id", sug.ID)
			return c.Redirect(302, "/creators")
		}
		ids := make([]pgtype.UUID, 0, len(members))
		for _, ch := range members {
			if ch != nil && ch.ID.Valid {
				ids = append(ids, ch.ID)
			}
		}
		var creatorID pgtype.UUID
		switch sug.Kind {
		case "add":
			if !sug.CreatorID.Valid {
				slog.Error("add suggestion missing creator", "suggestion_id", sug.ID)
				return c.Redirect(302, "/creators")
			}
			if _, err := q.GetCreator(ctx, sug.CreatorID); err != nil {
				slog.Error("add suggestion creator missing", "error", err, "creator_id", sug.CreatorID)
				return c.Redirect(302, "/creators")
			}
			creatorID = sug.CreatorID
		case "new":
			created, err := createCreatorFromSuggestion(ctx, q, sug)
			if err != nil {
				slog.Error("failed to create creator from suggestion", "error", err, "suggestion_id", sug.ID)
				return c.Redirect(302, "/creators")
			}
			creatorID = created.ID
		default:
			slog.Error("unknown creator suggestion kind", "kind", sug.Kind, "suggestion_id", sug.ID)
			return c.Redirect(302, "/creators")
		}
		if len(ids) > 0 {
			if err := q.LinkChannelsToCreator(ctx, &db.LinkChannelsToCreatorParams{
				CreatorID: creatorID,
				Ids:       ids,
			}); err != nil {
				slog.Error("failed to link suggestion channels", "error", err, "suggestion_id", sug.ID, "creator_id", creatorID)
				return c.Redirect(302, "/creators")
			}
		}
		if err := q.SetCreatorSuggestionStatus(ctx, &db.SetCreatorSuggestionStatusParams{
			Status: "accepted",
			ID:     sug.ID,
		}); err != nil {
			slog.Error("failed to accept creator suggestion", "error", err, "suggestion_id", sug.ID)
		}
		return c.Redirect(302, "/creators/"+creatorID.String())
	}
}

// HandleCreatorSuggestionDismiss serves POST /creators/suggestions/:id/dismiss.
func HandleCreatorSuggestionDismiss(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := common.RequireSessionUser(c, sm); err != nil {
			return c.Redirect(302, "/login")
		}
		id, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return c.Redirect(302, "/creators")
		}
		ctx := c.Request().Context()
		if err := dbc.Queries(ctx).SetCreatorSuggestionStatus(ctx, &db.SetCreatorSuggestionStatusParams{
			Status: "dismissed",
			ID:     id,
		}); err != nil {
			slog.Error("failed to dismiss creator suggestion", "error", err, "suggestion_id", id)
		}
		return c.Redirect(302, "/creators")
	}
}

func createCreatorFromSuggestion(ctx context.Context, q *db.Queries, sug *db.CreatorSuggestion) (*db.Creator, error) {
	name := strings.TrimSpace(sug.ProposedName)
	if name == "" {
		name = "Untitled creator"
	}
	if existing, err := q.GetCreatorByNameCI(ctx, name); err == nil && existing != nil {
		return existing, nil
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	notes := strings.TrimSpace(sug.Reason)
	var notesArg *string
	if notes != "" {
		notesArg = &notes
	}
	return q.CreateCreator(ctx, &db.CreateCreatorParams{
		Name:  name,
		Notes: notesArg,
	})
}

func parseChannelIDList(raw string) []pgtype.UUID {
	var out []pgtype.UUID
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var id pgtype.UUID
		if err := id.Scan(part); err == nil && id.Valid {
			out = append(out, id)
		}
	}
	return out
}

func buildCreatorReport(ctx context.Context, q *db.Queries, channels []*db.Channel) analyze.Report {
	ids := make([]pgtype.UUID, 0, len(channels))
	for _, ch := range channels {
		if ch != nil && ch.ID.Valid {
			ids = append(ids, ch.ID)
		}
	}
	if len(ids) == 0 {
		return analyze.Analyze(nil, time.Now())
	}
	rows, err := q.ListVideosForAnalyze(ctx, ids)
	if err != nil {
		slog.Error("failed to list videos for analyze", "error", err)
		return analyze.Analyze(nil, time.Now())
	}
	videos := make([]analyze.Video, 0, len(rows))
	for _, row := range rows {
		v, ok := analyzeVideoFromRow(row)
		if ok {
			videos = append(videos, v)
		}
	}
	return analyze.Analyze(videos, time.Now())
}

func analyzeVideoFromRow(row *db.ListVideosForAnalyzeRow) (analyze.Video, bool) {
	if row == nil || !row.UploadDate.Valid {
		return analyze.Video{}, false
	}
	format := row.Format
	if format == "" {
		format = "video"
	}
	v := analyze.Video{
		ID:         row.ID.String(),
		Platform:   channelid.PlatformFromSrc(row.Src),
		Format:     format,
		Title:      row.Title,
		UploadDate: row.UploadDate.Time,
	}
	if row.DurationSeconds != nil {
		v.DurationSeconds = int(*row.DurationSeconds)
	}
	if row.ViewCount != nil {
		v.ViewCount = *row.ViewCount
	}
	if row.LikeCount != nil {
		v.LikeCount = *row.LikeCount
	}
	v.CommentCount = row.CommentCount
	return v, true
}
