package wiki

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pmezard/go-difflib/difflib"
)

const (
	TreeCreator  = "creator"
	TreeChannel  = "channel"
	TreeClipping = "clipping"
	TreeTopic    = "topic"
)

var Trees = []string{TreeCreator, TreeChannel, TreeClipping, TreeTopic}

// ErrNotFound is returned when a page does not exist.
var ErrNotFound = fmt.Errorf("wiki: not found")

// ErrConflict is the UI/MCP name for a stale expected_revision.
var ErrConflict = ErrRevisionConflict

// TreeLabel is the short UI label for a tree.
func TreeLabel(tree string) string {
	switch tree {
	case TreeCreator:
		return "Creators"
	case TreeChannel:
		return "Channels"
	case TreeClipping:
		return "Clipping"
	case TreeTopic:
		return "Topics"
	default:
		return tree
	}
}

// TreeBlurb is a one-line description of a tree for the index.
func TreeBlurb(tree string) string {
	switch tree {
	case TreeCreator:
		return "People behind archived channels."
	case TreeChannel:
		return "Notes that belong to a channel, not a person."
	case TreeClipping:
		return "Show structure, episode pages, and how to cut."
	case TreeTopic:
		return "Subjects that span creators and episodes."
	default:
		return ""
	}
}

// TreeURL is the listing URL for a tree.
func TreeURL(tree string) string {
	return "/wiki/" + tree + "/index"
}

// ParentSlug returns the parent path of a nested slug, or empty.
func ParentSlug(slug string) string {
	slug = strings.Trim(slug, "/")
	i := strings.LastIndex(slug, "/")
	if i <= 0 {
		return ""
	}
	return slug[:i]
}

// PageGroup is a page and its nested children (direct subpages).
type PageGroup struct {
	Page     Page
	Children []PageGroup
}

// GroupPages nests pages by slug path. A child is grouped under the longest
// existing ancestor; pages with no ancestor in the set are roots.
func GroupPages(pages []Page) []PageGroup {
	bySlug := make(map[string]Page, len(pages))
	for _, p := range pages {
		bySlug[p.Slug] = p
	}
	kids := make(map[string][]Page)
	var roots []Page
	for _, p := range pages {
		attached := false
		parent := ParentSlug(p.Slug)
		for parent != "" {
			if _, ok := bySlug[parent]; ok {
				kids[parent] = append(kids[parent], p)
				attached = true
				break
			}
			parent = ParentSlug(parent)
		}
		if !attached {
			roots = append(roots, p)
		}
	}
	var build func(Page) PageGroup
	build = func(p Page) PageGroup {
		g := PageGroup{Page: p}
		for _, c := range kids[p.Slug] {
			g.Children = append(g.Children, build(c))
		}
		return g
	}
	out := make([]PageGroup, 0, len(roots))
	for _, r := range roots {
		out = append(out, build(r))
	}
	return out
}

// NormalizeSlug is the UI name for Slug.
func NormalizeSlug(s string) string { return Slug(s) }

// SlugFromName builds a flat slug from a display name.
func SlugFromName(name string) string {
	return Slug(strings.ReplaceAll(name, "/", " "))
}

// PageURL is the show URL for a vault page.
func PageURL(tree, slug string) string {
	return "/wiki/" + tree + "/" + slug
}

// EditURL is the editor URL for a vault page.
func EditURL(tree, slug string) string {
	return PageURL(tree, slug) + "/edit"
}

// HistoryURL is the revision list URL for a vault page.
func HistoryURL(tree, slug string) string {
	return PageURL(tree, slug) + "/history"
}

var (
	wikiLink = regexp.MustCompile(`\[\[([^\]|]+)(?:\|([^\]]+))?\]\]`)
	slugKeep = regexp.MustCompile(`[^a-z0-9]+`)
)

// ValidTree reports whether t is one of the four wiki trees.
func ValidTree(t string) bool {
	switch t {
	case TreeCreator, TreeChannel, TreeClipping, TreeTopic:
		return true
	default:
		return false
	}
}

