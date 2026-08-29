package settings_api

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	rewindmcp "thirdcoast.systems/rewind/internal/mcp"
	"thirdcoast.systems/rewind/pkg/encryption"
)

// HandleCreateToken mints a user API token for MCP and renders it once.
func HandleCreateToken(sm *auth.SessionManager, dbc *db.DatabaseConnection, encMgr *encryption.Manager, sc *db.SettingsCache) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, username, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}
		name := c.FormValue("name")
		plain, err := newTokenSecret()
		if err != nil {
			return echo.NewHTTPError(500, "could not generate token")
		}
		_, err = dbc.Queries(c.Request().Context()).InsertAPIToken(c.Request().Context(), &db.InsertAPITokenParams{
			UserID:    userUUID,
			Name:      name,
			TokenHash: rewindmcp.HashToken(plain),
			Scopes:    []string{"mcp:read", "mcp:write"},
		})
		if err != nil {
			slog.Error("create api token", "error", err)
			return echo.NewHTTPError(500, "could not save token")
		}

		cookies, _ := dbc.Queries(c.Request().Context()).GetUserCookies(c.Request().Context(), userUUID)
		cookiesValue := generateCookiesFile(encMgr, cookies)
		return renderSettingsPage(c, sm, dbc, encMgr, sc, userUUID, username, cookiesValue, "Token created", plain)
	}
}

// HandleRevokeToken marks one of the current user's API tokens as revoked.
func HandleRevokeToken(sm *auth.SessionManager, dbc *db.DatabaseConnection, encMgr *encryption.Manager, sc *db.SettingsCache) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, username, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}
		id, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return c.Redirect(302, "/settings")
		}
		if err := dbc.Queries(c.Request().Context()).RevokeAPIToken(c.Request().Context(), &db.RevokeAPITokenParams{
			ID:     id,
			UserID: userUUID,
		}); err != nil {
			slog.Error("revoke api token", "error", err, "token_id", id)
			return c.Redirect(302, "/settings?err=Could+not+revoke+token")
		}
		cookies, _ := dbc.Queries(c.Request().Context()).GetUserCookies(c.Request().Context(), userUUID)
		cookiesValue := generateCookiesFile(encMgr, cookies)
		return renderSettingsPage(c, sm, dbc, encMgr, sc, userUUID, username, cookiesValue, "Token revoked", "")
	}
}

func newTokenSecret() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "rw_" + hex.EncodeToString(b[:]), nil
}
