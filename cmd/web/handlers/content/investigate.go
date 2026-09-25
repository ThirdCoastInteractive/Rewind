package content

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/comments"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/videoid"
	"thirdcoast.systems/rewind/pkg/encryption"
	"thirdcoast.systems/rewind/pkg/ytdlp"
)

// HandleInvestigatePage serves GET /investigate — watchlist, open flags, campaigns, search.
func HandleInvestigatePage(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userID, username, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}
		ctx := c.Request().Context()

		if strings.EqualFold(strings.TrimSpace(c.Request().Header.Get("Datastar-Request")), "true") {
			var sig struct {
				Query string `json:"investigateQuery"`
			}
			_ = datastar.ReadSignals(c.Request(), &sig)
			hits := searchCommenters(ctx, dbc, strings.TrimSpace(sig.Query))
			sse := datastar.NewSSE(c.Response().Writer, c.Request())
			return sse.PatchElementTempl(templates.InvestigateSearchResults(hits), datastar.WithSelectorID("investigate-search-results"))
		}

		data := templates.InvestigateIndexData{
			Username:  username,
			Notice:    strings.TrimSpace(c.QueryParam("notice")),
			Watchlist: loadWatchlist(ctx, dbc, userID),
			Flags:     loadOpenFlags(ctx, dbc),
			Campaigns: loadRecentCampaigns(ctx, dbc, 25),
		}
		data.Graph = templates.PruneInvestigateGraph(
			buildInvestigateGraph(ctx, dbc, data.Watchlist, campaignLinkedFlags(data.Flags), data.Campaigns, data.Flags),
			90,
		)
		return templates.Investigate(data).Render(ctx, c.Response())
	}
}

// HandleInvestigateCommenterPage serves GET /investigate/commenters/:id.
func HandleInvestigateCommenterPage(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userID, username, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}
		id, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return c.Redirect(302, "/investigate")
		}
		ctx := c.Request().Context()
		dossier, ok := loadCommenterDossier(ctx, dbc, userID, id)
		if !ok {
			return c.Redirect(302, "/investigate")
		}
		return templates.InvestigateCommenter(dossier, username).Render(ctx, c.Response())
	}
}

// HandleInvestigateCampaignPage serves GET /investigate/campaigns/:id.
func HandleInvestigateCampaignPage(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		_, username, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}
		id, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return c.Redirect(302, "/investigate")
		}
		ctx := c.Request().Context()
		camp, ok := loadCampaignDetail(ctx, dbc, id)
		if !ok {
			return c.Redirect(302, "/investigate")
		}
		return templates.InvestigateCampaign(camp, username).Render(ctx, c.Response())
	}
}

// HandleInvestigateWatch serves POST /api/investigate/commenters/:id/watch.
func HandleInvestigateWatch(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}
		id, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		note := strings.TrimSpace(c.FormValue("note"))
		ctx := c.Request().Context()
		_, err = dbc.Pool.Exec(ctx, `
			INSERT INTO commenter_watchlist (user_id, commenter_id, note)
			VALUES ($1, $2, $3)
			ON CONFLICT (user_id, commenter_id)
			DO UPDATE SET note = EXCLUDED.note
		`, userID, id, note)
		if err != nil {
			slog.Error("investigate watch failed", "error", err, "commenter_id", id)
		}
		return c.Redirect(302, "/investigate/commenters/"+id.String())
	}
}

// HandleInvestigateUnwatch serves POST /api/investigate/commenters/:id/unwatch.
func HandleInvestigateUnwatch(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}
		id, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		ctx := c.Request().Context()
		_, err = dbc.Pool.Exec(ctx, `
			DELETE FROM commenter_watchlist
			WHERE user_id = $1 AND commenter_id = $2
		`, userID, id)
		if err != nil {
			slog.Error("investigate unwatch failed", "error", err, "commenter_id", id)
		}
		return c.Redirect(302, "/investigate/commenters/"+id.String())
	}
}

