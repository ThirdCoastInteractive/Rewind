// Package watch_api provides follow API handlers: creating, toggling,
// scanning, and deleting followed channels. All mutating handlers respond with
// a DataStar SSE patch of the follows list.
package watch_api

import (
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/ctxkeys"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/channelid"
	"thirdcoast.systems/rewind/internal/cronspec"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/videoid"
)

// renderSection patches the watches section (list + inline error) in place.
func renderSection(c echo.Context, q *db.Queries, errMsg string) error {
	ctx := c.Request().Context()
	rows, err := q.ListWatchedChannels(ctx)
	if err != nil {
		slog.Error("failed to list watched channels", "error", err)
		if errMsg == "" {
			errMsg = "Failed to load follows"
		}
	}
	sse := datastar.NewSSE(c.Response().Writer, c.Request())
	return sse.PatchElementTempl(templates.WatchesSection(rows, errMsg), datastar.WithSelectorID("watches-section"))
}

// isSingleYouTubeVideoURL reports whether raw is a YouTube URL that points at
// one video rather than a channel/playlist. Non-YouTube URLs return false:
// the classifier is YouTube-focused, and other sites' channel URLs must not
// be rejected.
func isSingleYouTubeVideoURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	isYT := host == "youtu.be" || host == "youtube.com" || strings.HasSuffix(host, ".youtube.com")
	return isYT && !videoid.IsPlaylistOrChannelURL(raw)
}

// HandleCreate serves POST /api/watches, registering a new watched channel.
func HandleCreate(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if live, _ := c.Request().Context().Value(ctxkeys.LiveProduct).(bool); live {
			return c.NoContent(404)
		}
		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		var sig struct {
			URL        string `json:"watchUrl"`
			Schedule   string `json:"watchSchedule"`
			CustomCron string `json:"watchCustomCron"`
			Label      string `json:"watchLabel"`
			Backfill   bool   `json:"watchBackfill"`
		}
		_ = datastar.ReadSignals(c.Request(), &sig)

		rawURL := strings.TrimSpace(sig.URL)
		if rawURL == "" {
			return renderSection(c, q, "A channel or playlist URL is required")
		}
		if !strings.Contains(rawURL, "://") {
			rawURL = "https://" + rawURL
		}
		if isSingleYouTubeVideoURL(rawURL) {
			return renderSection(c, q, "That looks like a single video URL — archive it from the home page instead, or follow the whole channel")
		}

		schedule := strings.TrimSpace(sig.Schedule)
		if schedule == "custom" {
			schedule = strings.TrimSpace(sig.CustomCron)
		}
		if err := cronspec.Validate(schedule); err != nil {
			return renderSection(c, q, err.Error())
		}

		ident := channelid.FromURL(rawURL)
		canon := ident.CanonicalURL
		if canon == "" {
			canon = rawURL
		}
		label := strings.TrimSpace(sig.Label)
		ch, err := q.UpsertChannel(ctx, &db.UpsertChannelParams{
			Platform:     ident.Platform,
			IdentityKey:  ident.Key,
			ChannelID:    ident.ChannelID,
			Uploader:     label,
			CanonicalURL: canon,
		})
		if err != nil {
			slog.Error("failed to upsert channel for follow", "error", err, "url", rawURL)
			return renderSection(c, q, "Failed to create follow")
		}

		if _, err := q.GetWatchedChannelByUserAndChannel(ctx, &db.GetWatchedChannelByUserAndChannelParams{
			CreatedBy: userUUID,
			ChannelID: ch.ID,
		}); err == nil {
			return renderSection(c, q, "You are already following this channel")
		} else if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("failed to check existing follow", "error", err, "url", rawURL)
			return renderSection(c, q, "Failed to create follow")
		}

		// First scan runs immediately: backfill starts archiving right away,
		// non-backfill seeds the seen-ledger so future uploads count as new.
		_, err = q.CreateWatchedChannel(ctx, &db.CreateWatchedChannelParams{
			CreatedBy:    userUUID,
			URL:          rawURL,
			Label:        label,
			CronSchedule: schedule,
			Backfill:     sig.Backfill,
			NextScanAt:   pgtype.Timestamptz{Time: time.Now(), Valid: true},
			ChannelID:    ch.ID,
		})
		if err != nil {
			if db.IsUniqueViolationErr(err) {
				return renderSection(c, q, "You are already following this channel")
			}
			slog.Error("failed to create watched channel", "error", err, "url", rawURL)
			return renderSection(c, q, "Failed to create follow")
		}

		// Clear the URL/label inputs for the next add; keep schedule+backfill.
		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		_ = sse.PatchSignals([]byte(`{"watchUrl":"","watchLabel":""}`))
		rows, lerr := q.ListWatchedChannels(ctx)
		if lerr != nil {
			slog.Error("failed to list watched channels", "error", lerr)
		}
		return sse.PatchElementTempl(templates.WatchesSection(rows, ""), datastar.WithSelectorID("watches-section"))
	}
}

// HandleToggle serves POST /api/watches/:id/toggle, pausing or resuming scans.
func HandleToggle(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := common.RequireSessionUser(c, sm); err != nil {
			return err
		}
		id, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		watch, err := q.GetWatchedChannel(ctx, id)
		if err != nil {
			return renderSection(c, q, "Watch not found")
		}
		if err := q.SetWatchedChannelEnabled(ctx, &db.SetWatchedChannelEnabledParams{
			ID:      id,
			Enabled: !watch.Enabled,
		}); err != nil {
			slog.Error("failed to toggle watch", "error", err, "watch_id", c.Param("id"))
			return renderSection(c, q, "Failed to update watch")
		}
		return renderSection(c, q, "")
	}
}

// HandleScanNow serves POST /api/watches/:id/scan, making the watch due
// immediately. The downloader's scheduler picks it up within its poll
// interval (~15s).
func HandleScanNow(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := common.RequireSessionUser(c, sm); err != nil {
			return err
		}
		id, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		if err := q.RequestWatchedChannelScan(ctx, id); err != nil {
			slog.Error("failed to request scan", "error", err, "watch_id", c.Param("id"))
			return renderSection(c, q, "Failed to request scan")
		}
		return renderSection(c, q, "")
	}
}

// HandleDelete serves POST /api/watches/:id/delete, removing the watch and its
// seen-ledger. Archived videos and past jobs are untouched.
func HandleDelete(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := common.RequireSessionUser(c, sm); err != nil {
			return err
		}
		id, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		if err := q.DeleteWatchedChannel(ctx, id); err != nil {
			slog.Error("failed to delete watch", "error", err, "watch_id", c.Param("id"))
			return renderSection(c, q, "Failed to delete watch")
		}
		return renderSection(c, q, "")
	}
}
