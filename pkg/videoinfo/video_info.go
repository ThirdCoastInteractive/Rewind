package videoinfo

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"thirdcoast.systems/rewind/pkg/utils/format"
)

// ============================================================================
// VIDEO INFO - Parsed metadata from yt-dlp info.json
// Uses value types throughout; zero values mean "not present".
// Implements sql.Scanner / driver.Valuer for JSONB column override in sqlc.
// Raw JSON from yt-dlp is preserved through Scan→Value round-trips so that
// fields not modelled in this struct are not lost on write.
// ============================================================================

// VideoInfo contains parsed metadata from yt-dlp info.json.
// All scalar fields use value types: empty string / 0 means absent.
type VideoInfo struct {
	raw json.RawMessage `json:"-"` // preserved for round-trip fidelity

	// Playback
	FPS            float64 `json:"fps"`
	Width          float64 `json:"width"`
	Height         float64 `json:"height"`
	Duration       float64 `json:"duration"`
	DurationString string  `json:"duration_string"`

	// Source / channel
	Uploader             string  `json:"uploader"`
	UploaderURL          string  `json:"uploader_url"`
	UploaderID           string  `json:"uploader_id"`
	Channel              string  `json:"channel"`
	ChannelURL           string  `json:"channel_url"`
	ChannelID            string  `json:"channel_id"`
	ChannelFollowerCount float64 `json:"channel_follower_count"`
	UploadDate           string  `json:"upload_date"`

	// Engagement
	ViewCount    float64 `json:"view_count"`
	LikeCount    float64 `json:"like_count"`
	CommentCount float64 `json:"comment_count"`

	// Classification
	Language     string   `json:"language"`
	Availability string   `json:"availability"`
	AgeLimit     float64  `json:"age_limit"`
	Categories   []string `json:"categories"`
	Tags         []string `json:"tags"`

	// Technical: selected format
	VCodec        string  `json:"vcodec"`
	ACodec        string  `json:"acodec"`
	Container     string  `json:"container"`
	Format        string  `json:"format"`
	FormatNote    string  `json:"format_note"`
	AudioChannels float64 `json:"audio_channels"`
	ASR           float64 `json:"asr"`
	TBR           float64 `json:"tbr"`
	VBR           float64 `json:"vbr"`
	ABR           float64 `json:"abr"`
	DynamicRange  string  `json:"dynamic_range"`
	AspectRatio   float64 `json:"aspect_ratio"`
	Resolution    string  `json:"resolution"`
	Ext           string  `json:"ext"`
	Protocol      string  `json:"protocol"`

	// File
	FilesizeApprox float64 `json:"filesize_approx"`

	// Platform
	Extractor    string `json:"extractor"`
	ExtractorKey string `json:"extractor_key"`
	DisplayID    string `json:"display_id"`
	WebpageURL   string `json:"webpage_url"`
	Domain       string `json:"webpage_url_domain"`

	// Live
	LiveStatus string `json:"live_status"`
	IsLive     bool   `json:"is_live"`
	WasLive    bool   `json:"was_live"`
	MediaType  string `json:"media_type"`

	// Available formats (for multi-track display)
	Formats []FormatInfo `json:"formats"`
}

// FormatInfo represents a single format entry from yt-dlp info.json.
type FormatInfo struct {
	FormatID     string  `json:"format_id"`
	FormatNote   string  `json:"format_note"`
	Ext          string  `json:"ext"`
	ACodec       string  `json:"acodec"`
	VCodec       string  `json:"vcodec"`
	ABR          float64 `json:"abr"`
	VBR          float64 `json:"vbr"`
	ASR          float64 `json:"asr"`
	FPS          float64 `json:"fps"`
	Width        float64 `json:"width"`
	Height       float64 `json:"height"`
	AudioCh      float64 `json:"audio_channels"`
	Language     string  `json:"language"`
	DynamicRange string  `json:"dynamic_range"`
	Resolution   string  `json:"resolution"`
	Protocol     string  `json:"protocol"`
}

// Scan implements sql.Scanner for JSONB columns.
func (v *VideoInfo) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("VideoInfo.Scan: expected []byte, got %T", value)
	}
	v.raw = append(json.RawMessage(nil), b...)
	return json.Unmarshal(b, v)
}

