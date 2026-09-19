package stitch_api

import (
	"testing"

	"thirdcoast.systems/rewind/internal/stitch"
)

func TestValidRenderRequestDefaults(t *testing.T) {
	rev := int64(4)
	req := renderRequest{ExpectedRevision: &rev, OperationKey: "export-key"}
	if err := validRenderRequest(&req); err != nil {
		t.Fatal(err)
	}
	if req.CaptionMode != "none" {
		t.Fatalf("caption default: %q", req.CaptionMode)
	}
	if req.Scope != "all" {
		t.Fatalf("scope default: %q", req.Scope)
	}
}

func TestValidRenderRequestRequiresRevisionAndKey(t *testing.T) {
	if err := validRenderRequest(&renderRequest{OperationKey: "k"}); err == nil {
		t.Fatal("expected revision error")
	}
	rev := int64(0)
	if err := validRenderRequest(&renderRequest{ExpectedRevision: &rev}); err == nil {
		t.Fatal("expected operation key error")
	}
}

func TestRenderJobResponseIncludesProgressPct(t *testing.T) {
	job := stitch.RenderJob{Kind: "", Status: "processing", Progress: 42, MIME: "video/mp4"}
	out := renderJobResponse(job)
	if out["kind"] != "export" {
		t.Fatalf("kind default: %v", out["kind"])
	}
	if out["progress_pct"] != int32(42) {
		t.Fatalf("progress_pct: %v", out["progress_pct"])
	}
	if out["progress"] != int32(42) {
		t.Fatalf("progress: %v", out["progress"])
	}
}
