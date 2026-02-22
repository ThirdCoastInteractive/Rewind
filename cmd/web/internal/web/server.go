package web

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/ctxkeys"
	"thirdcoast.systems/rewind/cmd/web/handlers/admin"
	authhandlers "thirdcoast.systems/rewind/cmd/web/handlers/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/content"
	"thirdcoast.systems/rewind/cmd/web/handlers/sessions"
	settingspage "thirdcoast.systems/rewind/cmd/web/handlers/settings"

	"thirdcoast.systems/rewind/cmd/web/handlers/api/clip_api"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/fileserver"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/job_api"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/marker_api"
	settingsapi "thirdcoast.systems/rewind/cmd/web/handlers/api/settings_api"
	"thirdcoast.systems/rewind/cmd/web/handlers/api/video_api"

	"thirdcoast.systems/rewind/cmd/web/internal/producer"
	"thirdcoast.systems/rewind/cmd/web/internal/telemetry"
	staticpkg "thirdcoast.systems/rewind/cmd/web/internal/web/utils/static"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/encryption"
)

type Webserver struct {
	*echo.Echo
	sessionManager      *auth.SessionManager
	encryptionManager   *encryption.Manager
	dbc                 *db.DatabaseConnection
	staticCache         *staticpkg.StaticCache
	fileServer          *fileserver.FileServer
	settingsCache       *db.SettingsCache
	telemetryHub        *telemetry.Hub
	sceneHub            *producer.SceneHub
	allowedExtensionIDs map[string]struct{}
}

