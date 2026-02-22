package ffmpeg

import (
	"bufio"
	"strconv"
	"strings"
)

// Progress represents ffmpeg encoding progress.
type Progress struct {
	Frame     int64   // Current frame number
	FPS       float64 // Current encoding speed in frames per second
	Bitrate   string  // Current bitrate (e.g., "1234.5kbits/s")
	TotalSize int64   // Current output size in bytes
	OutTimeUS int64   // Output timestamp in microseconds
	Speed     string  // Encoding speed multiplier (e.g., "2.5x")
	Progress  string  // "continue" or "end"
}

// OutTimeMS returns the output time in milliseconds.
func (p Progress) OutTimeMS() int64 {
	return p.OutTimeUS / 1000
}

// OutTimeSeconds returns the output time in seconds.
func (p Progress) OutTimeSeconds() float64 {
	return float64(p.OutTimeUS) / 1_000_000
}

// ParseProgressLine parses a single line from ffmpeg -progress output.
// Returns the key, value, and whether parsing succeeded.
func ParseProgressLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", "", false
	}

	idx := strings.Index(line, "=")
	if idx == -1 {
		return "", "", false
	}

	return line[:idx], line[idx+1:], true
}

// ProgressParser accumulates progress updates from ffmpeg output.
type ProgressParser struct {
	current Progress
}

// NewProgressParser creates a new progress parser.
func NewProgressParser() *ProgressParser {
	return &ProgressParser{}
}

// ParseLine parses a line and updates internal state.
// Returns true if a complete progress update is ready (on "progress=" line).
func (p *ProgressParser) ParseLine(line string) bool {
	key, value, ok := ParseProgressLine(line)
	if !ok {
		return false
	}

	switch key {
	case "frame":
		p.current.Frame, _ = strconv.ParseInt(value, 10, 64)
	case "fps":
		p.current.FPS, _ = strconv.ParseFloat(value, 64)
	case "bitrate":
		p.current.Bitrate = value
	case "total_size":
		p.current.TotalSize, _ = strconv.ParseInt(value, 10, 64)
	case "out_time_us":
		p.current.OutTimeUS, _ = strconv.ParseInt(value, 10, 64)
	case "speed":
		p.current.Speed = value
	case "progress":
		p.current.Progress = value
		return true // Complete update
	}

	return false
}

// Current returns the current progress state.
func (p *ProgressParser) Current() Progress {
	return p.current
}

// Reset clears the current progress state.
func (p *ProgressParser) Reset() {
	p.current = Progress{}
}

// ParseProgressOutput reads ffmpeg -progress output from a reader and sends updates to channel.
func ParseProgressOutput(scanner *bufio.Scanner, progress chan<- Progress) {
	parser := NewProgressParser()

	for scanner.Scan() {
		line := scanner.Text()
		if parser.ParseLine(line) {
			progress <- parser.Current()
			if parser.Current().Progress == "end" {
				break
			}
		}
	}
}
