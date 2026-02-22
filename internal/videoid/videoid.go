package videoid

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Well-known host aliases. Key: input host. Value: canonical domain.
//
// Keep this intentionally conservative: we only alias hosts that are truly the
// same "source website" from a user perspective.
var canonicalDomainByHost = map[string]string{
	"youtube.com":     "youtube.com",
	"www.youtube.com": "youtube.com",
	"m.youtube.com":   "youtube.com",
	"youtu.be":        "youtube.com",

	"x.com":              "x.com",
	"www.x.com":          "x.com",
	"twitter.com":        "x.com",
	"www.twitter.com":    "x.com",
	"mobile.twitter.com": "x.com",

	"twitch.tv":     "twitch.tv",
	"www.twitch.tv": "twitch.tv",
	"m.twitch.tv":   "twitch.tv",

	"kick.com":     "kick.com",
	"www.kick.com": "kick.com",
}

// ResolveCanonicalDomain returns the canonical domain for host.
//
// host should be a hostname without port.
func ResolveCanonicalDomain(host string) string {
	h := normalizeHost(host)
	if h == "" {
		return ""
	}
	if c, ok := canonicalDomainByHost[h]; ok {
		return c
	}
	return h
}

// NamespaceUUIDForDomain returns a deterministic UUIDv5 namespace for a domain.
// Example: uuid.NewSHA1(uuid.NameSpaceDNS, []byte("youtube.com")).
func NamespaceUUIDForDomain(domain string) uuid.UUID {
	d := strings.TrimSpace(strings.ToLower(domain))
	d = strings.TrimSuffix(d, ".")
	return uuid.NewSHA1(uuid.NameSpaceDNS, []byte(d))
}

// VideoUUID returns a deterministic UUIDv5 for a (domain, videoID) pair.
//
// The name string is exactly "{videoID}"; the domain is already scoped by the namespace.
func VideoUUID(domain string, videoID string) uuid.UUID {
	d := strings.TrimSpace(strings.ToLower(domain))
	d = strings.TrimSuffix(d, ".")
	v := strings.TrimSpace(videoID)

	ns := NamespaceUUIDForDomain(d)
	return uuid.NewSHA1(ns, []byte(v))
}

type ExpandResult struct {
	ExpandedURL     string
	ExpandedHost    string
	CanonicalDomain string
}

// ExpandAndCanonicalizeURL best-effort expands a URL by following redirects,
// then returns the expanded URL + host and canonical domain.
//
// This is intended to handle URL shorteners and common redirectors.
func ExpandAndCanonicalizeURL(ctx context.Context, raw string) (ExpandResult, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ExpandResult{}, errors.New("missing url")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return ExpandResult{}, err
	}
	if u.Scheme == "" {
		// Best effort: treat as https.
		u, err = url.Parse("https://" + raw)
		if err != nil {
			return ExpandResult{}, err
		}
	}

	expanded := u
	if expanded.Scheme == "http" || expanded.Scheme == "https" {
		if u2, ok := followRedirects(ctx, expanded); ok {
			expanded = u2
		}
	}

	// Remove fragment for stability.
	expanded.Fragment = ""

	host := normalizeHost(expanded.Host)
	canon := ResolveCanonicalDomain(host)

	return ExpandResult{
		ExpandedURL:     expanded.String(),
		ExpandedHost:    host,
		CanonicalDomain: canon,
	}, nil
}

