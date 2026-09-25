package content

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
)

// Same statements as ListCommenterNetworkNodes / ListCommenterNetworkEdges in
// network_queries.sql. Run via pool until 00087 lands and sqlc can generate.
const listCommenterNetworkNodesSQL = `
WITH sparse AS (
  SELECT c.id FROM commenters c WHERE c.channel_id IS NOT NULL
  UNION
  SELECT w.commenter_id FROM commenter_watchlist w
  UNION
  SELECT f.commenter_id FROM osint_flags f
  WHERE f.dismissed_at IS NULL AND f.commenter_id IS NOT NULL
  UNION
  SELECT l.a_id FROM commenter_links l WHERE l.kind = 'user'
  UNION
  SELECT l.b_id FROM commenter_links l WHERE l.kind = 'user'
)
SELECT c.id,
       c.source,
       c.display_name,
       c.author_url,
       c.comment_count,
       c.channel_id,
       EXISTS (
         SELECT 1 FROM commenter_watchlist w
         WHERE w.commenter_id = c.id AND w.user_id = $1
       ) AS watchlisted,
       COALESCE((
         SELECT array_agg(DISTINCT f.kind ORDER BY f.kind)
         FROM osint_flags f
         WHERE f.commenter_id = c.id AND f.dismissed_at IS NULL
       ), '{}'::text[])::text[] AS open_flag_kinds
FROM commenters c
WHERE c.id IN (SELECT id FROM sparse)
ORDER BY c.comment_count DESC, c.display_name, c.id
LIMIT 200`

const listCommenterNetworkEdgesSQL = `
WITH sparse AS (
  SELECT c.id FROM commenters c WHERE c.channel_id IS NOT NULL
  UNION
  SELECT w.commenter_id FROM commenter_watchlist w
  UNION
  SELECT f.commenter_id FROM osint_flags f
  WHERE f.dismissed_at IS NULL AND f.commenter_id IS NOT NULL
  UNION
  SELECT l.a_id FROM commenter_links l WHERE l.kind = 'user'
  UNION
  SELECT l.b_id FROM commenter_links l WHERE l.kind = 'user'
), edge_rows AS (
  SELECT e.id::text AS id,
         e.from_channel_id,
         e.commenter_id,
         NULL::uuid AS peer_commenter_id,
         e.kind,
         e.weight::float8 AS weight,
         COALESCE(e.evidence->>'summary', e.kind)::text AS evidence,
         e.video_id
  FROM commenter_edges e
  WHERE e.commenter_id IN (SELECT id FROM sparse)
    AND (e.kind <> 'commented_by' OR e.from_channel_id IS NOT NULL)
  UNION ALL
  SELECT ('style:' || l.a_id::text || ':' || l.b_id::text) AS id,
         NULL::uuid AS from_channel_id,
         l.a_id AS commenter_id,
         l.b_id AS peer_commenter_id,
         'style'::text AS kind,
         GREATEST(l.score, 1)::float8 AS weight,
         COALESCE(l.evidence->>'summary', 'style suggestion')::text AS evidence,
         NULL::uuid AS video_id
  FROM commenter_links l
  WHERE l.kind = 'style'
    AND l.a_id IN (SELECT id FROM sparse)
    AND l.b_id IN (SELECT id FROM sparse)
)
SELECT id, from_channel_id, commenter_id, peer_commenter_id, kind, weight, evidence, video_id
FROM edge_rows
ORDER BY weight DESC, id
LIMIT 200`

const listCommenterNetworkEdgesFallbackSQL = `
WITH sparse AS (
  SELECT c.id FROM commenters c WHERE c.channel_id IS NOT NULL
  UNION
  SELECT w.commenter_id FROM commenter_watchlist w
  UNION
  SELECT f.commenter_id FROM osint_flags f
  WHERE f.dismissed_at IS NULL AND f.commenter_id IS NOT NULL
  UNION
  SELECT l.a_id FROM commenter_links l WHERE l.kind = 'user'
  UNION
  SELECT l.b_id FROM commenter_links l WHERE l.kind = 'user'
)
SELECT ('fallback:' || v.channel_row_id::text || ':' || c.id::text) AS id,
       v.channel_row_id AS from_channel_id,
       c.id AS commenter_id,
       NULL::uuid AS peer_commenter_id,
       'commented_by'::text AS kind,
       count(*)::float8 AS weight,
       'commented_by'::text AS evidence,
       (array_agg(vc.video_id ORDER BY vc.published_at DESC NULLS LAST))[1] AS video_id
FROM commenters c
JOIN video_comments vc ON vc.commenter_id = c.id
JOIN videos v ON v.id = vc.video_id
WHERE c.id IN (SELECT id FROM sparse)
  AND v.channel_row_id IS NOT NULL
GROUP BY v.channel_row_id, c.id
ORDER BY count(*) DESC, c.id
LIMIT 200`

