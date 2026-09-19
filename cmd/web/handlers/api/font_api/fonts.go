package font_api

import (
	"log/slog"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates/components"
	"thirdcoast.systems/rewind/pkg/typefaces"
)

// HandleList patches the installed-fonts panel.
func HandleList(sm *auth.SessionManager) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return c.String(401, "unauthorized")
		}
		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		common.SetSSEHeaders(c)
		return sse.PatchElementTempl(components.FontsInstalled(typefaces.Installed()), datastar.WithSelectorID("font-installed"))
	}
}

// HandleCatalog searches Fontsource and patches the catalog list.
func HandleCatalog(sm *auth.SessionManager) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return c.String(401, "unauthorized")
		}
		q := strings.TrimSpace(c.QueryParam("q"))
		cat := strings.TrimSpace(c.QueryParam("cat"))
		pickable := c.QueryParam("pick") == "1"
		items, err := typefaces.SearchCatalogCat(c.Request().Context(), q, cat, 40)
		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		common.SetSSEHeaders(c)
		if err != nil {
			slog.Warn("font catalog", "error", err)
			return sse.PatchElementTempl(components.FontsCatalog(nil, err.Error(), pickable), datastar.WithSelectorID("font-catalog"))
		}
		return sse.PatchElementTempl(components.FontsCatalog(items, "", pickable), datastar.WithSelectorID("font-catalog"))
	}
}

type installBody struct {
	Family string `json:"family"`
	ID     string `json:"id"`
}

// HandleInstall downloads a Google family.
func HandleInstall(sm *auth.SessionManager) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return c.String(401, "unauthorized")
		}
		var req installBody
		_ = c.Bind(&req)
		name := strings.TrimSpace(req.Family)
		if name == "" {
			name = strings.TrimSpace(req.ID)
		}
		if name == "" {
			name = strings.TrimSpace(c.QueryParam("id"))
		}
		if name == "" {
			name = strings.TrimSpace(c.QueryParam("family"))
		}
		if name == "" {
			name = strings.TrimSpace(c.FormValue("family"))
		}
		if name == "" {
			return c.String(400, "family required")
		}
		q := strings.TrimSpace(c.QueryParam("q"))
		cat := strings.TrimSpace(c.QueryParam("cat"))
		pickable := c.QueryParam("pick") == "1"
		items, catErr := typefaces.SearchCatalogCat(c.Request().Context(), q, cat, 40)
		if catErr != nil {
			items = nil
		}
		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		common.SetSSEHeaders(c)
		if _, err := typefaces.Install(c.Request().Context(), name); err != nil {
			slog.Warn("font install", "family", name, "error", err)
			msg := err.Error()
			if catErr != nil {
				msg = err.Error()
			}
			_ = sse.PatchElementTempl(components.FontsCatalog(items, msg, pickable), datastar.WithSelectorID("font-catalog"))
			return sse.PatchElementTempl(components.FontsInstalled(typefaces.Installed()), datastar.WithSelectorID("font-installed"))
		}
		_ = sse.PatchElementTempl(components.FontsCatalog(items, "", pickable), datastar.WithSelectorID("font-catalog"))
		return sse.PatchElementTempl(components.FontsInstalled(typefaces.Installed()), datastar.WithSelectorID("font-installed"))
	}
}

// HandleRemove deletes a downloaded family.
func HandleRemove(sm *auth.SessionManager) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return c.String(401, "unauthorized")
		}
		id := strings.TrimSpace(c.Param("id"))
		if err := typefaces.Uninstall(id); err != nil {
			return c.String(400, err.Error())
		}
		sse := datastar.NewSSE(c.Response().Writer, c.Request())
		common.SetSSEHeaders(c)
		return sse.PatchElementTempl(components.FontsInstalled(typefaces.Installed()), datastar.WithSelectorID("font-installed"))
	}
}

// HandleJSON lists installed faces for stitch pickers (non-SSE).
func HandleJSON(sm *auth.SessionManager) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := sm.GetSession(c.Request()); err != nil {
			return c.String(401, "unauthorized")
		}
		return c.JSON(200, typefaces.Installed())
	}
}