// Value implements driver.Valuer for JSONB columns.
// Returns the preserved raw JSON when available so unmodelled yt-dlp fields are not lost.
func (v VideoInfo) Value() (driver.Value, error) {
	if len(v.raw) > 0 {
		return []byte(v.raw), nil
	}
	return json.Marshal(v)
}

// NewVideoInfo parses raw yt-dlp JSON into a VideoInfo, preserving the
// original bytes for write-back fidelity.
func NewVideoInfo(data []byte) (VideoInfo, error) {
	var v VideoInfo
	v.raw = append(json.RawMessage(nil), data...)
	if err := json.Unmarshal(data, &v); err != nil {
		return v, err
	}
	return v, nil
}

// RawJSON returns the original JSON bytes. Useful for storing in revision
// tables or other contexts that need the full unmodified blob.
func (v VideoInfo) RawJSON() []byte {
	if len(v.raw) > 0 {
		return v.raw
	}
	b, _ := json.Marshal(v)
	return b
}

// HasData returns true if any meaningful field is populated.
func (v VideoInfo) HasData() bool {
	return v.FPS > 0 || v.Width > 0 || v.Height > 0 || v.Duration > 0 ||
		v.DurationString != "" || v.Uploader != "" || v.UploaderURL != "" ||
		v.Channel != "" || v.ChannelURL != "" || v.UploadDate != "" ||
		v.ViewCount > 0 || v.LikeCount > 0 || v.CommentCount > 0 ||
		v.Language != "" || v.Availability != "" || v.AgeLimit > 0 ||
		len(v.Categories) > 0 || len(v.Tags) > 0 ||
		v.VCodec != "" || v.ACodec != "" || v.Container != "" ||
		v.ExtractorKey != "" || v.Domain != "" || v.LiveStatus != "" ||
		v.DynamicRange != "" || v.MediaType != ""
}

// GetFPS returns FPS or 0 if not set.
func (v VideoInfo) GetFPS() float64 {
	if v.FPS > 0 {
		return v.FPS
	}
	return 0
}

// FormatResolution returns e.g. "1920x1080 @ 60fps".
func (v VideoInfo) FormatResolution() string {
	if v.Width <= 0 || v.Height <= 0 {
		return ""
	}
	res := fmt.Sprintf("%dx%d", int(v.Width), int(v.Height))
	if v.FPS > 0 {
		res += fmt.Sprintf(" @ %.0ffps", v.FPS)
	}
	return res
}

// FormatVideoCodec returns the video codec or "".
func (v VideoInfo) FormatVideoCodec() string {
	c := strings.TrimSpace(v.VCodec)
	if c == "" || c == "none" {
		return ""
	}
	return c
}

// FormatAudioCodec returns codec with channel/sample rate info.
func (v VideoInfo) FormatAudioCodec() string {
	c := strings.TrimSpace(v.ACodec)
	if c == "" || c == "none" {
		return ""
	}
	parts := []string{c}
	if v.AudioChannels > 0 {
		ch := int(v.AudioChannels)
		switch ch {
		case 1:
			parts = append(parts, "mono")
		case 2:
			parts = append(parts, "stereo")
		default:
			parts = append(parts, fmt.Sprintf("%dch", ch))
		}
	}
	if v.ASR > 0 {
		parts = append(parts, fmt.Sprintf("%.0f Hz", v.ASR))
	}
	return strings.Join(parts, ", ")
}

// FormatBitrate returns a combined bitrate string.
func (v VideoInfo) FormatBitrate() string {
	var parts []string
	if v.TBR > 0 {
		parts = append(parts, fmt.Sprintf("%.0f kbps total", v.TBR))
	}
	if v.VBR > 0 {
		parts = append(parts, fmt.Sprintf("%.0f kbps video", v.VBR))
	}
	if v.ABR > 0 {
		parts = append(parts, fmt.Sprintf("%.0f kbps audio", v.ABR))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " / ")
}

// FormatLiveStatus returns a display-friendly live status.
func (v VideoInfo) FormatLiveStatus() string {
	switch strings.TrimSpace(v.LiveStatus) {
	case "", "not_live":
		return ""
	case "was_live":
		return "Was Live"
	case "is_live":
		return "Live Now"
	case "is_upcoming":
		return "Upcoming"
	case "post_live":
		return "Post-Live"
	default:
		return strings.TrimSpace(v.LiveStatus)
	}
}

