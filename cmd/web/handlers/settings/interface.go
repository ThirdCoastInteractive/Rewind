package settings_api

import (
	"encoding/json"
	"log/slog"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/interfaceprefs"
	"thirdcoast.systems/rewind/pkg/encryption"
)

// HandleSettingsInterface serves POST /settings/interface, saving user interface preferences like sound and motion settings.
func HandleSettingsInterface(sm *auth.SessionManager, dbc *db.DatabaseConnection, encMgr *encryption.Manager, sc *db.SettingsCache) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, username, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}

		raw, _ := json.Marshal(map[string]bool{"sounds_enabled": c.FormValue("sounds_enabled") == "on", "reduced_motion": c.FormValue("reduced_motion") == "on"})
		if err = dbc.Queries(c.Request().Context()).MergeInterfacePreferences(c.Request().Context(), &db.MergeInterfacePreferencesParams{UserID: userUUID, Preferences: raw}); err != nil {
			return err
		}

		slog.Info("interface preferences updated",
			"user", username,
			"sounds_enabled", c.FormValue("sounds_enabled"))

		return c.Redirect(303, "/settings")
	}
}

// HandleSettingsAppearance validates and persists only the changed appearance field.
func HandleSettingsAppearance(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		owner, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}
		key, value := c.QueryParam("key"), c.QueryParam("value")
		signal := ""
		switch {
		case key == "theme" && interfaceprefs.ValidTheme(value):
			signal = "uiTheme"
		case key == "color_mode" && interfaceprefs.ValidMode(value):
			signal = "uiColorMode"
		default:
			return echo.NewHTTPError(400, "Unknown theme or color mode")
		}
		raw, _ := json.Marshal(map[string]string{key: value})
		if err = dbc.Queries(c.Request().Context()).MergeInterfacePreferences(c.Request().Context(), &db.MergeInterfacePreferencesParams{UserID: owner, Preferences: raw}); err != nil {
			return err
		}
		patch, _ := json.Marshal(map[string]string{signal: value, "appearanceStatus": "Appearance saved"})
		return datastar.NewSSE(c.Response(), c.Request()).PatchSignals(patch)
	}
}
