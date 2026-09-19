package content

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/cmd/web/templates"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/wiki"
)

func networkMembers(channels []*db.ListNetworkChannelsRow, node string) ([]*db.ListNetworkChannelsRow, []pgtype.UUID) {
	members := []*db.ListNetworkChannelsRow{}
	ids := []pgtype.UUID{}
	for _, ch := range channels {
		if ch.ID.String() == node || (ch.CreatorID.Valid && "creator:"+ch.CreatorID.String() == node) {
			members = append(members, ch)
			ids = append(ids, ch.ID)
		}
	}
	return members, ids
}

func HandleNetworkInspect(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		userID, _, err := common.RequireSessionUser(c, sm)
		if err != nil {
			return err
		}
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		node := c.QueryParam("node")
		if strings.HasPrefix(node, "commenter:") {
			var commenterID pgtype.UUID
			if err := commenterID.Scan(strings.TrimPrefix(node, "commenter:")); err != nil || !commenterID.Valid {
				return echo.NewHTTPError(404, "Unknown commenter")
			}
			detail, err := loadNetworkCommenterInspector(ctx, dbc, commenterID, userID)
			if err != nil {
				if db.IsUndefinedColumnErr(err) {
					return echo.NewHTTPError(404, "Commenters are not available yet")
				}
				if !errors.Is(err, pgx.ErrNoRows) {
					slog.Error("failed to load commenter inspector", "error", err, "commenter_id", commenterID.String())
				}
				return echo.NewHTTPError(404, "Unknown commenter")
			}
			return datastar.NewSSE(c.Response(), c.Request()).PatchElementTempl(templates.NetworkCommenterInspector(*detail))
		}
		channels, err := q.ListNetworkChannels(ctx)
		if err != nil {
			return err
		}
		members, _ := networkMembers(channels, node)
		creators, err := q.ListCreators(ctx)
		if err != nil {
			return err
		}
		title := ""
		creatorID := ""
		if strings.HasPrefix(node, "creator:") {
			creatorID = strings.TrimPrefix(node, "creator:")
		}
		if len(members) > 0 {
			title = members[0].Uploader
			if members[0].CreatorID.Valid {
				if creatorID == "" {
					creatorID = members[0].CreatorID.String()
				}
				if strings.HasPrefix(node, "creator:") {
					title = members[0].CreatorName
				}
			}
		}
		if title == "" && creatorID != "" {
			for _, cr := range creators {
				if cr != nil && cr.ID.String() == creatorID {
					title = cr.Name
					break
				}
			}
		}
		if len(members) == 0 && creatorID == "" {
			return echo.NewHTTPError(404, "Unknown channel or creator")
		}
		if title == "" {
			title = node
		}
		vaultPages := loadNetworkVault(ctx, dbc, creatorID, members)
		homeTree, homeSlug := networkVaultHome(node, title, creatorID, members)
		return datastar.NewSSE(c.Response(), c.Request()).PatchElementTempl(templates.NetworkInspector(node, title, creatorID, members, channels, creators, vaultPages, homeTree, homeSlug))
	}
}

func HandleNetworkContext(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := common.RequireSessionUser(c, sm); err != nil {
			return err
		}
		ctx := c.Request().Context()
		q := dbc.Queries(ctx)
		channels, err := q.ListNetworkChannels(ctx)
		if err != nil {
			return err
		}
		_, ids := networkMembers(channels, c.QueryParam("node"))
		if len(ids) == 0 {
			return datastar.NewSSE(c.Response(), c.Request()).PatchElementTempl(templates.NetworkContexts(nil))
		}
		query := strings.TrimSpace(c.QueryParam("q"))
		if len(query) > 300 {
			return echo.NewHTTPError(400, "Search is too long")
		}
		rows, err := q.ListNetworkContext(ctx, &db.ListNetworkContextParams{ChannelIds: ids, Query: query})
		if err != nil {
			return err
		}
		return datastar.NewSSE(c.Response(), c.Request()).PatchElementTempl(templates.NetworkContexts(rows))
	}
}

var xHandlePattern = regexp.MustCompile(`^[a-zA-Z0-9_]{1,15}$`)