func NewWebserver(ctx context.Context, dbc *db.DatabaseConnection, encryptionManager *encryption.Manager, sessionManager *auth.SessionManager) (*Webserver, error) {
	e := echo.New()

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

	webserver := &Webserver{
		Echo:                e,
		sessionManager:      sessionManager,
		encryptionManager:   encryptionManager,
		dbc:                 dbc,
		staticCache:         staticCache,
		fileServer:          fileserver.NewFileServer(),
		settingsCache:       settingsCache,
		telemetryHub:        telemetry.NewHub(),
		sceneHub:            producer.NewSceneHub(),
		allowedExtensionIDs: parseCommaSeparatedSet(os.Getenv("EXTENSION_ALLOWED_CLIENT_IDS")),
	}

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

func (s *Webserver) setupMiddleware() error {
	s.HideBanner = true
	s.HidePort = true
	s.Use(middleware.BodyLimit("2M"))
	s.Use(middleware.Recover())
	s.Use(middleware.RequestID())
	s.Use(middleware.GzipWithConfig(middleware.GzipConfig{
		Level: 5,
	}))
	s.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		Skipper: func(c echo.Context) bool {
			switch c.Path() {
			case "/api/player-sessions/:code/player/telemetry",
				"/api/player-sessions/:code/player/stream",
				"/api/player-sessions/:code/producer/stream":
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
						// User deleted or DB error - clear session
						slog.Warn("session invalidation check failed", "user_id", userID, "error", err)
						s.sessionManager.ClearSession(c.Response().Writer, c.Request())
						accessLevel = auth.AccessUnauthenticated
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
	adminGroup.GET("/settings", admin.HandleAdminSettingsPage())
	adminGroup.POST("/settings", admin.HandleAdminSettings(s.sessionManager, s.dbc, s.settingsCache))
	adminGroup.GET("/users", admin.HandleAdminUsersPage(s.sessionManager, s.dbc))
	adminGroup.POST("/users/:id/enable", admin.HandleAdminUserEnable(s.sessionManager, s.dbc))
	adminGroup.POST("/users/:id/role", admin.HandleAdminUserRole(s.sessionManager, s.dbc))
	adminGroup.POST("/refresh-assets", admin.HandleAdminRefreshAssets(s.sessionManager, s.dbc))
	// Asset health
	adminGroup.GET("/asset-health", admin.HandleAdminAssetHealthPage(s.sessionManager, s.dbc))
	adminGroup.POST("/asset-health/:id/retry", admin.HandleAdminAssetHealthRetry(s.sessionManager, s.dbc))
	adminGroup.POST("/asset-health/retry-all", admin.HandleAdminAssetHealthRetryAll(s.sessionManager, s.dbc))
	// Exports management
	adminGroup.GET("/exports", admin.HandleAdminExportsPage(s.sessionManager, s.dbc))
	adminGroup.GET("/exports/index", admin.HandleAdminExportsIndex(s.sessionManager, s.dbc))
	adminGroup.POST("/exports/delete-all", admin.HandleAdminExportsDeleteAll(s.sessionManager, s.dbc))
	adminGroup.POST("/exports/delete/:status", admin.HandleAdminExportsDeleteByStatus(s.sessionManager, s.dbc))
	adminGroup.POST("/exports/requeue-errors", admin.HandleAdminExportsRequeueErrors(s.sessionManager, s.dbc))
	adminGroup.POST("/exports/:id/requeue", admin.HandleAdminExportRequeue(s.sessionManager, s.dbc))
	adminGroup.DELETE("/exports/:id", admin.HandleAdminExportDelete(s.sessionManager, s.dbc))

	apiGroup := s.Group("/api")
	apiGroup.GET("/videos/index", video_api.HandleIndex(s.sessionManager, s.dbc))
	apiGroup.GET("/videos/recent", video_api.HandleRecent(s.sessionManager, s.dbc))
	apiGroup.GET("/videos/:id/stream", video_api.HandleStream(s.sessionManager, s.dbc))
	apiGroup.GET("/videos/:id/streams/:filename", video_api.HandleStreamFile(s.sessionManager, s.dbc))
	apiGroup.GET("/videos/:id/hls/*", video_api.HandleHLS(s.sessionManager, s.dbc))
	apiGroup.GET("/videos/:id/hls-ready", video_api.HandleHLSCheck(s.sessionManager, s.dbc))
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
	apiGroup.GET("/videos/:id/transcript/render", video_api.HandleTranscriptRender(s.sessionManager))
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
	apiGroup.POST("/clips/:id/exports", clip_api.HandleEnqueueExport(s.sessionManager, s.dbc))
	apiGroup.GET("/clip-exports/:id/stream", clip_api.HandleExportStatusStream(s.sessionManager, s.dbc))
	apiGroup.GET("/clip-exports/:id/download", clip_api.HandleDownloadExport(s.sessionManager, s.dbc))
	apiGroup.GET("/videos/:videoId/clips/export-status", clip_api.HandleBankExportStatus(s.sessionManager, s.dbc))

	// Cut page SSE endpoints
	apiGroup.POST("/videos/:id/cut/filter-cards", video_api.HandleFilterCards())

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

	apiGroup.POST("/settings/keybindings", settingsapi.HandleKeybindingUpdate(s.sessionManager, s.dbc))
	apiGroup.DELETE("/settings/keybindings/:action", settingsapi.HandleKeybindingDelete(s.sessionManager, s.dbc))
	apiGroup.POST("/settings/keybindings/reset", settingsapi.HandleKeybindingReset(s.sessionManager, s.dbc))

	apiGroup.GET("/player-sessions/:code/producer/stream", sessions.HandleProducerStream(s.sessionManager, s.dbc, s.telemetryHub))
	apiGroup.GET("/player-sessions/:code/player/stream", sessions.HandlePlayerStream(s.sessionManager, s.dbc, s.telemetryHub, s.sceneHub))
	apiGroup.POST("/player-sessions/:code/player/telemetry", sessions.HandlePlayerTelemetry(s.telemetryHub))

	apiGroup.DELETE("/player-sessions/:id", sessions.HandleDeletePlayerSession(s.sessionManager, s.dbc))

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
	settingsGroup.GET("/keybindings", settingspage.HandleSettingsKeybindingsPage(s.sessionManager, s.dbc))

	producerGroup := s.Group("/producer")
	producerGroup.GET("", sessions.HandleProducerHomePage(s.sessionManager, s.dbc))
	producerGroup.GET("/sessions/manage", sessions.HandleProducerSessionManagePage(s.sessionManager, s.dbc))
	producerGroup.POST("/sessions", sessions.HandleProducerCreateSession(s.sessionManager, s.dbc))
	producerGroup.GET("/:code", sessions.HandleProducerSessionPage(s.sessionManager, s.dbc))
	producerGroup.POST("/:code/scenes/apply", sessions.HandleProducerApplyScene(s.sessionManager, s.dbc, s.sceneHub))
	producerGroup.POST("/:code/scenes/presets", sessions.HandleProducerSaveScenePreset(s.sessionManager, s.dbc))
	producerGroup.POST("/:code/scenes/presets/:id/apply", sessions.HandleProducerApplyScenePreset(s.sessionManager, s.dbc, s.sceneHub))
	producerGroup.POST("/:code/scenes/presets/:id/delete", sessions.HandleProducerDeleteScenePreset(s.sessionManager, s.dbc))

	playerGroup := s.Group("/player")
	playerGroup.GET("", sessions.HandlePlayerPage())
	playerGroup.POST("/join", sessions.HandlePlayerJoin(s.dbc))
	playerGroup.GET("/:code", sessions.HandlePlayerSessionPage(s.dbc))

	// Health check
	s.GET("/healthz", func(c echo.Context) error {
		return c.String(200, "ok")
	})

	// Static file serving
	s.GET("/static/*", s.staticCache.ServeStaticFile("/static/"))

	// Auth routes
	s.GET("/login", authhandlers.HandleLoginPage(s.sessionManager, s.dbc))
	s.POST("/login", authhandlers.HandleLogin(s.sessionManager, s.dbc))
	s.GET("/register", authhandlers.HandleRegisterPage(s.sessionManager, s.dbc, s.settingsCache))
	s.POST("/register", authhandlers.HandleRegister(s.sessionManager, s.dbc, s.settingsCache))
	s.GET("/logout", authhandlers.HandleLogout(s.sessionManager))

	// Content routes
	s.GET("/jobs", content.HandleJobsPage(s.sessionManager, s.dbc))
	s.GET("/jobs/:id", content.HandleJobDetailPage(s.sessionManager, s.dbc))
	s.GET("/videos", content.HandleVideosPage(s.sessionManager, s.dbc))
	s.GET("/videos/:id/cut", content.HandleVideoCutPage(s.sessionManager, s.dbc))
	s.GET("/videos/:id", content.HandleVideoDetailPage(s.sessionManager, s.dbc))
	s.GET("/bookmarklet", content.HandleBookmarklet(s.sessionManager, s.dbc))
	s.GET("/", content.HandleHomePage(s.sessionManager))

	return nil
}
