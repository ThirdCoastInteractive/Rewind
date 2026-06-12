// package tag_api provides video tag API handlers.
package tag_api

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates/components"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleTagsRender renders a video's tag editor (initial load).
func HandleTagsRender(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return echo.NewHTTPError(401, "unauthorized")
		}
		videoUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		ctx := c.Request().Context()
		data := loadTagEditorData(ctx, dbc.Queries(ctx), c.Param("id"), videoUUID)

		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		_ = sse.PatchElementTempl(components.VideoTagEditor(data),
			datastar.WithSelector("[data-video-tags]"), datastar.WithModeInner())
		return nil
	}
}

// HandleAddTag adds a tag (by name) to a video, then re-renders the chips.
func HandleAddTag(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}
		videoUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		var sig struct {
			NewTag string `json:"_newTag"`
		}
		_ = datastar.ReadSignals(c.Request(), &sig)

		name := strings.TrimSpace(sig.NewTag)
		if slug := tagSlug(name); slug != "" {
			if tag, err := q.UpsertTag(ctx, &db.UpsertTagParams{Name: name, Slug: slug, CreatedBy: userUUID}); err == nil {
				_ = q.AddVideoTag(ctx, &db.AddVideoTagParams{VideoID: videoUUID, TagID: tag.ID, CreatedBy: userUUID})
			}
		}

		data := loadTagEditorData(ctx, q, c.Param("id"), videoUUID)
		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		_ = sse.PatchElementTempl(components.TagChips(data),
			datastar.WithSelectorID("video-tags-chips"), datastar.WithModeInner())
		// Clear the input for the next tag.
		_ = sse.PatchSignals([]byte(`{"_newTag":""}`))
		return nil
	}
}

// HandleRemoveTag unlinks a tag from a video, then re-renders the chips.
func HandleRemoveTag(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return echo.NewHTTPError(401, "unauthorized")
		}
		videoUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		tagUUID, err := common.RequireUUIDParam(c, "tagId")
		if err != nil {
			return err
		}
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		_ = q.RemoveVideoTag(ctx, &db.RemoveVideoTagParams{VideoID: videoUUID, TagID: tagUUID})

		data := loadTagEditorData(ctx, q, c.Param("id"), videoUUID)
		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		_ = sse.PatchElementTempl(components.TagChips(data),
			datastar.WithSelectorID("video-tags-chips"), datastar.WithModeInner())
		return nil
	}
}

// HandleListTags renders the library tag filter bar (all tags with counts).
func HandleListTags(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return echo.NewHTTPError(401, "unauthorized")
		}
		ctx := c.Request().Context()
		rows, _ := dbc.Queries(ctx).ListAllTagsWithCounts(ctx)
		items := make([]components.TagFilterItem, 0, len(rows))
		for _, r := range rows {
			items = append(items, components.TagFilterItem{ID: r.ID.String(), Name: r.Name, Count: r.VideoCount})
		}
		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		_ = sse.PatchElementTempl(components.TagFilterBar(items),
			datastar.WithSelector("[data-tag-filter-bar]"), datastar.WithModeInner())
		return nil
	}
}

// HandleBulkTag applies a tag (by name) to all selected videos at once, then
// refreshes the tag filter bar and clears the selection.
func HandleBulkTag(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		var sig struct {
			SelectedVideoIDs []string `json:"selectedVideoIds"`
			BulkTag          string   `json:"_bulkTag"`
		}
		_ = datastar.ReadSignals(c.Request(), &sig)

		name := strings.TrimSpace(sig.BulkTag)
		ids := parseUUIDs(sig.SelectedVideoIDs)
		sse := datastar.NewSSE(c.Response().Writer, c.Request())

		if slug := tagSlug(name); slug != "" && len(ids) > 0 {
			if tag, err := q.UpsertTag(ctx, &db.UpsertTagParams{Name: name, Slug: slug, CreatedBy: userUUID}); err == nil {
				_ = q.AddVideoTagToMany(ctx, &db.AddVideoTagToManyParams{TagID: tag.ID, VideoIds: ids, CreatedBy: userUUID})
			}
			// Tag counts changed — refresh the filter bar.
			rows, _ := q.ListAllTagsWithCounts(ctx)
			items := make([]components.TagFilterItem, 0, len(rows))
			for _, r := range rows {
				items = append(items, components.TagFilterItem{ID: r.ID.String(), Name: r.Name, Count: r.VideoCount})
			}
			_ = sse.PatchElementTempl(components.TagFilterBar(items),
				datastar.WithSelector("[data-tag-filter-bar]"), datastar.WithModeInner())
		}
		// Clear selection + input regardless.
		_ = sse.PatchSignals([]byte(`{"selectedVideoIds":[],"_bulkTag":""}`))
		return nil
	}
}

// parseUUIDs parses id strings into a UUID slice, skipping invalid entries.
func parseUUIDs(ids []string) []pgtype.UUID {
	out := make([]pgtype.UUID, 0, len(ids))
	for _, s := range ids {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if g, err := uuid.Parse(s); err == nil {
			out = append(out, pgtype.UUID{Bytes: [16]byte(g), Valid: true})
		}
	}
	return out
}

// loadTagEditorData fetches a video's tags into the templ view model.
func loadTagEditorData(ctx context.Context, q *db.Queries, videoID string, videoUUID pgtype.UUID) components.TagEditorData {
	tags := []components.TagItem{}
	if rows, err := q.ListTagsForVideo(ctx, videoUUID); err == nil {
		for _, r := range rows {
			color := ""
			if r.Color != nil {
				color = *r.Color
			}
			tags = append(tags, components.TagItem{ID: r.ID.String(), Name: r.Name, Color: color})
		}
	}
	return components.TagEditorData{VideoID: videoID, Tags: tags}
}

// tagSlug normalizes a tag name into a case-insensitive slug (lowercased,
// internal whitespace collapsed to single spaces).
func tagSlug(name string) string {
	return strings.Join(strings.Fields(strings.ToLower(name)), " ")
}
