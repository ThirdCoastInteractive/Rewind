package mcp

import (
	"strings"
	"unicode/utf8"

	"thirdcoast.systems/rewind/internal/analyze"
	"thirdcoast.systems/rewind/internal/db"
)

// CatalogDescMax is the description cap for list_channel_catalog.
const CatalogDescMax = 400

// CatalogItem is one video in a channel catalog listing.
type CatalogItem struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Uploader    string `json:"uploader"`
	Format      string `json:"format"`
	Media       string `json:"media"`
	Description string `json:"description"`
	Published   string `json:"published,omitempty"`
	URI         string `json:"uri"`
	WebPath     string `json:"web_path"`
}

// BuildCatalog turns catalog query rows into MCP JSON. Description is always
// present and truncated to CatalogDescMax runes.
func BuildCatalog(channel string, rows []*db.ListChannelCatalogRow) map[string]any {
	items := make([]CatalogItem, 0, len(rows))
	for _, r := range rows {
		if r == nil {
			continue
		}
		id := uuidString(r.ID)
		item := CatalogItem{
			ID:          id,
			Title:       r.Title,
			Uploader:    r.Uploader,
			Format:      r.Format,
			Media:       r.Media,
			Description: TruncateDescription(r.Description, CatalogDescMax),
			URI:         "rewind://video/" + id,
			WebPath:     "/videos/" + id,
		}
		if r.UploadDate.Valid {
			item.Published = r.UploadDate.Time.Format("2006-01-02")
		}
		items = append(items, item)
	}
	return map[string]any{
		"channel": channel,
		"videos":  items,
	}
}

// TruncateDescription cuts s to max runes and appends an ellipsis when trimmed.
func TruncateDescription(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}

// ChannelAnalysis is the trajectory half of analyze_channel / compare_channels.
type ChannelAnalysis struct {
	Uploader           string                         `json:"uploader"`
	Status             string                         `json:"status"`
	Signals            []analyze.Signal               `json:"signals"`
	Stats              map[string]analyze.FormatStats `json:"stats"`
	OldFormatMix       map[string]float64             `json:"old_format_mix"`
	RecentFormatMix    map[string]float64             `json:"recent_format_mix"`
	AnalyzedVideoCount int                            `json:"analyzed_video_count"`
}

// AnalysisFromReport is the shipped compare/analyze payload for one channel.
func AnalysisFromReport(uploader string, report analyze.Report, videoCount int) ChannelAnalysis {
	return ChannelAnalysis{
		Uploader:           uploader,
		Status:             report.Status,
		Signals:            report.Signals,
		Stats:              report.Stats,
		OldFormatMix:       report.OldFormatMix,
		RecentFormatMix:    report.RecentFormatMix,
		AnalyzedVideoCount: videoCount,
	}
}

// CompareAnalyses pairs two trajectory reports for compare_channels.
func CompareAnalyses(a, b ChannelAnalysis) map[string]any {
	return map[string]any{
		"a": a,
		"b": b,
	}
}