// Slug lowercases s, keeps a-z0-9 and `/` for nested paths, and collapses other runs to `-`.
func Slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	parts := strings.Split(s, "/")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = slugKeep.ReplaceAllString(p, "-")
		p = strings.Trim(p, "-")
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, "/")
}

// Link is a parsed [[target]] or [[target|label]] wikilink.
type Link struct {
	Target string
	Label  string
	Tree   string // resolved destination tree
	Slug   string // resolved destination slug
}

// ParseLinks extracts unique wikilinks from body. Bare slugs inherit defaultTree;
// targets whose first path segment is a valid tree use that tree.
func ParseLinks(body, defaultTree string) []Link {
	ms := wikiLink.FindAllStringSubmatch(body, -1)
	out := make([]Link, 0, len(ms))
	seen := map[string]bool{}
	for _, p := range ms {
		raw := strings.TrimSpace(p[1])
		if raw == "" {
			continue
		}
		label := raw
		if len(p) > 2 && p[2] != "" {
			label = p[2]
		}
		tree, slug := resolveLinkTarget(raw, defaultTree)
		if slug == "" {
			continue
		}
		key := tree + "/" + slug
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Link{Target: raw, Label: label, Tree: tree, Slug: slug})
	}
	return out
}

// ResolveLink maps a wikilink target to tree+slug.
func ResolveLink(fromTree, target string) (tree, slug string) {
	return resolveLinkTarget(target, fromTree)
}

func resolveLinkTarget(target, defaultTree string) (tree, slug string) {
	target = strings.Trim(strings.TrimSpace(target), "/")
	if target == "" {
		return defaultTree, ""
	}
	parts := strings.Split(target, "/")
	if len(parts) >= 2 && ValidTree(parts[0]) {
		return parts[0], Slug(strings.Join(parts[1:], "/"))
	}
	return defaultTree, Slug(target)
}

// UnifiedDiff returns a unified diff of old→new. Empty when old is empty (create).
func UnifiedDiff(oldBody, newBody string) string {
	if oldBody == "" {
		return ""
	}
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(oldBody),
		B:        difflib.SplitLines(newBody),
		FromFile: "a",
		ToFile:   "b",
		Context:  3,
	})
	if err != nil {
		return fmt.Sprintf("diff error: %v", err)
	}
	return diff
}

// Actor identifies who wrote a revision.
type Actor struct {
	Kind          string // user | agent | system
	ID            string
	SessionID     string
	ClientName    string
	ClientVersion string
	TokenName     string
	UserID        pgtype.UUID
}

func (a Actor) updatedBy() string {
	if a.ID != "" {
		return a.Kind + ":" + a.ID
	}
	return a.Kind
}

func (a Actor) valid() bool {
	switch a.Kind {
	case "user", "agent", "system":
		return a.ID != "" || a.Kind == "system"
	default:
		return false
	}
}

// Page is the current tip of a wiki page.
type Page struct {
	Tree      string
	Slug      string
	Title     string
	Body      string
	Revision  int32
	CreatorID pgtype.UUID
	ChannelID pgtype.UUID
	UpdatedBy string
	UpdatedAt time.Time
	Summary   string
	HTML      string
	Headline  string
	Links     []Link
	Backlinks []Ref
}

// Ref is a lightweight page pointer (backlinks / history listings).
type Ref struct {
	Tree  string
	Slug  string
	Title string
}

// Revision is one immutable snapshot.
type Revision struct {
	ID        pgtype.UUID
	Tree      string
	Slug      string
	Revision  int32
	Title     string
	Body      string
	Diff      string
	Summary   string
	ActorKind  string
	ActorID    string
	SessionID  string
	ClientName string
	CreatedAt  time.Time
}

// SearchHit is a ranked search result.
type SearchHit struct {
	Page
	Rank     float64
	Headline string
}

// PutInput is a create-or-update write with optimistic concurrency.
// ExpectedRevision 0 creates a missing page at revision 1.
// Updates require ExpectedRevision to equal the current tip.
type PutInput struct {
	Tree             string
	Slug             string
	Title            string
	Body             string
	ExpectedRevision int32
	Summary          string
	CreatorID        pgtype.UUID
	ChannelID        pgtype.UUID
	Actor            Actor
}
