package content

import (
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/internal/scene"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/shownote"
)

// HandleShowNotesLibrary renders the show-note library (notes the user owns or hosts).
func HandleShowNotesLibrary(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, username, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}
		ctx := c.Request().Context()
		notes, err := dbc.Queries(ctx).ListShowNotesForUser(ctx, userUUID)
		if err != nil {
			return c.String(500, "failed to load show notes")
		}
		return templates.ShowNoteLibraryPage(notes, username).Render(ctx, c.Response())
	}
}

// HandleShowNoteCreate creates a new show note and redirects to its editor.
func HandleShowNoteCreate(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}
		ctx := c.Request().Context()
		tenantID, err := shownote.TenantForContext(ctx)
		if err != nil {
			return c.String(403, "workspace required")
		}
		title := strings.TrimSpace(c.FormValue("title"))
		if title == "" {
			title = "Untitled"
		}
		note, err := dbc.Queries(ctx).CreateShowNote(ctx, &db.CreateShowNoteParams{
			OwnerID: userUUID, Title: title, TenantID: tenantID,
		})
		if err != nil {
			return c.String(500, "failed to create show note")
		}
		if videoID := strings.TrimSpace(c.FormValue("video_id")); videoID != "" {
			desc := "rewind://video/" + videoID
			if t := strings.TrimSpace(c.FormValue("t")); t != "" {
				desc += "?t=" + t
			}
			if _, err = dbc.Queries(ctx).UpdateShowNote(ctx, &db.UpdateShowNoteParams{ID: note.ID, Description: &desc}); err != nil {
				return c.String(500, "failed to store moment reference")
			}
		}
		return c.Redirect(302, "/show-notes/"+note.ID.String())
	}
}

// HandleShowNoteEditor renders the collaborative editor for a show note. Access
// is limited to the owner or a roster member with the owner/host role.
func HandleShowNoteEditor(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return handleShowNoteWorkspacePage(sm, dbc, "")
}

// HandleShowNotePanel renders one standalone workspace panel for pop-out windows.
func HandleShowNotePanel(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return handleShowNoteWorkspacePage(sm, dbc, "param")
}

func handleShowNoteWorkspacePage(sm *auth.SessionManager, dbc *db.DatabaseConnection, fixedPanel string) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, username, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}
		noteUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return c.Redirect(302, "/show-notes")
		}
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		note, err := shownote.RequireTenant(ctx, dbc, noteUUID)
		if err != nil {
			return c.Redirect(302, "/show-notes")
		}
		role := "owner"
		if note.OwnerID != userUUID {
			role, err = q.GetHostRole(ctx, &db.GetHostRoleParams{ShowNoteID: noteUUID, UserID: userUUID})
			if err != nil || (role != "owner" && role != "host" && role != "viewer") {
				return c.Redirect(302, "/show-notes")
			}
		}
		panel := fixedPanel
		if fixedPanel == "param" {
			panel = strings.ToLower(c.Param("panel"))
			allowed := map[string]bool{"notes": true, "room": true, "call": true, "program": true, "controls": true}
			if !allowed[panel] {
				return echo.NewHTTPError(404, "unknown workspace panel")
			}
		}
		if err := shownote.EnsureWorkspaceDocument(ctx, dbc, noteUUID); err != nil {
			return echo.NewHTTPError(500, "workspace migration failed").SetInternal(err)
		}
		code := ""
		if note.PublicCode != nil {
			code = *note.PublicCode
		}

		data := templates.ShowNoteData{
			ID:          note.ID.String(),
			Title:       escapeJSSingleQuote(note.Title),
			Description: note.Description,
			IsLive:      note.IsLive,
			ClientID:    uuid.NewString(),
			UserID:      userUUID.String(),
			Username:    username,
			Role:        role,
			Code:        code,
			SceneB64:    scene.ToBase64(scene.FromState(note.SceneState)),
			Panel:       panel,
		}
		return templates.ShowNoteEditorPage(data, username).Render(ctx, c.Response())
	}
}
