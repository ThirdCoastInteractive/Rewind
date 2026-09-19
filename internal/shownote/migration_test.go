package shownote

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"thirdcoast.systems/rewind/internal/db"
)

func TestLegacyMarkdownPreservesDepthFirstOrderAndMetadata(t *testing.T) {
	sectionID := migrationTestUUID("11111111-1111-1111-1111-111111111111")
	clipBlockID := migrationTestUUID("22222222-2222-2222-2222-222222222222")
	clipID := migrationTestUUID("33333333-3333-3333-3333-333333333333")
	videoID := migrationTestUUID("44444444-4444-4444-4444-444444444444")
	duration := int32(45)
	created := pgtype.Timestamptz{Time: time.Unix(100, 0), Valid: true}

	blocks := []*db.ShowNoteBlock{
		{ID: clipBlockID, ParentID: sectionID, BlockType: "clip", Title: "Pull quote", Notes: "Start after the pause.", ClipID: clipID, Position: 1, DurationOverride: &duration, CreatedAt: created},
		{ID: migrationTestUUID("55555555-5555-5555-5555-555555555555"), ParentID: sectionID, BlockType: "video", Title: "Source", VideoID: videoID, Position: 0, CreatedAt: created},
		{ID: sectionID, BlockType: "section", Title: "Main story", Position: 0, CreatedAt: created},
		{ID: migrationTestUUID("66666666-6666-6666-6666-666666666666"), BlockType: "break", Title: "Sponsor read", Position: 1, CreatedAt: created},
		{ID: migrationTestUUID("77777777-7777-7777-7777-777777777777"), BlockType: "video", Title: "Lost source", Position: 2, CreatedAt: created},
	}
	clips := map[string]*db.Clip{clipID.String(): {ID: clipID, StartTs: 78, EndTs: 91.25}}

	got := LegacyMarkdown(blocks, clips)
	require.Equal(t, "# Main story\n\n"+
		"- [Source](rewind://video/44444444-4444-4444-4444-444444444444)\n\n"+
		"- [Pull quote](rewind://clip/33333333-3333-3333-3333-333333333333) @ 1:18–1:31\n"+
		"  Start after the pause.\n"+
		"  Duration override: 45 seconds.\n\n"+
		"> Break — Sponsor read\n\n"+
		"- [Missing video: Lost source](rewind://missing/77777777-7777-7777-7777-777777777777)\n", got)
}

func TestLegacyMarkdownCapsNestedHeadingDepth(t *testing.T) {
	blocks := make([]*db.ShowNoteBlock, 0, 8)
	var parent pgtype.UUID
	for i := 0; i < 8; i++ {
		id := migrationTestUUID(uuid.NewString())
		blocks = append(blocks, &db.ShowNoteBlock{ID: id, ParentID: parent, BlockType: "section", Title: "Layer", Position: 0})
		parent = id
	}

	got := LegacyMarkdown(blocks, nil)
	require.Contains(t, got, "###### Layer")
	require.NotContains(t, got, "####### Layer")
}

func migrationTestUUID(raw string) pgtype.UUID {
	id := uuid.MustParse(raw)
	return pgtype.UUID{Bytes: id, Valid: true}
}
