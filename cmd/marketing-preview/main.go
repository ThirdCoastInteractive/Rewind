// Command marketing-preview serves deterministic Rewind Live screens for sales
// pages, docs, and screenshot capture. It intentionally has no database or
// external service dependencies.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/ctxkeys"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/cmd/web/templates/components"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/filters"
	"thirdcoast.systems/rewind/pkg/videoinfo"
)

const (
	demoUserID    = "00000000-0000-4000-8000-000000000001"
	demoVideoID   = "00000000-0000-4000-8000-000000000101"
	demoClipID    = "00000000-0000-4000-8000-000000000301"
	demoProjectID = "00000000-0000-4000-8000-000000000201"
)

// Config controls the local, read-only preview server.
type Config struct {
	Addr       string
	StaticRoot string
	MediaPath  string
	AssetsDir  string
}

func main() {
	cfg := Config{}
	flag.StringVar(&cfg.Addr, "addr", envOr("MARKETING_PREVIEW_ADDR", ":18082"), "HTTP listen address")
	flag.StringVar(&cfg.StaticRoot, "static-root", envOr("REWIND_STATIC_ROOT", "static"), "directory containing Rewind static assets")
	flag.StringVar(&cfg.MediaPath, "media", envOr("MARKETING_PREVIEW_MEDIA", ""), "optional local MP4 served by demo playback routes")
	flag.StringVar(&cfg.AssetsDir, "assets", envOr("MARKETING_PREVIEW_ASSETS", filepath.Join(os.TempDir(), "rewind-marketing-preview-assets")), "directory for generated preview media assets")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	assets, err := preparePreviewAssets(ctx, cfg.AssetsDir, cfg.MediaPath)
	if err != nil {
		slog.Error("preview media assets are unavailable", "path", cfg.AssetsDir, "error", err)
		os.Exit(1)
	}
	srv := &previewServer{cfg: cfg, assets: assets, now: time.Date(2026, time.March, 20, 15, 4, 0, 0, time.UTC)}
	if _, err := os.Stat(cfg.StaticRoot); err != nil {
		slog.Warn("static asset directory is unavailable; HTML still renders", "path", cfg.StaticRoot, "error", err)
	}

	server := &http.Server{Addr: cfg.Addr, Handler: srv.routes(), ReadHeaderTimeout: 5 * time.Second}
	slog.Info("marketing preview listening", "addr", cfg.Addr, "static_root", cfg.StaticRoot, "media", cfg.MediaPath, "assets", cfg.AssetsDir)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("marketing preview stopped", "error", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

type previewServer struct {
	cfg    Config
	assets *previewAssets
	now    time.Time
}

func (s *previewServer) routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(s.cfg.StaticRoot))))
	mux.HandleFunc("/", s.page)
	mux.HandleFunc("/recordings", s.recordings)
	mux.HandleFunc("/editor", s.editor)
	mux.HandleFunc("/playback", s.playback)
	mux.HandleFunc("/videos/", s.videoRoutes)
	mux.HandleFunc("/stitch", s.stitchLibrary)
	mux.HandleFunc("/stitch/", s.stitchRoutes)
	mux.HandleFunc("/api/", s.api)
	return noWriteMiddleware(mux)
}

func noWriteMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "marketing preview is read-only", http.StatusMethodNotAllowed)
			return
		}
		r = r.WithContext(previewContext(r.Context()))
		next.ServeHTTP(w, r)
	})
}

func (s *previewServer) page(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/recordings", http.StatusFound)
}

func (s *previewServer) recordings(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, templates.Videos(demoVideos(s.now), nil, "", "", "", "demo producer"))
}

func (s *previewServer) editor(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/videos/"+demoVideoID+"/cut", http.StatusFound)
}

func (s *previewServer) renderEditor(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, templates.VideoCutPage(demoVideoDetail(s.now), demoClips(s.now), "demo producer", map[string]string{}))
}

func (s *previewServer) playback(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/videos/"+demoVideoID, http.StatusFound)
}

func (s *previewServer) renderPlayback(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, templates.VideoDetailPage(demoVideoDetail(s.now), demoClips(s.now), "demo producer", map[string]string{}))
}

func (s *previewServer) videoRoutes(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 2 || !isDemoVideoID(parts[1]) {
		http.NotFound(w, r)
		return
	}
	if len(parts) == 2 {
		s.renderPlayback(w, r)
		return
	}
	if len(parts) == 3 && parts[2] == "cut" {
		s.renderEditor(w, r)
		return
	}
	if len(parts) >= 3 && (parts[2] == "stream" || parts[2] == "preview.mp4") {
		s.serveMedia(w, r)
		return
	}
	if len(parts) >= 3 && parts[2] == "thumbnail" {
		s.thumbnail(w, r)
		return
	}
	http.NotFound(w, r)
}

