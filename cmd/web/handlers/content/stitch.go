package content

import (
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/stitch"
)

// HandleStitchLibrary renders the stitch project library page.
func HandleStitchLibrary(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, username, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		view := templates.StitchLibraryView{
			ViewerID:     userUUID,
			ViewerName:   username,
			FolderMode:   "all",
			Query:        strings.TrimSpace(c.QueryParam("q")),
			CanManage:    true,
		}

		userParam := strings.TrimSpace(c.QueryParam("user"))
		switch userParam {
		case "", "me":
			view.FilterUser = userUUID
			view.FilterUserName = username
		case "all":
			view.AllUsers = true
			view.FilterUserName = "Everyone"
			view.CanManage = false
		default:
			owner, err := common.ParseUUID(userParam)
			if err != nil {
				return c.Redirect(302, "/stitch")
			}
			view.FilterUser = owner
			view.CanManage = owner == userUUID
			if u, lookupErr := q.SelectUserByID(ctx, owner); lookupErr == nil && u != nil {
				view.FilterUserName = u.UserName
			} else {
				view.FilterUserName = "User"
			}
		}

		folderParam := strings.TrimSpace(c.QueryParam("folder"))
		if view.Query != "" {
			view.FolderMode = "all"
		} else if folderParam == "unfiled" {
			view.FolderMode = "unfiled"
		} else if folderParam != "" {
			folderID, err := common.ParseUUID(folderParam)
			if err != nil {
				return c.Redirect(302, "/stitch")
			}
			folder, err := q.GetStitchFolder(ctx, folderID)
			if err != nil {
				return c.Redirect(302, "/stitch")
			}
			if !view.AllUsers && folder.CreatedBy != view.FilterUser {
				return c.Redirect(302, "/stitch")
			}
			view.Folder = folder
			view.FolderMode = "folder"
			view.FilterUser = folder.CreatedBy
			view.AllUsers = false
			if u, lookupErr := q.SelectUserByID(ctx, folder.CreatedBy); lookupErr == nil && u != nil {
				view.FilterUserName = u.UserName
			}
			view.CanManage = folder.CreatedBy == userUUID
		}

		owners, err := q.ListStitchProjectOwners(ctx)
		if err != nil {
			return c.String(500, "failed to load projects")
		}
		view.Owners = owners

		if !view.AllUsers {
			folders, err := q.ListStitchFoldersForUser(ctx, view.FilterUser)
			if err != nil {
				return c.String(500, "failed to load folders")
			}
			view.Folders = folders
		}

		listArgs := &db.ListStitchProjectsParams{
			UserID:     view.FilterUser,
			FolderMode: view.FolderMode,
			FolderID:   pgtype.UUID{},
		}
		if view.Folder != nil {
			listArgs.FolderID = view.Folder.ID
		}
		if view.Query != "" {
			listArgs.Query = &view.Query
			listArgs.FolderMode = "all"
			view.FolderMode = "all"
		}
		projects, err := q.ListStitchProjects(ctx, listArgs)
		if err != nil {
			return c.String(500, "failed to load projects")
		}
		view.Projects = projects

		view.Exports = map[string]*db.LatestStitchJobPerProjectRow{}
		if len(projects) > 0 {
			ids := make([]pgtype.UUID, len(projects))
			for i, p := range projects {
				ids[i] = p.ID
			}
			latestJobs, err := q.LatestStitchJobPerProject(ctx, ids)
			if err == nil {
				for _, j := range latestJobs {
					if j.ProjectID.Valid {
						view.Exports[j.ProjectID.String()] = j
					}
				}
			}
		}

		return templates.StitchLibraryPage(view, username).Render(ctx, c.Response())
	}
}

// HandleStitchEditor renders the stitch editor loaded with a specific project.
func HandleStitchEditor(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, username, err := common.RequireSessionUser(c, sm)
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
		readOnly := project.CreatedBy != userUUID
		if !readOnly {
			if _, err := stitch.NewStore(dbc).Enable(ctx, userUUID, projectUUID); err != nil {
				return c.String(500, "failed to open editor")
			}
		}
		return templates.StitchWorkspacePage(projectUUID.String(), username, readOnly).Render(ctx, c.Response())
	}
}