// UniqueAudioLanguages returns deduplicated audio track languages.
func (v VideoInfo) UniqueAudioLanguages() []string {
	seen := map[string]bool{}
	var langs []string
	for _, f := range v.Formats {
		if strings.TrimSpace(f.ACodec) == "" || strings.TrimSpace(f.ACodec) == "none" {
			continue
		}
		if strings.TrimSpace(f.VCodec) != "" && strings.TrimSpace(f.VCodec) != "none" {
			continue // skip muxed
		}
		lang := "unknown"
		if strings.TrimSpace(f.Language) != "" {
			lang = strings.TrimSpace(f.Language)
		}
		if seen[lang] {
			continue
		}
		seen[lang] = true
		display := lang
		note := strings.TrimSpace(f.FormatNote)
		if strings.Contains(note, "original") || strings.Contains(note, "default") {
			display = lang + " *"
		}
		langs = append(langs, display)
	}
	return langs
}

// UniqueVideoFormats returns deduplicated video quality descriptions.
func (v VideoInfo) UniqueVideoFormats() []string {
	seen := map[string]bool{}
	var formats []string
	for _, f := range v.Formats {
		if strings.TrimSpace(f.VCodec) == "" || strings.TrimSpace(f.VCodec) == "none" {
			continue
		}
		if f.Width <= 0 || f.Height <= 0 {
			continue
		}
		res := fmt.Sprintf("%dp", int(f.Height))
		dr := strings.TrimSpace(f.DynamicRange)
		if dr != "" && dr != "SDR" {
			res += " " + dr
		}
		if seen[res] {
			continue
		}
		seen[res] = true
		formats = append(formats, res)
	}
	return formats
}

// QualityChip represents one available quality with download state info.
type QualityChip struct {
	Label      string // e.g. "1080p", "1080p HDR10"
	Downloaded bool   // true if we have this quality in the file
	FormatIDs  string // comma-separated yt-dlp format IDs for download
}

// QualityChips returns all available video qualities with download status.
// It cross-references yt-dlp's available formats with the downloaded video's
// probe data and any additional streams to determine which are already present.
func (v VideoInfo) QualityChips(probe *ProbeInfo, streamHeights []int) []QualityChip {
	// Build a set of downloaded resolutions from probe data.
	downloadedRes := map[string]bool{}
	if probe != nil {
		for _, s := range probe.Streams {
			if s.CodecType != "video" || s.Height <= 0 {
				continue
			}
			label := fmt.Sprintf("%dp", s.Height)
			downloadedRes[label] = true
			if s.ColorTransfer != "" && s.ColorTransfer != "bt709" && s.ColorTransfer != "unknown" {
				hdr := hdrLabel(s.ColorTransfer, s.ColorPrimaries)
				if hdr != "" {
					downloadedRes[label+" "+hdr] = true
				}
			}
		}
	}

	// Also mark heights from additional downloaded stream files as present.
	for _, h := range streamHeights {
		if h > 0 {
			downloadedRes[fmt.Sprintf("%dp", h)] = true
		}
	}

	// Group available formats by label, collecting format IDs.
	type formatGroup struct {
		label     string
		formatIDs []string
	}
	seen := map[string]*formatGroup{}
	var order []string

	for _, f := range v.Formats {
		if strings.TrimSpace(f.VCodec) == "" || strings.TrimSpace(f.VCodec) == "none" {
			continue
		}
		if f.Width <= 0 || f.Height <= 0 {
			continue
		}

		label := fmt.Sprintf("%dp", int(f.Height))
		dr := strings.TrimSpace(f.DynamicRange)
		if dr != "" && dr != "SDR" {
			label += " " + dr
		}

		if g, ok := seen[label]; ok {
			g.formatIDs = append(g.formatIDs, f.FormatID)
		} else {
			g := &formatGroup{label: label, formatIDs: []string{f.FormatID}}
			seen[label] = g
			order = append(order, label)
		}
	}

	chips := make([]QualityChip, 0, len(order))
	for _, label := range order {
		g := seen[label]
		chips = append(chips, QualityChip{
			Label:      label,
			Downloaded: downloadedRes[label],
			FormatIDs:  strings.Join(g.formatIDs, ","),
		})
	}
	return chips
}