func (s *previewServer) stitchLibrary(w http.ResponseWriter, r *http.Request) {
	view := demoStitchView(s.now)
	s.render(w, r, templates.StitchLibraryPage(view, "demo producer"))
}

func (s *previewServer) stitchRoutes(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != "stitch" || parts[1] != demoProjectID {
		http.NotFound(w, r)
		return
	}
	s.render(w, r, templates.StitchWorkspacePage(demoProjectID, "demo producer", false))
}

func (s *previewServer) api(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	switch {
	case path == "fonts/json":
		s.json(w, map[string]any{"fonts": []any{}})
	case path == "stitch/sources/json":
		s.json(w, map[string]any{"sources": []map[string]any{{"id": demoVideoID, "video_id": demoVideoID, "title": "Product Launch — Live Demo", "duration_us": 312000000}}})
	case path == "stitch/projects/"+demoProjectID+"/document":
		s.json(w, demoDocument())
	case path == "stitch/projects/"+demoProjectID+"/history":
		s.json(w, map[string]any{"history": []any{}})
	case path == "stitch/projects/"+demoProjectID+"/render-jobs":
		s.json(w, map[string]any{"jobs": []any{}})
	case path == "stitch/projects/"+demoProjectID+"/events":
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, ": marketing preview\n\n")
	case path == "videos/index":
		s.videosIndex(w, r)
	case path == "tags":
		s.tags(w, r)
	case strings.HasPrefix(path, "videos/"):
		parts := strings.Split(strings.TrimPrefix(path, "videos/"), "/")
		if len(parts) < 2 || !isDemoVideoID(parts[0]) {
			http.NotFound(w, r)
			return
		}
		s.videoAPI(w, r, parts[0], strings.Join(parts[1:], "/"))
	default:
		http.NotFound(w, r)
	}
}

func (s *previewServer) videosIndex(w http.ResponseWriter, r *http.Request) {
	sse := datastar.NewSSE(w, r)
	if err := sse.PatchElementTempl(templates.VideosGrid(demoVideos(s.now))); err != nil {
		http.Error(w, "video index fixture render failed", http.StatusInternalServerError)
		return
	}
	if err := sse.PatchElementTempl(templates.CatalogCandidates(nil)); err != nil {
		http.Error(w, "catalog fixture render failed", http.StatusInternalServerError)
		return
	}
	_ = sse.PatchElementTempl(components.PaginationControls(components.Pagination{
		CurrentPage: 1,
		TotalPages:  1,
		TotalItems:  6,
		PageSize:    48,
	}))
}

func (s *previewServer) tags(w http.ResponseWriter, r *http.Request) {
	sse := datastar.NewSSE(w, r)
	_ = sse.PatchElementTempl(components.TagFilterBar([]components.TagFilterItem{
		{ID: "tag-demo", Name: "demo", Count: 6},
		{ID: "tag-live", Name: "live", Count: 6},
		{ID: "tag-editing", Name: "editing", Count: 4},
	}))
}

