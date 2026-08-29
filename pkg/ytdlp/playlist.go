package ytdlp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

// FlatEntry is one video entry from a flat playlist/channel enumeration.
type FlatEntry struct {
	ID    string // yt-dlp entry id (e.g. the YouTube video id)
	URL   string // entry URL (often a canonical watch URL; may be empty for some extractors)
	Title string
	Type  string // yt-dlp "_type" of the entry ("url", "playlist", ...)
	IEKey string // extractor key yt-dlp reported for the entry (e.g. "Youtube", "YoutubeTab")
}

// maxChannelTabs bounds how many nested tab playlists ListChannelVideos will
// expand for a single channel URL (YouTube channels have ~3: Videos, Shorts,
// Live). Guards against pathological wrappers fanning out into many listings.
const maxChannelTabs = 6

// flatPayload is the JSON shape of --flat-playlist --dump-single-json output.
type flatPayload struct {
	ID      string          `json:"id"`
	URL     string          `json:"url"`
	Title   string          `json:"title"`
	Entries []flatEntryJSON `json:"entries"`
}

type flatEntryJSON struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	Title string `json:"title"`
	Type  string `json:"_type"`
	IEKey string `json:"ie_key"`
	// Entries is non-nil when the entry is itself a playlist that yt-dlp
	// resolved inline — bare channel URLs enumerate as a wrapper playlist
	// whose tab playlists (Videos/Shorts/Live) nest their videos this way.
	Entries []flatEntryJSON `json:"entries"`
}

// maxFlatNesting caps recursion through inline-nested playlists.
const maxFlatNesting = 3

// listFlat runs a flat (no-download) enumeration of url and returns the parsed
// JSON payload. It uses: --flat-playlist --dump-single-json --skip-download.
func (c *Client) listFlat(ctx context.Context, url string, extraArgs ...string) (*flatPayload, error) {
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
	payload := &flatPayload{}
	if err := json.Unmarshal(raw, payload); err != nil {
		return nil, fmt.Errorf("ytdlp: parse json: %w", err)
	}
	return payload, nil
}

// entries converts the payload to leaf (video) FlatEntry values, applying the
// single-video fallback: when given a non-playlist URL, yt-dlp emits one video
// object with no "entries" key, which is surfaced as a single entry (if it has
// an id). Inline-nested playlists (channel tabs) are flattened into their
// contained videos; playlist objects themselves are never returned as entries.
func (p *flatPayload) entries() []FlatEntry {
	if p.Entries == nil {
		if strings.TrimSpace(p.ID) == "" {
			return []FlatEntry{}
		}
		return []FlatEntry{{
			ID:    p.ID,
			URL:   p.URL,
			Title: p.Title,
		}}
	}
	return appendLeafEntries(make([]FlatEntry, 0, len(p.Entries)), p.Entries, 0)
}

// appendLeafEntries walks a flat-playlist entry list, recursing into
// inline-nested playlists and appending only leaf video entries to out.
func appendLeafEntries(out []FlatEntry, list []flatEntryJSON, depth int) []FlatEntry {
	if depth > maxFlatNesting {
		return out
	}
	for _, e := range list {
		// A non-nil Entries array marks a playlist yt-dlp resolved inline
		// (possibly empty). Flatten its videos; never emit the playlist.
		if e.Entries != nil {
			out = appendLeafEntries(out, e.Entries, depth+1)
			continue
		}
		// Playlist references without inline entries (e.g. lazy channel
		// tabs) are not videos either; callers wanting them must inspect
		// the raw payload.
		if isChannelTabJSON(e) {
			continue
		}
		// Skip entries without an id; they aren't actionable.
		if strings.TrimSpace(e.ID) == "" {
			continue
		}
		out = append(out, FlatEntry{
			ID:    e.ID,
			URL:   e.URL,
			Title: e.Title,
			Type:  e.Type,
			IEKey: e.IEKey,
		})
	}
	return out
}

// isChannelTabJSON reports whether a raw entry is a playlist/channel-tab
// object rather than a video.
func isChannelTabJSON(e flatEntryJSON) bool {
	return strings.EqualFold(e.IEKey, "YoutubeTab") || e.Type == "playlist"
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
	payload, err := c.listFlat(ctx, url, extraArgs...)
	if err != nil {
		return nil, err
	}
	return payload.entries(), nil
}

// ChannelListing is the result of enumerating the head of a channel/playlist.
type ChannelListing struct {
	Title   string      // channel/playlist title as reported by yt-dlp (may be empty)
	Entries []FlatEntry // video entries, newest first, deduplicated by id
}

// ListChannelVideos enumerates the newest videos of a channel/playlist URL
// without downloading. limit > 0 bounds how many entries are enumerated per
// playlist (and per channel tab); 0 means unbounded.
//
// Bare channel URLs on some sites (notably YouTube) enumerate as a wrapper
// playlist of tab playlists (Videos/Shorts/Live) rather than of videos.
// Tabs yt-dlp resolves inline are flattened directly; tabs returned only as
// URL references are enumerated with one extra yt-dlp call each. A tab that
// fails to enumerate is skipped (logged), not fatal.
func (c *Client) ListChannelVideos(ctx context.Context, url string, limit int, extraArgs ...string) (*ChannelListing, error) {
	args := append([]string{}, extraArgs...)
	if limit > 0 {
		args = append(args, "--playlist-end", strconv.Itoa(limit))
	}

	payload, err := c.listFlat(ctx, url, args...)
	if err != nil {
		return nil, err
	}

	listing := &ChannelListing{Title: strings.TrimSpace(payload.Title)}
	seen := make(map[string]bool)
	add := func(e FlatEntry) {
		id := strings.TrimSpace(e.ID)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		listing.Entries = append(listing.Entries, e)
	}

	// Leaf videos, including those inside inline-resolved tabs.
	for _, e := range payload.entries() {
		add(e)
	}

	// Tab playlists present only as URL references need their own listing.
	tabs := 0
	for _, raw := range payload.Entries {
		if raw.Entries != nil || !isChannelTabJSON(raw) || strings.TrimSpace(raw.URL) == "" {
			continue
		}
		if tabs >= maxChannelTabs {
			break
		}
		tabs++
		sub, err := c.listFlat(ctx, raw.URL, args...)
		if err != nil {
			slog.Warn("ytdlp: channel tab enumeration failed; skipping tab", "tab_url", raw.URL, "error", err)
			continue
		}
		for _, se := range sub.entries() {
			add(se)
		}
	}

	return listing, nil
}