// hdrLabel maps ffprobe color_transfer/primaries to the HDR label yt-dlp uses.
func hdrLabel(colorTransfer, colorPrimaries string) string {
	switch {
	case strings.Contains(colorTransfer, "smpte2084") || strings.Contains(colorTransfer, "st2084"):
		if strings.Contains(colorPrimaries, "bt2020") {
			return "HDR10"
		}
		return "HDR"
	case strings.Contains(colorTransfer, "arib-std-b67") || strings.Contains(colorTransfer, "hlg"):
		return "HLG"
	case strings.Contains(colorTransfer, "smpte428"):
		return "HDR"
	default:
		return ""
	}
}

// SourceInfoRows returns label→value pairs for the Source column.
func (v VideoInfo) SourceInfoRows() []InfoPair {
	var rows []InfoPair
	add := func(label, value string) {
		if value != "" {
			rows = append(rows, InfoPair{Label: label, Value: value})
		}
	}
	add("Platform", strings.TrimSpace(v.ExtractorKey))
	add("Domain", strings.TrimSpace(v.Domain))
	add("Video ID", strings.TrimSpace(v.DisplayID))
	add("Uploader", strings.TrimSpace(v.Uploader))
	ch := strings.TrimSpace(v.Channel)
	if ch != "" && ch != strings.TrimSpace(v.Uploader) {
		add("Channel", ch)
	}
	if v.ChannelFollowerCount > 0 {
		add("Subscribers", format.Number(int(v.ChannelFollowerCount)))
	}
	if v.UploadDate != "" {
		add("Upload Date", FormatUploadDate(v.UploadDate))
	}
	if v.ViewCount > 0 {
		add("Views", format.Number(int(v.ViewCount)))
	}
	if v.LikeCount > 0 {
		add("Likes", format.Number(int(v.LikeCount)))
	}
	if v.CommentCount > 0 {
		add("Comments", format.Number(int(v.CommentCount)))
	}
	return rows
}

// SourceLinkRows returns label→(url, displayText) pairs for linked rows.
func (v VideoInfo) SourceLinkRows() []InfoLinkPair {
	var rows []InfoLinkPair
	addLink := func(label, url string) {
		u := strings.TrimSpace(url)
		if u != "" {
			rows = append(rows, InfoLinkPair{Label: label, URL: u, Display: TruncateURL(u)})
		}
	}
	addLink("Uploader URL", v.UploaderURL)
	addLink("Channel URL", v.ChannelURL)
	return rows
}

// ClassificationInfoRows returns label→value pairs for the Classification column.
func (v VideoInfo) ClassificationInfoRows() []InfoPair {
	var rows []InfoPair
	add := func(label, value string) {
		if value != "" {
			rows = append(rows, InfoPair{Label: label, Value: value})
		}
	}
	add("Language", strings.TrimSpace(v.Language))
	add("Media Type", strings.TrimSpace(v.MediaType))
	add("Live Status", v.FormatLiveStatus())
	add("Availability", strings.TrimSpace(v.Availability))
	if v.AgeLimit > 0 {
		add("Age Limit", fmt.Sprintf("%d+", int(v.AgeLimit)))
	}
	if cats := strings.Join(v.Categories, ", "); cats != "" {
		add("Categories", cats)
	}
	return rows
}

// TechnicalInfoRows returns label→value pairs for the fallback technical column
// (used when ffprobe data is not available).
func (v VideoInfo) TechnicalInfoRows(fileSize *int64) []InfoPair {
	var rows []InfoPair
	add := func(label, value string) {
		if value != "" {
			rows = append(rows, InfoPair{Label: label, Value: value})
		}
	}
	if v.DurationString != "" {
		add("Duration", strings.TrimSpace(v.DurationString))
	} else if v.Duration > 0 {
		add("Duration", format.Duration(v.Duration))
	}
	add("Resolution", v.FormatResolution())
	add("Video Codec", v.FormatVideoCodec())
	add("Audio Codec", v.FormatAudioCodec())
	add("Bitrate", v.FormatBitrate())
	if strings.TrimSpace(v.Container) != "" {
		add("Container", strings.TrimSpace(v.Container))
	} else if strings.TrimSpace(v.Ext) != "" {
		add("Format", strings.TrimSpace(v.Ext))
	}
	if fileSize != nil && *fileSize > 0 {
		add("File Size", format.Bytes(*fileSize))
	} else if v.FilesizeApprox > 0 {
		add("File Size", "~"+format.Bytes(int64(v.FilesizeApprox)))
	}
	return rows
}