// HandleInvestigateDismissFlag serves POST /api/investigate/flags/:id/dismiss.
func HandleInvestigateDismissFlag(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}
		id, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		ctx := c.Request().Context()
		_, err = dbc.Pool.Exec(ctx, `
			UPDATE osint_flags
			SET dismissed_at = NOW(), dismissed_by = $2
			WHERE id = $1 AND dismissed_at IS NULL
		`, id, userID)
		if err != nil {
			slog.Error("investigate dismiss flag failed", "error", err, "flag_id", id)
		}
		redir := strings.TrimSpace(c.FormValue("redirect"))
		if redir == "" {
			redir = "/investigate"
		}
		return c.Redirect(302, redir)
	}
}

// HandleInvestigateLinkCommenters serves POST /api/investigate/commenters/:id/link
// (kind=user assertion that two commenters are the same actor; a_id < b_id).
func HandleInvestigateLinkCommenters(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}
		id, err := common.RequireUUIDParam(c, "id")
		if err != nil {
			return err
		}
		otherRaw := strings.TrimSpace(c.FormValue("other_id"))
		if otherRaw == "" {
			otherRaw = strings.TrimSpace(c.FormValue("commenter_id"))
		}
		other, err := common.ParseUUID(otherRaw)
		if err != nil || !other.Valid {
			return echo.NewHTTPError(400, "invalid other commenter id")
		}
		if id.String() == other.String() {
			return echo.NewHTTPError(400, "cannot link a commenter to itself")
		}
		ctx := c.Request().Context()
		_, err = dbc.Pool.Exec(ctx, `
			INSERT INTO commenter_links (a_id, b_id, kind, score, evidence, created_by)
			VALUES (
				LEAST($1::uuid, $2::uuid),
				GREATEST($1::uuid, $2::uuid),
				'user',
				1,
				'{}'::jsonb,
				$3
			)
			ON CONFLICT (a_id, b_id, kind)
			DO UPDATE SET
				score = EXCLUDED.score,
				created_by = COALESCE(EXCLUDED.created_by, commenter_links.created_by)
		`, id, other, userID)
		if err != nil {
			slog.Error("investigate link commenters failed", "error", err, "a", id, "b", other)
		}
		return c.Redirect(302, "/investigate/commenters/"+id.String())
	}
}

// HandleInvestigateIndexXReplies fetches public replies for an archived X status URL.
func HandleInvestigateIndexXReplies(sm *auth.SessionManager, dbc *db.DatabaseConnection, encMgr *encryption.Manager) echo.HandlerFunc {
	return func(c echo.Context) error {
		userID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return c.Redirect(302, "/login")
		}
		rawURL := strings.TrimSpace(c.FormValue("url"))
		normalized, canon, nerr := videoid.NormalizeSourceURL(rawURL)
		if nerr != nil || canon != "x.com" || !strings.Contains(normalized, "/status/") {
			return c.Redirect(302, "/investigate?notice="+url.QueryEscape("Not an X status URL."))
		}
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		video, err := q.SelectVideoBySrc(ctx, &db.SelectVideoBySrcParams{Src: normalized, TenantID: db.OSSTenant()})
		if err != nil {
			return c.Redirect(302, "/investigate?notice="+url.QueryEscape("Archive that X post first, then index replies."))
		}
		client := ytdlp.New()
		if cookies, cerr := q.GetUserCookies(ctx, userID); cerr == nil && len(cookies) > 0 {
			client.Cookies = netscapeCookies(encMgr, cookies)
		}
		err = comments.IndexXReplies(ctx, q, client, video.ID, normalized)
		if err != nil {
			return c.Redirect(302, "/investigate?notice="+url.QueryEscape(err.Error()))
		}
		return c.Redirect(302, "/investigate?notice="+url.QueryEscape("Indexed public replies."))
	}
}

func netscapeCookies(encMgr *encryption.Manager, cookies []*db.GetUserCookiesRow) string {
	if len(cookies) == 0 {
		return ""
	}
	lines := []string{"# Netscape HTTP Cookie File"}
	for _, cookie := range cookies {
		val := cookie.Value
		if err := encryption.Decrypt(encMgr, &val); err != nil {
			continue
		}
		plain, ok := val.Get()
		if !ok {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s\t%s\t%s\t%s\t%d\t%s\t%s",
			cookie.Domain, cookie.Flag, cookie.Path, cookie.Secure, cookie.Expiration, cookie.Name, plain))
	}
	return strings.Join(lines, "\n")
}

