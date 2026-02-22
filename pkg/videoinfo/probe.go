package videoinfo

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"thirdcoast.systems/rewind/pkg/utils/format"
)

// ============================================================================
// PROBE INFO - Parsed ffprobe output
// Implements sql.Scanner / driver.Valuer for JSONB column override in sqlc.
// ============================================================================

// ProbeInfo is the parsed ffprobe output.
type ProbeInfo struct {
	raw     json.RawMessage `json:"-"`
	Streams []ProbeStream   `json:"streams"`
	Format  ProbeFormat     `json:"format"`
}

// ProbeStream represents a single stream from ffprobe output.
type ProbeStream struct {
	Index          int               `json:"index"`
	CodecType      string            `json:"codec_type"`
	CodecName      string            `json:"codec_name"`
	CodecLongName  string            `json:"codec_long_name"`
	Profile        string            `json:"profile"`
	Width          int               `json:"width"`
	Height         int               `json:"height"`
	CodedWidth     int               `json:"coded_width"`
	CodedHeight    int               `json:"coded_height"`
	PixFmt         string            `json:"pix_fmt"`
	Level          int               `json:"level"`
	ColorRange     string            `json:"color_range"`
	ColorSpace     string            `json:"color_space"`
	ColorTransfer  string            `json:"color_transfer"`
	ColorPrimaries string            `json:"color_primaries"`
	RFrameRate     string            `json:"r_frame_rate"`
	AvgFrameRate   string            `json:"avg_frame_rate"`
	SampleRate     string            `json:"sample_rate"`
	Channels       int               `json:"channels"`
	ChannelLayout  string            `json:"channel_layout"`
	BitsPerSample  int               `json:"bits_per_sample"`
	BitRate        string            `json:"bit_rate"`
	Duration       string            `json:"duration"`
	NbFrames       string            `json:"nb_frames"`
	Tags           map[string]string `json:"tags"`
	Disposition    map[string]int    `json:"disposition"`
	SideDataList   []map[string]any  `json:"side_data_list"`
}

// ProbeFormat represents ffprobe format-level metadata.
type ProbeFormat struct {
	Filename       string            `json:"filename"`
	NbStreams      int               `json:"nb_streams"`
	NbPrograms     int               `json:"nb_programs"`
	FormatName     string            `json:"format_name"`
	FormatLongName string            `json:"format_long_name"`
	Duration       string            `json:"duration"`
	Size           string            `json:"size"`
	BitRate        string            `json:"bit_rate"`
	Tags           map[string]string `json:"tags"`
}

// Scan implements sql.Scanner for JSONB columns.
func (p *ProbeInfo) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("ProbeInfo.Scan: expected []byte, got %T", value)
	}
	p.raw = append(json.RawMessage(nil), b...)
	return json.Unmarshal(b, p)
}

// Value implements driver.Valuer for JSONB columns.
func (p ProbeInfo) Value() (driver.Value, error) {
	if len(p.raw) > 0 {
		return []byte(p.raw), nil
	}
	return json.Marshal(p)
}

// NewProbeInfo parses raw ffprobe JSON into a ProbeInfo, preserving the
// original bytes for write-back fidelity.
func NewProbeInfo(data []byte) *ProbeInfo {
	if len(data) == 0 {
		return nil
	}
	var p ProbeInfo
	p.raw = append(json.RawMessage(nil), data...)
	if err := json.Unmarshal(data, &p); err != nil {
		return nil
	}
	if len(p.Streams) == 0 {
		return nil
	}
	return &p
}

// RawJSON returns the original JSON bytes.
func (p ProbeInfo) RawJSON() []byte {
	if len(p.raw) > 0 {
		return p.raw
	}
	b, _ := json.Marshal(p)
	return b
}

// ParseProbeData parses raw ffprobe JSON bytes into a ProbeInfo.
// Deprecated: Use NewProbeInfo or sqlc column override instead.
func ParseProbeData(data []byte) *ProbeInfo {
	return NewProbeInfo(data)
}

