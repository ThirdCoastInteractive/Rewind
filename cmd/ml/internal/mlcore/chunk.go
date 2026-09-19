package mlcore

import (
	"fmt"
	"strings"
	"time"

	"thirdcoast.systems/rewind/pkg/captions"
)

const (
	// DefaultTargetTokens is a fallback cue-text budget when callers pass 0.
	// Production uses CueChunkTarget so the prompt fits num_ctx.
	DefaultTargetTokens = 8192
	// DefaultOverlapTokens repeats trailing cues so boundaries aren't lost.
	DefaultOverlapTokens = 1024
	PromptVersion        = "context-v5-topics"
	// contextBudget is Ollama num_ctx. One 27B sequence on a 24 GiB card holds
	// 32k; 16k rejected full chunks because EstimateTokens is byte-level.
	contextBudget = 32768
	predictTokens = 4096
	kvOverhead    = 512
)

// CueChunkTarget is the max cue-text bytes that still fit in ChatJSON with the
// given system prompt and extra user prefix (chunk header, retry guidance).
func CueChunkTarget(system string, extraUser int) int {
	if extraUser < 0 {
		extraUser = 0
	}
	n := contextBudget - predictTokens - kvOverhead - EstimateTokens(system) - extraUser
	if n < 1024 {
		return 1024
	}
	return n
}

func chatFits(system, user string) bool {
	return EstimateTokens(system)+EstimateTokens(user)+predictTokens+kvOverhead <= contextBudget
}

func userBudget(system string) int {
	n := contextBudget - predictTokens - kvOverhead - EstimateTokens(system)
	if n < 512 {
		return 512
	}
	return n
}

// trimUserToFit keeps ChatJSON inside num_ctx by shrinking the user message.
func trimUserToFit(system, user string) string {
	max := userBudget(system)
	if EstimateTokens(user) <= max {
		return user
	}
	if i := strings.IndexByte(user, '\n'); i >= 0 && i < 256 {
		header := user[:i+1]
		room := max - EstimateTokens(header)
		if room >= 256 {
			body := user[i+1:]
			if EstimateTokens(body) > room {
				body = body[:room]
				if j := strings.LastIndexByte(body, '\n'); j > room/2 {
					body = body[:j]
				}
			}
			return header + body
		}
	}
	if max > len(user) {
		return user
	}
	return user[:max]
}

// Cue is a transcript cue with a 1-based ID used in the model prompt.
type Cue struct {
	ID    int
	Start float64
	End   float64
	Text  string
}

// Chunk is a slice of cues plus the prompt body sent to Ollama.
type Chunk struct {
	Cues       []Cue
	StartID    int
	EndID      int
	Text       string
	TokenCount int
}

// CuesFromCaptions assigns 1-based IDs.
func CuesFromCaptions(src []captions.Cue) []Cue {
	out := make([]Cue, 0, len(src))
	for _, c := range src {
		text := strings.TrimSpace(c.Text)
		if text == "" {
			continue
		}
		out = append(out, Cue{ID: len(out) + 1, Start: c.Start, End: c.End, Text: text})
	}
	return out
}

// EstimateTokens uses a conservative byte-level tokenizer upper bound, including cue labels.
// This prevents timestamps, unusual Unicode, or dense punctuation from overflowing context.
func EstimateTokens(s string) int {
	n := len(s)
	if n < 1 && s != "" {
		return 1
	}
	return n
}

// FormatCueLine is the per-cue prompt line: [c0001 00:00:01.000-00:00:04.200] text
func FormatCueLine(c Cue) string {
	return fmt.Sprintf("[c%04d %s-%s] %s", c.ID, formatTS(c.Start), formatTS(c.End), c.Text)
}

func formatTS(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	d := time.Duration(seconds * float64(time.Second))
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	d -= s * time.Second
	ms := d / time.Millisecond
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms)
}

// ChunkCues splits cues into ~target-token windows with overlap. A single cue
// larger than target still becomes its own chunk.
func ChunkCues(cues []Cue, target, overlap int) []Chunk {
	if target <= 0 {
		target = DefaultTargetTokens
	}
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= target {
		overlap = target / 8
	}
	if len(cues) == 0 {
		return nil
	}

	var chunks []Chunk
	start := 0
	for start < len(cues) {
		token := 0
		end := start
		for end < len(cues) {
			lineTok := EstimateTokens(FormatCueLine(cues[end]))
			sep := 0
			if end > start {
				sep = 1
			}
			if end > start && token+sep+lineTok > target {
				break
			}
			token += sep + lineTok
			end++
			if token >= target {
				break
			}
		}
		if end <= start {
			end = start + 1
		}
		chunkCues := append([]Cue(nil), cues[start:end]...)
		var b strings.Builder
		for i, c := range chunkCues {
			if i > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(FormatCueLine(c))
		}
		text := b.String()
		chunks = append(chunks, Chunk{
			Cues:       chunkCues,
			StartID:    chunkCues[0].ID,
			EndID:      chunkCues[len(chunkCues)-1].ID,
			Text:       text,
			TokenCount: EstimateTokens(text),
		})
		if end >= len(cues) {
			break
		}
		// Walk back from end until we have ~overlap tokens for the next start.
		next := end
		ov := 0
		for next > start+1 {
			next--
			ov += EstimateTokens(FormatCueLine(cues[next]))
			if ov >= overlap {
				break
			}
		}
		if next <= start {
			next = start + 1
		}
		start = next
	}
	return chunks
}

// CueByID returns the cue with the given 1-based ID. Models often emit IDs
// relative to the current chunk (1..len) even when prompt labels start at c0500.
func CueByID(cues []Cue, id int) (Cue, bool) {
	for _, c := range cues {
		if c.ID == id {
			return c, true
		}
	}
	if id >= 1 && id <= len(cues) {
		return cues[id-1], true
	}
	return Cue{}, false
}

// DurationFromCues is the last cue end (or 0).
func DurationFromCues(cues []Cue) float64 {
	var max float64
	for _, c := range cues {
		if c.End > max {
			max = c.End
		}
	}
	return max
}
