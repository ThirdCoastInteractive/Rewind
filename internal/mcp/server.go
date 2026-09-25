// Package mcp serves Rewind's archive as a Model Context Protocol server
// (Streamable HTTP) so users can connect their own agents.
package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	xlanguage "golang.org/x/text/language"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/jsnum"
	"thirdcoast.systems/rewind/internal/search"
	"thirdcoast.systems/rewind/internal/wiki"
	"thirdcoast.systems/rewind/pkg/captions"
	"thirdcoast.systems/rewind/pkg/plugin"
	"thirdcoast.systems/rewind/pkg/utils/language"
)

// Handler returns an http.Handler for POST/GET /mcp.
func Handler(ctx context.Context, dbc *db.DatabaseConnection) http.Handler {
	srv := newServer(dbc)
	startShowNoteResourceNotifications(ctx, dbc, srv)
	inner := mcpsdk.NewStreamableHTTPHandler(func(r *http.Request) *mcpsdk.Server { return srv }, &mcpsdk.StreamableHTTPOptions{
		// Zero means never; Grok and other clients often skip DELETE on /mcp.
		SessionTimeout: 30 * time.Minute,
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, err := Authenticate(r.Context(), dbc, r.Header.Get("Authorization"))
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		sid := strings.TrimSpace(r.Header.Get("Mcp-Session-Id"))
		if sid == "" {
			sid = uuid.NewString()
		}
		reqCtx := withToken(r.Context(), tok)
		reqCtx = withSession(reqCtx, sessionInfo{ID: sid, ClientName: "mcp"})
		var tenant string
		if authn := plugin.Auth(); authn != nil {
			if scoped, ok := authn.(interface {
				WorkspaceForUser(context.Context, string) (string, error)
			}); ok {
				tenant, err = scoped.WorkspaceForUser(reqCtx, tok.UserID.String())
				if err != nil {
					http.Error(w, "workspace unavailable", http.StatusForbidden)
					return
				}
			}
		}
		if plugin.LiveIngest() != nil && strings.TrimSpace(tenant) == "" {
			http.Error(w, "workspace required", http.StatusForbidden)
			return
		}
		if plugin.LiveIngest() != nil {
			reqCtx = plugin.WithTenantScope(reqCtx, tenant, true)
		}
		inner.ServeHTTP(w, r.WithContext(reqCtx))
	})
}

