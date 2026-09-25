package stitch_api

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/stitch"
	"thirdcoast.systems/rewind/pkg/plugin"
)

func requireStitchJobAccess(c echo.Context, sm *auth.SessionManager, job *db.GetStitchJobRow, user pgtype.UUID) error {
	if job == nil || !job.CreatedBy.Valid || job.CreatedBy.Bytes != user.Bytes {
		if sm.GetAccessLevel(c.Request()) != auth.AccessAdmin {
			return echo.NewHTTPError(http.StatusNotFound, "stitch job not found")
		}
	}
	return nil
}

func requireStitchJobSources(c echo.Context, q *db.Queries, job *db.GetStitchJobRow) error {
	return requireStitchJobSourcesDepth(c, q, job, map[string]bool{}, 0)
}

// requireStitchRenderJobSources covers canonical jobs whose legacy segments
// column is intentionally empty. Download and stream handlers must validate
// the immutable document snapshot before serving an already-created file.
func requireStitchRenderJobSources(c echo.Context, dbc *db.DatabaseConnection, job *db.GetStitchJobRow) error {
	return requireStitchRenderJobSourcesDepth(c, dbc, job, map[string]bool{}, 0)
}

func requireStitchRenderJobSourcesDepth(c echo.Context, dbc *db.DatabaseConnection, job *db.GetStitchJobRow, seen map[string]bool, depth int) error {
	if depth > 4 || job == nil {
		return echo.NewHTTPError(http.StatusNotFound, "stitch source unavailable")
	}
	if seen[job.ID.String()] {
		return echo.NewHTTPError(http.StatusBadRequest, "cyclic stitch source")
	}
	seen[job.ID.String()] = true
	defer delete(seen, job.ID.String())
	if job == nil || !job.CreatedBy.Valid {
		return echo.NewHTTPError(http.StatusNotFound, "stitch source unavailable")
	}
	renderJob, err := stitch.NewStore(dbc).GetRenderJob(c.Request().Context(), job.CreatedBy, job.ID)
	if err != nil || len(renderJob.Document) == 0 {
		return requireStitchJobSources(c, dbc.Queries(c.Request().Context()), job)
	}
	var snapshot stitch.RenderSnapshot
	if err := json.Unmarshal(renderJob.Document, &snapshot); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "stitch source unavailable")
	}
	return requireStitchDocumentSources(c, dbc, snapshot.Document, seen, depth)
}

func requireStitchDocumentSources(c echo.Context, dbc *db.DatabaseConnection, document stitch.Document, seen map[string]bool, depth int) error {
	if depth > 4 {
		return echo.NewHTTPError(http.StatusNotFound, "stitch source unavailable")
	}
	q := dbc.Queries(c.Request().Context())
	for _, segment := range document.Segments {
		if segment.VideoID != "" {
			var id pgtype.UUID
			if id.Scan(segment.VideoID) != nil {
				return echo.NewHTTPError(http.StatusNotFound, "stitch source unavailable")
			}
			if _, err := common.RequireVideo(c, q, id, plugin.ActionVideoRead); err != nil {
				return err
			}
		}
		if segment.ClipID != "" {
			var id pgtype.UUID
			if id.Scan(segment.ClipID) != nil {
				return echo.NewHTTPError(http.StatusNotFound, "stitch source unavailable")
			}
			clip, err := q.GetClip(c.Request().Context(), id)
			if err != nil {
				return echo.NewHTTPError(http.StatusNotFound, "stitch source unavailable")
			}
			if _, err := common.RequireVideo(c, q, clip.VideoID, plugin.ActionVideoRead); err != nil {
				return err
			}
		}
		if segment.ExportJobID != "" {
			var id pgtype.UUID
			if id.Scan(segment.ExportJobID) != nil {
				return echo.NewHTTPError(http.StatusNotFound, "stitch source unavailable")
			}
			job, err := q.GetStitchJob(c.Request().Context(), id)
			if err != nil {
				return echo.NewHTTPError(http.StatusNotFound, "stitch source unavailable")
			}
			if err := requireStitchRenderJobSourcesDepth(c, dbc, job, seen, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func requireStitchJobSourcesDepth(c echo.Context, q *db.Queries, job *db.GetStitchJobRow, seen map[string]bool, depth int) error {
	if job == nil || depth > 4 {
		return echo.NewHTTPError(http.StatusNotFound, "stitch source unavailable")
	}
	key := job.ID.String()
	if seen[key] {
		return echo.NewHTTPError(http.StatusBadRequest, "cyclic stitch source")
	}
	seen[key] = true
	defer delete(seen, key)
	var segments []map[string]json.RawMessage
	if json.Unmarshal(job.Segments, &segments) != nil {
		return echo.NewHTTPError(http.StatusNotFound, "stitch source unavailable")
	}
	for _, segment := range segments {
		for _, field := range []string{"video_id", "source_video_id"} {
			if raw := segment[field]; len(raw) > 0 {
				var id string
				if json.Unmarshal(raw, &id) == nil && id != "" {
					var video pgtype.UUID
					if video.Scan(id) != nil {
						return echo.NewHTTPError(http.StatusNotFound, "stitch source unavailable")
					}
					if _, err := common.RequireVideo(c, q, video, plugin.ActionVideoRead); err != nil {
						return err
					}
				}
			}
		}
		if raw := segment["clip_id"]; len(raw) > 0 {
			var id string
			if json.Unmarshal(raw, &id) == nil && id != "" {
				var clipID pgtype.UUID
				if clipID.Scan(id) != nil {
					return echo.NewHTTPError(http.StatusNotFound, "stitch source unavailable")
				}
				clip, err := q.GetClip(c.Request().Context(), clipID)
				if err != nil {
					return echo.NewHTTPError(http.StatusNotFound, "stitch source unavailable")
				}
				if _, err := common.RequireVideo(c, q, clip.VideoID, plugin.ActionVideoRead); err != nil {
					return err
				}
			}
		}
		if raw := segment["export_job_id"]; len(raw) > 0 {
			var id string
			if json.Unmarshal(raw, &id) == nil && id != "" {
				var jobID pgtype.UUID
				if jobID.Scan(id) != nil {
					return echo.NewHTTPError(http.StatusNotFound, "stitch source unavailable")
				}
				nested, err := q.GetStitchJob(c.Request().Context(), jobID)
				if err != nil {
					return echo.NewHTTPError(http.StatusNotFound, "stitch source unavailable")
				}
				if err := requireStitchJobSourcesDepth(c, q, nested, seen, depth+1); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
