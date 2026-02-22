package settings_api

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/encryption"
)

func HandleSettingsCookies(sm *auth.SessionManager, dbc *db.DatabaseConnection, encMgr *encryption.Manager, sc *db.SettingsCache) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, username, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}

		cookiesContent := strings.TrimSpace(c.FormValue("cookies_content"))
		if cookiesContent == "" {
			return renderSettingsPage(c, sm, dbc, encMgr, sc, userUUID, username, "", "No cookies content provided")
		}

		normalizedContent := strings.ReplaceAll(cookiesContent, "\r\n", "\n")
		normalizedContent = strings.ReplaceAll(normalizedContent, "\r", "\n")
		lines := strings.Split(normalizedContent, "\n")

		validCount := 0
		invalidCount := 0
		var firstInvalidLine string

		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}

			parts := strings.Split(trimmed, "\t")
			if len(parts) >= 7 {
				expiration, err := strconv.ParseInt(parts[4], 10, 64)
				if err != nil {
					invalidCount++
					if firstInvalidLine == "" {
						firstInvalidLine = trimmed
					}
					continue
				}

				encryptedValue, err := encryption.Encrypt(encMgr, parts[6])
				if err != nil {
					slog.Error("failed to encrypt cookie value", "error", err)
					invalidCount++
					if firstInvalidLine == "" {
						firstInvalidLine = trimmed
					}
					continue
				}

				if err := dbc.Queries(c.Request().Context()).InsertCookie(c.Request().Context(), &db.InsertCookieParams{
					UserID:     userUUID,
					Domain:     parts[0],
					Flag:       parts[1],
					Path:       parts[2],
					Secure:     parts[3],
					Expiration: expiration,
					Name:       parts[5],
					Value:      encryptedValue,
				}); err != nil {
					slog.Error("failed to insert cookie", "error", err)
					invalidCount++
					if firstInvalidLine == "" {
						firstInvalidLine = trimmed
					}
					continue
				}

				validCount++
			} else {
				invalidCount++
				if firstInvalidLine == "" {
					firstInvalidLine = trimmed
				}
			}
		}

		if validCount == 0 {
			slog.Warn("invalid cookies format", "valid_lines", validCount, "invalid_lines", invalidCount, "first_invalid", firstInvalidLine, "user", username)
			errMsg := fmt.Sprintf("Invalid format. Found %d invalid cookie lines. ", invalidCount)
			errMsg += "Cookies must be in Netscape format with TAB-separated values (not spaces). "
			errMsg += "Each line should have: domain[TAB]flag[TAB]path[TAB]secure[TAB]expiration[TAB]name[TAB]value. "
			errMsg += "Use a browser extension like 'Get cookies.txt LOCALLY' or 'cookies.txt' to export in the correct format."
			return renderSettingsPage(c, sm, dbc, encMgr, sc, userUUID, username, "", errMsg)
		}

		slog.Info("cookies saved successfully", "user", username, "original_lines", len(lines), "valid_cookies", validCount, "invalid_lines", invalidCount)

		cookies, err := dbc.Queries(c.Request().Context()).GetUserCookies(c.Request().Context(), userUUID)
		if err != nil {
			slog.Error("failed to fetch cookies", "error", err)
		}
		cookiesDisplay := generateCookiesFile(encMgr, cookies)

		successMsg := fmt.Sprintf("Cookies saved successfully (%d valid cookies from %d total lines)", validCount, len(lines))
		return renderSettingsPage(c, sm, dbc, encMgr, sc, userUUID, username, cookiesDisplay, successMsg)
	}
}
