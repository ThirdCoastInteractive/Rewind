package upload_api

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/internal/db"
)

var allowedExts = map[string]bool{
	".mp4": true, ".mkv": true, ".webm": true, ".avi": true, ".mov": true,
	".flv": true, ".wmv": true, ".mpg": true, ".mpeg": true, ".m4v": true,
	".ts": true, ".mts": true, ".m2ts": true, ".vob": true, ".3gp": true,
	".ogv": true, ".divx": true, ".asf": true, ".f4v": true, ".rm": true,
	".rmvb": true,
}
// HandleUpload serves POST /api/upload, accepting a local video file upload and enqueuing it for ingest.
func HandleUpload(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		archivedByUUID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}

		file, err := c.FormFile("file")
		if err != nil {
			return c.JSON(400, map[string]string{"error": "file is required"})
		}

		ext := strings.ToLower(filepath.Ext(file.Filename))
		if !allowedExts[ext] {
			return c.JSON(400, map[string]string{"error": "unsupported file type: " + ext})
		}

		spoolID := uuid.New().String()
		spoolDir := filepath.Join("/downloads", ".upload-spool", spoolID)
		if err := os.MkdirAll(spoolDir, 0755); err != nil {
			slog.Error("failed to create upload spool dir", "error", err)
			return c.JSON(500, map[string]string{"error": "failed to create spool directory"})
		}

		videoFilename := spoolID + ext
		videoPath := filepath.Join(spoolDir, videoFilename)

		src, err := file.Open()
		if err != nil {
			return c.JSON(500, map[string]string{"error": "failed to open uploaded file"})
		}
		defer src.Close()

		dst, err := os.Create(videoPath)
		if err != nil {
			slog.Error("failed to create spool file", "error", err)
			return c.JSON(500, map[string]string{"error": "failed to write file"})
		}
		defer dst.Close()

		written, err := io.Copy(dst, src)
		if err != nil {
			slog.Error("failed to write uploaded file", "error", err)
			os.RemoveAll(spoolDir)
			return c.JSON(500, map[string]string{"error": "failed to write file"})
		}
		dst.Close()

		slog.Info("upload received", "filename", file.Filename, "size", written, "spool", spoolDir)

		title := strings.TrimSuffix(file.Filename, filepath.Ext(file.Filename))

		infoJSON := map[string]any{
			"id":           spoolID,
			"title":        title,
			"extractor":    "local_upload",
			"webpage_url":  "upload://" + file.Filename,
			"original_url": "upload://" + file.Filename,
			"upload_date":  time.Now().Format("20060102"),
		}
		infoBytes, _ := json.MarshalIndent(infoJSON, "", "  ")
		infoPath := filepath.Join(spoolDir, spoolID+".info.json")
		if err := os.WriteFile(infoPath, infoBytes, 0644); err != nil {
			slog.Error("failed to write info.json", "error", err)
			os.RemoveAll(spoolDir)
			return c.JSON(500, map[string]string{"error": "failed to write metadata"})
		}

		srcURL := fmt.Sprintf("upload://%s/%s", spoolID, file.Filename)

		job, err := dbc.Queries(c.Request().Context()).EnqueueUploadIngestJob(c.Request().Context(), &db.EnqueueUploadIngestJobParams{
			URL:          srcURL,
			ArchivedBy:   archivedByUUID,
			SpoolDir:     &spoolDir,
			InfoJsonPath: &infoPath,
		})
		if err != nil {
			slog.Error("failed to enqueue upload ingest job", "error", err)
			os.RemoveAll(spoolDir)
			return c.JSON(500, map[string]string{"error": "failed to enqueue ingest job"})
		}

		slog.Info("upload ingest job enqueued",
			"ingest_job_id", job.IngestJobID,
			"download_job_id", job.DownloadJobID,
			"filename", file.Filename)

		return c.JSON(200, map[string]any{
			"ingest_job_id":   job.IngestJobID.String(),
			"download_job_id": job.DownloadJobID.String(),
			"filename":        file.Filename,
			"size":            written,
		})
	}
}
