package videoid

import (
	"net/url"
	"strings"
)

// IsPlaylistOrChannelURL reports whether url points to a COLLECTION of videos
// (a playlist, channel, user/handle page, channel search tab, or site search)
// rather than a single video.
//
// It is YouTube-focused and conservative: anything it cannot confidently
// classify as a collection returns false. A YouTube /watch URL carrying a "v="
// param is treated as a single video even when it also has a "list=" param
// (the user is watching one video). youtu.be/<id> short links are single
// videos. For non-YouTube hosts it returns false unless the path obviously
// contains "/playlist".
func IsPlaylistOrChannelURL(rawURL string) bool {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return false
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if u.Scheme == "" {
		// Best effort: treat schemeless input as https so the host parses.
		u, err = url.Parse("https://" + rawURL)
		if err != nil {
			return false
		}
	}

	host := normalizeHost(u.Host)
	host = strings.TrimPrefix(host, "www.")
	host = strings.TrimPrefix(host, "m.")

	path := strings.ToLower(trimTrailingSlash(u.Path))
	query := u.Query()

	isYouTube := host == "youtube.com" || strings.HasSuffix(host, ".youtube.com")
	isShort := host == "youtu.be"

	if isShort {
		// youtu.be/<id> short links are always single videos.
		return false
	}

	if isYouTube {
		hasV := strings.TrimSpace(query.Get("v")) != ""
		hasList := strings.TrimSpace(query.Get("list")) != ""

		// A /watch (or any) URL with v= is a single video, even with list=.
		if hasV {
			return false
		}

		// Site search (/results) and channel search tabs (/…/search).
		if path == "/results" || strings.Contains(path, "/search") {
			return true
		}

		// A playlist page, or any URL with list= but no v=, is a collection.
		if path == "/playlist" || hasList {
			return true
		}

		// Channel / user / handle pages and their tabs (e.g. /@name/videos).
		switch {
		case strings.HasPrefix(path, "/channel/"),
			strings.HasPrefix(path, "/c/"),
			strings.HasPrefix(path, "/user/"),
			strings.HasPrefix(path, "/@"):
			return true
		}

		return false
	}

	// Non-YouTube hosts are out of scope for v1 unless the path obviously
	// names a playlist.
	if strings.Contains(path, "/playlist") {
		return true
	}

	return false
}

// IsLiveChannelURL reports whether rawURL is a channel live page (Kick/Twitch
// channel root, or a YouTube channel/handle page ending in /live) rather than
// a VOD, clip, watch URL, or generic channel collection page.
func IsLiveChannelURL(rawURL string) bool {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return false
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if u.Scheme == "" {
		u, err = url.Parse("https://" + rawURL)
		if err != nil {
			return false
		}
	}

	host := normalizeHost(u.Host)
	host = strings.TrimPrefix(host, "www.")
	host = strings.TrimPrefix(host, "m.")

	path := strings.ToLower(trimTrailingSlash(u.Path))
	if path == "" || path == "/" {
		return false
	}

	switch {
	case host == "kick.com":
		return isKickLiveChannelPath(path)
	case host == "twitch.tv":
		return isTwitchLiveChannelPath(path)
	case host == "youtube.com" || strings.HasSuffix(host, ".youtube.com"):
		return isYouTubeLiveChannelPath(path)
	default:
		return false
	}
}

// Kick channel live pages are /{user}. VODs, clips, and site sections are not.
func isKickLiveChannelPath(path string) bool {
	parts := splitPath(path)
	if len(parts) != 1 {
		return false
	}
	switch parts[0] {
	case "video", "categories", "search", "auth":
		return false
	}
	return parts[0] != ""
}

// Twitch channel live pages are /{user}. VODs and clip paths are not.
func isTwitchLiveChannelPath(path string) bool {
	parts := splitPath(path)
	if len(parts) != 1 {
		return false
	}
	switch parts[0] {
	case "videos", "directory", "clips", "downloads", "jobs", "settings", "subscriptions":
		return false
	}
	return parts[0] != ""
}

// YouTube live channel pages end with /live under a channel/handle prefix.
// Bare /live and non-/live channel tabs are rejected.
func isYouTubeLiveChannelPath(path string) bool {
	if !strings.HasSuffix(path, "/live") {
		return false
	}
	base := strings.TrimSuffix(path, "/live")
	switch {
	case strings.HasPrefix(base, "/@"):
		rest := strings.TrimPrefix(base, "/@")
		return rest != "" && !strings.Contains(rest, "/")
	case strings.HasPrefix(base, "/channel/"):
		rest := strings.TrimPrefix(base, "/channel/")
		return rest != "" && !strings.Contains(rest, "/")
	case strings.HasPrefix(base, "/c/"):
		rest := strings.TrimPrefix(base, "/c/")
		return rest != "" && !strings.Contains(rest, "/")
	default:
		return false
	}
}

func splitPath(path string) []string {
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}