const getCommenterNetworkInspectorSQL = `
SELECT c.id,
       c.source,
       c.display_name,
       c.author_url,
       c.comment_count,
       c.channel_id,
       EXISTS (
         SELECT 1 FROM commenter_watchlist w
         WHERE w.commenter_id = c.id AND w.user_id = $2
       ) AS watchlisted,
       COALESCE((
         SELECT array_agg(DISTINCT f.kind ORDER BY f.kind)
         FROM osint_flags f
         WHERE f.commenter_id = c.id AND f.dismissed_at IS NULL
       ), '{}'::text[])::text[] AS open_flag_kinds,
       (
         SELECT avg(cs.sentiment)
         FROM video_comments vc
         JOIN comment_scores cs ON cs.comment_id = vc.id
         WHERE vc.commenter_id = c.id AND cs.sentiment IS NOT NULL
       ) AS mean_sentiment,
       (
         SELECT avg(cs.toxicity)
         FROM video_comments vc
         JOIN comment_scores cs ON cs.comment_id = vc.id
         WHERE vc.commenter_id = c.id AND cs.toxicity IS NOT NULL
       ) AS mean_toxicity
FROM commenters c
WHERE c.id = $1`

func loadNetworkCommenters(ctx context.Context, dbc *db.DatabaseConnection, userID pgtype.UUID) ([]templates.NetworkCommenter, []templates.NetworkCommenterEdge) {
	nodes, err := queryCommenterNetworkNodes(ctx, dbc, userID)
	if err != nil {
		if !db.IsUndefinedColumnErr(err) {
			slog.Error("failed to list commenter network nodes", "error", err)
		}
		return nil, nil
	}
	edges, err := queryCommenterNetworkEdges(ctx, dbc)
	if err != nil {
		if missingRelation(err, "commenter_edges") {
			edges, err = queryCommenterNetworkEdgesFallback(ctx, dbc)
		}
		if err != nil {
			if !db.IsUndefinedColumnErr(err) {
				slog.Error("failed to list commenter network edges", "error", err)
			}
			edges = nil
		}
	}
	return nodes, edges
}

func missingRelation(err error, name string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42P01" {
		return false
	}
	if pgErr.TableName == name {
		return true
	}
	return strings.Contains(strings.ToLower(pgErr.Message), strings.ToLower(name))
}

func queryCommenterNetworkNodes(ctx context.Context, dbc *db.DatabaseConnection, userID pgtype.UUID) ([]templates.NetworkCommenter, error) {
	rows, err := dbc.Query(ctx, listCommenterNetworkNodesSQL, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []templates.NetworkCommenter
	for rows.Next() {
		var (
			id, channelID pgtype.UUID
			n             templates.NetworkCommenter
		)
		if err := rows.Scan(&id, &n.Source, &n.DisplayName, &n.AuthorURL, &n.CommentCount, &channelID, &n.Watchlisted, &n.OpenFlagKinds); err != nil {
			return nil, err
		}
		n.ID = id.String()
		if channelID.Valid {
			n.ChannelID = channelID.String()
		}
		if n.OpenFlagKinds == nil {
			n.OpenFlagKinds = []string{}
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func queryCommenterNetworkEdges(ctx context.Context, dbc *db.DatabaseConnection) ([]templates.NetworkCommenterEdge, error) {
	return scanCommenterNetworkEdges(ctx, dbc, listCommenterNetworkEdgesSQL)
}

func queryCommenterNetworkEdgesFallback(ctx context.Context, dbc *db.DatabaseConnection) ([]templates.NetworkCommenterEdge, error) {
	return scanCommenterNetworkEdges(ctx, dbc, listCommenterNetworkEdgesFallbackSQL)
}

func scanCommenterNetworkEdges(ctx context.Context, dbc *db.DatabaseConnection, sql string) ([]templates.NetworkCommenterEdge, error) {
	rows, err := dbc.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []templates.NetworkCommenterEdge
	for rows.Next() {
		var (
			fromChannel, commenter, peer, video pgtype.UUID
			e                                   templates.NetworkCommenterEdge
			weight                              float64
		)
		if err := rows.Scan(&e.ID, &fromChannel, &commenter, &peer, &e.Kind, &weight, &e.Evidence, &video); err != nil {
			return nil, err
		}
		if fromChannel.Valid {
			e.FromChannelID = fromChannel.String()
		}
		if commenter.Valid {
			e.CommenterID = commenter.String()
		}
		if peer.Valid {
			e.PeerCommenterID = peer.String()
		}
		if video.Valid {
			e.VideoID = video.String()
		}
		e.Weight = weight
		out = append(out, e)
	}
	return out, rows.Err()
}

func loadNetworkCommenterInspector(ctx context.Context, dbc *db.DatabaseConnection, commenterID, userID pgtype.UUID) (*templates.NetworkCommenter, error) {
	row := dbc.QueryRow(ctx, getCommenterNetworkInspectorSQL, commenterID, userID)
	var (
		id, channelID     pgtype.UUID
		n                 templates.NetworkCommenter
		meanSentiment     pgtype.Float8
		meanToxicity      pgtype.Float8
	)
	if err := row.Scan(
		&id, &n.Source, &n.DisplayName, &n.AuthorURL, &n.CommentCount, &channelID,
		&n.Watchlisted, &n.OpenFlagKinds, &meanSentiment, &meanToxicity,
	); err != nil {
		return nil, err
	}
	n.ID = id.String()
	if channelID.Valid {
		n.ChannelID = channelID.String()
	}
	if n.OpenFlagKinds == nil {
		n.OpenFlagKinds = []string{}
	}
	if meanSentiment.Valid {
		v := meanSentiment.Float64
		n.MeanSentiment = &v
	}
	if meanToxicity.Valid {
		v := meanToxicity.Float64
		n.MeanToxicity = &v
	}
	return &n, nil
}