func newServer(dbc *db.DatabaseConnection) *mcpsdk.Server {
	options := &mcpsdk.ServerOptions{
		Instructions: clippingInstructions + " For short teasers, Shorts, TikToks, Reels, or vertical clips, start with get_shortform_workflow.",
		GetSessionID: uuid.NewString,
		SubscribeHandler: func(ctx context.Context, request *mcpsdk.SubscribeRequest) error {
			if request.Params == nil {
				return fmt.Errorf("show-note subscription URI is required")
			}
			return authorizeShowNoteSubscription(ctx, dbc, request.Params.URI)
		},
		UnsubscribeHandler: func(context.Context, *mcpsdk.UnsubscribeRequest) error {
			return nil
		},
	}
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "rewind",
		Version: "1.0.0",
	}, options)
	srv.AddReceivingMiddleware(sessionIdentityMiddleware)
	if plugin.LiveIngest() != nil {
		srv.AddReceivingMiddleware(liveMCPAllowlistMiddleware(liveMCPToolAllowlist()))
	}
	registerOSINTTools(srv, dbc)
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "whoami",
		Description: "Return the authenticated MCP actor, token, and session identity.",
	}, whoami)
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "search_library",
		Description: "Search archived videos by title, uploader, tags, comments, transcripts, and context windows. Filter with sources (e.g. transcript+context_windows for talked-about). Returns citation objects.",
	}, searchLibrary(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "get_video",
		Description: "Get metadata for one archived video by UUID.",
	}, getVideo(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "search_transcripts",
		Description: "Search cleaned video transcripts and return timestamped cue hits. Nearby hits in the same Context Window collapse into one candidate.",
	}, searchTranscriptsV2(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "get_transcript",
		Description: "Return the cleaned transcript for a video, optionally a time range in seconds.",
	}, getTranscript(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "get_context_windows", Description: "List chapter Context Windows intersecting an optional video range. Each window includes nested shorts (8-45s punchy moments) when generated."}, getContextWindows(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "suggest_clip_boundaries", Description: "Suggest three silence-aware, frame-snapped candidates for each proposed edge."}, suggestClipBoundaries(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "get_compilation_plan", Description: "Return a persisted compilation plan, segments, media readiness, and render state."}, getCompilationPlan(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "list_compilation_plans", Description: "List compilation plans owned by the authenticated user."}, listCompilationPlans(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "library_stats",
		Description: "Counts and storage totals for this Rewind library.",
	}, libraryStats(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "resolve_uri",
		Description: "Resolve a rewind://video/{id} URI or video UUID to a citation.",
	}, resolveURI(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "get_related",
		Description: "Same-channel videos, clips, markers, neighbor channels, and other-channel context windows that share a canonical topic.",
	}, getRelated(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "get_channel_graph",
		Description: "Channel relationship graph as nodes and edges.",
	}, getChannelGraph(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "list_channels",
		Description: "List archived channels (uploaders), optionally filtered by name.",
	}, listChannels(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "get_channel",
		Description: "Get one channel by UUID or uploader name, including stats, creator, and harvested outlink/mention edges.",
	}, getChannel(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "list_creators",
		Description: "List creators, optionally filtered by name.",
	}, listCreators(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "get_creator",
		Description: "Get a creator and their linked channels.",
	}, getCreator(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "analyze_creator",
		Description: "Public-data decline analysis for a creator's archived videos.",
	}, analyzeCreator(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "analyze_channel",
		Description: "Public-data decline analysis for one channel (UUID or uploader name): views/day, cadence, format mix, engagement vs historical baseline, plus harvested edges.",
	}, analyzeChannel(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "list_channel_videos",
		Description: "Paginated videos for a channel (UUID or uploader). Includes media=file vs metadata.",
	}, listChannelVideos(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "list_channel_catalog",
		Description: "Recent titles and truncated descriptions for a channel (UUID or uploader), including metadata-only rows.",
	}, listChannelCatalog(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "compare_channels",
		Description: "Side-by-side decline analysis of two channels (UUID or uploader names).",
	}, compareChannels(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "list_creator_suggestions",
		Description: "Pending creator-link nominations (accept/dismiss from the Creators page).",
	}, listCreatorSuggestions(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "get_channel_neighborhood",
		Description: "1-hop in/out edges around a channel, including unresolved URLs. Optional kind: outlink, mention, commented.",
	}, getChannelNeighborhood(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "analyze_follows",
		Description: "Run decline analysis on every enabled follow. Returns status and top signals, dying first.",
	}, analyzeFollows(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "index_channel_descriptions",
		Description: "Enqueue a metadata-only crawl of a channel (titles/descriptions, no media). Requires mcp:write. Pass a channel UUID or uploader name.",
	}, indexChannelDescriptions(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "index_channel_catalog", Description: "Index or refresh the complete channel catalog, subtitles included. Requires mcp:write."}, indexChannelCatalogMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "index_creator_catalog", Description: "Index or refresh every channel linked to a creator. Requires mcp:write."}, indexCreatorCatalogMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "index_url",
		Description: "Index a YouTube video, playlist, channel, channel search, or results URL as titles + English captions without downloading media. Requires mcp:write.",
	}, indexURL(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "get_index_status",
		Description: "Poll a caption-index job from index_url, index_channel_catalog, or index_creator_catalog. Do not use get_transcription_status for these IDs.",
	}, getIndexStatus(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "create_context_window", Description: "Create a sparse, overlap-safe Context Window. Requires mcp:write."}, createContextWindowMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "update_context_window", Description: "Update Context Window bounds or metadata. Requires mcp:write."}, updateContextWindowMCP(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "save_compilation_plan", Description: "Persist a creator/query compilation plan without rendering it. Requires mcp:write."}, saveCompilationPlan(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "update_compilation_plan", Description: "Replace a persisted plan's ordered segments and create a new revision. Requires mcp:write."}, updateCompilationPlan(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "create_compilation", Description: "Idempotently archive missing sources and render a ready persisted plan. Requires mcp:write."}, createCompilation(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "suggest_channel_links",
		Description: "Candidate pairs of unassigned channels with similar uploader names.",
	}, suggestChannelLinks(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "search_comments",
		Description: "Search comments on one video, one uploader, or the whole library.",
	}, searchComments(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "list_clips",
		Description: "List clips for a video UUID.",
	}, listClips(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "list_markers",
		Description: "List markers for a video UUID.",
	}, listMarkers(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "list_follows",
		Description: "List watched (followed) channels.",
	}, listFollows(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "enqueue_download",
		Description: "Enqueue a URL for archival. Requires mcp:write.",
	}, enqueueDownload(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "follow_channel",
		Description: "Watch a channel or playlist URL. Requires mcp:write.",
	}, followChannel(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "unfollow_channel",
		Description: "Stop watching a channel URL or watch id. Requires mcp:write.",
	}, unfollowChannel(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "create_creator",
		Description: "Create a creator record. Requires mcp:write.",
	}, createCreator(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "link_channel_to_creator",
		Description: "Attach a channel row to a creator. Requires mcp:write.",
	}, linkChannelToCreator(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "unlink_channel",
		Description: "Detach a channel from its creator. Requires mcp:write.",
	}, unlinkChannel(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "accept_creator_suggestion",
		Description: "Accept a pending creator-link nomination. Requires mcp:write.",
	}, acceptCreatorSuggestion(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "dismiss_creator_suggestion",
		Description: "Dismiss a pending creator-link nomination. Requires mcp:write.",
	}, dismissCreatorSuggestion(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "add_tag",
		Description: "Add a tag to a video by name. Requires mcp:write.",
	}, addTag(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "refresh_video_metadata",
		Description: "Re-enqueue an archived video source to refresh metadata. Requires mcp:write.",
	}, refreshVideoMetadata(dbc))
	registerShowNoteMCP(srv, dbc)
	registerFrameTools(srv, dbc)
	registerVisionTools(srv, dbc)
	registerMLTools(srv, dbc)
	registerSettingsTools(srv, dbc)
	registerClippingTools(srv, dbc)
	registerStitchEditorTools(srv, dbc)
	registerStitchEditorRenderTools(srv, dbc)
	registerStitchAlignmentTools(srv, dbc)
	registerTeaserTools(srv, dbc)
	registerTeaserCaptionTools(srv, dbc)
	registerWikiTools(srv, dbc)
	SetWikiStore(newWikiStoreAdapter(wiki.New(dbc)))

	srv.AddPrompt(&mcpsdk.Prompt{
		Name:        "find_quote",
		Description: "Find a spoken quote in archived transcripts and cite it.",
		Arguments: []*mcpsdk.PromptArgument{
			{Name: "query", Description: "Words or phrase to find in transcripts", Required: true},
			{Name: "uploader", Description: "Optional uploader to narrow the search"},
		},
	}, findQuotePrompt)
	srv.AddPrompt(&mcpsdk.Prompt{
		Name:        "analyze_channel",
		Description: "Analyze an archived channel's trajectory and relationships.",
		Arguments: []*mcpsdk.PromptArgument{
			{Name: "channel", Description: "Uploader name or channel UUID", Required: true},
		},
	}, analyzeChannelPrompt)
	srv.AddPrompt(&mcpsdk.Prompt{
		Name:        "propose_rundown",
		Description: "Search evidence, inspect frames, propose a show-note patch, and wait for human review before compiling.",
		Arguments: []*mcpsdk.PromptArgument{
			{Name: "topic", Description: "What the rundown should cover", Required: true},
		},
	}, proposeRundownPrompt)

	return srv
}