// VideoStreams returns all video-type streams.
func (p *ProbeInfo) VideoStreams() []ProbeStream {
	var out []ProbeStream
	for _, s := range p.Streams {
		if s.CodecType == "video" {
			out = append(out, s)
		}
	}
	return out
}

// AudioStreams returns all audio-type streams.
func (p *ProbeInfo) AudioStreams() []ProbeStream {
	var out []ProbeStream
	for _, s := range p.Streams {
		if s.CodecType == "audio" {
			out = append(out, s)
		}
	}
	return out
}

// SubtitleStreams returns all subtitle-type streams.
func (p *ProbeInfo) SubtitleStreams() []ProbeStream {
	var out []ProbeStream
	for _, s := range p.Streams {
		if s.CodecType == "subtitle" {
			out = append(out, s)
		}
	}
	return out
}

// StreamLanguage returns the language tag for a stream, or "".
func (s ProbeStream) StreamLanguage() string {
	if lang, ok := s.Tags["language"]; ok && strings.TrimSpace(lang) != "" && lang != "und" {
		return strings.TrimSpace(lang)
	}
	return ""
}

// StreamTitle returns the title tag for a stream.
func (s ProbeStream) StreamTitle() string {
	if t, ok := s.Tags["title"]; ok && strings.TrimSpace(t) != "" {
		return strings.TrimSpace(t)
	}
	return ""
}

// IsDefault returns whether this stream has the default disposition flag.
func (s ProbeStream) IsDefault() bool {
	return s.Disposition["default"] == 1
}

// FormatFrameRate parses r_frame_rate (e.g. "30000/1001") into a display string.
func (s ProbeStream) FormatFrameRate() string {
	parts := strings.Split(s.RFrameRate, "/")
	if len(parts) != 2 {
		return s.RFrameRate
	}
	var num, den float64
	fmt.Sscanf(parts[0], "%f", &num)
	fmt.Sscanf(parts[1], "%f", &den)
	if den == 0 {
		return s.RFrameRate
	}
	fps := num / den
	if fps == float64(int(fps)) {
		return fmt.Sprintf("%d fps", int(fps))
	}
	return fmt.Sprintf("%.3f fps", fps)
}

// FormatBitrateStream formats a stream bitrate to human-readable.
func (s ProbeStream) FormatBitrateStream() string {
	if s.BitRate == "" {
		return ""
	}
	var bps float64
	fmt.Sscanf(s.BitRate, "%f", &bps)
	if bps <= 0 {
		return s.BitRate
	}
	if bps >= 1000000 {
		return fmt.Sprintf("%.1f Mbps", bps/1000000)
	}
	return fmt.Sprintf("%.0f kbps", bps/1000)
}

// FormatSampleRate formats a sample rate with kHz suffix.
func (s ProbeStream) FormatSampleRate() string {
	if s.SampleRate == "" {
		return ""
	}
	var sr float64
	fmt.Sscanf(s.SampleRate, "%f", &sr)
	if sr <= 0 {
		return s.SampleRate
	}
	return fmt.Sprintf("%.1f kHz", sr/1000)
}

// FormatCodecDisplay returns a human-readable codec string.
func (s ProbeStream) FormatCodecDisplay() string {
	if s.CodecLongName != "" && s.CodecName != "" && s.CodecLongName != s.CodecName {
		return fmt.Sprintf("%s (%s)", s.CodecName, s.CodecLongName)
	}
	if s.CodecName != "" {
		return s.CodecName
	}
	return "unknown"
}

