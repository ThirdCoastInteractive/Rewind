package main

import (
	"encoding/json"
	"strings"
)

type ytdlpInfo struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	WebpageURL   string `json:"webpage_url"`
	OriginalURL  string `json:"original_url"`
	Extractor    string `json:"extractor"`
	ExtractorKey string `json:"extractor_key"`
	CommentCount *int64 `json:"comment_count"`
}

type normalizedInfo struct {
	Description     string
	Tags            []string
	Uploader        string
	UploaderID      *string
	ChannelID       *string
	UploadDate      *string
	DurationSeconds *int32
	ViewCount       *int64
	LikeCount       *int64
}

func normalizeInfo(raw []byte) normalizedInfo {
	out := normalizedInfo{Description: "", Tags: []string{}, Uploader: ""}

	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return out
	}

	if v, ok := m["description"].(string); ok {
		out.Description = strings.TrimSpace(v)
	}
	if v, ok := m["uploader"].(string); ok {
		out.Uploader = strings.TrimSpace(v)
	}

	if v, ok := m["uploader_id"].(string); ok {
		v = strings.TrimSpace(v)
		if v != "" {
			out.UploaderID = &v
		}
	}
	if v, ok := m["channel_id"].(string); ok {
		v = strings.TrimSpace(v)
		if v != "" {
			out.ChannelID = &v
		}
	}
	if v, ok := m["upload_date"].(string); ok {
		v = strings.TrimSpace(v)
		if v != "" {
			out.UploadDate = &v
		}
	}

	if arr, ok := m["tags"].([]any); ok {
		out.Tags = out.Tags[:0]
		for _, it := range arr {
			s, ok := it.(string)
			if !ok {
				continue
			}
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			out.Tags = append(out.Tags, s)
		}
	}

	if v, ok := m["duration"].(float64); ok {
		if v > 0 {
			d := int32(v)
			out.DurationSeconds = &d
		}
	}
	if v, ok := m["view_count"].(float64); ok {
		c := int64(v)
		out.ViewCount = &c
	}
	if v, ok := m["like_count"].(float64); ok {
		c := int64(v)
		out.LikeCount = &c
	}

	return out
}
