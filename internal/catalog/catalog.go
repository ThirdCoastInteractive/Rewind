// Package catalog creates durable channel-history crawls.
package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
)

// Feed is one separately pageable surface exposed by a channel platform.
type Feed struct{ Kind, URL string }

// DefaultPageSize is the catalog crawl page width when page_size is unset.
const DefaultPageSize = 100

// PageRange returns the inclusive [start, end] playlist indices for a crawl page.
// next is 1-based next_page_index. The first page has no overlap; later pages
// rewind by overlap so items near the boundary are not missed. pageSize <= 0
// becomes DefaultPageSize; negative overlap is treated as 0; next < 1 as 1.
func PageRange(next, pageSize, overlap int) (start, end int) {
	if pageSize <= 0 {
		pageSize = DefaultPageSize
	}
	if overlap < 0 {
		overlap = 0
	}
	if next < 1 {
		next = 1
	}
	start = next
	if next > 1 {
		start = max(1, next-overlap)
	}
	end = next + pageSize - 1
	return start, end
}

// SkipPlatform is true for surfaces Rewind will not crawl (X/Twitter anti-bot).
func SkipPlatform(platform string) bool {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "twitter", "x":
		return true
	default:
		return false
	}
}

// Feeds returns the catalog surfaces for a channel.
func Feeds(ch *db.Channel) []Feed {
	if ch == nil || strings.TrimSpace(ch.CanonicalURL) == "" {
		return nil
	}
	if SkipPlatform(ch.Platform) {
		return nil
	}
	base := strings.TrimRight(strings.TrimSpace(ch.CanonicalURL), "/")
	if ch.Platform == "youtube" {
		for _, suffix := range []string{"/videos", "/shorts", "/streams"} {
			base = strings.TrimSuffix(base, suffix)
		}
		return []Feed{{"videos", base + "/videos"}, {"shorts", base + "/shorts"}, {"streams", base + "/streams"}}
	}
	return []Feed{{"videos", base}}
}

// IndexChannel creates or refreshes all durable feeds for a channel.
func IndexChannel(ctx context.Context, q *db.Queries, ch *db.Channel, userID pgtype.UUID, refresh bool) ([]*db.CatalogCrawl, error) {
	if ch != nil && SkipPlatform(ch.Platform) {
		return nil, fmt.Errorf("Twitter/X is not crawled")
	}
	feeds := Feeds(ch)
	if len(feeds) == 0 {
		return nil, fmt.Errorf("channel has no canonical feed URL")
	}
	out := make([]*db.CatalogCrawl, 0, len(feeds))
	for _, feed := range feeds {
		row, err := q.CreateCatalogCrawl(ctx, &db.CreateCatalogCrawlParams{ChannelID: ch.ID, RequestedBy: userID, FeedURL: feed.URL, FeedKind: feed.Kind, Platform: ch.Platform, Refresh: refresh})
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

// IndexCreator creates channel feeds for every channel linked to creatorID.
func IndexCreator(ctx context.Context, q *db.Queries, creatorID, userID pgtype.UUID, refresh bool) ([]*db.CatalogCrawl, error) {
	channels, err := q.ListChannelsByCreator(ctx, creatorID)
	if err != nil {
		return nil, err
	}
	var out []*db.CatalogCrawl
	for _, ch := range channels {
		if SkipPlatform(ch.Platform) {
			continue
		}
		rows, err := IndexChannel(ctx, q, ch, userID, refresh)
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	return out, nil
}
