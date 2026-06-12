package templates

import (
	"context"

	"thirdcoast.systems/rewind/cmd/web/ctxkeys"
)

// versionedAsset appends a cache-busting query parameter to a /static/dist/ path.
// The version is a content hash of all dist assets, set in request context by middleware.
func versionedAsset(ctx context.Context, path string) string {
	if v, ok := ctx.Value(ctxkeys.StaticVersion).(string); ok && v != "" {
		return path + "?v=" + v
	}
	return path
}
