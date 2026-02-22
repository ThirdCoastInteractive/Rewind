package content

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

// streamsManifest mirrors the ingest service's StreamsManifest type.
type streamsManifest struct {
	Streams []struct {
		Filename string `json:"filename"`
		Width    int    `json:"width"`
		Height   int    `json:"height"`
		Codec    string `json:"codec"`
	} `json:"streams"`
}

// readStreamsManifest reads the streams/manifest.json next to the video and
// returns stream heights (for quality chips) and stream qualities (for the
// player quality picker).
func readStreamsManifest(videoPath *string) ([]int, []templates.StreamQuality) {
	if videoPath == nil {
		return nil, nil
	}
	p := strings.TrimSpace(*videoPath)
	if p == "" {
		return nil, nil
	}
	manifestPath := filepath.Join(filepath.Dir(p), "streams", "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, nil
	}
	var m streamsManifest
	if err := json.Unmarshal(data, &m); err != nil {
		slog.Warn("failed to parse streams manifest", "path", manifestPath, "error", err)
		return nil, nil
	}
	var heights []int
	var qualities []templates.StreamQuality
	for _, s := range m.Streams {
		if s.Height > 0 {
			heights = append(heights, s.Height)
			qualities = append(qualities, templates.StreamQuality{
				Label:    fmt.Sprintf("%dp", s.Height),
				Filename: s.Filename,
				Height:   s.Height,
			})
		}
	}
	// Sort qualities by height descending (highest first)
	sort.Slice(qualities, func(i, j int) bool {
		return qualities[i].Height > qualities[j].Height
	})
	return heights, qualities
}

func HandleVideoDetailPage(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userUUID, username, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}

		videoUUID, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}

		videoRow, err := dbc.Queries(c.Request().Context()).GetVideoByID(c.Request().Context(), videoUUID)
		if err != nil || videoRow == nil {
			return c.String(404, "video not found")
		}

		videoData, err := dbc.Queries(c.Request().Context()).GetVideoWithDownloadJob(c.Request().Context(), videoUUID)
		if err != nil {
			slog.Error("failed to fetch video", "error", err)
			return c.String(404, "video not found")
		}

		var savedPosition float64
		positionRow, err := dbc.Queries(c.Request().Context()).GetPlaybackPosition(c.Request().Context(), &db.GetPlaybackPositionParams{
			UserID:  userUUID,
			VideoID: videoUUID,
		})
		if err == nil && positionRow != nil {
			savedPosition = positionRow.PositionSeconds
		}

		// Check for active (queued/processing) asset regeneration or ingest jobs
		activeRegenScopes := map[string]bool{}
		activeJobs, err := dbc.Queries(c.Request().Context()).GetActiveAssetJobsForVideo(c.Request().Context(), videoUUID)
		if err != nil {
			slog.Warn("failed to fetch active asset jobs", "error", err, "video_id", videoUUID)
		} else {
			for _, job := range activeJobs {
				if job.AssetScope == nil {
					activeRegenScopes[""] = true // all assets
				} else {
					activeRegenScopes[*job.AssetScope] = true
				}
			}
		}

		streamHeights, streamQualities := readStreamsManifest(videoRow.VideoPath)

		video := templates.VideoDetail{
			ID:                videoData.VideoID.String(),
			Src:               videoData.Src,
			Title:             videoData.Title,
			Description:       videoRow.Description,
			Info:              videoData.Info,
			ProbeInfo:         videoRow.ProbeData,
			SpoolDir:          "",
			CreatedAt:         videoData.VideoCreatedAt.Time.Format("January 2, 2006 at 3:04 PM"),
			SavedPosition:     savedPosition,
			FileSize:          videoRow.FileSize,
			ActiveRegenScopes: activeRegenScopes,
			VideoPath:         common.DerefString(videoRow.VideoPath),
			StreamHeights:     streamHeights,
			StreamQualities:   streamQualities,
		}

		// Count comments for this video
		commentCount, err := dbc.Queries(c.Request().Context()).CountVideoComments(c.Request().Context(), videoUUID)
		if err == nil {
			video.CommentCount = commentCount
		}

		if videoData.SpoolDir != nil {
			video.SpoolDir = *videoData.SpoolDir
		}

		clips, err := dbc.Queries(c.Request().Context()).ListClipsByVideo(c.Request().Context(), videoUUID)
		if err != nil {
			slog.Warn("failed to fetch clips for video detail", "error", err, "video_id", videoUUID)
			clips = []*db.Clip{}
		}

		keybindings := map[string]string{}
		if rows, err := dbc.Queries(c.Request().Context()).GetUserKeybindings(c.Request().Context(), userUUID); err == nil {
			keybindings = common.KeybindingsRowsToMap(rows)
		}

		return templates.VideoDetailPage(video, clips, username, keybindings).Render(c.Request().Context(), c.Response())
	}
}
