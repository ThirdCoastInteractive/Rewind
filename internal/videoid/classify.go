package videoid

import (
	"net/url"
	"strings"
)

// IsPlaylistOrChannelURL reports whether url points to a COLLECTION of videos
// (a playlist, channel, or user/handle page) rather than a single video.
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