// liveMCPToolAllowlist and its middleware keep Live limited to stores that
// enforce workspace scope. OSS retains the complete archive and OSINT surface.
func liveMCPToolAllowlist() map[string]struct{} {
	allowed := map[string]struct{}{
		"whoami":      {},
		"wiki_search": {}, "wiki_get": {}, "wiki_pages_for": {}, "list_topic_windows": {},
		"wiki_put": {}, "wiki_history": {}, "wiki_diff": {},
		"list_show_notes": {}, "get_show_note": {}, "join_show_note_room": {},
		"wait_show_note_events": {}, "leave_show_note_room": {}, "post_show_note_message": {},
		"add_show_note_comment": {}, "propose_show_note_patch": {},
		"stitch_inspect": {}, "stitch_apply": {}, "stitch_history": {}, "stitch_undo": {},
		"stitch_redo": {}, "stitch_wait": {}, "stitch_export": {}, "stitch_preview": {},
		"stitch_frame": {}, "stitch_render_status": {},
		"request_stitch_alignment": {}, "get_stitch_alignment_status": {},
		"get_clipping_workflow": {}, "find_clip_candidates": {}, "create_clip": {},
		"save_compilation_plan": {}, "append_compilation_segments": {},
		"create_stitch_project": {}, "get_compilation_plan": {}, "list_compilation_plans": {},
		"get_transcript": {}, "get_context_windows": {},
	}
	return allowed
}

