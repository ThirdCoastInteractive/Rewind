package content

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/channelid"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleChannelsPage serves GET /channels, listing every uploader in the
// archive with aggregate stats.
func HandleChannelsPage(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, username, err := sm.GetSession(c.Request())
		if err != nil {
			return c.Redirect(302, "/login")
		}

		// Fast shell; the list loads asynchronously via /api/channels/index.
		return templates.Channels(username).Render(c.Request().Context(), c.Response())
	}
}

// HandleChannelViewPage serves GET /channels/view?name=<uploader>, showing one
// channel's stats and its archived videos.
func HandleChannelViewPage(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, username, err := sm.GetSession(c.Request())
		if err != nil {
			return c.Redirect(302, "/login")
		}

		name := strings.TrimSpace(c.QueryParam("name"))
		if name == "" {
			return c.Redirect(302, "/channels")
		}

		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		overview, err := q.GetChannelOverview(ctx, name)
		if err != nil {
			// Unknown uploader (or query failure): the index is the safe landing.
			return c.Redirect(302, "/channels")
		}

		sort := "published-newest"
		videos, err := q.ListVideosPaginated(ctx, &db.ListVideosPaginatedParams{
			Uploader:  &name,
			SortOrder: sort,
			PageLimit: 60,
		})
		if err != nil {
			slog.Error("failed to list channel videos", "error", err, "uploader", name)
			videos = nil
		}

		var edges []*db.ListChannelEdgesForChannelRow
		if ch := lookupChannelByURL(ctx, q, overview); ch != nil {
			edges, err = q.ListChannelEdgesForChannel(ctx, ch.ID)
			if err != nil {
				slog.Error("failed to list channel edges", "error", err, "uploader", name)
				edges = nil
			}
		}

		return templates.ChannelView(overview, videos, username, edges).Render(ctx, c.Response())
	}
}

// HandleIndexChannelMetadata serves POST /channels/view/index-metadata.
// It enqueues a metadata-catalog job for the channel URL so descriptions can
// be indexed without downloading media.
func HandleIndexChannelMetadata(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}

		name := strings.TrimSpace(c.FormValue("uploader"))
		if name == "" {
			return c.Redirect(302, "/channels")
		}

		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		overview, err := q.GetChannelOverview(ctx, name)
		if err != nil {
			return c.Redirect(302, "/channels")
		}

		channelURL := strings.TrimSpace(overview.ChannelURL)
		if channelURL == "" {
			return c.Redirect(302, "/channels/view?name="+url.QueryEscape(name))
		}

		job, err := q.EnqueueMetadataCatalogJob(ctx, &db.EnqueueMetadataCatalogJobParams{
			URL:        channelURL,
			ArchivedBy: userUUID,
		})
		if err != nil {
			slog.Error("failed to enqueue metadata catalog", "error", err, "uploader", name, "url", channelURL)
			return c.Redirect(302, "/jobs")
		}

		slog.Info("enqueued metadata catalog", "job_id", job.ID, "uploader", name, "url", channelURL)
		return c.Redirect(302, "/jobs/"+job.ID.String())
	}
}

func lookupChannelByURL(ctx context.Context, q *db.Queries, overview *db.GetChannelOverviewRow) *db.Channel {
	if overview == nil {
		return nil
	}
	for _, raw := range []string{overview.ChannelURL, overview.UploaderURL} {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		id := channelid.FromURL(raw)
		if id.Key == "" || id.Key == "unknown" {
			continue
		}
		ch, err := q.GetChannelByIdentity(ctx, &db.GetChannelByIdentityParams{
			Platform:    id.Platform,
			IdentityKey: id.Key,
		})
		if err == nil && ch != nil {
			return ch
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("channel identity lookup failed", "error", err, "url", raw)
		}
	}
	return nil
}
