package ingest

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"thirdcoast.systems/rewind/pkg/plugin"
)

func persistDirToBlob(ctx context.Context, videoID, dir string) error {
	b := plugin.Blobs()
	if b == nil {
		return nil
	}
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		key := plugin.VideoKey(videoID, filepath.ToSlash(rel))
		if p, ok := b.LocalPath(key); ok {
			if samePath(p, path) {
				return nil
			}
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		out, err := b.Create(ctx, key)
		if err != nil {
			in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		in.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		return nil
	})
}

func samePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return aa == bb
}