func loadWatchlist(ctx context.Context, dbc *db.DatabaseConnection, userID pgtype.UUID) []templates.InvestigateWatchItem {
	rows, err := dbc.Pool.Query(ctx, `
		SELECT w.commenter_id, w.note, w.created_at,
		       c.display_name, c.author_id, c.source, c.comment_count, c.last_seen
		FROM commenter_watchlist w
		JOIN commenters c ON c.id = w.commenter_id
		WHERE w.user_id = $1
		ORDER BY w.created_at DESC
	`, userID)
	if err != nil {
		slog.Error("investigate load watchlist", "error", err)
		return nil
	}
	defer rows.Close()
	var out []templates.InvestigateWatchItem
	for rows.Next() {
		var item templates.InvestigateWatchItem
		var commenterID pgtype.UUID
		var createdAt, lastSeen time.Time
		if err := rows.Scan(&commenterID, &item.Note, &createdAt, &item.DisplayName, &item.AuthorID, &item.Source, &item.CommentCount, &lastSeen); err != nil {
			slog.Error("investigate scan watchlist", "error", err)
			continue
		}
		item.CommenterID = commenterID.String()
		item.LastSeen = formatTS(lastSeen)
		out = append(out, item)
	}
	return out
}

func loadOpenFlags(ctx context.Context, dbc *db.DatabaseConnection) []templates.InvestigateFlagItem {
	rows, err := dbc.Pool.Query(ctx, `
		SELECT f.id, f.kind, f.commenter_id, f.video_id, f.campaign_id, f.score, f.evidence, f.created_at,
		       COALESCE(c.display_name, '')
		FROM osint_flags f
		LEFT JOIN commenters c ON c.id = f.commenter_id
		WHERE f.dismissed_at IS NULL
		ORDER BY f.created_at DESC
		LIMIT 200
	`)
	if err != nil {
		slog.Error("investigate load open flags", "error", err)
		return nil
	}
	defer rows.Close()
	var out []templates.InvestigateFlagItem
	for rows.Next() {
		var item templates.InvestigateFlagItem
		var flagID pgtype.UUID
		var commenterID, videoID, campaignID pgtype.UUID
		var evidence []byte
		var createdAt time.Time
		if err := rows.Scan(&flagID, &item.Kind, &commenterID, &videoID, &campaignID, &item.Score, &evidence, &createdAt, &item.CommenterName); err != nil {
			slog.Error("investigate scan flag", "error", err)
			continue
		}
		item.ID = flagID.String()
		if commenterID.Valid {
			item.CommenterID = commenterID.String()
		}
		if videoID.Valid {
			item.VideoID = videoID.String()
		}
		if campaignID.Valid {
			item.CampaignID = campaignID.String()
		}
		item.Evidence = evidenceText(evidence)
		item.PeerID = sockPeerID(item.CommenterID, evidence)
		item.CreatedAt = formatTS(createdAt)
		out = append(out, item)
	}
	return out
}

