package auth

import (
	"log/slog"
	"strings"

	"github.com/labstack/echo/v4"
	webauth "thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleLoginPage(sm *webauth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		accessLevel, _ := c.Get("accessLevel").(string)
		if accessLevel != "" && accessLevel != string(webauth.AccessUnauthenticated) {
			// New extension auth flow: if already authenticated, bounce directly to finish.
			if c.QueryParam("extensionAuth") == "true" {
				clientID := strings.TrimSpace(c.QueryParam("client_id"))
				redirectURI := strings.TrimSpace(c.QueryParam("redirect_uri"))
				state := strings.TrimSpace(c.QueryParam("state"))
				return c.Redirect(302, "/api/extension/auth/finish?client_id="+clientID+"&redirect_uri="+redirectURI+"&state="+state)
			}

			// If already authenticated and this is an extension login, show success page
			if c.QueryParam("extensionLogin") == "true" {
				extensionID := strings.TrimSpace(c.QueryParam("extensionId"))
				if extensionIDPattern.MatchString(extensionID) {
					userID, _, err := sm.GetSession(c.Request())
					if err == nil && userID != "" {
						tokenStr, err := createExtensionToken(c.Request().Context(), dbc, userID)
						if err == nil {
							return templates.ExtensionLoginSuccessWithToken(tokenStr, extensionID).Render(c.Request().Context(), c.Response())
						}
						slog.Error("failed to create extension token", "error", err)
					}
				}
				return templates.ExtensionLoginSuccess().Render(c.Request().Context(), c.Response())
			}
			return c.Redirect(302, "/")
		}
		return templates.Login("").Render(c.Request().Context(), c.Response())
	}
}
