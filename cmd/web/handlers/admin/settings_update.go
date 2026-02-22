package admin

import (
	"log/slog"
	"net/url"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/internal/db"
)

func HandleAdminSettings(sm *auth.SessionManager, dbc *db.DatabaseConnection, sc *db.SettingsCache) echo.HandlerFunc {
	return func(c echo.Context) error {
		enabled := c.FormValue("registration_enabled") != ""
		q := dbc.Queries(c.Request().Context())

		// Parse admin emails from comma-separated input
		adminEmailsInput := strings.TrimSpace(c.FormValue("admin_emails"))
		var adminEmails []string
		if adminEmailsInput != "" {
			for _, email := range strings.Split(adminEmailsInput, ",") {
				if trimmed := strings.TrimSpace(email); trimmed != "" {
					adminEmails = append(adminEmails, trimmed)
				}
			}
		}

		if err := q.UpsertRegistrationEnabled(c.Request().Context(), &db.UpsertRegistrationEnabledParams{
			RegistrationEnabled: enabled,
			AdminEmails:         adminEmails,
		}); err != nil {
			slog.Error("failed to update registration_enabled", "error", err)
			return c.Redirect(302, "/settings?err="+url.QueryEscape("Failed to update settings"))
		}
		// Settings cache will be updated via LISTEN/NOTIFY

		// Parse human-readable storage limit (e.g., "100M", "10G", "1K")
		limitInput := strings.TrimSpace(c.FormValue("clip_export_storage_limit"))
		if limitInput != "" {
			bytes, err := humanize.ParseBytes(limitInput)
			if err != nil {
				slog.Warn("invalid storage limit format", "input", limitInput, "error", err)
				return c.Redirect(302, "/settings?err="+url.QueryEscape("Invalid storage limit format. Use formats like 100M, 10G, 1K, etc."))
			}
			if err := q.UpsertClipExportStorageLimit(c.Request().Context(), int64(bytes)); err != nil {
				if !db.IsUndefinedColumnErr(err) {
					slog.Error("failed to update clip_export_storage_limit_bytes", "error", err)
					return c.Redirect(302, "/settings?err="+url.QueryEscape("Failed to update settings"))
				}
			}
		} else {
			// Empty input means set to 0 (unlimited)
			if err := q.UpsertClipExportStorageLimit(c.Request().Context(), 0); err != nil {
				if !db.IsUndefinedColumnErr(err) {
					slog.Error("failed to update clip_export_storage_limit_bytes", "error", err)
					return c.Redirect(302, "/settings?err="+url.QueryEscape("Failed to update settings"))
				}
			}
		}

		// Update admin emails
		if err := q.UpsertAdminEmails(c.Request().Context(), adminEmails); err != nil {
			if !db.IsUndefinedColumnErr(err) {
				slog.Error("failed to update admin_emails", "error", err)
				return c.Redirect(302, "/settings?err="+url.QueryEscape("Failed to update settings"))
			}
		}
		// Reload settings cache eagerly so the change is visible immediately.
		if sc != nil {
			_ = sc.Reload(c.Request().Context())
		}

		return c.Redirect(302, "/settings?msg="+url.QueryEscape("Settings saved successfully"))
	}
}

// HandleAdminUsersPage shows the user management page.