func loadRecentCampaigns(ctx context.Context, dbc *db.DatabaseConnection, limit int) []templates.InvestigateCampaignItem {
	rows, err := dbc.Pool.Query(ctx, `
		SELECT id, kind, normalized_text, first_seen, last_seen,
		       comment_count, commenter_count, video_count, evidence
		FROM campaigns
		ORDER BY last_seen DESC, created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		slog.Error("investigate load campaigns", "error", err)
		return nil
	}
	defer rows.Close()
	var out []templates.InvestigateCampaignItem
	for rows.Next() {
		var item templates.InvestigateCampaignItem
		var id pgtype.UUID
		var firstSeen, lastSeen time.Time
		var evidence []byte
		if err := rows.Scan(&id, &item.Kind, &item.NormalizedText, &firstSeen, &lastSeen, &item.CommentCount, &item.CommenterCount, &item.VideoCount, &evidence); err != nil {
			slog.Error("investigate scan campaign", "error", err)
			continue
		}
		item.ID = id.String()
		item.FirstSeen = formatTS(firstSeen)
		item.LastSeen = formatTS(lastSeen)
		item.Evidence = evidenceText(evidence)
		out = append(out, item)
	}
	return out
}

func searchCommenters(ctx context.Context, dbc *db.DatabaseConnection, query string) []templates.InvestigateSearchHit {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	rows, err := dbc.Pool.Query(ctx, `
		SELECT id, display_name, author_id, author_url, comment_count
		FROM commenters
		WHERE display_name ILIKE '%' || $1 || '%'
		   OR author_id ILIKE '%' || $1 || '%'
		   OR author_url ILIKE '%' || $1 || '%'
		ORDER BY comment_count DESC, display_name
		LIMIT 40
	`, query)
	if err != nil {
		slog.Error("investigate search commenters", "error", err)
		return nil
	}
	defer rows.Close()
	var out []templates.InvestigateSearchHit
	for rows.Next() {
		var hit templates.InvestigateSearchHit
		var id pgtype.UUID
		if err := rows.Scan(&id, &hit.DisplayName, &hit.AuthorID, &hit.AuthorURL, &hit.CommentCount); err != nil {
			continue
		}
		hit.ID = id.String()
		out = append(out, hit)
	}
	return out
}

func loadCommenterDossier(ctx context.Context, dbc *db.DatabaseConnection, userID, id pgtype.UUID) (templates.CommenterDossier, bool) {
	var d templates.CommenterDossier
	var firstSeen, lastSeen time.Time
	err := dbc.Pool.QueryRow(ctx, `
		SELECT id, source, author_id, author_url, display_name, first_seen, last_seen, comment_count
		FROM commenters WHERE id = $1
	`, id).Scan(&id, &d.Source, &d.AuthorID, &d.AuthorURL, &d.DisplayName, &firstSeen, &lastSeen, &d.CommentCount)
	if err != nil {
		if err != pgx.ErrNoRows {
			slog.Error("investigate get commenter", "error", err)
		}
		return d, false
	}
	d.ID = id.String()
	d.FirstSeen = formatTS(firstSeen)
	d.LastSeen = formatTS(lastSeen)

	_ = dbc.Pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM commenter_watchlist WHERE user_id = $1 AND commenter_id = $2
		)
	`, userID, id).Scan(&d.Watching)

	var meanSent, meanTox *float64
	_ = dbc.Pool.QueryRow(ctx, `
		SELECT AVG(s.sentiment)::float8, AVG(s.toxicity)::float8
		FROM video_comments c
		JOIN comment_scores s ON s.comment_id = c.id
		WHERE c.commenter_id = $1
	`, id).Scan(&meanSent, &meanTox)
	d.MeanSentiment = meanSent
	d.MeanToxicity = meanTox

	d.Names = loadCommenterNames(ctx, dbc, id)
	d.Flags = loadCommenterFlags(ctx, dbc, id)
	d.Campaigns = loadCommenterCampaigns(ctx, dbc, id)
	d.StyleSuggestions = loadStyleSuggestions(ctx, dbc, id)
	d.RecentComments = loadCommenterComments(ctx, dbc, id, 40)
	seed := []templates.InvestigateWatchItem{{
		CommenterID: d.ID, DisplayName: d.DisplayName, AuthorID: d.AuthorID, Source: d.Source,
	}}
	d.Graph = buildInvestigateGraph(ctx, dbc, seed, d.Flags, d.Campaigns, d.Flags)
	return d, true
}

func loadCommenterNames(ctx context.Context, dbc *db.DatabaseConnection, id pgtype.UUID) []templates.CommenterNameHist {
	rows, err := dbc.Pool.Query(ctx, `
		SELECT display_name, first_seen, last_seen, n
		FROM commenter_names
		WHERE commenter_id = $1
		ORDER BY n DESC, last_seen DESC
	`, id)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []templates.CommenterNameHist
	for rows.Next() {
		var n templates.CommenterNameHist
		var firstSeen, lastSeen time.Time
		if err := rows.Scan(&n.DisplayName, &firstSeen, &lastSeen, &n.N); err != nil {
			continue
		}
		n.FirstSeen = formatTS(firstSeen)
		n.LastSeen = formatTS(lastSeen)
		out = append(out, n)
	}
	return out
}

