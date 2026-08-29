package channellinks

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/channelid"
	"thirdcoast.systems/rewind/internal/db"
)

// Record upserts one directed edge per hit from fromChannel. Hits whose URL
// already maps to an archived channel store to_channel_id; others keep to_url
// for later ResolveChannelEdges. Commented hits are skipped unless the target
// channel is already archived.
func Record(ctx context.Context, q *db.Queries, fromChannel pgtype.UUID, videoID pgtype.UUID, hits []Hit) error {
	if q == nil || !fromChannel.Valid {
		return nil
	}
	for _, hit := range hits {
		if err := recordOne(ctx, q, fromChannel, videoID, hit); err != nil {
			return err
		}
	}
	return nil
}

func recordOne(ctx context.Context, q *db.Queries, fromChannel pgtype.UUID, videoID pgtype.UUID, hit Hit) error {
	raw := strings.TrimSpace(hit.URL)
	if raw == "" || strings.TrimSpace(hit.Kind) == "" {
		return nil
	}
	identity := channelid.FromURL(raw)
	toURL := identity.CanonicalURL
	if toURL == "" {
		toURL = raw
	}

	toChannel, err := lookupTargetChannel(ctx, q, identity, toURL)
	if err != nil {
		return err
	}

	if hit.Kind == KindCommented && !toChannel.Valid {
		return nil
	}
	if toChannel.Valid && toChannel == fromChannel {
		return nil
	}

	edgeKey := toURL
	if toChannel.Valid {
		edgeKey = toChannel.String()
	}

	evidence := strings.TrimSpace(hit.Evidence)
	n, err := q.BumpChannelEdge(ctx, &db.BumpChannelEdgeParams{
		Evidence:      evidence,
		ToChannelID:   toChannel,
		VideoID:       videoID,
		FromChannelID: fromChannel,
		Kind:          hit.Kind,
		EdgeKey:       edgeKey,
	})
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}

	err = q.InsertChannelEdge(ctx, &db.InsertChannelEdgeParams{
		FromChannelID: fromChannel,
		ToChannelID:   toChannel,
		ToURL:         toURL,
		Kind:          hit.Kind,
		Evidence:      evidence,
		VideoID:       videoID,
	})
	if err != nil && !db.IsUniqueViolationErr(err) {
		return err
	}
	return nil
}

// HarvestVideo parses title+description and records edges from fromChannel,
// then harvests comment-author profile URLs on the same video.
func HarvestVideo(ctx context.Context, q *db.Queries, fromChannel pgtype.UUID, videoID pgtype.UUID, title, description string) error {
	if q == nil || !fromChannel.Valid {
		return nil
	}
	srcPlatform := ""
	if ch, err := q.GetChannel(ctx, fromChannel); err == nil && ch != nil {
		srcPlatform = ch.Platform
		if srcPlatform == "other" && isTwitterURL(ch.CanonicalURL) {
			srcPlatform = "twitter"
		}
	}
	if err := Record(ctx, q, fromChannel, videoID, ParseOn(srcPlatform, title+"\n"+description)); err != nil {
		return err
	}
	return HarvestComments(ctx, q, fromChannel, videoID)
}

// HarvestComments records KindCommented edges from distinct comment author URLs.
func HarvestComments(ctx context.Context, q *db.Queries, fromChannel pgtype.UUID, videoID pgtype.UUID) error {
	if q == nil || !fromChannel.Valid || !videoID.Valid {
		return nil
	}
	urls, err := q.ListDistinctCommentAuthorURLs(ctx, videoID)
	if err != nil {
		return err
	}
	hits := make([]Hit, 0, len(urls))
	for _, u := range urls {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		hits = append(hits, Hit{URL: u, Kind: KindCommented, Evidence: "comment author " + u})
	}
	return Record(ctx, q, fromChannel, videoID, hits)
}

func lookupTargetChannel(ctx context.Context, q *db.Queries, identity channelid.Identity, toURL string) (pgtype.UUID, error) {
	try := func(ch *db.Channel, err error) (pgtype.UUID, bool, error) {
		if err == nil && ch != nil && ch.ID.Valid {
			return ch.ID, true, nil
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return pgtype.UUID{}, false, err
		}
		return pgtype.UUID{}, false, nil
	}

	if identity.Platform != "other" && identity.Key != "" && identity.Key != "unknown" {
		id, ok, err := try(q.GetChannelByIdentity(ctx, &db.GetChannelByIdentityParams{
			Platform:    identity.Platform,
			IdentityKey: identity.Key,
		}))
		if err != nil || ok {
			return id, err
		}
	}
	if identity.Platform != "" && identity.ChannelID != "" {
		id, ok, err := try(q.GetChannelByChannelID(ctx, &db.GetChannelByChannelIDParams{
			Platform:  identity.Platform,
			ChannelID: identity.ChannelID,
		}))
		if err != nil || ok {
			return id, err
		}
	}
	if toURL != "" {
		id, ok, err := try(q.GetChannelByCanonicalURL(ctx, toURL))
		if err != nil || ok {
			return id, err
		}
		if h := twitterHandleFromURL(toURL); h != "" {
			id, ok, err = try(q.GetChannelByTwitterHandle(ctx, &h))
			if err != nil || ok {
				return id, err
			}
			id, ok, err = try(q.GetChannelByCanonicalURL(ctx, "https://twitter.com/"+h))
			if err != nil || ok {
				return id, err
			}
			id, ok, err = try(q.GetChannelByCanonicalURL(ctx, "https://x.com/"+h))
			if err != nil || ok {
				return id, err
			}
		}
	}
	handle := strings.TrimPrefix(identity.Key, "@")
	if identity.Platform != "" && handle != "" && handle != "unknown" {
		id, ok, err := try(q.GetChannelByHandleGuess(ctx, &db.GetChannelByHandleGuessParams{
			Platform: identity.Platform,
			Handle:   handle,
		}))
		if err != nil || ok {
			return id, err
		}
	}
	return pgtype.UUID{}, nil
}

func isTwitterURL(raw string) bool {
	s := strings.ToLower(raw)
	return strings.Contains(s, "twitter.com/") || strings.Contains(s, "x.com/")
}

func twitterHandleFromURL(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "www.")
	switch {
	case strings.HasPrefix(s, "twitter.com/"):
		s = strings.TrimPrefix(s, "twitter.com/")
	case strings.HasPrefix(s, "x.com/"):
		s = strings.TrimPrefix(s, "x.com/")
	case strings.HasPrefix(s, "youtube.com/@"):
		s = strings.TrimPrefix(s, "youtube.com/@")
	case strings.HasPrefix(s, "youtube.com/"):
		return ""
	default:
		return ""
	}
	s = strings.TrimPrefix(s, "@")
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return ""
	}
	for _, r := range s {
		if !(r == '_' || r == '.' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return ""
		}
	}
	return s
}
