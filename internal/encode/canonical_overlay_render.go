package encode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"thirdcoast.systems/rewind/internal/stitch"
)

// applyCanonicalOverlays applies the immutable overlay list in z order. Each
// pass consumes the previous output, so vector and raster overlays retain their
// exact interleaving order.
func applyCanonicalOverlays(ctx context.Context, outputPath string, snapshot stitch.RenderSnapshot) error {
	if len(snapshot.Document.Overlays) == 0 {
		return nil
	}
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		if _, statErr := os.Stat(`C:\bin\ffmpeg.exe`); statErr != nil {
			return err
		}
		ffmpegPath = `C:\bin\ffmpeg.exe`
	}
	assets, err := verifiedOverlayAssets(snapshot)
	if err != nil {
		return err
	}
	overlays := append([]stitch.Overlay(nil), snapshot.Document.Overlays...)
	sort.SliceStable(overlays, func(i, j int) bool { return overlays[i].Z < overlays[j].Z })
	fontDir, err := writeSnapshotFonts(snapshot.Fonts)
	if err != nil && len(snapshot.Fonts) > 0 {
		return err
	}
	if fontDir != "" {
		defer os.RemoveAll(fontDir)
	}
	current := outputPath
	for i, overlay := range overlays {
		if !overlay.Visible || overlay.EndUS <= overlay.StartUS {
			continue
		}
		if overlay.Kind == "image" || overlay.AssetID != "" {
			asset, ok := assets[overlay.AssetID]
			if !ok {
				return fmt.Errorf("overlay %q asset %q is missing", overlay.ID, overlay.AssetID)
			}
			current, err = renderImageOverlay(ctx, ffmpegPath, current, outputPath, i, overlay, asset)
		} else {
			current, err = renderVectorOverlay(ctx, ffmpegPath, current, outputPath, i, overlay, snapshot.Document.Width, snapshot.Document.Height, fontDir)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

type overlayAsset struct {
	Path string
	Hash string
	Size int64
}

func verifiedOverlayAssets(snapshot stitch.RenderSnapshot) (map[string]overlayAsset, error) {
	out := map[string]overlayAsset{}
	for _, raw := range snapshot.Assets {
		var a struct {
			ID, Path, Hash string
			Size           int64
		}
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, fmt.Errorf("invalid overlay asset metadata: %w", err)
		}
		if a.ID == "" || a.Path == "" {
			return nil, fmt.Errorf("overlay asset metadata missing id/path")
		}
		st, err := os.Stat(a.Path)
		if err != nil {
			return nil, fmt.Errorf("asset %q: %w", a.ID, err)
		}
		if a.Size > 0 && st.Size() != a.Size {
			return nil, fmt.Errorf("asset %q size changed", a.ID)
		}
		data, err := os.ReadFile(a.Path)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		if a.Hash != "" && !strings.EqualFold(a.Hash, hex.EncodeToString(sum[:])) {
			return nil, fmt.Errorf("asset %q hash changed", a.ID)
		}
		out[a.ID] = overlayAsset{Path: a.Path, Hash: a.Hash, Size: st.Size()}
	}
	return out, nil
}

func renderVectorOverlay(ctx context.Context, ffmpegPath, input, output string, index int, o stitch.Overlay, width, height int, fontDir string) (string, error) {
	ass, err := CompileCanonicalOverlays(stitch.Document{Width: width, Height: height, Overlays: []stitch.Overlay{o}})
	if err != nil {
		return input, err
	}
	assPath := filepath.Join(filepath.Dir(output), fmt.Sprintf(".%s.overlay-%d.ass", filepath.Base(output), index))
	defer os.Remove(assPath)
	if err := os.WriteFile(assPath, []byte(ass), 0o600); err != nil {
		return input, err
	}
	ext := ".mp4"
	if strings.HasSuffix(strings.ToLower(output), ".webm") {
		ext = ".webm"
	}
	tmp := filepath.Join(filepath.Dir(output), fmt.Sprintf(".%s.overlay-%d.tmp%s", filepath.Base(output), index, ext))
	defer os.Remove(tmp)
	assFile := strings.ReplaceAll(filepath.ToSlash(assPath), ":", `\:`)
	assFile = strings.ReplaceAll(assFile, "'", "\\'")
	filter := "ass=filename='" + assFile + "'"
	if fontDir != "" {
		filter += ":fontsdir='" + strings.ReplaceAll(filepath.ToSlash(fontDir), ":", `\:`) + "'"
	}
	codec := "libx264"
	audioCodec := "copy"
	if ext == ".webm" {
		codec = "libvpx-vp9"
		audioCodec = "libopus"
	}
	cmd := exec.CommandContext(ctx, ffmpegPath, "-hide_banner", "-loglevel", "error", "-y", "-i", input, "-vf", filter, "-map", "0:v:0", "-map", "0:a?", "-c:v", codec, "-c:a", audioCodec, tmp)
	if out, err := cmd.CombinedOutput(); err != nil {
		return input, fmt.Errorf("overlay %q: %w (%s)", o.ID, err, strings.TrimSpace(string(out)))
	}
	if err := os.Remove(output); err != nil {
		return input, err
	}
	if err := os.Rename(tmp, output); err != nil {
		return input, err
	}
	return output, nil
}

func renderImageOverlay(ctx context.Context, ffmpegPath, input, output string, index int, o stitch.Overlay, asset overlayAsset) (string, error) {
	ext := ".mp4"
	if strings.HasSuffix(strings.ToLower(output), ".webm") {
		ext = ".webm"
	}
	tmp := filepath.Join(filepath.Dir(output), fmt.Sprintf(".%s.overlay-%d.tmp%s", filepath.Base(output), index, ext))
	defer os.Remove(tmp)
	start, end := float64(o.StartUS)/1e6, float64(o.EndUS)/1e6
	opacity := o.Opacity
	alpha := clamp(opacity, 0, 1)
	scaleW, scaleH := int(o.Width), int(o.Height)
	if scaleW <= 0 {
		scaleW = -1
	}
	if scaleH <= 0 {
		scaleH = -1
	}
	chain := fmt.Sprintf("[1:v]format=rgba,scale=%d:%d", scaleW, scaleH)
	if o.Rotation != 0 {
		chain += fmt.Sprintf(",rotate=%f*PI/180:fillcolor=none", o.Rotation)
	}
	chain += fmt.Sprintf(",colorchannelmixer=aa=%.6f[ov];[0:v][ov]overlay=x=%.3f:y=%.3f:enable='between(t,%.6f,%.6f)':eof_action=pass:shortest=1[v]", alpha, o.X, o.Y, start, end)
	codec := "libx264"
	audioCodec := "copy"
	if ext == ".webm" {
		codec = "libvpx-vp9"
		audioCodec = "libopus"
	}
	cmd := exec.CommandContext(ctx, ffmpegPath, "-hide_banner", "-loglevel", "error", "-y", "-i", input, "-loop", "1", "-i", asset.Path, "-filter_complex", chain, "-map", "[v]", "-map", "0:a?", "-c:v", codec, "-c:a", audioCodec, tmp)
	if out, err := cmd.CombinedOutput(); err != nil {
		return input, fmt.Errorf("image overlay %q: %w (%s)", o.ID, err, strings.TrimSpace(string(out)))
	}
	if err := os.Remove(output); err != nil {
		return input, err
	}
	if err := os.Rename(tmp, output); err != nil {
		return input, err
	}
	return output, nil
}