func loadCommenterFlags(ctx context.Context, dbc *db.DatabaseConnection, id pgtype.UUID) []templates.InvestigateFlagItem {
	rows, err := dbc.Pool.Query(ctx, `
		SELECT id, kind, video_id, campaign_id, score, evidence, created_at
		FROM osint_flags
		WHERE commenter_id = $1 AND dismissed_at IS NULL
		ORDER BY created_at DESC
	`, id)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []templates.InvestigateFlagItem
	for rows.Next() {
		var item templates.InvestigateFlagItem
		var flagID, videoID, campaignID pgtype.UUID
		var evidence []byte
		var createdAt time.Time
		if err := rows.Scan(&flagID, &item.Kind, &videoID, &campaignID, &item.Score, &evidence, &createdAt); err != nil {
			continue
		}
		item.ID = flagID.String()
		item.CommenterID = id.String()
		if videoID.Valid {
			item.VideoID = videoID.String()
		}
		if campaignID.Valid {
			item.CampaignID = campaignID.String()
		}
		item.Evidence = evidenceText(evidence)
		item.PeerID = sockPeerID(item.CommenterID, evidence)
		item.CreatedAt = formatTS(createdAt)
		out = append(out, item)
	}
	return out
}

func loadCommenterCampaigns(ctx context.Context, dbc *db.DatabaseConnection, id pgtype.UUID) []templates.InvestigateCampaignItem {
	rows, err := dbc.Pool.Query(ctx, `
		SELECT DISTINCT camp.id, camp.kind, camp.normalized_text, camp.first_seen, camp.last_seen,
		       camp.comment_count, camp.commenter_count, camp.video_count, camp.evidence
		FROM campaigns camp
		JOIN campaign_members cm ON cm.campaign_id = camp.id
		JOIN video_comments vc ON vc.id = cm.comment_id
		WHERE vc.commenter_id = $1
		ORDER BY camp.last_seen DESC
		LIMIT 50
	`, id)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []templates.InvestigateCampaignItem
	for rows.Next() {
		var item templates.InvestigateCampaignItem
		var campID pgtype.UUID
		var firstSeen, lastSeen time.Time
		var evidence []byte
		if err := rows.Scan(&campID, &item.Kind, &item.NormalizedText, &firstSeen, &lastSeen, &item.CommentCount, &item.CommenterCount, &item.VideoCount, &evidence); err != nil {
			continue
		}
		item.ID = campID.String()
		item.FirstSeen = formatTS(firstSeen)
		item.LastSeen = formatTS(lastSeen)
		item.Evidence = evidenceText(evidence)
		out = append(out, item)
	}
	return out
}

func loadStyleSuggestions(ctx context.Context, dbc *db.DatabaseConnection, id pgtype.UUID) []templates.CommenterLinkItem {
	rows, err := dbc.Pool.Query(ctx, `
		SELECT cl.a_id, cl.b_id, cl.score, cl.evidence,
		       o.id, o.display_name
		FROM commenter_links cl
		JOIN commenters o ON o.id = CASE WHEN cl.a_id = $1 THEN cl.b_id ELSE cl.a_id END
		WHERE (cl.a_id = $1 OR cl.b_id = $1) AND cl.kind = 'style'
		ORDER BY cl.score DESC, cl.created_at DESC
		LIMIT 40
	`, id)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []templates.CommenterLinkItem
	for rows.Next() {
		var item templates.CommenterLinkItem
		var aID, bID, otherID pgtype.UUID
		var evidence []byte
		if err := rows.Scan(&aID, &bID, &item.Score, &evidence, &otherID, &item.OtherName); err != nil {
			continue
		}
		item.OtherID = otherID.String()
		item.Kind = "style"
		item.Evidence = evidenceText(evidence)
		out = append(out, item)
	}
	return out
}

func loadCommenterComments(ctx context.Context, dbc *db.DatabaseConnection, id pgtype.UUID, limit int) []templates.CommenterCommentItem {
	rows, err := dbc.Pool.Query(ctx, `
		SELECT c.video_id, COALESCE(v.title, ''), COALESCE(c.text, ''),
		       COALESCE(c.published_at, c.created_at)
		FROM video_comments c
		JOIN videos v ON v.id = c.video_id
		WHERE c.commenter_id = $1
		ORDER BY c.published_at DESC NULLS LAST, c.created_at DESC
		LIMIT $2
	`, id, limit)
	if err != nil {
		slog.Error("investigate load commenter comments", "error", err)
		return nil
	}
	defer rows.Close()
	var out []templates.CommenterCommentItem
	for rows.Next() {
		var item templates.CommenterCommentItem
		var videoID pgtype.UUID
		var at time.Time
		if err := rows.Scan(&videoID, &item.VideoTitle, &item.Text, &at); err != nil {
			continue
		}
		item.VideoID = videoID.String()
		item.TimeLabel = formatTS(at)
		out = append(out, item)
	}
	return out
}

