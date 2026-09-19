package comments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/videoid"
	"thirdcoast.systems/rewind/pkg/ytdlp"
)

// XSource is commenters.source for public X/Twitter replies.
const XSource = "x.com"

// ErrNoReplies is the visible failure when the extractor returns no replies.
var ErrNoReplies = errors.New("no public replies found")

// InfoGetter is the yt-dlp metadata surface used to fetch public replies.
type InfoGetter interface {
	GetInfo(ctx context.Context, url string, extraArgs ...string) (*ytdlp.Info, error)
}

// IndexXReplies fetches public replies for an X status URL and ingests them
// as comments on videoID. Empty extractor output is a hard error, not success.
func IndexXReplies(ctx context.Context, q Store, getter InfoGetter, videoID pgtype.UUID, statusURL string) error {
	if q == nil {
		return fmt.Errorf("index x replies: nil store")
	}
	if getter == nil {
		return fmt.Errorf("index x replies: nil fetcher")
	}
	normalized, canon, err := videoid.NormalizeSourceURL(statusURL)
	if err != nil {
		return fmt.Errorf("index x replies: %w", err)
	}
	if canon != "x.com" || !strings.Contains(normalized, "/status/") {
		return fmt.Errorf("index x replies: not an X status URL")
	}
	info, err := getter.GetInfo(ctx, normalized, "--write-comments")
	if err != nil {
		return err
	}
	if info == nil || commentsArrayLen(info.Raw) == 0 {
		return fmt.Errorf("%w for %s", ErrNoReplies, normalized)
	}
	return IngestFromInfoJSON(ctx, q, videoID, XSource, info.Raw)
}

func commentsArrayLen(raw []byte) int {
	var envelope struct {
		Comments json.RawMessage `json:"comments"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return 0
	}
	if len(envelope.Comments) == 0 || string(envelope.Comments) == "null" {
		return 0
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(envelope.Comments, &arr); err != nil {
		return 0
	}
	return len(arr)
}
