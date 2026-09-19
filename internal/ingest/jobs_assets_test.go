package ingest

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
)

func TestShouldEnqueuePostIngestAssets(t *testing.T) {
	current := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	other := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}

	if !shouldEnqueuePostIngestAssets(nil, current) {
		t.Fatal("empty queue should enqueue")
	}
	if !shouldEnqueuePostIngestAssets([]*db.GetActiveAssetJobsForVideoRow{
		{IngestJobID: current},
	}, current) {
		t.Fatal("current ingest job is not asset work")
	}
	if shouldEnqueuePostIngestAssets([]*db.GetActiveAssetJobsForVideoRow{
		{IngestJobID: current},
		{IngestJobID: other},
	}, current) {
		t.Fatal("existing asset job should skip enqueue")
	}
}

func TestDerivedAssetsIncomplete(t *testing.T) {
	if !derivedAssetsIncomplete(map[string]any{}) {
		t.Fatal("missing keys are incomplete")
	}
	if !derivedAssetsIncomplete(map[string]any{"preview": true, "waveform": true, "seek": false}) {
		t.Fatal("failed seek is incomplete")
	}
	if derivedAssetsIncomplete(map[string]any{
		"preview":  true,
		"waveform": true,
		"seek":     map[string]bool{"coarse": true, "fine": true},
	}) {
		t.Fatal("complete derived assets")
	}
}