func liveMCPAllowlistMiddleware(allowed map[string]struct{}) mcpsdk.Middleware {
	return func(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
		return func(ctx context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
			if method == "tools/call" {
				if call, ok := req.(*mcpsdk.CallToolRequest); ok {
					if _, permitted := allowed[call.Params.Name]; !permitted {
						return nil, fmt.Errorf("Live MCP tool is not available: %s", call.Params.Name)
					}
				}
			}
			result, err := next(ctx, method, req)
			if method == "tools/list" && err == nil {
				if listed, ok := result.(*mcpsdk.ListToolsResult); ok {
					filtered := listed.Tools[:0]
					for _, tool := range listed.Tools {
						if _, permitted := allowed[tool.Name]; permitted {
							filtered = append(filtered, tool)
						}
					}
					listed.Tools = filtered
				}
			}
			return result, err
		}
	}
}

func startShowNoteResourceNotifications(ctx context.Context, dbc *db.DatabaseConnection, srv *mcpsdk.Server) {
	go db.RunListenLoop(ctx, dbc, []string{"show_note_room_events"}, func(n *pgconn.Notification) {
		uri := "rewind://show-note/" + n.Payload
		if err := srv.ResourceUpdated(ctx, &mcpsdk.ResourceUpdatedNotificationParams{URI: uri}); err != nil {
			slog.Warn("MCP show-note resource notification failed", "uri", uri, "error", err)
		}
	})
}

type ctxKey struct{}

func withToken(ctx context.Context, tok *db.APIToken) context.Context {
	return context.WithValue(ctx, ctxKey{}, tok)
}

func tokenFrom(ctx context.Context) *db.APIToken {
	tok, _ := ctx.Value(ctxKey{}).(*db.APIToken)
	return tok
}

func requireWrite(ctx context.Context) error {
	tok := tokenFrom(ctx)
	if tok == nil {
		return fmt.Errorf("mcp:write scope required")
	}
	for _, s := range tok.Scopes {
		if s == "mcp:write" {
			return nil
		}
	}
	return fmt.Errorf("mcp:write scope required")
}

// HashToken returns the hex SHA-256 of a bearer secret.
func HashToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// Authenticate validates an Authorization header. Accepts both
// `Bearer rw_…` and a bare `rw_…` secret (Grok's mcp add often stores the latter).
func Authenticate(ctx context.Context, dbc *db.DatabaseConnection, header string) (*db.APIToken, error) {
	plain := strings.TrimSpace(header)
	if len(plain) >= 7 && strings.EqualFold(plain[:7], "bearer ") {
		plain = strings.TrimSpace(plain[7:])
	}
	if plain == "" {
		return nil, fmt.Errorf("missing bearer token")
	}
	row, err := dbc.Queries(ctx).GetAPITokenByHash(ctx, HashToken(plain))
	if err != nil || row == nil {
		return nil, fmt.Errorf("invalid token")
	}
	user, err := dbc.Queries(ctx).SelectUserByID(ctx, row.UserID)
	if err != nil || user == nil || !user.Enabled {
		return nil, fmt.Errorf("account is unavailable")
	}
	_ = dbc.Queries(ctx).TouchAPIToken(ctx, row.ID)
	return row, nil
}

type citation struct {
	Kind      string   `json:"kind"`
	ID        string   `json:"id,omitempty"`
	URI       string   `json:"uri,omitempty"`
	WebPath   string   `json:"web_path,omitempty"`
	Title     string   `json:"title,omitempty"`
	Quote     string   `json:"quote,omitempty"`
	Start     *float64 `json:"start,omitempty"`
	End       *float64 `json:"end,omitempty"`
	VideoID   string   `json:"video_id,omitempty"`
	Uploader  string   `json:"uploader,omitempty"`
	MatchFrom string   `json:"match_from,omitempty"`
}

func jsonResult(v any) (*mcpsdk.CallToolResult, any, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: string(b)}},
	}, nil, nil
}

func uuidString(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return uuid.UUID(id.Bytes).String()
}

