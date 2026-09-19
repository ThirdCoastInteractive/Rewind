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
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		src := filepath.Join(dir, e.Name())
		key := plugin.VideoKey(videoID, e.Name())
		if p, ok := b.LocalPath(key); ok {
			if samePath(p, src) {
				continue
			}
		}
		in, err := os.Open(src)
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
	}
	return nil
}

func samePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return aa == bb
}
