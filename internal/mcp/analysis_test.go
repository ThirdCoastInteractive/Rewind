package mcp

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/analyze"
	"thirdcoast.systems/rewind/internal/db"
)

func TestBuildCatalogIncludesTruncatedDescription(t *testing.T) {
	long := strings.Repeat("word ", 200)
	if utf8Count(long) <= CatalogDescMax {
		t.Fatalf("fixture too short")
	}
	id := mustPGUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	rows := []*db.ListChannelCatalogRow{{
		ID:          id,
		Title:       "Indexed title",
		Uploader:    "Louis Rossmann",
		Format:      "video",
		Media:       "metadata",
		Description: long,
		Src:         "https://youtube.com/watch?v=x",
	}}
	out := BuildCatalog("Louis Rossmann", rows)
	videos, ok := out["videos"].([]CatalogItem)
	if !ok || len(videos) != 1 {
		t.Fatalf("videos: %#v", out["videos"])
	}
	got := videos[0]
	if got.Title != "Indexed title" {
		t.Fatalf("title %q", got.Title)
	}
	if got.Description == "" {
		t.Fatal("description omitted")
	}
	if got.Description == long {
		t.Fatal("description was not truncated")
	}
	if !strings.HasSuffix(got.Description, "…") {
		t.Fatalf("missing ellipsis: %q", got.Description)
	}
	if n := utf8Count(strings.TrimSuffix(got.Description, "…")); n != CatalogDescMax {
		t.Fatalf("truncated length %d want %d", n, CatalogDescMax)
	}
	if got.Media != "metadata" {
		t.Fatalf("media %q", got.Media)
	}
}

func TestCompareAnalysesReturnsBothStatusAndSignals(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	dying := dyingFixture(now)
	fine := fineFixture(now)
	a := AnalysisFromReport("Channel A", analyze.Analyze(dying, now), len(dying))
	b := AnalysisFromReport("Channel B", analyze.Analyze(fine, now), len(fine))
	out := CompareAnalyses(a, b)
	left, _ := out["a"].(ChannelAnalysis)
	right, _ := out["b"].(ChannelAnalysis)
	if left.Uploader != "Channel A" || right.Uploader != "Channel B" {
		t.Fatalf("uploaders %+v %+v", left, right)
	}
	if left.Status == "" || right.Status == "" {
		t.Fatalf("missing status: %q %q", left.Status, right.Status)
	}
	if left.Signals == nil || right.Signals == nil {
		t.Fatal("signals omitted")
	}
	if left.Status == right.Status {
		t.Fatalf("expected distinct trajectories, both %q", left.Status)
	}
}

func utf8Count(s string) int {
	return len([]rune(s))
}

func mustPGUUID(s string) pgtype.UUID {
	u := uuid.MustParse(s)
	var id pgtype.UUID
	id.Bytes = u
	id.Valid = true
	return id
}

func dyingFixture(now time.Time) []analyze.Video {
	var out []analyze.Video
	for i := 0; i < 12; i++ {
		out = append(out, analyze.Video{
			ID:              "old-" + string(rune('a'+i)),
			Format:          "video",
			Title:           "old hit",
			UploadDate:      now.AddDate(0, 0, -400-i*7),
			DurationSeconds: 600,
			ViewCount:       500_000,
			LikeCount:       10_000,
			CommentCount:    800,
		})
	}
	for i := 0; i < 8; i++ {
		out = append(out, analyze.Video{
			ID:              "new-" + string(rune('a'+i)),
			Format:          "short",
			Title:           "new short",
			UploadDate:      now.AddDate(0, 0, -7-i),
			DurationSeconds: 30,
			ViewCount:       200,
			LikeCount:       2,
			CommentCount:    0,
		})
	}
	return out
}

func fineFixture(now time.Time) []analyze.Video {
	var out []analyze.Video
	for i := 0; i < 20; i++ {
		age := 40 + i*10
		if i >= 12 {
			age = i - 11 // recent
		}
		out = append(out, analyze.Video{
			ID:              "ok-" + string(rune('a'+i)),
			Format:          "video",
			Title:           "steady",
			UploadDate:      now.AddDate(0, 0, -age),
			DurationSeconds: 600,
			ViewCount:       50_000,
			LikeCount:       2000,
			CommentCount:    100,
		})
	}
	return out
}
