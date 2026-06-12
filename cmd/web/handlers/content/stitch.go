package content

import (
	"encoding/json"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

// HandleStitchLibrary renders the stitch project library page.
func HandleStitchLibrary(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, username, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}
		ctx := c.Request().Context()
		projects, err := dbc.Queries(ctx).ListStitchProjects(ctx, userUUID)
		if err != nil {
			return c.String(500, "failed to load projects")
		}

		// Fetch latest export status per project.
		exportMap := make(map[string]*db.LatestStitchJobPerProjectRow)
		if len(projects) > 0 {
			ids := make([]pgtype.UUID, len(projects))
			for i, p := range projects {
				ids[i] = p.ID
			}
			latestJobs, err := dbc.Queries(ctx).LatestStitchJobPerProject(ctx, ids)
			if err == nil {
				for _, j := range latestJobs {
					if j.ProjectID.Valid {
						exportMap[j.ProjectID.String()] = j
					}
				}
			}
		}

		return templates.StitchLibraryPage(projects, exportMap, username).Render(ctx, c.Response())
	}
}

// HandleStitchEditor renders the stitch editor loaded with a specific project.
func HandleStitchEditor(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, username, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}
		projectUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return c.Redirect(302, "/stitch")
		}
		ctx := c.Request().Context()
		project, err := dbc.Queries(ctx).GetStitchProject(ctx, projectUUID)
		if err != nil {
			return c.Redirect(302, "/stitch")
		}

		// Marshal segments to a clean JSON string for embedding in data-signals
		segJSON := "[]"
		if len(project.Segments) > 0 && string(project.Segments) != "null" {
			segJSON = string(project.Segments)
		}
		// Escape single quotes in the title for safe embedding in JS string literal
		safeTitle := escapeJSSingleQuote(project.Title)

		data := templates.StitchProjectData{
			ID:                project.ID.String(),
			Title:             safeTitle,
			Format:            project.Format,
			Quality:           project.Quality,
			SegmentsJSON:      segJSON,
			GlobalFiltersJSON: string(project.GlobalFilters),
		}

		return templates.StitchPage(data, username).Render(ctx, c.Response())
	}
}

func escapeJSSingleQuote(s string) string {
	b, _ := json.Marshal(s)
	// json.Marshal wraps in double quotes: "hello 'world'" → strip outer quotes
	// and replace inner characters appropriately for single-quote JS context
	if len(b) >= 2 {
		s = string(b[1 : len(b)-1])
	}
	// The result is already JS-safe (json.Marshal escapes backslashes, etc.)
	// But we need to also handle single quotes for the data-signals='...' context
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			result = append(result, '\\', '\'')
		} else {
			result = append(result, s[i])
		}
	}
	return string(result)
}
