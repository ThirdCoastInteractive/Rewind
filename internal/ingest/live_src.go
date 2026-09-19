package ingest

import (
	"net/url"
	"strings"

	"github.com/google/uuid"
	"thirdcoast.systems/rewind/internal/videoid"
)

// videosSrcInput is the pure input for live-aware videos.src selection.
// Expand/normalize of the job URL happens in the caller (may hit the network).
type videosSrcInput struct {
	RawJobURL       string
	ExpandedJobURL  string
	JobDerivedSrc   string
	WebpageURL      string
	OriginalURL     string
	InfoID          string
	CanonicalDomain string
	LiveStatus      string
	IsLive          bool
	WasLive         bool
}

func infoIsLiveSession(liveStatus string, isLive, wasLive bool) bool {
	if isLive || wasLive {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(liveStatus)) {
	case "is_live", "was_live", "post_live":
		return true
	default:
		return false
	}
}

// resolveVideosSrc picks videos.src and SelectVideoBySrc candidates.
// Non-live keeps job-URL preference. Live prefers a per-session URL and
// omits bare channel-page URLs from lookup so a later live does not merge.
func resolveVideosSrc(in videosSrcInput) (src string, candidates []string) {
	live := infoIsLiveSession(in.LiveStatus, in.IsLive, in.WasLive)
	src = strings.TrimSpace(in.JobDerivedSrc)
	if live {
		if preferred := preferLiveSessionSrc(in); preferred != "" {
			src = preferred
		}
	}
	return src, buildSrcLookupCandidates(in, src, live)
}

func preferLiveSessionSrc(in videosSrcInput) string {
	for _, u := range []string{in.WebpageURL, in.OriginalURL} {
		u = strings.TrimSpace(u)
		if u == "" || isLiveChannelPageURL(u) {
			continue
		}
		return normalizeSrcOrRaw(u)
	}
	if id := strings.TrimSpace(in.InfoID); id != "" && strings.TrimSpace(in.CanonicalDomain) != "" {
		if syn := synthesizeLiveSessionSrc(in.CanonicalDomain, id); syn != "" {
			return normalizeSrcOrRaw(syn)
		}
	}
	jobSrc := strings.TrimSpace(in.JobDerivedSrc)
	if jobSrc != "" && !isLiveChannelPageURL(jobSrc) {
		return jobSrc
	}
	if jobSrc != "" {
		return jobSrc
	}
	return ""
}

func synthesizeLiveSessionSrc(canonicalDomain, id string) string {
	domain := strings.TrimSpace(strings.ToLower(canonicalDomain))
	id = strings.TrimSpace(id)
	if domain == "" || id == "" {
		return ""
	}
	switch domain {
	case "kick.com":
		if looksLikeUUID(id) {
			return "https://kick.com/video/" + id
		}
		// Slug ids must not collapse to the channel login URL.
		return "https://kick.com/livestreams/" + id
	default:
		return "https://" + domain + "/" + id
	}
}

func buildSrcLookupCandidates(in videosSrcInput, src string, live bool) []string {
	candidates := make([]string, 0, 8)
	seen := map[string]struct{}{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		candidates = append(candidates, s)
	}
	maybeAdd := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if live && isLiveChannelPageURL(s) {
			return
		}
		add(s)
	}

	add(src)
	maybeAdd(in.RawJobURL)
	maybeAdd(in.ExpandedJobURL)
	maybeAdd(in.WebpageURL)
	maybeAdd(in.OriginalURL)

	for _, s := range []string{in.RawJobURL, in.ExpandedJobURL, in.WebpageURL, in.OriginalURL} {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if live && isLiveChannelPageURL(s) {
			continue
		}
		if normalized, _, nerr := videoid.NormalizeSourceURL(s); nerr == nil {
			if live && isLiveChannelPageURL(normalized) {
				continue
			}
			add(normalized)
		}
	}
	return candidates
}

func normalizeSrcOrRaw(raw string) string {
	if normalized, _, err := videoid.NormalizeSourceURL(raw); err == nil && strings.TrimSpace(normalized) != "" {
		return normalized
	}
	return strings.TrimSpace(raw)
}

func looksLikeUUID(s string) bool {
	_, err := uuid.Parse(strings.TrimSpace(s))
	return err == nil
}

// isLiveChannelPageURL reports Kick/Twitch channel pages and YouTube /live
// channel paths that must not be used as stable per-session videos.src keys.
func isLiveChannelPageURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme == "" {
		u, err = url.Parse("https://" + raw)
		if err != nil {
			return false
		}
	}
	host := videoid.ResolveCanonicalDomain(u.Hostname())
	segs := pathSegmentsLower(u.Path)
	switch host {
	case "kick.com":
		if len(segs) == 0 {
			return false
		}
		switch segs[0] {
		case "video", "videos", "livestreams", "categories", "category", "clips", "browse", "search":
			return false
		}
		if len(segs) == 1 {
			return true
		}
		return len(segs) == 2 && segs[1] == "videos"
	case "twitch.tv":
		if len(segs) == 0 {
			return false
		}
		switch segs[0] {
		case "videos", "directory", "downloads", "jobs", "p", "settings", "subscriptions", "inventory", "wallet", "messages", "search":
			return false
		}
		if len(segs) == 1 {
			return true
		}
		return len(segs) == 2 && segs[1] == "videos"
	case "youtube.com":
		if len(segs) == 0 {
			return false
		}
		if len(segs) == 1 && segs[0] == "live" {
			return true
		}
		// /@handle/live, /channel/UC…/live, /c/name/live — not /live/{videoId}
		return segs[len(segs)-1] == "live" && segs[0] != "live"
	default:
		return false
	}
}

func pathSegmentsLower(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	parts := strings.Split(path, "/")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		out = append(out, strings.ToLower(p))
	}
	return out
}
