package content

import (
	"context"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

func loadNetworkWiki(ctx context.Context, q *db.Queries) ([]*db.WikiPage, []*db.WikiLink) {
	pages, err := q.ListWikiPages(ctx, "")
	if err != nil {
		slog.Error("failed to list wiki pages for network", "error", err)
		pages = nil
	}
	links, err := q.ListWikiLinks(ctx)
	if err != nil {
		slog.Error("failed to list wiki links for network", "error", err)
		links = nil
	}
	return pages, links
}

// HandleNetworkPage serves GET /network, listing harvested channel-to-channel edges.
func HandleNetworkPage(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userID, username, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		hanging := c.QueryParam("hanging") == "1"
		bundleID := strings.TrimSpace(c.QueryParam("bundle"))
		edges, err := q.ListChannelEdges(ctx)
		if err != nil {
			slog.Error("failed to list channel edges", "error", err)
			edges = nil
		}
		bundles, err := q.ListCreatorBundles(ctx)
		if err != nil {
			slog.Error("failed to list creator bundles", "error", err)
			bundles = nil
		}
		var bundle pgtype.UUID
		if bundleID != "" {
			if err := bundle.Scan(bundleID); err != nil || !bundle.Valid {
				bundle = pgtype.UUID{}
				bundleID = ""
			}
		}
		includedCreators := map[string]bool{}
		if bundle.Valid {
			members, err := q.ListCreatorBundleMembers(ctx, bundle)
			if err != nil {
				slog.Error("failed to list bundle members", "error", err, "bundle_id", bundleID)
			} else {
				allow := map[string]struct{}{}
				for _, m := range members {
					if m != nil {
						allow[m.ID.String()] = struct{}{}
						includedCreators[m.ID.String()] = true
					}
				}
				filtered := edges[:0]
				for _, e := range edges {
					if e == nil {
						continue
					}
					_, from := allow[e.FromCreatorID.String()]
					_, to := allow[e.ToCreatorID.String()]
					if from || to {
						filtered = append(filtered, e)
					}
				}
				edges = filtered
			}
		}
		channels, err := q.ListNetworkChannels(ctx)
		if err != nil {
			return err
		}

		if bundle.Valid {
			includedChannels := map[string]bool{}
			for _, e := range edges {
				includedChannels[e.FromChannelID.String()] = true
				includedChannels[e.ToChannelID.String()] = true
			}
			filtered := channels[:0]
			for _, ch := range channels {
				if includedCreators[ch.CreatorID.String()] || includedChannels[ch.ID.String()] {
					filtered = append(filtered, ch)
				}
			}
			channels = filtered
		}
		wikiPages, wikiLinks := loadNetworkWiki(ctx, q)
		commenters, commenterEdges := loadNetworkCommenters(ctx, dbc, userID)
		return templates.Network(edges, bundles, bundleID, hanging, username, channels, wikiPages, wikiLinks, commenters, commenterEdges).Render(ctx, c.Response())
	}
}