func (s *previewServer) videoAPI(w http.ResponseWriter, r *http.Request, videoID, action string) {
	switch action {
	case "thumbnail":
		s.thumbnail(w, r)
	case "stream", "preview.mp4":
		s.serveMedia(w, r)
	case "seek/seek.json":
		if s.assets == nil {
			http.Error(w, "preview seek assets unavailable", http.StatusNotFound)
			return
		}
		s.serveAsset(w, r, s.assets.seekPath("seek.json"), "application/json")
	case "seek/levels/coarse/seek.vtt", "seek/levels/medium/seek.vtt", "seek/levels/fine/seek.vtt":
		if s.assets == nil {
			http.Error(w, "preview seek assets unavailable", http.StatusNotFound)
			return
		}
		parts := strings.Split(action, "/")
		s.serveAsset(w, r, s.assets.seekPath("levels", parts[2], parts[3]), "text/vtt; charset=utf-8")
	case "waveform/waveform.json":
		if s.assets == nil {
			http.Error(w, "preview waveform assets unavailable", http.StatusNotFound)
			return
		}
		s.serveAsset(w, r, s.assets.waveformPath("waveform.json"), "application/json")
	case "waveform/peaks.i16":
		if s.assets == nil {
			http.Error(w, "preview waveform assets unavailable", http.StatusNotFound)
			return
		}
		s.serveAsset(w, r, s.assets.waveformPath("peaks.i16"), "application/octet-stream")
	case "captions.vtt":
		s.serveAsset(w, r, filepath.Join(s.cfg.AssetsDir, "captions.vtt"), "text/vtt; charset=utf-8")
	case "markers":
		s.json(w, demoMarkerJSON(videoID))
	case "context-windows":
		s.contextWindows(w, r, videoID)
	case "tags/render", "jobs", "markers/render", "comments/render", "transcript/render":
		if err := s.patchVideoFragment(w, r, action); err != nil {
			http.Error(w, "fragment render failed: "+err.Error(), http.StatusInternalServerError)
		}
	case "clips/" + demoClipID + "/select":
		if err := s.patchClipSelection(w, r); err != nil {
			http.Error(w, "clip selection render failed: "+err.Error(), http.StatusInternalServerError)
		}
	case "clips/export-status":
		s.exportStatus(w, r, videoID)
	case "clips":
		s.json(w, demoClips(s.now))
	default:
		if strings.HasPrefix(action, "seek/levels/") {
			if s.assets == nil {
				http.Error(w, "preview seek assets unavailable", http.StatusNotFound)
				return
			}
			parts := strings.Split(action, "/")
			if len(parts) == 4 && strings.HasPrefix(parts[3], "seek-") && strings.HasSuffix(parts[3], ".jpg") {
				s.serveAsset(w, r, s.assets.seekPath("levels", parts[2], parts[3]), "image/jpeg")
				return
			}
		}
		http.NotFound(w, r)
	}
}

func (s *previewServer) patchClipSelection(w http.ResponseWriter, r *http.Request) error {
	clip := demoClips(s.now)[0]
	sse := datastar.NewSSE(w, r)
	if err := sse.PatchElementTempl(
		templates.ClipInspectorForm(clip),
		datastar.WithSelector("[data-cut-clip-form]"),
		datastar.WithModeReplace(),
	); err != nil {
		return err
	}
	signalJSON := fmt.Sprintf("{\"_filterStack\":[],\"_selectedClipId\":\"%s\",\"_clipDirty\":false,\"_clipStartTs\":%.3f,\"_clipEndTs\":%.3f}", clip.ID.String(), clip.StartTs, clip.EndTs)
	if err := sse.PatchSignals([]byte(signalJSON)); err != nil {
		return err
	}
	if err := sse.PatchElementTempl(
		components.FilterCardList(nil, filters.CutFilterConfig(demoVideoID), []filters.FilterOption{{Value: "", Label: "(select crop)"}}),
		datastar.WithSelectorID("filter-stack-list"),
	); err != nil {
		return err
	}
	if err := sse.PatchElementTempl(
		components.CutExportPanel(clip.Crops),
		datastar.WithSelectorID("cut-export-panel"),
	); err != nil {
		return err
	}
	return sse.PatchElementTempl(
		components.MulticamPanel(clip.ID.String(), clip.Crops, clip.ShotList),
		datastar.WithSelectorID("multicam-panel"),
	)
}

func (s *previewServer) patchVideoFragment(w http.ResponseWriter, r *http.Request, action string) error {
	sse := datastar.NewSSE(w, r)
	switch action {
	case "tags/render":
		return sse.PatchElementTempl(
			components.VideoTagEditor(components.TagEditorData{VideoID: demoVideoID, Tags: []components.TagItem{{ID: "tag-demo", Name: "demo"}, {ID: "tag-live", Name: "live"}, {ID: "tag-editing", Name: "editing"}}}),
			datastar.WithSelector("[data-video-tags]"), datastar.WithModeInner(),
		)
	case "transcript/render":
		return sse.PatchElementTempl(
			components.TranscriptList([]components.TranscriptCue{{Start: 0, End: 5.2, Text: "Stream everywhere. Archive everything."}, {Start: 5.2, End: 12.4, Text: "Clip the moments that matter in your browser."}, {Start: 12.4, End: 19.8, Text: "This fictional transcript is part of the product tour."}}),
			datastar.WithSelectorID("transcript-list-inner"),
		)
	case "markers/render":
		return sse.PatchElementTempl(
			components.MarkerList(demoVideoID, []components.MarkerItem{{ID: "marker-intro", Timestamp: 12, Duration: 8, Title: "Opening and promise", Description: "Product introduction"}}),
			datastar.WithSelector("[data-markers-list]"), datastar.WithModeInner(),
		)
	case "comments/render":
		return sse.PatchElementTempl(
			components.CommentSection(components.CommentListData{VideoID: demoVideoID, Comments: []components.CommentItem{}, TotalCount: 0, Page: 0, PageSize: 20}),
			datastar.WithSelector("[data-comments-list]"), datastar.WithModeInner(),
		)
	case "jobs":
		if err := sse.PatchElementTempl(templates.VideoProcessing(demoVideoID, nil, nil, true, 0)); err != nil {
			return err
		}
		return sse.PatchElementTempl(templates.VideoJobsList(nil, map[string][]*db.IngestJob{}), datastar.WithSelectorID("video-jobs-list"), datastar.WithModeInner())
	default:
		return nil
	}
}

