package common

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/plugin"
	"thirdcoast.systems/rewind/pkg/plugin/builtin"
)

type videoLookup interface {
	GetVideoByID(ctx context.Context, id pgtype.UUID) (*db.Video, error)
	GetVideoByIDAndTenant(ctx context.Context, arg *db.GetVideoByIDAndTenantParams) (*db.Video, error)
}

// GetVideo loads a video by id. A non-empty actorTenant is fail-closed.
func GetVideo(ctx context.Context, q videoLookup, id pgtype.UUID, actorTenant string) (*db.Video, error) {
	if actorTenant == "" {
		return q.GetVideoByID(ctx, id)
	}
	t := db.ParseTenant(actorTenant)
	if !t.Valid {
		return nil, errInvalidTenant
	}
	return q.GetVideoByIDAndTenant(ctx, &db.GetVideoByIDAndTenantParams{ID: id, TenantID: t})
}

// RequireVideo loads the video for this HTTP request.
// Miss, deny, or invalid tenant → echo.NewHTTPError(http.StatusNotFound, "video not found").
// OSS (empty TenantID, LocalAuthz allows logged-in users) behaves as today.
func RequireVideo(c echo.Context, q videoLookup, id pgtype.UUID, action string) (*db.Video, error) {
	actorTenant := ""
	if a := ActorFrom(c); a != nil {
		actorTenant = a.TenantID
	}
	ctx := c.Request().Context()
	video, err := GetVideo(ctx, q, id, actorTenant)
	if err != nil || video == nil {
		return nil, echo.NewHTTPError(http.StatusNotFound, "video not found")
	}
	if g := plugin.Guards(); g != nil {
		if !g.Allow(ctx, ActorFrom(c), action, id.String()) {
			return nil, echo.NewHTTPError(http.StatusNotFound, "video not found")
		}
	}
	return video, nil
}

// WithActorTenant sets p.TenantID from ActorFrom.
// Empty tenant: leave unset (OSS / staff, unscoped list).
// Live Guards + empty tenant + non-admin: dead UUID (empty list, not all tenants).
// Invalid tenant UUID: set a valid UUID that matches no rows (fail closed, empty list).
func WithActorTenant(c echo.Context, p *db.ListVideosPaginatedParams) {
	if p == nil {
		return
	}
	a := ActorFrom(c)
	tenant := ""
	if a != nil {
		tenant = strings.TrimSpace(a.TenantID)
	}
	if tenant == "" {
		if DenyUnscopedLibrary(c) {
			p.TenantID = deadTenant()
		}
		return
	}
	t := db.ParseTenant(tenant)
	if !t.Valid {
		p.TenantID = deadTenant()
		return
	}
	p.TenantID = t
}

func deadTenant() pgtype.UUID {
	var dead pgtype.UUID
	_ = dead.Scan("ffffffff-ffff-ffff-ffff-ffffffffffff")
	return dead
}

// DenyUnscopedLibrary is true when live Guards are on, ActorTenantID is empty, and the actor is not admin.
func DenyUnscopedLibrary(c echo.Context) bool {
	if builtin.OSSGuards() {
		return false
	}
	a := ActorFrom(c)
	if a != nil && strings.TrimSpace(a.TenantID) != "" {
		return false
	}
	if a != nil && a.HasRole("admin") {
		return false
	}
	return true
}

var errInvalidTenant = errors.New("invalid tenant")
