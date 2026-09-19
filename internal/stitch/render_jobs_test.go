package stitch

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestRenderSidecarURLsOnlyExposeReadyRequestedFormat(t *testing.T) {
	id := pgtype.UUID{Bytes: uuid.MustParse("11111111-1111-1111-1111-111111111111"), Valid: true}
	urls := renderSidecarURLs(id, "ready", "srt")
	if urls["srt"] != "/api/stitch/11111111-1111-1111-1111-111111111111/captions?format=srt" {
		t.Fatalf("urls=%v", urls)
	}
	if renderSidecarURLs(id, "processing", "srt") != nil || renderSidecarURLs(id, "ready", "none") != nil {
		t.Fatal("sidecars exposed before ready or for unsupported mode")
	}
}
