package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	ygowebsocket "github.com/reearth/ygo/provider/websocket"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/ctxkeys"
	"thirdcoast.systems/rewind/cmd/web/handlers/admin"
	authhandlers "thirdcoast.systems/rewind/cmd/web/handlers/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/content"
	settingspage "thirdcoast.systems/rewind/cmd/web/handlers/settings"

	"thirdcoast.systems/rewind/cmd/web/handlers/api/audio_api"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/channel_api"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/clip_api"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/compilation_api"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/creator_api"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/fileserver"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/font_api"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/home_api"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/job_api"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/marker_api"
	settingsapi "thirdcoast.systems/rewind/cmd/web/handlers/api/settings_api"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/shownote_api"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/stitch_api"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/tag_api"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/upload_api"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/video_api"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/vision_api"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/watch_api"

	"thirdcoast.systems/rewind/cmd/web/handlers/api/agent_api"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/runtime_api"
	"thirdcoast.systems/rewind/cmd/web/internal/producer"
	"thirdcoast.systems/rewind/cmd/web/internal/shownote"
	"thirdcoast.systems/rewind/cmd/web/internal/telemetry"
	staticpkg "thirdcoast.systems/rewind/cmd/web/internal/web/utils/static"
	"thirdcoast.systems/rewind/internal/agent"
	"thirdcoast.systems/rewind/internal/compilation"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/events"
	rewindmcp "thirdcoast.systems/rewind/internal/mcp"
	"thirdcoast.systems/rewind/internal/runtimecfg"
	workspace "thirdcoast.systems/rewind/internal/shownote"
	"thirdcoast.systems/rewind/pkg/encryption"
)

// Webserver is the main HTTP server that wires together routing, middleware, and all handler groups.
type Webserver struct {
	*echo.Echo
	ctx                 context.Context
	sessionManager      *auth.SessionManager
	encryptionManager   *encryption.Manager
	dbc                 *db.DatabaseConnection
	staticCache         *staticpkg.StaticCache
	fileServer          *fileserver.FileServer
	settingsCache       *db.SettingsCache
	telemetryHub        *telemetry.Hub
	sceneHub            *producer.SceneHub
	showNoteHub         *shownote.Hub
	showNoteCollab      *ygowebsocket.Server
	collaborationAccess collaborationAccess
	allowedExtensionIDs map[string]struct{}
}

// Shutdown drains persisted collaboration updates before closing HTTP peers.
func (s *Webserver) Shutdown(ctx context.Context) error {
	return errors.Join(s.showNoteCollab.Shutdown(ctx), s.Echo.Shutdown(ctx))
}

// NewWebserver initializes the Echo server, registers all routes and middleware, and returns a ready-to-start Webserver.
func NewWebserver(ctx context.Context, dbc *db.DatabaseConnection, encryptionManager *encryption.Manager, sessionManager *auth.SessionManager) (*Webserver, error) {
	if err := runtimecfg.Start(ctx, dbc, "web"); err != nil {
		return nil, err
	}
	e := echo.New()
	runtime_api.Register(e, sessionManager, dbc)
	agent_api.Register(e, sessionManager, dbc)
	agent.Start(ctx, dbc)
	events.Default.Start(ctx, dbc)
	go compilation.Run(ctx, dbc)
	vision_api.Register(e, sessionManager, dbc)
	compilation_api.Register(e, sessionManager, dbc)

	// Initialize static cache
	staticCache, err := staticpkg.NewStaticCache()
	if err != nil {
		return nil, err
	}

	// Initialize settings cache
	settingsCache, err := db.NewSettingsCache(ctx, dbc)
	if err != nil {
		return nil, err
	}
	report := workspace.WorkspacePreflight(ctx, dbc)
	if !report.CanEnable {
		return nil, fmt.Errorf("show-note workspace preflight failed: %d pending, %d failed", report.Pending, report.Failed)
	}

	collab := ygowebsocket.NewServerWithPersistence(workspace.NewPostgresPersistence(dbc))
	collab.PersistCoalesceWindow = -1
	go shownote_api.RunMaterializations(ctx, dbc, collab)
	collab.MaxPeersPerRoom = 32
	collab.MaxConnections = 512
	collab.MaxMessageBytes = 8 << 20
	collab.MaxUpdateBytes = 8 << 20
	collab.MaxAwarenessClientsPerRoom = 32
	collab.MaxAwarenessBytesPerRoom = 1 << 20
	collab.AwarenessExpiry = 45 * time.Second
	collab.Authorize = func(r *http.Request) (ygowebsocket.ConnectionConfig, bool) {
		if r.Context().Err() != nil {
			return ygowebsocket.ConnectionConfig{}, false
		}
		userID, _, err := sessionManager.GetSession(r)
		if err != nil {
			return ygowebsocket.ConnectionConfig{}, false
		}
		var userUUID, noteUUID pgtype.UUID
		if err := userUUID.Scan(userID); err != nil {
			return ygowebsocket.ConnectionConfig{}, false
		}
		if err := noteUUID.Scan(path.Base(r.URL.Path)); err != nil {
			return ygowebsocket.ConnectionConfig{}, false
		}
		access := workspace.UserAccess(r.Context(), dbc, noteUUID, userUUID)
		if access.Allowed {
			if err := workspace.EnsureWorkspaceDocument(r.Context(), dbc, noteUUID); err != nil {
				slog.Error("show-note workspace migration failed", "show_note_id", noteUUID.String(), "error", err)
				return ygowebsocket.ConnectionConfig{}, false
			}
		}
		return ygowebsocket.ConnectionConfig{ReadOnly: access.ReadOnly}, access.Allowed
	}

	webserver := &Webserver{
		Echo:                e,
		ctx:                 ctx,
		sessionManager:      sessionManager,
		encryptionManager:   encryptionManager,
		dbc:                 dbc,
		staticCache:         staticCache,
		fileServer:          fileserver.NewFileServer(),
		settingsCache:       settingsCache,
		telemetryHub:        telemetry.NewHub(),
		sceneHub:            producer.NewSceneHub(),
		showNoteHub:         shownote.NewHub(),
		showNoteCollab:      collab,
		allowedExtensionIDs: parseCommaSeparatedSet(os.Getenv("EXTENSION_ALLOWED_CLIENT_IDS")),
	}
	shownote.StartRoomEventNotifications(ctx, dbc, webserver.showNoteHub)

	if len(webserver.allowedExtensionIDs) == 0 {
		slog.Info("EXTENSION_ALLOWED_CLIENT_IDS not set; extension CORS will be allowed only on localhost/private IP")
	}

	if err = webserver.registerRoutes(); err != nil {
		return nil, err
	}

	if err = webserver.setupMiddleware(); err != nil {
		return nil, err
	}

	return webserver, nil
}