func loadCampaignDetail(ctx context.Context, dbc *db.DatabaseConnection, id pgtype.UUID) (templates.InvestigateCampaignDetail, bool) {
	var d templates.InvestigateCampaignDetail
	var firstSeen, lastSeen time.Time
	var evidence []byte
	err := dbc.Pool.QueryRow(ctx, `
		SELECT id, kind, normalized_text, first_seen, last_seen,
		       comment_count, commenter_count, video_count, evidence
		FROM campaigns WHERE id = $1
	`, id).Scan(&id, &d.Kind, &d.NormalizedText, &firstSeen, &lastSeen, &d.CommentCount, &d.CommenterCount, &d.VideoCount, &evidence)
	if err != nil {
		if err != pgx.ErrNoRows {
			slog.Error("investigate get campaign", "error", err)
		}
		return d, false
	}
	d.ID = id.String()
	d.FirstSeen = formatTS(firstSeen)
	d.LastSeen = formatTS(lastSeen)
	d.Evidence = evidenceText(evidence)

	rows, err := dbc.Pool.Query(ctx, `
		SELECT vc.video_id, COALESCE(v.title, ''), COALESCE(vc.text, ''),
		       COALESCE(vc.author, ''), vc.commenter_id,
		       COALESCE(vc.published_at, vc.created_at)
		FROM campaign_members cm
		JOIN video_comments vc ON vc.id = cm.comment_id
		JOIN videos v ON v.id = vc.video_id
		WHERE cm.campaign_id = $1
		ORDER BY vc.published_at DESC NULLS LAST
		LIMIT 100
	`, id)
	if err != nil {
		slog.Error("investigate load campaign members", "error", err)
		return d, true
	}
	defer rows.Close()
	for rows.Next() {
		var m templates.CampaignMemberItem
		var videoID, commenterID pgtype.UUID
		var at time.Time
		if err := rows.Scan(&videoID, &m.VideoTitle, &m.Text, &m.Author, &commenterID, &at); err != nil {
			continue
		}
		m.VideoID = videoID.String()
		if commenterID.Valid {
			m.CommenterID = commenterID.String()
		}
		m.TimeLabel = formatTS(at)
		d.Members = append(d.Members, m)
	}
	var seed []templates.InvestigateWatchItem
	seen := map[string]bool{}
	for _, m := range d.Members {
		if m.CommenterID == "" || seen[m.CommenterID] {
			continue
		}
		seen[m.CommenterID] = true
		seed = append(seed, templates.InvestigateWatchItem{
			CommenterID: m.CommenterID, DisplayName: m.Author,
		})
	}
	camps := []templates.InvestigateCampaignItem{{
		ID: d.ID, Kind: d.Kind, NormalizedText: d.NormalizedText,
		CommentCount: d.CommentCount, CommenterCount: d.CommenterCount, VideoCount: d.VideoCount,
		FirstSeen: d.FirstSeen, LastSeen: d.LastSeen,
	}}
	d.Graph = buildInvestigateGraph(ctx, dbc, seed, nil, camps, nil)
	return d, true
}

func formatTS(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02 15:04")
}

func sockPeerID(commenterID string, raw []byte) string {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	a, _ := m["a_id"].(string)
	b, _ := m["b_id"].(string)
	if a != "" && a != commenterID {
		return a
	}
	if b != "" && b != commenterID {
		return b
	}
	return ""
}

func evidenceText(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	if m, ok := v.(map[string]any); ok {
		if why, ok := m["why"].(string); ok && strings.TrimSpace(why) != "" {
			return why
		}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return string(raw)
	}
	return string(b)
}

func campaignLinkedFlags(flags []templates.InvestigateFlagItem) []templates.InvestigateFlagItem {
	var out []templates.InvestigateFlagItem
	for _, f := range flags {
		if f.CampaignID != "" {
			out = append(out, f)
		}
	}
	return out
}

