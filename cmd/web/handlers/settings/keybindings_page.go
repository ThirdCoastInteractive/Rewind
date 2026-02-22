package settings_api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/ctxkeys"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/encryption"
	"thirdcoast.systems/rewind/pkg/utils/crypto"
)

func HandleSettingsKeybindingsPage(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, username, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}

		bindings := map[string]string{}
		if rows, err := dbc.Queries(c.Request().Context()).GetUserKeybindings(c.Request().Context(), userUUID); err == nil {
			bindings = common.KeybindingsRowsToMap(rows)
		}

		rows := buildKeybindingRowModels(bindings)
		return templates.SettingsKeybindingsPage(username, rows, bindings).Render(c.Request().Context(), c.Response())
	}
}

// Helper functions
func renderSettingsPage(c echo.Context, sm *auth.SessionManager, dbc *db.DatabaseConnection, encMgr *encryption.Manager, sc *db.SettingsCache, userUUID pgtype.UUID, username string, cookiesValue string, message string) error {
	ctx := c.Request().Context()
	var adminSettings *db.InstanceSetting

	user, err := dbc.Queries(ctx).SelectUserByID(ctx, userUUID)
	if err == nil && user != nil && user.Role == "admin" {
		c.Set("accessLevel", "admin")
		ctx2 := context.WithValue(c.Request().Context(), ctxkeys.AccessLevel, "admin")
		c.SetRequest(c.Request().WithContext(ctx2))

		q := dbc.Queries(ctx)
		settings, err := q.GetInstanceSettings(ctx)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				settings = &db.InstanceSetting{RegistrationEnabled: true, AdminEmails: []string{}}
			} else {
				slog.Error("failed to load instance settings", "error", err)
				settings = &db.InstanceSetting{RegistrationEnabled: true, AdminEmails: []string{}}
			}
		}
		if settings.AdminEmails == nil {
			settings.AdminEmails = []string{}
		}

		// Get storage limit from database
		limitBytes, err := q.GetClipExportStorageLimit(ctx)
		if err == nil {
			settings.ClipExportStorageLimitBytes = limitBytes
		} else if !db.IsUndefinedColumnErr(err) {
			slog.Error("failed to load clip export storage limit", "error", err)
		}

		adminSettings = settings
	}

	return templates.Settings(cookiesValue, message, true, username, adminSettings).Render(ctx, c.Response())
}

func generateCookiesFile(encMgr *encryption.Manager, cookies []*db.GetUserCookiesRow) string {
	if len(cookies) == 0 {
		return ""
	}

	var lines []string
	lines = append(lines, "# Netscape HTTP Cookie File")
	lines = append(lines, "# This is a generated file! Do not edit.")

	for _, cookie := range cookies {
		var cookieValue crypto.EncryptedString = cookie.Value
		if err := encryption.Decrypt(encMgr, &cookieValue); err != nil {
			slog.Error("failed to decrypt cookie value", "error", err, "domain", cookie.Domain, "name", cookie.Name)
			continue
		}
		value, valid := cookieValue.Get()
		if !valid {
			continue
		}

		line := fmt.Sprintf("%s\t%s\t%s\t%s\t%d\t%s\t%s",
			cookie.Domain, cookie.Flag, cookie.Path, cookie.Secure,
			cookie.Expiration, cookie.Name, value)
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func buildKeybindingRowModels(bindings map[string]string) []templates.KeybindingRowModel {
	actions := []string{
		"play-pause", "seek-forward", "seek-backward", "volume-up", "volume-down",
		"toggle-mute", "toggle-fullscreen", "toggle-picture-in-picture",
		"playback-rate-increase", "playback-rate-decrease", "playback-rate-reset",
		"seek-to-start", "seek-to-end", "frame-forward", "frame-backward",
		"create-marker", "create-clip-in", "create-clip-out",
	}

	rows := make([]templates.KeybindingRowModel, 0, len(actions))
	caser := cases.Title(language.English)
	for _, action := range actions {
		label := strings.ReplaceAll(action, "-", " ")
		label = caser.String(label)
		key, exists := bindings[action]
		displayKey := key
		if !exists || key == "" {
			displayKey = "Not set"
		}
		rows = append(rows, templates.KeybindingRowModel{
			ID:         action,
			Label:      label,
			DefaultKey: "",
			DisplayKey: displayKey,
			HasCustom:  exists && key != "",
		})
	}
	return rows
}
