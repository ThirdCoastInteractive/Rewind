// Package automarkers turns yt-dlp chapters, description chapter lists, and
// comment timestamps into Marker rows without touching user-created markers.
package automarkers

import (
	"context"
	"encoding/json"
	"strings"

	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/utils/commentfmt"
)

const maxCommentMarkers = 50

type candidate struct {
	Start    float64
	End      float64
	Title    string
	Source   string
	Ref      string
	Chapter  bool
	Priority int
}

// IngestFromInfoJSON writes chapter + description markers, then comment markers.
func IngestFromInfoJSON(ctx context.Context, q *db.Queries, video *db.Video, rawInfoJSON []byte) {
	if video == nil {
		return
	}
	dur := 0.0
	if video.DurationSeconds != nil {
		dur = float64(*video.DurationSeconds)
	}

	for _, c := range chaptersFromInfo(rawInfoJSON, dur) {
		_ = upsert(ctx, q, video, c)
	}
	for _, c := range descriptionChapters(video.Description, dur) {
		_ = upsert(ctx, q, video, c)
	}
}

// IngestCommentJSON writes comment-sourced point markers from a yt-dlp comments array.
func IngestCommentJSON(ctx context.Context, q *db.Queries, video *db.Video, commentsJSON []byte) {
	var arr []map[string]any
	if err := json.Unmarshal(commentsJSON, &arr); err != nil {
		return
	}
	var stamps []commentStamp
	for _, c := range arr {
		id, _ := c["id"].(string)
		text, _ := c["text"].(string)
		if text == "" {
			text, _ = c["content"].(string)
		}
		if id == "" || text == "" {
			continue
		}
		st := commentStamp{ID: id, Text: text}
		if v, ok := c["like_count"].(float64); ok {
			st.Likes = int64(v)
		}
		if v, ok := c["is_pinned"].(bool); ok {
			st.Pinned = v
		}
		if v, ok := c["is_favorited"].(bool); ok {
			st.Hearted = v
		}
		if v, ok := c["author_is_uploader"].(bool); ok {
			st.Uploader = v
		}
		stamps = append(stamps, st)
	}
	ingestCommentStamps(ctx, q, video, stamps)
}

func ingestCommentStamps(ctx context.Context, q *db.Queries, video *db.Video, comments []commentStamp) {
	if video == nil || len(comments) == 0 {
		return
	}
	dur := 0.0
	if video.DurationSeconds != nil {
		dur = float64(*video.DurationSeconds)
	}
	var ranked []candidate
	for _, cm := range comments {
		for _, seg := range commentfmt.ParseSegments(cm.Text) {
			if !seg.IsTime {
				continue
			}
			if seg.Seconds <= 0 {
				continue
			}
			if dur > 0 && seg.Seconds > dur+1 {
				continue
			}
			title := strings.TrimSpace(strings.ReplaceAll(cm.Text, seg.Text, ""))
			title = strings.Trim(title, " \t-–—:|")
			if len(title) > 80 {
				title = title[:80]
			}
			if title == "" {
				title = "Comment"
			}
			pri := 0
			if cm.Pinned {
				pri += 3
			}
			if cm.Uploader {
				pri += 3
			}
			if cm.Hearted {
				pri += 2
			}
			if cm.Likes > 10 {
				pri += 1
			}
			ranked = append(ranked, candidate{
				Start:    seg.Seconds,
				Title:    title,
				Source:   "comment",
				Ref:      cm.ID,
				Priority: pri,
			})
		}
	}
	// Keep highest-priority unique whole-second buckets, cap.
	seen := map[int]bool{}
	n := 0
	// simple insertion by priority
	for i := 0; i < len(ranked); i++ {
		best := i
		for j := i + 1; j < len(ranked); j++ {
			if ranked[j].Priority > ranked[best].Priority {
				best = j
			}
		}
		ranked[i], ranked[best] = ranked[best], ranked[i]
		c := ranked[i]
		bucket := int(c.Start + 0.5)
		if seen[bucket] {
			continue
		}
		seen[bucket] = true
		_ = upsert(ctx, q, video, c)
		n++
		if n >= maxCommentMarkers {
			break
		}
	}
}

type commentStamp struct {
	ID       string
	Text     string
	Likes    int64
	Pinned   bool
	Hearted  bool
	Uploader bool
}

func chaptersFromInfo(raw []byte, duration float64) []candidate {
	var env struct {
		Chapters []struct {
			StartTime float64 `json:"start_time"`
			EndTime   float64 `json:"end_time"`
			Title     string  `json:"title"`
		} `json:"chapters"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil
	}
	var out []candidate
	for i, ch := range env.Chapters {
		title := strings.TrimSpace(ch.Title)
		if title == "" {
			continue
		}
		end := ch.EndTime
		if end < ch.StartTime {
			end = 0
		}
		out = append(out, candidate{
			Start:   ch.StartTime,
			End:     end,
			Title:   title,
			Source:  "chapter",
			Ref:     "info:" + itoa(i),
			Chapter: true,
		})
	}
	return out
}

func descriptionChapters(desc string, duration float64) []candidate {
	var out []candidate
	for i, line := range strings.Split(desc, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		segs := commentfmt.ParseSegments(line)
		if len(segs) == 0 || !segs[0].IsTime {
			continue
		}
		if segs[0].Seconds == 0 && i == 0 {
			// 0:00 at start of a chapter list is valid
		}
		rest := strings.TrimSpace(line[len(segs[0].Text):])
		rest = strings.TrimLeft(rest, " -–—|")
		if rest == "" {
			continue
		}
		out = append(out, candidate{
			Start:   segs[0].Seconds,
			Title:   rest,
			Source:  "chapter",
			Ref:     "desc:" + itoa(i),
			Chapter: true,
		})
	}
	if len(out) < 2 {
		// A single leading timestamp isn't a chapter list.
		return nil
	}
	return out
}

func upsert(ctx context.Context, q *db.Queries, video *db.Video, c candidate) error {
	color := "#64748b"
	mt := db.MarkerTypePoint
	var dur *float64
	if c.Chapter {
		color = "#a67c52"
		mt = db.MarkerTypeChapter
		if c.End > c.Start {
			d := c.End - c.Start
			dur = &d
		}
	}
	return q.UpsertAutoMarker(ctx, &db.UpsertAutoMarkerParams{
		VideoID:     video.ID,
		Timestamp:   c.Start,
		Title:       c.Title,
		Description: "",
		Color:       color,
		MarkerType:  mt,
		Duration:    dur,
		CreatedBy:   video.ArchivedBy,
		Source:      c.Source,
		SourceRef:   c.Ref,
	})
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [12]byte
	n := len(b)
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	return string(b[n:])
}
