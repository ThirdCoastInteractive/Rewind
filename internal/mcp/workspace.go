package mcp

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/plugin"
)

func workspaceTenant(ctx context.Context) (pgtype.UUID, bool, error) {
	id, scoped := plugin.TenantScope(ctx)
	if !scoped {
		return pgtype.UUID{}, false, nil
	}
	parsed, err := parseUUID(id)
	if err != nil {
		return pgtype.UUID{}, true, fmt.Errorf("workspace required")
	}
	return parsed, true, nil
}

func requireWorkspaceVideo(ctx context.Context, q *db.Queries, id pgtype.UUID) error {
	tenant, scoped, err := workspaceTenant(ctx)
	if err != nil || !scoped {
		return err
	}
	if _, err = q.GetVideoByIDAndTenant(ctx, &db.GetVideoByIDAndTenantParams{ID: id, TenantID: tenant}); err != nil {
		return fmt.Errorf("video is not in this workspace")
	}
	return nil
}

func requireWorkspaceSegments(ctx context.Context, q *db.Queries, segments []planSegmentInput) error {
	seen := map[string]struct{}{}
	for _, s := range segments {
		if _, ok := seen[s.VideoID]; ok {
			continue
		}
		seen[s.VideoID] = struct{}{}
		id, err := parseUUID(s.VideoID)
		if err != nil {
			return err
		}
		if err = requireWorkspaceVideo(ctx, q, id); err != nil {
			return err
		}
	}
	return nil
}