func parseCommaSeparatedSet(raw string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		v := strings.TrimSpace(part)
		if v == "" {
			continue
		}
		set[v] = struct{}{}
	}
	return set
}

// skipGzip avoids compressing media. Gzip on a multi-GB stitch MP4 stalls
// the download (no Content-Length, CPU-bound, browser tab hangs).
func skipGzip(c echo.Context) bool {
	if c.Request().Header.Get("Range") != "" {
		return true
	}
	p := c.Request().URL.Path
	switch {
	case p == "/mcp", p == "/api/ml/jobs/clear":
		return true
	case strings.Contains(p, "/download"), strings.Contains(p, "/stream"):
		return true
	case strings.HasPrefix(p, "/admin/debug/pprof"):
		return true
	case strings.Contains(c.Request().Header.Get("Accept"), "text/event-stream"):
		return true
	default:
		return false
	}
}

// securityHeaders adds standard security and privacy headers to every response.
func securityHeaders(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		h := c.Response().Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(self), microphone=(self), geolocation=()")
		return next(c)
	}
}

func (s *Webserver) setupMiddleware() error {
	s.HideBanner = true
	s.HidePort = true
	s.Use(middleware.BodyLimitWithConfig(middleware.BodyLimitConfig{
		Limit: "2M",
		Skipper: func(c echo.Context) bool {
			return c.Path() == "/api/upload"
		},
	}))
	s.Use(middleware.Recover())
	s.Use(middleware.RequestID())
	s.Use(middleware.GzipWithConfig(middleware.GzipConfig{
		Level:   5,
		Skipper: skipGzip,
	}))
	s.Use(securityHeaders)
	s.Use(pageNavigation)
	s.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		Skipper: func(c echo.Context) bool {
			switch c.Path() {
			case "/mcp",
				"/api/player-sessions/:code/player/telemetry",
				"/api/player-sessions/:code/player/stream",
				"/api/player-sessions/:code/producer/stream",
				"/api/show-notes/:id/events",
				"/api/show-notes/:id/signal",
				"/api/show-notes/:id/producer/stream",
				"/api/show-notes/:id/scene/stream",
				"/api/show-notes/:id/telemetry":
				return true
			default:
				return false
			}
		},
		LogURI:       true,
		LogMethod:    true,
		LogStatus:    true,
		LogLatency:   true,
		LogRemoteIP:  true,
		LogRequestID: true,
		LogError:     true,
		HandleError:  false,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			fields := []any{
				"method", v.Method,
				"uri", v.URI,
				"status", v.Status,
				"latency", v.Latency,
				"remote_ip", v.RemoteIP,
				"request_id", v.RequestID,
			}
			if v.Error != nil {
				fields = append(fields, "error", v.Error)
			}
			slog.Info("request", fields...)
			return nil
		},
	}))

	// Middleware to set access level and registration setting in context
	s.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Read access level directly from the session cookie (stored at login)
			accessLevel := s.sessionManager.GetAccessLevel(c.Request())

			// For authenticated users, validate the session against the DB.
			// This catches disabled users and sessions created before a
			// role change (sessions_invalidated_at).
			if accessLevel != auth.AccessUnauthenticated {
				userID, _, _ := s.sessionManager.GetSession(c.Request())
				var uid pgtype.UUID
				if err := uid.Scan(userID); err == nil {
					q := s.dbc.Queries(c.Request().Context())
					row, err := q.GetSessionInvalidation(c.Request().Context(), uid)
					if err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							slog.Info("missing user session cleared", "user_id", userID)
							s.sessionManager.ClearSession(c.Response().Writer, c.Request())
							accessLevel = auth.AccessUnauthenticated
						} else {
							// Transient DB errors (pool exhaustion, network) must not log the user out.
							slog.Warn("session invalidation check failed", "user_id", userID, "error", err)
						}
					} else if !row.Enabled {
						// User is disabled - clear session
						slog.Info("disabled user session cleared", "user_id", userID)
						s.sessionManager.ClearSession(c.Response().Writer, c.Request())
						accessLevel = auth.AccessUnauthenticated
					} else if row.SessionsInvalidatedAt.Valid {
						createdAt := s.sessionManager.GetSessionCreatedAt(c.Request())
						if !createdAt.IsZero() && row.SessionsInvalidatedAt.Time.After(createdAt) {
							slog.Info("invalidated session cleared", "user_id", userID)
							s.sessionManager.ClearSession(c.Response().Writer, c.Request())
							accessLevel = auth.AccessUnauthenticated
						}
					}
				}
			}

			// Set in Echo context for handlers (stored as plain string for cross-package compatibility)
			c.Set("accessLevel", string(accessLevel))

			// Read registration setting from cache (no DB round-trip)
			regEnabled := s.settingsCache.Get().RegistrationEnabled

			// Set both values in request context for templates
			ctx := context.WithValue(c.Request().Context(), ctxkeys.AccessLevel, string(accessLevel))
			ctx = context.WithValue(ctx, ctxkeys.RegistrationEnabled, regEnabled)
			ctx = context.WithValue(ctx, ctxkeys.StaticVersion, s.staticCache.DistVersion())
			if accessLevel != auth.AccessUnauthenticated {
				userID, _, _ := s.sessionManager.GetSession(c.Request())
				var uid pgtype.UUID
				if uid.Scan(userID) == nil {
					if raw, err := s.dbc.Queries(ctx).GetInterfacePreferences(ctx, uid); err == nil {
						var prefs map[string]any
						if json.Unmarshal(raw, &prefs) == nil {
							ctx = context.WithValue(ctx, ctxkeys.InterfacePreferences, prefs)
						}
					}
				}
			}
			c.SetRequest(c.Request().WithContext(ctx))

			return next(c)
		}
	})

	return nil
}