// NormalizeSourceURL normalizes a user-provided or yt-dlp-provided URL for stable
// storage and deduplication.
//
// It canonicalizes the host (e.g. twitter.com -> x.com) and strips non-essential
// fragments and query parameters that commonly vary (timestamps, tracking).
//
// For known video sources:
// - youtube.com: normalizes to https://youtube.com/watch?v={id} (keeps only v=)
// - twitch.tv: strips all query params
// - x.com: strips all query params
// - kick.com: strips all query params
//
// For unknown hosts, it removes fragments but preserves query params.
func NormalizeSourceURL(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", errors.New("missing url")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", "", err
	}
	if u.Scheme == "" {
		u, err = url.Parse("https://" + raw)
		if err != nil {
			return "", "", err
		}
	}

	// Remove fragment for stability.
	u.Fragment = ""
	// Drop userinfo.
	u.User = nil

	origHost := normalizeHost(u.Host)
	canon := ResolveCanonicalDomain(origHost)

	// For some sources (notably YouTube shortlinks), we need to extract the ID
	// before we rewrite the host.
	youtubeID := ""
	if canon == "youtube.com" {
		if id, _ := ExtractYouTubeVideoID(u.String()); strings.TrimSpace(id) != "" {
			youtubeID = strings.TrimSpace(id)
		}
	}

	if canon != "" {
		u.Host = canon
	}

	// Prefer https for http(s) URLs.
	if u.Scheme == "http" || u.Scheme == "https" {
		u.Scheme = "https"
	}

	// Normalize path trailing slash (but keep root as "/").
	u.Path = trimTrailingSlash(u.Path)

	switch canon {
	case "youtube.com":
		if youtubeID != "" {
			u.Path = "/watch"
			u.RawQuery = "v=" + url.QueryEscape(youtubeID)
		} else {
			// If we can't extract a video ID, don't destructively drop query params.
			// Still keep the host canonicalization and fragment stripping.
		}
	case "twitch.tv", "x.com", "kick.com":
		u.RawQuery = ""
	}

	return u.String(), canon, nil
}

func normalizeHost(hostport string) string {
	h := strings.TrimSpace(strings.ToLower(hostport))
	if h == "" {
		return ""
	}
	// url.URL.Host may include port.
	if strings.Contains(h, ":") {
		if parsed, err := url.Parse("//" + h); err == nil {
			if parsed.Hostname() != "" {
				h = parsed.Hostname()
			}
		}
	}
	h = strings.TrimSuffix(h, ".")
	return h
}

func trimTrailingSlash(p string) string {
	if p == "" {
		return ""
	}
	if p == "/" {
		return "/"
	}
	return strings.TrimRight(p, "/")
}

func followRedirects(ctx context.Context, u *url.URL) (*url.URL, bool) {
	client := &http.Client{
		Timeout: 6 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 8 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, false
	}
	req.Header.Set("User-Agent", "rewind-ingest")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	_ = resp.Body.Close()

	finalURL := resp.Request.URL
	if finalURL == nil {
		return nil, false
	}

	// If we ended up without a host, treat as failure.
	if strings.TrimSpace(finalURL.Host) == "" {
		return nil, false
	}

	return finalURL, true
}

// ExtractYouTubeVideoID extracts the YouTube video ID from a URL.
// Returns empty string and error if not a valid YouTube URL or ID cannot be extracted.
func ExtractYouTubeVideoID(urlStr string) (string, error) {
	urlStr = strings.TrimSpace(urlStr)
	if urlStr == "" {
		return "", errors.New("empty url")
	}

	u, err := url.Parse(urlStr)
	if err != nil {
		return "", err
	}

	host := normalizeHost(u.Host)

	// Handle youtu.be shortlinks
	if host == "youtu.be" {
		id := firstPathSegment(u.Path)
		if id == "" {
			return "", errors.New("not a youtube url or video id not found")
		}
		return id, nil
	}

	// Handle youtube.com URLs (including www/m.)
	if ResolveCanonicalDomain(host) == "youtube.com" || strings.Contains(host, "youtube.com") {
		// Check for /watch?v= format
		if q := u.Query().Get("v"); q != "" {
			return q, nil
		}
		// Check for /embed/ or /v/ format
		if strings.HasPrefix(u.Path, "/embed/") {
			id := firstPathSegment(strings.TrimPrefix(u.Path, "/embed/"))
			if id != "" {
				return id, nil
			}
		}
		if strings.HasPrefix(u.Path, "/v/") {
			id := firstPathSegment(strings.TrimPrefix(u.Path, "/v/"))
			if id != "" {
				return id, nil
			}
		}
		// Common modern formats.
		if strings.HasPrefix(u.Path, "/shorts/") {
			id := firstPathSegment(strings.TrimPrefix(u.Path, "/shorts/"))
			if id != "" {
				return id, nil
			}
		}
		if strings.HasPrefix(u.Path, "/live/") {
			id := firstPathSegment(strings.TrimPrefix(u.Path, "/live/"))
			if id != "" {
				return id, nil
			}
		}
	}

	return "", errors.New("not a youtube url or video id not found")
}

func firstPathSegment(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return ""
	}
	seg, _, _ := strings.Cut(p, "/")
	return strings.TrimSpace(seg)
}
