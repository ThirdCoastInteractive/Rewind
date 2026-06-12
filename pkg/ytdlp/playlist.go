package ytdlp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// FlatEntry is one video entry from a flat playlist/channel enumeration.
type FlatEntry struct {
	ID    string // yt-dlp entry id (e.g. the YouTube video id)
	URL   string // entry URL (often a canonical watch URL; may be empty for some extractors)
	Title string
}

// ListPlaylistEntries enumerates a playlist/channel/user URL WITHOUT downloading,
// using yt-dlp --flat-playlist. Returns one FlatEntry per contained video.
//
// It uses: --flat-playlist --dump-single-json --skip-download, which yields a
// single JSON object with an "entries" array. When given a non-playlist URL,
// yt-dlp emits a single video object with no "entries" key; in that case a
// single FlatEntry built from the top-level id/title is returned (or an empty
// slice if there is no id).
func (c *Client) ListPlaylistEntries(ctx context.Context, url string, extraArgs ...string) ([]FlatEntry, error) {
	if strings.TrimSpace(url) == "" {
		return nil, fmt.Errorf("ytdlp: url is required")
	}

	args := []string{"--flat-playlist", "--dump-single-json", "--skip-download"}
	args = append(args, extraArgs...)
	args = append(args, url)

	stdout, stderr, err := c.exec(ctx, args...)
	if err != nil {
		return nil, wrapExecError(c.PathOrDefault(), args, stdout, stderr, err)
	}

	raw := bytes.TrimSpace(stdout)

	var payload struct {
		ID      string `json:"id"`
		URL     string `json:"url"`
		Title   string `json:"title"`
		Entries []struct {
			ID    string `json:"id"`
			URL   string `json:"url"`
			Title string `json:"title"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("ytdlp: parse json: %w", err)
	}

	// Non-playlist URL: yt-dlp returns a single video object (no "entries").
	if payload.Entries == nil {
		if strings.TrimSpace(payload.ID) == "" {
			return []FlatEntry{}, nil
		}
		return []FlatEntry{{
			ID:    payload.ID,
			URL:   payload.URL,
			Title: payload.Title,
		}}, nil
	}

	entries := make([]FlatEntry, 0, len(payload.Entries))
	for _, e := range payload.Entries {
		// Skip entries without an id; they aren't actionable.
		if strings.TrimSpace(e.ID) == "" {
			continue
		}
		entries = append(entries, FlatEntry{
			ID:    e.ID,
			URL:   e.URL,
			Title: e.Title,
		})
	}

	return entries, nil
}
