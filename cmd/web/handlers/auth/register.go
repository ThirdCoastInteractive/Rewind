package auth

import (
	"log/slog"
	"strings"

	"github.com/labstack/echo/v4"
	webauth "thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleRegister(sm *webauth.SessionManager, dbc *db.DatabaseConnection, sc *db.SettingsCache) echo.HandlerFunc {
	return func(c echo.Context) error {
		username := strings.TrimSpace(c.FormValue("username"))
		email := strings.TrimSpace(c.FormValue("email"))
		password := c.FormValue("password")
		confirmPassword := c.FormValue("confirm_password")

		if username == "" || email == "" || password == "" {
			return templates.Register("All fields are required").Render(c.Request().Context(), c.Response())
		}

		if password != confirmPassword {
			return templates.Register("Passwords do not match").Render(c.Request().Context(), c.Response())
		}

		q := dbc.Queries(c.Request().Context())
		userCount, err := q.CountUsers(c.Request().Context())
		if err != nil {
			slog.Error("failed to count users", "error", err)
			return templates.Register("An error occurred. Please try again.").Render(c.Request().Context(), c.Response())
		}

		role := "user"
		if userCount == 0 {
			role = "admin"
		} else {
			settings := sc.Get()
			if settings != nil && !settings.RegistrationEnabled {
				return templates.Register("Registration is disabled on this instance").Render(c.Request().Context(), c.Response())
			}
		}

		// Check if username is taken
		taken, err := q.UsernameTaken(c.Request().Context(), username)
		if err != nil {
			slog.Error("failed to check username", "error", err)
			return templates.Register("An error occurred. Please try again.").Render(c.Request().Context(), c.Response())
		}
		if taken {
			return templates.Register("Username is already taken").Render(c.Request().Context(), c.Response())
		}

		// Check if email is registered
		registered, err := q.EmailRegistered(c.Request().Context(), email)
		if err != nil {
			slog.Error("failed to check email", "error", err)
			return templates.Register("An error occurred. Please try again.").Render(c.Request().Context(), c.Response())
		}
		if registered {
			return templates.Register("Email is already registered").Render(c.Request().Context(), c.Response())
		}

		// Create user with hashed password
		user, err := q.NewUser(c.Request().Context(), db.NewUserParams{
			Username: username,
			Email:    email,
			Password: password,
			Role:     role,
		})
		if err != nil {
			slog.Error("failed to create user", "error", err)
			return templates.Register("Password does not meet requirements (minimum 8 characters) or an error occurred").Render(c.Request().Context(), c.Response())
		}

		// Determine access level from role
		accessLevel := webauth.AccessUser
		if role == "admin" {
			accessLevel = webauth.AccessAdmin
		}

		// Save session
		if err := sm.SaveSession(c.Response().Writer, c.Request(), user.ID.String(), user.UserName, accessLevel); err != nil {
			slog.Error("failed to save session", "error", err)
			return templates.Register("Account created but failed to log in. Please try logging in.").Render(c.Request().Context(), c.Response())
		}

		return c.Redirect(302, "/")
	}
}
