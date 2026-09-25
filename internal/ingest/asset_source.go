package ingest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"thirdcoast.systems/rewind/pkg/plugin"
)

func isPrivateMasterKey(path string) bool {
	parts := strings.Split(strings.TrimSpace(path), "/")
	return len(parts) >= 3 && parts[0] == "org" && parts[1] != ""
}

// assetWork is an ffmpeg input plus a writable directory for sidecars.
type assetWork struct {
	Src     string // local path or HTTP URL for ffmpeg
	Dir     string // where thumbnails/seek/waveform/preview are written
	cleanup func()
}

func (w *assetWork) Cleanup() {
	if w == nil || w.cleanup == nil {
		return
	}
	w.cleanup()
	w.cleanup = nil
}

// prepareAssetWork resolves ffmpeg input and an output directory for derived assets.
// Local masters use VideoDir / the file's parent. Remote masters use plugin.MasterSource
// and a spool directory when Blob has no LocalPath.
func prepareAssetWork(ctx context.Context, videoID, videoPath string) (*assetWork, error) {
	videoID = strings.TrimSpace(videoID)
	videoPath = strings.TrimSpace(videoPath)
	if videoID == "" {
		return nil, fmt.Errorf("missing video id")
	}

	src, srcCleanup, err := resolveAssetSource(ctx, videoID, videoPath)
	if err != nil {
		return nil, err
	}

	dir, spool, err := resolveAssetOutDir(videoID, videoPath)
	if err != nil {
		if srcCleanup != nil {
			srcCleanup()
		}
		return nil, err
	}

	cleanup := func() {
		if srcCleanup != nil {
			srcCleanup()
		}
		if spool {
			_ = os.RemoveAll(dir)
		}
	}
	return &assetWork{Src: src, Dir: dir, cleanup: cleanup}, nil
}

func resolveAssetSource(ctx context.Context, videoID, videoPath string) (string, func(), error) {
	if plugin.LiveIngest() != nil {
		if _, scoped := plugin.TenantScope(ctx); !scoped {
			return "", nil, fmt.Errorf("workspace tenant scope required for live asset")
		}
		return plugin.MasterSourceAt(ctx, videoPath)
	}
	if videoPath != "" {
		if st, err := os.Stat(videoPath); err == nil && st.Mode().IsRegular() {
			return videoPath, nil, nil
		}
		if src, cleanup, err := plugin.MasterSourceAt(ctx, videoPath); err == nil {
			return src, cleanup, nil
		} else if plugin.LiveIngest() != nil {
			return "", nil, fmt.Errorf("authoritative master source: %w", err)
		}
	}
	if p, ok := plugin.LocalMaster(videoID); ok {
		return p, nil, nil
	}
	src, cleanup, err := plugin.MasterSource(ctx, videoID)
	if err != nil {
		return "", nil, fmt.Errorf("master source: %w", err)
	}
	if cleanup == nil {
		cleanup = func() {}
	}
	return src, cleanup, nil
}

func resolveAssetOutDir(videoID, videoPath string) (dir string, spool bool, err error) {
	if d, err := plugin.VideoDir(videoID); err == nil && strings.TrimSpace(d) != "" {
		if mkErr := os.MkdirAll(d, 0755); mkErr != nil {
			return "", false, fmt.Errorf("mkdir video dir: %w", mkErr)
		}
		return d, false, nil
	}
	if videoPath != "" {
		if st, err := os.Stat(videoPath); err == nil && st.Mode().IsRegular() {
			return filepath.Dir(videoPath), false, nil
		}
		parent := filepath.Dir(videoPath)
		if parent != "" && parent != "." && parent != string(filepath.Separator) {
			if st, err := os.Stat(parent); err == nil && st.IsDir() {
				// Only reuse an existing local parent; never treat a blob key as a path tree.
				if root, ok := plugin.LocalRoot(); ok {
					absParent, _ := filepath.Abs(parent)
					absRoot, _ := filepath.Abs(root)
					if absRoot != "" && absParent != "" && strings.HasPrefix(absParent, absRoot) {
						return parent, false, nil
					}
				}
			}
		}
	}

	base := strings.TrimSpace(os.Getenv("SPOOL_DIR"))
	if base == "" {
		base = os.TempDir()
	}
	if err := os.MkdirAll(base, 0755); err != nil {
		base = os.TempDir()
	}
	dir, err = os.MkdirTemp(base, "rewind-assets-"+videoID+"-")
	if err != nil {
		return "", false, fmt.Errorf("mkdir asset spool: %w", err)
	}
	return dir, true, nil
}
