package stitch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"os"
	"path/filepath"
	"strings"
	ffmpegfonts "thirdcoast.systems/rewind/pkg/ffmpeg"
	"thirdcoast.systems/rewind/pkg/typefaces"
)

// ResolvedRenderSegment is a render-time segment with absolute timeline bounds.
type ResolvedRenderSegment struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	ClipID      string          `json:"clip_id,omitempty"`
	VideoID     string          `json:"video_id,omitempty"`
	ExportJobID string          `json:"export_job_id,omitempty"`
	StartUS     int64           `json:"start_us"`
	DurationUS  int64           `json:"duration_us"`
	SourceInUS  int64           `json:"source_in_us"`
	Legacy      json.RawMessage `json:"legacy,omitempty"`
	Transition  *Transition     `json:"transition,omitempty"`
	Layout      *TeaserLayout   `json:"layout,omitempty"`
	Crops       []CameraCrop    `json:"crops,omitempty"`
	Shots       []CameraShot    `json:"shots,omitempty"`
	ClipStartUS int64           `json:"clip_start_us,omitempty"`
}

// RenderSnapshot is the immutable input passed to a future renderer.
type RenderSnapshot struct {
	Document Document                `json:"document"`
	Resolved []ResolvedRenderSegment `json:"resolved"`
	Options  RenderOptions           `json:"options"`
	Assets   []json.RawMessage       `json:"assets,omitempty"`
	Fonts    []FontReference         `json:"fonts,omitempty"`
}

// FontReference identifies an immutable installed font file.
type FontReference struct {
	Family string `json:"family"`
	Hash   string `json:"hash"`
	Path   string `json:"path"`
	Data   []byte `json:"data"`
}

// BuildRenderSnapshot freezes canonical state and render options for a job.
func BuildRenderSnapshot(d Document, o RenderOptions) (RenderSnapshot, error) {
	if err := Validate(d); err != nil {
		return RenderSnapshot{}, err
	}
	if err := validateRenderOptions(o, d); err != nil {
		return RenderSnapshot{}, err
	}
	b, _ := json.Marshal(d)
	var frozen Document
	if err := json.Unmarshal(b, &frozen); err != nil {
		return RenderSnapshot{}, err
	}
	out := RenderSnapshot{Document: frozen, Options: o}
	fonts := map[string]bool{}
	fonts["Tomorrow"] = true
	for _, c := range frozen.Captions {
		fonts[c.Style.Font] = true
	}
	for _, ov := range frozen.Overlays {
		fonts[ov.Font] = true
	}
	for _, seg := range frozen.Segments {
		var raw map[string]json.RawMessage
		if json.Unmarshal(seg.Legacy, &raw) == nil {
			var family string
			_ = json.Unmarshal(raw["font"], &family)
			fonts[family] = true
		}
	}
	for family := range fonts {
		if strings.TrimSpace(family) == "" {
			continue
		}
		var face typefaces.Face
		found := false
		for _, f := range typefaces.Installed() {
			if strings.EqualFold(f.Family, family) || strings.EqualFold(f.ID, family) {
				face = f
				found = true
				break
			}
		}
		if !found {
			return RenderSnapshot{}, fmt.Errorf("font %q is not installed", family)
		}
		path := face.Regular
		if path == "" {
			path = face.Bold
		}
		if path == "" && strings.EqualFold(face.Family, "Tomorrow") {
			_, path, _ = ffmpegfonts.TitleFontPaths()
		}
		if path == "" && strings.EqualFold(face.Family, "UnifrakturCook") {
			path = ffmpegfonts.TitleGothicPath()
		}
		if !filepath.IsAbs(path) && face.Dir != "" {
			path = filepath.Join(face.Dir, path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return RenderSnapshot{}, fmt.Errorf("font %q: %w", family, err)
		}
		if len(data) > 20<<20 {
			return RenderSnapshot{}, fmt.Errorf("font %q exceeds 20MB", family)
		}
		sum := sha256.Sum256(data)
		out.Fonts = append(out.Fonts, FontReference{Family: face.Family, Hash: hex.EncodeToString(sum[:]), Path: path, Data: data})
	}
	for _, rs := range Resolve(frozen) {
		s := rs.Segment
		out.Resolved = append(out.Resolved, ResolvedRenderSegment{ID: s.ID, Type: s.Type, ClipID: s.ClipID, VideoID: s.VideoID, ExportJobID: s.ExportJobID, StartUS: s.StartUS, DurationUS: s.DurationUS, SourceInUS: s.SourceInUS, Legacy: s.Legacy, Transition: s.Transition, Layout: cloneLayout(s.Layout), Crops: clone(s.Crops), Shots: clone(s.Shots), ClipStartUS: s.ClipStartUS})
	}
	return out, nil
}

// BuildRenderSnapshotWithAssets adds immutable project asset metadata to a snapshot.
func BuildRenderSnapshotWithAssets(ctx context.Context, tx pgx.Tx, owner, project pgtype.UUID, d Document, o RenderOptions) (RenderSnapshot, error) {
	s, err := BuildRenderSnapshot(d, o)
	if err != nil {
		return s, err
	}
	seen := map[string]bool{}
	for _, ov := range d.Overlays {
		if ov.AssetID == "" || seen[ov.AssetID] {
			continue
		}
		seen[ov.AssetID] = true
		var id pgtype.UUID
		var hash, path, mime string
		var w, h int
		var size int64
		if err = id.Scan(ov.AssetID); err != nil {
			return s, err
		}
		if err = tx.QueryRow(ctx, `SELECT id,hash,path,mime,width,height,size FROM stitch_assets WHERE id=$1 AND project_id=$2 AND owner_id=$3`, id, project, owner).Scan(&id, &hash, &path, &mime, &w, &h, &size); err != nil {
			return s, err
		}
		raw, _ := json.Marshal(map[string]any{"id": id.String(), "hash": hash, "path": path, "mime": mime, "width": w, "height": h, "size": size})
		s.Assets = append(s.Assets, raw)
	}
	return s, nil
}

// CompileRenderSnapshot converts frozen segments into renderer inputs, inserting explicit gaps.
func CompileRenderSnapshot(s RenderSnapshot) ([]map[string]any, error) {
	if len(s.Resolved) == 0 {
		return nil, fmt.Errorf("render snapshot has no segments")
	}
	var out []map[string]any
	var cursor int64
	for _, r := range s.Resolved {
		if r.StartUS > cursor {
			out = append(out, map[string]any{"type": "gap", "start_us": cursor, "duration_us": r.StartUS - cursor})
		}
		if r.DurationUS <= 0 {
			return nil, fmt.Errorf("segment %s has invalid duration", r.ID)
		}
		x := map[string]any{"id": r.ID, "type": r.Type, "clip_id": r.ClipID, "video_id": r.VideoID, "export_job_id": r.ExportJobID, "start_us": r.StartUS, "duration_us": r.DurationUS, "source_in_us": r.SourceInUS, "legacy": r.Legacy}
		if r.Transition != nil {
			x["transition"] = r.Transition
		}
		if r.Layout != nil {
			x["layout"] = r.Layout
		}
		if len(r.Crops) > 0 {
			x["crops"] = r.Crops
		}
		if len(r.Shots) > 0 {
			x["shots"] = r.Shots
		}
		if r.ClipStartUS != 0 {
			x["clip_start_us"] = r.ClipStartUS
		}
		out = append(out, x)
		if end := r.StartUS + r.DurationUS; end > cursor {
			cursor = end
		}
	}
	return out, nil
}