func networkXHandle(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if strings.Contains(value, "://") || strings.HasPrefix(value, "x.com/") || strings.HasPrefix(value, "twitter.com/") {
		if !strings.Contains(value, "://") {
			value = "https://" + value
		}
		u, err := url.Parse(value)
		if err != nil {
			return "", errors.New("invalid X profile URL")
		}
		host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
		if (u.Scheme != "https" && u.Scheme != "http") || (host != "x.com" && host != "twitter.com") || u.User != nil || u.Port() != "" {
			return "", errors.New("use an x.com or twitter.com profile URL")
		}
		value = strings.Trim(u.Path, "/")
	}
	value = strings.TrimPrefix(value, "@")
	if !xHandlePattern.MatchString(value) {
		return "", errors.New("use an X handle or profile URL, not a post URL")
	}
	value = strings.ToLower(value)
	switch value {
	case "home", "explore", "search", "i", "intent", "share", "hashtag", "settings", "compose", "messages", "notifications":
		return "", errors.New("this is an X navigation page, not an account")
	}
	return value, nil
}

func HandleNetworkGroup(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := common.RequireSessionUser(c, sm); err != nil {
			return err
		}
		notice := func(message string) error {
			return datastar.NewSSE(c.Response(), c.Request()).PatchElementTempl(templates.NetworkIdentityStatus(message))
		}
		var in struct {
			CreatorID  string   `json:"creator_id"`
			Name       string   `json:"name"`
			Handles    string   `json:"handles"`
			ChannelIDs []string `json:"channel_ids"`
		}
		if err := c.Bind(&in); err != nil {
			return notice("Invalid grouping request")
		}
		if len(in.ChannelIDs) > 100 || len(in.Handles) > 2000 {
			return notice("Group up to 100 accounts at a time")
		}
		ctx := c.Request().Context()
		q, tx, err := dbc.NewWithTX(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		// Serialize manual identity changes, including case-insensitive X lookups.
		if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(730215)"); err != nil {
			return err
		}
		var creator pgtype.UUID
		if in.CreatorID != "" {
			if err = creator.Scan(in.CreatorID); err != nil {
				return notice("Choose a valid creator")
			}
			if _, err = q.GetCreator(ctx, creator); err != nil {
				return notice("Creator not found")
			}
		} else {
			name := strings.TrimSpace(in.Name)
			if name == "" || len(name) > 200 {
				return notice("Enter a creator name")
			}
			existing, lookupErr := q.GetCreatorByNameCI(ctx, name)
			if lookupErr == nil {
				creator = existing.ID
			} else if errors.Is(lookupErr, pgx.ErrNoRows) {
				created, createErr := q.CreateCreator(ctx, &db.CreateCreatorParams{Name: name})
				if createErr != nil {
					return createErr
				}
				creator = created.ID
			} else {
				return lookupErr
			}
		}
		ids := map[pgtype.UUID]bool{}
		for _, raw := range in.ChannelIDs {
			var id pgtype.UUID
			if err = id.Scan(raw); err != nil || !id.Valid {
				return notice("Invalid channel")
			}
			ids[id] = true
		}
		for _, raw := range strings.FieldsFunc(in.Handles, func(r rune) bool { return r == ',' || r == '\n' }) {
			handle, e := networkXHandle(raw)
			if e != nil {
				return notice(e.Error())
			}
			ch, lookupErr := q.GetNetworkXChannel(ctx, handle)
			if errors.Is(lookupErr, pgx.ErrNoRows) {
				ch, lookupErr = q.UpsertChannel(ctx, &db.UpsertChannelParams{Platform: "twitter", IdentityKey: handle, Uploader: "@" + handle, CanonicalURL: "https://x.com/" + handle})
			}
			if lookupErr != nil {
				return lookupErr
			}
			ids[ch.ID] = true
		}
		if len(ids) == 0 {
			return notice("Select an account or enter an X handle")
		}
		for id := range ids {
			ch, e := q.GetChannel(ctx, id)
			if e != nil {
				return notice("Channel no longer exists")
			}
			if ch.CreatorID.Valid && ch.CreatorID != creator {
				return notice("An account already belongs to another creator. Ungroup it first to change that assignment.")
			}
			if err = q.SetChannelCreator(ctx, &db.SetChannelCreatorParams{ID: id, CreatorID: creator}); err != nil {
				return err
			}
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
		return patchNetworkIdentity(c, dbc, "Grouping saved. Select the creator to inspect all accounts.")
	}
}