func (s *Webserver) registerRoutes() error {
	adminGroup := s.Group("/admin")
	adminGroup.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			userID, username, err := s.sessionManager.GetSession(c.Request())
			if err != nil {
				return c.Redirect(302, "/login")
			}

			// Access level is stored in the session cookie at login time.
			if s.sessionManager.GetAccessLevel(c.Request()) != auth.AccessAdmin {
				return c.String(403, "forbidden")
			}

			var userUUID pgtype.UUID
			if err := userUUID.Scan(userID); err != nil {
				return c.String(500, "invalid session")
			}

			c.Set("currentUserUUID", userUUID)
			c.Set("currentUsername", username)
			return next(c)
		}
	})

	adminGroup.GET("", admin.HandleAdminHomePage(s.sessionManager, s.dbc))
	adminGroup.GET("/settings", admin.HandleAdminSettingsPage(s.sessionManager, s.dbc))
	adminGroup.POST("/settings", admin.HandleAdminSettings(s.sessionManager, s.dbc, s.settingsCache))
	adminGroup.GET("/users", admin.HandleAdminUsersPage(s.sessionManager, s.dbc))
	adminGroup.POST("/users/:id/enable", admin.HandleAdminUserEnable(s.sessionManager, s.dbc))
	adminGroup.POST("/users/:id/role", admin.HandleAdminUserRole(s.sessionManager, s.dbc))
	adminGroup.POST("/refresh-assets", admin.HandleAdminRefreshAssets(s.sessionManager, s.dbc))
	// Asset health
	adminGroup.GET("/asset-health", admin.HandleAdminAssetHealthPage(s.sessionManager, s.dbc))
	adminGroup.POST("/asset-health/:id/retry", admin.HandleAdminAssetHealthRetry(s.sessionManager, s.dbc))
	adminGroup.POST("/asset-health/retry-all", admin.HandleAdminAssetHealthRetryAll(s.sessionManager, s.dbc))
	adminGroup.GET("/database", admin.HandleAdminDatabasePage(s.sessionManager, s.dbc))
	adminGroup.POST("/database/reset-statements", admin.HandleAdminDatabaseResetStatements(s.sessionManager, s.dbc))
	registerAdminPprof(adminGroup)
	// Exports management
	adminGroup.GET("/exports", admin.HandleAdminExportsPage(s.sessionManager, s.dbc))
	adminGroup.GET("/exports/index", admin.HandleAdminExportsIndex(s.sessionManager, s.dbc))
	adminGroup.POST("/exports/delete-all", admin.HandleAdminExportsDeleteAll(s.sessionManager, s.dbc))
	adminGroup.POST("/exports/delete/:status", admin.HandleAdminExportsDeleteByStatus(s.sessionManager, s.dbc))
	adminGroup.POST("/exports/requeue-errors", admin.HandleAdminExportsRequeueErrors(s.sessionManager, s.dbc))
	adminGroup.POST("/exports/:id/requeue", admin.HandleAdminExportRequeue(s.sessionManager, s.dbc))
	adminGroup.DELETE("/exports/:id", admin.HandleAdminExportDelete(s.sessionManager, s.dbc))

	apiGroup := s.Group("/api")
	apiGroup.GET("/home/stats", home_api.HandleStats(s.sessionManager, s.dbc))
	apiGroup.GET("/home/recent-published", home_api.HandleRecentPublished(s.sessionManager, s.dbc))
	apiGroup.GET("/home/recent-clips", home_api.HandleRecentClips(s.sessionManager, s.dbc))
	apiGroup.GET("/videos/index", video_api.HandleIndex(s.sessionManager, s.dbc))
	apiGroup.GET("/videos/uploaders", video_api.HandleUploaderOptions(s.sessionManager, s.dbc))
	apiGroup.GET("/videos/recent", video_api.HandleRecent(s.sessionManager, s.dbc))
	apiGroup.GET("/videos/:id/stream", video_api.HandleStream(s.sessionManager, s.dbc))
	apiGroup.GET("/videos/:id/streams/:filename", video_api.HandleStreamFile(s.sessionManager, s.dbc))
	apiGroup.GET("/videos/:id/thumbnail", video_api.HandleThumbnail(s.sessionManager, s.dbc, s.fileServer))
	apiGroup.GET("/videos/:id/preview.mp4", video_api.HandlePreview(s.sessionManager, s.dbc, s.fileServer))
	apiGroup.GET("/videos/:id/seek/seek.json", video_api.HandleSeekManifest(s.sessionManager, s.dbc, s.fileServer))
	apiGroup.GET("/videos/:id/seek/levels/:level/seek.vtt", video_api.HandleSeekVTT(s.sessionManager, s.dbc, s.fileServer))
	apiGroup.GET("/videos/:id/seek/levels/:level/:sheet", video_api.HandleSeekSheet(s.sessionManager, s.dbc, s.fileServer))
	apiGroup.GET("/videos/:id/waveform/waveform.json", video_api.HandleWaveformManifest(s.sessionManager, s.dbc, s.fileServer))
	apiGroup.GET("/videos/:id/waveform/peaks.i16", video_api.HandleWaveformPeaks(s.sessionManager, s.dbc, s.fileServer))
	apiGroup.GET("/videos/:id/captions.vtt", video_api.HandleCaptions(s.sessionManager, s.dbc, s.fileServer))
	apiGroup.GET("/videos/:id/download", video_api.HandleDownload(s.sessionManager, s.dbc, s.fileServer))
	apiGroup.GET("/videos/:id/markers", video_api.HandleMarkers(s.sessionManager, s.dbc))
	apiGroup.GET("/videos/:id/markers/render", video_api.HandleMarkersRender(s.sessionManager, s.dbc))
	apiGroup.GET("/videos/:id/comments/render", video_api.HandleCommentsRender(s.sessionManager, s.dbc))
	apiGroup.GET("/videos/:id/tags/render", tag_api.HandleTagsRender(s.sessionManager, s.dbc))
	apiGroup.POST("/videos/:id/tags", tag_api.HandleAddTag(s.sessionManager, s.dbc))
	apiGroup.DELETE("/videos/:id/tags/:tagId", tag_api.HandleRemoveTag(s.sessionManager, s.dbc))
	apiGroup.GET("/tags", tag_api.HandleListTags(s.sessionManager, s.dbc))
	apiGroup.POST("/videos/bulk-tag", tag_api.HandleBulkTag(s.sessionManager, s.dbc))
	apiGroup.POST("/videos/bulk-delete", video_api.HandleBulkDelete(s.sessionManager, s.dbc))
	apiGroup.GET("/videos/:id/transcript/render", video_api.HandleTranscriptRender(s.sessionManager))
	apiGroup.GET("/videos/:id/context-windows", video_api.HandleContextWindowsList(s.sessionManager, s.dbc))
	apiGroup.POST("/videos/:id/context-windows", video_api.HandleContextWindowCreate(s.sessionManager, s.dbc))
	apiGroup.POST("/videos/:id/generation-retry", video_api.HandleGenerationRetry(s.sessionManager, s.dbc))
	apiGroup.POST("/videos/:id/context-windows/generate", video_api.HandleGenerateContextWindows(s.sessionManager, s.dbc))
	apiGroup.POST("/ml-jobs/:id/retry", video_api.HandleRetryMLJob(s.sessionManager, s.dbc))
	apiGroup.POST("/ml-jobs/:id/priority", video_api.HandleSetMLJobPriority(s.sessionManager, s.dbc))
	apiGroup.GET("/ml/health", video_api.HandleMLRuntimeHealth(s.sessionManager, s.dbc))
	apiGroup.GET("/ml/jobs", video_api.HandleMLJobs(s.sessionManager, s.dbc))
	apiGroup.POST("/ml/jobs/clear", video_api.HandleClearMLQueue(s.sessionManager, s.dbc))
	apiGroup.PUT("/context-windows/:id", video_api.HandleContextWindowUpdate(s.sessionManager, s.dbc))
	apiGroup.DELETE("/context-windows/:id", video_api.HandleContextWindowDelete(s.sessionManager, s.dbc))
	apiGroup.POST("/videos/:id/markers", video_api.HandleMarkersUpdate(s.sessionManager, s.dbc))
	apiGroup.GET("/videos/:id/clips", video_api.HandleClips(s.sessionManager, s.dbc))
	apiGroup.POST("/videos/:id/clips", video_api.HandleClipsCreate(s.sessionManager, s.dbc))
	apiGroup.POST("/videos/:id/redownload", video_api.HandleRedownload(s.sessionManager, s.dbc))
	apiGroup.POST("/videos/:id/download-format", video_api.HandleDownloadFormat(s.sessionManager, s.dbc))
	apiGroup.POST("/videos/:id/regenerate-assets", video_api.HandleRegenerateAssets(s.sessionManager, s.dbc))
	apiGroup.DELETE("/videos/:id", video_api.HandleDelete(s.sessionManager, s.dbc))
	apiGroup.GET("/videos/:id/jobs", video_api.HandleJobs(s.sessionManager, s.dbc))
	apiGroup.POST("/videos/:id/position", settingsapi.HandleSavePlaybackPosition(s.sessionManager, s.dbc))

	apiGroup.PUT("/markers/:id", marker_api.HandleCreateOrUpdate(s.sessionManager, s.dbc))
	apiGroup.DELETE("/markers/:id", marker_api.HandleDelete(s.sessionManager, s.dbc))

	apiGroup.PUT("/clips/:id", clip_api.HandleUpdate(s.sessionManager, s.dbc))
	apiGroup.DELETE("/clips/:id", clip_api.HandleDelete(s.sessionManager, s.dbc))
	apiGroup.POST("/clips/:id/split", clip_api.HandleSplit(s.sessionManager, s.dbc))
	apiGroup.GET("/videos/:videoId/clips/:clipId/select", clip_api.HandleSelect(s.sessionManager, s.dbc))
	apiGroup.GET("/videos/:videoId/clips/:clipId/seek", clip_api.HandleSeek(s.sessionManager, s.dbc))
	apiGroup.POST("/clips/:clipId/crops", clip_api.HandleCropCreate(s.sessionManager, s.dbc))
	apiGroup.PUT("/clips/:clipId/crops/:cropId", clip_api.HandleCropUpdate(s.sessionManager, s.dbc))
	apiGroup.DELETE("/clips/:clipId/crops/:cropId", clip_api.HandleCropDelete(s.sessionManager, s.dbc))
	apiGroup.PUT("/clips/:clipId/shot-list", clip_api.HandleShotListUpdate(s.sessionManager, s.dbc))
	apiGroup.POST("/clips/:clipId/multicam-export", clip_api.HandleMulticamExport(s.sessionManager, s.dbc))
	apiGroup.POST("/clips/:id/exports", clip_api.HandleEnqueueExport(s.sessionManager, s.dbc))
	apiGroup.GET("/clip-exports/:id/stream", clip_api.HandleExportStatusStream(s.sessionManager, s.dbc))
	apiGroup.GET("/clip-exports/:id/download", clip_api.HandleDownloadExport(s.sessionManager, s.dbc))
	apiGroup.GET("/videos/:videoId/clips/export-status", clip_api.HandleBankExportStatus(s.sessionManager, s.dbc))

	// Cut page SSE endpoints
	apiGroup.POST("/videos/:id/cut/filter-cards", video_api.HandleFilterCards())

	apiGroup.POST("/upload", upload_api.HandleUpload(s.sessionManager, s.dbc), middleware.BodyLimit("10G"))
	apiGroup.POST("/download-jobs", job_api.HandleCreateDownload(s.sessionManager, s.dbc))
	apiGroup.POST("/jobs/:id/retry", job_api.HandleRetry(s.sessionManager, s.dbc))
	apiGroup.POST("/jobs/:id/cancel", job_api.HandleCancel(s.sessionManager, s.dbc))
	apiGroup.POST("/jobs/:id/archive", job_api.HandleArchive(s.sessionManager, s.dbc))
	apiGroup.POST("/jobs/:id/unarchive", job_api.HandleUnarchive(s.sessionManager, s.dbc))
	apiGroup.POST("/jobs/archive", job_api.HandleArchiveBatch(s.sessionManager, s.dbc))
	apiGroup.GET("/jobs/index", job_api.HandleIndex(s.sessionManager, s.dbc))
	apiGroup.GET("/jobs/stream", job_api.HandleStream(s.sessionManager, s.dbc))
	apiGroup.GET("/jobs/:id/status", job_api.HandleStatus(s.sessionManager, s.dbc))
	apiGroup.GET("/jobs/:id/logs", job_api.HandleLogs(s.sessionManager, s.dbc))
	apiGroup.GET("/jobs/:id/logs/stream", job_api.HandleLogsStream(s.sessionManager, s.dbc))

	// Channel pages + channel watching (scheduled channel scans)
	apiGroup.GET("/channels/index", channel_api.HandleIndex(s.sessionManager, s.dbc))
	apiGroup.GET("/network/inspect", content.HandleNetworkInspect(s.sessionManager, s.dbc))
	apiGroup.GET("/network/context", content.HandleNetworkContext(s.sessionManager, s.dbc))
	apiGroup.POST("/network/group", content.HandleNetworkGroup(s.sessionManager, s.dbc))
	apiGroup.POST("/network/unlink", content.HandleNetworkUnlink(s.sessionManager, s.dbc))
	apiGroup.GET("/creators/channel-search", creator_api.HandleChannelSearch(s.sessionManager, s.dbc))
	apiGroup.GET("/creators/:id/channel-search", creator_api.HandleChannelSearch(s.sessionManager, s.dbc))
	apiGroup.POST("/watches", watch_api.HandleCreate(s.sessionManager, s.dbc))
	apiGroup.POST("/watches/:id/toggle", watch_api.HandleToggle(s.sessionManager, s.dbc))
	apiGroup.POST("/watches/:id/scan", watch_api.HandleScanNow(s.sessionManager, s.dbc))
	apiGroup.POST("/watches/:id/delete", watch_api.HandleDelete(s.sessionManager, s.dbc))

	apiGroup.POST("/settings/keybindings", settingsapi.HandleKeybindingUpdate(s.sessionManager, s.dbc))
	apiGroup.DELETE("/settings/keybindings/:action", settingsapi.HandleKeybindingDelete(s.sessionManager, s.dbc))
	apiGroup.POST("/settings/keybindings/reset", settingsapi.HandleKeybindingReset(s.sessionManager, s.dbc))

	// Show Notes API (workspace/collab document, hosts, live session)
	apiGroup.GET("/show-notes/:id/document/:room", echo.WrapHandler(s.collaborationAccess.wrap(s.showNoteCollab)))
	apiGroup.GET("/show-notes/:id/workspace", shownote_api.HandleWorkspaceState(s.sessionManager, s.dbc))
	apiGroup.GET("/show-notes/:id/events", shownote_api.HandleRoomEventStream(s.sessionManager, s.dbc, s.showNoteHub))
	apiGroup.POST("/show-notes/:id/messages", shownote_api.HandlePostRoomMessage(s.sessionManager, s.dbc))
	apiGroup.POST("/show-notes/:id/reviews", shownote_api.HandleCreateReview(s.sessionManager, s.dbc, s.showNoteCollab))
	apiGroup.POST("/show-notes/:id/reviews/:threadId/replies", shownote_api.HandlePostReviewReply(s.sessionManager, s.dbc))
	apiGroup.PUT("/show-notes/:id/reviews/:threadId/status", shownote_api.HandleReviewStatus(s.sessionManager, s.dbc, s.showNoteCollab))
	apiGroup.POST("/show-notes/:id/references/:referenceId/materialize", shownote_api.HandleMaterializeReference(s.sessionManager, s.dbc, s.showNoteCollab))
	apiGroup.GET("/show-notes/:id/signal", shownote_api.HandleSignalProxy(s.sessionManager, s.dbc)) // WebRTC signaling -> SFU
	apiGroup.POST("/show-notes/:id/hosts", s.collaborationAccess.changingHosts(shownote_api.HandleAddHost(s.sessionManager, s.dbc)))
	apiGroup.DELETE("/show-notes/:id/hosts/:userId", s.collaborationAccess.changingHosts(shownote_api.HandleRemoveHost(s.sessionManager, s.dbc)))
	apiGroup.PUT("/show-notes/:id", shownote_api.HandleUpdateShowNote(s.sessionManager, s.dbc))
	apiGroup.DELETE("/show-notes/:id", shownote_api.HandleDeleteShowNote(s.sessionManager, s.dbc))

	// Show Notes live session (producer v2: the show note IS the session)
	apiGroup.POST("/show-notes/:id/live", shownote_api.HandleGoLive(s.sessionManager, s.dbc, s.sceneHub))
	apiGroup.POST("/show-notes/:id/offline", shownote_api.HandleEndLive(s.sessionManager, s.dbc))
	apiGroup.POST("/show-notes/:id/scene/apply", shownote_api.HandleApplyScene(s.sessionManager, s.dbc, s.sceneHub))
	apiGroup.POST("/show-notes/:id/scene/set", shownote_api.HandleSetScene(s.sessionManager, s.dbc, s.sceneHub))
	apiGroup.GET("/show-notes/:id/scene/presets/render", shownote_api.HandleScenePresetsRender(s.sessionManager, s.dbc))
	apiGroup.POST("/show-notes/:id/scene/presets", shownote_api.HandleSaveScenePreset(s.sessionManager, s.dbc))
	apiGroup.POST("/show-notes/:id/scene/presets/:presetId/apply", shownote_api.HandleApplyScenePreset(s.sessionManager, s.dbc, s.sceneHub))
	apiGroup.DELETE("/show-notes/:id/scene/presets/:presetId", shownote_api.HandleDeleteScenePreset(s.sessionManager, s.dbc))
	apiGroup.GET("/show-notes/:id/content-sources", shownote_api.HandleContentSourcesRender(s.sessionManager, s.dbc))
	apiGroup.GET("/show-notes/:id/content/:videoId", shownote_api.HandleContentStream(s.dbc))
	apiGroup.POST("/show-notes/:id/director", shownote_api.HandleTakeDirector(s.sessionManager, s.dbc))
	apiGroup.GET("/show-notes/:id/producer/stream", shownote_api.HandleLiveProducerStream(s.sessionManager, s.dbc, s.telemetryHub))
	apiGroup.GET("/show-notes/:id/scene/stream", shownote_api.HandleLiveSceneStream(s.sessionManager, s.dbc, s.telemetryHub, s.sceneHub))
	apiGroup.POST("/show-notes/:id/telemetry", shownote_api.HandleLiveTelemetryPost(s.telemetryHub))

	// Extension API routes with CORS
	extensionAPIGroup := s.Group("/api/extension")
	extensionAPIGroup.Use(s.extensionCORSMiddleware)
	extensionAPIGroup.GET("/auth/start", s.HandleAPIExtensionAuthStart)
	extensionAPIGroup.GET("/auth/finish", s.HandleAPIExtensionAuthFinish)
	extensionAPIGroup.GET("/status", s.HandleAPIExtensionStatus)
	extensionAPIGroup.GET("/status/stream", s.HandleAPIExtensionStatusStream)
	extensionAPIGroup.POST("/archive", s.HandleAPIExtensionArchive)
	extensionAPIGroup.POST("/cookies", s.HandleAPIExtensionCookies)
	extensionAPIGroup.POST("/logout", s.HandleAPIExtensionLogout)

	settingsGroup := s.Group("/settings")
	settingsGroup.GET("", settingspage.HandleSettingsPage(s.sessionManager, s.dbc, s.encryptionManager, s.settingsCache))
	settingsGroup.POST("/cookies", settingspage.HandleSettingsCookies(s.sessionManager, s.dbc, s.encryptionManager, s.settingsCache))
	settingsGroup.GET("/cookies/view", settingspage.HandleSettingsViewCookies(s.sessionManager, s.dbc))
	settingsGroup.GET("/cookies/download", settingspage.HandleSettingsDownloadCookies(s.sessionManager, s.dbc, s.encryptionManager))
	settingsGroup.POST("/cookies/delete", settingspage.HandleSettingsDeleteCookies(s.sessionManager, s.dbc, s.encryptionManager, s.settingsCache))
	settingsGroup.POST("/interface", settingspage.HandleSettingsInterface(s.sessionManager, s.dbc, s.encryptionManager, s.settingsCache))
	settingsGroup.POST("/appearance", settingspage.HandleSettingsAppearance(s.sessionManager, s.dbc))
	settingsGroup.GET("/keybindings", settingspage.HandleSettingsKeybindingsPage(s.sessionManager, s.dbc))
	settingsGroup.POST("/tokens", settingspage.HandleCreateToken(s.sessionManager, s.dbc, s.encryptionManager, s.settingsCache))
	settingsGroup.POST("/tokens/:id/revoke", settingspage.HandleRevokeToken(s.sessionManager, s.dbc, s.encryptionManager, s.settingsCache))

	// MCP Streamable HTTP — bearer API tokens, not session cookies.
	s.Any("/mcp", echo.WrapHandler(rewindmcp.Handler(s.ctx, s.dbc)))

	// Health check
	s.GET("/healthz", func(c echo.Context) error {
		return c.String(200, "ok")
	})

	// Static file serving
	s.GET("/static/*", s.staticCache.ServeStaticFile("/static/"))

	// Favicon — serve a minimal transparent 1x1 ICO to prevent 404s on every page load.
	s.GET("/favicon.ico", func(c echo.Context) error {
		c.Response().Header().Set(echo.HeaderCacheControl, "public, max-age=86400")
		// 1x1 transparent ICO (70 bytes)
		ico := []byte{
			0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x01, 0x01,
			0x00, 0x00, 0x01, 0x00, 0x18, 0x00, 0x30, 0x00,
			0x00, 0x00, 0x16, 0x00, 0x00, 0x00, 0x28, 0x00,
			0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x02, 0x00,
			0x00, 0x00, 0x01, 0x00, 0x18, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		}
		return c.Blob(http.StatusOK, "image/x-icon", ico)
	})

	// Auth routes
	s.GET("/login", authhandlers.HandleLoginPage(s.sessionManager, s.dbc))
	s.POST("/login", authhandlers.HandleLogin(s.sessionManager, s.dbc))
	s.GET("/register", authhandlers.HandleRegisterPage(s.sessionManager, s.dbc, s.settingsCache))
	s.POST("/register", authhandlers.HandleRegister(s.sessionManager, s.dbc, s.settingsCache))
	s.GET("/logout", authhandlers.HandleLogout(s.sessionManager))

	// Stitch routes
	stitch_api.RegisterEditor(apiGroup, s.sessionManager, s.dbc)
	stitchExportsDir := strings.TrimSpace(os.Getenv("EXPORTS_DIR"))
	if stitchExportsDir == "" {
		stitchExportsDir = "/exports"
	}
	stitch_api.RegisterEditorMedia(apiGroup, s.sessionManager, s.dbc, stitchExportsDir)
	apiGroup.GET("/audio", audio_api.HandleList(s.sessionManager))
	apiGroup.GET("/audio/json", audio_api.HandleJSON(s.sessionManager))
	apiGroup.GET("/audio/:id/file", audio_api.HandleFile(s.sessionManager))
	apiGroup.POST("/audio", audio_api.HandleUpload(s.sessionManager))
	apiGroup.GET("/fonts", font_api.HandleList(s.sessionManager))
	apiGroup.GET("/fonts/json", font_api.HandleJSON(s.sessionManager))
	apiGroup.GET("/fonts/catalog", font_api.HandleCatalog(s.sessionManager))
	apiGroup.POST("/fonts", font_api.HandleInstall(s.sessionManager))
	apiGroup.DELETE("/fonts/:id", font_api.HandleRemove(s.sessionManager))

	apiGroup.GET("/stitch/sources/json", stitch_api.HandleStitchSourceBrowserJSON(s.sessionManager, s.dbc))
	apiGroup.GET("/stitch/:id/download", stitch_api.HandleStitchDownload(s.sessionManager, s.dbc))
	apiGroup.GET("/stitch/:id/captions", stitch_api.HandleStitchCaptions(s.sessionManager, s.dbc))
	apiGroup.GET("/stitch/:id/stream", stitch_api.HandleStitchStream(s.sessionManager, s.dbc))
	apiGroup.POST("/stitch/projects", stitch_api.HandleCreateProject(s.sessionManager, s.dbc))
	apiGroup.DELETE("/stitch/projects/:id", stitch_api.HandleDeleteProject(s.sessionManager, s.dbc))
	apiGroup.POST("/stitch/folders", stitch_api.HandleCreateFolder(s.sessionManager, s.dbc))
	apiGroup.POST("/stitch/folders/:id/delete", stitch_api.HandleDeleteFolder(s.sessionManager, s.dbc))
	apiGroup.POST("/stitch/projects/:id/folder", stitch_api.HandleMoveProject(s.sessionManager, s.dbc))

	// Content routes
	s.GET("/stitch", content.HandleStitchLibrary(s.sessionManager, s.dbc))
	s.GET("/stitch/:id", content.HandleStitchEditor(s.sessionManager, s.dbc))
	s.GET("/show-notes", content.HandleShowNotesLibrary(s.sessionManager, s.dbc))
	s.POST("/show-notes", content.HandleShowNoteCreate(s.sessionManager, s.dbc))
	s.GET("/show-notes/:id", content.HandleShowNoteEditor(s.sessionManager, s.dbc))
	s.GET("/show-notes/:id/panel/:panel", content.HandleShowNotePanel(s.sessionManager, s.dbc))
	s.GET("/show-notes/:id/live", content.HandleShowNoteLivePage(s.sessionManager, s.dbc))
	s.GET("/show/:code", content.HandleShowViewerPage(s.dbc))
	s.GET("/jobs", content.HandleJobsPage(s.sessionManager, s.dbc))
	s.GET("/follows", content.HandleFollowsPage(s.sessionManager, s.dbc))
	s.GET("/watches", func(c echo.Context) error {
		q := c.Request().URL.RawQuery
		if q != "" {
			return c.Redirect(302, "/follows?"+q)
		}
		return c.Redirect(302, "/follows")
	})
	s.GET("/wiki", content.HandleWikiIndex(s.sessionManager, s.dbc))
	s.POST("/wiki/save", content.HandleWikiSave(s.sessionManager, s.dbc))
	s.GET("/wiki/:tree", content.HandleWikiPage(s.sessionManager, s.dbc))
	s.GET("/wiki/:tree/*", content.HandleWikiPage(s.sessionManager, s.dbc))
	s.GET("/creators", content.HandleCreatorsPage(s.sessionManager, s.dbc))
	s.POST("/creators", content.HandleCreatorCreate(s.sessionManager, s.dbc))
	s.GET("/creators/new", content.HandleCreatorWizardPage(s.sessionManager))
	s.GET("/creators/bundles/new", content.HandleCreatorBundleWizardPage(s.sessionManager, s.dbc))
	s.POST("/creators/bundles", content.HandleCreatorBundleCreate(s.sessionManager, s.dbc))
	s.GET("/creators/bundles/:id", content.HandleCreatorBundleViewPage(s.sessionManager, s.dbc))
	s.POST("/creators/bundles/:id/add", content.HandleCreatorBundleAddMember(s.sessionManager, s.dbc))
	s.POST("/creators/bundles/:id/remove", content.HandleCreatorBundleRemoveMember(s.sessionManager, s.dbc))
	s.GET("/creators/:id", content.HandleCreatorViewPage(s.sessionManager, s.dbc))
	s.POST("/creators/:id/link", content.HandleCreatorLinkChannel(s.sessionManager, s.dbc))
	s.POST("/creators/:id/unlink", content.HandleCreatorUnlinkChannel(s.sessionManager, s.dbc))
	s.POST("/creators/:id/catalog", content.HandleCreatorCatalog(s.sessionManager, s.dbc))
	s.POST("/creators/suggestions/:id/accept", content.HandleCreatorSuggestionAccept(s.sessionManager, s.dbc))
	s.POST("/creators/suggestions/:id/dismiss", content.HandleCreatorSuggestionDismiss(s.sessionManager, s.dbc))
	s.GET("/network", content.HandleNetworkPage(s.sessionManager, s.dbc))
	s.GET("/investigate", content.HandleInvestigatePage(s.sessionManager, s.dbc))
	s.POST("/investigate/x-replies", content.HandleInvestigateIndexXReplies(s.sessionManager, s.dbc, s.encryptionManager))
	s.GET("/investigate/commenters/:id", content.HandleInvestigateCommenterPage(s.sessionManager, s.dbc))
	s.GET("/investigate/campaigns/:id", content.HandleInvestigateCampaignPage(s.sessionManager, s.dbc))
	s.POST("/api/investigate/commenters/:id/watch", content.HandleInvestigateWatch(s.sessionManager, s.dbc))
	s.POST("/api/investigate/commenters/:id/unwatch", content.HandleInvestigateUnwatch(s.sessionManager, s.dbc))
	s.POST("/api/investigate/commenters/:id/link", content.HandleInvestigateLinkCommenters(s.sessionManager, s.dbc))
	s.POST("/api/investigate/flags/:id/dismiss", content.HandleInvestigateDismissFlag(s.sessionManager, s.dbc))
	s.GET("/channels", content.HandleChannelsPage(s.sessionManager, s.dbc))
	s.GET("/channels/view", content.HandleChannelViewPage(s.sessionManager, s.dbc))
	s.POST("/channels/view/index-metadata", content.HandleIndexChannelMetadata(s.sessionManager, s.dbc))
	s.POST("/channels/view/catalog", content.HandleIndexChannelCatalog(s.sessionManager, s.dbc))
	s.POST("/catalog-crawls/:id/:action", content.HandleCatalogCrawlControl(s.sessionManager, s.dbc))
	s.GET("/jobs/:id", content.HandleJobDetailPage(s.sessionManager, s.dbc))
	s.GET("/videos", content.HandleVideosPage(s.sessionManager, s.dbc))
	s.GET("/videos/:id/cut", content.HandleVideoCutPage(s.sessionManager, s.dbc))
	s.GET("/videos/:id", content.HandleVideoDetailPage(s.sessionManager, s.dbc))
	s.GET("/upload", content.HandleUploadPage(s.sessionManager))
	s.GET("/bookmarklet", content.HandleBookmarklet(s.sessionManager, s.dbc))
	s.GET("/", content.HandleHomePage(s.sessionManager))
	s.POST("/archive", content.HandleArchiveSubmit(s.sessionManager, s.dbc))

	return nil
}

