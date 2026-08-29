// Package channelid derives a stable platform + identity key for a channel
// from yt-dlp metadata and source URLs.
package channelid

import (
	"net/url"
	"strings"
)

// Identity is one distribution slot (a YouTube channel, a Rumble user, …).
type Identity struct {
	Platform     string
	Key          string // unique per platform
	ChannelID    string
	Uploader     string
	CanonicalURL string
}

// PlatformFromSrc classifies a video/channel URL into a short platform name.
func PlatformFromSrc(src string) string {
	s := strings.ToLower(src)
	switch {
	case strings.Contains(s, "youtube.com"), strings.Contains(s, "youtu.be"):
		return "youtube"
	case strings.Contains(s, "rumble.com"):
		return "rumble"
	case strings.Contains(s, "vimeo.com"):
		return "vimeo"
	case strings.Contains(s, "kick.com"):
		return "kick"
	case strings.Contains(s, "twitter.com"), strings.Contains(s, "x.com"), strings.Contains(s, "t.co/"):
		return "twitter"
	case strings.Contains(s, "twitch.tv"):
		return "twitch"
	case strings.Contains(s, "bitchute.com"):
		return "bitchute"
	case strings.Contains(s, "odysee.com"):
		return "odysee"
	default:
		return "other"
	}
}

// FromMetadata builds an Identity from archived video fields.
func FromMetadata(src, uploader string, channelID, uploaderID *string, channelURL, uploaderURL string) Identity {
	id := Identity{
		Platform:     PlatformFromSrc(src),
		Uploader:     strings.TrimSpace(uploader),
		ChannelID:    deref(channelID),
		CanonicalURL: firstNonEmpty(channelURL, uploaderURL, src),
	}
	id.Key = firstNonEmpty(id.ChannelID, deref(uploaderID), canonicalizeURL(id.CanonicalURL), id.Uploader)
	if id.Key == "" {
		id.Key = "unknown"
	}
	return id
}

// FromURL builds an Identity from a user-supplied channel or playlist URL
// when yt-dlp metadata is not yet available. It recognizes YouTube
// /channel/UC…, /@handle, /c/, and /user/ paths, Rumble /c/, Kick channel
// URLs, and falls back to host+path for everything else.
func FromURL(raw string) Identity {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Identity{Platform: "other", Key: "unknown"}
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}

	id := Identity{
		Platform:     PlatformFromSrc(raw),
		CanonicalURL: canonicalizeURL(raw),
	}

	u, err := url.Parse(raw)
	if err != nil {
		id.Key = firstNonEmpty(id.CanonicalURL, "unknown")
		return id
	}

	id.CanonicalURL = stripCanonicalTab(id.CanonicalURL)
	id.CanonicalURL = attachPlaylistList(id.CanonicalURL, u)

	segs := dropTrailingChannelTab(pathSegments(u.Path))
	switch id.Platform {
	case "youtube":
		fillYouTubeIdentity(&id, segs, u)
	case "rumble":
		fillRumbleIdentity(&id, segs)
	case "kick":
		fillKickIdentity(&id, segs)
	case "twitter":
		fillTwitterIdentity(&id, segs)
	}

	if id.Key == "" {
		id.Key = genericHostPathKey(u, id.Platform != "other")
	}
	if id.Key == "" {
		id.Key = firstNonEmpty(id.CanonicalURL, "unknown")
	}
	if id.CanonicalURL == "" {
		id.CanonicalURL = canonicalizeURL(raw)
	}
	return id
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// canonicalizeURL drops tracking query params so @handle and /channel/UC
// variants still differ (those need yt-dlp to collapse) but https/www/trailing
// slash noise does not.
func canonicalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return strings.TrimRight(raw, "/")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	u.Fragment = ""
	u.RawQuery = ""
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String()
}

func fillYouTubeIdentity(id *Identity, segs []string, u *url.URL) {
	if len(segs) >= 2 && strings.EqualFold(segs[0], "channel") {
		id.ChannelID = segs[1]
		id.Key = segs[1]
		return
	}
	if len(segs) >= 1 && strings.HasPrefix(segs[0], "@") {
		id.Key = segs[0]
		id.Uploader = strings.TrimPrefix(segs[0], "@")
		return
	}
	if len(segs) >= 2 && strings.EqualFold(segs[0], "c") {
		id.Key = "c/" + segs[1]
		return
	}
	if len(segs) >= 2 && strings.EqualFold(segs[0], "user") {
		id.Key = "user/" + segs[1]
		return
	}
	if list := strings.TrimSpace(u.Query().Get("list")); list != "" {
		id.Key = list
	}
}

func fillRumbleIdentity(id *Identity, segs []string) {
	if len(segs) >= 2 && strings.EqualFold(segs[0], "c") {
		id.Key = segs[1]
	}
}

func fillTwitterIdentity(id *Identity, segs []string) {
	if len(segs) == 0 {
		return
	}
	h := strings.TrimPrefix(segs[0], "@")
	if h == "" {
		return
	}
	switch strings.ToLower(h) {
	case "home", "explore", "search", "i", "intent", "share", "hashtag", "settings", "compose":
		return
	}
	id.Key = h
	id.CanonicalURL = "https://x.com/" + h
}

func fillKickIdentity(id *Identity, segs []string) {
	if len(segs) == 0 {
		return
	}
	switch strings.ToLower(segs[0]) {
	case "video", "videos", "categories", "clips", "browse":
		return
	}
	id.Key = segs[0]
}

func genericHostPathKey(u *url.URL, dropTab bool) string {
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	segs := pathSegments(u.Path)
	if dropTab {
		segs = dropTrailingChannelTab(segs)
	}
	if host == "" {
		return strings.Join(segs, "/")
	}
	if len(segs) == 0 {
		return host
	}
	return host + "/" + strings.Join(segs, "/")
}

func pathSegments(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	parts := strings.Split(path, "/")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func dropTrailingChannelTab(segs []string) []string {
	if len(segs) < 2 {
		return segs
	}
	switch strings.ToLower(segs[len(segs)-1]) {
	case "videos", "streams", "shorts", "playlists", "community", "about", "featured", "live", "podcasts", "releases", "membership":
		return segs[:len(segs)-1]
	}
	return segs
}

func stripCanonicalTab(canon string) string {
	u, err := url.Parse(canon)
	if err != nil {
		return canon
	}
	segs := dropTrailingChannelTab(pathSegments(u.Path))
	if len(segs) == 0 {
		u.Path = ""
	} else {
		u.Path = "/" + strings.Join(segs, "/")
	}
	return u.String()
}

func attachPlaylistList(canon string, raw *url.URL) string {
	if raw == nil {
		return canon
	}
	list := strings.TrimSpace(raw.Query().Get("list"))
	if list == "" {
		return canon
	}
	u, err := url.Parse(canon)
	if err != nil {
		return canon
	}
	q := url.Values{}
	q.Set("list", list)
	u.RawQuery = q.Encode()
	return u.String()
}