func HandleNetworkUnlink(sm *auth.SessionManager, dbc *db.DatabaseConnection) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, _, err := common.RequireSessionUser(c, sm); err != nil {
			return err
		}
		var in struct {
			ChannelID string `json:"channel_id"`
			CreatorID string `json:"creator_id"`
		}
		if err := c.Bind(&in); err != nil {
			return err
		}
		var id, creator pgtype.UUID
		if err := id.Scan(in.ChannelID); err != nil {
			return echo.NewHTTPError(400)
		}
		if err := creator.Scan(in.CreatorID); err != nil {
			return echo.NewHTTPError(400)
		}
		if err := dbc.Queries(c.Request().Context()).UnlinkChannelFromCreator(c.Request().Context(), &db.UnlinkChannelFromCreatorParams{ID: id, CreatorID: creator}); err != nil {
			return err
		}
		return patchNetworkIdentity(c, dbc, "Account ungrouped. Its archive and evidence are unchanged.")
	}
}

func loadNetworkVault(ctx context.Context, dbc *db.DatabaseConnection, creatorID string, members []*db.ListNetworkChannelsRow) []wiki.Page {
	store := wikiStore(dbc)
	seen := map[string]bool{}
	var out []wiki.Page
	add := func(pages []wiki.Page) {
		for _, p := range pages {
			key := p.Tree + "/" + p.Slug
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, p)
		}
	}
	if creatorID != "" {
		var id pgtype.UUID
		if err := id.Scan(creatorID); err == nil && id.Valid {
			pages, _ := store.PagesForCreator(ctx, id)
			add(pages)
		}
	}
	for _, ch := range members {
		if ch == nil {
			continue
		}
		pages, _ := store.PagesForChannel(ctx, ch.ID)
		add(pages)
	}
	return out
}

func networkVaultHome(node, title, creatorID string, members []*db.ListNetworkChannelsRow) (tree, slug string) {
	if creatorID != "" {
		name := title
		if !strings.HasPrefix(node, "creator:") && len(members) > 0 && members[0].CreatorName != "" {
			name = members[0].CreatorName
		}
		return wiki.TreeCreator, wiki.SlugFromName(name)
	}
	if len(members) == 1 {
		return wiki.TreeChannel, wiki.SlugFromName(members[0].Uploader)
	}
	return wiki.TreeCreator, wiki.SlugFromName(title)
}

func patchNetworkIdentity(c echo.Context, dbc *db.DatabaseConnection, message string) error {
	ctx := c.Request().Context()
	q := dbc.Queries(ctx)
	channels, err := q.ListNetworkChannels(ctx)
	if err != nil {
		return err
	}
	edges, err := q.ListChannelEdges(ctx)
	if err != nil {
		return err
	}
	// Keep a selected creator bundle scoped after an identity mutation.
	if ref, e := url.Parse(c.Request().Referer()); e == nil && ref.Query().Get("bundle") != "" {
		var bundle pgtype.UUID
		if e = bundle.Scan(ref.Query().Get("bundle")); e == nil && bundle.Valid {
			members, e := q.ListCreatorBundleMembers(ctx, bundle)
			if e != nil {
				return e
			}
			allowed := map[string]bool{}
			for _, m := range members {
				allowed[m.ID.String()] = true
			}
			scopedEdges := edges[:0]
			channelIDs := map[string]bool{}
			for _, edge := range edges {
				if allowed[edge.FromCreatorID.String()] || allowed[edge.ToCreatorID.String()] {
					scopedEdges = append(scopedEdges, edge)
					channelIDs[edge.FromChannelID.String()] = true
					channelIDs[edge.ToChannelID.String()] = true
				}
			}
			edges = scopedEdges
			scopedChannels := channels[:0]
			for _, ch := range channels {
				if allowed[ch.CreatorID.String()] || channelIDs[ch.ID.String()] {
					scopedChannels = append(scopedChannels, ch)
				}
			}
			channels = scopedChannels
		}
	}
	wikiPages, wikiLinks := loadNetworkWiki(ctx, q)
	commenters, commenterEdges := loadNetworkCommenters(ctx, dbc, pgtype.UUID{})
	sse := datastar.NewSSE(c.Response(), c.Request())
	if err = sse.PatchElementTempl(templates.NetworkData(edges, channels, wikiPages, wikiLinks, commenters, commenterEdges), datastar.WithModeReplace()); err != nil {
		return err
	}
	return sse.PatchElementTempl(templates.NetworkIdentityStatus(message))
}
