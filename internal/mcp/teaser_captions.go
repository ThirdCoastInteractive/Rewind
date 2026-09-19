package mcp

import (
	"context"
	"fmt"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/stitch"
)

type styleTeaserCaptionsArgs struct {
	ProjectID        string                      `json:"project_id"`
	Revision         *int64                      `json:"revision"`
	ExpectedRevision *int64                      `json:"expected_revision,omitempty"`
	OperationKey     string                      `json:"operation_key"`
	SegmentID        string                      `json:"segment_id,omitempty"`
	Language         string                      `json:"language,omitempty"`
	ImportCaptions   bool                        `json:"import_captions,omitempty"`
	CaptionIDs       []string                    `json:"caption_ids,omitempty"`
	Style            string                      `json:"style,omitempty"`
	MaxCharsPerLine  int                         `json:"max_chars_per_line,omitempty"`
	MaxLines         int                         `json:"max_lines,omitempty"`
	MaxPhraseChars   int                         `json:"max_phrase_chars,omitempty"`
	SafeMargin       float64                     `json:"safe_margin,omitempty"`
	Margins          stitch.TeaserCaptionMargins `json:"margins,omitempty"`
}

// registerTeaserCaptionTools registers the bounded caption styling workflow.
func registerTeaserCaptionTools(srv *mcpsdk.Server, dbc *db.DatabaseConnection) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "style_teaser_captions",
		Description: "Format owned Stitch captions for a 9:16 teaser. Optionally imports archive captions, wraps readable phrases, applies basic/bold/outlined/karaoke styling, preserves source links, and reports alignment/preview follow-ups. Requires mcp:write.",
	}, styleTeaserCaptionsMCP(dbc))
}

func styleTeaserCaptionsMCP(dbc *db.DatabaseConnection) func(context.Context, *mcpsdk.CallToolRequest, *styleTeaserCaptionsArgs) (*mcpsdk.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *styleTeaserCaptionsArgs) (*mcpsdk.CallToolResult, any, error) {
		if err := requireWrite(ctx); err != nil {
			return nil, nil, err
		}
		if a == nil {
			return nil, nil, fmt.Errorf("project_id, revision, and operation_key are required")
		}
		revision := a.Revision
		if revision == nil {
			revision = a.ExpectedRevision
		}
		if revision == nil || *revision < 0 {
			return nil, nil, fmt.Errorf("revision is required")
		}
		expectedRevision := *revision
		key := strings.TrimSpace(a.OperationKey)
		if len(key) < 1 || len(key) > 180 {
			return nil, nil, fmt.Errorf("operation_key must be 1-180 characters")
		}
		owner := tokenFrom(ctx)
		if owner == nil {
			return nil, nil, fmt.Errorf("authentication required")
		}
		project, err := parseUUID(a.ProjectID)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid project_id: %w", err)
		}
		margins := a.Margins
		if a.SafeMargin != 0 {
			margins = stitch.TeaserCaptionMargins{Left: a.SafeMargin, Right: a.SafeMargin, Top: a.SafeMargin, Bottom: a.SafeMargin}
		}
		store := stitch.NewStore(dbc)
		actorInfo := ActorFrom(ctx)
		actor := stitch.Actor{Kind: string(actorInfo.Kind), ID: actorInfo.ID, Name: actorInfo.ClientName}
		selected := append([]string(nil), a.CaptionIDs...)
		lang := strings.TrimSpace(a.Language)
		if a.ImportCaptions {
			if strings.TrimSpace(a.SegmentID) == "" {
				return nil, nil, fmt.Errorf("segment_id is required when import_captions is true")
			}
			if lang == "" {
				lang = "en"
			}
		}
		options := stitch.TeaserCaptionOptions{
			CaptionIDs: selected, Style: a.Style, MaxCharsPerLine: a.MaxCharsPerLine,
			MaxLines: a.MaxLines, MaxPhraseChars: a.MaxPhraseChars, Margins: margins,
		}
		if err := stitch.ValidateTeaserCaptionOptions(options); err != nil {
			return nil, nil, err
		}
		importSegment := ""
		importLanguage := ""
		if a.ImportCaptions {
			importSegment = a.SegmentID
			importLanguage = lang
		}
		operation, err := stitch.NewTeaserCaptionOperation(options, importSegment, importLanguage)
		if err != nil {
			return nil, nil, err
		}
		committed, err := store.Commit(ctx, owner.UserID, project, expectedRevision, key, actor, "Style teaser captions", []stitch.Operation{operation})
		if err != nil {
			return nil, nil, err
		}
		changed := map[string]bool{}
		for _, id := range committed.ChangedIDs {
			changed[id] = true
		}
		reportIDs := []string{}
		wanted := map[string]bool{}
		for _, id := range selected {
			wanted[id] = true
		}
		for _, c := range committed.Document.Captions {
			include := len(selected) == 0 || wanted[c.ID] || changed[c.ID]
			if a.ImportCaptions && len(selected) == 0 {
				include = c.SegmentID == a.SegmentID && c.Language == lang
			}
			if include {
				reportIDs = append(reportIDs, c.ID)
			}
		}
		formatted, err := stitch.TeaserCaptionReport(committed.Document, options, reportIDs)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(teaserCaptionResponse(a.ProjectID, key, committed, formatted, committed.Document))
	}
}

func teaserCaptionResponse(projectID, key string, result stitch.Result, formatted stitch.TeaserCaptionResult, d stitch.Document) map[string]any {
	response := map[string]any{
		"project_id":    projectID,
		"revision":      result.Revision,
		"operation_key": key,
		"result":        result,
		"captions":      formatted.Captions,
		"sources":       formatted.Sources,
		"warnings":      formatted.Warnings,
		"alignment":     formatted.Alignment,
		"safe_margins":  formatted.Margins,
		"next_calls":    teaserCaptionNextCalls(projectID, result, formatted, d),
	}
	return response
}

func teaserCaptionNextCalls(projectID string, result stitch.Result, formatted stitch.TeaserCaptionResult, d stitch.Document) []map[string]any {
	next := []map[string]any{}
	for id, state := range formatted.Alignment {
		if state == "valid" {
			continue
		}
		lang := "en"
		for _, c := range d.Captions {
			if c.ID == id && strings.TrimSpace(c.Language) != "" {
				lang = c.Language
				break
			}
		}
		next = append(next, map[string]any{
			"tool":      "request_stitch_alignment",
			"arguments": map[string]any{"project_id": projectID, "caption_id": id, "revision": result.Revision, "operation_key": fmt.Sprintf("align-%s-%d", id, result.Revision), "language": lang},
		})
	}
	var duration int64
	for _, s := range d.Segments {
		if x := s.StartUS + s.DurationUS; x > duration {
			duration = x
		}
	}
	end := duration
	if end > 5_000_000 {
		end = 5_000_000
	}
	if end <= 0 {
		return next
	}
	next = append(next, map[string]any{
		"tool":      "stitch_preview",
		"arguments": map[string]any{"project_id": projectID, "revision": result.Revision, "operation_key": "preview-" + result.EditID.String(), "format": "mp4", "quality": "high", "caption_mode": "burn", "scope": "range", "start_us": int64(0), "end_us": end},
	})
	return next
}
