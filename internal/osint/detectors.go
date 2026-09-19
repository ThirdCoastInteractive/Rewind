package osint

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
)

const (
	batchLimit           = 500
	minCopyPasteLen      = 40
	copyPasteHammingMax  = 3
	toxicityLookback     = 20
	toxicityActiveLimit  = 2000
	sockCandidateLimit   = 80
	stylePairScanLimit   = 80
	styleRefreshMaxN     = 64
	styleTextCap         = 40
	raidVideoLimit       = 40
	raidLookbackInterval = "7 days"
)

type settings struct {
	toxicityFlag float64
	raidRatio    float64
	styleMinN    int
}

type scoredComment struct {
	CommentID   uuid.UUID
	VideoID     uuid.UUID
	CommenterID uuid.UUID
	Text        string
	PublishedAt time.Time
	Simhash     *int64
	Toxicity    *float64
}

func (s store) fillMissingSimhashes(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `
SELECT vc.id, vc.text, cs.simhash
FROM comment_scores cs
JOIN video_comments vc ON vc.id = cs.comment_id
WHERE cs.simhash IS NULL AND vc.text IS NOT NULL AND length(vc.text) > 0
ORDER BY cs.scored_at DESC NULLS LAST
LIMIT $1`, batchLimit)
	if err != nil {
		return err
	}
	defer rows.Close()

	type item struct {
		id   uuid.UUID
		text string
	}
	var items []item
	for rows.Next() {
		var it item
		var existing *int64
		if err := rows.Scan(&it.id, &it.text, &existing); err != nil {
			return err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, it := range items {
		h := Simhash64(NormalizeText(it.text))
		if err := s.updateCommentSimhash(ctx, it.id, h); err != nil {
			return err
		}
	}
	return nil
}

func (s store) refreshCommenterStyles(ctx context.Context) error {
	// Pick the commenters first (LIMIT), then at most styleTextCap texts each.
	// The previous GROUP BY joined every comment onto every under-trained
	// commenter (~1.1M × 2.6M) before applying LIMIT.
	rows, err := s.pool.Query(ctx, `
WITH need AS (
  SELECT id, style_features, style_n
  FROM commenters
  WHERE style_n < $1
  ORDER BY last_seen DESC NULLS LAST
  LIMIT $2
)
SELECT n.id, n.style_features, n.style_n,
       ARRAY(
         SELECT vc.text
         FROM video_comments vc
         WHERE vc.commenter_id = n.id
           AND vc.text IS NOT NULL
           AND length(vc.text) > 0
         ORDER BY vc.published_at DESC NULLS LAST
         LIMIT $3
       )
FROM need n`, styleRefreshMaxN, batchLimit/5, styleTextCap)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id      uuid.UUID
			featRaw []byte
			styleN  int
			texts   []string
		)
		if err := rows.Scan(&id, &featRaw, &styleN, &texts); err != nil {
			return err
		}
		if len(texts) == 0 {
			continue
		}
		next := StyleFeatures(texts)
		prior := map[string]float64{}
		if len(featRaw) > 0 {
			_ = json.Unmarshal(featRaw, &prior)
		}
		merged := MergeStyleFeatures(prior, styleN, next, len(texts))
		newN := styleN + len(texts)
		var hash *uint64
		h := Simhash64(NormalizeText(texts[0]))
		hash = &h
		if err := s.updateCommenterStyle(ctx, id, merged, newN, hash); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s store) detectCopyPasteCampaigns(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `
SELECT vc.id, vc.video_id, vc.commenter_id, vc.text, vc.published_at, cs.simhash
FROM comment_scores cs
JOIN video_comments vc ON vc.id = cs.comment_id
WHERE cs.simhash IS NOT NULL
  AND vc.commenter_id IS NOT NULL
  AND length(COALESCE(vc.text, '')) >= $1
ORDER BY cs.scored_at DESC NULLS LAST
LIMIT $2`, minCopyPasteLen, batchLimit)
	if err != nil {
		return err
	}
	defer rows.Close()

	type row struct {
		scoredComment
		norm string
		hash uint64
	}
	var items []row
	byHash := map[uint64][]int{}
	for rows.Next() {
		var r row
		var sh int64
		if err := rows.Scan(&r.CommentID, &r.VideoID, &r.CommenterID, &r.Text, &r.PublishedAt, &sh); err != nil {
			return err
		}
		r.hash = uint64(sh)
		r.norm = NormalizeText(r.Text)
		if len(r.norm) < minCopyPasteLen {
			continue
		}
		idx := len(items)
		items = append(items, r)
		byHash[r.hash] = append(byHash[r.hash], idx)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	visited := map[uint64]bool{}
	for hash := range byHash {
		if visited[hash] {
			continue
		}
		// Expand cluster with Hamming ≤ 3 neighbors present in this batch.
		clusterIdx := map[int]struct{}{}
		seedHashes := []uint64{hash}
		for _, h := range seedHashes {
			visited[h] = true
			for _, i := range byHash[h] {
				clusterIdx[i] = struct{}{}
			}
			for other, oidxs := range byHash {
				if visited[other] {
					continue
				}
				if HammingDistance(h, other) <= copyPasteHammingMax {
					visited[other] = true
					seedHashes = append(seedHashes, other)
					for _, i := range oidxs {
						clusterIdx[i] = struct{}{}
					}
				}
			}
		}
		commenters := map[uuid.UUID]struct{}{}
		videos := map[uuid.UUID]struct{}{}
		var commentIDs []string
		var first, last time.Time
		var sampleNorm string
		var repHash uint64
		for i := range clusterIdx {
			it := items[i]
			commenters[it.CommenterID] = struct{}{}
			videos[it.VideoID] = struct{}{}
			commentIDs = append(commentIDs, it.CommentID.String())
			if first.IsZero() || (!it.PublishedAt.IsZero() && it.PublishedAt.Before(first)) {
				first = it.PublishedAt
			}
			if last.IsZero() || it.PublishedAt.After(last) {
				last = it.PublishedAt
			}
			if sampleNorm == "" {
				sampleNorm = it.norm
				repHash = it.hash
			}
		}
		if len(commenters) < 3 && len(videos) < 2 {
			continue
		}
		if first.IsZero() {
			first = time.Now().UTC()
		}
		if last.IsZero() {
			last = first
		}
		sort.Strings(commentIDs)
		evidence := map[string]any{
			"comment_ids":     commentIDs,
			"commenter_count": len(commenters),
			"video_count":     len(videos),
			"why":             "identical/near-identical simhash across multiple authors or videos",
			"normalized_text": sampleNorm,
			"hamming_max":     copyPasteHammingMax,
		}
		campID, err := s.upsertCampaign(ctx, "copypaste", repHash, sampleNorm, first, last, len(clusterIdx), len(commenters), len(videos), evidence)
		if err != nil {
			return err
		}
		for i := range clusterIdx {
			it := items[i]
			if err := s.addCampaignMember(ctx, campID, it.CommentID, it.CommenterID); err != nil {
				return err
			}
		}
		nk := FlagNaturalKey("campaign", nil, nil, &campID, fmt.Sprintf("%d", repHash))
		if err := s.upsertFlag(ctx, "campaign", nil, nil, &campID, float64(len(commenters)), nk, evidence); err != nil {
			return err
		}
	}
	return nil
}