func buildInvestigateGraph(ctx context.Context, dbc *db.DatabaseConnection, watch []templates.InvestigateWatchItem, flags []templates.InvestigateFlagItem, camps []templates.InvestigateCampaignItem, evidenceFlags []templates.InvestigateFlagItem) templates.InvestigateGraph {
	var commenterIDs, campaignIDs []pgtype.UUID
	seenC, seenP := map[string]bool{}, map[string]bool{}
	addC := func(s string) {
		if s == "" || seenC[s] {
			return
		}
		u, err := common.ParseUUID(s)
		if err != nil || !u.Valid {
			return
		}
		seenC[s] = true
		commenterIDs = append(commenterIDs, u)
	}
	addP := func(s string) {
		if s == "" || seenP[s] {
			return
		}
		u, err := common.ParseUUID(s)
		if err != nil || !u.Valid {
			return
		}
		seenP[s] = true
		campaignIDs = append(campaignIDs, u)
	}
	for _, w := range watch {
		addC(w.CommenterID)
	}
	for _, f := range flags {
		addC(f.CommenterID)
		addP(f.CampaignID)
	}
	for _, c := range camps {
		addP(c.ID)
	}
	for _, f := range evidenceFlags {
		addC(f.CommenterID)
		addC(f.PeerID)
		addP(f.CampaignID)
	}
	extra := loadInvestigateGraphEdges(ctx, dbc, commenterIDs, campaignIDs)
	extra = append(extra, templates.EdgesFromFlagEvidence(evidenceFlags)...)
	return templates.BuildInvestigateGraph(watch, flags, camps, extra)
}

func loadInvestigateGraphEdges(ctx context.Context, dbc *db.DatabaseConnection, commenterIDs, campaignIDs []pgtype.UUID) []templates.InvestigateGraphEdge {
	var extra []templates.InvestigateGraphEdge
	if len(commenterIDs) > 0 {
		rows, err := dbc.Pool.Query(ctx, `
			SELECT cl.a_id, cl.b_id, cl.kind,
			       COALESCE(NULLIF(a.display_name, ''), a.author_id),
			       COALESCE(NULLIF(b.display_name, ''), b.author_id)
			FROM commenter_links cl
			JOIN commenters a ON a.id = cl.a_id
			JOIN commenters b ON b.id = cl.b_id
			WHERE cl.a_id = ANY($1::uuid[]) OR cl.b_id = ANY($1::uuid[])
			ORDER BY cl.kind, cl.score DESC
			LIMIT 200
		`, commenterIDs)
		if err != nil {
			slog.Error("investigate load graph links", "error", err)
		} else {
			for rows.Next() {
				var aID, bID pgtype.UUID
				var kind, aName, bName string
				if err := rows.Scan(&aID, &bID, &kind, &aName, &bName); err != nil {
					continue
				}
				extra = append(extra, templates.InvestigateGraphEdge{
					From: "commenter:" + aID.String(), To: "commenter:" + bID.String(), Kind: kind,
					FromLabel: aName, ToLabel: bName,
				})
			}
			rows.Close()
		}
	}
	if len(campaignIDs) > 0 {
		rows, err := dbc.Pool.Query(ctx, `
			SELECT DISTINCT cm.campaign_id, cm.commenter_id,
			       COALESCE(NULLIF(c.display_name, ''), c.author_id),
			       COALESCE(NULLIF(camp.normalized_text, ''), camp.kind)
			FROM campaign_members cm
			JOIN commenters c ON c.id = cm.commenter_id
			JOIN campaigns camp ON camp.id = cm.campaign_id
			WHERE cm.campaign_id = ANY($1::uuid[]) AND cm.commenter_id IS NOT NULL
			LIMIT 300
		`, campaignIDs)
		if err != nil {
			slog.Error("investigate load graph campaign members", "error", err)
			return extra
		}
		for rows.Next() {
			var campID, commenterID pgtype.UUID
			var actorLabel, campLabel string
			if err := rows.Scan(&campID, &commenterID, &actorLabel, &campLabel); err != nil {
				continue
			}
			extra = append(extra, templates.InvestigateGraphEdge{
				From: "commenter:" + commenterID.String(), To: "campaign:" + campID.String(), Kind: "campaign",
				FromLabel: actorLabel, ToLabel: campLabel,
			})
		}
		rows.Close()
	}
	return extra
}
