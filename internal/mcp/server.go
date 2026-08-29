// Package mcp serves Rewind's archive as a Model Context Protocol server
// (Streamable HTTP) so users can connect their own agents.
package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/search"
	"thirdcoast.systems/rewind/pkg/captions"
)

// Handler returns an http.Handler for POST/GET /mcp.
func Handler(dbc *db.DatabaseConnection) http.Handler {
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "rewind",
		Version: "1.0.0",
	}, nil)
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "search_library",
		Description: "Search archived videos by title, uploader, tags, comments, and transcripts. Returns citation objects.",
	}, searchLibrary(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "get_video",
		Description: "Get metadata for one archived video by UUID.",
	}, getVideo(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "search_transcripts",
		Description: "Search cleaned video transcripts and return timestamped cue hits.",
	}, searchTranscripts(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "get_transcript",
		Description: "Return the cleaned transcript for a video, optionally a time range in seconds.",
	}, getTranscript(dbc))
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
		Description: "Same-channel videos, clips, markers, and neighbor channels for a video UUID.",
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
		Description: "Get one channel by UUID or uploader name.",
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
		Name:        "suggest_channel_links",
		Description: "Candidate pairs of unassigned channels with similar uploader names.",
	}, suggestChannelLinks(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "search_comments",
		Description: "Search comments on an archived video.",
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
		Name:        "add_tag",
		Description: "Add a tag to a video by name. Requires mcp:write.",
	}, addTag(dbc))
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "refresh_video_metadata",
		Description: "Re-enqueue an archived video source to refresh metadata. Requires mcp:write.",
	}, refreshVideoMetadata(dbc))

	srv.AddPrompt(&mcpsdk.Prompt{
		Name:        "find_quote",
		Description: "Find a spoken quote in archived transcripts and cite it.",
		Arguments: []*mcpsdk.PromptArgument{
			{Name: "query", Description: "Words or phrase to find in transcripts", Required: true},
			{Name: "uploader", Description: "Optional uploader to narrow the search"},
		},
	}, findQuotePrompt)

	inner := mcpsdk.NewStreamableHTTPHandler(func(r *http.Request) *mcpsdk.Server {
		return srv
	}, nil)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, err := Authenticate(r.Context(), dbc, r.Header.Get("Authorization"))
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		r = r.WithContext(withToken(r.Context(), tok))
		inner.ServeHTTP(w, r)
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
	Query    string `json:"query" jsonschema:"Search text"`
	Uploader string `json:"uploader,omitempty" jsonschema:"Optional uploader substring"`
	Limit    int32  `json:"limit,omitempty" jsonschema:"Max results, default 20"`
}

func searchLibrary(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *searchArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *searchArgs) (*mcpsdk.CallToolResult, any, error) {
		if args.Limit <= 0 || args.Limit > 50 {
			args.Limit = 20
		}
		compiled := search.Compile(args.Query)
		rows, err := dbc.Queries(ctx).ListVideosPaginated(ctx, &db.ListVideosPaginatedParams{
			Query:      nullable(compiled.Raw),
			Tsquery:    nullable(compiled.TSQuery),
			Uploader:   nullable(strings.TrimSpace(args.Uploader)),
			SortOrder:  "relevance",
			PageOffset: 0,
			PageLimit:  args.Limit,
		})
		if err != nil {
			return nil, nil, err
		}
		out := make([]citation, 0, len(rows))
		for _, r := range rows {
			id := uuidString(r.ID)
			out = append(out, citation{
				Kind:     "video",
				ID:       id,
				URI:      "rewind://video/" + id,
				WebPath:  "/videos/" + id,
				Title:    r.Title,
				Uploader: r.Uploader,
			})
		}
		return jsonResult(out)
	}
}

type idArgs struct {
	ID string `json:"id" jsonschema:"Video UUID"`
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

type transcriptSearchArgs struct {
	Query string `json:"query" jsonschema:"Phrase or words to find in transcripts"`
	Limit int32  `json:"limit,omitempty"`
}

func searchTranscripts(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *transcriptSearchArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *transcriptSearchArgs) (*mcpsdk.CallToolResult, any, error) {
		compiled := search.Compile(args.Query)
		if compiled.TSQuery == "" {
			return jsonResult([]citation{})
		}
		if args.Limit <= 0 || args.Limit > 50 {
			args.Limit = 20
		}
		rows, err := dbc.Queries(ctx).SearchTranscripts(ctx, &db.SearchTranscriptsParams{
			Tsquery:   compiled.TSQuery,
			PageLimit: args.Limit,
		})
		if err != nil {
			return nil, nil, err
		}
		q := strings.ToLower(args.Query)
		out := []citation{}
		for _, r := range rows {
			vid := uuidString(r.VideoID)
			var cues []captions.Cue
			_ = json.Unmarshal(r.Cues, &cues)
			matched := false
			for _, cue := range cues {
				if q != "" && strings.Contains(strings.ToLower(cue.Text), q) {
					st, en := cue.Start, cue.End
					out = append(out, citation{
						Kind:     "cue",
						ID:       vid,
						URI:      "rewind://video/" + vid,
						WebPath:  fmt.Sprintf("/videos/%s?t=%.0f", vid, cue.Start),
						Title:    r.Title,
						Quote:    cue.Text,
						Start:    &st,
						End:      &en,
						VideoID:  vid,
						Uploader: r.Uploader,
					})
					matched = true
					if len(out) >= int(args.Limit) {
						break
					}
				}
			}
			if !matched {
				out = append(out, citation{
					Kind:     "video",
					ID:       vid,
					URI:      "rewind://video/" + vid + "/transcript",
					WebPath:  "/videos/" + vid,
					Title:    r.Title,
					Uploader: r.Uploader,
					Quote:    snippet(r.Text, args.Query),
				})
			}
			if len(out) >= int(args.Limit) {
				break
			}
		}
		return jsonResult(out)
	}
}

type getTranscriptArgs struct {
	ID    string   `json:"id" jsonschema:"Video UUID"`
	Start *float64 `json:"start,omitempty"`
	End   *float64 `json:"end,omitempty"`
}

func getTranscript(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *getTranscriptArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, args *getTranscriptArgs) (*mcpsdk.CallToolResult, any, error) {
		id, err := parseUUID(args.ID)
		if err != nil {
			return nil, nil, err
		}
		row, err := dbc.Queries(ctx).GetVideoTranscript(ctx, id)
		if err != nil || row == nil {
			return nil, nil, fmt.Errorf("no transcript")
		}
		var cues []captions.Cue
		_ = json.Unmarshal(row.Cues, &cues)
		if args.Start != nil || args.End != nil {
			lo, hi := 0.0, 1e12
			if args.Start != nil {
				lo = *args.Start
			}
			if args.End != nil {
				hi = *args.End
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
			"video_id": sid,
			"uri":      "rewind://video/" + sid + "/transcript",
			"lang":     row.Lang,
			"text":     captions.PlainText(cues),
			"cues":     cues,
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
