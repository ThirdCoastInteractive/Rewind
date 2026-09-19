package vision

import (
	"context"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/frames"
	"thirdcoast.systems/rewind/internal/jsnum"
)

// SearchInput selects exactly one query source and optional library filters.
type SearchInput struct {
	Text        string  `json:"text,omitempty"`
	ReferenceID string  `json:"reference_id,omitempty"`
	FrameRef    string  `json:"frame_ref,omitempty"`
	VideoID     string  `json:"video_id,omitempty"`
	ChannelID   string  `json:"channel_id,omitempty"`
	CreatorID   string  `json:"creator_id,omitempty"`
	Limit       jsnum.I `json:"limit,omitempty"`
}

// UUID parses optional identifiers without allowing malformed filters to widen a search.
func UUID(value string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if value == "" {
		return id, nil
	}
	e := id.Scan(value)
	return id, e
}

// ModelReady locates and registers the installed immutable model fingerprint.
func ModelReady(ctx context.Context, q *db.Queries, c *Client, name string) (Model, error) {
	caps, e := c.Capabilities(ctx)
	if e != nil {
		return Model{}, e
	}
	for _, m := range caps.Models {
		if m.Name == name && m.Ready && m.Dimensions == 512 && m.Fingerprint != "" {
			e = q.RegisterEmbeddingModel(ctx, &db.RegisterEmbeddingModelParams{ID: m.Fingerprint, Name: m.Name, Revision: m.Revision, Recipe: m.Recipe, License: m.License})
			return m, e
		}
	}
	return Model{}, fmt.Errorf("waiting_model: %s is not installed", name)
}

// Asset resolves an existing library record for frame inspection.
func Asset(ctx context.Context, q *db.Queries, id pgtype.UUID) (frames.Asset, error) {
	v, e := q.GetVideoByID(ctx, id)
	if e != nil {
		return frames.Asset{}, e
	}
	path := ""
	if v.VideoPath != nil {
		path = *v.VideoPath
	}
	duration := 0.0
	if v.DurationSeconds != nil {
		duration = float64(*v.DurationSeconds)
	}
	return frames.Resolve(id.String(), path, duration)
}

// Moment is a ranked visual hit plus nearby live context windows.
type Moment struct {
	Hit     *db.SearchVisualEmbeddingsRow        `json:"hit"`
	Windows []*db.ListContextWindowsForVideosRow `json:"context_windows"`
}

// Search embeds the query, applies database filters, and consolidates nearby moments.
func Search(ctx context.Context, dbc *db.DatabaseConnection, owner pgtype.UUID, in SearchInput) ([]Moment, error) {
	n := 0
	for _, s := range []string{in.Text, in.ReferenceID, in.FrameRef} {
		if s != "" {
			n++
		}
	}
	if n != 1 {
		return nil, fmt.Errorf("provide exactly one text, reference_id, or frame_ref")
	}
	limit := int(in.Limit)
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 50 {
		return nil, fmt.Errorf("limit must be 1–50")
	}
	q := dbc.Queries(ctx)
	c := FromEnv()
	m, e := ModelReady(ctx, q, c, CLIPModel)
	if e != nil {
		return nil, e
	}
	vector := ""
	if in.ReferenceID != "" {
		id, e := UUID(in.ReferenceID)
		if e != nil {
			return nil, e
		}
		ref, e := q.GetVisualReference(ctx, &db.GetVisualReferenceParams{ID: id, OwnerID: owner})
		if e != nil {
			return nil, e
		}
		if ref.ModelID != m.Fingerprint {
			return nil, fmt.Errorf("reference uses a different model; upload it again")
		}
		vector = ref.Embedding
	} else {
		var image []byte
		if in.FrameRef != "" {
			id, t, quality, fingerprint, e := frames.ParseReference(in.FrameRef)
			if e != nil {
				return nil, e
			}
			u, e := UUID(id)
			if e != nil {
				return nil, e
			}
			a, e := Asset(ctx, q, u)
			if e != nil {
				return nil, e
			}
			if frames.Fingerprint(a) != fingerprint {
				return nil, fmt.Errorf("frame asset changed; inspect it again")
			}
			fs, e := frames.Default.Get(ctx, a, []float64{t}, quality, 960)
			if e != nil {
				return nil, e
			}
			if fs[0].Error != "" {
				return nil, fmt.Errorf("%s", fs[0].Error)
			}
			image = fs[0].JPEG
		}
		p, e := c.Predict(ctx, in.Text, image)
		if e != nil {
			return nil, e
		}
		vector, e = Normalize(p.CLIP)
		if e != nil {
			return nil, e
		}
	}
	video, e := UUID(in.VideoID)
	if e != nil {
		return nil, e
	}
	channel, e := UUID(in.ChannelID)
	if e != nil {
		return nil, e
	}
	creator, e := UUID(in.CreatorID)
	if e != nil {
		return nil, e
	}
	params := &db.SearchVisualEmbeddingsParams{Embedding: vector, ModelID: m.Fingerprint, VideoID: video, ChannelID: channel, CreatorID: creator, CandidateLimit: 200}
	for {
		rows, e := q.SearchVisualEmbeddings(ctx, params)
		if e != nil {
			return nil, e
		}
		out := consolidate(rows, limit)
		if len(out) >= limit || params.CandidateLimit == 1000 || len(rows) < int(params.CandidateLimit) {
			return withWindows(ctx, q, out), nil
		}
		params.CandidateLimit = 1000
	}
}

func consolidate(rows []*db.SearchVisualEmbeddingsRow, limit int) []*db.SearchVisualEmbeddingsRow {
	out := []*db.SearchVisualEmbeddingsRow{}
	for _, r := range rows {
		duplicate := false
		for _, old := range out {
			if r.VideoID == old.VideoID && math.Abs(r.SampleTs-old.SampleTs) <= 10 {
				duplicate = true
				break
			}
		}
		if !duplicate {
			out = append(out, r)
			if len(out) == limit {
				break
			}
		}
	}
	return out
}

func withWindows(ctx context.Context, q *db.Queries, rows []*db.SearchVisualEmbeddingsRow) []Moment {
	out := make([]Moment, 0, len(rows))
	ids := make([]pgtype.UUID, 0, len(rows))
	seen := map[pgtype.UUID]bool{}
	for _, r := range rows {
		if !seen[r.VideoID] {
			seen[r.VideoID] = true
			ids = append(ids, r.VideoID)
		}
	}
	byVideo := map[pgtype.UUID][]*db.ListContextWindowsForVideosRow{}
	if len(ids) > 0 {
		if windows, err := q.ListContextWindowsForVideos(ctx, ids); err == nil {
			for _, w := range windows {
				byVideo[w.VideoID] = append(byVideo[w.VideoID], w)
			}
		}
	}
	for _, r := range rows {
		var nearby []*db.ListContextWindowsForVideosRow
		for _, w := range byVideo[r.VideoID] {
			if r.SampleTs >= w.StartTs && r.SampleTs <= w.EndTs {
				nearby = append(nearby, w)
			}
		}
		out = append(out, Moment{Hit: r, Windows: nearby})
	}
	return out
}