type searchArgs struct {
	Query          string   `json:"query" jsonschema:"Search text"`
	Uploader       string   `json:"uploader,omitempty" jsonschema:"Optional uploader substring"`
	CreatorID      string   `json:"creator_id,omitempty"`
	ChannelID      string   `json:"channel_id,omitempty"`
	Sources        []string `json:"sources,omitempty"`
	Limit          int32    `json:"limit,omitempty" jsonschema:"Max results, default 25"`
	IncludeCatalog *bool    `json:"include_catalog,omitempty"`
}

func searchLibrary(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *searchArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *searchArgs) (*mcpsdk.CallToolResult, any, error) {
		if args.Limit <= 0 || args.Limit > 100 {
			args.Limit = 25
		}
		compiled := search.Compile(args.Query)
		for i, source := range args.Sources {
			args.Sources[i] = strings.ToLower(strings.TrimSpace(source))
		}
		creatorID, err := optionalUUID(args.CreatorID)
		if err != nil {
			return nil, nil, err
		}
		channelID, err := optionalUUID(args.ChannelID)
		if err != nil {
			return nil, nil, err
		}
		rows, err := dbc.Queries(ctx).ListVideosPaginated(ctx, &db.ListVideosPaginatedParams{
			Sources:      args.Sources,
			Query:        nullable(compiled.Raw),
			Tsquery:      nullable(compiled.TSQuery),
			Uploader:     nullable(strings.TrimSpace(args.Uploader)),
			CreatorID:    creatorID,
			ChannelRowID: channelID,
			SortOrder:    "relevance",
			PageOffset:   0,
			PageLimit:    args.Limit,
		})
		if err != nil {
			return nil, nil, err
		}
		ids := make([]pgtype.UUID, 0, len(rows))
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
		evidence, err := dbc.Queries(ctx).GetLibraryEvidence(ctx, &db.GetLibraryEvidenceParams{VideoIds: ids, Tsquery: compiled.TSQuery})
		if err != nil {
			return nil, nil, err
		}
		evidenceByVideo := map[pgtype.UUID]*db.GetLibraryEvidenceRow{}
		for _, e := range evidence {
			evidenceByVideo[e.ID] = e
		}
		windows, err := dbc.Queries(ctx).ListContextWindowsForVideos(ctx, ids)
		if err != nil {
			return nil, nil, err
		}
		windowsByVideo := map[pgtype.UUID][]*db.ListContextWindowsForVideosRow{}
		for _, w := range windows {
			windowsByVideo[w.VideoID] = append(windowsByVideo[w.VideoID], w)
		}
		out := make([]map[string]any, 0, len(rows))
		for _, r := range rows {
			id := uuidString(r.ID)
			matches, snippets, timestamp := sourceEvidence(r, compiled, evidenceByVideo[r.ID], windowsByVideo[r.ID])
			if !sourceSelectionMatches(matches, args.Sources) {
				continue
			}
			webPath := "/videos/" + id
			if timestamp != nil {
				webPath = fmt.Sprintf("/videos/%s?t=%.3f", id, *timestamp)
			}
			out = append(out, map[string]any{"kind": "video", "id": id, "uri": "rewind://video/" + id, "web_path": webPath, "title": r.Title, "uploader": r.Uploader, "matched_sources": matches, "snippets": snippets, "transcript_timestamp": timestamp, "media_status": "playable"})
		}
		includeCatalog := args.IncludeCatalog == nil || *args.IncludeCatalog
		catalogRows := []*db.ListCatalogCandidatesRow{}
		if includeCatalog && compiled.TSQuery != "" {
			catalogRows, err = dbc.Queries(ctx).ListCatalogCandidates(ctx, &db.ListCatalogCandidatesParams{Uploader: nullable(strings.TrimSpace(args.Uploader)), Sources: args.Sources, Tsquery: nullable(compiled.TSQuery), CreatorID: creatorID, ChannelRowID: channelID, PageLimit: args.Limit})
			if err != nil {
				return nil, nil, err
			}
		}
		catalogOut := make([]map[string]any, 0, len(catalogRows))
		for _, r := range catalogRows {
			catalogOut = append(catalogOut, map[string]any{"id": uuidString(r.ID), "title": r.Title, "uploader": r.Uploader, "src": r.Src, "media_status": "catalog-only", "matched_sources": map[string]bool{"metadata": r.MetadataMatch, "comments": r.CommentMatch, "transcript": r.TranscriptMatch, "context_windows": r.ContextWindowMatch}})
		}
		totalPlayable := int64(0)
		if len(rows) > 0 {
			totalPlayable = rows[0].TotalCount
		}
		totalCatalog := int64(0)
		if len(catalogRows) > 0 {
			totalCatalog = catalogRows[0].TotalCount
		}
		return jsonResult(map[string]any{"total_playable": totalPlayable, "total_catalog": totalCatalog, "results": out, "catalog_candidates": catalogOut})
	}
}