func registerAdminPprof(g *echo.Group) {
	pprofGroup := g.Group("/debug/pprof")
	pprofGroup.Use(pprofMiddleware("/admin/debug/pprof"))
	pprofGroup.GET("", pprofNoop)
	pprofGroup.GET("/*", pprofNoop)
	pprofGroup.POST("/symbol", pprofNoop)
}

func pprofNoop(echo.Context) error { return nil }

func pprofMiddleware(prefix string) echo.MiddlewareFunc {
	prefix = strings.TrimRight(prefix, "/")
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if !pprofLocalRequest(c) {
				return c.String(http.StatusForbidden, "pprof is only available from localhost")
			}
			req := c.Request()
			reqPath := req.URL.Path
			switch {
			case strings.HasPrefix(reqPath, prefix+"/cmdline"):
				pprof.Cmdline(c.Response(), req)
			case strings.HasPrefix(reqPath, prefix+"/profile"):
				pprof.Profile(c.Response(), req)
			case strings.HasPrefix(reqPath, prefix+"/symbol"):
				pprof.Symbol(c.Response(), req)
			case strings.HasPrefix(reqPath, prefix+"/trace"):
				pprof.Trace(c.Response(), req)
			default:
				name := strings.TrimPrefix(reqPath, prefix)
				name = strings.TrimPrefix(name, "/")
				if name != "" {
					pprof.Handler(name).ServeHTTP(c.Response(), req)
				} else {
					pprof.Index(c.Response(), req)
				}
			}
			return nil
		}
	}
}

func pprofLocalRequest(c echo.Context) bool {
	if isLoopbackHost(hostWithoutPort(c.Request().Host)) {
		return true
	}
	if isLoopbackHost(c.RealIP()) {
		return true
	}
	if xff := c.Request().Header.Get(echo.HeaderXForwardedFor); xff != "" {
		first := strings.TrimSpace(strings.Split(xff, ",")[0])
		if isLoopbackHost(hostWithoutPort(first)) {
			return true
		}
	}
	if xri := c.Request().Header.Get(echo.HeaderXRealIP); xri != "" && isLoopbackHost(hostWithoutPort(xri)) {
		return true
	}
	return false
}

func hostWithoutPort(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return host
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
