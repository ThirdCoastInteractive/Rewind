package osint

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ChannelEngagement is harvested comment activity on a channel, split into
// organic vs campaign/sock-suspect. Sock flags are observations, not proof.
type ChannelEngagement struct {
	Videos            int64   `json:"videos"`
	VideosCommented   int64   `json:"videos_commented"`
	ViewsCommented    int64   `json:"views_commented"`
	PlatformComments  int64   `json:"platform_comments"`
	HarvestedComments int64   `json:"harvested_comments"`
	OrganicComments   int64   `json:"organic_comments"`
	CampaignComments  int64   `json:"campaign_comments"`
	SockComments      int64   `json:"sock_comments"`
	UniqueCommenters  int64   `json:"unique_commenters"`
	UniqueOrganic     int64   `json:"unique_organic"`
	SockCommenters    int64   `json:"sock_commenters"`
	HarvestedPer1k    float64 `json:"harvested_per_1k_views"`
	OrganicPer1k      float64 `json:"organic_per_1k_views"`
	Inflation         float64 `json:"inflation"`
}

type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Finalize fills per-1k rates and inflation from the raw counts.
func (e *ChannelEngagement) Finalize() {
	if e.ViewsCommented > 0 {
		e.HarvestedPer1k = float64(e.HarvestedComments) * 1000 / float64(e.ViewsCommented)
		e.OrganicPer1k = float64(e.OrganicComments) * 1000 / float64(e.ViewsCommented)
	}
	if e.HarvestedComments > 0 {
		e.Inflation = 1 - float64(e.OrganicComments)/float64(e.HarvestedComments)
		if e.Inflation < 0 {
			e.Inflation = 0
		}
	}
}

// Organic reports comments that are not in a detected campaign and not from an
// open sock_suggest flag. Campaign membership and sock flags can overlap.
func Organic(harvested, campaign, sockOverlapFree int64) int64 {
	n := harvested - campaign - sockOverlapFree
	if n < 0 {
		return 0
	}
	return n
}

// LoadChannelEngagement aggregates harvested comments for an uploader and/or
// channel row. Views are summed only on videos that have harvested comments.
func LoadChannelEngagement(ctx context.Context, q rowQuerier, uploader string, channelID pgtype.UUID) (ChannelEngagement, error) {
	var e ChannelEngagement
	if q == nil || (uploader == "" && !channelID.Valid) {
		return e, nil
	}
	err := q.QueryRow(ctx, `
WITH vids AS (
  SELECT v.id, COALESCE(v.view_count, 0)::bigint AS view_count,
         COALESCE(v.comment_count, 0)::bigint AS platform_comments
  FROM videos v
  WHERE ($1::text <> '' AND v.uploader = $1)
     OR ($2::uuid IS NOT NULL AND v.channel_row_id = $2)
),
flagged AS (
  SELECT DISTINCT commenter_id
  FROM osint_flags
  WHERE dismissed_at IS NULL
    AND kind = 'sock_suggest'
    AND commenter_id IS NOT NULL
),
scored AS (
  SELECT
    vc.commenter_id,
    (cm.comment_id IS NOT NULL) AS in_campaign,
    (f.commenter_id IS NOT NULL) AS sock_suspect
  FROM vids
  JOIN video_comments vc ON vc.video_id = vids.id
  LEFT JOIN campaign_members cm ON cm.comment_id = vc.id
  LEFT JOIN flagged f ON f.commenter_id = vc.commenter_id
)
SELECT
  (SELECT COUNT(*) FROM vids),
  (SELECT COUNT(*) FROM vids v WHERE EXISTS (SELECT 1 FROM video_comments c WHERE c.video_id = v.id)),
  (SELECT COALESCE(SUM(view_count), 0) FROM vids v WHERE EXISTS (SELECT 1 FROM video_comments c WHERE c.video_id = v.id)),
  (SELECT COALESCE(SUM(platform_comments), 0) FROM vids),
  COUNT(*)::bigint,
  COUNT(*) FILTER (WHERE in_campaign)::bigint,
  COUNT(*) FILTER (WHERE sock_suspect)::bigint,
  COUNT(*) FILTER (WHERE NOT in_campaign AND NOT sock_suspect)::bigint,
  COUNT(DISTINCT commenter_id) FILTER (WHERE commenter_id IS NOT NULL)::bigint,
  COUNT(DISTINCT commenter_id) FILTER (WHERE commenter_id IS NOT NULL AND NOT in_campaign AND NOT sock_suspect)::bigint,
  COUNT(DISTINCT commenter_id) FILTER (WHERE commenter_id IS NOT NULL AND sock_suspect)::bigint
FROM scored
`, uploader, channelID).Scan(
		&e.Videos,
		&e.VideosCommented,
		&e.ViewsCommented,
		&e.PlatformComments,
		&e.HarvestedComments,
		&e.CampaignComments,
		&e.SockComments,
		&e.OrganicComments,
		&e.UniqueCommenters,
		&e.UniqueOrganic,
		&e.SockCommenters,
	)
	if err != nil {
		return ChannelEngagement{}, err
	}
	e.Finalize()
	return e, nil
}