// StreamPropertyRows returns the key detail rows for this stream type,
// suitable for rendering in a column-per-stream table.
func (s ProbeStream) StreamPropertyRows() []InfoPair {
	var rows []InfoPair
	add := func(label, value string) {
		if value != "" {
			rows = append(rows, InfoPair{Label: label, Value: value})
		}
	}

	add("Codec", s.CodecName)
	add("Profile", s.Profile)

	switch s.CodecType {
	case "video":
		if s.Width > 0 && s.Height > 0 {
			add("Resolution", fmt.Sprintf("%dx%d", s.Width, s.Height))
		}
		if s.CodedWidth > 0 && s.CodedHeight > 0 && (s.CodedWidth != s.Width || s.CodedHeight != s.Height) {
			add("Coded Size", fmt.Sprintf("%dx%d", s.CodedWidth, s.CodedHeight))
		}
		add("Pixel Format", s.PixFmt)
		if fps := s.FormatFrameRate(); fps != "" && fps != "0/0" && fps != "0 fps" {
			add("Frame Rate", fps)
		}
		add("Color Space", s.ColorSpace)
		add("Color Transfer", s.ColorTransfer)
		add("Color Primaries", s.ColorPrimaries)
		add("Color Range", s.ColorRange)
		if s.Level > 0 {
			add("Level", fmt.Sprintf("%d", s.Level))
		}
	case "audio":
		if s.Channels > 0 {
			if s.ChannelLayout != "" {
				add("Channels", fmt.Sprintf("%d (%s)", s.Channels, s.ChannelLayout))
			} else {
				add("Channels", fmt.Sprintf("%d", s.Channels))
			}
		}
		if sr := s.FormatSampleRate(); sr != "" {
			add("Sample Rate", sr)
		}
		if s.BitsPerSample > 0 {
			add("Bit Depth", fmt.Sprintf("%d-bit", s.BitsPerSample))
		}
	}

	add("Bitrate", s.FormatBitrateStream())
	if s.NbFrames != "" && s.NbFrames != "N/A" {
		add("Frames", s.NbFrames)
	}
	add("Language", s.StreamLanguage())
	add("Title", s.StreamTitle())

	return rows
}

// TechnicalInfoRows returns label→value pairs for the Technical column
// when ffprobe data IS available.
func (p *ProbeInfo) TechnicalInfoRows(info VideoInfo, fileSize *int64) []InfoPair {
	var rows []InfoPair
	add := func(label, value string) {
		if value != "" {
			rows = append(rows, InfoPair{Label: label, Value: value})
		}
	}

	// Duration
	if p.Format.Duration != "" {
		add("Duration", FormatProbeDuration(p.Format.Duration))
	} else if info.DurationString != "" {
		add("Duration", strings.TrimSpace(info.DurationString))
	}

	// Container
	if p.Format.FormatLongName != "" {
		add("Container", p.Format.FormatLongName)
	} else if p.Format.FormatName != "" {
		add("Container", p.Format.FormatName)
	}

	// Total bitrate
	if p.Format.BitRate != "" {
		add("Bitrate", FormatProbeBitrate(p.Format.BitRate))
	}

	// File size
	if fileSize != nil && *fileSize > 0 {
		add("File Size", format.Bytes(*fileSize))
	} else if p.Format.Size != "" {
		add("File Size", FormatProbeSize(p.Format.Size))
	}

	// Stream summary
	add("Streams", fmt.Sprintf("%d total (%d video, %d audio, %d subtitle)",
		len(p.Streams), len(p.VideoStreams()), len(p.AudioStreams()), len(p.SubtitleStreams())))

	// Primary video stream
	if vs := p.VideoStreams(); len(vs) > 0 {
		add("Video Codec", vs[0].FormatCodecDisplay())
		if vs[0].Width > 0 && vs[0].Height > 0 {
			add("Resolution", fmt.Sprintf("%dx%d", vs[0].Width, vs[0].Height))
		}
		add("Pixel Format", vs[0].PixFmt)
		if fps := vs[0].FormatFrameRate(); fps != "" && fps != "0/0" && fps != "0 fps" {
			add("Frame Rate", fps)
		}
		add("Color Space", vs[0].ColorSpace)
		add("Color Transfer", vs[0].ColorTransfer)
		add("Color Primaries", vs[0].ColorPrimaries)
		add("Color Range", vs[0].ColorRange)
		add("Profile", vs[0].Profile)
	}

	// Primary audio stream
	if as := p.AudioStreams(); len(as) > 0 {
		add("Audio Codec", as[0].FormatCodecDisplay())
		if sr := as[0].FormatSampleRate(); sr != "" {
			add("Sample Rate", sr)
		}
		if as[0].ChannelLayout != "" {
			add("Channels", fmt.Sprintf("%d (%s)", as[0].Channels, as[0].ChannelLayout))
		} else if as[0].Channels > 0 {
			add("Channels", fmt.Sprintf("%d", as[0].Channels))
		}
	}

	return rows
}

