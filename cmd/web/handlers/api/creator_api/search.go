// Package creator_api serves DataStar fragments for the creator channel picker.
package creator_api

import (
	"log/slog"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

type pickerSignals struct {
	ChannelQ string `json:"channelQ"`
	Name     string `json:"name"`
}

// HandleChannelSearch serves GET /api/creators/channel-search and
// /api/creators/:id/channel-search. An empty box uses the creator name (or the
// wizard name field) as a suggestion hint so the user is not dumped a 200-row
// select.
func HandleChannelSearch(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return echo.NewHTTPError(401, "unauthorized")
		}
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)

		var sig pickerSignals
		_ = datastar.ReadSignals(c.Request(), &sig)
		query := strings.TrimSpace(sig.ChannelQ)
		suggested := false
		if query == "" {
			query = strings.TrimSpace(sig.Name)
			if query == "" {
				if id := strings.TrimSpace(c.Param("id")); id != "" {
					if creatorUUID, err := common.RequireUUIDParam(c, "id"); err == nil {
						if cr, err := q.GetCreator(ctx, creatorUUID); err == nil && cr != nil {
							query = strings.TrimSpace(cr.Name)
							suggested = query != ""
						}
					}
				}
			} else {
				suggested = true
			}
		}

		var hits []*db.SearchUnassignedChannelsRow
		if query != "" {
			rows, err := q.SearchUnassignedChannels(ctx, &db.SearchUnassignedChannelsParams{
				Query:     query,
				PageLimit: int32(24),
			})
			if err != nil {
				slog.Error("search unassigned channels", "error", err, "query", query)
			} else {
				hits = rows
			}
		}

		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		return sse.PatchElementTempl(templates.ChannelSearchResults(hits, query, suggested), datastar.WithSelectorID("creator-channel-results"))
	}
}
