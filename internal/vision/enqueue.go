package vision

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgtype"
	"math"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/frames"
	"thirdcoast.systems/rewind/internal/jsnum"
)

// IndexRange requests baseline or supplemental dense samples without replacing other recipes.
type IndexRange struct {
	VideoID string  `json:"video_id"`
	Start   jsnum.F `json:"start,omitempty"`
	End     jsnum.F `json:"end,omitempty"`
	Dense   bool    `json:"dense,omitempty"`
}

// QueueRange idempotently queues an explicit visual canary or supplemental range.
func QueueRange(ctx context.Context, dbc *db.DatabaseConnection, in IndexRange) (pgtype.UUID, error) {
	id, err := UUID(in.VideoID)
	if err != nil || !id.Valid {
		return pgtype.UUID{}, fmt.Errorf("video_id is required")
	}
	q := dbc.Queries(ctx)
	asset, err := Asset(ctx, q, id)
	if err != nil {
		return pgtype.UUID{}, err
	}
	start, end := float64(in.Start), float64(in.End)
	if end == 0 {
		end = asset.Duration
	}
	if math.IsNaN(start) || math.IsNaN(end) || start < 0 || end > asset.Duration || end <= start {
		return pgtype.UUID{}, fmt.Errorf("invalid indexing range")
	}
	model, err := ModelReady(ctx, q, FromEnv(), CLIPModel)
	if err != nil {
		return pgtype.UUID{}, err
	}
	interval := 5.0
	if in.Dense {
		interval = 1
	}
	checkpoint, _ := json.Marshal(map[string]float64{"Start": start, "End": end, "Interval": interval})
	recipe := fmt.Sprintf("samples-%gs-v1:%g:%g", interval, start, end)
	return q.QueueVisualRange(ctx, &db.QueueVisualRangeParams{VideoID: id, AssetFingerprint: frames.Fingerprint(asset), ModelID: model.Fingerprint, Recipe: recipe, Checkpoint: checkpoint})
}