func (s *previewServer) render(w http.ResponseWriter, r *http.Request, component interface {
	Render(context.Context, io.Writer) error
}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := component.Render(previewContext(r.Context()), w); err != nil {
		http.Error(w, "render failed: "+err.Error(), http.StatusInternalServerError)
	}
}

func previewContext(ctx context.Context) context.Context {
	ctx = context.WithValue(ctx, ctxkeys.AccessLevel, "user")
	ctx = context.WithValue(ctx, ctxkeys.RegistrationEnabled, false)
	ctx = context.WithValue(ctx, ctxkeys.LiveProduct, true)
	ctx = context.WithValue(ctx, ctxkeys.StaticVersion, "marketing-preview")
	ctx = context.WithValue(ctx, ctxkeys.InterfacePreferences, map[string]any{
		"theme": "mono", "color_mode": "dark", "reduced_motion": true, "sounds_enabled": false,
	})
	return ctx
}

func (s *previewServer) json(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func (s *previewServer) thumbnail(w http.ResponseWriter, r *http.Request) {
	if s.assets == nil {
		http.Error(w, "preview thumbnail unavailable", http.StatusNotFound)
		return
	}
	s.serveAsset(w, r, s.assets.thumbnail(r.URL.Query().Get("w")), "image/jpeg")
}

func (s *previewServer) serveMedia(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(s.cfg.MediaPath)
	if path == "" && s.assets != nil {
		path = s.assets.Source
	}
	if path == "" {
		http.Error(w, "preview media unavailable", http.StatusNotFound)
		return
	}
	path, err := filepath.Abs(path)
	if err != nil {
		http.Error(w, "invalid media path", http.StatusBadRequest)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		http.Error(w, "demo media unavailable", http.StatusNotFound)
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		http.Error(w, "demo media unavailable", http.StatusNotFound)
		return
	}
	http.ServeContent(w, r, filepath.Base(path), stat.ModTime(), file)
}

func (s *previewServer) serveAsset(w http.ResponseWriter, r *http.Request, path, contentType string) {
	if strings.TrimSpace(path) == "" {
		http.Error(w, "preview asset unavailable", http.StatusNotFound)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		http.Error(w, "preview asset unavailable", http.StatusNotFound)
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || stat.Size() == 0 {
		http.Error(w, "preview asset unavailable", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", contentType)
	http.ServeContent(w, r, filepath.Base(path), stat.ModTime(), file)
}

func isDemoVideoID(id string) bool {
	if id == demoVideoID {
		return true
	}
	for i := 2; i <= 6; i++ {
		if id == fmt.Sprintf("00000000-0000-4000-8000-0000000001%02d", i) {
			return true
		}
	}
	return false
}

func (s *previewServer) contextWindows(w http.ResponseWriter, r *http.Request, videoID string) {
	rows := demoContextWindows(videoID)
	if r.URL.Query().Get("render") != "1" {
		s.json(w, rows)
		return
	}
	sse := datastar.NewSSE(w, r)
	if err := sse.PatchElementTempl(templates.WatchContextWindows(videoID, rows, map[string][]templates.TopicChip{}, nil)); err != nil {
		http.Error(w, "context fixture render failed", http.StatusInternalServerError)
	}
}

func (s *previewServer) exportStatus(w http.ResponseWriter, r *http.Request, videoID string) {
	sse := datastar.NewSSE(w, r)
	for _, clip := range demoClips(s.now) {
		clipID := clip.ID.String()
		if err := sse.PatchElementTempl(
			components.ClipExportStatus(clipID, "Ready", "ready", ""),
			datastar.WithSelectorID("clip-export-status-"+clipID),
			datastar.WithModeReplace(),
		); err != nil {
			http.Error(w, "export status fixture render failed", http.StatusInternalServerError)
			return
		}
	}
}

func demoMarkerJSON(videoID string) []map[string]any {
	return []map[string]any{
		{"id": "marker-intro", "video_id": videoID, "timestamp": 12.0, "duration": 8.0, "title": "Opening and promise", "description": "Product introduction", "color": "#d67a4a", "marker_type": "chapter", "source": "archive"},
		{"id": "marker-editor", "video_id": videoID, "timestamp": 188.0, "duration": 14.0, "title": "The editor in action", "description": "Clip, stitch, export", "color": "#55aa83", "marker_type": "chapter", "source": "archive"},
	}
}

func demoContextWindows(videoID string) []*db.ListContextWindowsForVideoRow {
	videoUUID := uuid.MustParse(videoID)
	owner := uuid.MustParse(demoUserID)
	rows := []*db.ListContextWindowsForVideoRow{
		{ID: pgtype.UUID{Bytes: uuid.MustParse("00000000-0000-4000-8000-000000000401"), Valid: true}, VideoID: pgtype.UUID{Bytes: videoUUID, Valid: true}, StartTs: 12, EndTs: 44, Title: "Opening and promise", Summary: "The host introduces the stream-to-editor workflow.", Topics: []string{"live production", "editing"}, Entities: []string{"Rewind Live"}, Origin: "generated", Kind: "window", CreatedBy: pgtype.UUID{Bytes: owner, Valid: true}},
		{ID: pgtype.UUID{Bytes: uuid.MustParse("00000000-0000-4000-8000-000000000402"), Valid: true}, VideoID: pgtype.UUID{Bytes: videoUUID, Valid: true}, StartTs: 78, EndTs: 126, Title: "Studio walkthrough", Summary: "A tour of the browser based cutting tools.", Topics: []string{"studio"}, Entities: []string{"Rewind Live"}, Origin: "generated", Kind: "window", CreatedBy: pgtype.UUID{Bytes: owner, Valid: true}},
	}
	return rows
}

func demoVideos(now time.Time) []*db.ListVideosPaginatedRow {
	colors := [][2]string{{"#19324a", "#7d3d36"}, {"#163c35", "#584a9e"}, {"#392346", "#b26e2f"}, {"#1e3d56", "#927b3d"}, {"#2d2b4d", "#36716b"}, {"#173b42", "#8f4f67"}}
	titles := []string{"Product Launch — Live Demo", "Creator Q&A from Studio", "Behind the Stream: Episode 04", "Community Roundtable", "Patch Notes and Roadmap", "Live Production Field Guide"}
	out := make([]*db.ListVideosPaginatedRow, 0, len(titles))
	for i, title := range titles {
		id := uuid.MustParse(fmt.Sprintf("00000000-0000-4000-8000-0000000001%02d", i+1))
		start, end := colors[i][0], colors[i][1]
		duration := int32(312 - i*31)
		out = append(out, &db.ListVideosPaginatedRow{ID: pgtype.UUID{Bytes: id, Valid: true}, CreatedAt: pgtype.Timestamptz{Time: now.Add(-time.Duration(i+1) * 36 * time.Hour), Valid: true}, Title: title, Uploader: "Rewind Live Studio", Description: "Fictional sample recording for the Rewind Live product tour.", Tags: []string{"demo", "live", "rewind"}, Media: "file", Format: "mp4", DurationSeconds: &duration, CommentCount: int64(4 + i), ThumbGradientStart: &start, ThumbGradientEnd: &end, ThumbGradientAngle: func() *int32 { n := int32(135 + i*7); return &n }(), TotalCount: 6, ClipCount: int64(i + 1), MarkerCount: int64(i + 2)})
	}
	return out
}

func demoVideoDetail(now time.Time) templates.VideoDetail {
	return templates.VideoDetail{ID: demoVideoID, Src: "https://example.invalid/rewind-live-demo", Title: "Product Launch — Live Demo", Description: "A fictional recording used by the Rewind Live sales preview.", Info: videoinfo.VideoInfo{FPS: 30, Width: 1920, Height: 1080, Duration: 312, DurationString: "5:12", Uploader: "Rewind Live Studio", Channel: "Third Coast Demo", Format: "1080p", Container: "mp4", VCodec: "h264", ACodec: "aac", Ext: "mp4", Resolution: "1920x1080", Tags: []string{"demo", "live", "editing"}, LiveStatus: "was_live", WasLive: true}, Media: "file", HasMedia: true, VideoPath: "/demo/rewind-live-sample.mp4", CreatedAt: now.Add(-36 * time.Hour).Format("January 2, 2006 at 3:04 PM"), FileSize: func() *int64 { n := int64(184000000); return &n }(), CommentCount: 4, ActiveRegenScopes: map[string]bool{}}
}

func demoClips(now time.Time) []*db.Clip {
	videoID := uuid.MustParse(demoVideoID)
	owner := uuid.MustParse(demoUserID)
	rows := []struct {
		start, end   float64
		title, color string
	}{{12, 44, "Opening and promise", "#d67a4a"}, {78, 126, "Studio walkthrough", "#5c8fd6"}, {188, 226, "The editor in action", "#55aa83"}}
	out := make([]*db.Clip, 0, len(rows))
	for i, row := range rows {
		id := uuid.MustParse(fmt.Sprintf("00000000-0000-4000-8000-0000000003%02d", i+1))
		out = append(out, &db.Clip{ID: pgtype.UUID{Bytes: id, Valid: true}, VideoID: pgtype.UUID{Bytes: videoID, Valid: true}, CreatedBy: pgtype.UUID{Bytes: owner, Valid: true}, StartTs: row.start, EndTs: row.end, Duration: row.end - row.start, Title: row.title, Color: row.color, CreatedAt: pgtype.Timestamptz{Time: now.Add(-2 * time.Hour), Valid: true}, UpdatedAt: pgtype.Timestamptz{Time: now.Add(-time.Hour), Valid: true}})
	}
	return out
}

func demoStitchView(now time.Time) templates.StitchLibraryView {
	owner := pgtype.UUID{Bytes: uuid.MustParse(demoUserID), Valid: true}
	projectIDs := []uuid.UUID{uuid.MustParse(demoProjectID), uuid.MustParse("00000000-0000-4000-8000-000000000202"), uuid.MustParse("00000000-0000-4000-8000-000000000203")}
	projects := make([]*db.ListStitchProjectsRow, 0, len(projectIDs))
	for i, id := range projectIDs {
		segments := []byte(`[{"id":"seg-1","type":"video","video_id":"00000000-0000-4000-8000-000000000101","start_us":0,"duration_us":42000000},{"id":"seg-2","type":"video","video_id":"00000000-0000-4000-8000-000000000101","start_us":42000000,"duration_us":36000000}]`)
		projects = append(projects, &db.ListStitchProjectsRow{ID: pgtype.UUID{Bytes: id, Valid: true}, Title: []string{"Launch trailer — 60 seconds", "Studio highlights", "Community cut"}[i], Format: "mp4", Quality: "high", Segments: segments, CreatedAt: pgtype.Timestamptz{Time: now.Add(-time.Duration(i+2) * 24 * time.Hour), Valid: true}, UpdatedAt: pgtype.Timestamptz{Time: now.Add(-time.Duration(i+1) * 3 * time.Hour), Valid: true}, CreatedBy: owner, CreatedByName: "demo producer", Description: "Fictional compilation for the Rewind Live product tour.", Tags: []string{"demo", "launch"}})
	}
	return templates.StitchLibraryView{ViewerID: owner, ViewerName: "demo producer", FilterUser: owner, FilterUserName: "demo producer", FolderMode: "all", Projects: projects, Exports: map[string]*db.LatestStitchJobPerProjectRow{projectIDs[0].String(): {ProjectID: pgtype.UUID{Bytes: projectIDs[0], Valid: true}, ID: pgtype.UUID{Bytes: uuid.MustParse("00000000-0000-4000-8000-000000000301"), Valid: true}, Status: db.ExportStatusReady, CreatedAt: pgtype.Timestamptz{Time: now.Add(-2 * time.Hour), Valid: true}}}, CanManage: true}
}

func demoDocument() map[string]any {
	return map[string]any{"revision": 7, "description": "Fictional compilation for the Rewind Live product tour.", "tags": []string{"demo", "launch", "rewind-live"}, "document": map[string]any{"id": demoProjectID, "title": "Launch trailer — 60 seconds", "fps": 30, "segments": []map[string]any{{"id": "seg-1", "type": "video", "video_id": demoVideoID, "source_in_us": 12000000, "start_us": 0, "duration_us": 32000000, "title": "Opening and promise"}, {"id": "seg-2", "type": "video", "video_id": demoVideoID, "source_in_us": 78000000, "start_us": 32000000, "duration_us": 28000000, "title": "Studio walkthrough"}}, "captions": []any{}, "overlays": []any{}}}
}
