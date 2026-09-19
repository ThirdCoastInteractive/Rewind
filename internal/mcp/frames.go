package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/frames"
	"thirdcoast.systems/rewind/pkg/ffmpeg"
)

type frameArgs struct {
	VideoID    string    `json:"video_id"`
	Timestamps []float64 `json:"timestamps_seconds"`
	Quality    string    `json:"quality,omitempty"`
	MaxWidth   int       `json:"max_width,omitempty"`
}
type sheetArgs struct {
	VideoID string   `json:"video_id"`
	Start   *float64 `json:"start_seconds,omitempty"`
	End     *float64 `json:"end_seconds,omitempty"`
	Count   int      `json:"count,omitempty"`
	Quality string   `json:"quality,omitempty"`
}

func frameAsset(ctx context.Context, dbc *db.DatabaseConnection, id string) (frames.Asset, error) {
	u, e := optionalUUID(id)
	if e != nil || !u.Valid {
		return frames.Asset{}, fmt.Errorf("video_id is required")
	}
	v, e := dbc.Queries(ctx).GetVideoByID(ctx, u)
	if e != nil {
		return frames.Asset{}, e
	}
	path := ""
	if v.VideoPath != nil {
		path = *v.VideoPath
	}
	duration := 0.0
	if v.DurationSeconds != nil {
		duration = float64(*v.DurationSeconds)
	}
	asset, err := frames.Resolve(id, path, duration)
	if err != nil {
		return asset, err
	}
	if duration <= 0 && asset.File != "" {
		probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		probed, probeErr := ffmpeg.ProbeDuration(probeCtx, asset.File)
		cancel()
		if probeErr == nil && probed > 0 && probed < math.MaxInt32 {
			asset.Duration = probed
			seconds := int32(math.Ceil(probed))
			if err := dbc.Queries(ctx).RepairVideoDuration(ctx, &db.RepairVideoDurationParams{ID: u, DurationSeconds: &seconds}); err != nil {
				return asset, err
			}
		}
	}
	return asset, nil
}
func registerFrameTools(srv *mcpsdk.Server, dbc *db.DatabaseConnection) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "get_video_frames", Description: "Inspect 1–8 timestamped JPEG frames. Detail decodes the full archived frame; preview crops existing seek sprites. Returns actual images and timestamp provenance. Never downloads media."}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *frameArgs) (*mcpsdk.CallToolResult, any, error) {
		if len(a.Timestamps) < 1 || len(a.Timestamps) > 8 {
			return nil, nil, fmt.Errorf("request 1–8 timestamps")
		}
		asset, e := frameAsset(ctx, dbc, a.VideoID)
		if e != nil {
			return nil, nil, e
		}
		fs, e := frames.Default.Get(ctx, asset, a.Timestamps, a.Quality, a.MaxWidth)
		if e != nil {
			return nil, nil, e
		}
		return frameResult(fs, nil)
	})
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "get_video_contact_sheet", Description: "Inspect a numbered contact sheet across a video range, with up to 24 cells and per-cell timestamp provenance. Defaults to 12 seek previews."}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, a *sheetArgs) (*mcpsdk.CallToolResult, any, error) {
		asset, e := frameAsset(ctx, dbc, a.VideoID)
		if e != nil {
			return nil, nil, e
		}
		start, end := 0.0, asset.Duration
		if a.Start != nil {
			start = *a.Start
		}
		if a.End != nil {
			end = *a.End
		}
		if end <= start {
			return nil, nil, fmt.Errorf("a positive range is required")
		}
		n := a.Count
		if n == 0 {
			n = 12
		}
		if n < 1 || n > 24 {
			return nil, nil, fmt.Errorf("count must be 1–24")
		}
		q := a.Quality
		if q == "" {
			q = "preview"
		}
		ts := make([]float64, n)
		for i := range ts {
			ts[i] = start + (float64(i)+0.5)*(end-start)/float64(n)
		}
		fs, e := frames.Default.Get(ctx, asset, ts, q, 960)
		if e != nil {
			return nil, nil, e
		}
		raw, cells, e := frames.ContactSheet(fs, q == "detail")
		if e != nil {
			return nil, nil, e
		}
		return frameResult(cells, raw)
	})
}
func frameResult(fs []frames.Frame, sheet []byte) (*mcpsdk.CallToolResult, any, error) {
	content := []mcpsdk.Content{&mcpsdk.TextContent{}}
	used := 0
	success := 0
	const rawBudget = (6 << 20) * 3 / 4 // MCP JSON base64 expands bytes by four thirds.
	if len(sheet) > 0 && len(sheet) <= rawBudget {
		content = append(content, &mcpsdk.ImageContent{MIMEType: "image/jpeg", Data: sheet})
		for _, f := range fs {
			if f.Error == "" {
				success++
			}
		}
	} else {
		for i := range fs {
			f := &fs[i]
			if f.Error != "" {
				continue
			}
			if used+len(f.JPEG) > rawBudget {
				f.Error = "image payload budget exceeded"
				continue
			}
			if len(f.JPEG) == 0 {
				f.Error = "image unavailable"
				continue
			}
			f.ContentIndex = len(content)
			content = append(content, &mcpsdk.ImageContent{MIMEType: "image/jpeg", Data: f.JPEG})
			used += len(f.JPEG)
			success++
		}
	}
	out := map[string]any{"frames": fs, "returned": success}
	raw, e := json.Marshal(out)
	if e != nil {
		return nil, nil, e
	}
	content[0] = &mcpsdk.TextContent{Text: string(raw)}
	return &mcpsdk.CallToolResult{Content: content, IsError: success == 0}, out, nil
}