func sourceEvidence(r *db.ListVideosPaginatedRow, compiled search.Result, evidence *db.GetLibraryEvidenceRow, windows []*db.ListContextWindowsForVideosRow) (map[string]bool, map[string]string, *float64) {
	matches := map[string]bool{"title": r.SearchMatchTitle, "uploader": r.SearchMatchUploader, "description": r.SearchMatchDescription, "tags": r.SearchMatchTags, "comments": r.SearchMatchComment, "transcript": r.SearchMatchTranscript, "context_windows": r.SearchMatchContextWindow}
	snippets := map[string]string{}
	if matches["title"] {
		snippets["title"] = r.Title
	}
	if matches["uploader"] {
		snippets["uploader"] = r.Uploader
	}
	if matches["description"] {
		snippets["description"] = snippet(r.Description, compiled.Raw)
	}
	if matches["tags"] {
		snippets["tags"] = strings.Join(r.Tags, ", ")
	}
	var timestamp *float64
	if evidence != nil {
		if matches["comments"] {
			snippets["comments"] = snippet(evidence.CommentText, compiled.Raw)
		}
		var cues []captions.Cue
		if matches["transcript"] && json.Unmarshal(evidence.Cues, &cues) == nil {
			for i, cue := range cues {
				if text := matchedCueEvidence(cues, i, compiled); text != "" {
					snippets["transcript"] = text
					t := cue.Start
					timestamp = &t
					break
				}
			}
		}
	}
	if matches["context_windows"] {
		for _, cw := range windows {
			text := strings.Join([]string{cw.Title, cw.Summary, strings.Join(cw.Topics, " "), strings.Join(cw.Entities, " ")}, " ")
			if compiled.Match(text) {
				snippets["context_windows"] = strings.TrimSpace(cw.Title + ": " + cw.Summary)
				if timestamp == nil {
					t := cw.StartTs
					timestamp = &t
				}
				break
			}
		}
	}
	return matches, snippets, timestamp
}

func sourceSelectionMatches(matches map[string]bool, sources []string) bool {
	if len(sources) == 0 {
		return true
	}
	for _, source := range sources {
		if matches[strings.ToLower(strings.TrimSpace(source))] {
			return true
		}
	}
	return false
}

type idArgs struct {
	ID string `json:"id" jsonschema:"Resource UUID for this tool"`
}

func getVideo(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *idArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *idArgs) (*mcpsdk.CallToolResult, any, error) {
		id, err := parseUUID(args.ID)
		if err != nil {
			return nil, nil, err
		}
		v, err := dbc.Queries(ctx).GetVideoByID(ctx, id)
		if err != nil || v == nil {
			return nil, nil, fmt.Errorf("video not found")
		}
		sid := uuidString(v.ID)
		if v.DurationSeconds == nil || *v.DurationSeconds <= 0 {
			if asset, e := frameAsset(ctx, dbc, sid); e == nil && asset.Duration > 0 {
				seconds := int32(asset.Duration)
				v.DurationSeconds = &seconds
			}
		}
		return jsonResult(map[string]any{
			"id":               sid,
			"uri":              "rewind://video/" + sid,
			"web_path":         "/videos/" + sid,
			"title":            v.Title,
			"uploader":         v.Uploader,
			"description":      v.Description,
			"src":              v.Src,
			"duration_seconds": v.DurationSeconds,
			"view_count":       v.ViewCount,
			"tags":             v.Tags,
			"format":           v.Format,
		})
	}
}

type getTranscriptArgs struct {
	ID       string   `json:"id" jsonschema:"Video UUID"`
	Language string   `json:"language,omitempty" jsonschema:"Language from a clip candidate; omit to prefer English"`
	Start    *jsnum.F `json:"start,omitempty"`
	End      *jsnum.F `json:"end,omitempty"`
}

