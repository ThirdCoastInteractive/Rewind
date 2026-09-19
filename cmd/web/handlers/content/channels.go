package content

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/catalog"
	"thirdcoast.systems/rewind/internal/channelid"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/osint"
	"thirdcoast.systems/rewind/internal/wiki"
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

// HandleIndexChannelCatalog starts or refreshes every catalog feed for a channel.
func HandleIndexChannelCatalog(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}
		name := strings.TrimSpace(c.FormValue("uploader"))
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		overview, err := q.GetChannelOverview(ctx, name)
		if err != nil {
			return c.Redirect(302, "/channels")
		}
		ch := lookupChannelByURL(ctx, q, overview)
		if ch == nil {
			return echo.NewHTTPError(404, "channel identity not found")
		}
		if catalog.SkipPlatform(ch.Platform) {
			return c.Redirect(302, "/channels/view?name="+url.QueryEscape(name))
		}
		refresh := c.FormValue("refresh") == "true"
		if _, err := catalog.IndexChannel(ctx, q, ch, userID, refresh); err != nil {
			slog.Error("failed to create channel catalog crawls", "error", err, "channel_id", ch.ID)
			return echo.NewHTTPError(500, "could not start catalog")
		}
		return c.Redirect(302, "/channels/view?name="+url.QueryEscape(name))
	}
}

// HandleCatalogCrawlControl pauses, resumes, cancels, or retries a crawl.
func HandleCatalogCrawlControl(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := common.RequireSessionUser(c, sm); err != nil {
			return c.Redirect(302, "/login")
		}
		id, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		status := map[string]string{"pause": "paused", "resume": "queued", "cancel": "cancelled", "retry": "queued"}[c.Param("action")]
		if status == "" {
			return echo.NewHTTPError(400, "unknown catalog action")
		}
		if err := dbc.Queries(c.Request().Context()).SetCatalogCrawlStatus(c.Request().Context(), &db.SetCatalogCrawlStatusParams{ID: id, Status: status, LastError: ""}); err != nil {
			return err
		}
		return c.Redirect(302, c.Request().Referer())
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
		var crawls []*db.CatalogCrawl
		var vaultPages []wiki.Page
		var channelID pgtype.UUID
		homeSlug := wiki.SlugFromName(overview.Uploader)
		skipCatalog := catalog.SkipPlatform(channelid.PlatformFromSrc(overview.ChannelURL)) ||
			catalog.SkipPlatform(channelid.PlatformFromSrc(overview.UploaderURL))
		if ch := lookupChannelByURL(ctx, q, overview); ch != nil {
			channelID = ch.ID
			if catalog.SkipPlatform(ch.Platform) {
				skipCatalog = true
			}
			edges, err = q.ListChannelEdgesForChannel(ctx, ch.ID)
			if err != nil {
				slog.Error("failed to list channel edges", "error", err, "uploader", name)
				edges = nil
			}
			crawls, err = q.ListCatalogCrawlsForChannel(ctx, ch.ID)
			if err != nil {
				slog.Error("failed to list channel catalog crawls", "error", err, "channel_id", ch.ID)
				crawls = nil
			}
			vaultPages, _ = wikiStore(dbc).PagesForChannel(ctx, ch.ID)
			vaultPages = decorateWikiPages(vaultPages, wikiClipLookup(ctx, dbc))
		}
		eng, engErr := osint.LoadChannelEngagement(ctx, dbc.Pool, name, channelID)
		if engErr != nil {
			slog.Error("failed to load channel comment engagement", "error", engErr, "uploader", name)
		}

		return templates.ChannelView(overview, videos, username, edges, crawls, skipCatalog, vaultPages, homeSlug, eng).Render(ctx, c.Response())
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
