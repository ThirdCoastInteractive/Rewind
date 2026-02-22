// package clip_api provides clip-related API handlers.
package clip_api

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"thirdcoast.systems/rewind/internal/db"
)

// cleanupClipExportsLRU removes old clip exports to stay under the storage limit.
func cleanupClipExportsLRU(ctx context.Context, dbc *db.DatabaseConnection) {
	q := dbc.Queries(ctx)

	limit, err := q.GetClipExportStorageLimit(ctx)
	if err != nil || limit == 0 {
		return
	}

	totalSize, err := q.GetTotalClipExportSize(ctx)
	if err != nil {
		return
	}

	if totalSize <= limit {
		return
	}

	toFree := totalSize - limit
	freed := int64(0)

	exports, err := q.ListOldestClipExportsForCleanup(ctx)
	if err != nil {
		return
	}
	if len(exports) <= 1 {
		return
	}
	protectedID := exports[len(exports)-1].ID

	for _, exp := range exports {
		if freed >= toFree {
			break
		}
		if exp.ID == protectedID {
			continue
		}

		_ = os.Remove(exp.FilePath)

		if err := q.DeleteClipExport(ctx, exp.ID); err != nil {
			slog.Warn("failed to delete clip export", "id", exp.ID, "error", err)
			continue
		}

		freed += exp.SizeBytes
	}
}

// safeClipExportDir returns a safe directory path for clip exports.
func safeClipExportDir(clipID string) string {
	return filepath.Join(string(os.PathSeparator), "exports", "clips", clipID)
}

// ensureDir creates a directory and all parent directories.
func ensureDir(path string) error {
	return os.MkdirAll(path, 0755)
}