func getTranscript(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *getTranscriptArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *getTranscriptArgs) (*mcpsdk.CallToolResult, any, error) {
		id, err := parseUUID(args.ID)
		if err != nil {
			return nil, nil, err
		}
		if err = requireWorkspaceVideo(ctx, dbc.Queries(ctx), id); err != nil {
			return nil, nil, err
		}
		var row *db.VideoTranscript
		if args.Language == "" {
			row, err = dbc.Queries(ctx).GetVideoTranscript(ctx, id)
		} else {
			var lang language.Tag
			if err = lang.Scan(args.Language); err != nil {
				return nil, nil, err
			}
			row, err = dbc.Queries(ctx).GetVideoTranscriptByLanguage(ctx, &db.GetVideoTranscriptByLanguageParams{VideoID: id, Lang: lang})
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, err
		}
		if errors.Is(err, pgx.ErrNoRows) || row == nil {
			if _, videoErr := dbc.Queries(ctx).GetVideoByID(ctx, id); videoErr != nil {
				return nil, nil, videoErr
			}
			coverage, coverageErr := dbc.Queries(ctx).ListTranscriptCoverage(ctx, id)
			if coverageErr != nil {
				return nil, nil, coverageErr
			}
			jobs, jobsErr := dbc.Queries(ctx).ListMLJobsForVideo(ctx, id)
			if jobsErr != nil {
				return nil, nil, jobsErr
			}
			pending := []map[string]any{}
			for _, j := range jobs {
				if j.Kind == "transcribe" {
					pending = append(pending, map[string]any{"job_id": uuidString(j.ID), "status": j.Status, "start": j.RangeStart, "end": j.RangeEnd})
				}
			}
			return jsonResult(map[string]any{"video_id": args.ID, "status": "transcript_unavailable", "transcripts": coverageInfo(coverage), "transcription_jobs": pending, "next_tool": "enqueue_transcribe", "next_action": "Choose an available language, inspect an existing job, or enqueue transcription. For a known moment supply absolute start/end."})
		}
		var cues []captions.Cue
		_ = json.Unmarshal(row.Cues, &cues)
		if args.Start != nil || args.End != nil {
			lo, hi := 0.0, 1e12
			if args.Start != nil {
				lo = float64(*args.Start)
			}
			if args.End != nil {
				hi = float64(*args.End)
			}
			filtered := make([]captions.Cue, 0, len(cues))
			for _, c := range cues {
				if c.End >= lo && c.Start <= hi {
					filtered = append(filtered, c)
				}
			}
			cues = filtered
		}
		sid := uuidString(row.VideoID)
		return jsonResult(map[string]any{
			"video_id":        sid,
			"uri":             "rewind://video/" + sid + "/transcript",
			"lang":            xlanguage.Tag(row.Lang).String(),
			"text":            captions.PlainText(cues),
			"cues":            cues,
			"complete":        len(row.Coverage) == 0,
			"coverage_ranges": json.RawMessage(row.Coverage),
		})
	}
}

func libraryStats(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *struct{}) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ *struct{}) (*mcpsdk.CallToolResult, any, error) {
		row, err := dbc.Queries(ctx).GetHomeStats(ctx)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(row)
	}
}

func resolveURI(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *idArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcpsdk.CallToolRequest, args *idArgs) (*mcpsdk.CallToolResult, any, error) {
		raw := strings.TrimSpace(args.ID)
		raw = strings.TrimPrefix(raw, "rewind://video/")
		raw = strings.TrimSuffix(raw, "/transcript")
		if i := strings.IndexByte(raw, '/'); i >= 0 {
			raw = raw[:i]
		}
		args.ID = raw
		return getVideo(dbc)(ctx, req, args)
	}
}

func parseUUID(s string) (pgtype.UUID, error) {
	var id pgtype.UUID
	u, err := uuid.Parse(strings.TrimSpace(s))
	if err != nil {
		return id, fmt.Errorf("invalid uuid")
	}
	id.Bytes = u
	id.Valid = true
	return id, nil
}

func nullable(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

func snippet(text, q string) string {
	low := strings.ToLower(text)
	qi := strings.ToLower(strings.TrimSpace(q))
	i := strings.Index(low, qi)
	if i < 0 {
		if len(text) > 180 {
			return text[:180] + "…"
		}
		return text
	}
	start := i - 40
	if start < 0 {
		start = 0
	}
	end := i + len(qi) + 80
	if end > len(text) {
		end = len(text)
	}
	return strings.TrimSpace(text[start:end])
}
