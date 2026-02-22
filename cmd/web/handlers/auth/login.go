package auth

import (
	"log/slog"
	"strings"

	"github.com/labstack/echo/v4"
	webauth "thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/utils/passwords"
)

func HandleLogin(sm *webauth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		username := strings.TrimSpace(c.FormValue("username"))
		password := c.FormValue("password")

		if username == "" || password == "" {
			return templates.Login("Username and password are required").Render(c.Request().Context(), c.Response())
		}

		// Try to find user by username first, then by email
		user, err := dbc.Queries(c.Request().Context()).SelectUserByUserName(c.Request().Context(), username)
		if err != nil {
			// Try email
			user, err = dbc.Queries(c.Request().Context()).SelectUserByEmail(c.Request().Context(), username)
			if err != nil {
				return templates.Login("Invalid username or password").Render(c.Request().Context(), c.Response())
			}
		}

		// Verify password
		matches, err := user.Password.ComparePasswordAndHash(passwords.PasswordInput{Password: password})
		if err != nil || !matches {
			return templates.Login("Invalid username or password").Render(c.Request().Context(), c.Response())
		}

		// Check if user is enabled
		if !user.Enabled {
			return templates.Login("Account is disabled").Render(c.Request().Context(), c.Response())
		}

		// Determine access level from role
		accessLevel := webauth.AccessUser
		if user.Role == "admin" {
			accessLevel = webauth.AccessAdmin
		}

		// Save session
		if err := sm.SaveSession(c.Response().Writer, c.Request(), user.ID.String(), user.UserName, accessLevel); err != nil {
			slog.Error("failed to save session", "error", err)
			return templates.Login("An error occurred. Please try again.").Render(c.Request().Context(), c.Response())
		}

		// New extension auth flow
		if c.QueryParam("extensionAuth") == "true" {
			clientID := strings.TrimSpace(c.QueryParam("client_id"))
			redirectURI := strings.TrimSpace(c.QueryParam("redirect_uri"))
			state := strings.TrimSpace(c.QueryParam("state"))
			return c.Redirect(302, "/api/extension/auth/finish?client_id="+clientID+"&redirect_uri="+redirectURI+"&state="+state)
		}

		// Check if this is an extension login
		if c.QueryParam("extensionLogin") == "true" {
			extensionID := strings.TrimSpace(c.QueryParam("extensionId"))
			if extensionIDPattern.MatchString(extensionID) {
				tokenStr, err := createExtensionToken(c.Request().Context(), dbc, user.ID.String())
				if err != nil {
					slog.Error("failed to create extension token", "error", err)
					return templates.Login("An error occurred. Please try again.").Render(c.Request().Context(), c.Response())
				}
				return templates.ExtensionLoginSuccessWithToken(tokenStr, extensionID).Render(c.Request().Context(), c.Response())
			}
			return templates.ExtensionLoginSuccess().Render(c.Request().Context(), c.Response())
		}

		return c.Redirect(302, "/")
	}
}
