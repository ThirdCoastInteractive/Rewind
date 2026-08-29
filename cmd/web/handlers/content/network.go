package content

import (
	"log/slog"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleNetworkPage serves GET /network, listing harvested channel-to-channel edges.
func HandleNetworkPage(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, username, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}
		ctx := c.Request().Context()
		edges, err := dbc.Queries(ctx).ListChannelEdges(ctx)
		if err != nil {
			slog.Error("failed to list channel edges", "error", err)
			edges = nil
		}
		return templates.Network(edges, username).Render(ctx, c.Response())
	}
}