// AllPropertyLabels collects the union of all property labels across streams,
// preserving the order of first appearance.
func AllPropertyLabels(streams []ProbeStream) []string {
	seen := map[string]bool{}
	var labels []string
	for _, s := range streams {
		for _, row := range s.StreamPropertyRows() {
			if !seen[row.Label] {
				seen[row.Label] = true
				labels = append(labels, row.Label)
			}
		}
	}
	return labels
}

// PropertyLabelsForType collects property labels across streams of a given type.
func PropertyLabelsForType(streams []ProbeStream, codecType string) []string {
	seen := map[string]bool{}
	var labels []string
	for _, s := range streams {
		if s.CodecType != codecType {
			continue
		}
		for _, row := range s.StreamPropertyRows() {
			if !seen[row.Label] {
				seen[row.Label] = true
				labels = append(labels, row.Label)
			}
		}
	}
	return labels
}

// StreamPropertyMap builds a label→value map for a stream's properties.
func (s ProbeStream) StreamPropertyMap() map[string]string {
	m := make(map[string]string)
	for _, row := range s.StreamPropertyRows() {
		m[row.Label] = row.Value
	}
	return m
}

// BuildStreamColumns converts probe streams into StreamColumn data for the table.
func BuildStreamColumns(streams []ProbeStream) []StreamColumn {
	cols := make([]StreamColumn, len(streams))
	for i, s := range streams {
		cols[i] = StreamColumn{
			Index:      i,
			CodecType:  s.CodecType,
			IsDefault:  s.IsDefault(),
			Language:   s.StreamLanguage(),
			Title:      s.StreamTitle(),
			Properties: s.StreamPropertyMap(),
		}
	}
	return cols
}

// BuildColumnsForType builds StreamColumn data for streams of a given codec type.
func BuildColumnsForType(streams []ProbeStream, codecType string) []StreamColumn {
	var cols []StreamColumn
	for _, s := range streams {
		if s.CodecType != codecType {
			continue
		}
		cols = append(cols, StreamColumn{
			Index:      s.Index,
			CodecType:  s.CodecType,
			IsDefault:  s.IsDefault(),
			Language:   s.StreamLanguage(),
			Title:      s.StreamTitle(),
			Properties: s.StreamPropertyMap(),
		})
	}
	return cols
}

// BuildVideoSummaryRows returns InfoPair rows for the first video stream.
func BuildVideoSummaryRows(streams []ProbeStream) []InfoPair {
	for _, s := range streams {
		if s.CodecType == "video" {
			return s.StreamPropertyRows()
		}
	}
	return nil
}

// VideoStreamHDRInfo extracts HDR metadata from side_data_list for the video stream.
func VideoStreamHDRInfo(streams []ProbeStream) []InfoPair {
	for _, s := range streams {
		if s.CodecType != "video" {
			continue
		}
		var rows []InfoPair
		for _, sd := range s.SideDataList {
			sdType, _ := sd["side_data_type"].(string)
			if sdType == "" {
				continue
			}
			switch {
			case strings.Contains(sdType, "Mastering display"):
				rows = append(rows, InfoPair{Label: "Mastering Display", Value: "Present"})
			case strings.Contains(sdType, "Content light level"):
				if maxCLL, ok := sd["max_content"].(float64); ok {
					if maxFALL, ok := sd["max_average"].(float64); ok {
						rows = append(rows, InfoPair{
							Label: "HDR Luminance",
							Value: fmt.Sprintf("MaxCLL: %.0f, MaxFALL: %.0f", maxCLL, maxFALL),
						})
					}
				}
			case strings.Contains(sdType, "DOVI"):
				rows = append(rows, InfoPair{Label: "Dolby Vision", Value: "Present"})
			}
		}
		return rows
	}
	return nil
}