func (s store) detectRaids(ctx context.Context, raidRatio float64) error {
	rows, err := s.pool.Query(ctx, `
SELECT video_id
FROM video_comments
WHERE published_at > NOW() - $1::interval
  AND commenter_id IS NOT NULL
GROUP BY video_id
ORDER BY max(published_at) DESC
LIMIT $2`, raidLookbackInterval, raidVideoLimit)
	if err != nil {
		return err
	}
	defer rows.Close()
	var videoIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return err
		}
		videoIDs = append(videoIDs, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	for _, videoID := range videoIDs {
		if err := s.detectRaidForVideo(ctx, videoID, raidRatio); err != nil {
			return err
		}
	}
	return nil
}

func (s store) detectRaidForVideo(ctx context.Context, videoID uuid.UUID, raidRatio float64) error {
	rows, err := s.pool.Query(ctx, `
SELECT vc.id, vc.published_at, vc.commenter_id, c.first_seen
FROM video_comments vc
JOIN commenters c ON c.id = vc.commenter_id
WHERE vc.video_id = $1 AND vc.published_at IS NOT NULL AND vc.commenter_id IS NOT NULL
ORDER BY vc.published_at`, videoID)
	if err != nil {
		return err
	}
	defer rows.Close()

	var stamps []CommentStamp
	type meta struct {
		commentID   uuid.UUID
		commenterID uuid.UUID
	}
	metas := map[string]meta{}
	for rows.Next() {
		var (
			cid   uuid.UUID
			pub   time.Time
			crID  uuid.UUID
			first time.Time
		)
		if err := rows.Scan(&cid, &pub, &crID, &first); err != nil {
			return err
		}
		id := cid.String()
		stamps = append(stamps, CommentStamp{
			PublishedAt: pub,
			CommenterID: crID.String(),
			FirstSeen:   first,
			CommentID:   id,
		})
		metas[id] = meta{commentID: cid, commenterID: crID}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	buckets := BucketComments(stamps)
	median := MedianNonemptyBucketCount(buckets)
	for _, b := range buckets {
		ok, frac := IsRaidBurst(b, median, raidRatio)
		if !ok {
			continue
		}
		authors := map[string]struct{}{}
		var commentIDs []string
		var first, last time.Time
		for _, c := range b.Comments {
			authors[c.CommenterID] = struct{}{}
			commentIDs = append(commentIDs, c.CommentID)
			if first.IsZero() || c.PublishedAt.Before(first) {
				first = c.PublishedAt
			}
			if last.IsZero() || c.PublishedAt.After(last) {
				last = c.PublishedAt
			}
		}
		sort.Strings(commentIDs)
		evidence := map[string]any{
			"comment_ids":     commentIDs,
			"video_id":        videoID.String(),
			"bucket_start":    b.Start.UTC().Format(time.RFC3339),
			"bucket_count":    b.Count,
			"median_bucket":   median,
			"raid_ratio":      raidRatio,
			"newcomer_frac":   frac,
			"why":             "15m comment burst ≥ ratio×median with many brand-new authors",
			"commenter_count": len(authors),
		}
		campID, err := s.insertRaidCampaign(ctx, videoID, b.Start, "raid:"+RaidBucketKey(videoID, b.Start.Unix()), first, last, b.Count, len(authors), evidence)
		if err != nil {
			return err
		}
		for _, c := range b.Comments {
			m := metas[c.CommentID]
			if m.commentID == uuid.Nil {
				continue
			}
			if err := s.addCampaignMember(ctx, campID, m.commentID, m.commenterID); err != nil {
				return err
			}
		}
		extra := RaidBucketKey(videoID, b.Start.Unix())
		nk := FlagNaturalKey("raid", nil, &videoID, &campID, extra)
		if err := s.upsertFlag(ctx, "raid", nil, &videoID, &campID, float64(b.Count)/math.Max(median, 1), nk, evidence); err != nil {
			return err
		}
	}
	return nil
}

func (s store) detectNewcomers(ctx context.Context, toxicityFlag float64) error {
	// LIMIT the 1M-row newcomer set *before* per-row toxicity/raid probes.
	rows, err := s.pool.Query(ctx, `
WITH newcomers AS (
  SELECT id, comment_count
  FROM commenters
  WHERE comment_count <= 3
  ORDER BY last_seen DESC NULLS LAST
  LIMIT $1
),
raiders AS (
  SELECT DISTINCT cm.commenter_id
  FROM osint_flags f
  JOIN campaign_members cm ON cm.campaign_id = f.campaign_id
  WHERE f.kind = 'raid' AND f.dismissed_at IS NULL AND cm.commenter_id IS NOT NULL
)
SELECT n.id, n.comment_count,
       COALESCE((
         SELECT avg(cs.toxicity)
         FROM video_comments vc
         JOIN comment_scores cs ON cs.comment_id = vc.id
         WHERE vc.commenter_id = n.id AND cs.toxicity IS NOT NULL
       ), 0),
       EXISTS(SELECT 1 FROM raiders r WHERE r.commenter_id = n.id)
FROM newcomers n`, batchLimit)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id      uuid.UUID
			count   int64
			meanTox float64
			inRaid  bool
		)
		if err := rows.Scan(&id, &count, &meanTox, &inRaid); err != nil {
			return err
		}
		if !inRaid && meanTox < toxicityFlag {
			continue
		}
		why := "low comment_count with elevated toxicity"
		if inRaid {
			why = "low comment_count appearing inside an open raid window"
		}
		evidence := map[string]any{
			"commenter_id":  id.String(),
			"comment_count": count,
			"mean_toxicity": meanTox,
			"in_raid":       inRaid,
			"why":           why,
		}
		nk := FlagNaturalKey("newcomer", &id, nil, nil, "")
		if err := s.upsertFlag(ctx, "newcomer", &id, nil, nil, meanTox, nk, evidence); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s store) detectToxicityBursts(ctx context.Context, toxicityFlag float64) error {
	rows, err := s.pool.Query(ctx, `
WITH active AS (
  SELECT id
  FROM commenters
  WHERE last_seen > NOW() - INTERVAL '30 days'
  ORDER BY last_seen DESC NULLS LAST
  LIMIT $4
)
SELECT a.id, avg(s.toxicity) AS mean_tox,
       array_agg(s.comment_id::text ORDER BY s.ts DESC) AS comment_ids
FROM active a
JOIN LATERAL (
  SELECT cs.toxicity, cs.comment_id, COALESCE(vc.published_at, cs.scored_at) AS ts
  FROM video_comments vc
  JOIN comment_scores cs ON cs.comment_id = vc.id
  WHERE vc.commenter_id = a.id AND cs.toxicity IS NOT NULL
  ORDER BY COALESCE(vc.published_at, cs.scored_at) DESC
  LIMIT $1
) s ON true
GROUP BY a.id
HAVING count(*) >= 3 AND avg(s.toxicity) >= $2
ORDER BY avg(s.toxicity) DESC
LIMIT $3`, toxicityLookback, toxicityFlag, batchLimit/2, toxicityActiveLimit)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id         uuid.UUID
			meanTox    float64
			commentIDs []string
		)
		if err := rows.Scan(&id, &meanTox, &commentIDs); err != nil {
			return err
		}
		if len(commentIDs) > toxicityLookback {
			commentIDs = commentIDs[:toxicityLookback]
		}
		evidence := map[string]any{
			"commenter_id":  id.String(),
			"comment_ids":   commentIDs,
			"mean_toxicity": meanTox,
			"window":        toxicityLookback,
			"why":           fmt.Sprintf("mean toxicity of last %d scored comments ≥ %.2f", toxicityLookback, toxicityFlag),
		}
		nk := FlagNaturalKey("toxicity_burst", &id, nil, nil, "")
		if err := s.upsertFlag(ctx, "toxicity_burst", &id, nil, nil, meanTox, nk, evidence); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s store) detectSockSuggestions(ctx context.Context, styleMinN int) error {
	rows, err := s.pool.Query(ctx, `
SELECT id, style_features, style_n
FROM commenters
WHERE style_n >= $1 AND style_features <> '{}'::jsonb
ORDER BY last_seen DESC NULLS LAST
LIMIT $2`, styleMinN, sockCandidateLimit)
	if err != nil {
		return err
	}
	defer rows.Close()

	type cand struct {
		id    uuid.UUID
		feat  map[string]float64
		style int
	}
	var cands []cand
	for rows.Next() {
		var (
			id     uuid.UUID
			raw    []byte
			styleN int
			feat   map[string]float64
		)
		if err := rows.Scan(&id, &raw, &styleN); err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &feat); err != nil || len(feat) == 0 {
			continue
		}
		cands = append(cands, cand{id: id, feat: feat, style: styleN})
	}
	if err := rows.Err(); err != nil {
		return err
	}

	linked, err := s.userLinkSet(ctx)
	if err != nil {
		return err
	}

	limit := stylePairScanLimit
	if len(cands) < limit {
		limit = len(cands)
	}
	for i := 0; i < limit; i++ {
		for j := i + 1; j < limit; j++ {
			a, b := cands[i], cands[j]
			dist := StyleDistance(a.feat, b.feat)
			if dist > ConservativeStyleThreshold {
				continue
			}
			lo, hi := OrderedCommenterPair(a.id, b.id)
			if _, ok := linked[[2]uuid.UUID{lo, hi}]; ok {
				// Operator already asserted identity; do not re-flag style socks.
				continue
			}
			overlap := OverlappingFeatureNames(a.feat, b.feat, ConservativeStyleThreshold)
			if len(overlap) == 0 {
				continue
			}
			score := 1.0 - dist
			evidence := map[string]any{
				"a_id":                 a.id.String(),
				"b_id":                 b.id.String(),
				"distance":             dist,
				"overlapping_features": overlap,
				"why":                  "style feature distance below conservative threshold (hypothesis only; not a merge)",
			}
			if err := s.upsertStyleLink(ctx, a.id, b.id, score, evidence); err != nil {
				return err
			}
			nk := SockNaturalKey(a.id, b.id)
			if err := s.upsertFlag(ctx, "sock_suggest", &lo, nil, nil, score, nk, evidence); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s store) maintainCommentedByEdges(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `
WITH sparse AS (
  SELECT id FROM commenters WHERE channel_id IS NOT NULL
  UNION
  SELECT commenter_id FROM osint_flags
  WHERE dismissed_at IS NULL AND commenter_id IS NOT NULL
  UNION
  SELECT a_id FROM commenter_links WHERE kind = 'user'
  UNION
  SELECT b_id FROM commenter_links WHERE kind = 'user'
)
SELECT DISTINCT v.channel_row_id, s.id, vc.video_id
FROM sparse s
JOIN video_comments vc ON vc.commenter_id = s.id
JOIN videos v ON v.id = vc.video_id
WHERE v.channel_row_id IS NOT NULL
ORDER BY v.channel_row_id, s.id
LIMIT $1`, batchLimit)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var channelID, commenterID, videoID uuid.UUID
		if err := rows.Scan(&channelID, &commenterID, &videoID); err != nil {
			return err
		}
		evidence := map[string]any{
			"video_id": videoID.String(),
			"why":      "commenter is watchlisted, flagged, user-linked, or has channel_id",
		}
		if err := s.upsertCommentedByEdge(ctx, channelID, commenterID, 1, &videoID, evidence); err != nil {
			return err
		}
	}
	return rows.Err()
}

// tablesReady is true when the contract OSINT tables exist.
func (s store) tablesReady(ctx context.Context) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM information_schema.tables
  WHERE table_schema = 'public' AND table_name = 'osint_flags'
)`).Scan(&ok)
	return ok, err
}
